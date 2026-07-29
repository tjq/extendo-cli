package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	// Registering the decoders lets image.DecodeConfig read the bounds of any
	// blob the app captured. Blank imports are the only way to do this.
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/tiff"
)

// ErrNoStore reports that the directory holds no history file — extendo has
// either never run or keeps its data somewhere else.
var ErrNoStore = errors.New("extendo history not found")

// ErrNotFound reports that no item matched the caller's index or ID.
var ErrNotFound = errors.New("no matching item")

// ErrAmbiguous reports that an ID prefix matched more than one item.
var ErrAmbiguous = errors.New("ambiguous item prefix")

// ErrUnsafePath reports a path in history.json that does not stay inside the
// store directory.
var ErrUnsafePath = errors.New("path escapes the store directory")

// Store reads the clipboard history rooted at Dir.
type Store struct {
	Dir string
}

// Open returns a store rooted at dir. It touches no files.
func Open(dir string) *Store {
	return &Store{Dir: dir}
}

// bundleID is the extendo app's identifier, which names its sandbox container.
const bundleID = "com.tjq.extendo"

// DefaultDir is where the extendo app keeps its data, unless EXTENDO_STORE_DIR
// overrides it.
//
// There are two candidates because the app may be sandboxed. A sandboxed
// process asking for Application Support is redirected by macOS into
// ~/Library/Containers/<bundle-id>/Data, so that is where a signed build of
// extendo writes its history. ext is not sandboxed and gets no such redirect,
// so it has to look in both: the unsandboxed path first, since a development
// build writes there, then the container.
func DefaultDir() string {
	if dir := os.Getenv("EXTENDO_STORE_DIR"); dir != "" {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	appSupport := filepath.Join("Library", "Application Support", "extendo")
	candidates := []string{
		filepath.Join(home, appSupport),
		filepath.Join(home, "Library", "Containers", bundleID, "Data", appSupport),
	}

	// A history file is the conclusive signal, so it decides before mere
	// existence does: an installed app that has been launched but never used
	// leaves an empty directory behind, and that should not outrank the
	// candidate holding actual items.
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "history.json")); err == nil {
			return dir
		}
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	// Neither is there. Report the unsandboxed path, which is the one to create.
	return candidates[0]
}

// HistoryPath is the JSON file holding every item's metadata.
func (s *Store) HistoryPath() string {
	return filepath.Join(s.Dir, "history.json")
}

// blobPath resolves an external payload path, which is relative to <dir>/blobs.
//
// The path comes out of history.json, which ext reads but does not own: it is
// written by the app, and by anything else that can reach the file. A `path` of
// "../../.ssh/id_rsa" would otherwise be read and rendered as a clipboard item,
// so anything that is not a relative path staying under blobs is refused
// instead of joined.
func (s *Store) blobPath(rel string) (string, error) {
	local := filepath.FromSlash(rel)
	if !filepath.IsLocal(local) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}

	return filepath.Join(s.Dir, "blobs", local), nil
}

// Load reads every item in the history, in the order the file lists them. It
// returns ErrNoStore when the history file does not exist.
func (s *Store) Load() ([]Item, error) {
	data, err := os.ReadFile(s.HistoryPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w at %s", ErrNoStore, s.HistoryPath())
	}

	if err != nil {
		return nil, fmt.Errorf("reading history: %w", err)
	}

	items := []Item{}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.HistoryPath(), err)
	}

	return items, nil
}

// defaultHistoryMode is the permission a freshly created history file gets.
// os.CreateTemp makes files 0600, so Save always sets the mode explicitly
// rather than tightening what the app wrote.
const defaultHistoryMode = 0o644

// Save replaces the history file with items. The write goes to a temp file in
// s.Dir — the same filesystem, so the rename below is atomic — and a reader
// racing with us sees either the whole old file or the whole new one.
func (s *Store) Save(items []Item) error {
	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("encoding history: %w", err)
	}

	tmp, err := os.CreateTemp(s.Dir, "history-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", s.Dir, err)
	}

	tmpPath := tmp.Name()

	if err := s.writeTemp(tmp, data); err != nil {
		_ = os.Remove(tmpPath)

		return err
	}

	if err := os.Rename(tmpPath, s.HistoryPath()); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("replacing %s: %w", s.HistoryPath(), err)
	}

	return nil
}

// writeTemp fills and closes an open temp file, giving it the mode the history
// file already carries.
func (s *Store) writeTemp(tmp *os.File, data []byte) error {
	mode := os.FileMode(defaultHistoryMode)
	if info, err := os.Stat(s.HistoryPath()); err == nil {
		mode = info.Mode().Perm()
	}

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()

		return fmt.Errorf("setting mode on %s: %w", tmp.Name(), err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()

		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmp.Name(), err)
	}

	return nil
}

// TogglePin flips an item's pinned flag and reports its new state.
//
// The history is re-read here rather than taken from the caller: the macOS app
// owns the same file and may have appended or trimmed items since the caller
// loaded it, and writing back a stale slice would erase those changes.
func (s *Store) TogglePin(id string) (bool, error) {
	items, err := s.Load()
	if err != nil {
		return false, err
	}

	index := indexOfID(items, id)
	if index < 0 {
		return false, fmt.Errorf("%w with id %s", ErrNotFound, id)
	}

	items[index].IsPinned = !items[index].IsPinned

	if err := s.Save(items); err != nil {
		return false, err
	}

	return items[index].IsPinned, nil
}

// Delete drops an item from the history and removes its blob directory. Like
// TogglePin it re-reads the file first, so a concurrent write by the app is
// preserved.
func (s *Store) Delete(id string) error {
	items, err := s.Load()
	if err != nil {
		return err
	}

	index := indexOfID(items, id)
	if index < 0 {
		return fmt.Errorf("%w with id %s", ErrNotFound, id)
	}

	// The blob directory is named after the item's ID, which is a string out of
	// history.json rather than anything ext generated. An ID of ".." would
	// aim os.RemoveAll at the whole store, so an ID that does not name a
	// directory inside blobs loses its blobs rather than its neighbours'.
	blobs := ""
	if id := items[index].ID; filepath.IsLocal(id) {
		blobs = filepath.Join(s.Dir, "blobs", id)
	}

	if err := s.Save(slices.Delete(items, index, index+1)); err != nil {
		return err
	}

	// Best effort: the history no longer points at these blobs, so failing to
	// unlink them wastes disk but leaves the store consistent.
	if blobs != "" {
		_ = os.RemoveAll(blobs)
	}

	return nil
}

// Resolve picks the item a user's argument names: a 1-based index into items,
// or a case-insensitive ID prefix. items must already be Sorted, so the index
// matches what the caller printed.
func (s *Store) Resolve(items []Item, arg string) (Item, error) {
	if index, err := strconv.Atoi(arg); err == nil {
		if index < 1 || index > len(items) {
			return Item{}, fmt.Errorf("%w: index %d is outside 1..%d", ErrNotFound, index, len(items))
		}

		return items[index-1], nil
	}

	if arg == "" {
		return Item{}, fmt.Errorf("%w: empty index or id", ErrNotFound)
	}

	matches := []Item{}

	for _, it := range items {
		if strings.HasPrefix(strings.ToLower(it.ID), strings.ToLower(arg)) {
			matches = append(matches, it)
		}
	}

	switch len(matches) {
	case 0:
		return Item{}, fmt.Errorf("%w with id prefix %q", ErrNotFound, arg)
	case 1:
		return matches[0], nil
	default:
		return Item{}, fmt.Errorf("%w: %q matches %d items", ErrAmbiguous, arg, len(matches))
	}
}

// indexOfID locates an item by full ID, returning -1 when absent. The compare
// ignores case so a hand-typed ID matches the app's uppercase UUIDs.
func indexOfID(items []Item, id string) int {
	return slices.IndexFunc(items, func(it Item) bool {
		return strings.EqualFold(it.ID, id)
	})
}

// RepData returns a representation's bytes, reading the blob file when the
// payload is stored externally.
func (s *Store) RepData(it Item, rep Representation) ([]byte, error) {
	if rep.Payload.Path == "" {
		return rep.Payload.Inline, nil
	}

	path, err := s.blobPath(rep.Payload.Path)
	if err != nil {
		return nil, fmt.Errorf("blob for item %s: %w", it.ID, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading blob for item %s: %w", it.ID, err)
	}

	return data, nil
}

// PlainText returns the item's text content, preferring the most specific
// plain-text pasteboard type present. The second result is false when the item
// carries no text at all.
func (s *Store) PlainText(it Item) (string, bool) {
	rep, ok := it.findRep(textTypes)
	if !ok {
		return "", false
	}

	data, err := s.RepData(it, rep)
	if err != nil {
		return "", false
	}

	return string(data), true
}

// ImageInfo is a decoded image representation: the picture itself, the encoding
// it was stored in, and how many bytes the blob holding it takes.
type ImageInfo struct {
	Image  image.Image
	Format string // as image.Decode names it: "png", "jpeg", "tiff"
	Bytes  int
}

// ImageInfo decodes the item's image representation, preferring the most
// specific type present. The second result is false when the item carries no
// image, when its blob cannot be read, or when nothing can decode it.
//
// Decoding a whole image is much more expensive than the DisplayLabel path,
// which reads dimensions from the header alone, so this is for the preview
// pane rather than for anything drawn per row.
func (s *Store) ImageInfo(it Item) (ImageInfo, bool) {
	rep, ok := it.findRep(imageTypes)
	if !ok {
		return ImageInfo{}, false
	}

	data, err := s.RepData(it, rep)
	if err != nil {
		return ImageInfo{}, false
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ImageInfo{}, false
	}

	return ImageInfo{Image: img, Format: format, Bytes: len(data)}, true
}

// DisplayLabel renders a single-line summary of the item, suitable for a list
// row. The result is always printable — a filename and a pasteboard type are
// as much someone else's bytes as the text is.
func (s *Store) DisplayLabel(it Item) string {
	return Printable(s.label(it))
}

func (s *Store) label(it Item) string {
	switch it.Kind() {
	case KindImage:
		return s.imageLabel(it)
	case KindFile:
		return s.fileLabel(it)
	case KindText, KindRichText:
		return textLabel(s.PlainText(it))
	default:
		if len(it.Reps) > 0 {
			return it.Reps[0].Type
		}

		return "(empty)"
	}
}

// Printable makes text safe to write to a terminal: newlines survive, because
// every caller either reflows them or cuts at the first one; a tab becomes a
// single space; everything else a terminal acts on instead of printing, or
// draws nothing for, is dropped.
//
// Clipboard text is arbitrary bytes from arbitrary places. A copy button on a
// web page can plant "\x1b]52;c;<base64>\x07" in what it puts on the
// pasteboard, and any tool that echoes that back is handing the terminal an
// instruction to rewrite the system clipboard; "\x1b[2J" blanks the screen and
// "\x1b[?1049l" tears down an alt-screen. An escape sequence takes up no
// display width, so nothing about the line hiding it looks wrong.
//
// Tabs go here rather than being left for each caller to expand, so the rule
// holds wherever this is applied and not only where someone remembered: a tab
// is a column separator to text/tabwriter and an unmeasurable nothing to a
// fixed-width frame. Indentation survives as one space per level.
//
// Unicode's format characters (category Cf) go the same way. They are not
// control characters, so IsControl leaves them, and they cost no display width
// either: U+202E RIGHT-TO-LEFT OVERRIDE draws "invoice‮gpj.exe" as
// invoiceexe.jpg, and a soft hyphen or a zero-width space splits a word the eye
// reads as whole. A row is a claim about what copying the item hands over, and
// these are exactly the characters that make it lie. The cost is that an emoji
// joined by U+200D comes apart into its pieces, which is a wrong picture rather
// than a wrong claim.
//
// Only the display path sanitizes. What goes back onto the pasteboard, and what
// `get --print` writes, stay byte-for-byte what was captured.
func Printable(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		default:
			return r
		}
	}, text)
}

// textLabel collapses text down to its first printable line with runs of
// whitespace squeezed to single spaces.
//
// It sanitizes before collapsing rather than leaving it to DisplayLabel: a
// stripped escape sequence between two words would otherwise leave the gap it
// was hiding in.
func textLabel(text string, ok bool) string {
	if !ok {
		return "(empty)"
	}

	first, _, _ := strings.Cut(Printable(strings.ReplaceAll(text, "\r\n", "\n")), "\n")

	collapsed := strings.Join(strings.Fields(first), " ")
	if collapsed == "" {
		return "(empty)"
	}

	return collapsed
}

// imageLabel describes the image's dimensions and encoding, degrading to a bare
// "Image" when the blob is missing or undecodable.
func (s *Store) imageLabel(it Item) string {
	rep, ok := it.findRep(imageTypes)
	if !ok {
		return "Image"
	}

	data, err := s.RepData(it, rep)
	if err != nil {
		return "Image"
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "Image"
	}

	return fmt.Sprintf("Image %d×%d · %s", cfg.Width, cfg.Height, strings.ToUpper(format))
}

// fileLabel lists the basenames of every file URL the item carries.
func (s *Store) fileLabel(it Item) string {
	names := []string{}

	for _, rep := range it.Reps {
		if !slices.Contains(fileTypes, rep.Type) {
			continue
		}

		data, err := s.RepData(it, rep)
		if err != nil {
			continue
		}

		names = append(names, fileBasename(string(data)))
	}

	if len(names) == 0 {
		return "(empty)"
	}

	return strings.Join(names, ", ")
}

// fileBasename extracts the last path component of a file URL, undoing percent
// encoding when the URL parses.
func fileBasename(raw string) string {
	trimmed := strings.TrimSpace(raw)

	if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
		return path.Base(strings.TrimSuffix(parsed.Path, "/"))
	}

	return path.Base(strings.TrimSuffix(trimmed, "/"))
}

// findRep returns the item's representation whose type comes first in types,
// so callers get the most specific match rather than whichever the app happened
// to write first.
func (it Item) findRep(types []string) (Representation, bool) {
	for _, want := range types {
		for _, rep := range it.Reps {
			if rep.Type == want {
				return rep, true
			}
		}
	}

	return Representation{}, false
}

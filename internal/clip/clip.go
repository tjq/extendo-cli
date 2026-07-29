// Package clip restores clipboard-history items to the macOS pasteboard,
// driving pbcopy for text and osascript for the typed flavors AppleScript can
// place directly (images, RTF, file references).
package clip

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/tjq/extendo-cli/internal/store"
)

// ErrUnsupportedKind reports an item carrying no representation this package
// knows how to put back on the pasteboard.
var ErrUnsupportedKind = errors.New("unsupported item kind")

const (
	fileURLType = "public.file-url"
	rtfType     = "public.rtf"

	// classRTF is padded to four characters. The trailing space is
	// load-bearing: AppleScript does not recognise «class RTF».
	classRTF = "«class RTF »"
)

// imageFlavors maps the image UTIs the app stores to the AppleScript four-char
// class that reads them back, in the order we prefer them. PNG first because it
// is lossless and the smallest of the three; TIFF last because the app writes
// it for everything and it is the bulkiest.
var imageFlavors = []struct {
	repType   string
	class     string
	extension string
}{
	{repType: "public.png", class: "«class PNGf»", extension: "png"},
	{repType: "public.jpeg", class: "«class JPEG»", extension: "jpeg"},
	{repType: "public.tiff", class: "«class TIFF»", extension: "tiff"},
}

// Copy puts an item back on the pasteboard. When the extendo app is running its
// monitor re-ingests the copy as the most recent item, exactly as the popup
// does.
func Copy(s *store.Store, it store.Item, r Runner) error {
	switch it.Kind() {
	case store.KindImage:
		return copyImage(s, it, r)
	case store.KindFile:
		return copyFiles(s, it, r)
	case store.KindRichText:
		return copyRichText(s, it, r)
	case store.KindText:
		return copyText(s, it, r)
	default:
		return ErrUnsupportedKind
	}
}

// copyText is the plain-text path, and the fallback for anything richer whose
// specific flavor we could not use.
func copyText(s *store.Store, it store.Item, r Runner) error {
	text, ok := s.PlainText(it)
	if !ok {
		return fmt.Errorf("%w: item %s carries no readable text", ErrUnsupportedKind, it.ID)
	}

	if _, err := r.Run([]byte(text), "pbcopy"); err != nil {
		return fmt.Errorf("copying text: %w", err)
	}

	return nil
}

// copyRichText prefers RTF, which AppleScript can set as a typed flavor, and
// degrades to plain text for HTML-only items — AppleScript has no HTML class.
func copyRichText(s *store.Store, it store.Item, r Runner) error {
	rep, ok := findRep(it, rtfType)
	if !ok {
		return copyText(s, it, r)
	}

	data, err := s.RepData(it, rep)
	if err != nil {
		return err
	}

	return copyTypedFile(r, data, "rtf", classRTF)
}

func copyImage(s *store.Store, it store.Item, r Runner) error {
	for _, flavor := range imageFlavors {
		rep, ok := findRep(it, flavor.repType)
		if !ok {
			continue
		}

		data, err := s.RepData(it, rep)
		if err != nil {
			return err
		}

		return copyTypedFile(r, data, flavor.extension, flavor.class)
	}

	return fmt.Errorf("%w: item %s carries no supported image type", ErrUnsupportedKind, it.ID)
}

// copyTypedFile hands data to the pasteboard through a temp file, the only
// route AppleScript offers for non-text flavors. The file is removed whether or
// not osascript succeeds.
func copyTypedFile(r Runner, data []byte, extension, class string) error {
	path, err := writeTemp(data, extension)
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(path) }()

	script := fmt.Sprintf("set the clipboard to (read (POSIX file %s) as %s)", quote(path), class)

	if _, err := r.Run(nil, "osascript", "-e", script); err != nil {
		return fmt.Errorf("copying %s data: %w", extension, err)
	}

	return nil
}

// copyFiles sets the pasteboard to file references, so a Finder paste moves the
// actual files rather than their paths as text. The braces list is used even
// for a single file — AppleScript accepts a one-element list.
func copyFiles(s *store.Store, it store.Item, r Runner) error {
	refs := []string{}

	for _, rep := range it.Reps {
		if rep.Type != fileURLType {
			continue
		}

		data, err := s.RepData(it, rep)
		if err != nil {
			return err
		}

		path, err := posixPath(string(data))
		if err != nil {
			return err
		}

		refs = append(refs, "POSIX file "+quote(path))
	}

	if len(refs) == 0 {
		return fmt.Errorf("%w: item %s carries no file url", ErrUnsupportedKind, it.ID)
	}

	script := fmt.Sprintf("set the clipboard to {%s}", strings.Join(refs, ", "))

	if _, err := r.Run(nil, "osascript", "-e", script); err != nil {
		return fmt.Errorf("copying file references: %w", err)
	}

	return nil
}

// posixPath turns a pasteboard file-url payload into a filesystem path. The
// payload is normally a percent-encoded file:// URL, but the pasteboard also
// carries NUL-terminated strings and, from some apps, bare paths.
func posixPath(raw string) (string, error) {
	trimmed := strings.Trim(raw, " \t\r\n\x00")
	if trimmed == "" {
		return "", errors.New("empty file url")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" {
		return trimmed, nil
	}

	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported file url scheme %q", parsed.Scheme)
	}

	return parsed.Path, nil
}

// writeTemp spills data to a uniquely named temp file carrying the extension
// AppleScript expects for the flavor.
func writeTemp(data []byte, extension string) (string, error) {
	tmp, err := os.CreateTemp("", "ext-*."+extension)
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}

	path := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(path)

		return "", fmt.Errorf("writing %s: %w", path, err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)

		return "", fmt.Errorf("closing %s: %w", path, err)
	}

	return path, nil
}

// quote renders a path as an AppleScript string literal. Backslash and double
// quote are the only characters a literal escapes, and macOS allows both in
// filenames.
func quote(path string) string {
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)

	return `"` + escaped + `"`
}

// findRep returns the item's representation of exactly repType.
func findRep(it store.Item, repType string) (store.Representation, bool) {
	for _, rep := range it.Reps {
		if rep.Type == repType {
			return rep, true
		}
	}

	return store.Representation{}, false
}

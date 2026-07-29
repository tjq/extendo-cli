package store_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/tjq/extendo-cli/internal/store"
)

// refDate is the Foundation reference date: createdAt is seconds since here.
var refDate = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// fixtureDir copies testdata/history_sample.json into a fresh temp directory as
// history.json, so tests get a real store layout they may also write blobs into.
func fixtureDir(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "history_sample.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "history.json"), data, 0o644); err != nil {
		t.Fatalf("write history.json: %v", err)
	}

	return dir
}

func loadFixture(t *testing.T) (*store.Store, []store.Item) {
	t.Helper()

	s := store.Open(fixtureDir(t))

	items, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return s, items
}

func TestLoadFixture(t *testing.T) {
	s, items := loadFixture(t)

	if len(items) != 4 {
		t.Fatalf("len(items) = %d, expected 4", len(items))
	}

	first := items[0]
	if first.ID != "AAAAAAAA-1111-1111-1111-111111111111" {
		t.Errorf("items[0].ID = %q, expected AAAAAAAA-1111-1111-1111-111111111111", first.ID)
	}

	if first.IsPinned {
		t.Error("items[0].IsPinned = true, expected false")
	}

	if first.SourceBundle != "com.apple.Terminal" {
		t.Errorf("items[0].SourceBundle = %q, expected com.apple.Terminal", first.SourceBundle)
	}

	expectedCreated := refDate.Add(774300000 * time.Second)
	if !first.CreatedAt.Equal(expectedCreated) {
		t.Errorf("items[0].CreatedAt = %v, expected %v", first.CreatedAt, expectedCreated)
	}

	if first.Kind() != store.KindText {
		t.Errorf("items[0].Kind() = %v, expected KindText", first.Kind())
	}

	text, ok := s.PlainText(first)
	if !ok || text != "git rebase -i HEAD~3" {
		t.Errorf("PlainText(items[0]) = %q, %v; expected %q, true", text, ok, "git rebase -i HEAD~3")
	}

	// The image rep wins over the plain-text rep on the same item.
	if items[2].Kind() != store.KindImage {
		t.Errorf("items[2].Kind() = %v, expected KindImage", items[2].Kind())
	}

	if items[3].Kind() != store.KindFile {
		t.Errorf("items[3].Kind() = %v, expected KindFile", items[3].Kind())
	}

	if label := s.DisplayLabel(items[3]); label != "report-final.pdf" {
		t.Errorf("DisplayLabel(items[3]) = %q, expected report-final.pdf", label)
	}

	if !items[1].IsPinned {
		t.Error("items[1].IsPinned = false, expected true")
	}
}

func TestSorted(t *testing.T) {
	_, items := loadFixture(t)

	sorted := store.Sorted(items)

	expected := []string{
		"BBBBBBBB-2222-2222-2222-222222222222",
		"AAAAAAAA-1111-1111-1111-111111111111",
		"CCCCCCCC-3333-3333-3333-333333333333",
		"DDDDDDDD-4444-4444-4444-444444444444",
	}

	if len(sorted) != len(expected) {
		t.Fatalf("len(sorted) = %d, expected %d", len(sorted), len(expected))
	}

	for i, want := range expected {
		if sorted[i].ID != want {
			t.Errorf("sorted[%d].ID = %q, expected %q", i, sorted[i].ID, want)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := store.Open(t.TempDir()).Load()
	if err == nil {
		t.Fatal("Load on empty dir returned nil error, expected ErrNoStore")
	}

	if !errors.Is(err, store.ErrNoStore) {
		t.Errorf("Load error = %v, expected ErrNoStore", err)
	}
}

func TestRoundTripUnknownFields(t *testing.T) {
	_, items := loadFixture(t)

	out, err := json.Marshal(items[0])
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if !bytes.Contains(out, []byte(`"futureField"`)) {
		t.Errorf("marshalled item dropped futureField: %s", out)
	}

	if !bytes.Contains(out, []byte(`"createdAt":774300000`)) {
		t.Errorf("createdAt did not round-trip byte-identically: %s", out)
	}

	if !bytes.Contains(out, []byte(`"isPinned":false`)) {
		t.Errorf("marshalled item missing isPinned: %s", out)
	}
}

// isPinned is the sole field this CLI ever writes back, so re-encoding must
// carry a flipped flag through while leaving every other key untouched.
func TestRoundTripOverwritesIsPinned(t *testing.T) {
	_, items := loadFixture(t)

	item := items[0]
	item.IsPinned = true

	out, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if !bytes.Contains(out, []byte(`"isPinned":true`)) {
		t.Errorf("flipped IsPinned did not reach the output: %s", out)
	}

	if !bytes.Contains(out, []byte(`"futureField"`)) {
		t.Errorf("overwriting isPinned dropped futureField: %s", out)
	}

	var reloaded []store.Item
	if err := json.Unmarshal([]byte("["+string(out)+"]"), &reloaded); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if !reloaded[0].IsPinned {
		t.Error("reloaded item IsPinned = false, expected true")
	}

	if !reloaded[0].CreatedAt.Equal(item.CreatedAt) {
		t.Errorf("reloaded CreatedAt = %v, expected %v", reloaded[0].CreatedAt, item.CreatedAt)
	}
}

// Items built in code rather than decoded have no raw map, so marshalling falls
// back to the known fields — including createdAt in Foundation seconds.
func TestMarshalItemWithoutRaw(t *testing.T) {
	item := store.Item{
		ID:        "EEEEEEEE-5555-5555-5555-555555555555",
		CreatedAt: refDate.Add(774300000 * time.Second),
		IsPinned:  true,
		Reps: []store.Representation{
			{Type: "public.utf8-plain-text", Payload: store.Payload{Inline: []byte("hello")}},
		},
	}

	out, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var reloaded store.Item
	if err := json.Unmarshal(out, &reloaded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if reloaded.ID != item.ID {
		t.Errorf("ID = %q, expected %q", reloaded.ID, item.ID)
	}

	if !reloaded.CreatedAt.Equal(item.CreatedAt) {
		t.Errorf("CreatedAt = %v, expected %v", reloaded.CreatedAt, item.CreatedAt)
	}

	if !reloaded.IsPinned {
		t.Error("IsPinned = false, expected true")
	}

	if bytes.Contains(out, []byte("sourceBundleIdentifier")) {
		t.Errorf("absent source bundle should stay absent: %s", out)
	}

	s := store.Open(t.TempDir())
	if text, ok := s.PlainText(reloaded); !ok || text != "hello" {
		t.Errorf("PlainText = %q, %v; expected %q, true", text, ok, "hello")
	}
}

func TestDisplayLabelImage(t *testing.T) {
	dir := fixtureDir(t)

	blobDir := filepath.Join(dir, "blobs", "CCCCCCCC-3333-3333-3333-333333333333")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	if err := os.WriteFile(filepath.Join(blobDir, "rep-0.bin"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	s := store.Open(dir)

	items, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if label := s.DisplayLabel(items[2]); label != "Image 3×2 · PNG" {
		t.Errorf("DisplayLabel(items[2]) = %q, expected %q", label, "Image 3×2 · PNG")
	}
}

// TestDisplayLabelStripsControlCharacters guards the one thing a clipboard
// label must never do: hand the terminal an instruction. Copied text comes from
// arbitrary places, and an escape sequence costs no display width — a row
// carrying one would look completely ordinary while acting on the terminal that
// printed it.
func TestDisplayLabelStripsControlCharacters(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "clipboard write",
			text:     "totally normal\x1b]52;c;cGF5bG9hZA==\x07 text",
			expected: "totally normal]52;c;cGF5bG9hZA== text",
		},
		{
			name:     "screen clear",
			text:     "before\x1b[2Jafter",
			expected: "before[2Jafter",
		},
		{
			name:     "bell and backspace",
			text:     "ding\adel\bete",
			expected: "dingdelete",
		},
		{
			name:     "tabs still collapse to spaces",
			text:     "one\ttwo",
			expected: "one two",
		},
		{
			// U+202E reverses everything after it, so a row reading
			// "invoice‮gpj.exe" is drawn as "invoiceexe.jpg" — a label that
			// lies about what copying the item hands over.
			name:     "bidi override",
			text:     "invoice‮gpj.exe",
			expected: "invoicegpj.exe",
		},
		{
			name:     "soft hyphen and zero width space",
			text:     "pass­word​here",
			expected: "passwordhere",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := store.Open(t.TempDir())

			it := store.Item{Reps: []store.Representation{{
				Type:    "public.utf8-plain-text",
				Payload: store.Payload{Inline: []byte(tc.text)},
			}}}

			label := s.DisplayLabel(it)
			if label != tc.expected {
				t.Errorf("DisplayLabel = %q, expected %q", label, tc.expected)
			}

			if strings.ContainsFunc(label, unicode.IsControl) {
				t.Errorf("DisplayLabel = %q, which still carries a control character", label)
			}

			if strings.ContainsFunc(label, func(r rune) bool { return unicode.Is(unicode.Cf, r) }) {
				t.Errorf("DisplayLabel = %q, which still carries a format character", label)
			}
		})
	}
}

// TestDisplayLabelStripsControlCharactersFromFilenames covers the other way
// someone else's bytes become a label. A file URL is normally percent-encoded,
// but nothing stops a pasteboard writer from putting raw ones on it.
func TestDisplayLabelStripsControlCharactersFromFilenames(t *testing.T) {
	s := store.Open(t.TempDir())

	it := store.Item{Reps: []store.Representation{{
		Type:    "public.file-url",
		Payload: store.Payload{Inline: []byte("file:///tmp/re\x1b[2Jport.pdf")},
	}}}

	label := s.DisplayLabel(it)
	if label != "re[2Jport.pdf" {
		t.Errorf("DisplayLabel = %q, expected %q", label, "re[2Jport.pdf")
	}
}

func TestDefaultDirEnvOverride(t *testing.T) {
	t.Setenv("EXTENDO_STORE_DIR", "/tmp/custom-extendo")

	if dir := store.DefaultDir(); dir != "/tmp/custom-extendo" {
		t.Errorf("DefaultDir() = %q, expected /tmp/custom-extendo", dir)
	}
}

// appSupport and container are the two places a history file can be, the second
// being where macOS redirects a sandboxed extendo's writes.
var (
	appSupport = filepath.Join("Library", "Application Support", "extendo")
	container  = filepath.Join("Library", "Containers", "com.tjq.extendo", "Data", appSupport)
)

// fakeHome points os.UserHomeDir at a temporary directory holding a history
// file in each of the named locations.
func fakeHome(t *testing.T, withHistory ...string) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("EXTENDO_STORE_DIR", "")
	t.Setenv("HOME", home)

	for _, rel := range withHistory {
		dir := filepath.Join(home, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}

		if err := os.WriteFile(filepath.Join(dir, "history.json"), []byte("[]"), 0o644); err != nil {
			t.Fatalf("write history.json in %s: %v", rel, err)
		}
	}

	return home
}

// TestDefaultDirFindsSandboxContainer is the case a signed extendo produces:
// the app is sandboxed, so its Application Support writes land in its container
// and the unsandboxed path ext used to look in is never created at all.
func TestDefaultDirFindsSandboxContainer(t *testing.T) {
	home := fakeHome(t, container)

	if dir, expected := store.DefaultDir(), filepath.Join(home, container); dir != expected {
		t.Errorf("DefaultDir() = %q, expected the container at %q", dir, expected)
	}
}

func TestDefaultDirPrefersAppSupport(t *testing.T) {
	home := fakeHome(t, appSupport, container)

	if dir, expected := store.DefaultDir(), filepath.Join(home, appSupport); dir != expected {
		t.Errorf("DefaultDir() = %q, expected the unsandboxed path at %q", dir, expected)
	}
}

// TestDefaultDirPrefersHistoryOverEmptyDir guards the ordering: an app that has
// been launched but never used leaves an empty directory behind, and an empty
// directory must not outrank the one that actually holds the items.
func TestDefaultDirPrefersHistoryOverEmptyDir(t *testing.T) {
	home := fakeHome(t, container)

	if err := os.MkdirAll(filepath.Join(home, appSupport), 0o755); err != nil {
		t.Fatalf("mkdir app support: %v", err)
	}

	if dir, expected := store.DefaultDir(), filepath.Join(home, container); dir != expected {
		t.Errorf("DefaultDir() = %q, expected the container at %q", dir, expected)
	}
}

// TestDefaultDirWithoutEitherPath keeps the message a user sees pointed at the
// path they can create, rather than at a container only macOS makes.
func TestDefaultDirWithoutEitherPath(t *testing.T) {
	home := fakeHome(t)

	if dir, expected := store.DefaultDir(), filepath.Join(home, appSupport); dir != expected {
		t.Errorf("DefaultDir() = %q, expected %q", dir, expected)
	}
}

package store_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tjq/extendo-cli/internal/store"
)

const (
	idA = "AAAAAAAA-1111-1111-1111-111111111111"
	idB = "BBBBBBBB-2222-2222-2222-222222222222"
	idC = "CCCCCCCC-3333-3333-3333-333333333333"
)

// itemByID finds an item by exact ID, failing the test when it is absent.
func itemByID(t *testing.T, items []store.Item, id string) store.Item {
	t.Helper()

	for _, it := range items {
		if it.ID == id {
			return it
		}
	}

	t.Fatalf("item %s not found in %d items", id, len(items))

	return store.Item{}
}

func TestTogglePin(t *testing.T) {
	dir := fixtureDir(t)
	s := store.Open(dir)

	nowPinned, err := s.TogglePin(idA)
	if err != nil {
		t.Fatalf("TogglePin: %v", err)
	}

	if !nowPinned {
		t.Error("TogglePin returned nowPinned = false, expected true")
	}

	reloaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load after TogglePin: %v", err)
	}

	if !itemByID(t, reloaded, idA).IsPinned {
		t.Error("reloaded item IsPinned = false, expected the pin to reach disk")
	}

	if !itemByID(t, reloaded, idB).IsPinned {
		t.Error("toggling one item changed another item's pin")
	}

	data, err := os.ReadFile(filepath.Join(dir, "history.json"))
	if err != nil {
		t.Fatalf("read history.json: %v", err)
	}

	if !bytes.Contains(data, []byte(`"futureField"`)) {
		t.Errorf("TogglePin dropped futureField from the file: %s", data)
	}

	// Toggling again must return the item to its original state.
	nowPinned, err = s.TogglePin(idA)
	if err != nil {
		t.Fatalf("second TogglePin: %v", err)
	}

	if nowPinned {
		t.Error("second TogglePin returned nowPinned = true, expected false")
	}
}

// TogglePin must re-read the file, so writes the macOS app made after the CLI
// last loaded survive the mutation instead of being clobbered.
func TestTogglePinRereadsFromDisk(t *testing.T) {
	dir := fixtureDir(t)
	s := store.Open(dir)

	if _, err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The app appends an item behind the CLI's back.
	appended := `[{"id":"EEEEEEEE-5555-5555-5555-555555555555","createdAt":774400000,` +
		`"isPinned":false,"representations":[]},` +
		`{"id":"` + idA + `","createdAt":774300000,"isPinned":false,` +
		`"futureField":{"x":1},"representations":[]}]`

	if err := os.WriteFile(filepath.Join(dir, "history.json"), []byte(appended), 0o644); err != nil {
		t.Fatalf("write history.json: %v", err)
	}

	if _, err := s.TogglePin(idA); err != nil {
		t.Fatalf("TogglePin: %v", err)
	}

	reloaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load after TogglePin: %v", err)
	}

	if len(reloaded) != 2 {
		t.Fatalf("len(reloaded) = %d, expected 2 — TogglePin wrote a stale slice", len(reloaded))
	}

	itemByID(t, reloaded, "EEEEEEEE-5555-5555-5555-555555555555")

	if !itemByID(t, reloaded, idA).IsPinned {
		t.Error("reloaded item IsPinned = false, expected true")
	}
}

func TestTogglePinUnknownID(t *testing.T) {
	if _, err := store.Open(fixtureDir(t)).TogglePin("ZZZZ"); err == nil {
		t.Error("TogglePin on an unknown id returned nil error")
	}
}

func TestDeleteRemovesItemAndBlobs(t *testing.T) {
	dir := fixtureDir(t)

	blobDir := filepath.Join(dir, "blobs", idC)
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}

	if err := os.WriteFile(filepath.Join(blobDir, "rep-0.bin"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	s := store.Open(dir)
	if err := s.Delete(idC); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	reloaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load after Delete: %v", err)
	}

	if len(reloaded) != 3 {
		t.Fatalf("len(reloaded) = %d, expected 3", len(reloaded))
	}

	for _, it := range reloaded {
		if it.ID == idC {
			t.Fatalf("deleted item %s is still in the history", idC)
		}
	}

	if _, err := os.Stat(blobDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Stat(%s) error = %v, expected the blob directory to be gone", blobDir, err)
	}
}

// Deleting an item whose blobs were never written must still succeed.
func TestDeleteWithoutBlobs(t *testing.T) {
	s := store.Open(fixtureDir(t))
	if err := s.Delete(idA); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	reloaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load after Delete: %v", err)
	}

	if len(reloaded) != 3 {
		t.Errorf("len(reloaded) = %d, expected 3", len(reloaded))
	}
}

func TestDeleteUnknownID(t *testing.T) {
	if err := store.Open(fixtureDir(t)).Delete("ZZZZ"); err == nil {
		t.Error("Delete on an unknown id returned nil error")
	}
}

// writeHistory puts a hand-written history.json in a fresh directory, for the
// shapes the sample fixture deliberately does not have.
func writeHistory(t *testing.T, items string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "history.json"), []byte(items), 0o644); err != nil {
		t.Fatalf("write history.json: %v", err)
	}

	return dir
}

// TestRepDataRejectsATraversingBlobPath: a payload path is joined onto
// <dir>/blobs and read. history.json is a file ext does not write alone — the
// app owns it, and anything that can edit it can point a representation at
// ~/.ssh/id_rsa and have the picker render it.
func TestRepDataRejectsATraversingBlobPath(t *testing.T) {
	dir := writeHistory(t, `[{"id":"`+idA+`","createdAt":774300000,"isPinned":false,`+
		`"representations":[{"type":"public.utf8-plain-text",`+
		`"payload":{"kind":"external","path":"../secret.txt"}}]}]`)

	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("token"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	s := store.Open(dir)

	items, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	data, err := s.RepData(items[0], items[0].Reps[0])
	if err == nil {
		t.Fatalf("RepData read outside the store: %q", data)
	}

	if !errors.Is(err, store.ErrUnsafePath) {
		t.Errorf("RepData error = %v, expected ErrUnsafePath", err)
	}

	// Every display path goes through RepData, so the refusal has to degrade
	// into a label rather than into the file's contents.
	if label := s.DisplayLabel(items[0]); strings.Contains(label, "token") {
		t.Errorf("DisplayLabel = %q, which is the file it was pointed at", label)
	}
}

// TestRepDataReadsANestedBlobPath keeps the guard from rejecting the layout the
// app actually writes: a path below <dir>/blobs, with a directory in it.
func TestRepDataReadsANestedBlobPath(t *testing.T) {
	dir := writeHistory(t, `[{"id":"`+idA+`","createdAt":774300000,"isPinned":false,`+
		`"representations":[{"type":"public.utf8-plain-text",`+
		`"payload":{"kind":"external","path":"`+idA+`/rep-0.bin"}}]}]`)

	blobDir := filepath.Join(dir, "blobs", idA)
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}

	if err := os.WriteFile(filepath.Join(blobDir, "rep-0.bin"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	s := store.Open(dir)

	items, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	data, err := s.RepData(items[0], items[0].Reps[0])
	if err != nil {
		t.Fatalf("RepData: %v", err)
	}

	if string(data) != "hello" {
		t.Errorf("RepData = %q, expected %q", data, "hello")
	}
}

// TestDeleteSkipsBlobsForATraversingID is the same hole with teeth: the ID goes
// into the path Delete hands to os.RemoveAll, so an item calling itself `..`
// would take the store — or the directory above it — with it.
func TestDeleteSkipsBlobsForATraversingID(t *testing.T) {
	dir := writeHistory(t, `[{"id":"../evil","createdAt":774300000,"isPinned":false,`+
		`"representations":[]},{"id":"`+idA+`","createdAt":774200000,`+
		`"isPinned":false,"representations":[]}]`)

	// blobs/../evil is <dir>/evil: outside the directory Delete owns.
	bystander := filepath.Join(dir, "evil")
	if err := os.MkdirAll(bystander, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(bystander, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("write bystander: %v", err)
	}

	s := store.Open(dir)
	if err := s.Delete("../evil"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The history entry still goes: dropping it is what the user asked for, and
	// it is the blob removal that has to be held back.
	reloaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load after Delete: %v", err)
	}

	if len(reloaded) != 1 || reloaded[0].ID != idA {
		t.Errorf("history after Delete = %+v, expected only %s", reloaded, idA)
	}

	if _, err := os.Stat(filepath.Join(bystander, "keep.txt")); err != nil {
		t.Errorf("Delete removed a directory outside blobs: %v", err)
	}
}

func TestSaveAtomic(t *testing.T) {
	dir := fixtureDir(t)
	s := store.Open(dir)

	items, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := s.Save(items); err != nil {
		t.Fatalf("Save: %v", err)
	}

	leftovers, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	if len(leftovers) != 0 {
		t.Errorf("Save left temp files behind: %v", leftovers)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("dir holds %d entries, expected only history.json", len(entries))
	}

	reloaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}

	if len(reloaded) != len(items) {
		t.Errorf("len(reloaded) = %d, expected %d", len(reloaded), len(items))
	}

	data, err := os.ReadFile(filepath.Join(dir, "history.json"))
	if err != nil {
		t.Fatalf("read history.json: %v", err)
	}

	if !bytes.Contains(data, []byte(`"futureField"`)) {
		t.Errorf("Save dropped futureField: %s", data)
	}
}

func TestResolve(t *testing.T) {
	s, items := loadFixture(t)
	sorted := store.Sorted(items)

	// The pinned item sorts first, so index 1 is BBBB rather than file order.
	resolved, err := s.Resolve(sorted, "1")
	if err != nil {
		t.Fatalf(`Resolve("1"): %v`, err)
	}

	if resolved.ID != idB {
		t.Errorf(`Resolve("1").ID = %q, expected %q`, resolved.ID, idB)
	}

	resolved, err = s.Resolve(sorted, "aaaa")
	if err != nil {
		t.Fatalf(`Resolve("aaaa"): %v`, err)
	}

	if resolved.ID != idA {
		t.Errorf(`Resolve("aaaa").ID = %q, expected %q`, resolved.ID, idA)
	}

	for _, arg := range []string{"zz", "0", "5", "-1", ""} {
		if _, err := s.Resolve(sorted, arg); err == nil {
			t.Errorf("Resolve(%q) returned nil error, expected a failure", arg)
		}
	}
}

func TestResolveAmbiguousPrefix(t *testing.T) {
	items := []store.Item{
		{ID: "FEED0001-0000-0000-0000-000000000000"},
		{ID: "FEED0002-0000-0000-0000-000000000000"},
	}

	s := store.Open(t.TempDir())

	if _, err := s.Resolve(items, "feed"); err == nil {
		t.Error(`Resolve("feed") returned nil error, expected an ambiguity failure`)
	}

	resolved, err := s.Resolve(items, "feed0002")
	if err != nil {
		t.Fatalf(`Resolve("feed0002"): %v`, err)
	}

	if resolved.ID != "FEED0002-0000-0000-0000-000000000000" {
		t.Errorf("Resolve(\"feed0002\").ID = %q, expected FEED0002-…", resolved.ID)
	}
}

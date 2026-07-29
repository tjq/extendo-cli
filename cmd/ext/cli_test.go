package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/tjq/extendo-cli/internal/clip"
	"github.com/tjq/extendo-cli/internal/store"
)

// isUpdate rewrites the golden files instead of comparing against them:
//
//	go test ./cmd/ext -run TestList -update
var isUpdate = flag.Bool("update", false, "rewrite golden files")

// refDate mirrors store's Foundation reference date, which the fixture's
// createdAt numbers count seconds from.
var refDate = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// fixedNow sits one hour after the newest fixture item, so every AGE cell in
// the goldens has exactly one right answer.
var fixedNow = refDate.Add(774303600 * time.Second)

const (
	idAAAA = "AAAAAAAA-1111-1111-1111-111111111111"
	idBBBB = "BBBBBBBB-2222-2222-2222-222222222222"
	idCCCC = "CCCCCCCC-3333-3333-3333-333333333333"
	idDDDD = "DDDDDDDD-4444-4444-4444-444444444444"
)

// call records one command a fakeRunner was asked to run.
type call struct {
	stdin []byte
	name  string
	args  []string
}

// fakeRunner stands in for the pasteboard tools so tests can inspect the exact
// command shapes without touching the real clipboard.
type fakeRunner struct {
	calls []call
}

func (r *fakeRunner) Run(stdin []byte, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, call{
		stdin: slices.Clone(stdin),
		name:  name,
		args:  slices.Clone(args),
	})

	return nil, nil
}

// fixtureDir copies the sample history into a fresh temp directory and seeds
// the external blob item CCCC points at, so DisplayLabel can decode it.
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

	blobDir := filepath.Join(dir, "blobs", idCCCC)
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	buf := &bytes.Buffer{}
	if err := png.Encode(buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	if err := os.WriteFile(filepath.Join(blobDir, "rep-0.bin"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	return dir
}

// freezeClock pins the AGE column and restores the real clock afterwards.
func freezeClock(t *testing.T) {
	t.Helper()

	previous := now
	now = func() time.Time { return fixedNow }

	t.Cleanup(func() { now = previous })
}

// forceTTY pins the terminal check the bare `ext` command branches on.
func forceTTY(t *testing.T, isTerminal bool) {
	t.Helper()

	previous := isStdoutTTY
	isStdoutTTY = func() bool { return isTerminal }

	t.Cleanup(func() { isStdoutTTY = previous })
}

// result holds everything one CLI invocation produced.
type result struct {
	stdout string
	stderr string
	err    error
}

// run executes the CLI against dir with the given arguments.
func run(t *testing.T, dir string, r clip.Runner, args ...string) result {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	cmd := newRootCmd(store.Open(dir), r)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	// A variadic call with no arguments yields a nil slice, and cobra reads
	// os.Args when its argument slice is nil — which under `go test` is the
	// test binary's own flags.
	cmd.SetArgs(append([]string{}, args...))

	err := cmd.Execute()

	return result{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// checkGolden compares got against testdata/name, or rewrites it under -update.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if *isUpdate {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("update %s: %v", path, err)
		}

		return
	}

	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if got != string(expected) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- expected ---\n%s", path, got, expected)
	}
}

// loadItems re-reads the store from disk, so mutation tests assert on the file
// rather than on in-memory state.
func loadItems(t *testing.T, dir string) []store.Item {
	t.Helper()

	items, err := store.Open(dir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return items
}

func findItem(t *testing.T, items []store.Item, id string) store.Item {
	t.Helper()

	index := slices.IndexFunc(items, func(it store.Item) bool { return it.ID == id })
	if index < 0 {
		t.Fatalf("item %s not found in %d items", id, len(items))
	}

	return items[index]
}

func TestListGolden(t *testing.T) {
	freezeClock(t)

	got := run(t, fixtureDir(t), &fakeRunner{}, "list")
	if got.err != nil {
		t.Fatalf("list: %v (stderr %q)", got.err, got.stderr)
	}

	if got.stderr != "" {
		t.Errorf("list wrote to stderr: %q", got.stderr)
	}

	checkGolden(t, "list.golden", got.stdout)
}

func TestListJSONGolden(t *testing.T) {
	freezeClock(t)

	got := run(t, fixtureDir(t), &fakeRunner{}, "list", "--json")
	if got.err != nil {
		t.Fatalf("list --json: %v (stderr %q)", got.err, got.stderr)
	}

	entries := []map[string]any{}
	if err := json.Unmarshal([]byte(got.stdout), &entries); err != nil {
		t.Fatalf("output is not valid json: %v\n%s", err, got.stdout)
	}

	checkGolden(t, "list_json.golden", got.stdout)
}

// The secret fixture: one text item holding an Anthropic key, plus the pieces
// tests assert on. maskedPrefix is the part of the real value that Mask reveals,
// and the exact string that must never reach a status line.
const (
	secretValue       = "sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWX"
	secretMaskPrefix  = "sk-ant-"
	secretMaskedLabel = "Anthropic key · sk-ant-••••••••••"
	secretCategory    = "Anthropic key"
)

// secretDir builds a one-item store whose only entry classifies as a secret.
func secretDir(t *testing.T) string {
	t.Helper()

	return textDir(t, secretValue)
}

// textDir builds a one-item store holding text verbatim, bytes and all.
func textDir(t *testing.T, text string) string {
	t.Helper()

	history := fmt.Sprintf(
		`[{"id":%q,"createdAt":774300000,"isPinned":false,`+
			`"representations":[{"type":"public.utf8-plain-text","payload":{"kind":"inline","data":%q}}]}]`,
		idAAAA, base64.StdEncoding.EncodeToString([]byte(text)),
	)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "history.json"), []byte(history), 0o644); err != nil {
		t.Fatalf("write history.json: %v", err)
	}

	return dir
}

// hostileSecret is a credential preceded by a screen clear and a tab. It is
// described by Mask rather than by the label path, which copies the first runes
// of the first line through verbatim.
const hostileSecret = "\x1b[2Jx\txx\nsk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"

// TestListStripsControlCharacters covers the table's half of the rule the
// picker's frames follow: nothing printed to a terminal may carry a character
// the terminal acts on. A tab matters twice here — the table is tab-delimited,
// so one inside a cell would split the row into a column of its own.
func TestListStripsControlCharacters(t *testing.T) {
	freezeClock(t)

	got := run(t, textDir(t, hostileSecret), &fakeRunner{}, "list")
	if got.err != nil {
		t.Fatalf("list: %v", got.err)
	}

	for _, line := range strings.Split(got.stdout, "\n") {
		if index := strings.IndexFunc(line, unicode.IsControl); index >= 0 {
			t.Errorf("table line carries %q: %q", line[index], line)
		}
	}

	if !strings.Contains(got.stdout, secretCategory+" · ") {
		t.Errorf("table does not name the category:\n%s", got.stdout)
	}
}

// TestListTruncatesLongLabels covers the column cap. tabwriter widens a column
// to its widest cell, so one long paste used to pad every row out to match and
// push AGE and PIN past the edge of the screen.
func TestListTruncatesLongLabels(t *testing.T) {
	freezeClock(t)

	got := run(t, textDir(t, strings.Repeat("x", 400)), &fakeRunner{}, "list")
	if got.err != nil {
		t.Fatalf("list: %v", got.err)
	}

	for _, line := range strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Errorf("table line is %d cells wide, expected at most 80: %q", width, line)
		}
	}

	if !strings.Contains(got.stdout, "…") {
		t.Errorf("long label was not marked as cut:\n%s", got.stdout)
	}
}

// TestListJSONKeepsLongLabels is the other half: the cap is a display rule for
// the table alone, and --json is what scripts read.
func TestListJSONKeepsLongLabels(t *testing.T) {
	freezeClock(t)

	label := strings.Repeat("x", 400)

	got := run(t, textDir(t, label), &fakeRunner{}, "list", "--json")
	if got.err != nil {
		t.Fatalf("list --json: %v", got.err)
	}

	var entries []listEntry
	if err := json.Unmarshal([]byte(got.stdout), &entries); err != nil {
		t.Fatalf("decoding json: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, expected 1", len(entries))
	}

	if entries[0].Label != label {
		t.Errorf("json label is %d runes, expected the whole %d", len(entries[0].Label), len(label))
	}
}

func TestListMasksSecrets(t *testing.T) {
	freezeClock(t)

	secret := secretValue
	dir := secretDir(t)
	expectedLabel := secretMaskedLabel

	got := run(t, dir, &fakeRunner{}, "list")
	if got.err != nil {
		t.Fatalf("list: %v", got.err)
	}

	if !strings.Contains(got.stdout, expectedLabel) {
		t.Errorf("list output missing masked label %q:\n%s", expectedLabel, got.stdout)
	}

	if strings.Contains(got.stdout, secret) {
		t.Errorf("list output leaked the secret:\n%s", got.stdout)
	}

	jsonOut := run(t, dir, &fakeRunner{}, "list", "--json")
	if jsonOut.err != nil {
		t.Fatalf("list --json: %v", jsonOut.err)
	}

	entries := []struct {
		Label  string `json:"label"`
		Secret string `json:"secret"`
	}{}
	if err := json.Unmarshal([]byte(jsonOut.stdout), &entries); err != nil {
		t.Fatalf("decode json: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, expected 1", len(entries))
	}

	if entries[0].Label != expectedLabel {
		t.Errorf("json label = %q, expected %q", entries[0].Label, expectedLabel)
	}

	if entries[0].Secret != "anthropicKey" {
		t.Errorf("json secret = %q, expected anthropicKey", entries[0].Secret)
	}
}

func TestGetPrint(t *testing.T) {
	runner := &fakeRunner{}

	got := run(t, fixtureDir(t), runner, "get", "1", "--print")
	if got.err != nil {
		t.Fatalf("get 1 --print: %v", got.err)
	}

	if got.stdout != "tyler@example.com" {
		t.Errorf("stdout = %q, expected %q", got.stdout, "tyler@example.com")
	}

	if len(runner.calls) != 0 {
		t.Errorf("--print ran %d commands, expected none", len(runner.calls))
	}
}

func TestGetPrintRejectsImage(t *testing.T) {
	got := run(t, fixtureDir(t), &fakeRunner{}, "get", "3", "--print")
	if got.err == nil {
		t.Fatalf("get 3 --print returned nil error, expected a refusal (stdout %q)", got.stdout)
	}

	if got.stdout != "" {
		t.Errorf("stdout = %q, expected empty", got.stdout)
	}
}

func TestGetCopiesImage(t *testing.T) {
	runner := &fakeRunner{}

	got := run(t, fixtureDir(t), runner, "get", "3")
	if got.err != nil {
		t.Fatalf("get 3: %v", got.err)
	}

	if got.stdout != "" {
		t.Errorf("stdout = %q, expected empty — status lines belong on stderr", got.stdout)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("len(calls) = %d, expected 1: %+v", len(runner.calls), runner.calls)
	}

	only := runner.calls[0]
	if only.name != "osascript" {
		t.Errorf("ran %q, expected osascript", only.name)
	}

	script := strings.Join(only.args, " ")
	if !strings.Contains(script, "«class PNGf»") {
		t.Errorf("script does not set a PNG flavor: %q", script)
	}

	expectedStatus := "✓ copied #3 (Image 3×2 · PNG)\n"
	if got.stderr != expectedStatus {
		t.Errorf("stderr = %q, expected %q", got.stderr, expectedStatus)
	}
}

func TestGetCopiesTextByIDPrefix(t *testing.T) {
	runner := &fakeRunner{}

	got := run(t, fixtureDir(t), runner, "get", "bbbb")
	if got.err != nil {
		t.Fatalf("get bbbb: %v", got.err)
	}

	if len(runner.calls) != 1 || runner.calls[0].name != "pbcopy" {
		t.Fatalf("calls = %+v, expected a single pbcopy", runner.calls)
	}

	if string(runner.calls[0].stdin) != "tyler@example.com" {
		t.Errorf("pbcopy stdin = %q, expected %q", runner.calls[0].stdin, "tyler@example.com")
	}

	if got.stderr != "✓ copied #1 (tyler@example.com)\n" {
		t.Errorf("stderr = %q", got.stderr)
	}
}

// TestGetConfirmationHidesSecret guards the one place a value fragment would
// outlive the command: stderr, which lands in scrollback, `2>` redirects, and
// CI logs. The confirmation names the category and nothing derived from the
// value — not even the prefix the list is allowed to reveal.
func TestGetConfirmationHidesSecret(t *testing.T) {
	dir := secretDir(t)
	runner := &fakeRunner{}

	got := run(t, dir, runner, "get", "1")
	if got.err != nil {
		t.Fatalf("get 1: %v", got.err)
	}

	expectedStatus := "✓ copied #1 (" + secretCategory + ")\n"
	if got.stderr != expectedStatus {
		t.Errorf("stderr = %q, expected %q", got.stderr, expectedStatus)
	}

	for _, leak := range []string{secretValue, secretMaskPrefix, secretMaskedLabel, "sk-"} {
		if strings.Contains(got.stderr, leak) {
			t.Errorf("stderr leaked %q: %q", leak, got.stderr)
		}
	}

	// The value still has to reach the pasteboard intact.
	if len(runner.calls) != 1 || string(runner.calls[0].stdin) != secretValue {
		t.Errorf("calls = %+v, expected one pbcopy carrying the full secret", runner.calls)
	}
}

func TestPinTogglesOnDisk(t *testing.T) {
	dir := fixtureDir(t)

	got := run(t, dir, &fakeRunner{}, "pin", "2")
	if got.err != nil {
		t.Fatalf("pin 2: %v", got.err)
	}

	if got.stderr != "📌 pinned #2\n" {
		t.Errorf("stderr = %q, expected %q", got.stderr, "📌 pinned #2\n")
	}

	if got.stdout != "" {
		t.Errorf("stdout = %q, expected empty", got.stdout)
	}

	if !findItem(t, loadItems(t, dir), idAAAA).IsPinned {
		t.Error("AAAA is still unpinned on disk")
	}

	// Pinning moved AAAA to the top of the sorted list, so #2 is now BBBB.
	again := run(t, dir, &fakeRunner{}, "pin", "2")
	if again.err != nil {
		t.Fatalf("pin 2 (second): %v", again.err)
	}

	if again.stderr != "unpinned #2\n" {
		t.Errorf("stderr = %q, expected %q", again.stderr, "unpinned #2\n")
	}

	if findItem(t, loadItems(t, dir), idBBBB).IsPinned {
		t.Error("BBBB is still pinned on disk")
	}
}

func TestDeleteRemovesItem(t *testing.T) {
	dir := fixtureDir(t)

	got := run(t, dir, &fakeRunner{}, "delete", "4")
	if got.err != nil {
		t.Fatalf("delete 4: %v", got.err)
	}

	if got.stderr != "✗ deleted #4\n" {
		t.Errorf("stderr = %q, expected %q", got.stderr, "✗ deleted #4\n")
	}

	items := loadItems(t, dir)
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, expected 3", len(items))
	}

	if slices.ContainsFunc(items, func(it store.Item) bool { return it.ID == idDDDD }) {
		t.Error("DDDD survived the delete")
	}
}

func TestVersionDefaultsToDev(t *testing.T) {
	got := run(t, fixtureDir(t), &fakeRunner{}, "version")
	if got.err != nil {
		t.Fatalf("version: %v", got.err)
	}

	if got.stdout != "dev\n" {
		t.Errorf("stdout = %q, expected %q", got.stdout, "dev\n")
	}
}

func TestRootPipedRunsList(t *testing.T) {
	freezeClock(t)
	forceTTY(t, false)

	got := run(t, fixtureDir(t), &fakeRunner{})
	if got.err != nil {
		t.Fatalf("bare ext: %v", got.err)
	}

	checkGolden(t, "list.golden", got.stdout)
}

func TestMissingStoreIsAnError(t *testing.T) {
	got := run(t, t.TempDir(), &fakeRunner{}, "list")
	if got.err == nil {
		t.Fatal("list against an empty dir returned nil error, expected ErrNoStore")
	}
}

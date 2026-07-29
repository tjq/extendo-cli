package clip

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tjq/extendo-cli/internal/store"
)

// call is one recorded Runner invocation. Because the package deletes its temp
// files as soon as osascript returns, the fake also snapshots the file each
// script points at — a test inspecting the recording afterwards would find
// nothing on disk.
type call struct {
	stdin      []byte
	name       string
	args       []string
	paths      []string
	pathExists []bool
	pathData   [][]byte
}

type fakeRunner struct {
	calls []call
	err   error
}

func (f *fakeRunner) Run(stdin []byte, name string, args ...string) ([]byte, error) {
	recorded := call{stdin: stdin, name: name, args: args}

	for _, arg := range args {
		for _, path := range posixPaths(arg) {
			data, err := os.ReadFile(path)

			recorded.paths = append(recorded.paths, path)
			recorded.pathExists = append(recorded.pathExists, err == nil)
			recorded.pathData = append(recorded.pathData, data)
		}
	}

	f.calls = append(f.calls, recorded)

	return nil, f.err
}

// only returns the single call the fake recorded, failing the test otherwise.
func (f *fakeRunner) only(t *testing.T) call {
	t.Helper()

	if len(f.calls) != 1 {
		t.Fatalf("expected exactly 1 command, got %d: %+v", len(f.calls), f.calls)
	}

	return f.calls[0]
}

// script returns the -e argument of an osascript call.
func (c call) script(t *testing.T) string {
	t.Helper()

	if c.name != "osascript" {
		t.Fatalf("expected osascript, got %q", c.name)
	}

	if len(c.args) != 2 || c.args[0] != "-e" {
		t.Fatalf("expected [-e <script>], got %q", c.args)
	}

	return c.args[1]
}

// posixPaths pulls every AppleScript `POSIX file "…"` literal out of a script,
// undoing the backslash escaping the package applies. Reusing it to locate temp
// files means the escaping is exercised by every test, not just the dedicated
// one.
func posixPaths(script string) []string {
	paths := []string{}
	rest := script

	for {
		_, after, found := strings.Cut(rest, `POSIX file "`)
		if !found {
			return paths
		}

		var literal strings.Builder

		index := 0

		for index < len(after) && after[index] != '"' {
			if after[index] == '\\' && index+1 < len(after) {
				literal.WriteByte(after[index+1])
				index += 2

				continue
			}

			literal.WriteByte(after[index])
			index++
		}

		paths = append(paths, literal.String())
		rest = after[index:]
	}
}

func inlineItem(reps ...store.Representation) store.Item {
	return store.Item{ID: "ITEM-1", Reps: reps}
}

func inlineRep(repType, payload string) store.Representation {
	return store.Representation{
		Type:    repType,
		Payload: store.Payload{Inline: []byte(payload)},
	}
}

func TestCopyTextUsesPbcopy(t *testing.T) {
	runner := &fakeRunner{}
	it := inlineItem(inlineRep("public.utf8-plain-text", "git rebase -i HEAD~3"))

	if err := Copy(store.Open(t.TempDir()), it, runner); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	got := runner.only(t)

	if got.name != "pbcopy" {
		t.Errorf("name = %q, expected pbcopy", got.name)
	}

	if len(got.args) != 0 {
		t.Errorf("args = %q, expected none", got.args)
	}

	if string(got.stdin) != "git rebase -i HEAD~3" {
		t.Errorf("stdin = %q", got.stdin)
	}
}

func TestCopyTextFromBlob(t *testing.T) {
	dir := t.TempDir()
	blob := filepath.Join(dir, "blobs", "ITEM-1")

	if err := os.MkdirAll(blob, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(blob, "text.txt"), []byte("from a blob"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := store.Representation{
		Type:    "public.utf8-plain-text",
		Payload: store.Payload{Path: "ITEM-1/text.txt"},
	}

	runner := &fakeRunner{}

	if err := Copy(store.Open(dir), inlineItem(rep), runner); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if got := string(runner.only(t).stdin); got != "from a blob" {
		t.Errorf("stdin = %q, expected %q", got, "from a blob")
	}
}

func TestCopyImagePrefersPNGThenJPEGThenTIFF(t *testing.T) {
	png := inlineRep("public.png", "PNG-BYTES")
	jpeg := inlineRep("public.jpeg", "JPEG-BYTES")
	tiff := inlineRep("public.tiff", "TIFF-BYTES")

	tests := []struct {
		name              string
		reps              []store.Representation
		expectedClass     string
		expectedExtension string
		expectedData      string
	}{
		{
			name:              "all three reps prefer png",
			reps:              []store.Representation{tiff, jpeg, png},
			expectedClass:     "«class PNGf»",
			expectedExtension: ".png",
			expectedData:      "PNG-BYTES",
		},
		{
			name:              "no png prefers jpeg",
			reps:              []store.Representation{tiff, jpeg},
			expectedClass:     "«class JPEG»",
			expectedExtension: ".jpeg",
			expectedData:      "JPEG-BYTES",
		},
		{
			name:              "tiff only",
			reps:              []store.Representation{tiff},
			expectedClass:     "«class TIFF»",
			expectedExtension: ".tiff",
			expectedData:      "TIFF-BYTES",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{}

			if err := Copy(store.Open(t.TempDir()), inlineItem(test.reps...), runner); err != nil {
				t.Fatalf("Copy: %v", err)
			}

			got := runner.only(t)
			script := got.script(t)

			if !strings.Contains(script, test.expectedClass) {
				t.Errorf("script %q does not contain %q", script, test.expectedClass)
			}

			assertTempFile(t, got, test.expectedExtension, test.expectedData)
		})
	}
}

func TestCopyImageScriptShape(t *testing.T) {
	runner := &fakeRunner{}
	it := inlineItem(inlineRep("public.png", "PNG-BYTES"))

	if err := Copy(store.Open(t.TempDir()), it, runner); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	got := runner.only(t)
	script := got.script(t)

	expected := `set the clipboard to (read (POSIX file "` + got.paths[0] + `") as «class PNGf»)`
	if script != expected {
		t.Errorf("script = %q, expected %q", script, expected)
	}
}

func TestCopyRichTextUsesRTFClass(t *testing.T) {
	runner := &fakeRunner{}
	it := inlineItem(
		inlineRep("public.rtf", `{\rtf1\ansi hello}`),
		inlineRep("public.utf8-plain-text", "hello"),
	)

	if err := Copy(store.Open(t.TempDir()), it, runner); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	got := runner.only(t)
	script := got.script(t)

	// The four-char code is padded to four characters — the trailing space is
	// load-bearing, AppleScript rejects «class RTF».
	if !strings.Contains(script, "«class RTF »") {
		t.Errorf("script %q does not contain the padded RTF class", script)
	}

	assertTempFile(t, got, ".rtf", `{\rtf1\ansi hello}`)
}

func TestCopyRichTextWithoutRTFFallsBackToText(t *testing.T) {
	runner := &fakeRunner{}
	it := inlineItem(
		inlineRep("public.html", "<b>hello</b>"),
		inlineRep("public.utf8-plain-text", "hello"),
	)

	if err := Copy(store.Open(t.TempDir()), it, runner); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	got := runner.only(t)

	if got.name != "pbcopy" {
		t.Fatalf("name = %q, expected pbcopy", got.name)
	}

	if string(got.stdin) != "hello" {
		t.Errorf("stdin = %q, expected %q", got.stdin, "hello")
	}
}

func TestCopyFilesBuildsBracesList(t *testing.T) {
	tests := []struct {
		name     string
		urls     []string
		expected string
	}{
		{
			name:     "single file still uses braces",
			urls:     []string{"file:///Users/tjq/report-final.pdf"},
			expected: `set the clipboard to {POSIX file "/Users/tjq/report-final.pdf"}`,
		},
		{
			name: "two files",
			urls: []string{"file:///Users/tjq/report-final.pdf", "file:///Users/tjq/notes.txt"},
			expected: `set the clipboard to {POSIX file "/Users/tjq/report-final.pdf", ` +
				`POSIX file "/Users/tjq/notes.txt"}`,
		},
		{
			name:     "percent encoding is decoded",
			urls:     []string{"file:///Users/tjq/my%20report.pdf"},
			expected: `set the clipboard to {POSIX file "/Users/tjq/my report.pdf"}`,
		},
		{
			name:     "bare posix path",
			urls:     []string{"/Users/tjq/report-final.pdf"},
			expected: `set the clipboard to {POSIX file "/Users/tjq/report-final.pdf"}`,
		},
		{
			name:     "trailing nul from the pasteboard is trimmed",
			urls:     []string{"file:///Users/tjq/report-final.pdf\x00"},
			expected: `set the clipboard to {POSIX file "/Users/tjq/report-final.pdf"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reps := make([]store.Representation, 0, len(test.urls))
			for _, raw := range test.urls {
				reps = append(reps, inlineRep("public.file-url", raw))
			}

			runner := &fakeRunner{}

			if err := Copy(store.Open(t.TempDir()), inlineItem(reps...), runner); err != nil {
				t.Fatalf("Copy: %v", err)
			}

			if script := runner.only(t).script(t); script != test.expected {
				t.Errorf("script = %q, expected %q", script, test.expected)
			}
		})
	}
}

func TestCopyFilesEscapesQuotesAndBackslashes(t *testing.T) {
	const path = `/Users/tjq/say "hi"\weird.txt`

	runner := &fakeRunner{}
	it := inlineItem(inlineRep("public.file-url", path))

	if err := Copy(store.Open(t.TempDir()), it, runner); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	got := runner.only(t)

	expected := `set the clipboard to {POSIX file "/Users/tjq/say \"hi\"\\weird.txt"}`
	if script := got.script(t); script != expected {
		t.Errorf("script = %q, expected %q", script, expected)
	}

	if len(got.paths) != 1 || got.paths[0] != path {
		t.Errorf("unescaped path = %q, expected %q", got.paths, path)
	}
}

func TestCopyUnsupportedKind(t *testing.T) {
	runner := &fakeRunner{}
	it := inlineItem(inlineRep("com.acme.private", "opaque"))

	err := Copy(store.Open(t.TempDir()), it, runner)
	if !errors.Is(err, ErrUnsupportedKind) {
		t.Fatalf("err = %v, expected ErrUnsupportedKind", err)
	}

	if len(runner.calls) != 0 {
		t.Errorf("expected no commands, got %+v", runner.calls)
	}
}

func TestCopyRemovesTempFileWhenRunnerFails(t *testing.T) {
	boom := errors.New("osascript exploded")
	runner := &fakeRunner{err: boom}
	it := inlineItem(inlineRep("public.png", "PNG-BYTES"))

	err := Copy(store.Open(t.TempDir()), it, runner)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, expected it to wrap %v", err, boom)
	}

	got := runner.only(t)

	if !got.pathExists[0] {
		t.Errorf("temp file %s did not exist when osascript ran", got.paths[0])
	}

	if _, err := os.Stat(got.paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file %s survived a failed run", got.paths[0])
	}
}

func TestCopyMissingBlobReturnsError(t *testing.T) {
	rep := store.Representation{
		Type:    "public.png",
		Payload: store.Payload{Path: "ITEM-1/missing.png"},
	}

	runner := &fakeRunner{}

	if err := Copy(store.Open(t.TempDir()), inlineItem(rep), runner); err == nil {
		t.Fatal("expected an error for an unreadable blob")
	}

	if len(runner.calls) != 0 {
		t.Errorf("expected no commands, got %+v", runner.calls)
	}
}

func TestExecRunnerFeedsStdin(t *testing.T) {
	out, err := ExecRunner{}.Run([]byte("round trip"), "cat")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if string(out) != "round trip" {
		t.Errorf("output = %q, expected %q", out, "round trip")
	}
}

func TestExecRunnerReportsFailure(t *testing.T) {
	if _, err := (ExecRunner{}).Run(nil, "false"); err == nil {
		t.Fatal("expected an error from a non-zero exit")
	}
}

// assertTempFile checks that the file the script pointed at held the expected
// bytes while the command ran, and no longer exists now that Copy has returned.
func assertTempFile(t *testing.T, got call, extension, expectedData string) {
	t.Helper()

	if len(got.paths) != 1 {
		t.Fatalf("expected 1 POSIX file literal, got %q", got.paths)
	}

	path := got.paths[0]

	if !strings.HasSuffix(path, extension) {
		t.Errorf("temp path %q does not end in %q", path, extension)
	}

	if !got.pathExists[0] {
		t.Fatalf("temp file %s did not exist when the command ran", path)
	}

	if string(got.pathData[0]) != expectedData {
		t.Errorf("temp file held %q, expected %q", got.pathData[0], expectedData)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file %s was not removed (stat err %v)", path, err)
	}
}

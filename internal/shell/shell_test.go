package shell

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isUpdate rewrites the golden files instead of comparing against them:
//
//	go test ./internal/shell -update
var isUpdate = flag.Bool("update", false, "rewrite golden files")

// brewPath is where a Homebrew install puts the binary, and the path every
// golden block below is rendered for.
const brewPath = "/opt/homebrew/bin/ext"

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

func TestDetect(t *testing.T) {
	cases := []struct {
		name     string
		env      string
		expected Kind
	}{
		{name: "zsh", env: "/bin/zsh", expected: Zsh},
		{name: "homebrew zsh", env: "/opt/homebrew/bin/zsh", expected: Zsh},
		{name: "bash", env: "/bin/bash", expected: Bash},
		{name: "login shell argv", env: "-zsh", expected: Zsh},
		{name: "surrounding space", env: " /bin/bash\n", expected: Bash},
		{name: "fish", env: "/opt/homebrew/bin/fish", expected: Unknown},
		{name: "empty", env: "", expected: Unknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.env); got != tc.expected {
				t.Errorf("Detect(%q) = %v, expected %v", tc.env, got, tc.expected)
			}
		})
	}
}

func TestProfilePath(t *testing.T) {
	// The zsh block defines a ZLE widget, which only an interactive shell reads:
	// .zshrc, deliberately, and not the .zprofile the spec first named.
	if got, err := ProfilePath(Zsh, "/Users/x"); err != nil || got != "/Users/x/.zshrc" {
		t.Errorf("ProfilePath(Zsh) = %q, %v; expected /Users/x/.zshrc", got, err)
	}

	if got, err := ProfilePath(Bash, "/Users/x"); err != nil || got != "/Users/x/.bash_profile" {
		t.Errorf("ProfilePath(Bash) = %q, %v; expected /Users/x/.bash_profile", got, err)
	}

	if _, err := ProfilePath(Unknown, "/Users/x"); err == nil {
		t.Error("ProfilePath(Unknown) returned nil error")
	}

	// Without a home directory the join would yield ".zshrc", which writes into
	// whatever directory the command happened to be run from.
	if _, err := ProfilePath(Zsh, ""); err == nil {
		t.Error("ProfilePath with no home returned nil error")
	}
}

func TestRenderZshGolden(t *testing.T) {
	checkGolden(t, "zsh.golden", Render(Zsh, brewPath, DefaultKey))
}

func TestRenderBashGolden(t *testing.T) {
	checkGolden(t, "bash.golden", Render(Bash, brewPath, DefaultKey))
}

// TestRenderAliasesOnlyWhenTheNameDiffers keeps `ext` meaning the binary the
// block put on PATH. A build installed under another name needs the alias; the
// ordinary install would have it shadow itself.
func TestRenderAliasesOnlyWhenTheNameDiffers(t *testing.T) {
	if strings.Contains(Render(Zsh, brewPath, DefaultKey), "alias ext=") {
		t.Errorf("block aliases ext over itself:\n%s", Render(Zsh, brewPath, DefaultKey))
	}

	renamed := Render(Zsh, "/Users/x/go/bin/ext-dev", DefaultKey)
	if !strings.Contains(renamed, `alias ext="/Users/x/go/bin/ext-dev"`) {
		t.Errorf("block does not alias a renamed binary:\n%s", renamed)
	}
}

// TestBindingTargetsTheBinaryByPath is the fix for a ctrl-G that only worked
// for a binary named `ext`. The zsh widget used to call `command ext` and the
// bash binding `ext`: `command` skips aliases outright, and an alias is not
// expanded in argument position either, so the alias branch below left the
// hotkey bound to a command that does not exist. Both shells now name the
// binary by path, which also pins which ext runs when another is on PATH first.
func TestBindingTargetsTheBinaryByPath(t *testing.T) {
	const renamedPath = "/Users/x/go/bin/ext-dev"

	cases := []struct {
		name     string
		kind     Kind
		exePath  string
		expected string
	}{
		{
			name: "zsh", kind: Zsh, exePath: brewPath,
			expected: `_ext_picker() { "/opt/homebrew/bin/ext" </dev/tty >/dev/tty; zle reset-prompt }`,
		},
		{
			name: "zsh, renamed binary", kind: Zsh, exePath: renamedPath,
			expected: `_ext_picker() { "/Users/x/go/bin/ext-dev" </dev/tty >/dev/tty; zle reset-prompt }`,
		},
		{
			name: "bash", kind: Bash, exePath: brewPath,
			expected: `bind -x '"\C-g": "/opt/homebrew/bin/ext"' 2>/dev/null || true`,
		},
		{
			name: "bash, renamed binary", kind: Bash, exePath: renamedPath,
			expected: `bind -x '"\C-g": "/Users/x/go/bin/ext-dev"' 2>/dev/null || true`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block := Render(tc.kind, tc.exePath, DefaultKey)

			if !strings.Contains(block, tc.expected) {
				t.Errorf("block does not carry %q:\n%s", tc.expected, block)
			}

			// The two spellings that resolve by name rather than by path.
			for _, byName := range []string{"command ext", `"\C-g": ext'`} {
				if strings.Contains(block, byName) {
					t.Errorf("block still binds ctrl-G by name (%q):\n%s", byName, block)
				}
			}
		})
	}
}

// TestBindingQuotesAwkwardPaths covers the two things a path can carry that a
// shell would otherwise act on inside double quotes. A `go build -o` into a
// directory with a space in it is ordinary; the dollar sign is not, but the
// escape costs nothing.
func TestBindingQuotesAwkwardPaths(t *testing.T) {
	block := Render(Zsh, "/Users/x/Application Support/ext$dev", DefaultKey)

	expected := `_ext_picker() { "/Users/x/Application Support/ext\$dev" </dev/tty >/dev/tty; zle reset-prompt }`
	if !strings.Contains(block, expected) {
		t.Errorf("block does not carry %q:\n%s", expected, block)
	}

	if !strings.Contains(block, `alias ext="/Users/x/Application Support/ext\$dev"`) {
		t.Errorf("alias is unquoted:\n%s", block)
	}
}

func TestRenderUnknownIsEmpty(t *testing.T) {
	if got := Render(Unknown, brewPath, DefaultKey); got != "" {
		t.Errorf("Render(Unknown, DefaultKey) = %q, expected empty", got)
	}
}

func TestApplyAppendsBelowExistingContent(t *testing.T) {
	block := Render(Zsh, brewPath, DefaultKey)

	got := Apply("export EDITOR=vim\n", block)

	expected := "export EDITOR=vim\n\n" + block
	if got != expected {
		t.Errorf("Apply appended wrong\n--- got ---\n%q\n--- expected ---\n%q", got, expected)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	block := Render(Zsh, brewPath, DefaultKey)

	once := Apply("export EDITOR=vim\n", block)

	if twice := Apply(once, block); twice != once {
		t.Errorf("Apply is not idempotent\n--- twice ---\n%q\n--- once ---\n%q", twice, once)
	}
}

func TestApplyOnEmptyProfile(t *testing.T) {
	block := Render(Zsh, brewPath, DefaultKey)

	if got := Apply("", block); got != block {
		t.Errorf("Apply on an empty profile = %q, expected the bare block", got)
	}
}

// TestApplyReplacesStaleBlock is the upgrade path: the binary moved, so the
// block that names its old directory has to go, and everything the user wrote
// around it has to stay.
func TestApplyReplacesStaleBlock(t *testing.T) {
	stale := Apply("export EDITOR=vim\n", Render(Zsh, "/usr/local/bin/ext", DefaultKey))
	stale += "\nexport PAGER=less\n"

	fresh := Render(Zsh, brewPath, DefaultKey)

	got := Apply(stale, fresh)

	if strings.Contains(got, "/usr/local/bin") {
		t.Errorf("stale block survived:\n%s", got)
	}

	if strings.Count(got, startMarker) != 1 {
		t.Errorf("profile carries %d blocks, expected 1:\n%s", strings.Count(got, startMarker), got)
	}

	for _, kept := range []string{"export EDITOR=vim", "export PAGER=less", "/opt/homebrew/bin"} {
		if !strings.Contains(got, kept) {
			t.Errorf("replacement dropped %q:\n%s", kept, got)
		}
	}
}

func TestRemoveRoundTrip(t *testing.T) {
	block := Render(Zsh, brewPath, DefaultKey)

	cases := []struct {
		name     string
		existing string
	}{
		{name: "empty profile", existing: ""},
		{name: "trailing newline", existing: "export EDITOR=vim\n"},
		{name: "no trailing newline", existing: "export EDITOR=vim"},
		{name: "blank lines at the end", existing: "export EDITOR=vim\n\n\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installed := Apply(tc.existing, block)

			got, removed := Remove(installed)
			if !removed {
				t.Fatalf("Remove reported no block in:\n%s", installed)
			}

			expected := strings.TrimRight(tc.existing, "\n")
			if expected != "" {
				expected += "\n"
			}

			if got != expected {
				t.Errorf("Remove = %q, expected %q", got, expected)
			}
		})
	}
}

// TestRemoveKeepsSurroundingContent covers the block a user has written past,
// where dropping its lines outright would leave the two halves stuck together
// or a doubled blank line between them.
func TestRemoveKeepsSurroundingContent(t *testing.T) {
	installed := Apply("export EDITOR=vim\n", Render(Zsh, brewPath, DefaultKey)) + "\nexport PAGER=less\n"

	got, removed := Remove(installed)
	if !removed {
		t.Fatal("Remove reported no block")
	}

	expected := "export EDITOR=vim\n\nexport PAGER=less\n"
	if got != expected {
		t.Errorf("Remove = %q, expected %q", got, expected)
	}
}

func TestRemoveReportsAnAbsentBlock(t *testing.T) {
	existing := "export EDITOR=vim\n"

	got, removed := Remove(existing)
	if removed {
		t.Error("Remove reported a block in a profile that has none")
	}

	if got != existing {
		t.Errorf("Remove rewrote a profile it did not own: %q", got)
	}
}

func TestIsInstalled(t *testing.T) {
	if IsInstalled("export EDITOR=vim\n") {
		t.Error("IsInstalled reported a block in a profile that has none")
	}

	if !IsInstalled(Apply("", Render(Bash, brewPath, DefaultKey))) {
		t.Error("IsInstalled missed a block it just wrote")
	}

	// A half-written profile — the user deleted the closing marker by hand — is
	// not something Apply can splice, so it must not count as installed either.
	if IsInstalled(startMarker + "\nexport PATH=/nowhere\n") {
		t.Error("IsInstalled accepted a block with no end marker")
	}
}

// TestParseKey covers the spellings a person might reach for, and the two
// reasons ext turns one down: it cannot express the chord, or it will not take
// the chord away from the terminal.
func TestParseKey(t *testing.T) {
	accepted := []struct {
		name     string
		spec     string
		expected string
	}{
		{name: "ctrl dash", spec: "ctrl-t", expected: "ctrl-t"},
		{name: "ctrl plus", spec: "ctrl+t", expected: "ctrl-t"},
		{name: "control dash", spec: "control-t", expected: "ctrl-t"},
		{name: "readline style", spec: "C-t", expected: "ctrl-t"},
		{name: "caret style", spec: "^T", expected: "ctrl-t"},
		{name: "bare letter", spec: "t", expected: "ctrl-t"},
		{name: "uppercase", spec: "CTRL-T", expected: "ctrl-t"},
		{name: "surrounding space", spec: "  ctrl-t  ", expected: "ctrl-t"},
		{name: "the default", spec: "ctrl-g", expected: "ctrl-g"},
	}

	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			key, err := ParseKey(tc.spec)
			if err != nil {
				t.Fatalf("ParseKey(%q): %v", tc.spec, err)
			}

			if got := key.String(); got != tc.expected {
				t.Errorf("ParseKey(%q) = %q, want %q", tc.spec, got, tc.expected)
			}
		})
	}

	rejected := []struct {
		name string
		spec string
	}{
		{name: "empty", spec: ""},
		{name: "only whitespace", spec: "   "},
		{name: "two letters", spec: "ctrl-ab"},
		{name: "a digit", spec: "ctrl-1"},
		{name: "punctuation", spec: "ctrl-]"},
		{name: "a named key", spec: "ctrl-space"},
		{name: "no letter at all", spec: "ctrl-"},
		{name: "interrupt", spec: "ctrl-c"},
		{name: "end of file", spec: "ctrl-d"},
		{name: "return", spec: "ctrl-m"},
		{name: "tab", spec: "ctrl-i"},
		{name: "suspend", spec: "ctrl-z"},
		{name: "flow control", spec: "ctrl-s"},
	}

	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if _, err := ParseKey(tc.spec); !errors.Is(err, ErrUnknownKey) {
				t.Errorf("ParseKey(%q) error = %v, want ErrUnknownKey", tc.spec, err)
			}
		})
	}
}

// TestRenderBindsTheChosenKey pins the two spellings apart: zsh takes caret
// notation and bash takes readline's, and getting either wrong leaves a block
// that loads without error and binds nothing.
func TestRenderBindsTheChosenKey(t *testing.T) {
	key, err := ParseKey("ctrl-t")
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}

	zsh := Render(Zsh, "/usr/local/bin/ext", key)
	if !strings.Contains(zsh, `bindkey '^T' _ext_picker`) {
		t.Errorf("zsh block does not bind ^T:\n%s", zsh)
	}

	if strings.Contains(zsh, "^G") {
		t.Errorf("zsh block still mentions the default ^G:\n%s", zsh)
	}

	bash := Render(Bash, "/usr/local/bin/ext", key)
	if !strings.Contains(bash, `"\C-t"`) {
		t.Errorf(`bash block does not bind \C-t:`+"\n%s", bash)
	}

	if strings.Contains(bash, `\C-g`) {
		t.Errorf(`bash block still mentions the default \C-g:`+"\n%s", bash)
	}
}

// TestRenderDefaultsAZeroKey keeps a caller who never parsed one from splicing
// a NUL byte into somebody's profile.
func TestRenderDefaultsAZeroKey(t *testing.T) {
	if got, expected := Render(Zsh, "/usr/local/bin/ext", Key{}), "bindkey '^G' _ext_picker"; !strings.Contains(got, expected) {
		t.Errorf("a zero Key did not render the default binding:\n%s", got)
	}
}

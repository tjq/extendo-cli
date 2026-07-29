package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file hand the rendered block to a real shell. Everything
// else in the package is string work checked against goldens; this is the only
// thing that can tell whether the strings are a shell program a shell agrees
// with — a golden happily records a syntax error.
//
// They skip rather than fail when the shell is absent, so the suite still runs
// somewhere without bash or zsh installed.

// shellPath finds an interpreter, skipping the test when there is none.
func shellPath(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed: %v", name, err)
	}

	return path
}

// writeBlock renders a block and writes it to a file the shell can source.
func writeBlock(t *testing.T, k Kind, exePath string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "block.sh")
	if err := os.WriteFile(path, []byte(Render(k, exePath, DefaultKey)), 0o644); err != nil {
		t.Fatalf("write block: %v", err)
	}

	return path
}

// fakeExt writes an executable that announces itself, under whatever name the
// caller wants it installed as.
//
// It announces itself on both streams. The bash binding runs it with neither
// redirected, and the zsh widget sends its stdout to the terminal and captures
// only its stderr — so a marker on one stream alone would leave one of the two
// tests with nothing to look at.
func fakeExt(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho picked\necho picked >&2\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

// runShell runs a script through an interpreter with a PATH holding nothing of
// ours, so anything the block resolves it resolved by itself.
func runShell(t *testing.T, shell string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.Command(shell, args...)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")

	out, err := cmd.CombinedOutput()

	return string(out), err
}

// TestRenderedBlocksParse is the check a golden cannot make: that the block is
// a program the shell it was written for accepts.
func TestRenderedBlocksParse(t *testing.T) {
	cases := []struct {
		name    string
		shell   string
		kind    Kind
		exePath string
	}{
		{name: "zsh", shell: "zsh", kind: Zsh, exePath: brewPath},
		{name: "zsh, renamed binary", shell: "zsh", kind: Zsh, exePath: "/Users/x/go/bin/ext-dev"},
		{name: "zsh, awkward path", shell: "zsh", kind: Zsh, exePath: "/Users/x/App Support/ext$dev"},
		{name: "bash", shell: "bash", kind: Bash, exePath: brewPath},
		{name: "bash, renamed binary", shell: "bash", kind: Bash, exePath: "/Users/x/go/bin/ext-dev"},
		{name: "bash, awkward path", shell: "bash", kind: Bash, exePath: "/Users/x/App Support/ext$dev"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sh := shellPath(t, tc.shell)

			out, err := runShell(t, sh, "-n", writeBlock(t, tc.kind, tc.exePath))
			if err != nil {
				t.Errorf("%s -n rejected the block: %v\n%s\n--- block ---\n%s",
					tc.shell, err, out, Render(tc.kind, tc.exePath, DefaultKey))
			}
		})
	}
}

// TestZshWidgetRunsARenamedBinary is the regression the binding-by-path change
// was for. The binary is installed as `ext-dev`, nothing named `ext` is on PATH,
// and the widget still has to run it — which the previous `command ext` could
// not.
//
// It now also pins the redirection order, which is the part of the widget that
// is easy to get backwards. `2>&1 >/dev/tty` captures stderr and puts stdout on
// the terminal; the intuitive-looking `>/dev/tty 2>&1` puts both on the terminal
// and captures nothing — which would print the picker's errors into whatever
// screen the user pressed the key over, the one thing the widget exists to
// avoid. The fake ext writes its marker to stderr, so a block with the
// redirections the wrong way round captures nothing and this test fails.
//
// The body is run outside zle, so `/dev/tty` becomes `/dev/null` — `go test` has
// no controlling terminal — and the two `zle` calls, which only work inside a
// widget, are dropped or replaced with a print of the message they were handed.
func TestZshWidgetRunsARenamedBinary(t *testing.T) {
	sh := shellPath(t, "zsh")

	exe := fakeExt(t, "ext-dev")

	script := `
source ` + writeBlock(t, Zsh, exe) + `
if whence -p ext >/dev/null; then
  print -r -- "FAIL: a binary named ext is on PATH; this proves nothing"
  exit 1
fi
body=${functions[_ext_picker]}
body=${body//\/dev\/tty/\/dev\/null}
body=${body//zle reset-prompt/}
body=${body//zle -M/print -r --}
eval "_ext_test() { $body }"
_ext_test
`

	out, err := runShell(t, sh, "-f", "-i", "-c", script)
	if err != nil {
		t.Fatalf("zsh: %v\n%s", err, out)
	}

	if !strings.Contains(out, "picked") {
		t.Errorf("the widget did not run %s, or captured the wrong stream:\n%s", exe, out)
	}
}

// TestZshWidgetSaysNothingWhenThePickerDoes is the other half of not disturbing
// the screen: a run that prints nothing has to leave the widget silent, rather
// than calling `zle -M` with an empty message.
func TestZshWidgetSaysNothingWhenThePickerDoes(t *testing.T) {
	sh := shellPath(t, "zsh")

	quiet := filepath.Join(t.TempDir(), "ext")
	if err := os.WriteFile(quiet, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", quiet, err)
	}

	script := `
source ` + writeBlock(t, Zsh, quiet) + `
body=${functions[_ext_picker]}
body=${body//\/dev\/tty/\/dev\/null}
body=${body//zle reset-prompt/}
body=${body//zle -M/print -r -- MESSAGE:}
eval "_ext_test() { $body }"
_ext_test
print -r -- done
`

	out, err := runShell(t, sh, "-f", "-i", "-c", script)
	if err != nil {
		t.Fatalf("zsh: %v\n%s", err, out)
	}

	if strings.Contains(out, "MESSAGE:") {
		t.Errorf("the widget posted a message for a silent run:\n%s", out)
	}
}

// TestZshBlockRegistersTheWidget checks the other half: that an interactive zsh
// sourcing the block ends up with ctrl-G bound to it.
func TestZshBlockRegistersTheWidget(t *testing.T) {
	sh := shellPath(t, "zsh")

	script := "source " + writeBlock(t, Zsh, brewPath) + "\nbindkey '^G'\nzle -l\n"

	out, err := runShell(t, sh, "-f", "-i", "-c", script)
	if err != nil {
		t.Fatalf("zsh: %v\n%s", err, out)
	}

	if !strings.Contains(out, `"^G" _ext_picker`) {
		t.Errorf("ctrl-G is not bound to the widget:\n%s", out)
	}
}

// TestBashBindingIsAcceptedAndRunsTheBinary takes the bind line apart: readline
// has to accept the quoting, and the command it was handed has to be the
// renamed binary. The block guards the bind with `|| true`, so a binding
// readline rejected would otherwise fail silently.
func TestBashBindingIsAcceptedAndRunsTheBinary(t *testing.T) {
	sh := shellPath(t, "bash")

	exe := fakeExt(t, "ext-dev")

	bind, command := bindParts(t, Render(Bash, exe, DefaultKey))

	if out, err := runShell(t, sh, "--norc", "-i", "-c", bind); err != nil {
		t.Errorf("bash rejected %q: %v\n%s", bind, err, out)
	}

	out, err := runShell(t, sh, "--norc", "-c", command)
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, out)
	}

	if !strings.Contains(out, "picked") {
		t.Errorf("the binding's command did not run %s:\n%s", exe, out)
	}
}

// bindParts pulls the bind line out of a bash block, returning it without the
// `2>/dev/null || true` guard, and the command it binds on its own.
func bindParts(t *testing.T, block string) (string, string) {
	t.Helper()

	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(line, "bind -x ") {
			continue
		}

		bind := strings.TrimSuffix(line, ` 2>/dev/null || true`)

		_, command, ok := strings.Cut(bind, `"\C-g": `)
		if !ok {
			t.Fatalf("bind line does not bind ctrl-G: %q", line)
		}

		return bind, strings.TrimSuffix(command, "'")
	}

	t.Fatalf("no bind line in:\n%s", block)

	return "", ""
}

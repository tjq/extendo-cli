package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tjq/extendo-cli/internal/shell"
)

// appRunner answers the one command doctor runs: `pgrep -x extendo`.
type appRunner struct {
	out   []byte
	err   error
	calls []call
}

func (r *appRunner) Run(stdin []byte, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, call{stdin: slices.Clone(stdin), name: name, args: slices.Clone(args)})

	return r.out, r.err
}

// runningApp is a pgrep that found the app.
func runningApp() *appRunner { return &appRunner{out: []byte("412\n")} }

// missingApp is a pgrep that matched nothing, which it reports by exiting 1.
func missingApp() *appRunner { return &appRunner{err: errors.New("running pgrep: exit status 1")} }

// pinLookPath fixes what doctor finds on PATH, so the report does not depend on
// the machine the test runs on.
func pinLookPath(t *testing.T, found ...string) {
	t.Helper()

	previous := lookPath
	lookPath = func(name string) (string, error) {
		if !slices.Contains(found, name) {
			return "", errors.New("exec: \"" + name + "\": executable file not found in $PATH")
		}

		return "/usr/bin/" + name, nil
	}

	t.Cleanup(func() { lookPath = previous })
}

// pinProfile points $HOME at a fresh directory and, when isInstalled is set,
// writes a .zshrc carrying the managed block.
func pinProfile(t *testing.T, isInstalled bool) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")

	if !isInstalled {
		return
	}

	block := shell.Apply("", shell.Render(shell.Zsh, "/opt/homebrew/bin/ext"))
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(block), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
}

// healthyDoctor sets up everything doctor looks at outside the store.
func healthyDoctor(t *testing.T) {
	t.Helper()

	pinLookPath(t, "pbcopy", "osascript")
	pinProfile(t, true)
}

func TestDoctorReportsAHealthyInstall(t *testing.T) {
	healthyDoctor(t)

	runner := runningApp()

	got := run(t, fixtureDir(t), runner, "doctor")
	if got.err != nil {
		t.Fatalf("doctor: %v\n%s", got.err, got.stdout)
	}

	if got.stderr != "" {
		t.Errorf("doctor wrote to stderr: %q", got.stderr)
	}

	for _, expected := range []string{"4 items", "pbcopy", "osascript", ".zshrc", glyphSample} {
		if !strings.Contains(got.stdout, expected) {
			t.Errorf("report is missing %q:\n%s", expected, got.stdout)
		}
	}

	if strings.Contains(got.stdout, markWarn) || strings.Contains(got.stdout, markFail) {
		t.Errorf("healthy report carries a warning or a failure:\n%s", got.stdout)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("ran %d commands, expected one pgrep: %+v", len(runner.calls), runner.calls)
	}

	only := runner.calls[0]
	if only.name != "pgrep" || !slices.Equal(only.args, []string{"-x", "extendo"}) {
		t.Errorf("ran %q %v, expected pgrep -x extendo", only.name, only.args)
	}
}

// TestDoctorWarnsWhenTheAppIsNotRunning is the warning-versus-failure line: the
// history on disk is readable whether or not the app is up, so `ext` still
// works and doctor still exits 0.
func TestDoctorWarnsWhenTheAppIsNotRunning(t *testing.T) {
	healthyDoctor(t)

	got := run(t, fixtureDir(t), missingApp(), "doctor")
	if got.err != nil {
		t.Fatalf("doctor: %v\n%s", got.err, got.stdout)
	}

	if !strings.Contains(got.stdout, "not running") {
		t.Errorf("report does not say the app is down:\n%s", got.stdout)
	}

	if !strings.Contains(got.stdout, markWarn) {
		t.Errorf("report does not mark the warning:\n%s", got.stdout)
	}

	if strings.Contains(got.stdout, markFail) {
		t.Errorf("a stopped app was reported as a failure:\n%s", got.stdout)
	}
}

// TestDoctorWarnsWithoutTheProfileBlock: shell integration is opt-in, so a
// profile without the block is worth pointing at and not worth exiting 1 over.
func TestDoctorWarnsWithoutTheProfileBlock(t *testing.T) {
	pinLookPath(t, "pbcopy", "osascript")
	pinProfile(t, false)

	got := run(t, fixtureDir(t), runningApp(), "doctor")
	if got.err != nil {
		t.Fatalf("doctor: %v\n%s", got.err, got.stdout)
	}

	if !strings.Contains(got.stdout, "ext install") {
		t.Errorf("report does not name the fix:\n%s", got.stdout)
	}
}

// TestDoctorWarnsUnderAnUnsupportedShell: the row cannot point at `ext install
// --profile <file>`, because install refuses an unsupported shell whether or
// not that flag is given. It says what is true instead.
func TestDoctorWarnsUnderAnUnsupportedShell(t *testing.T) {
	pinLookPath(t, "pbcopy", "osascript")
	pinProfile(t, false)
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")

	got := run(t, fixtureDir(t), runningApp(), "doctor")
	if got.err != nil {
		t.Fatalf("doctor: %v\n%s", got.err, got.stdout)
	}

	if !strings.Contains(got.stdout, "zsh and bash") {
		t.Errorf("report does not say which shells are supported:\n%s", got.stdout)
	}

	if strings.Contains(got.stdout, "--profile") {
		t.Errorf("report still offers --profile, which does not help here:\n%s", got.stdout)
	}
}

func TestDoctorFailsWithoutAStore(t *testing.T) {
	healthyDoctor(t)

	got := run(t, t.TempDir(), runningApp(), "doctor")
	if got.err == nil {
		t.Fatalf("doctor against an empty dir returned nil error:\n%s", got.stdout)
	}

	// The report still has to be printed: a bare exit code names nothing.
	if !strings.Contains(got.stdout, markFail) {
		t.Errorf("failing report was not printed:\n%s", got.stdout)
	}
}

func TestDoctorFailsWhenPbcopyIsMissing(t *testing.T) {
	pinLookPath(t, "osascript")
	pinProfile(t, true)

	got := run(t, fixtureDir(t), runningApp(), "doctor")
	if got.err == nil {
		t.Fatalf("doctor without pbcopy returned nil error:\n%s", got.stdout)
	}

	if !strings.Contains(got.stdout, "pbcopy") {
		t.Errorf("report does not name pbcopy:\n%s", got.stdout)
	}
}

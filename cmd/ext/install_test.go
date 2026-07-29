package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// pinExecutable fixes the path the managed block puts on PATH. Under `go test`
// os.Executable names the test binary, which lives in a directory that changes
// every run.
func pinExecutable(t *testing.T, path string) {
	t.Helper()

	previous := executablePath
	executablePath = func() (string, error) { return path, nil }

	t.Cleanup(func() { executablePath = previous })
}

// zshProfile prepares a home directory whose .zshrc already has a line in it,
// and returns the path to that file.
func zshProfile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(path, []byte("export EDITOR=vim\n"), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	return path
}

// currentUmask reads the process umask, which is only readable by setting it.
func currentUmask(t *testing.T) os.FileMode {
	t.Helper()

	mask := syscall.Umask(0)
	syscall.Umask(mask)

	return os.FileMode(mask)
}

// profileText returns a profile's contents.
func profileText(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}

	return string(data)
}

func TestInstallWritesTheBlock(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	pinExecutable(t, "/opt/homebrew/bin/ext")

	profile := zshProfile(t)

	got := run(t, t.TempDir(), &fakeRunner{}, "install", "--profile", profile)
	if got.err != nil {
		t.Fatalf("install: %v (stderr %q)", got.err, got.stderr)
	}

	if got.stdout != "" {
		t.Errorf("stdout = %q, expected empty — status lines belong on stderr", got.stdout)
	}

	if !strings.Contains(got.stderr, profile) {
		t.Errorf("stderr does not name the file it wrote: %q", got.stderr)
	}

	checkGolden(t, "install_zshrc.golden", profileText(t, profile))
}

// TestInstallIsIdempotent is what makes `ext install` safe to re-run after an
// upgrade: the second write replaces the block rather than stacking another
// copy of it under the first.
func TestInstallIsIdempotent(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	pinExecutable(t, "/opt/homebrew/bin/ext")

	profile := zshProfile(t)

	if got := run(t, t.TempDir(), &fakeRunner{}, "install", "--profile", profile); got.err != nil {
		t.Fatalf("install: %v", got.err)
	}

	once := profileText(t, profile)

	if got := run(t, t.TempDir(), &fakeRunner{}, "install", "--profile", profile); got.err != nil {
		t.Fatalf("install (second): %v", got.err)
	}

	if twice := profileText(t, profile); twice != once {
		t.Errorf("second install changed the profile\n--- twice ---\n%s\n--- once ---\n%s", twice, once)
	}
}

func TestInstallCreatesAMissingProfile(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	pinExecutable(t, "/opt/homebrew/bin/ext")

	profile := filepath.Join(t.TempDir(), ".zshrc")

	if got := run(t, t.TempDir(), &fakeRunner{}, "install", "--profile", profile); got.err != nil {
		t.Fatalf("install: %v", got.err)
	}

	info, err := os.Stat(profile)
	if err != nil {
		t.Fatalf("stat profile: %v", err)
	}

	// A profile is sourced, not executed: 0644 is what the file would have had
	// if the user made it. The umask has the last word on that, as it does for
	// anything else they create, so the expectation goes through it too.
	expected := os.FileMode(0o644 &^ currentUmask(t))
	if info.Mode().Perm() != expected {
		t.Errorf("profile mode = %v, expected %v", info.Mode().Perm(), expected)
	}
}

func TestInstallDefaultsToTheDetectedProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	pinExecutable(t, "/opt/homebrew/bin/ext")

	got := run(t, t.TempDir(), &fakeRunner{}, "install")
	if got.err != nil {
		t.Fatalf("install: %v", got.err)
	}

	if !strings.Contains(profileText(t, filepath.Join(home, ".bash_profile")),
		`bind -x '"\C-g": "/opt/homebrew/bin/ext" --quiet'`) {
		t.Error("bash profile is missing the key binding")
	}
}

func TestInstallUninstallRestoresTheProfile(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	pinExecutable(t, "/opt/homebrew/bin/ext")

	profile := zshProfile(t)

	if got := run(t, t.TempDir(), &fakeRunner{}, "install", "--profile", profile); got.err != nil {
		t.Fatalf("install: %v", got.err)
	}

	got := run(t, t.TempDir(), &fakeRunner{}, "install", "--uninstall", "--profile", profile)
	if got.err != nil {
		t.Fatalf("install --uninstall: %v", got.err)
	}

	if !strings.Contains(got.stderr, profile) {
		t.Errorf("stderr does not name the file it edited: %q", got.stderr)
	}

	if left := profileText(t, profile); left != "export EDITOR=vim\n" {
		t.Errorf("profile after uninstall = %q, expected the line it started with", left)
	}
}

// TestInstallUninstallWithoutABlockIsNotAnError keeps `--uninstall` safe to run
// twice, and safe to run against a profile ext never touched.
func TestInstallUninstallWithoutABlockIsNotAnError(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	pinExecutable(t, "/opt/homebrew/bin/ext")

	profile := zshProfile(t)

	got := run(t, t.TempDir(), &fakeRunner{}, "install", "--uninstall", "--profile", profile)
	if got.err != nil {
		t.Fatalf("install --uninstall: %v", got.err)
	}

	if profileText(t, profile) != "export EDITOR=vim\n" {
		t.Error("uninstall rewrote a profile it does not own")
	}
}

// pinTmux fixes what `tmux -V` reports, so the --tmux branch can be driven on a
// machine with no tmux on it.
func pinTmux(t *testing.T, version string, err error) {
	t.Helper()

	previous := tmuxVersion
	tmuxVersion = func() (string, error) { return version, err }

	t.Cleanup(func() { tmuxVersion = previous })
}

func TestInstallTmuxWritesTheBinding(t *testing.T) {
	// $SHELL is fish on purpose: a tmux binding is the same line whatever runs
	// in the pane, so --tmux is the one route to the picker that works under a
	// shell install otherwise has to turn down.
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")
	pinExecutable(t, "/opt/homebrew/bin/ext")
	pinTmux(t, "tmux 3.4\n", nil)

	conf := filepath.Join(t.TempDir(), ".tmux.conf")

	got := run(t, t.TempDir(), &fakeRunner{}, "install", "--tmux", "--profile", conf)
	if got.err != nil {
		t.Fatalf("install --tmux: %v (stderr %q)", got.err, got.stderr)
	}

	if !strings.Contains(got.stderr, "tmux source-file "+conf) {
		t.Errorf("stderr does not say how to reload tmux: %q", got.stderr)
	}

	checkGolden(t, "install_tmux_conf.golden", profileText(t, conf))
}

// TestInstallTmuxRefusesAnOldTmux is the check that matters most here. A binding
// using display-popup is an unknown command to a tmux older than 3.2, and an
// error stops tmux reading the rest of the config — so an install that went
// ahead would cost the user every setting below the block.
func TestInstallTmuxRefusesAnOldTmux(t *testing.T) {
	pinExecutable(t, "/opt/homebrew/bin/ext")
	pinTmux(t, "tmux 3.0a\n", nil)

	conf := filepath.Join(t.TempDir(), ".tmux.conf")

	got := run(t, t.TempDir(), &fakeRunner{}, "install", "--tmux", "--profile", conf)
	if got.err == nil {
		t.Fatal("install --tmux on tmux 3.0a returned nil error, expected a refusal")
	}

	for _, expected := range []string{"3.0a", "3.2"} {
		if !strings.Contains(got.err.Error(), expected) {
			t.Errorf("refusal does not mention %q: %v", expected, got.err)
		}
	}

	if _, err := os.Stat(conf); !os.IsNotExist(err) {
		t.Errorf("a refused install still wrote %s (stat err %v)", conf, err)
	}
}

func TestInstallTmuxRefusesAMissingTmux(t *testing.T) {
	pinExecutable(t, "/opt/homebrew/bin/ext")
	pinTmux(t, "", errors.New("executable file not found in $PATH"))

	conf := filepath.Join(t.TempDir(), ".tmux.conf")

	got := run(t, t.TempDir(), &fakeRunner{}, "install", "--tmux", "--profile", conf)
	if got.err == nil {
		t.Fatal("install --tmux with no tmux returned nil error, expected a refusal")
	}

	if !strings.Contains(got.err.Error(), "not appear to be installed") {
		t.Errorf("refusal does not say tmux is missing: %v", got.err)
	}
}

// TestInstallTmuxUninstall keeps the two blocks independent: --tmux --uninstall
// edits the tmux config, and says the thing about tmux that a shell profile does
// not need — a running server keeps the binding until it is told otherwise.
func TestInstallTmuxUninstall(t *testing.T) {
	pinExecutable(t, "/opt/homebrew/bin/ext")
	pinTmux(t, "tmux 3.4\n", nil)

	conf := filepath.Join(t.TempDir(), ".tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g mouse on\n"), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}

	if got := run(t, t.TempDir(), &fakeRunner{}, "install", "--tmux", "--profile", conf); got.err != nil {
		t.Fatalf("install --tmux: %v", got.err)
	}

	got := run(t, t.TempDir(), &fakeRunner{}, "install", "--tmux", "--uninstall", "--profile", conf)
	if got.err != nil {
		t.Fatalf("install --tmux --uninstall: %v", got.err)
	}

	if !strings.Contains(got.stderr, "unbind-key") {
		t.Errorf("stderr does not say the running server keeps the binding: %q", got.stderr)
	}

	if left := profileText(t, conf); left != "set -g mouse on\n" {
		t.Errorf("conf after uninstall = %q, expected the line it started with", left)
	}
}

// TestTmuxConfPrefersAnExistingXDGFile: tmux 3.1 and later read
// ~/.config/tmux/tmux.conf too, and someone who keeps one there has moved
// deliberately. Writing into the file they already have puts the binding where
// they will find it.
func TestTmuxConfPrefersAnExistingXDGFile(t *testing.T) {
	home := t.TempDir()

	if got, expected := tmuxConf(home), filepath.Join(home, ".tmux.conf"); got != expected {
		t.Errorf("tmuxConf with no XDG file = %q, expected %q", got, expected)
	}

	xdg := filepath.Join(home, ".config", "tmux")
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// The directory alone is not the file: tmux reads the file, and creating one
	// beside a config the user actually uses would split their settings in two.
	if got, expected := tmuxConf(home), filepath.Join(home, ".tmux.conf"); got != expected {
		t.Errorf("tmuxConf with an empty XDG directory = %q, expected %q", got, expected)
	}

	conf := filepath.Join(xdg, "tmux.conf")
	if err := os.WriteFile(conf, nil, 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}

	if got := tmuxConf(home); got != conf {
		t.Errorf("tmuxConf = %q, expected %q", got, conf)
	}
}

// TestParseTmuxVersion covers the shapes `tmux -V` actually prints. A patch
// release carries a letter, and a build from the development branch is named
// for the release it precedes — which is newer than that release, so it has to
// be accepted rather than rejected as unparseable.
func TestParseTmuxVersion(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		expected int
		ok       bool
	}{
		{name: "release", out: "tmux 3.4\n", expected: 304, ok: true},
		{name: "patch letter", out: "tmux 3.2a\n", expected: 302, ok: true},
		{name: "development build", out: "tmux next-3.6\n", expected: 306, ok: true},
		{name: "the oldest that works", out: "tmux 3.2\n", expected: 302, ok: true},
		{name: "too old", out: "tmux 2.9\n", expected: 209, ok: true},
		{name: "no version", out: "tmux\n", ok: false},
		{name: "empty", out: "", ok: false},
		{name: "no minor", out: "tmux 3\n", ok: false},
		{name: "not a number", out: "tmux x.y\n", ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTmuxVersion(tc.out)
			if ok != tc.ok {
				t.Fatalf("parseTmuxVersion(%q) ok = %v, expected %v", tc.out, ok, tc.ok)
			}

			if ok && got != tc.expected {
				t.Errorf("parseTmuxVersion(%q) = %d, expected %d", tc.out, got, tc.expected)
			}
		})
	}

	// The comparison the version gate makes, spelled out: 3.2 is in and 2.9 is
	// out, and a two-digit minor must not sort below a one-digit one.
	if tmuxVersionOrdinal("3.2") != 302 {
		t.Errorf("tmuxVersionOrdinal(3.2) = %d, expected 302", tmuxVersionOrdinal("3.2"))
	}

	if ordinal, _ := parseTmuxVersion("tmux 3.10\n"); ordinal <= 304 {
		t.Errorf("tmux 3.10 sorted below 3.4: %d", ordinal)
	}
}

// TestInstallRefusesAnUnknownShell: the refusal has to be honest about what is
// and is not supported, since there is nothing here the user can do to make it
// work.
func TestInstallRefusesAnUnknownShell(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")
	pinExecutable(t, "/opt/homebrew/bin/ext")

	got := run(t, t.TempDir(), &fakeRunner{}, "install")
	if got.err == nil {
		t.Fatal("install under fish returned nil error, expected a refusal")
	}

	for _, expected := range []string{"fish", "zsh and bash"} {
		if !strings.Contains(got.err.Error(), expected) {
			t.Errorf("refusal does not mention %q: %v", expected, got.err)
		}
	}
}

// TestInstallRefusesAnUnknownShellWithProfile is why the message above no
// longer offers --profile as the way out: the flag names the file, not the
// syntax, so the same refusal comes back one step later. A message pointing at
// it would be sending the user round a loop.
func TestInstallRefusesAnUnknownShellWithProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")
	pinExecutable(t, "/opt/homebrew/bin/ext")

	profile := filepath.Join(t.TempDir(), "config.fish")

	got := run(t, t.TempDir(), &fakeRunner{}, "install", "--profile", profile)
	if got.err == nil {
		t.Fatal("install --profile under fish returned nil error, expected a refusal")
	}

	if !strings.Contains(got.err.Error(), "zsh and bash") {
		t.Errorf("refusal does not say which shells are supported: %v", got.err)
	}

	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Errorf("a refused install still wrote %s (stat err %v)", profile, err)
	}
}

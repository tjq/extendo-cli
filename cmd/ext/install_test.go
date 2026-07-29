package main

import (
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
		`bind -x '"\C-g": "/opt/homebrew/bin/ext"'`) {
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

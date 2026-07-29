package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tjq/extendo-cli/internal/shell"
)

// executablePath reports the running binary's path, which is what the managed
// block puts on PATH. It is a variable so tests can pin a path that does not
// change with the directory `go test` builds into.
var executablePath = os.Executable

// profileMode is what a profile ext creates is given. Every shell the user
// starts reads this file; nothing but they write it.
const profileMode = 0o644

func newInstallCmd() *cobra.Command {
	profile := ""
	isUninstall := false
	isTmux := false
	keySpec := shell.DefaultKey.String()

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the managed block into your shell profile",
		Long: "install adds a block to your shell profile that puts ext on PATH and\n" +
			"binds a key to the picker.\n\n" +
			"The key is ctrl-G unless --key names another; ext binds ctrl plus a\n" +
			"letter, and refuses the handful the terminal needs for itself, such as\n" +
			"ctrl-C.\n\n" +
			"--tmux writes a tmux binding to ~/.tmux.conf instead. A shell binding\n" +
			"only fires at the shell's own prompt; a tmux one fires while a program in\n" +
			"the pane owns the terminal too, so the picker opens over a password\n" +
			"prompt, an editor, or a REPL.\n\n" +
			"The block is delimited by markers, so re-running install after an upgrade\n" +
			"replaces it rather than adding a second copy, and --uninstall takes it back\n" +
			"out leaving the rest of the file alone.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInstall(cmd, profile, keySpec, isUninstall, isTmux)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "",
		"edit this file instead of the profile belonging to $SHELL")
	cmd.Flags().BoolVar(&isUninstall, "uninstall", false,
		"remove the managed block instead of writing it")
	cmd.Flags().StringVar(&keySpec, "key", keySpec,
		"bind this chord instead, written like \"ctrl-t\"")
	cmd.Flags().BoolVar(&isTmux, "tmux", false,
		"bind the key in ~/.tmux.conf, where it fires over a running program too")

	return cmd
}

func runInstall(cmd *cobra.Command, profile, keySpec string, isUninstall, isTmux bool) error {
	path, err := resolveTarget(profile, isTmux)
	if err != nil {
		return err
	}

	existing, err := readProfile(path)
	if err != nil {
		return err
	}

	if isUninstall {
		return uninstall(cmd, path, existing, isTmux)
	}

	// Parsed after the uninstall branch: taking the block back out does not
	// bind anything, so a bad --key should not stand in the way of removing it.
	key, err := shell.ParseKey(keySpec)
	if err != nil {
		return err
	}

	if isTmux {
		return installTmux(cmd, path, existing, key)
	}

	return install(cmd, path, existing, key)
}

func install(cmd *cobra.Command, path, existing string, key shell.Key) error {
	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("locating the ext binary: %w", err)
	}

	block := shell.Render(shell.Detect(os.Getenv(shellEnv)), exe, key)
	if block == "" {
		return errUnknownShell()
	}

	if err := writeProfile(path, shell.Apply(existing, block)); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "✓ wrote the extendo-cli block to %s\n", path)
	fmt.Fprintf(cmd.ErrOrStderr(), "  %s opens the picker; open a new terminal, or run: source %s\n",
		key, path)

	return nil
}

// installTmux writes the popup binding into a tmux config.
//
// The tmux version is checked before anything is written. A binding using
// display-popup is an unknown command to a tmux older than 3.2, and an error in
// a config file stops tmux reading the rest of it — so an unchecked install
// would silently cost the user every setting below the block.
func installTmux(cmd *cobra.Command, path, existing string, key shell.Key) error {
	if err := checkTmux(); err != nil {
		return err
	}

	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("locating the ext binary: %w", err)
	}

	if err := writeProfile(path, shell.Apply(existing, shell.RenderTmux(exe, key))); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "✓ wrote the extendo-cli block to %s\n", path)
	fmt.Fprintf(cmd.ErrOrStderr(),
		"  %s opens the picker in a popup, over whatever the pane is running\n", key)
	fmt.Fprintf(cmd.ErrOrStderr(), "  reload tmux with: tmux source-file %s\n", path)

	return nil
}

func uninstall(cmd *cobra.Command, path, existing string, isTmux bool) error {
	updated, wasInstalled := shell.Remove(existing)
	if !wasInstalled {
		fmt.Fprintf(cmd.ErrOrStderr(), "no extendo-cli block in %s\n", path)

		return nil
	}

	if err := writeProfile(path, updated); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "✓ removed the extendo-cli block from %s\n", path)

	// A shell reads its profile at startup, so a new terminal is already clean.
	// tmux holds its bindings in a running server, which re-reading the config
	// does not clear: the binding survives until it is unbound or the server
	// stops, and saying so beats leaving the user to find that out by pressing
	// the key.
	//
	// The chord is left as a placeholder rather than filled in. Uninstall does
	// not parse --key — deliberately, so a bad one cannot stand in the way of a
	// removal — and printing the default at someone who bound ctrl-T would be a
	// command that silently unbinds nothing.
	if isTmux {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"  the running tmux server keeps the binding until it exits; "+
				"drop it now with: tmux unbind-key -n C-<key>")
	}

	return nil
}

// shellEnv names the variable every shell exports with its own path.
const shellEnv = "SHELL"

// resolveTarget picks the file to edit: the one the user named, the tmux config
// under --tmux, or the profile belonging to $SHELL.
//
// --tmux never consults $SHELL. A tmux binding is the same line whatever shell
// runs inside the pane, so this is the one route to the picker that works under
// fish — which install otherwise has to turn down.
func resolveTarget(profile string, isTmux bool) (string, error) {
	if profile != "" {
		return profile, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating your home directory: %w", err)
	}

	if isTmux {
		return tmuxConf(home), nil
	}

	path, err := shell.ProfilePath(shell.Detect(os.Getenv(shellEnv)), home)
	if errors.Is(err, shell.ErrUnknownShell) {
		return "", errUnknownShell()
	}

	if err != nil {
		return "", err
	}

	return path, nil
}

// tmuxConf picks which config file to write.
//
// tmux 3.1 and later read ~/.config/tmux/tmux.conf as well as ~/.tmux.conf, and
// someone who keeps one there has moved deliberately. Writing into the file
// that already exists puts the binding where they will find it; ~/.tmux.conf is
// the fallback, and the file tmux has always read.
func tmuxConf(home string) string {
	xdg := filepath.Join(home, ".config", "tmux", "tmux.conf")
	if _, err := os.Stat(xdg); err == nil {
		return xdg
	}

	return filepath.Join(home, ".tmux.conf")
}

// tmuxVersion reports what `tmux -V` prints. It is a variable so tests can drive
// the --tmux branch on a machine with no tmux, or an old one.
var tmuxVersion = func() (string, error) {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// checkTmux refuses to write the popup binding into a tmux that cannot run it.
// See installTmux for why this is worth a check rather than a broken key.
func checkTmux() error {
	out, err := tmuxVersion()
	if err != nil {
		return fmt.Errorf("running `tmux -V`: %w — "+
			"--tmux writes a binding for tmux, which does not appear to be installed", err)
	}

	version, ok := parseTmuxVersion(out)
	if !ok {
		return fmt.Errorf("cannot read a version out of `tmux -V` (%q) — "+
			"the popup binding needs tmux %s or newer", strings.TrimSpace(out), shell.MinTmux)
	}

	if version < tmuxVersionOrdinal(shell.MinTmux) {
		return fmt.Errorf("tmux %s is too old: display-popup arrived in %s, and a binding "+
			"using it would stop tmux reading the rest of your config",
			strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "tmux ")), shell.MinTmux)
	}

	return nil
}

// parseTmuxVersion turns `tmux -V` output into a comparable number.
//
// The version is not a plain decimal: a patch release is "3.2a", and a build
// from the development branch says "tmux next-3.6". Both carry the major and
// minor that decide whether display-popup is there, so both are read rather
// than rejected — a `next` build is newer than the release it is named for, and
// refusing to install on one would be wrong.
func parseTmuxVersion(out string) (int, bool) {
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return 0, false
	}

	major, rest, found := strings.Cut(strings.TrimPrefix(fields[1], "next-"), ".")
	if !found {
		return 0, false
	}

	return versionOrdinal(major, leadingDigits(rest))
}

// tmuxVersionOrdinal reads a version this package spells itself, where a
// malformed one is a bug rather than a machine's answer.
func tmuxVersionOrdinal(version string) int {
	major, minor, _ := strings.Cut(version, ".")

	ordinal, _ := versionOrdinal(major, minor)

	return ordinal
}

// versionOrdinal packs a major and minor into one comparable int. The minor is
// given a hundred values, which is more than tmux has ever used.
func versionOrdinal(major, minor string) (int, bool) {
	majorN, err := strconv.Atoi(major)
	if err != nil {
		return 0, false
	}

	minorN, err := strconv.Atoi(minor)
	if err != nil {
		return 0, false
	}

	return majorN*100 + minorN, true
}

// leadingDigits returns the digits s starts with, dropping the patch letter a
// tmux minor version can carry.
func leadingDigits(s string) string {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return s[:i]
		}
	}

	return s
}

// errUnknownShell reports a $SHELL there is no block to write for.
//
// It does not offer --profile as a way out. That flag names the file to write
// and nothing else: the block is still zsh or bash syntax, so install with
// --profile under fish fails here all the same, one step later. Sending someone
// round that loop is worse than telling them plainly that their shell is not
// supported.
func errUnknownShell() error {
	return fmt.Errorf("%w: $SHELL is %q — ext only knows zsh and bash, "+
		"and --profile names a file, not a syntax; ctrl-G is unavailable, "+
		"but every `ext` command works as it is",
		shell.ErrUnknownShell, os.Getenv(shellEnv))
}

// readProfile returns a profile's contents, treating a file that does not exist
// as an empty one: install creates it.
func readProfile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	return string(data), nil
}

func writeProfile(path, contents string) error {
	if err := os.WriteFile(path, []byte(contents), profileMode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

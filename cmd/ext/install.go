package main

import (
	"errors"
	"fmt"
	"os"

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
	keySpec := shell.DefaultKey.String()

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the managed block into your shell profile",
		Long: "install adds a block to your shell profile that puts ext on PATH and\n" +
			"binds a key to the picker.\n\n" +
			"The key is ctrl-G unless --key names another; ext binds ctrl plus a\n" +
			"letter, and refuses the handful the terminal needs for itself, such as\n" +
			"ctrl-C.\n\n" +
			"The block is delimited by markers, so re-running install after an upgrade\n" +
			"replaces it rather than adding a second copy, and --uninstall takes it back\n" +
			"out leaving the rest of the file alone.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInstall(cmd, profile, keySpec, isUninstall)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "",
		"edit this file instead of the profile belonging to $SHELL")
	cmd.Flags().BoolVar(&isUninstall, "uninstall", false,
		"remove the managed block instead of writing it")
	cmd.Flags().StringVar(&keySpec, "key", keySpec,
		"bind this chord instead, written like \"ctrl-t\"")

	return cmd
}

func runInstall(cmd *cobra.Command, profile, keySpec string, isUninstall bool) error {
	path, err := resolveProfile(profile)
	if err != nil {
		return err
	}

	existing, err := readProfile(path)
	if err != nil {
		return err
	}

	if isUninstall {
		return uninstall(cmd, path, existing)
	}

	// Parsed after the uninstall branch: taking the block back out does not
	// bind anything, so a bad --key should not stand in the way of removing it.
	key, err := shell.ParseKey(keySpec)
	if err != nil {
		return err
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

func uninstall(cmd *cobra.Command, path, existing string) error {
	updated, wasInstalled := shell.Remove(existing)
	if !wasInstalled {
		fmt.Fprintf(cmd.ErrOrStderr(), "no extendo-cli block in %s\n", path)

		return nil
	}

	if err := writeProfile(path, updated); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "✓ removed the extendo-cli block from %s\n", path)

	return nil
}

// shellEnv names the variable every shell exports with its own path.
const shellEnv = "SHELL"

// resolveProfile picks the file to edit: the one the user named, or the profile
// belonging to $SHELL.
func resolveProfile(profile string) (string, error) {
	if profile != "" {
		return profile, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating your home directory: %w", err)
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

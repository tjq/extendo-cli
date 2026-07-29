package main

import (
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/tjq/extendo-cli/internal/clip"
	"github.com/tjq/extendo-cli/internal/store"
	"github.com/tjq/extendo-cli/internal/tui"
)

// openPicker runs the interactive picker. It is a variable so tests can drive
// the terminal branch of the root command without a terminal.
var openPicker = tui.Run

// isStdoutTTY reports whether stdout is a terminal rather than a pipe or file.
// It is a variable so tests can pin the branch a bare `ext` takes.
var isStdoutTTY = func() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// newRootCmd builds the whole command tree. Every dependency the commands
// touch is passed in, so tests drive the CLI against a temp store and a fake
// pasteboard.
func newRootCmd(s *store.Store, r clip.Runner) *cobra.Command {
	isASCII := false
	isNerd := false

	root := &cobra.Command{
		Use:   "ext",
		Short: "Browse and restore extendo clipboard history",
		Long: "ext reads the clipboard history the extendo macOS app keeps and puts\n" +
			"entries back on the pasteboard.\n\n" +
			"Run without arguments it opens the interactive picker on a terminal, and\n" +
			"prints the list when its output is piped.",
		Args: cobra.NoArgs,
		// Cobra would otherwise dump the full usage text after every runtime
		// failure, and print the error a second time on top of Execute's.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isStdoutTTY() {
				return runList(cmd, s, false)
			}

			return runPicker(cmd, s, r, tui.Options{
				ASCII: isASCII || isASCIIEnv(),
				Nerd:  isNerd || isNerdEnv(),
			})
		},
	}

	root.PersistentFlags().BoolVar(&isASCII, "ascii", false,
		"draw the picker with plain ASCII stand-ins instead of symbols")
	root.PersistentFlags().BoolVar(&isNerd, "nerd", false,
		"draw the picker with Nerd Font icons, which need a patched font")
	root.PersistentFlags().BoolP(quietFlag, "q", false,
		"do not print the ✓ confirmation line; errors are still reported")

	root.AddCommand(
		newListCmd(s),
		newGetCmd(s, r),
		newPinCmd(s),
		newDeleteCmd(s),
		newInstallCmd(),
		newDoctorCmd(s, r),
		newVersionCmd(),
	)

	return root
}

// asciiEnv opts into the plain glyph set from the environment, for terminals
// whose font a user cannot change per-invocation.
const asciiEnv = "EXTENDO_ASCII"

// nerdEnv opts into the Nerd Font glyph set from the environment, for a
// terminal whose patched font is a fixed part of its profile.
const nerdEnv = "EXTENDO_NERD"

// isASCIIEnv reports whether the environment asks for the plain glyph set. Any
// value but "0" counts as on, which is how an opt-in variable is expected to
// behave from a shell profile.
func isASCIIEnv() bool {
	return isEnvOn(asciiEnv)
}

// isNerdEnv reports whether the environment asks for the Nerd Font glyph set.
func isNerdEnv() bool {
	return isEnvOn(nerdEnv)
}

func isEnvOn(name string) bool {
	value := os.Getenv(name)

	return value != "" && value != "0"
}

// quietFlag names the persistent flag that suppresses confirmation lines.
const quietFlag = "quiet"

// confirmWriter returns where a command's ✓ line goes: stderr, or nowhere under
// --quiet.
//
// It suppresses confirmations and not errors, which is the distinction the flag
// is for. The hotkey bindings `ext install` writes pass it because a key pressed
// over a password prompt or a half-typed command has no business printing
// anything into that screen — but a copy that actually failed still has to say
// so, and the zsh widget puts that text on the status line rather than in
// scrollback.
//
// A lookup failure means no such flag, which only happens if a command is built
// outside the root: stderr is the safe answer, since the cost of getting it
// wrong is a line of output rather than a silent failure.
func confirmWriter(cmd *cobra.Command) io.Writer {
	if isQuiet, err := cmd.Flags().GetBool(quietFlag); err == nil && isQuiet {
		return io.Discard
	}

	return cmd.ErrOrStderr()
}

// runPicker opens the picker and copies whatever the user chose.
//
// The copy happens here rather than inside the picker so that it runs after the
// alt-screen is torn down: a failure can then report itself on a restored
// terminal instead of into a frame that is about to be wiped.
func runPicker(cmd *cobra.Command, s *store.Store, r clip.Runner, opts tui.Options) error {
	selected, err := openPicker(s, opts)
	if err != nil {
		return err
	}

	if selected == nil {
		return nil
	}

	if err := clip.Copy(s, selected.Item, r); err != nil {
		return err
	}

	label := confirmLabel(s, selected.Item)

	// The picker can pin or delete while it is open, so the number this line
	// quotes comes from a fresh read rather than from the order the picker
	// started with. A read that fails, or an item the app has trimmed in the
	// meantime, costs the number and nothing else: the copy has happened, and
	// reporting it as a failure would be a lie.
	position := 0
	if items, err := loadSorted(s); err == nil {
		position = positionOf(items, selected.Item)
	}

	if position == 0 {
		fmt.Fprintf(confirmWriter(cmd), "✓ copied (%s)\n", label)

		return nil
	}

	fmt.Fprintf(confirmWriter(cmd), "✓ copied #%d (%s)\n", position, label)

	return nil
}

// Execute runs the CLI against the real store and pasteboard, returning the
// process exit code. Keeping os.Exit out of the command bodies lets deferred
// cleanup run.
func Execute() int {
	root := newRootCmd(store.Open(store.DefaultDir()), clip.ExecRunner{})

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ext:", err)

		return 1
	}

	return 0
}

// loadSorted reads the history in the order the popup shows it, which is also
// the order the printed indices refer to.
func loadSorted(s *store.Store) ([]store.Item, error) {
	items, err := s.Load()
	if err != nil {
		return nil, err
	}

	return store.Sorted(items), nil
}

// positionOf reports an item's 1-based place in items, so status lines can name
// the same number the user typed. It returns 0 when the item is absent, which
// cannot happen for an item Resolve just returned.
func positionOf(items []store.Item, it store.Item) int {
	return slices.IndexFunc(items, func(other store.Item) bool { return other.ID == it.ID }) + 1
}

// resolveArg turns a user's index or ID prefix into an item and its position.
func resolveArg(s *store.Store, arg string) (store.Item, int, error) {
	items, err := loadSorted(s)
	if err != nil {
		return store.Item{}, 0, err
	}

	it, err := s.Resolve(items, arg)
	if err != nil {
		return store.Item{}, 0, err
	}

	return it, positionOf(items, it), nil
}

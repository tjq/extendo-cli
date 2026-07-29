package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/tjq/extendo-cli/internal/clip"
	"github.com/tjq/extendo-cli/internal/store"
)

func newGetCmd(s *store.Store, r clip.Runner) *cobra.Command {
	isPrint := false

	cmd := &cobra.Command{
		Use:   "get <n|id>",
		Short: "Put a history item back on the pasteboard",
		Long: "get copies the item named by its list position or by any unambiguous\n" +
			"prefix of its id back onto the pasteboard.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			it, position, err := resolveArg(s, args[0])
			if err != nil {
				return err
			}

			if isPrint {
				return printText(cmd.OutOrStdout(), s, it)
			}

			if err := clip.Copy(s, it, r); err != nil {
				return err
			}

			fmt.Fprintf(confirmWriter(cmd), "✓ copied #%d (%s)\n", position, confirmLabel(s, it))

			return nil
		},
	}

	cmd.Flags().BoolVar(&isPrint, "print", false, "write the item's text to stdout instead of copying it")

	return cmd
}

// confirmLabel names an item in a one-line confirmation.
//
// A secret is named by its category alone. The list's masked preview reveals a
// seven-character prefix of the value, which is a reasonable trade when the
// user is looking at their own history on screen — but this line goes to
// stderr, where it survives in scrollback, in `2>` redirects, and in CI logs
// long after the copy. Nothing derived from the value belongs there.
func confirmLabel(s *store.Store, it store.Item) string {
	label, category := describe(s, it)
	if category != "" {
		return category.Label()
	}

	return label
}

// printText writes an item's text verbatim — no trailing newline, so what a
// caller captures is byte-identical to what a paste would produce.
func printText(w io.Writer, s *store.Store, it store.Item) error {
	if kind := it.Kind(); kind == store.KindImage || kind == store.KindFile {
		return fmt.Errorf("item %s holds %s content, which has no text to print", it.ID, kind)
	}

	text, ok := s.PlainText(it)
	if !ok {
		return fmt.Errorf("item %s carries no readable text", it.ID)
	}

	if _, err := io.WriteString(w, text); err != nil {
		return fmt.Errorf("writing item text: %w", err)
	}

	return nil
}

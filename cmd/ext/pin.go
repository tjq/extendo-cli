package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tjq/extendo-cli/internal/store"
)

func newPinCmd(s *store.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "pin <n|id>",
		Short: "Pin or unpin a history item",
		Long: "pin flips an item's pinned flag. Pinned items sort to the top of the\n" +
			"list and survive the app's history trimming.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			it, position, err := resolveArg(s, args[0])
			if err != nil {
				return err
			}

			isPinned, err := s.TogglePin(it.ID)
			if err != nil {
				return err
			}

			status := "unpinned"
			if isPinned {
				status = pinMark + " pinned"
			}

			fmt.Fprintf(confirmWriter(cmd), "%s #%d\n", status, position)

			return nil
		},
	}
}

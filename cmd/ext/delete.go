package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tjq/extendo-cli/internal/store"
)

func newDeleteCmd(s *store.Store) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <n|id>",
		Aliases: []string{"rm"},
		Short:   "Remove a history item",
		Long: "delete drops an item from the history and unlinks any blob files it\n" +
			"owned. The removal is permanent.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			it, position, err := resolveArg(s, args[0])
			if err != nil {
				return err
			}

			if err := s.Delete(it.ID); err != nil {
				return err
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "✗ deleted #%d\n", position)

			return nil
		},
	}
}

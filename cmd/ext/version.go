package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is stamped in at build time:
//
//	go build -ldflags "-X main.version=$(git describe --tags)" ./cmd/ext
//
// An unstamped binary reports "dev" rather than a version it cannot back up.
var version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ext version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), version); err != nil {
				return fmt.Errorf("writing version: %w", err)
			}

			return nil
		},
	}
}

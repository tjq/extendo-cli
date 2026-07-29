package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/tjq/extendo-cli/internal/secrets"
	"github.com/tjq/extendo-cli/internal/store"
	"github.com/tjq/extendo-cli/internal/tui"
)

// now reports the current time. It is a variable so tests can freeze the AGE
// column and compare the table against a golden file.
var now = time.Now

func newListCmd(s *store.Store) *cobra.Command {
	isJSON := false

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List clipboard history, pinned first then newest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd, s, isJSON)
		},
	}

	cmd.Flags().BoolVar(&isJSON, "json", false, "emit the history as a JSON array")

	return cmd
}

// runList is shared with the root command, which lists when its output is piped.
func runList(cmd *cobra.Command, s *store.Store, isJSON bool) error {
	items, err := loadSorted(s)
	if err != nil {
		return err
	}

	if isJSON {
		return writeJSON(cmd.OutOrStdout(), s, items)
	}

	return writeTable(cmd.OutOrStdout(), s, items)
}

// listEntry is the --json shape. The field order fixes the key order in the
// encoded output.
type listEntry struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	CreatedAt string `json:"createdAt"`
	Pinned    bool   `json:"pinned"`
	Secret    string `json:"secret"`
}

func writeJSON(w io.Writer, s *store.Store, items []store.Item) error {
	entries := make([]listEntry, 0, len(items))

	for _, it := range items {
		label, category := describe(s, it)

		entries = append(entries, listEntry{
			ID:        it.ID,
			Kind:      it.Kind().String(),
			Label:     label,
			CreatedAt: it.CreatedAt.Format(time.RFC3339),
			Pinned:    it.IsPinned,
			Secret:    string(category),
		})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding items: %w", err)
	}

	if _, err := fmt.Fprintln(w, string(data)); err != nil {
		return fmt.Errorf("writing json: %w", err)
	}

	return nil
}

// pinMark flags a pinned row. It is the last column, so its double display
// width never disturbs the alignment of the columns before it.
const pinMark = "📌"

// maxLabelWidth bounds the LABEL column. tabwriter sizes a column to its widest
// cell, so without a cap one multi-line paste — a thousand cells wide is
// ordinary — pads every other row out to match and pushes AGE and PIN off the
// screen. 55 cells is what the other columns and their padding leave of an
// 80-column terminal.
//
// It bounds only this table. --json carries the label whole, because that is
// the output scripts read.
const maxLabelWidth = 55

// writeTable renders the history as aligned columns.
//
// The output goes through a buffer rather than straight to w because tabwriter
// pads the AGE cell of every unpinned row out to the column width, leaving
// trailing spaces that make the golden files — and a terminal's selection —
// messier than they need to be.
func writeTable(w io.Writer, s *store.Store, items []store.Item) error {
	buf := &bytes.Buffer{}
	tw := tabwriter.NewWriter(buf, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "N\tKIND\tLABEL\tAGE\tPIN")

	reference := now()

	for i, it := range items {
		label, _ := describe(s, it)

		mark := ""
		if it.IsPinned {
			mark = pinMark
		}

		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n",
			i+1,
			it.Kind(),
			tui.Truncate(label, maxLabelWidth),
			tui.Rel(it.CreatedAt, reference),
			mark,
		)
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("rendering table: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if _, err := fmt.Fprintln(w, strings.TrimRight(line, " ")); err != nil {
			return fmt.Errorf("writing table: %w", err)
		}
	}

	return nil
}

// describe renders the label a row shows, masking the value when it looks like
// a credential.
//
// Only textual items are classified. An image or file item can carry an
// incidental text representation, but that text is not what the row displays,
// so matching a secret in it would replace an accurate label with a wrong one.
func describe(s *store.Store, it store.Item) (string, secrets.Category) {
	isTextual := it.Kind() == store.KindText || it.Kind() == store.KindRichText
	if !isTextual {
		return s.DisplayLabel(it), ""
	}

	text, ok := s.PlainText(it)
	if !ok {
		return s.DisplayLabel(it), ""
	}

	category, isSecret := secrets.Classify(text)
	if !isSecret {
		return s.DisplayLabel(it), ""
	}

	// Classification reads the text as captured; the mask is built from a
	// sanitized copy. Mask keeps the first few runes of the first line verbatim,
	// and this cell goes through a tab-delimited writer to a terminal.
	return category.Label() + " · " + secrets.Mask(store.Printable(text)), category
}

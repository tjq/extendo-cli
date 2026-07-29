// Package tui renders the fullscreen picker: one page of clipboard history at
// a time, with a number key on every row.
package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tjq/extendo-cli/internal/store"
)

// Options configures one run of the picker.
type Options struct {
	// ASCII swaps the default glyphs for plain stand-ins.
	ASCII bool
	// Nerd swaps them for Nerd Font icons, which need a patched font. ASCII
	// wins if both are set, being the one that renders anywhere.
	Nerd bool
	// Now reports the current time; nil means time.Now. Tests pin it so the age
	// column has exactly one right answer.
	Now func() time.Time
}

// Selected is the item a user picked.
//
// Label is safe to echo once the picker has exited: for an item that looks like
// a credential it names the category and nothing else, because a confirmation
// line survives in the terminal's scrollback.
type Selected struct {
	Item  store.Item
	Label string
}

// Run opens the picker and returns the item the user chose, or nil when they
// quit without choosing.
//
// The picker never touches the pasteboard itself. Copying is the caller's job,
// so it happens after the alt-screen is torn down — a copy that fails can then
// report itself on a restored terminal instead of into a frame that is about to
// be wiped.
//
// Store failures during the session are shown in the picker's footer and are
// not returned here: the user has already seen them, and a pin that failed
// should not cancel the selection they went on to make.
//
// A history file that does not exist is not a failure either. The picker opens
// on a screen that says where it looked, and quits cleanly — telling someone
// who has just installed the CLI that extendo has never run is an answer, not
// an error.
func Run(s *store.Store, opts Options) (*Selected, error) {
	picker := newModel(s, opts)
	if err := picker.startupError(); err != nil {
		return nil, err
	}

	final, err := tea.NewProgram(picker, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, fmt.Errorf("running picker: %w", err)
	}

	finished, _ := final.(model)

	return finished.selected, nil
}

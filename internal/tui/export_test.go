package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tjq/extendo-cli/internal/store"
)

// NewModel builds a picker without starting a program, so tests can drive it
// through teatest instead of a real terminal.
func NewModel(s *store.Store, opts Options) tea.Model {
	return newModel(s, opts)
}

// SelectionOf reports what Run would hand back for a finished model.
func SelectionOf(m tea.Model) *Selected {
	picker, _ := m.(model)

	return picker.selected
}

// StartupError reports the read failure that would stop Run from opening this
// picker. A history file that is merely absent is not one of them.
func StartupError(m tea.Model) error {
	picker, _ := m.(model)

	return picker.startupError()
}

package tui

import (
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/tjq/extendo-cli/internal/store"
)

// searchPrompt labels the footer's field with the same mark the header hints
// at, so the two read as one feature.
const searchPrompt = "⌕ "

// newSearchInput builds the footer's query field.
//
// The cursor is static rather than blinking. A blink is a timer, and a timer in
// a picker that is usually open for two keystrokes buys nothing but a stream of
// redraws — and a frame that renders differently depending on when it was
// captured.
func newSearchInput() textinput.Model {
	input := textinput.New()
	input.Prompt = searchPrompt
	input.Cursor.SetMode(cursor.CursorStatic)

	return input
}

// searchWidth is how much of the footer line the field may fill. It leaves the
// prompt its cells and one more for the cursor, which textinput draws past the
// end of the value.
func (m model) searchWidth() int {
	return max(m.bodyWidth()-lipgloss.Width(searchPrompt)-1, 8)
}

func (m model) query() string {
	return m.search.Value()
}

// isFiltered reports whether the list is showing a subset. It is independent of
// isSearching: ⏎ hands the keyboard back to the list and leaves the query in
// place.
func (m model) isFiltered() bool {
	return m.query() != ""
}

// openSearch gives the query field the keyboard.
func (m model) openSearch() (tea.Model, tea.Cmd) {
	m.isSearching = true

	return m, m.search.Focus()
}

// handleSearchKey routes a key press while the query field has the keyboard.
// Every rune belongs to the query — including the digits, which is why the
// number row only copies once ⏎ has handed focus back to the list.
func (m model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.isSearching = false
		m.search.Blur()

		return m, nil
	case tea.KeyEsc:
		return m.clearSearch(), nil
	}

	before := m.query()

	input, cmd := m.search.Update(msg)
	m.search = input

	if m.query() == before {
		return m, cmd
	}

	return m.applyFilter(), cmd
}

// clearSearch drops the query and the focus together, leaving the whole list on
// screen.
func (m model) clearSearch() model {
	m.isSearching = false
	m.search.Blur()
	m.search.SetValue("")

	return m.applyFilter()
}

// applyFilter re-runs the query and parks the cursor on the first match. The
// rows under it have just changed, so keeping the old index would leave the
// highlight on an item the user never chose.
func (m model) applyFilter() model {
	m = m.refilter()
	m.cursor = 0
	m.page = 0
	m.revealed = ""

	return m
}

// refilter recomputes the visible rows from the query, leaving the cursor to
// the caller.
//
// Matching runs against the text each row displays, which for a credential is
// the masked label: a query drawn from the hidden part of a value must not be
// able to find — or confirm — the item it belongs to.
//
// Matches keep the list's own order, pinned first and then newest, rather than
// being re-ranked by score. The order is the one the user was just looking at,
// and the number keys make it load-bearing: narrowing a list should not also
// shuffle it.
func (m model) refilter() model {
	if !m.isFiltered() {
		m.visible = m.items

		return m
	}

	haystack := make([]string, 0, len(m.items))
	for _, it := range m.items {
		haystack = append(haystack, m.descriptions[it.ID].masked)
	}

	matches := fuzzy.FindNoSort(m.query(), haystack)

	visible := make([]store.Item, 0, len(matches))
	for _, match := range matches {
		visible = append(visible, m.items[match.Index])
	}

	m.visible = visible

	return m
}

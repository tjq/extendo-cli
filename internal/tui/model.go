package tui

import (
	"errors"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tjq/extendo-cli/internal/secrets"
	"github.com/tjq/extendo-cli/internal/store"
)

// perPage is fixed by the number row: ten items, labelled 1..9 then 0.
const perPage = 10

// defaultWidth and defaultHeight stand in until the first tea.WindowSizeMsg
// lands, and are the frame the layout was drawn against.
const (
	defaultWidth  = 72
	defaultHeight = 24
)

// model is the picker's whole state. Bubble Tea copies it on every message, so
// every mutating helper takes and returns a value rather than a pointer.
type model struct {
	store *store.Store
	items []store.Item // always store.Sorted: pinned first, then newest
	// visible is what the number row, the cursor and the pager all count: every
	// item, or the subset a search query matched. See search.go.
	visible []store.Item
	page    int // zero-based; ten items each
	cursor  int // index into visible, never a per-page offset
	// revealed is the ID whose credential is shown in full rather than masked
	// ("" = none). Moving the cursor clears it, so a secret never stays bare on
	// screen after the user has looked away from it.
	revealed string
	// descriptions holds each item's rendered text, keyed by ID. See description.
	descriptions map[string]description
	// search is the footer's query field, and isSearching is true only while it
	// has the keyboard. The filter outlives the focus: ⏎ hands the number row
	// back to the list without dropping the query.
	search      textinput.Model
	isSearching bool
	// isPreview swaps the list for one item's full contents. Any key returns.
	isPreview bool
	ascii     bool
	nerd      bool
	now       func() time.Time
	width     int
	// height is what the preview wraps and crops its body against.
	height int
	// selected is set by a number key and makes the program quit. The picker
	// never copies: see Run.
	selected *Selected
	// err holds the last store failure, which the footer shows. It is not
	// returned to the caller — the user has already seen it.
	err error
	// loadErr is what reading the history at startup produced. A missing file
	// gets a screen rather than an error; anything else stops Run.
	loadErr error
}

// newModel builds the picker and reads the history it will show.
//
// A history file that is not there is not a failure: extendo may simply never
// have run, and the picker answers that with a screen naming the path it looked
// at. Every other read failure is left in loadErr for Run to return instead of
// opening a program over an empty list.
func newModel(s *store.Store, opts Options) model {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	m := model{
		store:  s,
		search: newSearchInput(),
		ascii:  opts.ASCII,
		nerd:   opts.Nerd,
		now:    opts.Now,
		width:  defaultWidth,
		height: defaultHeight,
	}
	m.search.Width = m.searchWidth()

	items, err := s.Load()
	if err != nil {
		m.loadErr = err

		return m
	}

	m.items = store.Sorted(items)
	m.visible = m.items
	m.descriptions = describeAll(s, m.items)

	return m
}

// isMissing reports whether the history file is absent, which is a designed
// screen rather than an error.
func (m model) isMissing() bool {
	return errors.Is(m.loadErr, store.ErrNoStore)
}

// startupError is the read failure that should stop the picker from opening.
func (m model) startupError() error {
	if m.isMissing() {
		return nil
	}

	return m.loadErr
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width == m.width && msg.Height == m.height {
			return m, nil
		}

		m.width, m.height = msg.Width, msg.Height
		m.search.Width = m.searchWidth()

		// Bubble Tea already drops its render cache on a resize, but that only
		// makes it redraw every line of the *new* frame. It does not erase what
		// the old one left behind: a frame drawn for a wider terminal wraps when
		// the terminal narrows, so it occupies more rows than the replacement
		// paints over, and the wrapped tail survives below the new frame. Wiping
		// the screen first is what keeps a shrink from leaving debris.
		return m, tea.ClearScreen
	case tea.KeyMsg:
		return m.handleKey(msg)
	default:
		return m, nil
	}
}

// handleKey routes a key press to whichever plane owns it.
//
// ctrl+c is answered before anything else. "Any key leaves the preview" and
// "every rune goes into the query" are both true of the screens below, and a
// user who wants out of the program should not have to find their way back to
// the list first.
func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch {
	case key == "ctrl+c":
		return m, tea.Quit
	case m.isMissing():
		return m.handleMissingKey(key)
	case m.isPreview:
		m.isPreview = false

		return m, nil
	case m.isSearching:
		return m.handleSearchKey(msg)
	default:
		return m.handleListKey(key)
	}
}

// handleMissingKey answers the no-store screen, which has no rows to act on.
func (m model) handleMissingKey(key string) (tea.Model, tea.Cmd) {
	if key == "q" || key == "esc" {
		return m, tea.Quit
	}

	return m, nil
}

// handleListKey routes a key press on the list. The number row is a plane of
// its own: it acts on the row it names regardless of where the cursor sits, so
// the common case — glance, press one key, done — never involves moving
// anything.
func (m model) handleListKey(key string) (tea.Model, tea.Cmd) {
	if position, ok := rowPosition(key); ok {
		return m.selectRow(m.page*perPage + position)
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "c":
		// The number row's counterpart for the cursor: same copy, but aimed by
		// the keys that moved there rather than by reading a label off a row.
		return m.selectRow(m.cursor)
	case "esc":
		// A filter is the nearest thing to leave, and leaving it is what a user
		// who has narrowed the list to nothing wants. Only an unfiltered list
		// takes esc as "quit".
		if m.isFiltered() {
			return m.clearSearch(), nil
		}

		return m, tea.Quit
	case "up", "k":
		return m.moveCursor(-1), nil
	case "down", "j":
		return m.moveCursor(1), nil
	case "left", "h":
		return m.flipPage(-1), nil
	case "right", "l":
		return m.flipPage(1), nil
	case "/":
		return m.openSearch()
	case "s":
		return m.toggleReveal(), nil
	case "enter", "tab":
		return m.openPreview(), nil
	case "p":
		return m.togglePin(), nil
	case "d":
		return m.deleteItem(), nil
	default:
		return m, nil
	}
}

// rowPosition maps a number key to a row's position on the page: 1..9 count
// from the top and 0 is the tenth row, matching the labels the rows carry.
func rowPosition(key string) (int, bool) {
	if len(key) != 1 || key[0] < '0' || key[0] > '9' {
		return 0, false
	}

	if key[0] == '0' {
		return perPage - 1, true
	}

	return int(key[0] - '1'), true
}

// selectRow records the caller's prize and quits. An index past the end of a
// short final page is a no-op, not an error: the row is blank on screen.
func (m model) selectRow(index int) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.visible) {
		return m, nil
	}

	it := m.visible[index]
	_, safe, _ := m.describe(it)
	m.selected = &Selected{Item: it, Label: safe}

	return m, tea.Quit
}

// moveCursor walks the list one row at a time, crossing page boundaries. It
// stops at both ends rather than wrapping, so holding a key cannot silently
// carry the cursor from the newest item to the oldest.
func (m model) moveCursor(delta int) model {
	next := m.cursor + delta
	if next < 0 || next >= len(m.visible) {
		return m
	}

	m.cursor = next
	m.page = next / perPage
	m.revealed = ""

	return m
}

// flipPage moves a whole page and parks the cursor on its first row.
func (m model) flipPage(delta int) model {
	next := m.page + delta
	if next < 0 || next >= m.pageCount() {
		return m
	}

	m.page = next
	m.cursor = next * perPage
	m.revealed = ""

	return m
}

// pageCount is at least one, so an empty history still has a page to draw.
func (m model) pageCount() int {
	if len(m.visible) == 0 {
		return 1
	}

	return (len(m.visible) + perPage - 1) / perPage
}

func (m model) currentItem() (store.Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return store.Item{}, false
	}

	return m.visible[m.cursor], true
}

// toggleReveal shows the highlighted credential in full, and hides it again on
// a second press. Only a row that is actually masked has anything to reveal.
func (m model) toggleReveal() model {
	it, ok := m.currentItem()
	if !ok || !m.descriptions[it.ID].isSecret {
		return m
	}

	if m.revealed == it.ID {
		m.revealed = ""

		return m
	}

	m.revealed = it.ID

	return m
}

// openPreview swaps to the full-screen view of the highlighted item. An empty
// list has nothing to show, so the key does nothing there.
func (m model) openPreview() model {
	if _, ok := m.currentItem(); !ok {
		return m
	}

	m.isPreview = true

	return m
}

func (m model) togglePin() model {
	it, ok := m.currentItem()
	if !ok {
		return m
	}

	if _, err := m.store.TogglePin(it.ID); err != nil {
		m.err = err

		return m
	}

	return m.reload()
}

func (m model) deleteItem() model {
	it, ok := m.currentItem()
	if !ok {
		return m
	}

	if err := m.store.Delete(it.ID); err != nil {
		m.err = err

		return m
	}

	return m.reload()
}

// reload re-reads the history after a mutation rather than patching the slice
// in memory. The macOS app owns the same file and may have captured or trimmed
// items while the picker was open, and re-sorting is the only way a freshly
// pinned item lands where the popup would put it.
//
// The cursor keeps its index and is pulled back inside the list, so it stays
// where the user left it instead of jumping to whichever row the mutation moved.
func (m model) reload() model {
	items, err := m.store.Load()
	if err != nil {
		m.err = err

		return m
	}

	m.err = nil
	m.items = store.Sorted(items)
	m.descriptions = describeAll(m.store, m.items)
	m.revealed = ""
	m = m.refilter()
	m.cursor = min(m.cursor, max(len(m.visible)-1, 0))
	m.page = m.cursor / perPage

	return m
}

// description is everything the view needs to write one item's text. It is
// built when the history loads rather than per frame: labelling an image means
// reading and decoding its blob, and a page of screenshots would otherwise be
// re-read from disk on every keystroke.
type description struct {
	// masked is the row's text; for a credential it is a preview with the
	// value hidden. full is the same row once revealed.
	masked string
	full   string
	// safe names the item after the picker exits. For a credential it is the
	// category alone — a confirmation line ends up in scrollback.
	safe     string
	isSecret bool
}

// describe returns an item's text and its safe name, honouring reveal.
func (m model) describe(it store.Item) (row, safe string, isSecret bool) {
	desc := m.descriptions[it.ID]
	if m.revealed == it.ID {
		return desc.full, desc.safe, desc.isSecret
	}

	return desc.masked, desc.safe, desc.isSecret
}

func describeAll(s *store.Store, items []store.Item) map[string]description {
	out := make(map[string]description, len(items))
	for _, it := range items {
		out[it.ID] = describeItem(s, it)
	}

	return out
}

// describeItem classifies textual items only. An image or file item can carry
// an incidental text representation, but that text is not what the row
// displays, so matching a secret in it would replace an accurate label with a
// wrong one.
func describeItem(s *store.Store, it store.Item) description {
	label := s.DisplayLabel(it)
	plain := description{masked: label, full: label, safe: label}

	isTextual := it.Kind() == store.KindText || it.Kind() == store.KindRichText
	if !isTextual {
		return plain
	}

	text, ok := s.PlainText(it)
	if !ok {
		return plain
	}

	category, ok := secrets.Classify(text)
	if !ok {
		return plain
	}

	// Classification reads the text as captured; the mask is built from a
	// sanitized copy. Mask keeps the first few runes of the first line verbatim,
	// which for text that opens with an escape sequence would carry it into
	// every row and preview header the item appears in.
	return description{
		masked:   category.Label() + " · " + secrets.Mask(store.Printable(text)),
		full:     category.Label() + " · " + label,
		safe:     category.Label(),
		isSecret: true,
	}
}

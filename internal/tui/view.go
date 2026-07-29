package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	// minWidth is the narrowest frame the layout was designed for. Below it the
	// picker keeps drawing at this width and lets the terminal wrap, which is
	// uglier than reflowing but never silently drops a column.
	minWidth = 56
	// maxDots is how many page dots stay countable at a glance. Past it the
	// pager switches to a compact "p 12/34".
	maxDots = 10
	// queryHintWidth keeps a long query from crowding out the header's mark.
	queryHintWidth = 24

	// Fixed column widths, in terminal cells. The text column takes whatever is
	// left over.
	cursorWidth = 1
	labelWidth  = 1
	ageWidth    = 4

	// The blank columns between the frame's borders and what sits inside them.
	// The right gutter is the wider of the two because the pin lands in the last
	// column of every pinned row, and a mark one cell off the border reads as
	// having overflowed it.
	gutterLeft  = 1
	gutterRight = 2

	gap      = "  "
	ellipsis = "…"

	dotCurrent = "●"
	dotOther   = "○"
)

// The verbs the footer lists are the approved mockup's.
const (
	helpMove   = "1-0/c copy · ↑↓ move · ←→ page"
	helpVerbs  = "p pin · d del · s reveal · ⏎ preview · q quit"
	helpSearch = "⏎ keep · esc clear · ctrl+c quit"
	helpQuit   = "q quit"
)

// The two screens that hold no rows say so in full sentences. Neither is an
// error: one means the user has copied nothing yet, the other that extendo has
// never run on this machine.
const (
	emptyHistory = "nothing copied yet — go copy something wonderful"
	emptyMatches = "no match"
	noStoreTitle = "extendo history not found"
	noStoreHint  = "is extendo installed?"
)

// View draws whichever screen the picker is on.
func (m model) View() string {
	st := newStyles()
	body := m.bodyWidth()

	switch {
	case m.isMissing():
		return m.missingView(st, body)
	case m.isPreview:
		return m.previewView(st, body)
	default:
		return m.listView(st, body)
	}
}

// listView is the picker proper: a rounded frame around a header, one page of
// rows, and the footer.
func (m model) listView(st styles, body int) string {
	return frameBox(st, body,
		plainLines([]string{m.header(st, body)}),
		m.rows(st, body),
		plainLines(m.footer(st, body)))
}

// missingView stands in for the list when there is no history file to read. It
// names the path that was tried, because the one thing the user can act on is
// whether that is where extendo actually keeps its data.
func (m model) missingView(st styles, body int) string {
	rows := []string{
		"",
		st.dim.Render(centre(noStoreTitle, body)),
		centre(Truncate(m.store.HistoryPath(), body), body),
		"",
		st.dim.Render(centre(noStoreHint, body)),
		"",
	}

	return frameBox(st, body,
		plainLines([]string{m.brand(st)}),
		plainLines(rows),
		plainLines([]string{spread("", st.dim.Render(helpQuit), body)}),
	)
}

// bodyLine is one line of the frame's inside.
//
// The fill is kept apart from the text because of the cursor's row. Its
// highlight has to reach both borders, and the text stops well short of the
// right one, by the width of the gutter. Styling the text alone would draw
// a bar that stopped beside the pin and left the gutter dark, which reads as a
// highlight that came up short rather than as a margin.
type bodyLine struct {
	text string
	// fill paints the whole inside of the frame, gutters included. Text handed
	// to a filled line must be unstyled, or the two sets of escapes fight.
	fill lipgloss.Style
	// hasFill distinguishes no fill from the zero Style, which is a real style
	// that happens to add nothing.
	hasFill bool
}

func plainLine(text string) bodyLine {
	return bodyLine{text: text}
}

func plainLines(texts []string) []bodyLine {
	lines := make([]bodyLine, 0, len(texts))
	for _, text := range texts {
		lines = append(lines, plainLine(text))
	}

	return lines
}

// frameBox draws the picker's box: a header block, a body block and a footer
// block, each fenced off by a rule. Every screen is one of these, so the border
// arithmetic lives in one place.
func frameBox(st styles, body int, header, rows, footer []bodyLine) string {
	lines := make([]string, 0, len(header)+len(rows)+len(footer)+4)
	lines = append(lines, rule(st, frame.TopLeft, frame.TopRight, body))

	for _, line := range header {
		lines = append(lines, content(st, line, body))
	}

	lines = append(lines, rule(st, frame.MiddleLeft, frame.MiddleRight, body))

	for _, line := range rows {
		lines = append(lines, content(st, line, body))
	}

	lines = append(lines, rule(st, frame.MiddleLeft, frame.MiddleRight, body))

	for _, line := range footer {
		lines = append(lines, content(st, line, body))
	}

	lines = append(lines, rule(st, frame.BottomLeft, frame.BottomRight, body))

	return strings.Join(lines, "\n")
}

// bodyWidth is the room inside the frame, discounting the two border columns
// and the gutter on each side.
func (m model) bodyWidth() int {
	return max(m.width, minWidth) - 2 - gutterLeft - gutterRight
}

// rule draws one of the frame's horizontal lines. It spans the gutters as well
// as the body, so the corners sit above and below the side borders.
func rule(st styles, left, right string, body int) string {
	return st.dim.Render(left + strings.Repeat(frame.Top, body+gutterLeft+gutterRight) + right)
}

// content wraps one body line in the frame's side borders, padding it out to
// the full width so that the right border stays in its column. Lines arrive
// styled, so the padding is added rather than the line being reflowed.
func content(st styles, line bodyLine, body int) string {
	inside := strings.Repeat(" ", gutterLeft) + pad(line.text, body) + strings.Repeat(" ", gutterRight)
	if line.hasFill {
		inside = line.fill.Render(inside)
	}

	return st.dim.Render(frame.Left) + inside + st.dim.Render(frame.Right)
}

// header shows the app mark on the left, the item count and the search hint on
// the right.
func (m model) header(st styles, body int) string {
	return spread(m.brand(st), st.dim.Render(m.countHint()), body)
}

func (m model) brand(st styles) string {
	return st.accent.Render(dotCurrent) + " " + st.title.Render("extendo")
}

// countHint is the header's right-hand text: how much is on screen, and either
// how to narrow it or what it is currently narrowed by. While the query field
// has focus the query is already on screen, so the header does not repeat it.
func (m model) countHint() string {
	if !m.isFiltered() {
		return fmt.Sprintf("%d %s · ⌕ /", len(m.items), plural(len(m.items), "item"))
	}

	shown := fmt.Sprintf("%d of %d", len(m.visible), len(m.items))
	if m.isSearching {
		return shown
	}

	return shown + " · " + searchPrompt + Truncate(m.query(), queryHintWidth)
}

func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}

	return noun + "s"
}

// rows renders one screenful. Every page is as tall as the fullest one — a
// short final page is padded with blanks — so the frame does not jump height
// while paging. A history shorter than a page keeps the frame short.
func (m model) rows(st styles, body int) []bodyLine {
	switch {
	case len(m.items) == 0:
		return notice(st, emptyHistory, body)
	case len(m.visible) == 0:
		return notice(st, emptyMatches+" for "+Truncate(m.query(), queryHintWidth), body)
	}

	count := min(len(m.visible), perPage)
	out := make([]bodyLine, 0, count)

	for position := range count {
		index := m.page*perPage + position
		if index >= len(m.visible) {
			out = append(out, plainLine(""))

			continue
		}

		out = append(out, m.row(st, index, position, body))
	}

	return out
}

// notice is what fills the row block when there are no rows: one dim line, held
// off the rules above and below it so it reads as a state rather than as an
// item.
func notice(st styles, text string, body int) []bodyLine {
	return plainLines([]string{"", st.dim.Render(centre(text, body)), ""})
}

// cells are one row's columns, each already padded to its width. Padding
// happens before colouring so the columns line up whether or not a cell carries
// a style, and so the cursor bar can be painted over the row as a single span —
// a background cannot be laid across segments that already reset the colour.
type cells struct {
	cursor, label, icon, text, age, pin string
}

func (c cells) plain() string {
	return c.cursor + c.label + gap + c.icon + gap + c.text + " " + c.age + gap + c.pin
}

func (c cells) styled(st styles) string {
	return st.accent.Render(c.cursor) + st.dim.Render(c.label) + gap +
		st.dim.Render(c.icon) + gap + c.text + " " +
		st.dim.Render(c.age) + gap + st.accent.Render(c.pin)
}

// row renders one list line: number label, kind icon, text, age, pin mark.
func (m model) row(st styles, index, position, body int) bodyLine {
	glyphs := m.glyphs()
	it := m.visible[index]
	text, _, isSecret := m.describe(it)

	icon := glyphs.forKind(it.Kind())
	if isSecret {
		icon = glyphs.Secret
	}

	pin := ""
	if it.IsPinned {
		pin = glyphs.Pin
	}

	cursor := " "
	if index == m.cursor {
		cursor = glyphs.Cursor
	}

	row := cells{
		cursor: cursor,
		label:  strconv.Itoa((position + 1) % perPage),
		icon:   fit(icon, glyphs.iconWidth()),
		text:   fit(text, m.textWidth(body)),
		age:    alignRight(Rel(it.CreatedAt, m.now()), ageWidth),
		pin:    fit(pin, lipgloss.Width(glyphs.Pin)),
	}

	if index == m.cursor {
		// Unstyled text plus a fill, so the highlight is painted once across the
		// whole inside of the frame rather than stopping where the pin does.
		return bodyLine{text: row.plain(), fill: st.cursor, hasFill: true}
	}

	return plainLine(row.styled(st))
}

// textWidth is what the label column gets once every fixed column and the
// single spaces between them are accounted for.
func (m model) textWidth(body int) int {
	glyphs := m.glyphs()
	fixed := cursorWidth + labelWidth + len(gap) +
		glyphs.iconWidth() + len(gap) +
		1 + ageWidth + len(gap) + lipgloss.Width(glyphs.Pin)

	return max(body-fixed, 0)
}

func (m model) glyphs() glyphSet {
	switch {
	case m.ascii:
		return ascii
	case m.nerd:
		return nerd
	default:
		return standard
	}
}

// footer draws the pager and the help lines. A store failure takes the second
// line's left half, where it sits until the next mutation succeeds.
func (m model) footer(st styles, body int) []string {
	left := ""
	if m.err != nil {
		left = st.dim.Render("! " + fit(m.err.Error(), body/2))
	}

	if m.isSearching {
		return []string{
			pad(m.search.View(), body),
			spread(left, st.dim.Render(helpSearch), body),
		}
	}

	return []string{
		spread(m.pager(st), st.dim.Render(helpMove), body),
		spread(left, st.dim.Render(helpVerbs), body),
	}
}

// pager renders the page indicator: one dot per page while they stay
// countable, and a compact count once they do not.
func (m model) pager(st styles) string {
	pages := m.pageCount()
	if pages > maxDots {
		return st.dim.Render(fmt.Sprintf("p %d/%d", m.page+1, pages))
	}

	dots := make([]string, 0, pages)

	for i := range pages {
		if i == m.page {
			dots = append(dots, st.accent.Render(dotCurrent))

			continue
		}

		dots = append(dots, st.dim.Render(dotOther))
	}

	return strings.Join(dots, " ") + st.dim.Render(fmt.Sprintf("   page %d/%d", m.page+1, pages))
}

// spread pushes left against the frame's left edge and right against its right.
//
// Neither side is truncated. Both may already carry styles, and cutting an
// escape sequence in half would bleed colour across the rest of the frame; a
// terminal too narrow to hold both gets one overlong line instead.
func spread(left, right string, width int) string {
	middle := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)

	return left + strings.Repeat(" ", middle) + right
}

// fit sizes unstyled text to exactly width cells, padding or truncating. Width
// is counted in terminal cells, so a double-width glyph costs two.
func fit(text string, width int) string {
	if width <= 0 {
		return ""
	}

	text = Truncate(text, width)

	return text + strings.Repeat(" ", width-lipgloss.Width(text))
}

// centre sizes unstyled text to exactly width cells with the padding split
// either side, leaning left when it cannot be split evenly.
func centre(text string, width int) string {
	text = Truncate(text, width)
	left := max((width-lipgloss.Width(text))/2, 0)

	return strings.Repeat(" ", left) + fit(text, width-left)
}

// pad extends already-styled text to width cells. Unlike fit it never
// truncates: the text may carry escape sequences, and cutting one in half would
// bleed colour across the rest of the frame.
func pad(text string, width int) string {
	gap := width - lipgloss.Width(text)
	if gap <= 0 {
		return text
	}

	return text + strings.Repeat(" ", gap)
}

func alignRight(text string, width int) string {
	gap := width - lipgloss.Width(text)
	if gap <= 0 {
		return text
	}

	return strings.Repeat(" ", gap) + text
}

// Truncate shortens text to at most width cells, marking the cut with an
// ellipsis. It measures rune by rune rather than by byte count, so a label full
// of CJK or emoji is cut where it actually reaches the column's edge.
//
// It is exported because `ext list` bounds its LABEL column the same way the
// picker bounds its rows, and one item long enough to need cutting should not
// be cut two different ways depending on which one drew it.
func Truncate(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}

	limit := width - lipgloss.Width(ellipsis)
	kept := strings.Builder{}
	used := 0

	for _, r := range text {
		size := lipgloss.Width(string(r))
		if used+size > limit {
			break
		}

		kept.WriteRune(r)
		used += size
	}

	return kept.String() + ellipsis
}

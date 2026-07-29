package tui

import "github.com/charmbracelet/lipgloss"

// The palette is deliberately two colours. One accent carries the four things a
// user's eye needs to find — the cursor bar, the current page dot, pin marks
// and the header bullet — and everything else is chrome in a single dim tone.
// Adding a third colour would cost the accent its meaning.
//
// lipgloss picks the light or dark variant from the terminal's background and
// drops colour entirely under NO_COLOR, so nothing here has to check.
var (
	accentColor = lipgloss.AdaptiveColor{Light: "#6B5BD2", Dark: "#9D8CFF"}
	chromeColor = lipgloss.AdaptiveColor{Light: "#9A9A9A", Dark: "#5C5C64"}
	// onAccentColor keeps the cursor row legible against the accent bar, which
	// is dark on a light terminal and light on a dark one.
	onAccentColor = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1C1B22"}
)

// frame supplies the runes the picker's border is drawn from. The box is
// assembled by hand rather than with lipgloss's Border decorator because the
// layout needs the two interior rules, which a decorator cannot draw.
var frame = lipgloss.RoundedBorder()

// styles groups every style the view applies, so the palette above is the only
// place a colour is named.
type styles struct {
	accent lipgloss.Style
	dim    lipgloss.Style
	cursor lipgloss.Style
	title  lipgloss.Style
}

func newStyles() styles {
	return styles{
		accent: lipgloss.NewStyle().Foreground(accentColor),
		dim:    lipgloss.NewStyle().Foreground(chromeColor),
		cursor: lipgloss.NewStyle().Foreground(onAccentColor).Background(accentColor),
		title:  lipgloss.NewStyle().Bold(true),
	}
}

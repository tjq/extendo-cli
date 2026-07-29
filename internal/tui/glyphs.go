package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/tjq/extendo-cli/internal/store"
)

// glyphSet is the icon vocabulary one terminal can render.
//
// Only the marks that need a patched font or emoji support live here. The
// frame, the header bullet and the page dots are drawn with box-drawing and
// geometric characters that any modern font carries, so they do not vary.
type glyphSet struct{ Text, Rich, Image, File, Other, Secret, Pin, Cursor string }

var (
	// standard is the default: assigned Unicode characters that any font
	// carrying a reasonable symbol range already has, so nothing needs
	// installing.
	//
	// Every one of them is picked on measured width, not on looks. A character
	// whose East Asian Width is Ambiguous is one cell to some terminals and two
	// to others, and a column budgeted at the wrong one drags the frame's right
	// border out of true — so these are all unambiguously single-cell, which
	// TestGlyphWidthsAreUnambiguous holds them to. That rules out a good many
	// prettier candidates: ▪ and ⚑ are Ambiguous, and anything the emoji tables
	// know about is liable to be drawn double-width whatever its width class
	// says.
	standard = glyphSet{
		Text:   "⍞", // U+235E quad quote — quoted text
		Rich:   "⌸", // U+2338 quad equal — text with formatting
		Image:  "◰", // U+25F0 square with a filled quadrant
		File:   "▢", // U+25A2 rounded square — a document
		Other:  "⍰", // U+2370 quad question
		Secret: "⊛", // U+229B circled asterisk — a masked value
		Pin:    "✦", // U+2726 four pointed star
		Cursor: "▸", // U+25B8
	}
	// nerd is the Nerd Font set, selected by --nerd. Its icons live in the
	// private use area, which has no assigned width and no fallback font: a
	// terminal without a patched font draws tofu, and draws it at whatever
	// width it likes. They are spelled as escapes because they render as tofu
	// in any editor without a patched font, which is where they get misread.
	nerd = glyphSet{
		Text:   "\U000F018F", // md-clipboard_text
		Rich:   "\U000F0284", // md-format_text
		Image:  "\U000F02E9", // md-image
		File:   "\U000F0219", // md-file_document
		Other:  "\U000F0613", // md-help_circle_outline
		Secret: "\U0001F512", // 🔒
		Pin:    "\U000F0403", // md-pin
		Cursor: "▸",
	}
	// ascii is the last resort for a font with no symbol coverage at all,
	// selected by --ascii.
	ascii = glyphSet{
		Text: "txt", Rich: "rtf", Image: "img", File: "fil",
		Other: "???", Secret: "SEC", Pin: "*", Cursor: ">",
	}
)

// forKind returns the icon an item shows in the glyph column.
func (g glyphSet) forKind(kind store.Kind) string {
	switch kind {
	case store.KindText:
		return g.Text
	case store.KindRichText:
		return g.Rich
	case store.KindImage:
		return g.Image
	case store.KindFile:
		return g.File
	default:
		return g.Other
	}
}

// iconWidth is how many cells the glyph column needs to hold any icon in the
// set. The set mixes widths — a Nerd Font icon is one cell, the lock emoji two,
// an ASCII stand-in three — so the column is sized once rather than per row.
func (g glyphSet) iconWidth() int {
	width := 0
	for _, icon := range []string{g.Text, g.Rich, g.Image, g.File, g.Other, g.Secret} {
		width = max(width, lipgloss.Width(icon))
	}

	return width
}

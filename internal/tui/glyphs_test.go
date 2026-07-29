package tui

import (
	"testing"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// TestStandardGlyphsAreAssignedAndSingleCell is the guard on the default glyph
// set, and it holds it to the two properties the Nerd Font icons lacked.
//
// Assigned, first. A private use codepoint means whatever the installed font
// says it means, and no font outside the Nerd Font patch set maps them at all,
// so a terminal without one draws the missing-glyph box — at a width of its own
// choosing, since private use has no assigned East Asian Width either. The
// layout budgeted one cell, terminals drew two, and every row's right border
// was pushed a cell past the rules above and below it.
//
// Single cell, second: that is what the column arithmetic in view.go assumes.
//
// What this cannot promise is a terminal running in CJK mode, where Ambiguous
// characters are drawn double-width. The frame's own box-drawing characters are
// Ambiguous, so such a terminal doubles the whole frame and no choice of glyph
// here would save it. The set is picked from unambiguous characters anyway,
// which costs nothing and is checked below.
func TestStandardGlyphsAreAssignedAndSingleCell(t *testing.T) {
	wide := &runewidth.Condition{EastAsianWidth: true}

	for _, g := range []struct{ name, glyph string }{
		{"Text", standard.Text},
		{"Rich", standard.Rich},
		{"Image", standard.Image},
		{"File", standard.File},
		{"Other", standard.Other},
		{"Secret", standard.Secret},
		{"Pin", standard.Pin},
		{"Cursor", standard.Cursor},
	} {
		for _, r := range g.glyph {
			if unicode.In(r, unicode.Co) {
				t.Errorf("%s glyph %q is private use (U+%04X): no font outside a Nerd Font "+
					"patch maps it, and it has no assigned width", g.name, g.glyph, r)
			}
		}

		width := runewidth.StringWidth(g.glyph)
		if width != 1 {
			t.Errorf("%s glyph %q is %d cells, expected 1", g.name, g.glyph, width)
		}

		if ambiguous := wide.StringWidth(g.glyph); ambiguous != width {
			t.Errorf("%s glyph %q is %d cells normally but %d with ambiguous-as-wide; "+
				"pick an unambiguous character instead", g.name, g.glyph, width, ambiguous)
		}
	}
}

// TestGlyphColumnsAreSizedForTheSet holds the icon column to the widest icon it
// has to hold. The pin column has no such slack — it holds one glyph — which is
// why the pin was where the Nerd Font width mismatch first showed.
func TestGlyphColumnsAreSizedForTheSet(t *testing.T) {
	for _, set := range []struct {
		name   string
		glyphs glyphSet
	}{{"standard", standard}, {"ascii", ascii}} {
		width := set.glyphs.iconWidth()

		for _, icon := range []string{
			set.glyphs.Text, set.glyphs.Rich, set.glyphs.Image,
			set.glyphs.File, set.glyphs.Other, set.glyphs.Secret,
		} {
			if got := runewidth.StringWidth(icon); got > width {
				t.Errorf("%s: icon %q needs %d cells, column is %d", set.name, icon, got, width)
			}
		}
	}
}

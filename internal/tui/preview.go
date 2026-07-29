package tui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tjq/extendo-cli/internal/store"
)

const (
	// previewChrome is how many lines the preview spends on its frame: three
	// rules, two header lines, one footer line and the bottom border.
	previewChrome = 7
	// thumbCols caps the thumbnail's width. A half-block cell is roughly twice
	// as tall as it is wide, so a picture drawn edge to edge across a wide
	// terminal would be a stripe.
	thumbCols = 48

	helpPreview = "any key returns"
	// revealHint tells the reader why a preview is showing a mask, and where the
	// key that lifts it lives — `s` in the preview returns to the list, like
	// every other key.
	revealHint = "hidden — press s on the row to reveal"
)

// previewView draws one item's full contents in place of the list.
func (m model) previewView(st styles, body int) string {
	it, ok := m.currentItem()
	if !ok {
		return m.listView(st, body)
	}

	return frameBox(st, body,
		plainLines(m.previewHeader(st, it, body)),
		plainLines(m.previewBody(st, it, body, max(m.height-previewChrome, 1))),
		plainLines([]string{spread("", st.dim.Render(helpPreview), body)}),
	)
}

// previewHeader names the item: what it is and how old, then where it came from.
func (m model) previewHeader(st styles, it store.Item, body int) []string {
	glyphs := m.glyphs()
	text, _, isSecret := m.describe(it)

	icon := glyphs.forKind(it.Kind())
	if isSecret {
		icon = glyphs.Secret
	}

	title := fit(icon, glyphs.iconWidth()) + " " +
		Truncate(text, max(body-glyphs.iconWidth()-ageWidth-2, 1))

	pin := ""
	if it.IsPinned {
		pin = st.accent.Render(glyphs.Pin) + st.dim.Render(" pinned")
	}

	return []string{
		spread(st.title.Render(title), st.dim.Render(Rel(it.CreatedAt, m.now())), body),
		spread(st.dim.Render(Truncate(it.SourceBundle, body)), pin, body),
	}
}

// previewBody renders the item's contents, cropped to the lines the window has
// room for. A clipboard entry can be a whole file, and a preview that outgrows
// the terminal would push its own frame off the top of the screen.
func (m model) previewBody(st styles, it store.Item, body, height int) []string {
	lines := m.previewLines(st, it, body)
	if len(lines) <= height {
		return lines
	}

	cropped := make([]string, 0, height)
	cropped = append(cropped, lines[:height-1]...)

	return append(cropped, st.dim.Render(centre(ellipsis, body)))
}

func (m model) previewLines(st styles, it store.Item, body int) []string {
	if it.Kind() == store.KindImage {
		return m.imageLines(it, body)
	}

	// A credential stays masked here for the same reason it does in the list:
	// the preview fills the screen, and filling the screen with someone's API
	// key is exactly what a shoulder-surfer is waiting for.
	desc := m.descriptions[it.ID]
	if desc.isSecret && m.revealed != it.ID {
		return []string{
			fit(desc.masked, body),
			"",
			st.dim.Render(fit(revealHint, body)),
		}
	}

	text, ok := m.store.PlainText(it)
	if !ok {
		return []string{fit(m.store.DisplayLabel(it), body)}
	}

	return wrap(text, body)
}

// imageLines describes the picture and then draws it.
func (m model) imageLines(it store.Item, body int) []string {
	info, ok := m.store.ImageInfo(it)
	if !ok {
		return []string{fit(m.store.DisplayLabel(it), body)}
	}

	bounds := info.Image.Bounds()
	meta := fmt.Sprintf("%d×%d · %s · %s",
		bounds.Dx(),
		bounds.Dy(),
		strings.ToUpper(info.Format),
		byteSize(info.Bytes),
	)

	// Two lines for the metadata and the blank under it, on top of the frame's
	// own, are what the thumbnail has to fit around.
	rows := max(m.height-previewChrome-2, 1)

	return append([]string{fit(meta, body), ""}, thumbnail(info.Image, min(body, thumbCols), rows)...)
}

// halfBlock fills the upper half of a cell, which lets one cell carry two
// stacked pixels: the upper one is the glyph's foreground, the lower one the
// background it sits on.
const halfBlock = "▀"

// thumbnail draws img as half-block cells, sampling nearest-neighbour rather
// than averaging — a clipboard thumbnail is a glance, not a print.
func thumbnail(img image.Image, maxCols, maxRows int) []string {
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || maxCols <= 0 || maxRows <= 0 {
		return nil
	}

	cols, rows := thumbSize(bounds.Dx(), bounds.Dy(), maxCols, maxRows)
	lines := make([]string, 0, rows)

	for row := range rows {
		line := strings.Builder{}

		for col := range cols {
			x := bounds.Min.X + col*bounds.Dx()/cols
			upper := bounds.Min.Y + 2*row*bounds.Dy()/(2*rows)
			lower := bounds.Min.Y + (2*row+1)*bounds.Dy()/(2*rows)

			line.WriteString(lipgloss.NewStyle().
				Foreground(cellColor(img.At(x, upper))).
				Background(cellColor(img.At(x, lower))).
				Render(halfBlock))
		}

		lines = append(lines, line.String())
	}

	return lines
}

// thumbSize fits an image into the box it is allowed, remembering that one cell
// is two pixels tall.
func thumbSize(width, height, maxCols, maxRows int) (cols, rows int) {
	cols = min(maxCols, width)
	rows = max(height*cols/(width*2), 1)

	if rows <= maxRows {
		return cols, rows
	}

	rows = maxRows

	return max(min(maxCols, width*rows*2/height), 1), rows
}

// cellColor converts a pixel to the colour lipgloss paints a cell with. Alpha
// is dropped: a terminal cell has no transparency, so a see-through pixel comes
// out as whatever it was composited against when it was captured.
func cellColor(c color.Color) lipgloss.Color {
	r, g, b, _ := c.RGBA()

	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8))
}

// byteSize renders a blob's size the way a file listing would.
func byteSize(bytes int) string {
	const unit = 1024

	switch {
	case bytes < unit:
		return fmt.Sprintf("%d B", bytes)
	case bytes < unit*unit:
		return fmt.Sprintf("%.1f KB", float64(bytes)/unit)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(unit*unit))
	}
}

// wrap reflows text to width cells, one entry per line.
//
// Carriage returns become newlines and tabs become spaces, so a clipboard entry
// captured on another operating system cannot push the frame's right border
// around. Everything else a terminal would act on instead of print is dropped
// by store.Printable: lipgloss measures an escape sequence as zero-width but
// passes it through, so without that step the preview would hand the terminal
// whatever the copied page felt like sending it.
func wrap(text string, width int) []string {
	clean := strings.ReplaceAll(text, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")

	return strings.Split(lipgloss.NewStyle().Width(width).Render(store.Printable(clean)), "\n")
}

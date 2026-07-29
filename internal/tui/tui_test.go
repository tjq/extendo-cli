package tui_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/muesli/termenv"

	"github.com/tjq/extendo-cli/internal/store"
	"github.com/tjq/extendo-cli/internal/tui"
)

// TestMain pins the colour profile to Ascii. lipgloss otherwise picks one from
// the environment, and a CI runner that advertises truecolor would write escape
// sequences into every golden frame.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)

	os.Exit(m.Run())
}

// refDate mirrors store's Foundation reference date, which the fixture's
// createdAt numbers count seconds from.
var refDate = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// fixedNow sits one hour after the newest fixture item, so every age cell in
// the goldens has exactly one right answer.
var fixedNow = refDate.Add(774303600 * time.Second)

const (
	idAAAA = "AAAAAAAA-1111-1111-1111-111111111111"
	idBBBB = "BBBBBBBB-2222-2222-2222-222222222222"
	idCCCC = "CCCCCCCC-3333-3333-3333-333333333333"
	idDDDD = "DDDDDDDD-4444-4444-4444-444444444444"
)

// termWidth and termHeight are the terminal every golden was captured at.
const (
	termWidth  = 72
	termHeight = 24
)

// fixtureDir copies the store package's sample history into a fresh temp
// directory and seeds the external blob item CCCC points at, so DisplayLabel
// can decode its dimensions.
func fixtureDir(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "store", "testdata", "history_sample.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "history.json"), data, 0o644); err != nil {
		t.Fatalf("write history.json: %v", err)
	}

	blobDir := filepath.Join(dir, "blobs", idCCCC)
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	buf := &bytes.Buffer{}
	if err := png.Encode(buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	if err := os.WriteFile(filepath.Join(blobDir, "rep-0.bin"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	return dir
}

// textDir writes a history of plain-text items, one per string, newest first.
func textDir(t *testing.T, texts ...string) string {
	t.Helper()

	items := make([]store.Item, 0, len(texts))

	for i, text := range texts {
		items = append(items, store.Item{
			ID:        fmt.Sprintf("%08d-0000-0000-0000-000000000000", i+1),
			CreatedAt: fixedNow.Add(-time.Duration(i+1) * time.Minute),
			Reps: []store.Representation{{
				Type:    "public.utf8-plain-text",
				Payload: store.Payload{Inline: []byte(text)},
			}},
		})
	}

	dir := t.TempDir()
	if err := store.Open(dir).Save(items); err != nil {
		t.Fatalf("Save: %v", err)
	}

	return dir
}

// start opens a picker over dir and returns it ready for key presses.
func start(t *testing.T, dir string) *teatest.TestModel {
	t.Helper()

	return startWith(t, dir, tui.Options{})
}

func startWith(t *testing.T, dir string, opts tui.Options) *teatest.TestModel {
	t.Helper()

	opts.Now = func() time.Time { return fixedNow }
	m := tui.NewModel(store.Open(dir), opts)

	return teatest.NewTestModel(t, m, teatest.WithInitialTermSize(termWidth, termHeight))
}

// clips builds n plain-text item bodies, newest first.
func clips(n int) []string {
	texts := make([]string, 0, n)
	for i := range n {
		texts = append(texts, fmt.Sprintf("clip %02d", i+1))
	}

	return texts
}

// press sends one key, naming the special keys the picker binds and treating
// anything else as literal runes.
//
// A frame is only worth a golden when the picker is still showing it, and every
// exit key the picker binds also leaves the screen it was on — so tests that
// golden a preview or a revealed row quit with ctrl+c, the one key that quits
// from wherever it is pressed.
func press(tm *teatest.TestModel, keys ...string) {
	for _, key := range keys {
		switch key {
		case "up":
			tm.Send(tea.KeyMsg{Type: tea.KeyUp})
		case "down":
			tm.Send(tea.KeyMsg{Type: tea.KeyDown})
		case "left":
			tm.Send(tea.KeyMsg{Type: tea.KeyLeft})
		case "right":
			tm.Send(tea.KeyMsg{Type: tea.KeyRight})
		case "esc":
			tm.Send(tea.KeyMsg{Type: tea.KeyEscape})
		case "enter":
			tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		case "tab":
			tm.Send(tea.KeyMsg{Type: tea.KeyTab})
		case "ctrl+c":
			tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		default:
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}
	}
}

// typed sends one key press per rune, which is what a user typing into the
// search box produces — a single multi-rune KeyMsg is one paste, not six keys.
func typed(tm *teatest.TestModel, text string) {
	for _, r := range text {
		press(tm, string(r))
	}
}

// finish waits for the program to exit and returns the model it left behind.
func finish(t *testing.T, tm *teatest.TestModel) tea.Model {
	t.Helper()

	return tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second))
}

// loadItems re-reads the store from disk, so mutation tests assert on the file
// rather than on in-memory state.
func loadItems(t *testing.T, dir string) []store.Item {
	t.Helper()

	items, err := store.Open(dir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return items
}

func findItem(t *testing.T, items []store.Item, id string) store.Item {
	t.Helper()

	index := slices.IndexFunc(items, func(it store.Item) bool { return it.ID == id })
	if index < 0 {
		t.Fatalf("item %s not found in %d items", id, len(items))
	}

	return items[index]
}

func TestListFrame(t *testing.T) {
	tm := start(t, fixtureDir(t))

	press(tm, "q")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

func TestSecretMasked(t *testing.T) {
	dir := textDir(t, secretText)
	tm := start(t, dir)

	press(tm, "1")

	final := finish(t, tm)

	selected := tui.SelectionOf(final)
	if selected == nil {
		t.Fatal("no selection")
	}

	// The confirmation the caller prints must name the category and nothing
	// else — not the value, not even the masked preview.
	if selected.Label != "Anthropic key" {
		t.Errorf("Label = %q, want %q", selected.Label, "Anthropic key")
	}

	teatest.RequireEqualOutput(t, []byte(final.View()))
}

func TestNumberSelectQuits(t *testing.T) {
	tm := start(t, fixtureDir(t))

	press(tm, "3")

	selected := tui.SelectionOf(finish(t, tm))
	if selected == nil {
		t.Fatal("no selection")
	}

	if selected.Item.ID != idCCCC {
		t.Errorf("selected %s, want %s", selected.Item.ID, idCCCC)
	}

	if selected.Label != "Image 3×2 · PNG" {
		t.Errorf("Label = %q, want %q", selected.Label, "Image 3×2 · PNG")
	}
}

// TestCursorCopyQuits covers `c`, which copies wherever the cursor is rather
// than the row a number names. Two presses of down put it on the third row, so
// a `c` that read the cursor and a `c` that fell through to some fixed row are
// told apart by the result.
func TestCursorCopyQuits(t *testing.T) {
	tm := start(t, fixtureDir(t))

	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	press(tm, "c")

	selected := tui.SelectionOf(finish(t, tm))
	if selected == nil {
		t.Fatal("no selection")
	}

	if selected.Item.ID != idCCCC {
		t.Errorf("selected %s, want %s", selected.Item.ID, idCCCC)
	}
}

// TestCursorCopyOnEmptyListIsANoOp guards the index: an empty list leaves the
// cursor at zero with nothing under it, and `c` must not select a row that is
// not there.
func TestCursorCopyOnEmptyListIsANoOp(t *testing.T) {
	tm := start(t, textDir(t))

	press(tm, "c")
	press(tm, "q")

	if selected := tui.SelectionOf(finish(t, tm)); selected != nil {
		t.Errorf("selected %v from an empty list", selected.Item.ID)
	}
}

func TestASCIIFrame(t *testing.T) {
	tm := startWith(t, fixtureDir(t), tui.Options{ASCII: true})

	press(tm, "q")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

// TestPagerCountsPastTenPages locks the compact pager: past ten pages the dots
// stop being countable and give way to a "p n/m".
func TestPagerCountsPastTenPages(t *testing.T) {
	tm := start(t, textDir(t, clips(105)...))

	press(tm, "right", "right", "q")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

func TestPaging(t *testing.T) {
	tm := start(t, textDir(t, clips(25)...))

	// Right flips to page two; 0 then copies its tenth row, the twentieth item.
	press(tm, "right", "0")

	final := finish(t, tm)

	selected := tui.SelectionOf(final)
	if selected == nil {
		t.Fatal("no selection")
	}

	if selected.Label != "clip 20" {
		t.Errorf("Label = %q, want %q", selected.Label, "clip 20")
	}

	teatest.RequireEqualOutput(t, []byte(final.View()))
}

func TestPinReorders(t *testing.T) {
	dir := fixtureDir(t)
	tm := start(t, dir)

	// Row two is AAAA, the newest unpinned item; pinning it moves it above the
	// already-pinned BBBB, which is older.
	press(tm, "down", "p", "q")

	final := finish(t, tm)

	if pinned := findItem(t, loadItems(t, dir), idAAAA).IsPinned; !pinned {
		t.Error("AAAA is not pinned on disk")
	}

	teatest.RequireEqualOutput(t, []byte(final.View()))
}

func TestDelete(t *testing.T) {
	dir := fixtureDir(t)
	tm := start(t, dir)

	press(tm, "down", "d", "q")

	final := finish(t, tm)

	items := loadItems(t, dir)
	if len(items) != 3 {
		t.Errorf("history holds %d items, want 3", len(items))
	}

	if slices.ContainsFunc(items, func(it store.Item) bool { return it.ID == idAAAA }) {
		t.Error("AAAA survived the delete")
	}

	teatest.RequireEqualOutput(t, []byte(final.View()))
}

func TestQuitLeavesNoSelection(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			tm := start(t, fixtureDir(t))

			if key == "ctrl+c" {
				tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
			} else {
				press(tm, key)
			}

			if selected := tui.SelectionOf(finish(t, tm)); selected != nil {
				t.Errorf("quit produced selection %+v", selected)
			}
		})
	}
}

// TestSearchFrame locks the searching footer: the pager and the copy hints give
// way to the input, and the header counts matches instead of items.
func TestSearchFrame(t *testing.T) {
	tm := start(t, fixtureDir(t))

	press(tm, "/")
	typed(tm, "rebase")
	press(tm, "ctrl+c")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

// TestSearchFiltersAndSelects walks the whole search plane: type a query, hand
// focus back to the list with ⏎, then press the number the filtered row now
// carries. Row 1 of the filtered view is AAAA, which was row 2 unfiltered.
func TestSearchFiltersAndSelects(t *testing.T) {
	tm := start(t, fixtureDir(t))

	press(tm, "/")
	typed(tm, "rebase")
	press(tm, "enter", "1")

	final := finish(t, tm)

	selected := tui.SelectionOf(final)
	if selected == nil {
		t.Fatal("no selection")
	}

	if selected.Item.ID != idAAAA {
		t.Errorf("selected %s, want %s", selected.Item.ID, idAAAA)
	}

	teatest.RequireEqualOutput(t, []byte(final.View()))
}

// TestSearchEscClearsFilter checks that esc drops the query rather than
// quitting: the frame it leaves behind is the unfiltered list.
func TestSearchEscClearsFilter(t *testing.T) {
	tm := start(t, fixtureDir(t))

	press(tm, "/")
	typed(tm, "rebase")
	press(tm, "esc", "ctrl+c")

	final := finish(t, tm)

	if selected := tui.SelectionOf(final); selected != nil {
		t.Errorf("esc produced selection %+v", selected)
	}

	teatest.RequireEqualOutput(t, []byte(final.View()))
}

func TestSearchWithoutMatches(t *testing.T) {
	tm := start(t, fixtureDir(t))

	press(tm, "/")
	typed(tm, "zzzz")
	press(tm, "enter", "ctrl+c")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

// TestSearchNeverMatchesSecretText is the one search behaviour that is a
// security property rather than an ergonomic one: the haystack is the masked
// label, so a query drawn from the hidden part of a credential finds nothing.
func TestSearchNeverMatchesSecretText(t *testing.T) {
	tm := start(t, textDir(t, secretText, "a plain note"))

	press(tm, "/")
	typed(tm, "uvwxyz")
	press(tm, "enter", "1", "ctrl+c")

	if selected := tui.SelectionOf(finish(t, tm)); selected != nil {
		t.Errorf("a query matching only the hidden value selected %+v", selected)
	}
}

// secretText is a fake Anthropic key, and secretTail is the stretch of it that
// Mask hides — the part no row, no query and no confirmation may show. A list
// row is too narrow to hold the whole value even revealed, so assertions about
// rows look for the tail rather than for the value.
const (
	secretText = "sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"
	secretTail = "api03-abcdefghij"
)

func TestSecretMaskAndReveal(t *testing.T) {
	dir := textDir(t, secretText, "a plain note")

	t.Run("masked", func(t *testing.T) {
		tm := start(t, dir)

		press(tm, "ctrl+c")

		teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
	})

	t.Run("revealed", func(t *testing.T) {
		tm := start(t, dir)

		press(tm, "s", "ctrl+c")

		view := finish(t, tm).View()
		if !strings.Contains(view, secretTail) {
			t.Errorf("revealed row does not show the value:\n%s", view)
		}

		teatest.RequireEqualOutput(t, []byte(view))
	})

	t.Run("remasked by moving the cursor", func(t *testing.T) {
		tm := start(t, dir)

		press(tm, "s", "down", "ctrl+c")

		view := finish(t, tm).View()
		if strings.Contains(view, secretTail) {
			t.Errorf("moving the cursor left the value on screen:\n%s", view)
		}

		teatest.RequireEqualOutput(t, []byte(view))
	})
}

func TestPreviewText(t *testing.T) {
	tm := start(t, fixtureDir(t))

	// Row two is AAAA, the only fixture item carrying a source bundle.
	press(tm, "down", "enter", "ctrl+c")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

// TestPreviewWrapsLongText checks the body against the frame rather than the
// terminal: a paragraph longer than one line is wrapped, and text taller than
// the window is cut rather than pushing the frame off screen.
func TestPreviewWrapsLongText(t *testing.T) {
	tm := start(t, textDir(t, strings.TrimSpace(strings.Repeat("the quick brown fox jumps over the lazy dog. ", 12))))

	press(tm, "tab", "ctrl+c")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

func TestPreviewImage(t *testing.T) {
	tm := start(t, fixtureDir(t))

	// Row three is CCCC, the 3×2 PNG the fixture seeds a blob for.
	press(tm, "down", "down", "enter", "ctrl+c")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

// The hostile fixtures are clipboard entries of the kind a copy button on a web
// page can plant: an OSC 52 sequence telling the terminal to rewrite the system
// clipboard, a screen clear, an alt-screen teardown, a tab that would split a
// column. None of it costs any display width, so a frame carrying it would look
// perfectly ordinary.
//
// hostileSecret matters separately from hostileText: it classifies as a
// credential, and a credential is described by Mask rather than by the label
// path — Mask copies the first runes of the first line through verbatim, so it
// is its own way into a frame.
const (
	hostileText = "harmless\tlooking\x1b]52;c;cGF5bG9hZA==\x07 line\n" +
		"\x1b[2J\x1b[?1049l and the rest of the note"
	hostileSecret = "\x1b[2Jxx\nsk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"
)

// TestControlCharactersNeverReachTheTerminal is why the display path
// sanitizes: the picker prints bytes it did not author, and an escape sequence
// among them is an instruction to the terminal rather than something to read.
// Every screen that can show them has to come out clean.
func TestControlCharactersNeverReachTheTerminal(t *testing.T) {
	cases := []struct {
		name string
		text string
		keys []string
	}{
		{name: "list", text: hostileText, keys: []string{"ctrl+c"}},
		{name: "preview", text: hostileText, keys: []string{"enter", "ctrl+c"}},
		{name: "masked row", text: hostileSecret, keys: []string{"ctrl+c"}},
		{name: "masked preview", text: hostileSecret, keys: []string{"enter", "ctrl+c"}},
		{name: "revealed row", text: hostileSecret, keys: []string{"s", "ctrl+c"}},
		{name: "revealed preview", text: hostileSecret, keys: []string{"s", "enter", "ctrl+c"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tm := start(t, textDir(t, tc.text))

			press(tm, tc.keys...)

			view := finish(t, tm).View()
			if index := indexOfControl(view); index >= 0 {
				t.Errorf("frame carries %q at byte %d:\n%q", view[index], index, view)
			}
		})
	}
}

// indexOfControl finds the first character a terminal would act on instead of
// print. Newlines are what separate the frame's own lines, so they are the one
// control character a rendered frame is allowed to hold.
func indexOfControl(frame string) int {
	for index, r := range frame {
		if r != '\n' && unicode.IsControl(r) {
			return index
		}
	}

	return -1
}

func TestPreviewSecret(t *testing.T) {
	dir := textDir(t, secretText, "a plain note")

	t.Run("masked", func(t *testing.T) {
		tm := start(t, dir)

		press(tm, "enter", "ctrl+c")

		view := finish(t, tm).View()
		if strings.Contains(view, secretText) {
			t.Errorf("preview leaked the value:\n%s", view)
		}

		teatest.RequireEqualOutput(t, []byte(view))
	})

	t.Run("revealed", func(t *testing.T) {
		tm := start(t, dir)

		press(tm, "s", "enter", "ctrl+c")

		view := finish(t, tm).View()
		if !strings.Contains(view, secretText) {
			t.Errorf("revealed preview does not show the value:\n%s", view)
		}

		teatest.RequireEqualOutput(t, []byte(view))
	})
}

// TestPreviewReturnsOnAnyKey checks that the preview is a detour, not a mode:
// the key that leaves it is whichever one the user presses next.
func TestPreviewReturnsOnAnyKey(t *testing.T) {
	tm := start(t, fixtureDir(t))

	press(tm, "enter", "x", "q")

	view := finish(t, tm).View()
	if !strings.Contains(view, "page 1/1") {
		t.Errorf("picker did not return to the list:\n%s", view)
	}
}

func TestEmptyState(t *testing.T) {
	tm := start(t, textDir(t))

	press(tm, "q")

	teatest.RequireEqualOutput(t, []byte(finish(t, tm).View()))
}

// missingDir names a directory that cannot exist, so the golden below carries a
// stable path. Nothing is ever read from or written to it.
const missingDir = "/nonexistent/extendo"

func TestNoStoreState(t *testing.T) {
	tm := startWith(t, missingDir, tui.Options{})

	press(tm, "q")

	final := finish(t, tm)

	if selected := tui.SelectionOf(final); selected != nil {
		t.Errorf("the no-store screen produced selection %+v", selected)
	}

	teatest.RequireEqualOutput(t, []byte(final.View()))
}

// TestStartupErrorTriage locks which read failures get a screen and which get
// an error: a history that is merely absent is a designed state and must leave
// the exit code at zero, while one that cannot be parsed is a real failure.
func TestStartupErrorTriage(t *testing.T) {
	t.Run("missing history opens a screen", func(t *testing.T) {
		m := tui.NewModel(store.Open(missingDir), tui.Options{})
		if err := tui.StartupError(m); err != nil {
			t.Errorf("StartupError = %v, want nil", err)
		}
	})

	t.Run("unreadable history is an error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "history.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatalf("write history.json: %v", err)
		}

		if err := tui.StartupError(tui.NewModel(store.Open(dir), tui.Options{})); err == nil {
			t.Error("StartupError = nil, want a parse failure")
		}
	})
}

func TestRel(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		elapsed  time.Duration
		expected string
	}{
		{name: "same instant", elapsed: 0, expected: "now"},
		{name: "sub second", elapsed: 999 * time.Millisecond, expected: "now"},
		{name: "one second", elapsed: time.Second, expected: "1s"},
		{name: "seconds", elapsed: 45 * time.Second, expected: "45s"},
		{name: "floors to seconds", elapsed: 59*time.Second + 999*time.Millisecond, expected: "59s"},
		{name: "one minute", elapsed: time.Minute, expected: "1m"},
		{name: "minutes", elapsed: 2 * time.Minute, expected: "2m"},
		{name: "floors to minutes", elapsed: 59*time.Minute + 59*time.Second, expected: "59m"},
		{name: "one hour", elapsed: time.Hour, expected: "1h"},
		{name: "floors to hours", elapsed: 23*time.Hour + 59*time.Minute, expected: "23h"},
		{name: "one day", elapsed: 24 * time.Hour, expected: "1d"},
		{name: "days", elapsed: 48 * time.Hour, expected: "2d"},
		{name: "floors to days", elapsed: 6 * 24 * time.Hour, expected: "6d"},
		{name: "one week", elapsed: 7 * 24 * time.Hour, expected: "1w"},
		{name: "weeks", elapsed: 21 * 24 * time.Hour, expected: "3w"},
		{name: "stays in weeks past a year", elapsed: 400 * 24 * time.Hour, expected: "57w"},
		{name: "future item reads as now", elapsed: -time.Hour, expected: "now"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tui.Rel(now.Add(-tc.elapsed), now)
			if got != tc.expected {
				t.Errorf("Rel(-%s) = %q, want %q", tc.elapsed, got, tc.expected)
			}
		})
	}
}

// TestResizeReflowsTheFrame checks that a resize actually redraws at the new
// width. A frame still drawn for the old, wider terminal is what wraps and
// leaves debris on screen.
func TestResizeReflowsTheFrame(t *testing.T) {
	cases := []struct {
		name  string
		width int
	}{
		{name: "narrower", width: 60},
		{name: "wider", width: 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tui.NewModel(store.Open(fixtureDir(t)), tui.Options{Now: func() time.Time { return fixedNow }})
			resized, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: termHeight})

			for i, line := range strings.Split(resized.View(), "\n") {
				if got := lipgloss.Width(line); got != tc.width {
					t.Errorf("line %d is %d cells wide, want %d", i, got, tc.width)
				}
			}
		})
	}
}

// TestResizeClearsTheScreen pins the debris fix itself. Bubble Tea drops its
// render cache on a resize but does not erase the screen, so the new frame
// paints over only the rows it occupies and the old one's wrapped tail lives
// on below it.
func TestResizeClearsTheScreen(t *testing.T) {
	m := tui.NewModel(store.Open(fixtureDir(t)), tui.Options{Now: func() time.Time { return fixedNow }})

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: termHeight})
	if cmd == nil {
		t.Fatal("resize returned no command, want tea.ClearScreen")
	}

	if got := cmd(); got != tea.ClearScreen() {
		t.Errorf("resize returned %T, want the message tea.ClearScreen sends", got)
	}
}

// TestResizeToTheSameSizeIsANoOp keeps the clear off the path a redundant
// WindowSizeMsg takes: erasing the screen on every repeat is a visible flicker.
func TestResizeToTheSameSizeIsANoOp(t *testing.T) {
	m := tui.NewModel(store.Open(fixtureDir(t)), tui.Options{Now: func() time.Time { return fixedNow }})

	sized, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: termHeight})
	if _, cmd := sized.Update(tea.WindowSizeMsg{Width: 60, Height: termHeight}); cmd != nil {
		t.Errorf("re-sending the same size returned %T, want nil", cmd())
	}
}

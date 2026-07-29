package main

import (
	"strings"
	"testing"

	"github.com/tjq/extendo-cli/internal/store"
	"github.com/tjq/extendo-cli/internal/tui"
)

// fakePicker stands in for the interactive picker so the terminal branch can be
// driven without a terminal. It records the options it was handed and returns a
// canned selection.
type fakePicker struct {
	opts     tui.Options
	selected *tui.Selected
	err      error
	calls    int
}

func (p *fakePicker) run(s *store.Store, opts tui.Options) (*tui.Selected, error) {
	p.calls++
	p.opts = opts

	return p.selected, p.err
}

// usePicker pins the picker the root command opens on a terminal.
func usePicker(t *testing.T, p *fakePicker) {
	t.Helper()

	previous := openPicker
	openPicker = p.run

	t.Cleanup(func() { openPicker = previous })
}

// firstItem is the item at the top of a store's sorted history, which is what
// the fake picker hands back.
func firstItem(t *testing.T, dir string) store.Item {
	t.Helper()

	items, err := loadSorted(store.Open(dir))
	if err != nil {
		t.Fatalf("loadSorted: %v", err)
	}

	if len(items) == 0 {
		t.Fatal("history is empty")
	}

	return items[0]
}

func TestRootTerminalOpensPicker(t *testing.T) {
	forceTTY(t, true)

	dir := fixtureDir(t)
	picker := &fakePicker{selected: &tui.Selected{Item: firstItem(t, dir), Label: "tyler@example.com"}}
	usePicker(t, picker)

	runner := &fakeRunner{}

	got := run(t, dir, runner, "--ascii")
	if got.err != nil {
		t.Fatalf("bare ext: %v", got.err)
	}

	if picker.calls != 1 {
		t.Fatalf("picker ran %d times, expected 1", picker.calls)
	}

	if !picker.opts.ASCII {
		t.Error("--ascii did not reach the picker")
	}

	if len(runner.calls) != 1 || string(runner.calls[0].stdin) != "tyler@example.com" {
		t.Fatalf("calls = %+v, expected one pbcopy carrying the selection", runner.calls)
	}

	if got.stderr != "✓ copied #1 (tyler@example.com)\n" {
		t.Errorf("stderr = %q", got.stderr)
	}

	if got.stdout != "" {
		t.Errorf("stdout = %q, expected empty — status lines belong on stderr", got.stdout)
	}
}

// TestRootPickerConfirmationHidesSecret extends the get command's guarantee to
// the picker: the line that lands in scrollback names the category and nothing
// derived from the value.
func TestRootPickerConfirmationHidesSecret(t *testing.T) {
	forceTTY(t, true)

	dir := secretDir(t)
	usePicker(t, &fakePicker{selected: &tui.Selected{Item: firstItem(t, dir), Label: secretCategory}})

	runner := &fakeRunner{}

	got := run(t, dir, runner)
	if got.err != nil {
		t.Fatalf("bare ext: %v", got.err)
	}

	if got.stderr != "✓ copied #1 ("+secretCategory+")\n" {
		t.Errorf("stderr = %q", got.stderr)
	}

	for _, leak := range []string{secretValue, secretMaskPrefix, secretMaskedLabel, "sk-"} {
		if strings.Contains(got.stderr, leak) {
			t.Errorf("stderr leaked %q: %q", leak, got.stderr)
		}
	}

	if len(runner.calls) != 1 || string(runner.calls[0].stdin) != secretValue {
		t.Errorf("calls = %+v, expected one pbcopy carrying the full secret", runner.calls)
	}
}

func TestRootQuitCopiesNothing(t *testing.T) {
	forceTTY(t, true)
	usePicker(t, &fakePicker{})

	runner := &fakeRunner{}

	got := run(t, fixtureDir(t), runner)
	if got.err != nil {
		t.Fatalf("bare ext: %v", got.err)
	}

	if len(runner.calls) != 0 {
		t.Errorf("quitting the picker ran %d commands, expected none", len(runner.calls))
	}

	if got.stderr != "" || got.stdout != "" {
		t.Errorf("quitting the picker wrote stdout %q stderr %q, expected silence", got.stdout, got.stderr)
	}
}

func TestASCIIComesFromFlagOrEnvironment(t *testing.T) {
	cases := []struct {
		name     string
		env      string
		args     []string
		expected bool
	}{
		{name: "off by default", expected: false},
		{name: "flag", args: []string{"--ascii"}, expected: true},
		{name: "environment", env: "1", expected: true},
		{name: "environment off", env: "0", expected: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forceTTY(t, true)
			t.Setenv("EXTENDO_ASCII", tc.env)

			picker := &fakePicker{}
			usePicker(t, picker)

			got := run(t, fixtureDir(t), &fakeRunner{}, tc.args...)
			if got.err != nil {
				t.Fatalf("bare ext: %v", got.err)
			}

			if picker.opts.ASCII != tc.expected {
				t.Errorf("ASCII = %v, expected %v", picker.opts.ASCII, tc.expected)
			}
		})
	}
}

// TestRootPipedIgnoresPicker keeps the piped branch honest: --ascii is accepted
// everywhere but the picker is never opened when stdout is not a terminal.
func TestRootPipedIgnoresPicker(t *testing.T) {
	freezeClock(t)
	forceTTY(t, false)

	picker := &fakePicker{}
	usePicker(t, picker)

	got := run(t, fixtureDir(t), &fakeRunner{}, "--ascii")
	if got.err != nil {
		t.Fatalf("bare ext: %v", got.err)
	}

	if picker.calls != 0 {
		t.Errorf("picker ran %d times on a pipe, expected none", picker.calls)
	}

	checkGolden(t, "list.golden", got.stdout)
}

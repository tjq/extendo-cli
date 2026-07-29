package shell

import (
	"strings"
	"testing"
)

func TestRenderTmuxGolden(t *testing.T) {
	checkGolden(t, "tmux.golden", RenderTmux(brewPath, DefaultKey))
}

// TestRenderTmuxSpellsTheKeyItsOwnWay is the one place the three shells diverge
// on the same chord: zsh wants ^G, readline wants \C-g, and tmux wants C-g with
// a lowercase letter — C-G there is ctrl plus shift plus G, a chord the user
// never asked for and will never press.
func TestRenderTmuxSpellsTheKeyItsOwnWay(t *testing.T) {
	if got := RenderTmux(brewPath, DefaultKey); !strings.Contains(got, "bind-key -n C-g ") {
		t.Errorf("tmux block does not bind C-g:\n%s", got)
	}

	key, err := ParseKey("ctrl-t")
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}

	got := RenderTmux(brewPath, key)

	if !strings.Contains(got, "bind-key -n C-t ") {
		t.Errorf("tmux block does not bind C-t:\n%s", got)
	}

	if strings.Contains(got, "C-g") {
		t.Errorf("tmux block still mentions the default C-g:\n%s", got)
	}
}

// TestRenderTmuxRunsTheBinaryQuietly keeps the popup from being the one caller
// that prints a confirmation. It has nowhere to print it to — the popup closes
// the moment the picker exits — and the point of the tmux route is that nothing
// reaches the pane underneath.
func TestRenderTmuxRunsTheBinaryQuietly(t *testing.T) {
	got := RenderTmux(brewPath, DefaultKey)

	if !strings.Contains(got, `'"/opt/homebrew/bin/ext" --quiet'`) {
		t.Errorf("tmux block does not run the binary quietly by path:\n%s", got)
	}

	// -E closes the popup when the picker exits. Without it the user is left
	// looking at a dead popup they have to dismiss by hand.
	if !strings.Contains(got, "display-popup -E ") {
		t.Errorf("tmux block does not close the popup on exit:\n%s", got)
	}
}

// TestRenderTmuxQuotesAwkwardPaths covers the two-level quoting: the shell sees
// a double-quoted path, so a space or a `$` in it survives, and tmux sees one
// single-quoted word, so it runs no format expansion over any of it.
func TestRenderTmuxQuotesAwkwardPaths(t *testing.T) {
	got := RenderTmux("/Users/x/Application Support/ext$dev", DefaultKey)

	if !strings.Contains(got, `'"/Users/x/Application Support/ext\$dev" --quiet'`) {
		t.Errorf("tmux block does not quote an awkward path:\n%s", got)
	}
}

// TestTmuxBlockRoundTrips is what makes `ext install --tmux` safe to re-run and
// safe to undo. The markers are the shell block's, and a `#` comment means the
// same thing in a tmux config, so Apply and Remove work on it unchanged — this
// pins that they do.
func TestTmuxBlockRoundTrips(t *testing.T) {
	const existing = "set -g mouse on\n"

	installed := Apply(existing, RenderTmux(brewPath, DefaultKey))

	if !IsInstalled(installed) {
		t.Fatalf("IsInstalled missed the tmux block it just wrote:\n%s", installed)
	}

	if twice := Apply(installed, RenderTmux(brewPath, DefaultKey)); twice != installed {
		t.Errorf("a second --tmux install changed the file:\n%s", twice)
	}

	got, removed := Remove(installed)
	if !removed {
		t.Fatalf("Remove reported no block in:\n%s", installed)
	}

	if got != existing {
		t.Errorf("Remove = %q, expected %q", got, existing)
	}
}

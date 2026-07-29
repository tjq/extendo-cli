package shell

import (
	"fmt"
	"strings"
)

// The popup's size, which the two dimensions want spelled differently.
//
// The picker's frame is a fixed sixteen lines — one page is ten rows whatever
// the terminal, and a short page is padded rather than shrunk — so the height is
// an absolute count with a couple of lines to spare. A percentage would clip the
// footer on a short terminal and leave half a screen of nothing on a tall one.
// tmux clamps a popup larger than the window, so the small case still fits.
//
// The width is a percentage because the frame does stretch: it fills whatever it
// is given, the same as `ext` run at the prompt.
const (
	popupWidth  = "80%"
	popupHeight = 18
)

// MinTmux is the first tmux with display-popup, which the binding below needs.
// A tmux older than this parses the line as an unknown command and stops
// reading the rest of the config file, so install checks the version rather
// than writing a block that would take the user's whole tmux setup down.
const MinTmux = "3.2"

// RenderTmux returns the managed block for ~/.tmux.conf, ending in a newline.
//
// This is the binding that answers the case a shell binding cannot. `bindkey`
// and `bind -x` only fire while the shell's line editor owns the terminal; at a
// password prompt, in an editor, or inside a REPL, the foreground process owns
// it and the shell is blocked waiting for that process to exit — the chord is
// delivered to the program, and the widget never runs. tmux reads keys before
// the pane's process does, so the same chord works everywhere.
//
// The popup is a pane of its own with its own pty, so the picker never draws on
// the terminal the paused program is holding: nothing is restored afterwards
// because nothing was overwritten. Selecting still only copies — there is no
// spelling of this that types into the pane, and typing a clipboard item into a
// password prompt is exactly what nobody wants.
//
// Unlike Render there is no PATH line and no alias. A tmux config cannot export
// anything into the user's shells, and the binding names the binary by path.
func RenderTmux(exePath string, key Key) string {
	lines := []string{
		startMarker,
		managedNote(" --tmux"),
		"# a tmux binding is read before the pane's own program sees the key, so this opens",
		"# over a password prompt or an editor — where a shell binding cannot fire at all",
		fmt.Sprintf("bind-key -n %s display-popup -E -w %s -h %d %s",
			key.tmux(), popupWidth, popupHeight, tmuxCommand(exePath)),
		endMarker,
		"",
	}

	return strings.Join(lines, "\n")
}

// tmuxCommand renders what the popup runs, quoted for both parsers it passes
// through.
//
// Two levels stack here: tmux takes the command as a single word and hands that
// word to `sh -c`. So the path is double-quoted for the shell, which keeps a
// space or a `$` in it intact, and the result is single-quoted for tmux, which
// treats a single-quoted string as literal and runs no `#{...}` expansion over
// it.
//
// A path containing a single quote has no spelling here at all: tmux's quoting
// has no escape for one, unlike the shell's. Nothing else a path can hold needs
// one.
func tmuxCommand(exePath string) string {
	return `'` + quoted(exePath) + ` ` + quietFlag + `'`
}

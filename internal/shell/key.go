package shell

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnknownKey reports a chord ext cannot bind: one it could not parse, or one
// it refuses to take away from the terminal. See ParseKey.
var ErrUnknownKey = errors.New("shell: unusable key")

// Key is the control chord the managed block binds the picker to.
//
// Only ctrl plus a letter is representable, which is the whole of what both
// shells spell the same way — zsh's caret notation and readline's `\C-` both
// cover exactly this and diverge past it.
type Key struct {
	// letter is lowercase 'a'..'z'. The zero value is not a usable Key; every
	// one comes from ParseKey or from DefaultKey.
	letter byte
}

// DefaultKey is ctrl-G, which the picker has always bound. In zsh it is
// send-break, the least missed of the chords a terminal leaves free.
var DefaultKey = Key{letter: 'g'}

// reserved are the chords ext will not bind, because the terminal or the line
// editor needs them more than the picker does. Binding one of these is not a
// preference ext can honour: it takes away interrupt, or end-of-file, or the
// key that submits the line.
var reserved = map[byte]string{
	'c': "interrupt",
	'd': "end of file",
	'h': "backspace",
	'i': "tab",
	'j': "newline",
	'm': "return",
	'q': "flow control (resume)",
	's': "flow control (suspend output)",
	'z': "suspend",
}

// ParseKey reads a chord written the way a person would: "ctrl-t", "ctrl+t",
// "^T" and "t" all name the same binding, in any case.
//
// It rejects what it cannot spell in both shells, and separately refuses the
// chords in reserved — a picker on ctrl-C would cost the user the only reliable
// way to stop a runaway program.
func ParseKey(spec string) (Key, error) {
	trimmed := strings.ToLower(strings.TrimSpace(spec))
	if trimmed == "" {
		return Key{}, fmt.Errorf("%w: no key given", ErrUnknownKey)
	}

	letter := trimmed
	for _, prefix := range []string{"ctrl-", "ctrl+", "control-", "c-", "^"} {
		if rest, found := strings.CutPrefix(letter, prefix); found {
			letter = rest

			break
		}
	}

	if len(letter) != 1 || letter[0] < 'a' || letter[0] > 'z' {
		return Key{}, fmt.Errorf(
			"%w: %q — ext binds ctrl plus a letter, spelled like \"ctrl-t\"", ErrUnknownKey, spec)
	}

	if what, isReserved := reserved[letter[0]]; isReserved {
		return Key{}, fmt.Errorf(
			"%w: ctrl-%s is %s, which the terminal needs more than the picker does",
			ErrUnknownKey, letter, what)
	}

	return Key{letter: letter[0]}, nil
}

// String names the key the way ParseKey accepts it, so that a message quoting
// one can be pasted straight back into the flag.
func (k Key) String() string {
	return "ctrl-" + string(k.resolved())
}

// caret is zsh's spelling: ^ and the uppercase letter.
func (k Key) caret() string {
	return "^" + strings.ToUpper(string(k.resolved()))
}

// readline is bash's spelling, and the backslash has to survive quoting to
// reach it.
func (k Key) readline() string {
	return `\C-` + string(k.resolved())
}

// tmux is tmux's spelling. Its key names are case-sensitive — `C-G` is ctrl
// plus shift plus G, which is a different chord — so the letter stays
// lowercase here, unlike zsh's caret notation.
func (k Key) tmux() string {
	return "C-" + string(k.resolved())
}

// resolved reads the letter, standing in the default for a zero Key so that a
// caller who forgot to parse one gets ctrl-G rather than a NUL byte spliced
// into somebody's profile.
func (k Key) resolved() byte {
	if k.letter == 0 {
		return DefaultKey.letter
	}

	return k.letter
}

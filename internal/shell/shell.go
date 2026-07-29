// Package shell renders the block `ext install` manages inside a user's shell
// profile, and splices it into — or out of — the file around it.
//
// Everything here is string work. Reading and writing the profile is the
// install command's job, which keeps the two things that can go wrong apart:
// the block's contents are golden-tested without a home directory, and the
// command is tested without a shell.
package shell

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrUnknownShell reports that ext does not know where a shell keeps the
// startup file this block belongs in.
var ErrUnknownShell = errors.New("shell: unrecognized shell")

// ErrNoHome reports that there is no home directory to resolve a profile
// against. Joining onto an empty home would yield a relative ".zshrc", written
// into whatever directory the command was run from.
var ErrNoHome = errors.New("shell: no home directory")

// Kind is a shell ext knows how to write a profile block for.
//
// Zsh rather than Unknown sits at the zero value, which is the order the plan
// fixed. Nothing here declares a bare Kind: every one comes from Detect, so
// there is no defaulted value to mistake for a detected zsh.
type Kind int

const (
	Zsh Kind = iota
	Bash
	Unknown
)

// binaryName is what the block's function and key binding call. A binary
// installed under any other name needs an alias to answer to it.
const binaryName = "ext"

// The markers delimit the region ext owns. Everything between them is rewritten
// on the next install; everything outside them is the user's.
const (
	startMarker = "# >>> extendo-cli >>>"
	endMarker   = "# <<< extendo-cli <<<"
)

// managedNote is the block's first line, so that someone reading their profile
// months later knows what wrote it and how to take it back out.
const managedNote = "# managed by `ext install` — do not edit; `ext install --uninstall` removes it"

// Detect maps the value of $SHELL onto the shell ext can configure. Anything
// else — fish, nushell, an empty environment — is Unknown, which the caller has
// to report rather than guess past: writing zsh syntax into a fish config
// breaks every shell the user opens afterwards.
func Detect(shellEnv string) Kind {
	// A login shell is spelled "-zsh" in argv[0], and $SHELL is occasionally
	// copied from there.
	base := strings.TrimPrefix(filepath.Base(strings.TrimSpace(shellEnv)), "-")

	switch base {
	case "zsh":
		return Zsh
	case "bash":
		return Bash
	default:
		return Unknown
	}
}

// ProfilePath returns the file the block belongs in.
//
// Zsh gets .zshrc rather than the .zprofile the spec first named: the block
// defines a ZLE widget and binds a key, and only an interactive shell reads
// .zshrc. Bash gets .bash_profile, which is what a macOS terminal reads because
// it starts login shells.
func ProfilePath(k Kind, home string) (string, error) {
	if home == "" {
		return "", ErrNoHome
	}

	switch k {
	case Zsh:
		return filepath.Join(home, ".zshrc"), nil
	case Bash:
		return filepath.Join(home, ".bash_profile"), nil
	default:
		return "", ErrUnknownShell
	}
}

// Render returns the managed block for a shell, ending in a newline. exePath is
// the running binary: its directory goes on PATH, and its name decides whether
// the block also needs an alias. key is the chord the picker is bound to; a
// zero Key means DefaultKey.
//
// The path is used as given rather than resolved through its symlinks. A
// Homebrew install is a link from a versioned Cellar directory into
// /opt/homebrew/bin, and the link is the part that survives an upgrade.
//
// An Unknown shell renders nothing — there is no syntax to write it in.
func Render(k Kind, exePath string, key Key) string {
	if k == Unknown {
		return ""
	}

	dir := filepath.Dir(exePath)

	lines := []string{startMarker, managedNote, fileNote(k, key)}

	// The guard keeps a re-sourced profile from growing $PATH a copy at a time,
	// which is what a bare export in an rc file does.
	lines = append(lines, fmt.Sprintf(
		`case ":$PATH:" in *":%s:"*) ;; *) export PATH="%s:$PATH" ;; esac`,
		escaped(dir), escaped(dir)))

	if filepath.Base(exePath) != binaryName {
		lines = append(lines, fmt.Sprintf(`alias ext=%s`, quoted(exePath)))
	}

	lines = append(lines, binding(k, exePath, key)...)
	lines = append(lines, endMarker, "")

	return strings.Join(lines, "\n")
}

// fileNote records why the block lives in this particular file, since neither
// choice is the obvious one.
func fileNote(k Kind, key Key) string {
	if k == Bash {
		return "# in ~/.bash_profile: a macOS terminal starts a login shell, which reads this file"
	}

	return fmt.Sprintf(
		"# in ~/.zshrc rather than ~/.zprofile: the %s widget below needs an interactive shell",
		key.caret())
}

// binding returns the lines that put the picker on key.
//
// Zsh gets a widget: the picker draws on the alt-screen and reads keys, so it
// is handed the terminal directly, and `zle reset-prompt` redraws the line it
// was called from afterwards. Bash's readline can only run a command, and a
// bash old enough to lack `bind -x` must not take the profile down with it.
//
// Both name the binary by path rather than as `ext`. The name only resolves
// when the binary is called ext: an alias is not expanded in argument position,
// and `command` skips aliases outright, so a binary installed under any other
// name — the alias branch in Render — left ctrl-G bound to something that does
// not exist. The path also pins which ext the hotkey runs when another one sits
// earlier on PATH, which the alias already did for the prompt.
func binding(k Kind, exePath string, key Key) []string {
	target := quoted(exePath)

	if k == Bash {
		// bind takes the whole binding as one word, so the command it runs is
		// wrapped in single quotes. The `\C-` inside them is readline's spelling
		// of a control chord and has to reach it with the backslash intact.
		return []string{fmt.Sprintf(`bind -x %s 2>/dev/null || true`,
			singleQuoted(`"`+key.readline()+`": `+target))}
	}

	return []string{
		fmt.Sprintf("_ext_picker() { %s </dev/tty >/dev/tty; zle reset-prompt }", target),
		"zle -N _ext_picker",
		fmt.Sprintf("bindkey '%s' _ext_picker", key.caret()),
	}
}

// quoted renders a path as a double-quoted shell word. Double rather than
// single quotes because the block already spells its paths that way, and
// because a single-quoted word cannot be nested inside bind's own quoting.
func quoted(s string) string {
	return `"` + escaped(s) + `"`
}

// escaped backslash-escapes the four characters a shell still acts on inside
// double quotes. A path is data — the user chose where to build the binary —
// and a directory holding a `$` should install a working block, not a broken
// one.
func escaped(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(s)
}

// singleQuoted renders a fragment as a single-quoted shell word, which a shell
// takes literally. A single quote cannot appear inside one at all, so each is
// replaced by the close-escape-reopen dance every shell requires.
func singleQuoted(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

// IsInstalled reports whether a profile already carries a complete managed
// block.
func IsInstalled(existing string) bool {
	_, _, ok := findBlock(existing)

	return ok
}

// Apply returns existing with block installed: replacing the managed region
// when the profile already has one, appending it below a blank line otherwise.
//
// Applying twice is the same as applying once, which is what makes `ext
// install` safe to re-run after every upgrade.
func Apply(existing, block string) string {
	body := strings.Split(strings.TrimRight(block, "\n"), "\n")

	start, end, ok := findBlock(existing)
	if !ok {
		return appendBlock(existing, body)
	}

	lines := strings.Split(existing, "\n")

	spliced := make([]string, 0, len(lines)+len(body))
	spliced = append(spliced, lines[:start]...)
	spliced = append(spliced, body...)
	spliced = append(spliced, lines[end+1:]...)

	return strings.Join(spliced, "\n")
}

// appendBlock puts the block at the end of a profile that has none, separated
// from whatever the user wrote by one blank line. Trailing blank lines in the
// original are collapsed into that separator rather than added to it.
func appendBlock(existing string, body []string) string {
	block := strings.Join(body, "\n") + "\n"

	trimmed := strings.TrimRight(existing, "\n")
	if trimmed == "" {
		return block
	}

	return trimmed + "\n\n" + block
}

// Remove takes the managed block back out, reporting whether there was one.
// A profile ext never wrote to is returned untouched, byte for byte.
func Remove(existing string) (string, bool) {
	start, end, ok := findBlock(existing)
	if !ok {
		return existing, false
	}

	lines := strings.Split(existing, "\n")

	kept := make([]string, 0, len(lines))
	kept = append(kept, lines[:start]...)
	kept = append(kept, lines[end+1:]...)

	return strings.Join(closeSeam(kept, start), "\n"), true
}

// closeSeam drops the blank line the block was installed under, so that
// removing it leaves the file as it was rather than with a growing gap where
// the block used to be. at is the index the removed lines started at.
func closeSeam(lines []string, at int) []string {
	// A block at the top of the file leaves its separator below it instead.
	if at == 0 {
		if len(lines) > 1 && lines[0] == "" {
			return lines[1:]
		}

		return lines
	}

	isSeparated := lines[at-1] == ""

	// Past the end of the slice means the block ran to the end of the file, and
	// the blank line above it is now trailing.
	isFollowedByBlank := at >= len(lines) || lines[at] == ""

	if isSeparated && isFollowedByBlank {
		return append(lines[:at-1], lines[at:]...)
	}

	return lines
}

// findBlock locates the managed region, returning the line indices of its start
// and end markers. A profile whose end marker someone deleted by hand has no
// region that can be replaced, so it does not count as found.
func findBlock(existing string) (int, int, bool) {
	lines := strings.Split(existing, "\n")

	start := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if start < 0 {
			if trimmed == startMarker {
				start = i
			}

			continue
		}

		if trimmed == endMarker {
			return start, i, true
		}
	}

	return -1, -1, false
}

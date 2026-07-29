package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tjq/extendo-cli/internal/clip"
	"github.com/tjq/extendo-cli/internal/shell"
	"github.com/tjq/extendo-cli/internal/store"
)

// lookPath reports where a command lives on PATH. It is a variable so tests can
// pin what doctor finds, rather than reporting on the machine running them.
var lookPath = exec.LookPath

// appProcess is the macOS app's process name, as pgrep matches it.
const appProcess = "extendo"

// glyphSample is drawn with the same characters the picker uses for text,
// images and files (see internal/tui/glyphs.go) plus its pin. They are ordinary
// assigned Unicode, so a font missing them is unusual — but whether this
// terminal has one is a font question no check can answer from here, and only
// the person reading the report can see the answer.
const glyphSample = "⍞ ◰ ▢  ✦ — if these are boxes, use --ascii"

// The marks a row is prefixed with. A warning is something worth knowing that
// still leaves ext working; a failure is something that does not.
const (
	markOK   = "✓"
	markWarn = "!"
	markFail = "✗"
)

type status int

const (
	statusOK status = iota
	statusWarn
	statusFail
)

// check is one line of the report.
type check struct {
	name   string
	detail string
	status status
}

// Rows are built through these three rather than as literals, so that no row
// can end up with a status nobody chose.
func okCheck(name, detail string) check { return check{name: name, detail: detail} }
func warnCheck(name, detail string) check {
	return check{name: name, detail: detail, status: statusWarn}
}
func failCheck(name, detail string) check {
	return check{name: name, detail: detail, status: statusFail}
}

func (c check) mark() string {
	switch c.status {
	case statusWarn:
		return markWarn
	case statusFail:
		return markFail
	default:
		return markOK
	}
}

func newDoctorCmd(s *store.Store, r clip.Runner) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check everything ext depends on",
		Long: "doctor reports on the store extendo writes, the app that fills it, the\n" +
			"tools ext copies with, and the shell profile `ext install` manages.\n\n" +
			"It exits 1 when a check fails. A warning — the app not running, no shell\n" +
			"block installed — is reported but does not fail the command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd, s, r)
		},
	}
}

func runDoctor(cmd *cobra.Command, s *store.Store, r clip.Runner) error {
	checks := []check{
		directoryCheck(s),
		historyCheck(s),
		blobsCheck(s),
		appCheck(r),
		toolCheck("pbcopy", "ext puts text back on the pasteboard with it"),
		toolCheck("osascript", "ext puts images and rich text back with it"),
		profileCheck(),
	}

	// The report is printed whatever it says: an exit code on its own names
	// nothing, and the failing row is the whole point of running this.
	if err := writeReport(cmd.OutOrStdout(), checks); err != nil {
		return err
	}

	failed := 0

	for _, c := range checks {
		if c.status == statusFail {
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(checks))
	}

	return nil
}

func directoryCheck(s *store.Store) check {
	const name = "store directory"

	info, err := os.Stat(s.Dir)
	if err != nil {
		return failCheck(name, fmt.Sprintf("%s is not there — is extendo installed?", s.Dir))
	}

	if !info.IsDir() {
		return failCheck(name, fmt.Sprintf("%s is not a directory", s.Dir))
	}

	return okCheck(name, s.Dir)
}

func historyCheck(s *store.Store) check {
	const name = "history"

	items, err := s.Load()
	if err != nil {
		return failCheck(name, err.Error())
	}

	return okCheck(name, countLabel(len(items), "item", "items"))
}

// blobsCheck reads the directory holding the payloads too large to inline. It
// is missing on a store that has only ever held text, which is ordinary rather
// than broken.
func blobsCheck(s *store.Store) check {
	const name = "blobs"

	path := filepath.Join(s.Dir, "blobs")

	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return warnCheck(name, "not created yet — nothing large enough has been copied")
	}

	if err != nil {
		return failCheck(name, fmt.Sprintf("%s is unreadable: %v", path, err))
	}

	return okCheck(name, countLabel(len(entries), "entry", "entries"))
}

// appCheck asks whether the macOS app is up. It is the one check that only
// warns: the history it wrote is on disk either way, so everything ext does
// keeps working — it just stops gaining new entries.
func appCheck(r clip.Runner) check {
	const name = "extendo app"

	// pgrep exits 1 when nothing matched, which the runner reports as an error.
	out, err := r.Run(nil, "pgrep", "-x", appProcess)

	pids := strings.Fields(string(out))
	if err != nil || len(pids) == 0 {
		return warnCheck(name, "not running — nothing is recording new clipboard entries")
	}

	return okCheck(name, "running (pid "+pids[0]+")")
}

func toolCheck(name, purpose string) check {
	path, err := lookPath(name)
	if err != nil {
		return failCheck(name, "not on PATH — "+purpose)
	}

	return okCheck(name, path)
}

// profileCheck looks for the block `ext install` writes. Shell integration is
// opt-in — ext is perfectly usable without ctrl-G — so a profile without it is
// worth pointing at and not worth failing over.
func profileCheck() check {
	const name = "shell profile"

	home, err := os.UserHomeDir()
	if err != nil {
		return warnCheck(name, "no home directory to look in")
	}

	path, err := shell.ProfilePath(shell.Detect(os.Getenv(shellEnv)), home)
	if err != nil {
		// Not `ext install --profile <file>`: that flag chooses the file, not
		// the syntax written into it, so install refuses an unsupported shell
		// with or without it. The row says what is true instead of what would
		// send someone off to try something that cannot work.
		return warnCheck(name, fmt.Sprintf("$SHELL is %q — ext only manages zsh and bash "+
			"profiles; everything but ctrl-G works without one", os.Getenv(shellEnv)))
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil || !shell.IsInstalled(string(data)) {
		return warnCheck(name, "no managed block in "+path+" — run `ext install`")
	}

	return okCheck(name, path)
}

// writeReport renders the checks as aligned columns, then the glyph line.
//
// Like the list table this goes through a buffer so that the padding tabwriter
// leaves on the last cell of a row does not reach the terminal.
func writeReport(w io.Writer, checks []check) error {
	buf := &bytes.Buffer{}
	tw := tabwriter.NewWriter(buf, 0, 0, 2, ' ', 0)

	for _, c := range checks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.mark(), c.name, c.detail)
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if _, err := fmt.Fprintln(w, strings.TrimRight(line, " ")); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
	}

	// The glyph line sits outside the table: it is a font test rather than a
	// check, and its icons have display widths tabwriter would have to guess at.
	if _, err := fmt.Fprintf(w, "\nglyphs  %s\n", glyphSample); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	return nil
}

// countLabel renders a count with the right plural, so a one-item history does
// not report "1 items". Both spellings are passed in because English does not
// derive one from the other.
func countLabel(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}

	return fmt.Sprintf("%d %s", n, plural)
}

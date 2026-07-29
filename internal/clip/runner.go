package clip

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Runner executes an external command, feeding it stdin and returning whatever
// it wrote. Every process this package spawns goes through a Runner, so tests
// can record the exact command shapes instead of touching the real pasteboard.
type Runner interface {
	Run(stdin []byte, name string, args ...string) ([]byte, error)
}

// ExecRunner is the production Runner and the only place in the package that
// reaches for os/exec.
type ExecRunner struct{}

// Run executes name with args. The command's stderr is folded into the returned
// output because osascript reports syntax errors there, and the caller has
// nowhere else to look when a script fails.
func (ExecRunner) Run(stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = bytes.NewReader(stdin)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("running %s: %w", name, err)
	}

	return out, nil
}

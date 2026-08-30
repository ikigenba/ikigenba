// Package cli implements the testable command-line interface for idgen.
package cli

import (
	"io"
	"time"

	"github.com/ikigenba/ikigenba/idgen/internal/idgen"
)

const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

// Clock supplies time to the CLI.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// Run executes the CLI and returns its process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, clock Clock) int {
	_ = stdin
	_ = stderr

	if len(args) != 0 {
		return exitSuccess
	}

	_, _ = io.WriteString(stdout, idgen.MintAt("R", clock.Now())+"\n")
	return exitSuccess
}

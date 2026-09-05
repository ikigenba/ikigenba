// Package cli sequences the agent-repl command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ikigenba/ikigenba/agent-repl/internal/options"
)

type exitCode int

const (
	exitSuccess exitCode = 0
	exitFailure exitCode = 1
	exitUsage   exitCode = 2
)

// Deps carries the environmental dependencies of a session.
type Deps struct {
	Home       string
	Getenv     func(string) string
	Now        func() time.Time
	Root       string
	Interrupts <-chan struct{}
}

// Run executes the CLI and returns its process exit code without terminating
// the calling process. args excludes the program name.
func Run(_ context.Context, args []string, _ io.Reader, stdout, stderr io.Writer, _ Deps) int {
	return int(run(args, stdout, stderr))
}

// run uses a distinct type for exit codes internally. Run converts at the
// public boundary because its fixed API contract returns int.
func run(args []string, stdout, stderr io.Writer) exitCode {
	flags, err := options.ParseFlags(args)
	if errors.Is(err, options.ErrHelp) {
		return writeText(stdout, options.Usage(), exitSuccess)
	}
	if err != nil {
		return writeUsageError(stderr, err)
	}

	if flags.Version {
		return writeText(stdout, version+"\n", exitSuccess)
	}

	if _, err := flags.Validate(); err != nil {
		return writeUsageError(stderr, err)
	}

	// Session setup and execution begin here in the next phase.
	return exitSuccess
}

func writeUsageError(stderr io.Writer, cause error) exitCode {
	if _, err := fmt.Fprintln(stderr, cause); err != nil {
		return exitFailure
	}
	return writeText(stderr, options.Usage(), exitUsage)
}

func writeText(destination io.Writer, value string, successCode exitCode) exitCode {
	if _, err := io.WriteString(destination, value); err != nil {
		return exitFailure
	}
	return successCode
}

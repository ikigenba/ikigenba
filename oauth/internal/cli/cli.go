// Package cli provides the in-process entry point for the OAuth CLI.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
	"github.com/ikigenba/ikigenba/oauth/internal/callback"
	"github.com/ikigenba/ikigenba/oauth/internal/options"
)

type exitCode int

const (
	exitSuccess exitCode = 0
	exitFailure exitCode = 1
	exitUsage   exitCode = 2
)

// Deps carries the non-deterministic dependencies used by a login.
type Deps struct {
	Launcher   browser.Launcher
	Entropy    io.Reader
	HTTPClient *http.Client
	Listen     callback.ListenFunc
}

// Run executes the CLI and returns its process exit code without terminating
// the calling process.
func Run(_ context.Context, args []string, stdout, stderr io.Writer, _ Deps) int {
	return int(run(args, stdout, stderr))
}

func run(args []string, stdout, stderr io.Writer) exitCode {
	parsed, err := options.Parse(args)
	if errors.Is(err, options.ErrHelp) {
		_, _ = io.WriteString(stdout, options.Usage())

		return exitSuccess
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n%s", strconv.Quote(err.Error()), options.Usage())

		return exitUsage
	}
	if parsed.Version {
		return exitSuccess
	}

	return exitSuccess
}

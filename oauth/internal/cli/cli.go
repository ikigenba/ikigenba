// Package cli provides the in-process entry point for the OAuth CLI.
package cli

import (
	"context"
	"io"
	"net/http"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
	"github.com/ikigenba/ikigenba/oauth/internal/callback"
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
func Run(_ context.Context, _ []string, _, _ io.Writer, _ Deps) int {
	return 0
}

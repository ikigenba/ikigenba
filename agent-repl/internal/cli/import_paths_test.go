package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agent-repl/internal/cli"
	"github.com/ikigenba/ikigenba/agent-repl/internal/help"
	"github.com/ikigenba/ikigenba/agent-repl/internal/options"
	"github.com/ikigenba/ikigenba/agent-repl/internal/render"
	"github.com/ikigenba/ikigenba/agent-repl/internal/session"
)

// R-R5A1-54IQ
func TestDesignedPackagesImportFromModulePath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(t.Context(), []string{"--version"}, strings.NewReader(""), &stdout, &stderr, cli.Deps{
		Home:   t.TempDir(),
		Getenv: func(string) string { return "" },
		Root:   t.TempDir(),
	})
	if code != 0 || stdout.Len() == 0 {
		t.Errorf("cli.Run(--version) = %d with stdout %q, want 0 and a version line", code, stdout.String())
	}

	if _, err := options.ParseFlags([]string{"--version"}); err != nil {
		t.Errorf("options.ParseFlags(--version) error = %v", err)
	}

	if help.Providers() == "" {
		t.Error("help.Providers() returned empty text")
	}

	if got := render.OneLine("a\nb"); got != "a b" {
		t.Errorf("render.OneLine = %q, want %q", got, "a b")
	}

	if _, err := session.Resolve(session.Config{
		Provider: "no-such-provider",
		Home:     t.TempDir(),
		Getenv:   func(string) string { return "" },
	}); err == nil {
		t.Error("session.Resolve accepted an unknown provider")
	}
}

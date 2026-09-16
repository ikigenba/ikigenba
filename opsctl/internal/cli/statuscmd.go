package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func runStatus(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) != 0 {
		return runCommandFrame("status", args, stdout, stderr, deps)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	rows, err := apps.Status(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	})
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}

	if err := writeStatusRows(stdout, rows); err != nil {
		writeDiagnostic(stderr, fmt.Errorf("status failed: write report: %w", err))
		return exitFail
	}
	return exitOK
}

func writeStatusRows(output io.Writer, rows []apps.StatusRow) error {
	var report strings.Builder
	for _, row := range rows {
		_, _ = fmt.Fprintf(&report, "%s %s %s %s\n", row.Name, row.Version, row.State, row.JournalMode)
	}
	_, err := io.WriteString(output, report.String())
	return err
}

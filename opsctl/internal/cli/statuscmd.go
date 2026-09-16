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
	if isCommandHelp(args) {
		return writeOut(stdout, statusUsage)
	}
	if len(args) != 0 {
		return writeLifecycleUsageError(stderr, "status", "status takes no arguments")
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	rows, err := apps.Status(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	})
	if err != nil {
		writeDiagnostic(stderr, fmt.Errorf("status report not produced: service enumeration did not complete: %w", err))
		return exitFail
	}

	if err := writeStatusRows(stdout, rows); err != nil {
		writeDiagnostic(stderr, fmt.Errorf("status failed: write report: %w", err))
		return exitFail
	}
	return exitOK
}

const statusUsage = `Usage: opsctl status

Print one line per service on this host, in name order: its name, the version
its own binary reports, the state of its systemd unit, and the journal mode of
the database its manifest declares. A service is any /opt/<name>/ with an etc/
or state/ directory; '-' means opsctl could not ask, or there was nothing to
ask.

A declared database must stay in WAL mode: litestream cannot replicate one in
any other mode, so a service reporting anything but 'wal' is a service whose
data is not reaching S3.

The exit code is 0 whatever the report says. A failed unit and an unreplicable
database are facts about the host, not failures of this command.
`

func writeStatusRows(output io.Writer, rows []apps.StatusRow) error {
	var report strings.Builder
	for _, row := range rows {
		_, _ = fmt.Fprintf(&report, "%s %s %s %s\n", row.Name, row.Version, row.State, row.JournalMode)
	}
	_, err := io.WriteString(output, report.String())
	return err
}

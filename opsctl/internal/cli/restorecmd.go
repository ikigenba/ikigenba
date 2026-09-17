package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

const restoreUsage = `Usage: opsctl restore SERVICE [--at <timestamp>]

Replace /opt/SERVICE/etc/ and /opt/SERVICE/state/ with a backup, and, when
SERVICE declares a [database], replace that database with what litestream
holds. Without --at both halves are the newest there is. Nothing under bin/ or
share/ is touched.

SERVICE's unit is stopped for the restore and started again after it, and so is
litestream.service, because the files step deletes the database both of them
hold open. A restore is a brief outage. An app already stopped is left stopped,
unless an earlier failed restore stopped it: a successful retry on the same
running host starts it again. A failed restore leaves both stopped.

Before litestream.service comes back, /etc/litestream.yml is regenerated from
the manifest the restore put in place, so a database restored into a host that
never ran SERVICE is replicated from the start line on.

Options:
  --at <timestamp>    restore the service as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself. The two
are not the same instant, because the tarball is written on a timer and the
database is replicated continuously.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
  host.name       the fully-qualified name this host answers at
  backup.service_db_seconds  how often a declared database is snapshotted whole
  backup.service_wal_seconds  how often a declared database's committed changes are shipped
`

type restoreInvocation struct {
	service string
	at      *time.Time
}

func runRestore(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, restoreUsage)
	}
	invocation, message := parseRestoreInvocation(args)
	if message != "" {
		return writeRestoreUsageError(stderr, message)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}

	store := config.Store{Root: deps.Root}
	hostName, code := certConfigValue(store, "host.name", stderr, deps)
	if code != exitOK {
		return code
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	report, runErr := backup.Restore(
		context.Background(), env, deps.Cloud, store, invocation.service, invocation.at,
		func(ctx context.Context) error { return nginx.Write(ctx, env, hostName) },
	)
	return renderRestoreOutcome(stdout, stderr, invocation.service, report, runErr)
}

func parseRestoreInvocation(args []string) (restoreInvocation, string) {
	var invocation restoreInvocation
	seenAt := false
	remaining := args
	for len(remaining) > 0 {
		argument := remaining[0]
		remaining = remaining[1:]
		if argument == "--at" {
			if seenAt {
				return restoreInvocation{}, "duplicate option '--at'"
			}
			seenAt = true
			if len(remaining) == 0 {
				return restoreInvocation{}, "--at takes an RFC 3339 timestamp"
			}
			timestamp := remaining[0]
			remaining = remaining[1:]
			parsed, err := time.Parse(time.RFC3339, timestamp)
			if err != nil {
				return restoreInvocation{}, "--at takes an RFC 3339 timestamp"
			}
			invocation.at = &parsed
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return restoreInvocation{}, "unknown option '" + diagnosticArg(argument) + "'"
		}
		if invocation.service != "" {
			return restoreInvocation{}, "restore takes one SERVICE"
		}
		invocation.service = argument
	}
	if invocation.service == "" {
		return restoreInvocation{}, "restore needs SERVICE"
	}
	return invocation, ""
}

func writeRestoreUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl restore --help' for usage\n")
	return exitUsage
}

func renderRestoreOutcome(stdout, stderr io.Writer, service string, report backup.RestoreReport, runErr error) exitCode {
	var rendered strings.Builder
	for _, step := range report.Steps {
		if step.Err != nil {
			_, _ = fmt.Fprintf(&rendered, "%s: failed: %s\n", diagnosticArg(step.Name), conciseCause(step.Err))
			continue
		}
		_, _ = fmt.Fprintf(&rendered, "%s: ok (%s)\n", diagnosticArg(step.Name), diagnosticArg(step.Detail))
	}
	if _, err := io.WriteString(stdout, rendered.String()); err != nil {
		writeDiagnostic(stderr, hostOperationDiagnostic{message: "restore " + diagnosticArg(service) + " report not produced", cause: err})
		return exitFail
	}
	if runErr == nil {
		return exitOK
	}
	failedStep := ""
	if len(report.Steps) > 0 && report.Steps[len(report.Steps)-1].Err != nil {
		failedStep = report.Steps[len(report.Steps)-1].Name
	}
	if failedStep == "" {
		writeRestoreUnreportedFailureDiagnostic(stderr, runErr)
		return exitFail
	}
	writeRestoreFailureDiagnostic(stderr, service, failedStep, runErr)
	return exitFail
}

func writeRestoreUnreportedFailureDiagnostic(stderr io.Writer, err error) {
	var failure *backup.RestoreError
	if !errors.As(err, &failure) || len(failure.Stopped) == 0 {
		writeDiagnostic(stderr, err)
		return
	}
	message, _, _ := strings.Cut(err.Error(), "\n")
	_, _ = io.WriteString(stderr, "opsctl: "+diagnosticArg(message)+"\n\n")
	writeRestoreStoppedDetail(stderr, failure.Stopped)
	for _, commandErr := range joinedCommandErrors(err) {
		quoteCapture(stderr, commandErr.Result.Stdout)
		quoteCapture(stderr, commandErr.Result.Stderr)
	}
}

func writeRestoreFailureDiagnostic(stderr io.Writer, service, step string, err error) {
	_, _ = fmt.Fprintf(stderr, "opsctl: restore %s failed at %s\n", diagnosticArg(service), diagnosticArg(step))
	var failure *backup.RestoreError
	var stopped []string
	if errors.As(err, &failure) {
		stopped = failure.Stopped
	}
	commandErrors := joinedCommandErrors(err)
	if len(stopped) == 0 && len(commandErrors) == 0 {
		return
	}
	_, _ = io.WriteString(stderr, "\n")
	writeRestoreStoppedDetail(stderr, stopped)
	for _, commandErr := range commandErrors {
		if len(commandErrors) > 1 {
			_, _ = io.WriteString(stderr, diagnosticArg(commandErr.Error())+"\n")
		}
		quoteCapture(stderr, commandErr.Result.Stdout)
		quoteCapture(stderr, commandErr.Result.Stderr)
	}
}

func writeRestoreStoppedDetail(stderr io.Writer, stopped []string) {
	switch len(stopped) {
	case 0:
		return
	case 1:
		_, _ = fmt.Fprintf(stderr, "%s was left stopped\n", diagnosticArg(stopped[0]))
	case 2:
		_, _ = fmt.Fprintf(stderr, "%s and %s were left stopped\n", diagnosticArg(stopped[0]), diagnosticArg(stopped[1]))
	default:
		_, _ = fmt.Fprintf(stderr, "%s were left stopped\n", diagnosticArg(strings.Join(stopped, ", ")))
	}
}

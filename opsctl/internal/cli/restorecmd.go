package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
	"github.com/ikigenba/ikigenba/opsctl/internal/services"
)

const restoreUsage = `Usage: opsctl restore SERVICE [--at <timestamp> | --from <uri>]

Replace /var/opt/ikigenba/SERVICE/state/ with a backup, and, when SERVICE
declares a [database], replace that database with what litestream holds.
Without --at or --from both halves are the newest there is. With --from,
everything comes from the one snapshot at that URI instead, database included.
SERVICE must be an app in the current release: its
/opt/ikigenba/current/SERVICE/etc/manifest.toml says what it declares, and
nothing under /opt/ikigenba/ is touched. An etc/ that an older backup or
snapshot holds is ignored. A SERVICE that is not in the current release is
refused.

On a host laid out per app, with no /opt/ikigenba/current, SERVICE must be
installed under /opt/SERVICE/ instead, whichever opsctl runs the restore, and
one that is not, or whose state/ or environment file is still under
/opt/SERVICE/, is refused: install it first. On a fresh host, with neither
/opt/ikigenba/current nor any app under /opt/, the opsctl inside a release,
/opt/ikigenba/releases/<sha>/opsctl/bin/opsctl, restores against that
release: SERVICE must be one of its apps, and its manifest there says what it
declares. Any other opsctl refuses to run there at all.

The environment file, /etc/opt/ikigenba/SERVICE/env, is never backed up. The
restore writes it as 'opsctl activate' does, from the parameter
/<host.name>/SERVICE, the manifest, and the commit and label of the release
it restores against, reading the parameter before anything is stopped.

SERVICE's socket and service are stopped for the restore, socket first so no
request starts the service again mid-restore, and started again after it; so
is litestream.service, because the files step deletes the database the app and
litestream both hold open. A restore is a brief outage. An app whose socket
was already stopped is left stopped, unless an earlier failed restore stopped
it: a successful retry on the same running host starts it again. A disabled
app stays disabled: neither of its units is enabled or started. A failed
restore leaves them all stopped.

Before litestream.service comes back, /etc/litestream.yml is regenerated from
the manifests of the release the restore runs against, as 'opsctl activate'
does.

Options:
  --at <timestamp>    restore the service as it was at this RFC 3339 moment
  --from <uri>        restore the service from the snapshot at this s3:// URI

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself. The two
are not the same instant, because the tarball is written on a timer and the
database is replicated continuously.

--from takes state/ and the database from a snapshot 'opsctl snapshot' wrote,
and reads neither the backups nor litestream's replica. It cannot be combined
with --at.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
  host.name       the fully-qualified name this host answers at
  apps.drain_seconds  how long the app may drain when stopped (default 5)
  backup.service_db_seconds  how often a declared database is snapshotted whole
  backup.service_wal_seconds  how often a declared database's committed changes are shipped
`

type restoreInvocation struct {
	service string
	at      *time.Time
	from    string
}

type restoreStore interface {
	Get(string) (string, error)
}

func runRestore(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	return runRestoreWithStore(args, stdout, stderr, deps, config.Store{Root: deps.Root})
}

func runRestoreWithStore(args []string, stdout, stderr io.Writer, deps Deps, store restoreStore) exitCode {
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

	hostName, err := store.Get("host.name")
	hostName = host.NormalizeName(hostName)
	if err != nil || hostName == "" {
		if err == nil || errors.Is(err, config.ErrNotSet) {
			writeDiagnostic(stderr, errors.New("host.name not set"))
			return exitFail
		}
		return configActionErr(stderr, "get", deps, err)
	}
	apexApp, err := store.Get("host.apex")
	if errors.Is(err, config.ErrNotSet) {
		apexApp = ""
	} else if err != nil {
		return configActionErr(stderr, "get", deps, err)
	}
	if apexApp != "" {
		if _, err := host.Apex(hostName); err != nil {
			writeDiagnostic(stderr, fmt.Errorf("host.apex is set but host.name '%s' has no parent domain", hostName))
			return exitFail
		}
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	var own *release.Release
	if deps.Executable != nil {
		if executable, err := deps.Executable(); err == nil {
			if found, ok := release.Of(deps.Root, executable); ok {
				own = &found
			}
		}
	}
	report, runErr := backup.Restore(
		context.Background(), env, deps.Cloud, config.Store{Root: deps.Root}, invocation.service, invocation.at, invocation.from, own,
		func(ctx context.Context) error {
			if err := nginx.Write(ctx, env, hostName, apexApp); err != nil {
				return err
			}
			_, err := services.Write(ctx, env, hostName)
			return err
		},
	)
	return renderRestoreOutcome(stdout, stderr, invocation.service, report, runErr)
}

func parseRestoreInvocation(args []string) (restoreInvocation, string) {
	var invocation restoreInvocation
	seenAt, seenFrom := false, false
	remaining := args
	for len(remaining) > 0 {
		argument := remaining[0]
		remaining = remaining[1:]
		if argument == "--at" {
			if seenAt {
				return restoreInvocation{}, "duplicate option '--at'"
			}
			seenAt = true
			if seenFrom {
				return restoreInvocation{}, "--at and --from cannot be combined"
			}
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
		if argument == "--from" {
			if seenFrom {
				return restoreInvocation{}, "duplicate option '--from'"
			}
			seenFrom = true
			if seenAt {
				return restoreInvocation{}, "--at and --from cannot be combined"
			}
			if len(remaining) == 0 {
				return restoreInvocation{}, "--from takes an s3:// URI"
			}
			value := remaining[0]
			remaining = remaining[1:]
			if !validRestoreURI(value) {
				return restoreInvocation{}, "--from takes an s3:// URI"
			}
			invocation.from = value
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
		_, _ = fmt.Fprintf(stderr, "%s, and %s were left stopped\n", diagnosticArg(strings.Join(stopped[:len(stopped)-1], ", ")), diagnosticArg(stopped[len(stopped)-1]))
	}
}

func validRestoreURI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "s3" && parsed.Hostname() != "" && parsed.Host == parsed.Hostname() && parsed.User == nil && parsed.Opaque == "" && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == "" && !strings.Contains(value, "#") && strings.HasPrefix(parsed.Path, "/") && len(parsed.Path) > 1
}

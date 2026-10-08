package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const snapshotUsage = `Usage: opsctl snapshot [SERVICE]

Copy every service's state/, /var/opt/ikigenba/SERVICE/state/, and the
database of a service that declares a [database], to snapshots/<service>/
under the prefix in backup.s3_uri, or just SERVICE when one is named. Every
snapshot of one run carries the same timestamp.

The database copy is rebuilt from the replica litestream.service keeps, so
nothing is stopped; it may trail the live database by the changes litestream
has not yet shipped. Never copied: /opt/ikigenba/, which 'opsctl activate'
brings; cache/; anything opsctl generates, the environment file that holds the
service's secrets among them; and the database's -wal and -shm and its
litestream metadata directory.

'opsctl restore SERVICE --from URI' puts a snapshot back.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
`

func runSnapshot(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, snapshotUsage)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return writeSnapshotUsageError(stderr, "unknown option '"+diagnosticArg(arg)+"'")
		}
	}
	if len(args) > 1 {
		return writeSnapshotUsageError(stderr, "snapshot takes zero or one SERVICE")
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if len(args) == 1 && args[0] == "" {
		writeDiagnostic(stderr, fmt.Errorf("invalid service %q", args[0]))
		return exitFail
	}

	service := ""
	if len(args) == 1 {
		service = args[0]
	}
	return executeSnapshot(service, stdout, stderr, deps)
}

func executeSnapshot(service string, stdout, stderr io.Writer, deps Deps) exitCode {
	results, runErr := backup.Snapshot(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}, deps.Cloud, config.Store{Root: deps.Root}, service)
	allOK, writeErr := writeSnapshotResults(stdout, results)
	if writeErr != nil {
		writeDiagnostic(stderr, snapshotDiagnostic{message: "snapshot failed", cause: writeErr})
		return exitFail
	}
	if runErr != nil {
		return writeSnapshotOperationalError(stderr, results, runErr)
	}
	if !allOK {
		return exitFail
	}
	return exitOK
}

func writeSnapshotOperationalError(stderr io.Writer, results []backup.SnapshotResult, runErr error) exitCode {
	if len(results) == 0 {
		writeDiagnostic(stderr, runErr)
		return exitFail
	}
	message := "snapshot failed"
	cause := runErr
	if interrupted := interruptedSnapshotService(results, runErr); interrupted != "" {
		message += " at " + diagnosticArg(interrupted)
		cause = results[len(results)-1].Err
	}
	writeDiagnostic(stderr, snapshotDiagnostic{message: message, cause: cause})
	return exitFail
}

func writeSnapshotUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl snapshot --help' for usage\n")
	return exitUsage
}

func writeSnapshotResults(output io.Writer, results []backup.SnapshotResult) (bool, error) {
	ordered := append([]backup.SnapshotResult(nil), results...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Service < ordered[j].Service })
	allOK := true
	var report strings.Builder
	for _, result := range ordered {
		if result.Err != nil {
			allOK = false
			_, _ = fmt.Fprintf(&report, "%s: failed: %s\n", diagnosticArg(result.Service), conciseCause(result.Err))
			continue
		}
		_, _ = fmt.Fprintf(&report, "%s: ok (%s, %s)\n",
			diagnosticArg(result.Service), diagnosticArg(result.URI), formatMebibytes(result.Size))
	}
	_, err := io.WriteString(output, report.String())
	return allOK, err
}

func interruptedSnapshotService(results []backup.SnapshotResult, runErr error) string {
	if len(results) == 0 {
		return ""
	}
	last := results[len(results)-1]
	if last.Err == nil {
		return ""
	}
	for _, interruption := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(runErr, interruption) && errors.Is(last.Err, interruption) {
			return last.Service
		}
	}
	return ""
}

type snapshotDiagnostic struct {
	message string
	cause   error
}

func (failure snapshotDiagnostic) Error() string { return failure.message }

func (failure snapshotDiagnostic) Unwrap() error { return failure.cause }

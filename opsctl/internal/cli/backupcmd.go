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

const backupUsage = `Usage: opsctl backup [SERVICE]

Copy every service's etc/ and state/ to the prefix in backup.s3_uri, under the
service's own name, or just SERVICE when one is named.

Never copied: cache/, anything opsctl generates, and -- for a service that
declares a [database] -- the database file, its -wal and -shm, and its
litestream metadata directory. Those are replicated continuously by
litestream.service. The host's own /etc/ is 'opsctl host backup'.

A service declares its database with a [database] table in etc/manifest.toml
naming its engine and its path. 'opsctl init' writes the timer that runs this
at backup.service_files_seconds.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
`

func runBackup(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, backupUsage)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return writeBackupUsageError(stderr, "unknown option '"+diagnosticArg(arg)+"'")
		}
	}
	if len(args) > 1 {
		return writeBackupUsageError(stderr, "backup takes zero or one SERVICE")
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
	return executeBackup(service, stdout, stderr, deps)
}

func executeBackup(service string, stdout, stderr io.Writer, deps Deps) exitCode {
	results, runErr := backup.Files(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}, deps.Cloud, config.Store{Root: deps.Root}, service)
	allOK, writeErr := writeBackupResults(stdout, results)
	if writeErr != nil {
		writeDiagnostic(stderr, backupDiagnostic{message: "backup failed", cause: writeErr})
		return exitFail
	}
	if runErr != nil {
		writeBackupOperationalError(stderr, results, runErr)
		return exitFail
	}
	if !allOK {
		return exitFail
	}
	return exitOK
}

func writeBackupOperationalError(stderr io.Writer, results []backup.FileResult, runErr error) {
	if len(results) == 0 {
		writeDiagnostic(stderr, runErr)
		return
	}
	message := "backup failed"
	cause := runErr
	if interrupted := interruptedBackupService(results, runErr); interrupted != "" {
		message += " at " + diagnosticArg(interrupted)
		cause = results[len(results)-1].Err
	}
	writeDiagnostic(stderr, backupDiagnostic{message: message, cause: cause})
}

func writeBackupUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl backup --help' for usage\n")
	return exitUsage
}

func writeBackupResults(output io.Writer, results []backup.FileResult) (bool, error) {
	ordered := append([]backup.FileResult(nil), results...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Service < ordered[j].Service })
	allOK := true
	var report strings.Builder
	for _, result := range ordered {
		if result.Err != nil {
			allOK = false
			_, _ = fmt.Fprintf(&report, "%s: failed: %s\n", diagnosticArg(result.Service), diagnosticArg(result.Err.Error()))
			continue
		}
		_, _ = fmt.Fprintf(&report, "%s: ok (%s, %s)\n",
			diagnosticArg(result.Service), diagnosticArg(result.Object), formatMebibytes(result.Size))
	}
	_, err := io.WriteString(output, report.String())
	return allOK, err
}

func formatMebibytes(size int64) string {
	return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
}

func interruptedBackupService(results []backup.FileResult, runErr error) string {
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

type backupDiagnostic struct {
	message string
	cause   error
}

func (failure backupDiagnostic) Error() string { return failure.message }

func (failure backupDiagnostic) Unwrap() error { return failure.cause }

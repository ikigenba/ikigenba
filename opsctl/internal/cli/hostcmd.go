package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const hostUsage = `Usage: opsctl host <subcommand>

Back up and restore the host's own configuration: /etc/ikigenba/ and
/etc/letsencrypt/, under 'host/' in backup.s3_uri. Nothing under /opt is
touched either way -- that is 'opsctl backup' and 'opsctl restore'.

Subcommands:
  backup    write /etc/ikigenba/ and /etc/letsencrypt/ to S3
  restore   replace them with the newest backup

'opsctl init' writes the timer that runs the backup at
backup.host_files_seconds.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
`

func runHost(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, hostUsage)
	}
	if len(args) == 0 {
		return writeHostUsageError(stderr, "no host subcommand given")
	}

	subcommand := args[0]
	if subcommand != "backup" && subcommand != "restore" {
		return writeHostUsageError(stderr, "unknown host subcommand '"+diagnosticArg(subcommand)+"'")
	}
	if len(args) > 1 {
		if strings.HasPrefix(args[1], "-") {
			return writeHostUsageError(stderr, "unknown option '"+diagnosticArg(args[1])+"'")
		}
		return writeHostUsageError(stderr, "host "+subcommand+" takes no arguments")
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}

	switch subcommand {
	case "backup":
		return executeHostBackup(stdout, stderr, deps)
	case "restore":
		return executeHostRestore(stdout, stderr, deps)
	default:
		panic("validated host subcommand was not dispatched")
	}
}

func writeHostUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl host --help' for usage\n")
	return exitUsage
}

func executeHostBackup(stdout, stderr io.Writer, deps Deps) exitCode {
	result, runErr := backup.HostBackup(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}, deps.Cloud, config.Store{Root: deps.Root})
	return renderHostBackupOutcome(stdout, stderr, result, runErr)
}

func renderHostBackupOutcome(stdout, stderr io.Writer, result backup.FileResult, runErr error) exitCode {
	if result.Service != "" {
		if err := writeHostBackupResult(stdout, result); err != nil {
			writeDiagnostic(stderr, hostOperationDiagnostic{message: "host backup failed", cause: err})
			return exitFail
		}
	}
	if runErr != nil {
		if result.Service == "" {
			writeDiagnostic(stderr, runErr)
		} else {
			cause := runErr
			if result.Err != nil {
				cause = result.Err
			}
			writeDiagnostic(stderr, hostOperationDiagnostic{message: "host backup failed", cause: cause})
		}
		return exitFail
	}
	if result.Err != nil {
		return exitFail
	}
	return exitOK
}

func writeHostBackupResult(output io.Writer, result backup.FileResult) error {
	if result.Err != nil {
		_, err := fmt.Fprintf(output, "host: failed: %s\n", conciseCause(result.Err))
		return err
	}
	_, err := fmt.Fprintf(output, "host: ok (%s, %s)\n", diagnosticArg(result.Object), formatKibibytes(result.Size))
	return err
}

func executeHostRestore(stdout, stderr io.Writer, deps Deps) exitCode {
	result, runErr := backup.HostRestore(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}, deps.Cloud, config.Store{Root: deps.Root})

	var report strings.Builder
	if result.SourceReady {
		_, _ = fmt.Fprintf(&report, "source: ok (%s, %s)\n", diagnosticArg(result.Object), formatKibibytes(result.Size))
	}
	if result.FilesRestored {
		_, _ = fmt.Fprintf(&report, "files: ok (/etc/ikigenba, /etc/letsencrypt, %d files)\n", result.Files)
	}
	if runErr != nil && result.FailedStep != "" {
		_, _ = fmt.Fprintf(&report, "%s: failed: %s\n", diagnosticArg(result.FailedStep), conciseCause(runErr))
	}
	if report.Len() > 0 {
		if _, err := io.WriteString(stdout, report.String()); err != nil {
			writeDiagnostic(stderr, hostOperationDiagnostic{message: "host restore failed", cause: err})
			return exitFail
		}
	}
	if runErr == nil {
		return exitOK
	}
	if result.FailedStep == "" {
		writeDiagnostic(stderr, runErr)
	} else {
		writeDiagnostic(stderr, hostOperationDiagnostic{
			message: "host restore failed at " + diagnosticArg(result.FailedStep),
			cause:   runErr,
		})
	}
	return exitFail
}

func formatKibibytes(size int64) string {
	return fmt.Sprintf("%.1f KiB", float64(size)/1024)
}

func conciseCause(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return diagnosticArg(line)
}

type hostOperationDiagnostic struct {
	message string
	cause   error
}

func (failure hostOperationDiagnostic) Error() string { return failure.message }

func (failure hostOperationDiagnostic) Unwrap() error { return failure.cause }

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

const retireUsage = `Usage: opsctl retire

Take a host's final backup before it is discarded. Stop every service, then
litestream.service so it ships every committed change it holds, then copy
every service's files and the host's own configuration to backup.s3_uri
exactly as 'opsctl backup' and 'opsctl host backup' would.

Nothing is deleted or disabled. The units are left stopped; a host kept after
all comes back with 'systemctl start' or a reboot.

Configuration keys:
  aws.region      the region the backup bucket lives in
  backup.s3_uri   the prefix this host backs up to
`

func runRetire(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, retireUsage)
	}
	if len(args) > 0 {
		if strings.HasPrefix(args[0], "-") {
			return writeRetireUsageError(stderr, "unknown option '"+diagnosticArg(args[0])+"'")
		}
		return writeRetireUsageError(stderr, "retire takes no arguments")
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}

	result, runErr := backup.Retire(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}, deps.Cloud, config.Store{Root: deps.Root})
	return renderRetireOutcome(stdout, stderr, result, runErr)
}

func writeRetireUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl retire --help' for usage\n")
	return exitUsage
}

func renderRetireOutcome(stdout, stderr io.Writer, result backup.RetireResult, runErr error) exitCode {
	var report strings.Builder
	if result.ServicesStopped {
		stopped := "none"
		if len(result.Services) > 0 {
			stopped = strings.Join(result.Services, ", ") + " stopped"
		}
		_, _ = fmt.Fprintf(&report, "services: ok (%s)\n", stopped)
	}
	if result.LitestreamStopped {
		status := "stopped"
		if len(result.SyncedDatabases) > 0 {
			status += ", " + strings.Join(result.SyncedDatabases, ", ") + " synced"
		}
		_, _ = fmt.Fprintf(&report, "litestream: ok (%s)\n", status)
	}
	archivesOK, _ := writeBackupResults(&report, result.Files)
	if result.Host.Service != "" {
		if err := writeHostBackupResult(&report, result.Host); err != nil {
			writeDiagnostic(stderr, hostOperationDiagnostic{message: "retire failed", cause: err})
			return exitFail
		}
		if result.Host.Err != nil {
			archivesOK = false
		}
	}
	if runErr != nil && result.FailedStep != "" && !retireArchiveFailureReported(result) {
		_, _ = fmt.Fprintf(&report, "%s: failed: %s\n", diagnosticArg(result.FailedStep), conciseCause(runErr))
	}
	if report.Len() > 0 {
		if _, err := io.WriteString(stdout, report.String()); err != nil {
			writeDiagnostic(stderr, hostOperationDiagnostic{message: "retire failed", cause: err})
			return exitFail
		}
	}
	if runErr != nil {
		diagnosticCause := retireDiagnosticCause(result, runErr)
		if result.FailedStep == "" {
			if report.Len() == 0 {
				writeDiagnostic(stderr, diagnosticCause)
			} else {
				writeDiagnostic(stderr, hostOperationDiagnostic{message: "retire failed", cause: diagnosticCause})
			}
		} else {
			writeDiagnostic(stderr, hostOperationDiagnostic{
				message: "retire failed at " + diagnosticArg(result.FailedStep),
				cause:   diagnosticCause,
			})
		}
		return exitFail
	}
	if !archivesOK {
		return exitFail
	}
	return exitOK
}

func retireDiagnosticCause(result backup.RetireResult, runErr error) error {
	if result.FailedStep == "host" && result.Host.Service == "host" && result.Host.Err != nil {
		return result.Host.Err
	}
	for _, archive := range result.Files {
		if archive.Service == result.FailedStep && archive.Err != nil {
			return archive.Err
		}
	}
	return runErr
}

func retireArchiveFailureReported(result backup.RetireResult) bool {
	if result.Host.Service != "" && result.Host.Err != nil && result.FailedStep == "host" {
		return true
	}
	for _, archive := range result.Files {
		if archive.Service == result.FailedStep && archive.Err != nil {
			return true
		}
	}
	return false
}

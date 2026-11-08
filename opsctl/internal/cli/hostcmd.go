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

Back up and restore the host itself, under 'host/' in backup.s3_uri: its
configuration, /etc/ikigenba/ and /etc/letsencrypt/, and its accounts, every
business group with /srv/SLUG and every user with /home/NAME and their keys,
together with sshd's host keys. No other account is backed up. Nothing under
/opt, /etc/opt, or /var/opt is touched either way -- that is 'opsctl backup'
and 'opsctl restore'.

Subcommands:
  backup    write the configuration and the accounts to S3
  restore   put back the newest backup, recreating its groups and users

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

func renderHostBackupOutcome(stdout, stderr io.Writer, result backup.HostBackupResult, runErr error) exitCode {
	attempted := false
	var cause error
	for _, archive := range []backup.FileResult{result.Accounts, result.Host} {
		if archive.Service == "" {
			continue
		}
		attempted = true
		if err := writeHostBackupResult(stdout, archive); err != nil {
			writeDiagnostic(stderr, hostOperationDiagnostic{message: "host backup failed", cause: err})
			return exitFail
		}
		if cause == nil {
			cause = archive.Err
		}
	}
	if cause == nil {
		cause = runErr
	}
	if cause != nil {
		if attempted {
			writeDiagnostic(stderr, hostOperationDiagnostic{message: "host backup failed", cause: cause})
		} else {
			writeDiagnostic(stderr, cause)
		}
		return exitFail
	}
	return exitOK
}

func writeHostBackupResult(output io.Writer, result backup.FileResult) error {
	if result.Err != nil {
		_, err := fmt.Fprintf(output, "%s: failed: %s\n", diagnosticArg(result.Service), conciseCause(result.Err))
		return err
	}
	size := formatKibibytes(result.Size)
	if result.Service == "accounts" {
		size = formatMebibytes(result.Size)
	}
	_, err := fmt.Fprintf(output, "%s: ok (%s, %s)\n", diagnosticArg(result.Service), diagnosticArg(result.Object), size)
	return err
}

func executeHostRestore(stdout, stderr io.Writer, deps Deps) exitCode {
	result, runErr := backup.HostRestore(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}, deps.Cloud, config.Store{Root: deps.Root})
	return renderHostRestoreOutcome(stdout, stderr, result, runErr)
}

func renderHostRestoreOutcome(stdout, stderr io.Writer, result backup.HostRestoreResult, runErr error) exitCode {
	var report strings.Builder
	if result.SourceReady {
		_, _ = fmt.Fprintf(&report, "source: ok (%s, %s)\n", diagnosticArg(result.Object), formatKibibytes(result.Size))
	}
	if result.AccountsReady {
		if result.AccountsObject == "" {
			_, _ = io.WriteString(&report, "accounts: ok (none in this backup)\n")
		} else {
			_, _ = fmt.Fprintf(&report, "accounts: ok (%s, %s)\n", diagnosticArg(result.AccountsObject), formatMebibytes(result.AccountsSize))
		}
	}
	if result.FilesRestored {
		_, _ = fmt.Fprintf(&report, "files: ok (/etc/ikigenba, /etc/letsencrypt, %d files)\n", result.Files)
	}
	if result.GroupsRestored {
		groups := "none"
		if len(result.Groups) > 0 {
			groups = strings.Join(result.Groups, ", ")
		}
		_, _ = fmt.Fprintf(&report, "groups: ok (%s)\n", diagnosticArg(groups))
	}
	if result.UsersRestored {
		users := "none"
		if len(result.Users) > 0 {
			locked := make(map[string]bool, len(result.Locked))
			for _, name := range result.Locked {
				locked[name] = true
			}
			names := make([]string, 0, len(result.Users))
			for _, name := range result.Users {
				if locked[name] {
					name += " locked"
				}
				names = append(names, name)
			}
			users = strings.Join(names, ", ")
			if !result.TimersInstalled {
				users += "; turns.timer not installed, no timers started"
			}
		}
		_, _ = fmt.Fprintf(&report, "users: ok (%s)\n", diagnosticArg(users))
	}
	if result.SSHRestored {
		_, _ = fmt.Fprintf(&report, "ssh: ok (%d host keys, sshd reloaded)\n", result.HostKeys)
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

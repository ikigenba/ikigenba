package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const restartUsage = `Usage: opsctl restart APP

Restart ikigenba-APP.service and report the service as the last line of
'opsctl install' does. Nothing on disk changes: the binary, the environment
file, and the unit are what the last install wrote, so a secret pushed since
then is not picked up here. A unit that is inactive or failed is started.
`

const uninstallUsage = `Usage: opsctl uninstall APP

Take APP off the host: stop and disable ikigenba-APP.service and remove it,
then remove /opt/APP/bin/, etc/, share/, and cache/. /opt/APP/state/ is kept
untouched, so APP is still a service the host backs up, and a later install
lands over its data the way an install over a restore does. Removing state/ is
a decision made by hand, never here.

The nginx configuration and /etc/litestream.yml are regenerated from every app
left on the host, so APP's name stops answering, and a database APP declared
stops being replicated once litestream has shipped what it holds.

The parameter /ikigenba/<host.name>/APP is not touched: it is devctl's.

Configuration keys:
  host.name  the fully-qualified name this host answers at
  aws.region  the region the backup bucket lives in
  backup.s3_uri  the prefix this host backs up to
  backup.service_db_seconds  how often a declared database is snapshotted whole
  backup.service_wal_seconds  how often a declared database's committed changes are shipped
`

func runLifecycleAction(name string, args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		usage := restartUsage
		if name == "uninstall" {
			usage = uninstallUsage
		}
		return writeOut(stdout, usage)
	}
	if len(args) == 0 {
		return writeLifecycleUsageError(stderr, name, name+" needs APP")
	}
	if len(args) != 1 {
		return writeLifecycleUsageError(stderr, name, name+" takes one APP")
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if name == "restart" {
		return runRestart(args[0], stdout, stderr, deps)
	}
	writeDiagnostic(stderr, errors.New(diagnosticArg(name)+": operation is not implemented"))
	return exitFail
}

func runRestart(app string, stdout, stderr io.Writer, deps Deps) exitCode {
	row, err := apps.Restart(context.Background(), host.Env{
		Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now,
	}, app)
	if err == nil {
		if _, writeErr := io.WriteString(stdout, fmt.Sprintf("service: ok (%s %s active)\n", row.Name, row.Version)); writeErr != nil {
			writeDiagnostic(stderr, &apps.LifecycleError{Code: 1, Message: "restart failed", Cause: writeErr})
			return exitFail
		}
		return exitOK
	}

	var failure *apps.LifecycleError
	if !errors.As(err, &failure) {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	if isRestartPreflightFailure(failure) {
		writeDiagnostic(stderr, failure)
		return exitCode(failure.Code)
	}

	detail := strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(failure.Message)
	_, reportErr := io.WriteString(stdout, "service: failed: "+detail+"\n")
	cause := error(failure)
	if reportErr != nil {
		cause = errors.Join(failure, reportErr)
	}
	writeDiagnostic(stderr, &apps.LifecycleError{Code: 1, Message: "restart failed", Cause: cause})
	return exitFail
}

func isRestartPreflightFailure(failure *apps.LifecycleError) bool {
	return failure.Code == 2 || strings.HasPrefix(failure.Message, "no service '") ||
		strings.HasSuffix(failure.Message, " is not installed") || strings.HasPrefix(failure.Message, "inspect service") ||
		strings.HasPrefix(failure.Message, "inspect installed app")
}

func writeLifecycleUsageError(stderr io.Writer, command, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl "+command+" --help' for usage\n")
	return exitUsage
}

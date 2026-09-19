package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
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

The parameter /<host.name>/APP is not touched: it is devctl's.

Configuration keys:
  host.name  the fully-qualified name this host answers at
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
	return runUninstall(args[0], stdout, stderr, deps)
}

func runUninstall(app string, stdout, stderr io.Writer, deps Deps) exitCode {
	if err := apps.ValidateName(app); err != nil {
		failure := &apps.LifecycleError{
			Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", diagnosticArg(app)), Cause: err,
		}
		writeDiagnostic(stderr, failure)
		return exitUsage
	}

	store := config.Store{Root: deps.Root}
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
	reported := false
	report := func(step, detail string, success bool) error {
		reported = true
		return writeInstallReport(stdout, step, detail, success)
	}
	err = apps.Uninstall(context.Background(), env, app, apps.UninstallHooks{
		Report: report,
		Configure: func(ctx context.Context, manifest apps.Manifest) error {
			return configureUninstalledApp(ctx, env, store, hostName, apexApp, manifest, report)
		},
	})
	if err == nil {
		return exitOK
	}

	var failure *apps.LifecycleError
	if !errors.As(err, &failure) {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	if !reported {
		writeDiagnostic(stderr, failure)
		return exitCode(failure.Code)
	}
	writeDiagnostic(stderr, &apps.LifecycleError{Code: 1, Message: "uninstall failed", Cause: failure.Cause})
	return exitFail
}

func configureUninstalledApp(
	ctx context.Context,
	env host.Env,
	store config.Store,
	hostName string,
	apexApp string,
	manifest apps.Manifest,
	report func(string, string, bool) error,
) error {
	names := manifest.App + "." + hostName
	if manifest.Default {
		names += ", " + hostName
	}
	if manifest.App == apexApp {
		apexName, err := host.Apex(hostName)
		if err != nil {
			return reportLifecycleConfigurationFailure(report, "nginx", err)
		}
		names += ", " + apexName
	}
	if err := nginx.Apply(ctx, env, hostName, apexApp); err != nil {
		return reportLifecycleConfigurationFailure(report, "nginx", err)
	}
	if err := report("nginx", names+" removed", true); err != nil {
		return err
	}

	changed, err := backup.Regenerate(ctx, env, store)
	if err != nil {
		return reportLifecycleConfigurationFailure(report, "litestream", err)
	}
	detail := "unchanged"
	if changed {
		detail = "updated"
		if manifest.Database != nil {
			detail = manifest.Database.Path + " removed"
		}
		if err := restartUninstallLitestream(ctx, env); err != nil {
			return reportLifecycleConfigurationFailure(report, "litestream", err)
		}
	}
	return report("litestream", detail, true)
}

func restartUninstallLitestream(ctx context.Context, env host.Env) error {
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"restart", "litestream.service"}})
	if err != nil {
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			return err
		}
		return &host.CommandError{Label: "restart litestream.service", Result: result, Err: err}
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: "restart litestream.service", Result: result}
	}
	return nil
}

func reportLifecycleConfigurationFailure(report func(string, string, bool) error, step string, cause error) error {
	if reportErr := report(step, cause.Error(), false); reportErr != nil {
		return errors.Join(cause, reportErr)
	}
	return cause
}

func runRestart(app string, stdout, stderr io.Writer, deps Deps) exitCode {
	if err := apps.ValidateName(app); err != nil {
		failure := &apps.LifecycleError{
			Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", diagnosticArg(app)), Cause: err,
		}
		writeDiagnostic(stderr, failure)
		return exitUsage
	}
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
	detail := strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(failure.Message)
	_, reportErr := io.WriteString(stdout, "service: failed: "+detail+"\n")
	cause := error(failure)
	if reportErr != nil {
		cause = errors.Join(failure, reportErr)
	}
	writeDiagnostic(stderr, &apps.LifecycleError{Code: 1, Message: "restart failed", Cause: cause})
	return exitFail
}

func writeLifecycleUsageError(stderr io.Writer, command, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl "+command+" --help' for usage\n")
	return exitUsage
}

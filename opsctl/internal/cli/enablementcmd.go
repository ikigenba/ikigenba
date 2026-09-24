package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

const disableUsage = `Usage: opsctl disable APP

Stop ikigenba-APP.socket and ikigenba-APP.service, socket first so no request
starts the service again, and disable both, so neither starts at boot or on a
request. The nginx configuration is then regenerated, so APP's names answer
503 until it is enabled. Nothing on disk under /opt/APP/ changes.
'opsctl enable APP' undoes it.

auth, the authenticator every other app is checked against, is never
disabled.

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none
`

const enableUsage = `Usage: opsctl enable APP

Enable ikigenba-APP.socket and ikigenba-APP.service and start the socket,
regenerate the nginx configuration so APP's names reach it again, then start
the service and report it as the last line of 'opsctl install' does. Nothing
on disk under /opt/APP/ changes.

Configuration keys:
  host.name  the fully-qualified name this host answers at
  host.apex  the app that answers at the parent of host.name; unset means none
`

func runEnablement(action, app string, stdout, stderr io.Writer, deps Deps) exitCode {
	if err := apps.ValidateName(app); err != nil {
		writeDiagnostic(stderr, &apps.LifecycleError{Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", diagnosticArg(app)), Cause: err})
		return exitUsage
	}
	var hostName, apexApp string
	if action != "disable" || app != "auth" {
		_, name, apex, code := lifecycleHostConfig(deps, stderr)
		if code != exitOK {
			return code
		}
		hostName, apexApp = name, apex
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	reported := false
	report := func(step, detail string, success bool) error {
		reported = true
		return writeInstallReport(stdout, step, detail, success)
	}
	hooks := apps.LifecycleHooks{
		Report: report,
		Configure: func(ctx context.Context, manifest apps.Manifest) error {
			detail := lifecycleRoutedNames(manifest, hostName, apexApp)
			changed, err := nginx.Update(ctx, env, hostName, apexApp)
			if err != nil {
				return reportLifecycleConfigurationFailure(report, "nginx", err)
			}
			if !changed {
				detail = "unchanged"
			} else if action == "disable" {
				detail += " disabled"
			}
			return report("nginx", detail, true)
		},
	}
	var err error
	if action == "disable" {
		err = apps.Disable(context.Background(), env, app, hooks)
	} else {
		err = apps.Enable(context.Background(), env, app, hooks)
	}
	if err == nil {
		return exitOK
	}
	var failure *apps.LifecycleError
	if errors.As(err, &failure) && !reported {
		writeDiagnostic(stderr, failure)
		return exitCode(failure.Code)
	}
	if !reported {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	writeDiagnostic(stderr, &apps.LifecycleError{Code: 1, Message: action + " failed", Cause: err})
	return exitFail
}

func lifecycleRoutedNames(manifest apps.Manifest, hostName, apexApp string) string {
	names := manifest.App + "." + hostName
	if manifest.Default {
		names += ", " + hostName
	}
	if manifest.App == apexApp {
		apex, _ := host.Apex(hostName) // checked before the workflow starts
		names += ", " + apex
	}
	return names
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const restartUsage = `Usage: opsctl restart APP

Restart ikigenba-APP.service and report the service as 'opsctl activate'
reports each app. The socket is never restarted: it keeps listening, so
requests that arrive during the restart wait and are answered by the new
process. Nothing on disk changes: the binary, the environment file, and the
units are what the last activate wrote, so a secret pushed since then is not
picked up here, nor a timing setting changed since then ('opsctl init'
applies those). A service that is inactive or failed is started, and so is
its socket if it was stopped. A disabled app is not started: it stays
disabled until 'opsctl enable'.
`

func runLifecycleAction(name string, args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		usage := map[string]string{"restart": restartUsage,
			"disable": disableUsage, "enable": enableUsage}[name]
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
	return runEnablement(name, args[0], stdout, stderr, deps)
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
		if _, writeErr := io.WriteString(stdout, fmt.Sprintf("service: ok (%s %s %s)\n", row.Name, row.Version, row.State)); writeErr != nil {
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

func lifecycleHostConfig(deps Deps, stderr io.Writer) (config.Store, string, string, exitCode) {
	store := config.Store{Root: deps.Root}
	hostName, err := store.Get("host.name")
	hostName = host.NormalizeName(hostName)
	if err != nil || hostName == "" {
		if err == nil || errors.Is(err, config.ErrNotSet) {
			writeDiagnostic(stderr, errors.New("host.name not set"))
			return store, "", "", exitFail
		}
		return store, "", "", configActionErr(stderr, "get", deps, err)
	}
	apexApp, err := store.Get("host.apex")
	if errors.Is(err, config.ErrNotSet) {
		apexApp = ""
	} else if err != nil {
		return store, "", "", configActionErr(stderr, "get", deps, err)
	}
	if apexApp != "" {
		if _, err := host.Apex(hostName); err != nil {
			writeDiagnostic(stderr, fmt.Errorf("host.apex is set but host.name '%s' has no parent domain", hostName))
			return store, "", "", exitFail
		}
	}
	return store, hostName, apexApp, exitOK
}

func writeLifecycleUsageError(stderr io.Writer, command, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl "+command+" --help' for usage\n")
	return exitUsage
}

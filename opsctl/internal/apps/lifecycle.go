package apps

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// UninstallHooks connects app removal to CLI-owned reporting and configuration.
type UninstallHooks struct {
	Report    func(step, detail string, success bool) error
	Configure func(context.Context, Manifest) error
}

// StatusRow contains the observable host state of one service.
type StatusRow struct {
	Name        string
	Version     string
	State       string
	Socket      string
	JournalMode string
}

// ServiceReport is the result of a service start or restart.
type ServiceReport struct {
	Name    string
	Version string
	State   string
}

// LifecycleHooks connects enable and disable to reporting and routing.
type LifecycleHooks struct {
	Report    func(step, detail string, success bool) error
	Configure func(context.Context, Manifest) error
}

// LifecycleError describes an app lifecycle failure and its process exit code.
type LifecycleError struct {
	Code    int
	Message string
	Cause   error
}

func (failure *LifecycleError) Error() string { return failure.Message }

func (failure *LifecycleError) Unwrap() error { return failure.Cause }

// Uninstall removes an installed app's executable configuration while keeping
// its state for backup and a later installation.
func Uninstall(ctx context.Context, env host.Env, app string, hooks UninstallHooks) error {
	workflow, err := prepareUninstall(env, app, hooks)
	if err != nil {
		return err
	}
	return workflow.run(ctx)
}

type uninstallWorkflow struct {
	env      host.Env
	app      string
	service  string
	socket   string
	hooks    UninstallHooks
	manifest Manifest
}

func prepareUninstall(env host.Env, app string, hooks UninstallHooks) (*uninstallWorkflow, error) {
	if err := ValidateName(app); err != nil {
		return nil, &LifecycleError{Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", safeDiagnosticToken(app)), Cause: err}
	}
	if hooks.Report == nil {
		return nil, &LifecycleError{Code: 1, Message: "uninstall report hook not set", Cause: errors.New("callback is nil")}
	}
	if hooks.Configure == nil {
		return nil, &LifecycleError{Code: 1, Message: "uninstall configure hook not set", Cause: errors.New("callback is nil")}
	}
	if env.Execute == nil {
		return nil, &LifecycleError{Code: 1, Message: "uninstall failed", Cause: errors.New("host execution is not configured")}
	}

	return &uninstallWorkflow{env: env, app: app, hooks: hooks}, nil
}

func uninstallFiles(root, app string) (Manifest, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return Manifest{}, &LifecycleError{Code: 1, Message: "inspect service failed", Cause: err}
	}
	defer func() { _ = filesystem.Close() }()

	appPath := path.Join("opt", app)
	if err := requireAppDirectory(filesystem, app); err != nil {
		return Manifest{}, err
	}
	if err := requireInstalledBinary(filesystem, app); err != nil {
		return Manifest{}, err
	}
	info, err := filesystem.Lstat(path.Join(appPath, "etc"))
	if err != nil {
		return Manifest{}, &LifecycleError{Code: 1, Message: "read installed manifest failed", Cause: err}
	}
	if !info.IsDir() {
		err = errors.New("installed manifest parent is not a directory")
		return Manifest{}, &LifecycleError{Code: 1, Message: "uninstall prerequisites failed", Cause: err}
	}
	info, err = filesystem.Lstat(path.Join(appPath, "etc", "manifest.toml"))
	if err != nil {
		return Manifest{}, &LifecycleError{Code: 1, Message: "read installed manifest failed", Cause: err}
	}
	if !info.Mode().IsRegular() {
		err = errors.New("installed manifest is not a regular file")
		return Manifest{}, &LifecycleError{Code: 1, Message: "uninstall prerequisites failed", Cause: err}
	}
	manifestData, err := filesystem.ReadFile(path.Join(appPath, "etc", "manifest.toml"))
	if err != nil {
		return Manifest{}, &LifecycleError{Code: 1, Message: "read installed manifest failed", Cause: err}
	}
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return Manifest{}, &LifecycleError{Code: 1, Message: "installed manifest is invalid", Cause: err}
	}
	if manifest.App != app {
		err = fmt.Errorf("manifest app %q does not match service directory %q", manifest.App, app)
		return Manifest{}, &LifecycleError{Code: 1, Message: "uninstall prerequisites failed", Cause: err}
	}
	for _, unit := range []string{socketUnitName(app), appUnitName(app)} {
		if _, err := filesystem.Lstat(path.Join("etc", "systemd", "system", unit)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return Manifest{}, &LifecycleError{Code: 1, Message: safeDiagnosticToken(app) + " is not installed", Cause: err}
			}
			return Manifest{}, &LifecycleError{Code: 1, Message: "inspect installed unit failed", Cause: err}
		}
	}
	return manifest, nil
}

func socketUnitName(app string) string { return "ikigenba-" + app + ".socket" }

func uninstallUnitActive(ctx context.Context, env host.Env, unit string) (bool, error) {
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"is-active", unit}})
	if err != nil {
		cause := commandTransportError(fmt.Sprintf("%s state", safeDiagnosticToken(unit)), err)
		return false, &LifecycleError{Code: 1, Message: "inspect service failed", Cause: cause}
	}
	switch result.ExitCode {
	case 0:
		return true, nil
	case 3, 4:
		return false, nil
	default:
		cause := &host.CommandError{Label: fmt.Sprintf("%s state", safeDiagnosticToken(unit)), Result: result}
		return false, &LifecycleError{Code: 1, Message: "inspect service failed", Cause: cause}
	}
}

func (workflow *uninstallWorkflow) run(ctx context.Context) error {
	if err := workflow.stop(ctx); err != nil {
		return err
	}
	if err := workflow.removeUnit(ctx); err != nil {
		return err
	}
	if err := workflow.removeFiles(); err != nil {
		return err
	}
	if err := workflow.hooks.Configure(ctx, workflow.manifest); err != nil {
		return &LifecycleError{Code: 1, Message: "uninstall failed", Cause: err}
	}
	return nil
}

func (workflow *uninstallWorkflow) stop(ctx context.Context) error {
	manifest, err := uninstallFiles(workflow.env.Root, workflow.app)
	if err != nil {
		return workflow.failPrerequisite(err)
	}
	workflow.manifest = manifest
	workflow.service, workflow.socket = appUnitName(workflow.app), socketUnitName(workflow.app)
	socketActive, err := uninstallUnitActive(ctx, workflow.env, workflow.socket)
	if err != nil {
		return workflow.fail("stop", err)
	}
	serviceActive, err := uninstallUnitActive(ctx, workflow.env, workflow.service)
	if err != nil {
		return workflow.fail("stop", err)
	}
	for _, unit := range []string{workflow.socket, workflow.service} {
		if err := executeInstallCommand(ctx, workflow.env, "stop "+safeDiagnosticToken(unit), host.Command{Name: "systemctl", Args: []string{"stop", unit}}); err != nil {
			return workflow.fail("stop", err)
		}
	}
	if err := executeInstallCommand(ctx, workflow.env, "disable app units", host.Command{
		Name: "systemctl", Args: []string{"disable", workflow.socket, workflow.service},
	}); err != nil {
		return workflow.fail("stop", err)
	}
	detail := safeDiagnosticToken(workflow.socket) + ", " + safeDiagnosticToken(workflow.service) + " already inactive, disabled"
	if socketActive || serviceActive {
		detail = safeDiagnosticToken(workflow.socket) + ", " + safeDiagnosticToken(workflow.service) + " stopped, disabled"
	}
	return workflow.report("stop", detail)
}

func (workflow *uninstallWorkflow) failPrerequisite(err error) error {
	var failure *LifecycleError
	message := err.Error()
	if errors.As(err, &failure) {
		message = failure.Message
	}
	if reportErr := workflow.hooks.Report("stop", message, false); reportErr != nil {
		return &LifecycleError{Code: 1, Message: message, Cause: errors.Join(err, reportErr)}
	}
	return err
}

func (workflow *uninstallWorkflow) removeUnit(ctx context.Context) error {
	filesystem, err := os.OpenRoot(workflow.env.Root)
	if err == nil {
		for _, unit := range []string{workflow.socket, workflow.service} {
			if err = filesystem.Remove(path.Join("etc", "systemd", "system", unit)); err != nil {
				break
			}
		}
		err = errors.Join(err, filesystem.Close())
	}
	if err != nil {
		return workflow.fail("unit", err)
	}
	if err := executeInstallCommand(ctx, workflow.env, "reload systemd units", host.Command{
		Name: "systemctl", Args: []string{"daemon-reload"},
	}); err != nil {
		return workflow.fail("unit", err)
	}
	return workflow.report("unit", "removed "+safeDiagnosticToken(workflow.socket)+", "+safeDiagnosticToken(workflow.service))
}

func (workflow *uninstallWorkflow) removeFiles() error {
	filesystem, err := os.OpenRoot(workflow.env.Root)
	if err == nil {
		for _, directory := range []string{"bin", "etc", "share", "cache"} {
			if removeErr := filesystem.RemoveAll(path.Join("opt", workflow.app, directory)); removeErr != nil {
				err = removeErr
				break
			}
		}
		err = errors.Join(err, filesystem.Close())
	}
	if err != nil {
		return workflow.fail("files", err)
	}
	return workflow.report("files", fmt.Sprintf("removed /opt/%s/bin, etc, share, cache; kept state", safeDiagnosticToken(workflow.app)))
}

func (workflow *uninstallWorkflow) report(step, detail string) error {
	if err := workflow.hooks.Report(step, detail, true); err != nil {
		return &LifecycleError{Code: 1, Message: "uninstall failed", Cause: err}
	}
	return nil
}

func (workflow *uninstallWorkflow) fail(step string, cause error) error {
	reportErr := workflow.hooks.Report(step, cause.Error(), false)
	if reportErr != nil {
		cause = errors.Join(cause, reportErr)
	}
	return &LifecycleError{Code: 1, Message: "uninstall failed", Cause: cause}
}

func unitProperties(ctx context.Context, env host.Env, unit string, names ...string) (map[string]string, error) {
	args := []string{"show"}
	for _, name := range names {
		args = append(args, "--property="+name)
	}
	args = append(args, unit)
	if env.Execute == nil {
		return nil, commandTransportError("inspect "+unit, errors.New("host execution is not configured"))
	}
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: args})
	if err != nil {
		return nil, commandTransportError("inspect "+unit, err)
	}
	if result.ExitCode != 0 {
		return nil, &host.CommandError{Label: "inspect " + unit, Result: result}
	}
	properties := make(map[string]string, len(names))
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if ok {
			properties[key] = value
		}
	}
	return properties, nil
}

// Disabled observes only the socket unit's enablement.
func Disabled(ctx context.Context, env host.Env, app string) (bool, error) {
	if err := ValidateName(app); err != nil {
		return false, &LifecycleError{Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", safeDiagnosticToken(app)), Cause: err}
	}
	properties, err := unitProperties(ctx, env, socketUnitName(app), "LoadState", "UnitFileState")
	if err != nil {
		return false, err
	}
	return properties["LoadState"] == "loaded" && properties["UnitFileState"] == "disabled", nil
}

func lifecyclePrerequisites(root, app string) (Manifest, error) { return uninstallFiles(root, app) }

func lifecycleHookError(action string) error {
	return &LifecycleError{Code: 1, Message: action + " hooks not set", Cause: errors.New("callback is nil")}
}

func lifecycleOutcome(hooks LifecycleHooks, step, detail string, success bool, cause error) error {
	reportErr := hooks.Report(step, detail, success)
	if reportErr != nil {
		cause = errors.Join(cause, reportErr)
	}
	if cause != nil {
		return &LifecycleError{Code: 1, Message: detail, Cause: cause}
	}
	return nil
}

func lifecycleFail(hooks LifecycleHooks, step string, err error) error {
	var failure *LifecycleError
	message := err.Error()
	if errors.As(err, &failure) {
		message = failure.Message
	}
	return lifecycleOutcome(hooks, step, message, false, err)
}

func lifecycleUnitStates(ctx context.Context, env host.Env, app string) ([2]map[string]string, error) {
	var states [2]map[string]string
	for i, unit := range []string{socketUnitName(app), appUnitName(app)} {
		properties, err := unitProperties(ctx, env, unit, "ActiveState", "UnitFileState")
		if err != nil {
			return states, err
		}
		states[i] = properties
	}
	return states, nil
}

func lifecycleCommand(ctx context.Context, env host.Env, args ...string) error {
	return executeInstallCommand(ctx, env, strings.Join(args, " "), host.Command{Name: "systemctl", Args: args})
}

// Disable stops and disables both units before routing is reconfigured.
func Disable(ctx context.Context, env host.Env, app string, hooks LifecycleHooks) error {
	if err := ValidateName(app); err != nil {
		return &LifecycleError{Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", safeDiagnosticToken(app)), Cause: err}
	}
	if app == "auth" {
		message := "auth is the authenticator and cannot be disabled"
		if hooks.Report == nil {
			return &LifecycleError{Code: 1, Message: message, Cause: errors.New("report callback is nil")}
		}
		return lifecycleOutcome(hooks, "stop", message, false, errors.New(message))
	}
	if hooks.Report == nil || hooks.Configure == nil {
		return lifecycleHookError("disable")
	}
	manifest, err := lifecyclePrerequisites(env.Root, app)
	if err != nil {
		return lifecycleFail(hooks, "stop", err)
	}
	states, err := lifecycleUnitStates(ctx, env, app)
	if err != nil {
		return lifecycleFail(hooks, "stop", err)
	}
	socket, service := socketUnitName(app), appUnitName(app)
	active := states[0]["ActiveState"] == "active" || states[1]["ActiveState"] == "active"
	already := !active && states[0]["UnitFileState"] == "disabled" && states[1]["UnitFileState"] == "disabled"
	if !already {
		for _, unit := range []string{socket, service} {
			if err := lifecycleCommand(ctx, env, "stop", unit); err != nil {
				return lifecycleFail(hooks, "stop", err)
			}
		}
		if err := lifecycleCommand(ctx, env, "disable", socket, service); err != nil {
			return lifecycleFail(hooks, "stop", err)
		}
	}
	detail := socket + ", " + service + " already inactive, disabled"
	if active {
		detail = socket + ", " + service + " stopped, disabled"
	}
	if err := lifecycleOutcome(hooks, "stop", detail, true, nil); err != nil {
		return err
	}
	if err := hooks.Configure(ctx, manifest); err != nil {
		return &LifecycleError{Code: 1, Message: "disable failed", Cause: err}
	}
	return nil
}

// Enable enables both units, starts the socket, reconfigures routing, and starts the service.
func Enable(ctx context.Context, env host.Env, app string, hooks LifecycleHooks) error {
	if err := ValidateName(app); err != nil {
		return &LifecycleError{Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", safeDiagnosticToken(app)), Cause: err}
	}
	if hooks.Report == nil || hooks.Configure == nil {
		return lifecycleHookError("enable")
	}
	manifest, err := lifecyclePrerequisites(env.Root, app)
	if err != nil {
		return lifecycleFail(hooks, "enable", err)
	}
	states, err := lifecycleUnitStates(ctx, env, app)
	if err != nil {
		return lifecycleFail(hooks, "enable", err)
	}
	socket, service := socketUnitName(app), appUnitName(app)
	already := states[0]["UnitFileState"] == "enabled" && states[1]["UnitFileState"] == "enabled"
	if !already {
		if err := lifecycleCommand(ctx, env, "enable", socket, service); err != nil {
			return lifecycleFail(hooks, "enable", err)
		}
	}
	if states[0]["ActiveState"] != "active" {
		if err := lifecycleCommand(ctx, env, "start", socket); err != nil {
			return lifecycleFail(hooks, "enable", err)
		}
	}
	detail := socket + ", " + service
	if already {
		detail += " already enabled"
	}
	if err := lifecycleOutcome(hooks, "enable", detail, true, nil); err != nil {
		return err
	}
	if err := hooks.Configure(ctx, manifest); err != nil {
		return &LifecycleError{Code: 1, Message: "enable failed", Cause: err}
	}
	if states[1]["ActiveState"] != "active" {
		if err := lifecycleCommand(ctx, env, "start", service); err != nil {
			return lifecycleFail(hooks, "service", restartStartFailure(ctx, env, app, service, err))
		}
	}
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"is-active", service}})
	if err != nil {
		return lifecycleFail(hooks, "service", restartStartFailure(ctx, env, app, service, commandTransportError("inspect "+service, err)))
	}
	if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "active" {
		return lifecycleFail(hooks, "service", restartStartFailure(ctx, env, app, service, &host.CommandError{Label: "inspect " + service, Result: result}))
	}
	version, err := restartVersion(ctx, env, app)
	if err != nil {
		return lifecycleFail(hooks, "service", err)
	}
	return lifecycleOutcome(hooks, "service", app+" "+version+" active", true, nil)
}

// Restart restarts an installed app and reports its resulting service state.
func Restart(ctx context.Context, env host.Env, app string) (ServiceReport, error) {
	if err := ValidateName(app); err != nil {
		return ServiceReport{}, &LifecycleError{Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", safeDiagnosticToken(app)), Cause: err}
	}
	if env.Execute == nil {
		err := errors.New("host execution is not configured")
		return ServiceReport{}, &LifecycleError{Code: 1, Message: "restart failed", Cause: err}
	}
	if err := restartPrerequisites(env.Root, app); err != nil {
		return ServiceReport{}, err
	}
	disabled, err := Disabled(ctx, env, app)
	if err != nil {
		return ServiceReport{}, &LifecycleError{Code: 1, Message: "inspect app enablement failed", Cause: err}
	}
	if !disabled {
		if err := restartInstalledUnit(ctx, env, app); err != nil {
			return ServiceReport{}, err
		}
	}
	version, err := restartVersion(ctx, env, app)
	if err != nil {
		return ServiceReport{}, err
	}
	state := "active"
	if disabled {
		state = "disabled"
	}
	return ServiceReport{Name: app, Version: version, State: state}, nil
}

func restartInstalledUnit(ctx context.Context, env host.Env, app string) error {
	unit := appUnitName(app)
	if err := executeInstallCommand(ctx, env, fmt.Sprintf("restart %s", safeDiagnosticToken(unit)), host.Command{
		Name: "systemctl", Args: []string{"restart", unit},
	}); err != nil {
		return restartStartFailure(ctx, env, app, unit, err)
	}
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"is-active", unit}})
	if err != nil {
		failure := commandTransportError(fmt.Sprintf("inspect %s", safeDiagnosticToken(unit)), err)
		return restartStartFailure(ctx, env, app, unit, failure)
	}
	if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "active" {
		failure := &host.CommandError{Label: fmt.Sprintf("inspect %s", safeDiagnosticToken(unit)), Result: result}
		return restartStartFailure(ctx, env, app, unit, failure)
	}
	return nil
}

func restartVersion(ctx context.Context, env host.Env, app string) (string, error) {
	binary := rootedHostPath(env.Root, "opt", app, "bin", app)
	result, err := env.Execute(ctx, host.Command{Name: binary, Args: []string{"--version"}})
	if err != nil {
		cause := commandTransportError(fmt.Sprintf("execute %s binary", safeDiagnosticToken(app)), err)
		return "", &LifecycleError{Code: 1, Message: "read app version failed", Cause: cause}
	}
	if result.ExitCode != 0 {
		cause := &host.CommandError{Label: fmt.Sprintf("execute %s binary", safeDiagnosticToken(app)), Result: result}
		return "", &LifecycleError{Code: 1, Message: "read app version failed", Cause: cause}
	}
	return trimVersionLineEnding(string(result.Stdout)), nil
}

func restartPrerequisites(root, app string) error {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return &LifecycleError{Code: 1, Message: "inspect service failed", Cause: err}
	}
	defer func() { _ = filesystem.Close() }()

	if err := requireAppDirectory(filesystem, app); err != nil {
		return err
	}
	return requireInstalledBinary(filesystem, app)
}

func requireAppDirectory(filesystem *os.Root, app string) error {
	info, err := filesystem.Lstat(path.Join("opt", app))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &LifecycleError{Code: 1, Message: fmt.Sprintf("no service '%s'", safeDiagnosticToken(app)), Cause: err}
		}
		return &LifecycleError{Code: 1, Message: "inspect service failed", Cause: err}
	}
	if !info.IsDir() {
		err = errors.New("service path is not a directory")
		return &LifecycleError{Code: 1, Message: "invalid service layout", Cause: err}
	}
	return nil
}

func requireInstalledBinary(filesystem *os.Root, app string) error {
	appPath := path.Join("opt", app)
	info, err := filesystem.Lstat(path.Join(appPath, "bin"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &LifecycleError{Code: 1, Message: safeDiagnosticToken(app) + " is not installed", Cause: err}
		}
		return &LifecycleError{Code: 1, Message: "inspect installed app failed", Cause: err}
	}
	if !info.IsDir() {
		err = errors.New("installed bin path is not a directory")
		return &LifecycleError{Code: 1, Message: "invalid installation layout", Cause: err}
	}
	info, err = filesystem.Lstat(path.Join(appPath, "bin", app))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &LifecycleError{Code: 1, Message: safeDiagnosticToken(app) + " is not installed", Cause: err}
		}
		return &LifecycleError{Code: 1, Message: "inspect installed app failed", Cause: err}
	}
	if !info.Mode().IsRegular() {
		err = errors.New("installed app binary is not a regular file")
		return &LifecycleError{Code: 1, Message: "invalid installation layout", Cause: err}
	}
	return nil
}

func restartStartFailure(ctx context.Context, env host.Env, app, unit string, startErr error) *LifecycleError {
	message := fmt.Sprintf("%s: service failed to start", safeDiagnosticToken(app))
	result, err := env.Execute(ctx, host.Command{
		Name: "journalctl", Args: []string{"--unit", unit, "--no-pager", "--lines", "50"},
	})
	if err != nil {
		journalErr := commandTransportError(fmt.Sprintf("obtain %s journal", safeDiagnosticToken(unit)), err)
		return &LifecycleError{Code: 1, Message: message, Cause: errors.Join(startErr, journalErr)}
	}
	if result.ExitCode != 0 {
		journalErr := &host.CommandError{Label: fmt.Sprintf("obtain %s journal", safeDiagnosticToken(unit)), Result: result}
		return &LifecycleError{Code: 1, Message: message, Cause: errors.Join(startErr, journalErr)}
	}
	cause := &host.CommandError{Label: "journal captured", Result: result, Err: startErr}
	return &LifecycleError{Code: 1, Message: message, Cause: cause}
}

// Status reports the independently observable state of every discovered service.
func Status(ctx context.Context, env host.Env) ([]StatusRow, error) {
	services, err := Discover(env.Root)
	if err != nil {
		return nil, &LifecycleError{Code: 1, Message: "status failed", Cause: err}
	}

	rows := make([]StatusRow, 0, len(services))
	for _, service := range services {
		row := StatusRow{
			Name:        service.Name,
			Version:     serviceVersion(ctx, env, service.Name),
			State:       serviceState(ctx, env, service.Name),
			Socket:      socketState(ctx, env, service.Name),
			JournalMode: serviceJournalMode(env.Root, service),
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func serviceVersion(ctx context.Context, env host.Env, name string) string {
	if env.Execute == nil {
		return "-"
	}
	binary := rootedHostPath(env.Root, "opt", name, "bin", name)
	result, err := env.Execute(ctx, host.Command{Name: binary, Args: []string{"--version"}})
	if err != nil || result.ExitCode != 0 {
		return "-"
	}
	version := trimVersionLineEnding(string(result.Stdout))
	if version == "" {
		return "-"
	}
	return version
}

func trimVersionLineEnding(version string) string {
	if strings.HasSuffix(version, "\r\n") {
		return strings.TrimSuffix(version, "\r\n")
	}
	return strings.TrimSuffix(version, "\n")
}

func serviceState(ctx context.Context, env host.Env, name string) string {
	if ValidateName(name) != nil || env.Execute == nil {
		return "-"
	}
	properties, err := unitProperties(ctx, env, appUnitName(name), "LoadState", "ActiveState")
	if err != nil || properties["LoadState"] != "loaded" || properties["ActiveState"] == "" {
		return "-"
	}
	return properties["ActiveState"]
}

func socketState(ctx context.Context, env host.Env, name string) string {
	if ValidateName(name) != nil || env.Execute == nil {
		return "-"
	}
	properties, err := unitProperties(ctx, env, socketUnitName(name), "LoadState", "ActiveState", "UnitFileState")
	if err != nil || properties["LoadState"] != "loaded" {
		return "-"
	}
	if properties["UnitFileState"] == "disabled" {
		return "disabled"
	}
	if properties["ActiveState"] == "" {
		return "-"
	}
	return properties["ActiveState"]
}

func serviceJournalMode(root string, service Service) string {
	if service.ManifestError != nil || service.Manifest == nil || service.Manifest.Database == nil {
		return "-"
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return "-"
	}
	defer func() { _ = filesystem.Close() }()

	databasePath := path.Join("opt", service.Name, service.Manifest.Database.Path)
	file, err := filesystem.Open(databasePath)
	if err != nil {
		return "-"
	}
	defer func() { _ = file.Close() }()
	return sqliteJournalMode(file)
}

func sqliteJournalMode(file *os.File) string {
	header := make([]byte, 100)
	if _, err := file.ReadAt(header, 0); err != nil {
		return "-"
	}
	pageSize := sqlitePageSize(binary.BigEndian.Uint16(header[16:18]))
	if string(header[:16]) != "SQLite format 3\x00" || pageSize == 0 {
		return "-"
	}
	info, err := file.Stat()
	if err != nil || info.Size() < pageSize || info.Size()%pageSize != 0 {
		return "-"
	}
	if header[21] != 64 || header[22] != 32 || header[23] != 32 {
		return "-"
	}
	if schema := binary.BigEndian.Uint32(header[44:48]); schema < 1 || schema > 4 {
		return "-"
	}
	if encoding := binary.BigEndian.Uint32(header[56:60]); encoding < 1 || encoding > 3 {
		return "-"
	}
	if header[18] != header[19] {
		return "-"
	}
	switch header[18] {
	case 1:
		return "delete"
	case 2:
		return "wal"
	default:
		return "-"
	}
}

func sqlitePageSize(encoded uint16) int64 {
	if encoded == 1 {
		return 65536
	}
	if encoded >= 512 && encoded <= 32768 && encoded&(encoded-1) == 0 {
		return int64(encoded)
	}
	return 0
}

var _ error = (*LifecycleError)(nil)
var _ interface{ Unwrap() error } = (*LifecycleError)(nil)

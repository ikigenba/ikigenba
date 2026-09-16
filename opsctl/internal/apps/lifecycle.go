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
	JournalMode string
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
	workflow, err := prepareUninstall(ctx, env, app, hooks)
	if err != nil {
		return err
	}
	return workflow.run(ctx)
}

type uninstallWorkflow struct {
	env      host.Env
	app      string
	unit     string
	hooks    UninstallHooks
	manifest Manifest
	active   bool
}

func prepareUninstall(ctx context.Context, env host.Env, app string, hooks UninstallHooks) (*uninstallWorkflow, error) {
	if err := ValidateName(app); err != nil {
		return nil, &LifecycleError{Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", safeDiagnosticToken(app)), Cause: err}
	}
	if hooks.Report == nil {
		return nil, &LifecycleError{Code: 1, Message: "uninstall report hook not set", Cause: errors.New("report callback is nil")}
	}
	if hooks.Configure == nil {
		return nil, &LifecycleError{Code: 1, Message: "uninstall configure hook not set", Cause: errors.New("configure callback is nil")}
	}
	if env.Execute == nil {
		return nil, &LifecycleError{Code: 1, Message: "uninstall failed", Cause: errors.New("host execution is not configured")}
	}

	manifest, unit, err := uninstallFiles(env.Root, app)
	if err != nil {
		return nil, err
	}
	active, err := uninstallUnitActive(ctx, env, unit)
	if err != nil {
		return nil, err
	}
	return &uninstallWorkflow{env: env, app: app, unit: unit, hooks: hooks, manifest: manifest, active: active}, nil
}

func uninstallFiles(root, app string) (Manifest, string, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "inspect service failed", Cause: err}
	}
	defer func() { _ = filesystem.Close() }()

	appPath := path.Join("opt", app)
	if err := requireAppDirectory(filesystem, app); err != nil {
		return Manifest{}, "", err
	}
	if err := requireInstalledBinary(filesystem, app); err != nil {
		return Manifest{}, "", err
	}
	info, err := filesystem.Lstat(path.Join(appPath, "etc"))
	if err != nil {
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "read installed manifest failed", Cause: err}
	}
	if !info.IsDir() {
		err = errors.New("installed manifest parent is not a directory")
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "uninstall prerequisites failed", Cause: err}
	}
	info, err = filesystem.Lstat(path.Join(appPath, "etc", "manifest.toml"))
	if err != nil {
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "read installed manifest failed", Cause: err}
	}
	if !info.Mode().IsRegular() {
		err = errors.New("installed manifest is not a regular file")
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "uninstall prerequisites failed", Cause: err}
	}
	manifestData, err := filesystem.ReadFile(path.Join(appPath, "etc", "manifest.toml"))
	if err != nil {
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "read installed manifest failed", Cause: err}
	}
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "installed manifest is invalid", Cause: err}
	}
	if manifest.App != app {
		err = fmt.Errorf("manifest app %q does not match service directory %q", manifest.App, app)
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "uninstall prerequisites failed", Cause: err}
	}
	unit := appUnitName(app)
	if _, err := filesystem.Lstat(path.Join("etc", "systemd", "system", unit)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, "", &LifecycleError{Code: 1, Message: safeDiagnosticToken(unit) + " is not installed", Cause: err}
		}
		return Manifest{}, "", &LifecycleError{Code: 1, Message: "inspect installed unit failed", Cause: err}
	}
	return manifest, unit, nil
}

func uninstallUnitActive(ctx context.Context, env host.Env, unit string) (bool, error) {
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"is-active", unit}})
	if err != nil {
		cause := commandTransportError(fmt.Sprintf("inspect %s", safeDiagnosticToken(unit)), err)
		return false, &LifecycleError{Code: 1, Message: "inspect service failed", Cause: cause}
	}
	switch result.ExitCode {
	case 0:
		return true, nil
	case 3, 4:
		return false, nil
	default:
		cause := &host.CommandError{Label: fmt.Sprintf("inspect %s", safeDiagnosticToken(unit)), Result: result}
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
	safeUnit := safeDiagnosticToken(workflow.unit)
	if workflow.active {
		if err := executeInstallCommand(ctx, workflow.env, "stop "+safeUnit, host.Command{
			Name: "systemctl", Args: []string{"stop", workflow.unit},
		}); err != nil {
			return workflow.fail("stop", err)
		}
	}
	if err := executeInstallCommand(ctx, workflow.env, "disable "+safeUnit, host.Command{
		Name: "systemctl", Args: []string{"disable", workflow.unit},
	}); err != nil {
		return workflow.fail("stop", err)
	}
	detail := safeUnit + " already inactive, disabled"
	if workflow.active {
		detail = safeUnit + " stopped, disabled"
	}
	return workflow.report("stop", detail)
}

func (workflow *uninstallWorkflow) removeUnit(ctx context.Context) error {
	filesystem, err := os.OpenRoot(workflow.env.Root)
	if err == nil {
		err = filesystem.Remove(path.Join("etc", "systemd", "system", workflow.unit))
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
	return workflow.report("unit", "removed "+safeDiagnosticToken(workflow.unit))
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

// Restart restarts an installed app and reports its resulting service state.
func Restart(ctx context.Context, env host.Env, app string) (StatusRow, error) {
	if err := ValidateName(app); err != nil {
		return StatusRow{}, &LifecycleError{Code: 2, Message: fmt.Sprintf("'%s' is not a usable app name", safeDiagnosticToken(app)), Cause: err}
	}
	if err := restartPrerequisites(env.Root, app); err != nil {
		return StatusRow{}, err
	}
	if env.Execute == nil {
		err := errors.New("host execution is not configured")
		return StatusRow{}, &LifecycleError{Code: 1, Message: "restart failed", Cause: err}
	}

	if err := restartInstalledUnit(ctx, env, app); err != nil {
		return StatusRow{}, err
	}
	version, err := restartVersion(ctx, env, app)
	if err != nil {
		return StatusRow{}, err
	}
	return StatusRow{Name: app, Version: version, State: "active", JournalMode: "-"}, nil
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
		cause := commandTransportError(fmt.Sprintf("read %s version", safeDiagnosticToken(app)), err)
		return "", &LifecycleError{Code: 1, Message: "read app version failed", Cause: cause}
	}
	if result.ExitCode != 0 {
		cause := &host.CommandError{Label: fmt.Sprintf("read %s version", safeDiagnosticToken(app)), Result: result}
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
	cause := &host.CommandError{Label: "journal captured after service startup failure", Result: result, Err: startErr}
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
	result, err := env.Execute(ctx, host.Command{
		Name: "systemctl",
		Args: []string{"show", "--property=LoadState", "--property=ActiveState", appUnitName(name)},
	})
	if err != nil || result.ExitCode != 0 {
		return "-"
	}

	properties := make(map[string]string, 2)
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		key, value, found := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if found {
			properties[key] = value
		}
	}
	if properties["LoadState"] != "loaded" {
		return "-"
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

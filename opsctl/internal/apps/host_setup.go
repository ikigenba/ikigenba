package apps

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// EnsureDataDirectory prepares missing data directories without changing existing ones.
func EnsureDataDirectory(ctx context.Context, env host.Env, app string) error {
	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	for _, directory := range []string{"var", "var/opt", strings.TrimPrefix(DataRoot, "/"), path.Join(strings.TrimPrefix(DataRoot, "/"), app)} {
		info, err := filesystem.Lstat(directory)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", directory)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		mode := fs.FileMode(0o755)
		isApp := directory == path.Join(strings.TrimPrefix(DataRoot, "/"), app)
		if isApp {
			mode = 0o750
		}
		if err := filesystem.Mkdir(directory, mode); err != nil {
			return err
		}
		if err := filesystem.Chmod(directory, mode); err != nil {
			return err
		}
		if isApp {
			if err := executeAppCommand(ctx, env, "set "+app+" data ownership", host.Command{Name: "chown", Args: []string{"ikigenba:ikigenba", rootedHostPath(env.Root, DataRoot, app)}}); err != nil {
				return err
			}
		}
	}
	return nil
}

func safeDiagnosticToken(value string) string {
	for _, character := range value {
		if character < ' ' || character == '\u007f' {
			return fmt.Sprintf("%q", value)
		}
	}
	return value
}

func readSlice(root, unit string, needsMemory bool) (int64, error) {
	hostPath := "/etc/systemd/system/" + unit
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", hostPath, err)
	}
	defer func() { _ = filesystem.Close() }()
	relative := "etc/systemd/system/" + unit
	var data []byte
	if needsMemory {
		data, err = filesystem.ReadFile(relative)
	} else {
		_, err = filesystem.Stat(relative)
	}
	if errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("%s is missing; run 'opsctl init'", hostPath)
	}
	if err != nil {
		return 0, fmt.Errorf("%s: %w", hostPath, err)
	}
	if !needsMemory {
		return 0, nil
	}
	value := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemoryMax=") {
			value = strings.TrimPrefix(line, "MemoryMax=")
		}
	}
	count, ok := memoryBytes(tomlValue{kind: tomlString, text: value})
	if !ok {
		return 0, fmt.Errorf("%s has no MemoryMax; run 'opsctl init'", hostPath)
	}
	return count, nil
}

func renderMemory(count *big.Int) string {
	for _, scale := range []struct {
		bytes  int64
		suffix string
	}{{1048576, "M"}, {1024, "K"}} {
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(count, big.NewInt(scale.bytes), remainder)
		if remainder.Sign() == 0 {
			return quotient.String() + scale.suffix
		}
	}
	return count.String()
}

func obtainSecrets(ctx context.Context, client cloud.Client, hostName string, manifest Manifest) (map[string]string, error) {
	names := distinctNames(manifest.Secrets)
	parameter := "/" + hostName + "/" + manifest.App
	values := map[string]string{}
	if len(names) > 0 {
		var err error
		values, err = client.ReadSecrets(ctx, parameter)
		if errors.Is(err, cloud.ErrNotFound) {
			values = map[string]string{}
		} else if err != nil {
			return nil, err
		}
		if values == nil {
			values = map[string]string{}
		}
	}
	for _, name := range names {
		if _, present := values[name]; !present {
			err := fmt.Errorf("%s: no value for '%s' in %s", safeDiagnosticToken(manifest.App), safeDiagnosticToken(name), safeDiagnosticToken(parameter))
			return nil, err
		}
	}
	if err := validateEnvironment(manifest, values); err != nil {
		return nil, err
	}
	return values, nil
}

func distinctNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	distinct := make([]string, 0, len(names))
	for _, name := range names {
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		distinct = append(distinct, name)
	}
	return distinct
}

func validateEnvironment(manifest Manifest, secrets map[string]string) error {
	secretNames := distinctNames(manifest.Secrets)
	secretSet := make(map[string]struct{}, len(secretNames))
	for _, name := range secretNames {
		if !validEnvironmentName(name) {
			return fmt.Errorf("%s: invalid secret name %q", safeDiagnosticToken(manifest.App), name)
		}
		if name == "DRAIN_SECONDS" || name == ServicesEnv || name == "PORT" {
			return fmt.Errorf("%s: secret name %s is reserved", safeDiagnosticToken(manifest.App), name)
		}
		secretSet[name] = struct{}{}
		if invalidEnvironmentValue(secrets[name]) {
			return fmt.Errorf("%s: secret %q contains a forbidden character", safeDiagnosticToken(manifest.App), name)
		}
	}
	names := make([]string, 0, len(manifest.Env))
	for name := range manifest.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := manifest.Env[name]
		if !validEnvironmentName(name) {
			return fmt.Errorf("%s: invalid setting name %q", safeDiagnosticToken(manifest.App), name)
		}
		if name == "DRAIN_SECONDS" || name == ServicesEnv || name == "PORT" {
			return fmt.Errorf("%s: setting name %s is reserved", safeDiagnosticToken(manifest.App), name)
		}
		if _, overlap := secretSet[name]; overlap {
			return fmt.Errorf("%s: %q is both a secret and a plain setting", safeDiagnosticToken(manifest.App), name)
		}
		if invalidEnvironmentValue(value) {
			return fmt.Errorf("%s: setting %q contains a forbidden character", safeDiagnosticToken(manifest.App), name)
		}
	}
	return nil
}

func validEnvironmentName(name string) bool {
	if name == "" || !asciiLetter(name[0]) && name[0] != '_' {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !asciiLetter(name[index]) && (name[index] < '0' || name[index] > '9') && name[index] != '_' {
			return false
		}
	}
	return true
}

func asciiLetter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func invalidEnvironmentValue(value string) bool {
	return strings.ContainsAny(value, "\x00\r\n")
}

func renderEnvironment(manifest Manifest, secrets map[string]string, drainSeconds int64) []byte {
	var output strings.Builder
	for _, name := range distinctNames(manifest.Secrets) {
		writeEnvironmentEntry(&output, name, secrets[name])
	}
	names := make([]string, 0, len(manifest.Env))
	for name := range manifest.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writeEnvironmentEntry(&output, name, manifest.Env[name])
	}
	output.WriteString("DRAIN_SECONDS=")
	output.WriteString(strconv.FormatInt(drainSeconds, 10))
	output.WriteByte('\n')
	output.WriteString(ServicesEnv + "=" + PerAppServicesPath + "\n")
	return []byte(output.String())
}

func writeEnvironmentEntry(output *strings.Builder, name, value string) {
	output.WriteString(name)
	output.WriteString("=\"")
	output.WriteString(strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(value))
	output.WriteString("\"\n")
}

func socketUnitBytes(root, app string) []byte {
	socket := rootedHostPath(root, "run", "ikigenba", app+".sock")
	return []byte("[Unit]\nDescription=Ikigenba " + app + " socket\n\n" +
		"[Socket]\nListenStream=" + socket + "\nSocketUser=ikigenba\nSocketGroup=nginx\n" +
		"SocketMode=0660\nRemoveOnStop=yes\nBacklog=4096\n\n" +
		"[Install]\nWantedBy=sockets.target\n")
}

func serviceUnitBytes(root, app string, stopSeconds int64, resources Resources) []byte {
	appRoot := rootedHostPath(root, "opt", app)
	socket := socketUnitName(app)
	if resources.Slice == "" {
		resources = defaultResources()
	}
	return []byte("[Unit]\nDescription=Ikigenba " + app + " app\nRequires=" + socket + "\nAfter=" + socket + "\n\n" +
		"[Service]\nType=notify\nExecStart=" + filepath.Join(appRoot, "bin", app) + "\n" +
		"WorkingDirectory=" + rootedHostPath(root, DataRoot, app) + "\nEnvironmentFile=" + rootedHostPath(root, EnvRoot, app, "env") + "\n" +
		"User=ikigenba\nRestart=on-failure\nTimeoutStopSec=" + strconv.FormatInt(stopSeconds, 10) + "\n" + resourceUnitLines(resources) + "\n" +
		"[Install]\nWantedBy=multi-user.target\n")
}

func resourceUnitLines(resources Resources) string {
	var limits strings.Builder
	fmt.Fprintf(&limits, "Slice=ikigenba-%s.slice\nCPUWeight=%d\nMemoryMax=%d\n", resources.Slice, resources.CPUWeight, resources.MemoryMax)
	if resources.Slice == "core" {
		limits.WriteString("MemoryLow=32M\n")
	}
	fmt.Fprintf(&limits, "Environment=GOMEMLIMIT=%d\n", resources.GoMemoryLimit)
	if resources.Delegate {
		limits.WriteString("Delegate=yes\n")
	}
	if resources.OOMPolicy == "continue" {
		limits.WriteString("OOMPolicy=continue\n")
	}
	return limits.String()
}

func publishAtomicFile(filesystem *os.Root, directory, destination string, contents []byte, mode fs.FileMode) error {
	var temporary string
	var file *os.File
	for range 100 {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return err
		}
		temporary = path.Join(directory, fmt.Sprintf(".opsctl-unit-%x", suffix))
		var err error
		file, err = filesystem.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if file == nil {
		return errors.New("create unique unit staging file")
	}
	defer func() { _ = filesystem.Remove(temporary) }()
	if _, err := file.Write(contents); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Chmod(mode); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	return filesystem.Rename(temporary, destination)
}

func rootedHostPath(root string, elements ...string) string {
	parts := append([]string{root}, elements...)
	return filepath.Clean(filepath.Join(parts...))
}

func appUnitName(app string) string {
	return "ikigenba-" + app + ".service"
}

func executeAppCommand(ctx context.Context, env host.Env, label string, command host.Command) error {
	result, err := env.Execute(ctx, command)
	if err != nil {
		return commandTransportError(label, err)
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result}
	}
	return nil
}

func commandTransportError(label string, err error) error {
	var commandErr *host.CommandError
	if errors.As(err, &commandErr) {
		return err
	}
	return &host.CommandError{Label: label, Err: err}
}

// SetupTimeouts updates per-app service timing and resource settings.
func SetupTimeouts(ctx context.Context, env host.Env, store config.Store) error {
	timeouts, err := ReadTimeouts(store)
	if err != nil {
		return err
	}
	services, err := Discover(env.Root)
	if err != nil {
		return err
	}
	type changedApp struct {
		name string
	}
	var changed []changedApp
	unitChanged := false
	var installed []Service
	for _, service := range services {
		if ValidateName(service.Name) != nil {
			continue
		}
		binary := rootedHostPath(env.Root, "opt", service.Name, "bin", service.Name)
		info, statErr := lstatTimeoutPath(env.Root, binary)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			continue
		}
		installed = append(installed, service)
	}
	for _, service := range installed {
		if _, err := lstatTimeoutPath(env.Root, rootedHostPath(env.Root, EnvRoot, service.Name, "env")); errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: /opt/%s/etc/env has not moved", service.Name, service.Name)
		} else if err != nil {
			return fmt.Errorf("%s: inspect environment: %w", service.Name, err)
		}
	}
	for _, service := range installed {
		if service.ManifestError != nil {
			return fmt.Errorf("%s: etc/manifest.toml: %w", service.Name, service.ManifestError)
		}
	}
	for _, service := range installed {
		envPath := rootedHostPath(env.Root, EnvRoot, service.Name, "env")
		current, readErr := readTimeoutFile(env.Root, envPath)
		if readErr != nil {
			return fmt.Errorf("%s: read etc/env: %w", service.Name, readErr)
		}
		updated := updateEnvironmentEntry(current, "DRAIN_SECONDS", strconv.FormatInt(timeouts.DrainSeconds, 10))
		updated = updateEnvironmentEntry(updated, ServicesEnv, PerAppServicesPath)
		appChanged := !bytes.Equal(current, updated)
		if appChanged {
			if err := writeTimeoutFile(env.Root, envPath, updated, 0o600); err != nil {
				return fmt.Errorf("%s: update etc/env: %w", service.Name, err)
			}
		} else if envInfo, statErr := lstatTimeoutPath(env.Root, envPath); statErr != nil {
			return fmt.Errorf("%s: inspect etc/env: %w", service.Name, statErr)
		} else if envInfo.Mode().Perm() != 0o600 {
			if err := chmodTimeoutPath(env.Root, envPath, 0o600); err != nil {
				return fmt.Errorf("%s: set etc/env mode: %w", service.Name, err)
			}
		}
		unitPath := rootedHostPath(env.Root, "etc", "systemd", "system", appUnitName(service.Name))
		var resources Resources
		if service.Manifest != nil {
			resources = service.Manifest.Resources
		}
		unit := serviceUnitBytes(env.Root, service.Name, timeouts.StopSeconds, resources)
		oldUnit, readErr := readTimeoutFile(env.Root, unitPath)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		if !bytes.Equal(oldUnit, unit) {
			if err := writeTimeoutFile(env.Root, unitPath, unit, 0o644); err != nil {
				return err
			}
			unitChanged, appChanged = true, true
		}
		if appChanged {
			changed = append(changed, changedApp{name: service.Name})
		}
	}
	if unitChanged {
		if err := executeAppCommand(ctx, env, "reload systemd units", host.Command{Name: "systemctl", Args: []string{"daemon-reload"}}); err != nil {
			return err
		}
	}
	for _, app := range changed {
		disabled, err := Disabled(ctx, env, app.name)
		if err != nil {
			return err
		}
		if disabled {
			continue
		}
		socket := socketUnitName(app.name)
		result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"is-active", socket}})
		if err != nil {
			return commandTransportError("inspect "+socket, err)
		}
		if result.ExitCode == 3 || result.ExitCode == 4 {
			continue
		}
		if result.ExitCode != 0 {
			return &host.CommandError{Label: "inspect " + socket, Result: result}
		}
		if strings.TrimSpace(string(result.Stdout)) != "active" {
			continue
		}
		if err := executeAppCommand(ctx, env, "restart "+appUnitName(app.name), host.Command{Name: "systemctl", Args: []string{"restart", appUnitName(app.name)}}); err != nil {
			return err
		}
	}
	return nil
}

func updateEnvironmentEntry(contents []byte, name, value string) []byte {
	replacement := []byte(name + "=" + value)
	lines := bytes.SplitAfter(contents, []byte("\n"))
	var output []byte
	found := false
	for _, line := range lines {
		bare := bytes.TrimSuffix(line, []byte("\n"))
		if bytes.HasPrefix(bare, []byte(name+"=")) {
			if !found {
				output = append(output, replacement...)
				if len(line) > len(bare) {
					output = append(output, '\n')
				}
				found = true
			}
			continue
		}
		output = append(output, line...)
	}
	if found {
		return output
	}
	if len(output) > 0 && output[len(output)-1] != '\n' {
		output = append(output, '\n')
	}
	return append(append(output, replacement...), '\n')
}

func writeTimeoutFile(root, destination string, data []byte, mode fs.FileMode) error {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	relative, err := filepath.Rel(root, destination)
	if err != nil {
		return err
	}
	if err := filesystem.MkdirAll(path.Dir(relative), 0o755); err != nil {
		return err
	}
	return publishAtomicFile(filesystem, path.Dir(relative), relative, data, mode)
}

func readTimeoutFile(root, destination string) ([]byte, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()
	relative, err := filepath.Rel(root, destination)
	if err != nil {
		return nil, err
	}
	info, err := filesystem.Lstat(relative)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", relative)
	}
	return filesystem.ReadFile(relative)
}

func lstatTimeoutPath(root, destination string) (fs.FileInfo, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()
	relative, err := filepath.Rel(root, destination)
	if err != nil {
		return nil, err
	}
	return filesystem.Lstat(relative)
}

func chmodTimeoutPath(root, destination string, mode fs.FileMode) error {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	relative, err := filepath.Rel(root, destination)
	if err != nil {
		return err
	}
	return filesystem.Chmod(relative, mode)
}

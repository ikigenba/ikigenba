package apps

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
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

// InstallHooks joins artifact installation to CLI-owned reporting and host
// configuration.
type InstallHooks struct {
	Report    func(step, detail string, success bool) error
	Configure func(context.Context, Manifest) error
}

// InstallError describes a failed installation and retains its cause.
type InstallError struct {
	Code    int
	Message string
	Cause   error
}

// Error returns the stable diagnostic message for an installation failure.
func (failure *InstallError) Error() string { return failure.Message }

// Unwrap exposes the operation that caused an installation failure.
func (failure *InstallError) Unwrap() error { return failure.Cause }

// Install installs the app artifact at uri into env.
func Install(ctx context.Context, env host.Env, remote cloud.Env, store config.Store, uri string, hooks InstallHooks) error {
	workflow, err := prepareInstall(env, remote, store, uri, hooks)
	if err != nil {
		return err
	}
	return workflow.run(ctx)
}

type installWorkflow struct {
	env      host.Env
	remote   cloud.Env
	uri      string
	hooks    InstallHooks
	hostName string
	region   string
	basename string
	client   cloud.Client
	artifact []byte
	checked  *inspectedArtifact
	secrets  map[string]string
}

func prepareInstall(env host.Env, remote cloud.Env, store config.Store, uri string, hooks InstallHooks) (*installWorkflow, error) {
	parsed, err := parseArtifactURI(uri)
	if err != nil {
		return nil, installInputError(2, "install takes an s3:// URI", err)
	}
	if hooks.Report == nil {
		return nil, installInputError(1, "install report hook not set", errors.New("report callback is nil"))
	}
	if hooks.Configure == nil {
		return nil, installInputError(1, "install configure hook not set", errors.New("configure callback is nil"))
	}
	hostName, message, err := installConfig(store, "host.name")
	if err != nil {
		return nil, installInputError(1, message, err)
	}
	region, message, err := installConfig(store, "aws.region")
	if err != nil {
		return nil, installInputError(1, message, err)
	}
	return &installWorkflow{
		env: env, remote: remote, uri: uri, hooks: hooks,
		hostName: hostName, region: region, basename: path.Base(parsed.Path),
	}, nil
}

func (workflow *installWorkflow) run(ctx context.Context) error {
	stages := []func(context.Context) error{
		workflow.fetch,
		workflow.inspectFiles,
		workflow.fetchSecrets,
		workflow.unpack,
		workflow.publishUnit,
		workflow.configure,
		workflow.activate,
	}
	for _, stage := range stages {
		if err := stage(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (workflow *installWorkflow) fetch(ctx context.Context) error {
	artifact, client, failure := downloadArtifact(ctx, workflow.remote, workflow.region, workflow.uri)
	if failure != nil {
		reportErr := workflow.hooks.Report("fetch", failure.detail, false)
		cause, code := failure.cause, failure.code
		if reportErr != nil {
			cause, code = errors.Join(cause, reportErr), 1
		}
		return &InstallError{Code: code, Message: "artifact download failed", Cause: cause}
	}
	detail := fmt.Sprintf("%s, %.1f MiB", safeDiagnosticToken(workflow.basename), float64(len(artifact))/1048576)
	if err := workflow.hooks.Report("fetch", detail, true); err != nil {
		return &InstallError{Code: 1, Message: "install failed", Cause: err}
	}
	workflow.artifact, workflow.client = artifact, client
	return nil
}

func (workflow *installWorkflow) inspectFiles(ctx context.Context) error {
	checked, failure := inspectArtifact(ctx, workflow.env, workflow.artifact, workflow.basename)
	if failure == nil {
		failure = completeFileStage(ctx, workflow.env, checked)
	}
	if failure != nil {
		return failInstallStage(workflow.hooks, "file", failure)
	}
	if err := workflow.hooks.Report("file", manifestDetail(checked.manifest), true); err != nil {
		return &InstallError{Code: 1, Message: "install failed", Cause: err}
	}
	workflow.checked = checked
	return nil
}

func (workflow *installWorkflow) fetchSecrets(ctx context.Context) error {
	values, failure := obtainSecrets(ctx, workflow.client, workflow.hostName, workflow.checked.manifest)
	if failure != nil {
		return failInstallStage(workflow.hooks, "secrets", failure)
	}
	detail := fmt.Sprintf("%d keys", len(distinctNames(workflow.checked.manifest.Secrets)))
	if err := workflow.hooks.Report("secrets", detail, true); err != nil {
		return &InstallError{Code: 1, Message: "install failed", Cause: err}
	}
	workflow.secrets = values
	return nil
}

func (workflow *installWorkflow) unpack(context.Context) error {
	environment := renderEnvironment(workflow.checked.manifest, workflow.secrets)
	if failure := replaceInstalledFiles(workflow.env.Root, workflow.checked, environment); failure != nil {
		return failInstallStage(workflow.hooks, "unpack", failure)
	}
	detail := fmt.Sprintf("/opt/%s", safeDiagnosticToken(workflow.checked.manifest.App))
	if err := workflow.hooks.Report("unpack", detail, true); err != nil {
		return &InstallError{Code: 1, Message: "install failed", Cause: err}
	}
	return nil
}

func (workflow *installWorkflow) publishUnit(ctx context.Context) error {
	if failure := publishAppUnit(ctx, workflow.env, workflow.checked); failure != nil {
		return failInstallStage(workflow.hooks, "unit", failure)
	}
	unit := appUnitName(workflow.checked.manifest.App)
	if err := workflow.hooks.Report("unit", safeDiagnosticToken(unit), true); err != nil {
		return &InstallError{Code: 1, Message: "install failed", Cause: err}
	}
	return nil
}

func (workflow *installWorkflow) configure(ctx context.Context) error {
	if err := workflow.hooks.Configure(ctx, workflow.checked.manifest); err != nil {
		return &InstallError{Code: 1, Message: "install failed", Cause: err}
	}
	return nil
}

func (workflow *installWorkflow) activate(ctx context.Context) error {
	detail, failure := activateInstalledApp(ctx, workflow.env, workflow.checked)
	if failure != nil {
		return failInstallStage(workflow.hooks, "service", failure)
	}
	if err := workflow.hooks.Report("service", detail, true); err != nil {
		return &InstallError{Code: 1, Message: "install failed", Cause: err}
	}
	return nil
}

type stageFailure struct {
	code    int
	detail  string
	cause   error
	message string
}

type inspectedArtifact struct {
	basename string
	manifest Manifest
	entries  []archiveEntry
	active   bool
}

func (artifact *inspectedArtifact) hasTopLevel(name string) bool {
	for _, entry := range artifact.entries {
		if entry.name == name || strings.HasPrefix(entry.name, name+"/") {
			return true
		}
	}
	return false
}

type archiveEntry struct {
	name string
	mode fs.FileMode
	data []byte
	dir  bool
}

func failInstallStage(hooks InstallHooks, step string, failure *stageFailure) error {
	reportErr := hooks.Report(step, failure.detail, false)
	cause := failure.cause
	code := failure.code
	if reportErr != nil {
		cause = errors.Join(cause, reportErr)
		code = 1
	}
	message := failure.message
	if message == "" {
		message = "install failed"
	}
	return &InstallError{Code: code, Message: message, Cause: cause}
}

func installInputError(code int, message string, cause error) error {
	return &InstallError{Code: code, Message: message, Cause: cause}
}

func parseArtifactURI(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "s3" || parsed.Host == "" ||
		strings.TrimPrefix(parsed.EscapedPath(), "/") == "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || strings.Contains(value, "#") || parsed.User != nil || parsed.OmitHost || parsed.Opaque != "" {
		return nil, errors.New("URI is not an absolute S3 object location")
	}
	return parsed, nil
}

func installConfig(store config.Store, key string) (string, string, error) {
	value, err := store.Get(key)
	if err != nil {
		if errors.Is(err, config.ErrNotSet) {
			return "", key + " not set", err
		}
		return "", "read " + key + " configuration failed", err
	}
	if value == "" {
		return "", key + " not set", errors.New("configuration value is empty")
	}
	return value, "", nil
}

func downloadArtifact(ctx context.Context, remote cloud.Env, region, uri string) ([]byte, cloud.Client, *stageFailure) {
	if remote.Open == nil {
		err := errors.New("cloud open not configured")
		return nil, nil, &stageFailure{code: 1, detail: err.Error(), cause: err}
	}
	client, err := remote.Open(ctx, region)
	if err != nil {
		return nil, nil, &stageFailure{code: 1, detail: err.Error(), cause: err}
	}
	if client == nil {
		err = errors.New("cloud open returned no client")
		return nil, nil, &stageFailure{code: 1, detail: err.Error(), cause: err}
	}

	reader, getErr := client.GetObject(ctx, uri)
	if getErr != nil {
		cause := getErr
		if reader != nil {
			cause = errors.Join(cause, reader.Close())
		}
		basename := artifactBasename(uri)
		if errors.Is(getErr, cloud.ErrNotFound) {
			detail := fmt.Sprintf("%s: no such object", safeDiagnosticToken(basename))
			return nil, nil, &stageFailure{code: 2, detail: detail, cause: cause}
		}
		detail := fmt.Sprintf("%s: %s", safeDiagnosticToken(basename), safeDiagnosticText(getErr.Error()))
		return nil, nil, &stageFailure{code: 1, detail: detail, cause: cause}
	}
	if reader == nil {
		err = errors.New("object download returned no reader")
		detail := fmt.Sprintf("%s: %s", safeDiagnosticToken(artifactBasename(uri)), safeDiagnosticText(err.Error()))
		return nil, nil, &stageFailure{code: 1, detail: detail, cause: err}
	}

	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		cause := errors.Join(readErr, closeErr)
		detail := fmt.Sprintf("%s: %s", safeDiagnosticToken(artifactBasename(uri)), safeDiagnosticText(cause.Error()))
		return nil, nil, &stageFailure{code: 1, detail: detail, cause: cause}
	}
	return data, client, nil
}

func artifactBasename(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return path.Base(uri)
	}
	return path.Base(parsed.Path)
}

func safeDiagnosticToken(value string) string {
	for _, character := range value {
		if character < ' ' || character == '\u007f' {
			return fmt.Sprintf("%q", value)
		}
	}
	return value
}

func safeDiagnosticText(value string) string {
	return strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(value)
}

func inspectArtifact(ctx context.Context, env host.Env, artifact []byte, basename string) (*inspectedArtifact, *stageFailure) {
	archive, failure := decompressInstallArtifact(ctx, env, artifact, basename)
	if failure != nil {
		return nil, failure
	}
	entries, manifestData, failure := inspectArchiveContents(archive, basename)
	if failure != nil {
		return nil, failure
	}
	manifest, failure := inspectInstallManifest(manifestData, basename)
	if failure != nil {
		return nil, failure
	}
	if failure := requireAppExecutable(entries, manifest, basename); failure != nil {
		return nil, failure
	}
	return &inspectedArtifact{basename: basename, manifest: manifest, entries: entries}, nil
}

func decompressInstallArtifact(ctx context.Context, env host.Env, artifact []byte, basename string) ([]byte, *stageFailure) {
	if env.Execute == nil {
		err := errors.New("host execution not configured")
		return nil, operationalFailure(err)
	}
	result, err := env.Execute(ctx, host.Command{
		Name:  "xz",
		Args:  []string{"--decompress", "--stdout"},
		Stdin: bytes.NewReader(artifact),
	})
	if err != nil {
		return nil, operationalFailure(commandTransportError("decompress artifact", err))
	}
	if result.ExitCode != 0 {
		err := &host.CommandError{Label: "decompress artifact", Result: result}
		detail := fmt.Sprintf("%s: invalid xz archive", safeDiagnosticToken(basename))
		return nil, &stageFailure{code: 2, detail: detail, cause: err}
	}
	return result.Stdout, nil
}

func inspectArchiveContents(archive []byte, basename string) ([]archiveEntry, []byte, *stageFailure) {
	entries, manifestData, err := validateArchive(archive)
	if err != nil {
		detail := fmt.Sprintf("%s: %s", safeDiagnosticToken(basename), safeDiagnosticText(err.Error()))
		return nil, nil, &stageFailure{code: 2, detail: detail, cause: err}
	}
	if manifestData == nil {
		err := errors.New("no etc/manifest.toml in the file")
		detail := fmt.Sprintf("%s: %s", safeDiagnosticToken(basename), safeDiagnosticText(err.Error()))
		return nil, nil, &stageFailure{code: 2, detail: detail, cause: err}
	}
	return entries, manifestData, nil
}

func inspectInstallManifest(manifestData []byte, basename string) (Manifest, *stageFailure) {
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		var unusable *unusableAppNameError
		if errors.As(err, &unusable) {
			detail := fmt.Sprintf("'%s' is not a usable app name", safeDiagnosticToken(unusable.name))
			return Manifest{}, &stageFailure{code: 2, detail: detail, cause: err}
		}
		detail := fmt.Sprintf("%s: etc/manifest.toml: %s", safeDiagnosticToken(basename), safeDiagnosticText(err.Error()))
		return Manifest{}, &stageFailure{code: 2, detail: detail, cause: err}
	}
	if manifest.App == "" {
		err = &unusableAppNameError{name: ""}
		return Manifest{}, &stageFailure{code: 2, detail: "'' is not a usable app name", cause: err}
	}
	if manifest.Port == 0 {
		err = errors.New("manifest port is not set")
		detail := fmt.Sprintf("%s: %s", safeDiagnosticToken(basename), safeDiagnosticText(err.Error()))
		return Manifest{}, &stageFailure{code: 2, detail: detail, cause: err}
	}
	return manifest, nil
}

func requireAppExecutable(entries []archiveEntry, manifest Manifest, basename string) *stageFailure {
	executable := path.Join("bin", manifest.App)
	for _, entry := range entries {
		if entry.name == executable && !entry.dir && entry.mode.IsRegular() && entry.mode.Perm()&0o111 != 0 {
			return nil
		}
	}
	err := fmt.Errorf("no executable %s in the file", safeDiagnosticToken(executable))
	detail := fmt.Sprintf("%s: %s", safeDiagnosticToken(basename), safeDiagnosticText(err.Error()))
	return &stageFailure{code: 2, detail: detail, cause: err}
}

func validateArchive(data []byte) ([]archiveEntry, []byte, error) {
	reader := tar.NewReader(bytes.NewReader(data))
	seen := make(map[string]struct{})
	var entries []archiveEntry
	var manifest []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("invalid or truncated tar archive: %w", err)
		}
		name, err := validateArchivePath(header.Name)
		if err != nil {
			return nil, nil, err
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate archive path %s", safeDiagnosticToken(name))
		}
		seen[name] = struct{}{}
		entry := archiveEntry{name: name, mode: header.FileInfo().Mode()}
		switch header.Typeflag {
		case tar.TypeDir:
			entry.dir = true
		case tar.TypeReg, 0:
			entry.data, err = io.ReadAll(reader)
			if err != nil {
				return nil, nil, fmt.Errorf("truncated archive while reading %s: %w", safeDiagnosticToken(name), err)
			}
		default:
			return nil, nil, fmt.Errorf("archive path %s is not a regular file or directory", safeDiagnosticToken(name))
		}
		if !strings.Contains(name, "/") && !entry.dir {
			return nil, nil, fmt.Errorf("archive path %s is not a top-level directory", safeDiagnosticToken(name))
		}
		if name == "etc/manifest.toml" {
			if entry.dir {
				return nil, nil, errors.New("etc/manifest.toml is not a regular file")
			}
			manifest = append([]byte(nil), entry.data...)
		}
		entries = append(entries, entry)
	}
	return entries, manifest, nil
}

func validateArchivePath(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("invalid archive path %q", name)
	}
	trimmed := strings.TrimSuffix(name, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] != "bin" && parts[0] != "etc" && parts[0] != "share" {
		return "", fmt.Errorf("archive path %s is outside bin, etc, and share", safeDiagnosticToken(name))
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid archive path %q", name)
		}
	}
	return strings.Join(parts, "/"), nil
}

func completeFileStage(ctx context.Context, env host.Env, artifact *inspectedArtifact) *stageFailure {
	services, err := Discover(env.Root)
	if err != nil {
		return &stageFailure{code: 1, detail: err.Error(), cause: err}
	}
	for _, service := range services {
		if service.ManifestError != nil {
			err = fmt.Errorf("%s: %w", safeDiagnosticToken(service.Name), service.ManifestError)
			return &stageFailure{code: 1, detail: err.Error(), cause: err}
		}
		if artifact.manifest.Default && service.Name != artifact.manifest.App && service.Manifest != nil && service.Manifest.Default {
			err = fmt.Errorf("%s: %s is already the default app", safeDiagnosticToken(artifact.manifest.App), safeDiagnosticToken(service.Name))
			return &stageFailure{code: 1, detail: err.Error(), cause: err}
		}
	}
	unit := appUnitName(artifact.manifest.App)
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"is-active", unit}})
	if err != nil {
		err = commandTransportError(fmt.Sprintf("inspect %s", safeDiagnosticToken(unit)), err)
		return &stageFailure{code: 1, detail: err.Error(), cause: err}
	}
	switch result.ExitCode {
	case 0:
		artifact.active = true
	case 3, 4:
		artifact.active = false
	default:
		err = &host.CommandError{Label: fmt.Sprintf("inspect %s", safeDiagnosticToken(unit)), Result: result}
		return &stageFailure{code: 1, detail: err.Error(), cause: err}
	}
	return nil
}

func manifestDetail(manifest Manifest) string {
	detail := fmt.Sprintf("%s, port %d", safeDiagnosticToken(manifest.App), manifest.Port)
	if manifest.Default {
		detail += ", default"
	}
	return detail
}

func obtainSecrets(ctx context.Context, client cloud.Client, hostName string, manifest Manifest) (map[string]string, *stageFailure) {
	names := distinctNames(manifest.Secrets)
	parameter := "/ikigenba/" + hostName + "/" + manifest.App
	values := map[string]string{}
	if len(names) > 0 {
		var err error
		values, err = client.ReadSecrets(ctx, parameter)
		if errors.Is(err, cloud.ErrNotFound) {
			values = map[string]string{}
		} else if err != nil {
			return nil, &stageFailure{code: 1, detail: err.Error(), cause: err}
		}
		if values == nil {
			values = map[string]string{}
		}
	}
	for _, name := range names {
		if _, present := values[name]; !present {
			err := fmt.Errorf("%s: no value for '%s' in %s", safeDiagnosticToken(manifest.App), safeDiagnosticToken(name), safeDiagnosticToken(parameter))
			return nil, &stageFailure{code: 1, detail: err.Error(), cause: err}
		}
	}
	if err := validateEnvironment(manifest, values); err != nil {
		return nil, &stageFailure{code: 1, detail: err.Error(), cause: err}
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
		if name == "PORT" {
			return fmt.Errorf("%s: secret name PORT is reserved", safeDiagnosticToken(manifest.App))
		}
		secretSet[name] = struct{}{}
		if invalidEnvironmentValue(secrets[name]) {
			return fmt.Errorf("%s: secret %q contains a forbidden character", safeDiagnosticToken(manifest.App), name)
		}
	}
	for name, value := range manifest.Env {
		if !validEnvironmentName(name) {
			return fmt.Errorf("%s: invalid setting name %q", safeDiagnosticToken(manifest.App), name)
		}
		if name == "PORT" {
			return fmt.Errorf("%s: setting name PORT is reserved", safeDiagnosticToken(manifest.App))
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

// renderEnvironment receives the manifest and values accepted by obtainSecrets.
func renderEnvironment(manifest Manifest, secrets map[string]string) []byte {
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
	writeEnvironmentEntry(&output, "PORT", strconv.Itoa(manifest.Port))
	return []byte(output.String())
}

func writeEnvironmentEntry(output *strings.Builder, name, value string) {
	output.WriteString(name)
	output.WriteString("=\"")
	output.WriteString(strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(value))
	output.WriteString("\"\n")
}

func replaceInstalledFiles(root string, artifact *inspectedArtifact, environment []byte) *stageFailure {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return unpackFailure(err)
	}
	defer func() { _ = filesystem.Close() }()

	appRoot := path.Join("opt", artifact.manifest.App)
	if err := rejectDestinationSymlinks(filesystem, appRoot); err != nil {
		return unpackFailure(err)
	}
	if err := ensureOptDirectory(filesystem); err != nil {
		return unpackFailure(err)
	}
	if err := filesystem.MkdirAll(appRoot, 0o750); err != nil {
		return unpackFailure(err)
	}
	staging, err := makeStagingDirectory(filesystem, artifact.manifest.App)
	if err != nil {
		return unpackFailure(err)
	}
	defer func() { _ = filesystem.RemoveAll(staging) }()
	for _, directory := range []string{"bin", "etc"} {
		if err := filesystem.Mkdir(path.Join(staging, directory), 0o750); err != nil {
			return unpackFailure(err)
		}
	}
	if artifact.hasTopLevel("share") {
		if err := filesystem.Mkdir(path.Join(staging, "share"), 0o750); err != nil {
			return unpackFailure(err)
		}
	}
	for _, entry := range artifact.entries {
		target := path.Join(staging, entry.name)
		if entry.dir {
			if err := filesystem.MkdirAll(target, entry.mode.Perm()); err != nil {
				return unpackFailure(err)
			}
			continue
		}
		if err := filesystem.MkdirAll(path.Dir(target), 0o750); err != nil {
			return unpackFailure(err)
		}
		if err := filesystem.WriteFile(target, entry.data, entry.mode.Perm()); err != nil {
			return unpackFailure(err)
		}
	}
	if err := filesystem.WriteFile(path.Join(staging, "etc", "env"), environment, 0o600); err != nil {
		return unpackFailure(err)
	}
	if err := filesystem.Chmod(path.Join(staging, "etc", "env"), 0o600); err != nil {
		return unpackFailure(err)
	}
	for _, directory := range []string{"bin", "etc"} {
		if err := replaceDirectory(filesystem, staging, appRoot, directory); err != nil {
			return unpackFailure(err)
		}
	}
	if artifact.hasTopLevel("share") {
		if err := replaceDirectory(filesystem, staging, appRoot, "share"); err != nil {
			return unpackFailure(err)
		}
	} else if err := filesystem.RemoveAll(path.Join(appRoot, "share")); err != nil {
		return unpackFailure(err)
	}
	return nil
}

func ensureOptDirectory(filesystem *os.Root) error {
	if _, err := filesystem.Lstat("opt"); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := filesystem.Mkdir("opt", 0o755); err != nil {
		return err
	}
	return filesystem.Chmod("opt", 0o755)
}

func rejectDestinationSymlinks(filesystem *os.Root, appRoot string) error {
	for _, name := range []string{"opt", appRoot} {
		info, err := filesystem.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("destination %s is a symbolic link", safeDiagnosticToken("/"+name))
		}
		if !info.IsDir() {
			return fmt.Errorf("destination %s is not a directory", safeDiagnosticToken("/"+name))
		}
	}
	return nil
}

func makeStagingDirectory(filesystem *os.Root, app string) (string, error) {
	for range 100 {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return "", err
		}
		name := path.Join("opt", fmt.Sprintf(".opsctl-install-%s-%x", app, suffix))
		if err := filesystem.Mkdir(name, 0o700); err == nil {
			return name, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("create unique install staging directory")
}

func replaceDirectory(filesystem *os.Root, staging, appRoot, directory string) error {
	destination := path.Join(appRoot, directory)
	backup := path.Join(staging, ".old-"+directory)
	if _, err := filesystem.Lstat(destination); err == nil {
		if err := filesystem.Rename(destination, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := filesystem.Rename(path.Join(staging, directory), destination); err != nil {
		if _, backupErr := filesystem.Lstat(backup); backupErr == nil {
			_ = filesystem.Rename(backup, destination)
		}
		return err
	}
	return filesystem.RemoveAll(backup)
}

func unpackFailure(err error) *stageFailure {
	return &stageFailure{code: 1, detail: err.Error(), cause: err}
}

func publishAppUnit(ctx context.Context, env host.Env, artifact *inspectedArtifact) *stageFailure {
	manifest := artifact.manifest
	if err := ensureServiceAccount(ctx, env); err != nil {
		return operationalFailure(err)
	}

	appRoot := rootedHostPath(env.Root, "opt", manifest.App)
	if err := makeAppRootWritable(env.Root, manifest.App); err != nil {
		return operationalFailure(err)
	}
	if err := executeInstallCommand(ctx, env, fmt.Sprintf("make %s writable", safeDiagnosticToken(manifest.App)), host.Command{
		Name: "chown", Args: []string{"ikigenba:ikigenba", appRoot},
	}); err != nil {
		return operationalFailure(err)
	}
	if err := applyInstalledTreeOwnership(ctx, env, manifest.App); err != nil {
		return operationalFailure(err)
	}
	if err := applyInstalledTreeModes(env.Root, artifact); err != nil {
		return operationalFailure(err)
	}
	unitName := appUnitName(manifest.App)
	if err := writeAppUnit(env.Root, unitName, manifest.App); err != nil {
		return operationalFailure(err)
	}
	if err := executeInstallCommand(ctx, env, "reload systemd units", host.Command{
		Name: "systemctl", Args: []string{"daemon-reload"},
	}); err != nil {
		return operationalFailure(err)
	}
	if err := executeInstallCommand(ctx, env, fmt.Sprintf("enable %s", safeDiagnosticToken(unitName)), host.Command{
		Name: "systemctl", Args: []string{"enable", unitName},
	}); err != nil {
		return operationalFailure(err)
	}
	return nil
}

func ensureServiceAccount(ctx context.Context, env host.Env) error {
	uid, err := inspectServiceAccount(ctx, env)
	if err == nil {
		if uid == 0 {
			return errors.New("ikigenba account must not be root")
		}
		return requireServiceAccountGroup(ctx, env)
	}
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Result.ExitCode != 1 {
		return err
	}
	if err := executeInstallCommand(ctx, env, "create ikigenba account", host.Command{
		Name: "useradd", Args: []string{"--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "--user-group", "ikigenba"},
	}); err != nil {
		return err
	}
	return nil
}

func requireServiceAccountGroup(ctx context.Context, env host.Env) error {
	result, err := env.Execute(ctx, host.Command{Name: "id", Args: []string{"--group", "--name", "ikigenba"}})
	if err != nil {
		return commandTransportError("inspect ikigenba primary group", err)
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: "inspect ikigenba primary group", Result: result}
	}
	group := strings.TrimSpace(string(result.Stdout))
	if group != "ikigenba" {
		return fmt.Errorf("ikigenba account primary group is %q, want ikigenba", group)
	}
	return nil
}

func applyInstalledTreeOwnership(ctx context.Context, env host.Env, app string) error {
	args := []string{"--recursive", "root:ikigenba"}
	for _, tree := range []string{"bin", "etc", "share"} {
		name := rootedHostPath(env.Root, "opt", app, tree)
		if _, err := os.Lstat(name); err == nil {
			args = append(args, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return executeInstallCommand(ctx, env, fmt.Sprintf("set %s installed-tree ownership", safeDiagnosticToken(app)), host.Command{
		Name: "chown", Args: args,
	})
}

func applyInstalledTreeModes(root string, artifact *inspectedArtifact) error {
	app := artifact.manifest.App
	executable := make(map[string]bool, len(artifact.entries))
	for _, entry := range artifact.entries {
		if !entry.dir && entry.mode.Perm()&0o111 != 0 {
			executable[filepath.FromSlash(entry.name)] = true
		}
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	appRoot := path.Join("opt", app)
	for _, tree := range []string{"bin", "etc", "share"} {
		base := path.Join(appRoot, tree)
		if _, err := filesystem.Lstat(base); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := applyInstalledPathModes(filesystem, base, appRoot, executable); err != nil {
			return err
		}
	}
	return nil
}

func applyInstalledPathModes(filesystem *os.Root, name, appRoot string, executable map[string]bool) error {
	info, err := filesystem.Lstat(name)
	if err != nil {
		return err
	}
	mode := fs.FileMode(0o640)
	relative := filepath.FromSlash(strings.TrimPrefix(name, appRoot+"/"))
	if info.IsDir() || executable[relative] {
		mode = 0o750
	}
	if name == path.Join(appRoot, "etc", "env") {
		mode = 0o600
	}
	if err := filesystem.Chmod(name, mode); err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	directory, err := filesystem.Open(name)
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	for _, entry := range entries {
		if err := applyInstalledPathModes(filesystem, path.Join(name, entry.Name()), appRoot, executable); err != nil {
			return err
		}
	}
	return nil
}

func makeAppRootWritable(root, app string) error {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	return filesystem.Chmod(path.Join("opt", app), 0o750)
}

func inspectServiceAccount(ctx context.Context, env host.Env) (uint64, error) {
	result, err := env.Execute(ctx, host.Command{Name: "id", Args: []string{"--user", "ikigenba"}})
	if err != nil {
		return 0, commandTransportError("inspect ikigenba account", err)
	}
	if result.ExitCode != 0 {
		return 0, &host.CommandError{Label: "inspect ikigenba account", Result: result}
	}
	uidText := strings.TrimSpace(string(result.Stdout))
	uid, parseErr := strconv.ParseUint(uidText, 10, 32)
	if parseErr != nil {
		return 0, fmt.Errorf("inspect ikigenba account: invalid uid %q", uidText)
	}
	return uid, nil
}

func writeAppUnit(root, unitName, app string) error {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	unitDirectory := path.Join("etc", "systemd", "system")
	if err := filesystem.MkdirAll(unitDirectory, 0o755); err != nil {
		return err
	}
	unitPath := path.Join(unitDirectory, unitName)
	if info, statErr := filesystem.Lstat(unitPath); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unit %s is a symbolic link", safeDiagnosticToken("/"+unitPath))
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unit %s is not a regular file", safeDiagnosticToken("/"+unitPath))
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	appRoot := rootedHostPath(root, "opt", app)
	unit := "[Unit]\nDescription=Ikigenba " + app + " app\n\n" +
		"[Service]\n" +
		"ExecStart=" + filepath.Join(appRoot, "bin", app) + "\n" +
		"WorkingDirectory=" + appRoot + "\n" +
		"EnvironmentFile=" + filepath.Join(appRoot, "etc", "env") + "\n" +
		"User=ikigenba\n" +
		"Restart=on-failure\n\n" +
		"[Install]\nWantedBy=multi-user.target\n"
	return publishAtomicFile(filesystem, unitDirectory, unitPath, []byte(unit), 0o644)
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

func activateInstalledApp(ctx context.Context, env host.Env, artifact *inspectedArtifact) (string, *stageFailure) {
	unit := appUnitName(artifact.manifest.App)
	action := "start"
	if artifact.active {
		action = "restart"
	}
	if err := executeInstallCommand(ctx, env, fmt.Sprintf("%s %s", action, safeDiagnosticToken(unit)), host.Command{
		Name: "systemctl", Args: []string{action, unit},
	}); err != nil {
		return "", appStartFailure(ctx, env, artifact.manifest.App, unit, err)
	}
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"is-active", unit}})
	if err != nil {
		return "", appStartFailure(ctx, env, artifact.manifest.App, unit,
			commandTransportError(fmt.Sprintf("inspect %s", safeDiagnosticToken(unit)), err))
	}
	if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "active" {
		stateErr := &host.CommandError{Label: fmt.Sprintf("inspect %s", safeDiagnosticToken(unit)), Result: result}
		return "", appStartFailure(ctx, env, artifact.manifest.App, unit, stateErr)
	}

	binary := rootedHostPath(env.Root, "opt", artifact.manifest.App, "bin", artifact.manifest.App)
	result, err = env.Execute(ctx, host.Command{Name: binary, Args: []string{"--version"}})
	if err != nil {
		return "", operationalFailure(commandTransportError(
			fmt.Sprintf("read %s version", safeDiagnosticToken(artifact.manifest.App)), err))
	}
	if result.ExitCode != 0 {
		err = &host.CommandError{Label: fmt.Sprintf("read %s version", safeDiagnosticToken(artifact.manifest.App)), Result: result}
		return "", operationalFailure(err)
	}
	version := safeDiagnosticToken(strings.TrimRight(string(result.Stdout), "\r\n"))
	return fmt.Sprintf("%s %s active", safeDiagnosticToken(artifact.manifest.App), version), nil
}

func appStartFailure(ctx context.Context, env host.Env, app, unit string, startErr error) *stageFailure {
	detail := fmt.Sprintf("%s: service failed to start", safeDiagnosticToken(app))
	result, journalErr := env.Execute(ctx, host.Command{
		Name: "journalctl", Args: []string{"--unit", unit, "--no-pager", "--lines", "50"},
	})
	if journalErr != nil {
		journalCause := commandTransportError(
			fmt.Sprintf("obtain %s journal", safeDiagnosticToken(unit)), journalErr)
		return &stageFailure{code: 1, detail: detail, cause: errors.Join(startErr, journalCause), message: detail}
	}
	if result.ExitCode != 0 {
		journalFailure := &host.CommandError{Label: fmt.Sprintf("obtain %s journal", safeDiagnosticToken(unit)), Result: result}
		return &stageFailure{code: 1, detail: detail, cause: errors.Join(startErr, journalFailure), message: detail}
	}
	cause := &host.CommandError{Label: "journal captured after service startup failure", Result: result, Err: startErr}
	return &stageFailure{code: 1, detail: detail, cause: cause, message: detail}
}

func executeInstallCommand(ctx context.Context, env host.Env, label string, command host.Command) error {
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

func operationalFailure(err error) *stageFailure {
	return &stageFailure{code: 1, detail: err.Error(), cause: err}
}

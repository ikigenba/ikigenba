package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// NginxRegenerator rebuilds nginx configuration after restored files have
// been published.
type NginxRegenerator func(context.Context) error

// RestoreStep describes one attempted restore phase.
type RestoreStep struct {
	Name   string
	Detail string
	Err    error
}

// RestoreReport retains restore phases in execution order.
type RestoreReport struct {
	Steps []RestoreStep
}

// RestoreError describes an operational restore failure.
type RestoreError struct {
	Service string
	Stage   string
	Err     error
	Stopped []string
}

// Error returns the service, stage, and underlying failure.
func (failure *RestoreError) Error() string {
	return failure.Service + ": " + failure.Stage + ": " + failure.Err.Error()
}

// Unwrap returns the underlying failure.
func (failure *RestoreError) Unwrap() error { return failure.Err }

type serviceRestoreEntry struct {
	name     string
	typeflag byte
	mode     fs.FileMode
	linkname string
	data     []byte
	uid      int
	gid      int
	uname    string
	gname    string
}

type serviceRestoreSource struct {
	basename string
	size     int64
	entries  []serviceRestoreEntry
	manifest *apps.Manifest
}

// Restore selects and validates the requested service backup before any host
// state is changed.
func Restore(ctx context.Context, env host.Env, cloudEnv cloud.Env, store config.Store, service string, at *time.Time, regenerateNginx NginxRegenerator) (RestoreReport, error) {
	var report RestoreReport
	if invalidFileServiceName(service) {
		return report, fmt.Errorf("invalid service %q", service)
	}
	prefix, region, err := fileBackupConfiguration(store, service)
	if err != nil {
		return report, err
	}
	if regenerateNginx == nil {
		return report, errors.New("restore nginx regeneration is not configured")
	}
	if err := ctx.Err(); err != nil {
		return report, fmt.Errorf("restore %q: %w", service, err)
	}
	if env.Execute == nil {
		return report, errors.New("restore service: host execution is not configured")
	}
	if cloudEnv.Open == nil {
		return report, errors.New("restore service: cloud access is not configured")
	}
	client, err := cloudEnv.Open(ctx, region)
	if err != nil {
		err = fmt.Errorf("open backup storage: %w", err)
		return failRestoreStep(report, service, "source", "source", err, nil)
	}
	if client == nil {
		err = errors.New("open backup storage: cloud client is not configured")
		return failRestoreStep(report, service, "source", "source", err, nil)
	}
	if err := ctx.Err(); err != nil {
		err = fmt.Errorf("restore %q: %w", service, err)
		return failRestoreStep(report, service, "source", "source", err, nil)
	}

	source, err := loadServiceRestoreSource(ctx, env, client, prefix, service, at)
	if err != nil {
		report.Steps = append(report.Steps, RestoreStep{Name: "source", Err: err})
		return report, &RestoreError{Service: service, Stage: "source", Err: err}
	}
	report.Steps = append(report.Steps, RestoreStep{
		Name:   "source",
		Detail: fmt.Sprintf("%s/%s, %.1f MiB", service, source.basename, float64(source.size)/1048576),
	})
	unit := ""
	unitInstalled := false
	unitActive := false
	activationIntent := false
	if apps.ValidateName(service) == nil {
		unit = "ikigenba-" + service + ".service"
		unitInstalled, unitActive, err = inspectRestoreUnit(ctx, env, unit)
		if err != nil {
			return failRestoreStep(report, service, "stop", "unit inspection", err, nil)
		}
		if unitInstalled {
			activationIntent, err = hasRestoreActivationMarker(env.Root, service)
			if err != nil {
				return report, &RestoreError{Service: service, Stage: "activation marker", Err: err}
			}
			activationIntent = activationIntent || unitActive
		}
		if unitActive {
			if err := publishRestoreActivationMarker(env.Root, service); err != nil {
				return report, &RestoreError{Service: service, Stage: "activation marker", Err: err}
			}
			if err := runRestoreCommand(ctx, env, "stop "+unit, "systemctl", "stop", unit); err != nil {
				return failRestoreStep(report, service, "stop", "stop", err, nil)
			}
		}
	}

	databaseIncoming := source.manifest != nil && source.manifest.Database != nil
	if databaseIncoming {
		if err := runRestoreCommand(ctx, env, "stop litestream.service", "systemctl", "stop", "litestream.service"); err != nil {
			stopped := restoreStoppedUnits(unit, activationIntent, false)
			return failRestoreStep(report, service, "stop", "stop", err, stopped)
		}
	}
	report.Steps = append(report.Steps, RestoreStep{Name: "stop", Detail: restoreStopDetail(unit, unitInstalled, unitActive, databaseIncoming)})

	identity, err := prepareRestoreIdentity(ctx, env, service, source.entries, source.manifest)
	if err != nil {
		return failRestoreStep(report, service, "files", "ownership", err, restoreStoppedUnits(unit, activationIntent, databaseIncoming))
	}
	count, err := replaceServiceRestoreTrees(ctx, env.Root, service, source.entries, identity)
	if err != nil {
		return failRestoreStep(report, service, "files", "files", err, restoreStoppedUnits(unit, activationIntent, databaseIncoming))
	}
	report.Steps = append(report.Steps, RestoreStep{
		Name:   "files",
		Detail: fmt.Sprintf("/opt/%s/etc, /opt/%s/state, %d files", service, service, count),
	})
	stopped := restoreStoppedUnits(unit, activationIntent, databaseIncoming)
	if databaseIncoming {
		database := *source.manifest.Database
		recovered, restoreErr := restoreServiceDatabase(ctx, env, prefix, service, database, at)
		if restoreErr != nil {
			return failRestoreStep(report, service, "db", "litestream restore", restoreErr, stopped)
		}
		if walErr := setRestoredDatabaseWAL(env.Root, service, database.Path); walErr != nil {
			return failRestoreStep(report, service, "db", "wal mode", walErr, stopped)
		}
		if ownershipErr := applyRestoredDatabaseOwnership(env.Root, service, database.Path, identity); ownershipErr != nil {
			return failRestoreStep(report, service, "db", "database ownership", ownershipErr, stopped)
		}
		detail := "/opt/" + service + "/" + database.Path
		if at == nil {
			detail += ", newest " + recovered
		} else {
			detail += ", at " + at.Format(time.RFC3339Nano)
		}
		report.Steps = append(report.Steps, RestoreStep{Name: "db", Detail: detail})

		changed, regenerateErr := Regenerate(ctx, env, store)
		if regenerateErr != nil {
			return failRestoreStep(report, service, "litestream", "litestream regeneration", regenerateErr, stopped)
		}
		regenerateDetail := "unchanged"
		if changed {
			regenerateDetail = database.Path
		}
		report.Steps = append(report.Steps, RestoreStep{Name: "litestream", Detail: regenerateDetail})
	}
	if err := regenerateNginx(ctx); err != nil {
		return report, &RestoreError{Service: service, Stage: "nginx regeneration", Err: err, Stopped: stopped}
	}
	if databaseIncoming {
		if err := runRestoreCommand(ctx, env, "start litestream.service", "systemctl", "start", "litestream.service"); err != nil {
			return failRestoreStep(report, service, "start", "start", err, stopped)
		}
		stopped = restoreStoppedUnits(unit, activationIntent, false)
	}
	startDetail := restoreStartDetail(unit, unitInstalled, activationIntent, databaseIncoming)
	if activationIntent {
		if err := runRestoreCommand(ctx, env, "start "+unit, "systemctl", "start", unit); err != nil {
			return failRestoreStep(report, service, "start", "start", err, stopped)
		}
	}
	report.Steps = append(report.Steps, RestoreStep{Name: "start", Detail: startDetail})
	if activationIntent {
		if err := removeRestoreActivationMarker(env.Root, service); err != nil {
			return report, &RestoreError{Service: service, Stage: "activation marker", Err: err}
		}
	}
	return report, nil
}

type restoreLTXFile struct {
	Timestamp time.Time `json:"timestamp"`
}

func restoreServiceDatabase(ctx context.Context, env host.Env, prefix, service string, database apps.Database, at *time.Time) (string, error) {
	if err := prepareRestoredDatabasePath(env.Root, service, database.Path); err != nil {
		return "", err
	}
	replica := strings.TrimSuffix(prefix, "/") + "/" + service + "/"
	result, err := env.Execute(ctx, host.Command{Name: "litestream", Args: []string{"ltx", "-level", "all", "-json", replica}})
	if err != nil || result.ExitCode != 0 {
		return "", restoreCommandError("list Litestream restore points", result, err)
	}
	var files []restoreLTXFile
	if err := json.Unmarshal(result.Stdout, &files); err != nil {
		return "", fmt.Errorf("list Litestream restore points: invalid JSON: %w", err)
	}
	var recovered time.Time
	for _, file := range files {
		if file.Timestamp.IsZero() || at != nil && file.Timestamp.After(*at) {
			continue
		}
		if recovered.IsZero() || file.Timestamp.After(recovered) {
			recovered = file.Timestamp
		}
	}
	if recovered.IsZero() {
		return "", errors.New("no snapshot under the prefix")
	}
	destination := filepath.Join(env.Root, filepath.FromSlash(path.Join("opt", service, database.Path)))
	args := []string{"restore", "-o", destination}
	if at != nil {
		args = append(args, "-timestamp", at.Format(time.RFC3339Nano))
	}
	args = append(args, replica)
	result, err = env.Execute(ctx, host.Command{Name: "litestream", Args: args})
	if err != nil || result.ExitCode != 0 {
		if bytes.Contains(result.Stderr, []byte("no matching backup files")) {
			return "", errors.New("no snapshot under the prefix")
		}
		return "", restoreCommandError("restore database with Litestream", result, err)
	}
	if _, err := os.Lstat(destination); err != nil {
		return "", fmt.Errorf("restore database with Litestream: %w", err)
	}
	return recovered.UTC().Format(time.RFC3339Nano), nil
}

func prepareRestoredDatabasePath(rootName, service, databasePath string) error {
	filesystem, err := os.OpenRoot(rootName)
	if err != nil {
		return fmt.Errorf("open restore root: %w", err)
	}
	defer func() { _ = filesystem.Close() }()
	relative := filepath.FromSlash(path.Join("opt", service, databasePath))
	for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
		info, statErr := filesystem.Lstat(parent)
		if statErr != nil {
			return fmt.Errorf("inspect database directory: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("database directory %q is not a directory", parent)
		}
	}
	for _, name := range []string{relative, relative + "-wal", relative + "-shm"} {
		if err := filesystem.RemoveAll(name); err != nil {
			return fmt.Errorf("remove prior database state: %w", err)
		}
	}
	return nil
}

func setRestoredDatabaseWAL(rootName, service, databasePath string) error {
	filesystem, err := os.OpenRoot(rootName)
	if err != nil {
		return fmt.Errorf("open restore root: %w", err)
	}
	defer func() { _ = filesystem.Close() }()
	name := filepath.FromSlash(path.Join("opt", service, databasePath))
	file, err := filesystem.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open restored database: %w", err)
	}
	defer func() { _ = file.Close() }()
	header := make([]byte, 100)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("read restored database header: %w", err)
	}
	if !bytes.Equal(header[:16], []byte("SQLite format 3\x00")) {
		return errors.New("restored database is not SQLite")
	}
	if header[18] != 2 || header[19] != 2 {
		header[18], header[19] = 2, 2
		if _, err := file.WriteAt(header[18:20], 18); err != nil {
			return fmt.Errorf("set restored database WAL mode: %w", err)
		}
		if err := file.Sync(); err != nil {
			return fmt.Errorf("sync restored database WAL mode: %w", err)
		}
	}
	return nil
}

func applyRestoredDatabaseOwnership(rootName, service, databasePath string, identity restoreIdentity) error {
	if !identity.needed {
		return nil
	}
	filesystem, err := os.OpenRoot(rootName)
	if err != nil {
		return fmt.Errorf("open restore root: %w", err)
	}
	defer func() { _ = filesystem.Close() }()
	base := filepath.FromSlash(path.Join("opt", service, databasePath))
	for _, name := range []string{base, base + "-wal", base + "-shm"} {
		info, err := filesystem.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("database path %q is not a regular file", name)
		}
		if err := filesystem.Chmod(name, 0o600); err != nil {
			return fmt.Errorf("set database mode: %w", err)
		}
		if err := filesystem.Chown(name, identity.uid, identity.gid); err != nil {
			return fmt.Errorf("set database ownership: %w", err)
		}
	}
	return nil
}

func restoreStartDetail(unit string, installed, active, database bool) string {
	app := "no app unit"
	if unit != "" {
		switch {
		case active:
			app = unit
		case installed:
			app = unit + " left inactive"
		default:
			app = "no " + unit
		}
	}
	if database {
		if active {
			return "litestream.service, " + app
		}
		if installed {
			return "litestream.service, " + app
		}
		return "litestream.service"
	}
	return app
}

type restoreIdentity struct {
	needed bool
	uid    int
	gid    int
}

func failRestoreStep(report RestoreReport, service, step, stage string, err error, stopped []string) (RestoreReport, error) {
	report.Steps = append(report.Steps, RestoreStep{Name: step, Err: err})
	return report, &RestoreError{Service: service, Stage: stage, Err: err, Stopped: stopped}
}

func loadServiceRestoreSource(ctx context.Context, env host.Env, client cloud.Client, prefix, service string, at *time.Time) (serviceRestoreSource, error) {
	servicePrefix := strings.TrimSuffix(prefix, "/") + "/" + service + "/"
	objects, err := client.ListObjects(ctx, servicePrefix)
	if err != nil {
		return serviceRestoreSource{}, fmt.Errorf("list backups for %q: %w", service, err)
	}
	if err := ctx.Err(); err != nil {
		return serviceRestoreSource{}, fmt.Errorf("list backups for %q: %w", service, err)
	}
	selected, anyBackup, found := selectServiceRestoreObject(objects, servicePrefix, at)
	if !anyBackup {
		return serviceRestoreSource{}, fmt.Errorf("no backups for %s under %s", service, servicePrefix)
	}
	if !found {
		return serviceRestoreSource{}, fmt.Errorf("%s: no backup at or before %s", service, at.Format(time.RFC3339Nano))
	}

	reader, err := client.GetObject(ctx, selected.URI)
	if err != nil {
		if reader != nil {
			err = errors.Join(err, reader.Close())
		}
		return serviceRestoreSource{}, fmt.Errorf("download %q: %w", selected.URI, err)
	}
	if reader == nil {
		return serviceRestoreSource{}, fmt.Errorf("download %q: object reader is not configured", selected.URI)
	}
	compressed, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return serviceRestoreSource{}, fmt.Errorf("download %q: %w", selected.URI, err)
	}
	if err := ctx.Err(); err != nil {
		return serviceRestoreSource{}, fmt.Errorf("download %q: %w", selected.URI, err)
	}

	archive, err := decompressServiceArchive(ctx, env.Execute, service, compressed)
	if err != nil {
		return serviceRestoreSource{}, err
	}
	entries, manifest, err := validateServiceRestoreArchive(archive, service)
	if err != nil {
		return serviceRestoreSource{}, fmt.Errorf("validate %q: %w", selected.URI, err)
	}
	if err := ctx.Err(); err != nil {
		return serviceRestoreSource{}, fmt.Errorf("validate %q: %w", selected.URI, err)
	}
	return serviceRestoreSource{
		basename: strings.TrimPrefix(selected.URI, servicePrefix),
		size:     int64(len(compressed)),
		entries:  entries,
		manifest: manifest,
	}, nil
}

func selectServiceRestoreObject(objects []cloud.Object, prefix string, at *time.Time) (cloud.Object, bool, bool) {
	var selected cloud.Object
	var selectedTime time.Time
	anyBackup := false
	found := false
	for _, object := range objects {
		if !strings.HasPrefix(object.URI, prefix) {
			continue
		}
		basename := strings.TrimPrefix(object.URI, prefix)
		if basename == "" || strings.Contains(basename, "/") || !strings.HasSuffix(basename, ".tar.zst") {
			continue
		}
		stamp, err := time.Parse(time.RFC3339Nano, strings.TrimSuffix(basename, ".tar.zst"))
		if err != nil {
			continue
		}
		anyBackup = true
		if at != nil && stamp.After(*at) {
			continue
		}
		if !found || stamp.After(selectedTime) || stamp.Equal(selectedTime) && object.URI < selected.URI {
			selected = object
			selectedTime = stamp
			found = true
		}
	}
	return selected, anyBackup, found
}

func decompressServiceArchive(ctx context.Context, execute func(context.Context, host.Command) (host.Result, error), service string, compressed []byte) ([]byte, error) {
	result, err := execute(ctx, host.Command{
		Name:  "zstd",
		Args:  []string{"--quiet", "--decompress", "--stdout"},
		Stdin: bytes.NewReader(compressed),
	})
	label := "decompress " + fmt.Sprintf("%q", service) + " archive"
	if err != nil {
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			return nil, err
		}
		return nil, &host.CommandError{Label: label, Result: result, Err: err}
	}
	if result.ExitCode != 0 {
		return nil, &host.CommandError{Label: label, Result: result}
	}
	return result.Stdout, nil
}

func validateServiceRestoreArchive(archive []byte, service string) ([]serviceRestoreEntry, *apps.Manifest, error) {
	if len(archive) < 1024 || len(archive)%512 != 0 || !allZero(archive[len(archive)-1024:]) {
		return nil, nil, errors.New("invalid tar archive termination")
	}
	raw := bytes.NewReader(archive)
	reader := tar.NewReader(raw)
	entries := make([]serviceRestoreEntry, 0)
	byName := make(map[string]serviceRestoreEntry)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			remaining := archive[len(archive)-raw.Len():]
			if !allZero(remaining) {
				return nil, nil, errors.New("invalid data after tar archive end")
			}
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read tar archive: %w", err)
		}
		name, err := validServiceArchiveName(header.Name)
		if err != nil {
			return nil, nil, err
		}
		if _, duplicate := byName[name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate archive entry %q", header.Name)
		}
		entry := serviceRestoreEntry{
			name: name, typeflag: header.Typeflag, mode: restoredFileMode(header.Mode), linkname: header.Linkname,
			uid: header.Uid, gid: header.Gid, uname: header.Uname, gname: header.Gname,
		}
		switch header.Typeflag {
		case tar.TypeReg, byte(0):
			entry.typeflag = tar.TypeReg
			entry.data, err = io.ReadAll(reader)
			if err != nil {
				return nil, nil, fmt.Errorf("read archive entry %q: %w", header.Name, err)
			}
		case tar.TypeDir, tar.TypeSymlink:
			if header.Size != 0 {
				return nil, nil, fmt.Errorf("archive entry %q has unexpected data", header.Name)
			}
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return nil, nil, fmt.Errorf("archive entry %q is a special device", header.Name)
		default:
			return nil, nil, fmt.Errorf("archive entry %q has unsupported type %d", header.Name, header.Typeflag)
		}
		entries = append(entries, entry)
		byName[name] = entry
	}
	for _, entry := range entries {
		for parent := path.Dir(entry.name); parent != "."; parent = path.Dir(parent) {
			ancestor, ok := byName[parent]
			if !ok {
				continue
			}
			if ancestor.typeflag == tar.TypeSymlink {
				return nil, nil, fmt.Errorf("archive entry %q traverses symlink %q", entry.name, parent)
			}
			if ancestor.typeflag != tar.TypeDir {
				return nil, nil, fmt.Errorf("archive entry %q traverses non-directory %q", entry.name, parent)
			}
		}
	}
	manifestEntry, exists := byName["etc/manifest.toml"]
	if !exists {
		return entries, nil, nil
	}
	if manifestEntry.typeflag != tar.TypeReg {
		return nil, nil, errors.New("incoming manifest is not a regular file")
	}
	manifest, err := apps.ParseManifest(manifestEntry.data)
	if err != nil {
		return nil, nil, err
	}
	if manifest.App != "" && manifest.App != service {
		return nil, nil, fmt.Errorf("manifest app %q does not match service %q", manifest.App, service)
	}
	return entries, &manifest, nil
}

func inspectRestoreUnit(ctx context.Context, env host.Env, unit string) (bool, bool, error) {
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=ActiveState", unit}})
	if err != nil || result.ExitCode != 0 {
		return false, false, restoreCommandError("inspect "+unit, result, err)
	}
	values := map[string]string{}
	for line := range strings.SplitSeq(strings.ReplaceAll(string(result.Stdout), "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	if values["LoadState"] == "" || values["ActiveState"] == "" {
		return false, false, errors.New("inspect " + unit + ": response omitted unit state")
	}
	installed := values["LoadState"] != "not-found"
	return installed, installed && values["ActiveState"] == "active", nil
}

func runRestoreCommand(ctx context.Context, env host.Env, label, name string, args ...string) error {
	result, err := env.Execute(ctx, host.Command{Name: name, Args: args})
	if err != nil || result.ExitCode != 0 {
		return restoreCommandError(label, result, err)
	}
	return nil
}

func restoreCommandError(label string, result host.Result, err error) error {
	var commandErr *host.CommandError
	if errors.As(err, &commandErr) {
		return err
	}
	return &host.CommandError{Label: label, Result: result, Err: err}
}

func restoreStopDetail(unit string, installed, active, database bool) string {
	app := "no app unit"
	if unit != "" {
		switch {
		case active:
			app = unit
		case installed:
			app = unit + " already inactive"
		default:
			app = "no " + unit
		}
	}
	if !database {
		return app
	}
	if active {
		return app + ", litestream.service"
	}
	return "litestream.service, " + app
}

func restoreStoppedUnits(unit string, appStopped, litestreamStopped bool) []string {
	var stopped []string
	if appStopped {
		stopped = append(stopped, unit)
	}
	if litestreamStopped {
		stopped = append(stopped, "litestream.service")
	}
	return stopped
}

func restoreMarkerName(service string) string {
	return path.Join("run/opsctl/restore", service+".active")
}

func hasRestoreActivationMarker(rootName, service string) (bool, error) {
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(restoreMarkerName(service))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("restore activation marker is not a regular file")
	}
	return true, nil
}

func publishRestoreActivationMarker(rootName, service string) error {
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, directory := range []string{"run", "run/opsctl", "run/opsctl/restore"} {
		if err := root.MkdirAll(directory, 0o700); err != nil {
			return err
		}
		if directory != "run" {
			if err := root.Chmod(directory, 0o700); err != nil {
				return err
			}
		}
	}
	file, err := root.OpenFile(restoreMarkerName(service), os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if closeErr := file.Close(); closeErr != nil {
		return closeErr
	}
	return root.Chmod(restoreMarkerName(service), 0o600)
}

func removeRestoreActivationMarker(rootName, service string) error {
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.Remove(restoreMarkerName(service)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func prepareRestoreIdentity(ctx context.Context, env host.Env, service string, entries []serviceRestoreEntry, manifest *apps.Manifest) (restoreIdentity, error) {
	needed := manifest != nil && manifest.App == service
	for _, entry := range entries {
		needed = needed || entry.uname == "ikigenba" || entry.gname == "ikigenba"
	}
	if !needed {
		return restoreIdentity{}, nil
	}
	uid, gid, err := ensureRestoreAccount(ctx, env)
	if err != nil {
		return restoreIdentity{}, err
	}
	return restoreIdentity{needed: true, uid: uid, gid: gid}, nil
}

func ensureRestoreAccount(ctx context.Context, env host.Env) (int, int, error) {
	lookup := func() (int, int, bool, error) {
		result, err := env.Execute(ctx, host.Command{Name: "getent", Args: []string{"passwd", "ikigenba"}})
		if err != nil {
			return 0, 0, false, restoreCommandError("inspect ikigenba account", result, err)
		}
		if result.ExitCode == 2 {
			return 0, 0, false, nil
		}
		if result.ExitCode != 0 {
			return 0, 0, false, restoreCommandError("inspect ikigenba account", result, nil)
		}
		fields := strings.Split(strings.TrimSpace(string(result.Stdout)), ":")
		if len(fields) < 7 || fields[0] != "ikigenba" {
			return 0, 0, false, errors.New("inspect ikigenba account: invalid response")
		}
		uid, uidErr := strconv.Atoi(fields[2])
		gid, gidErr := strconv.Atoi(fields[3])
		if uidErr != nil || gidErr != nil || uid == 0 {
			return 0, 0, false, errors.New("inspect ikigenba account: invalid non-root identity")
		}
		return uid, gid, true, nil
	}
	uid, gid, found, err := lookup()
	if err != nil {
		return 0, 0, err
	}
	if !found {
		if err := runRestoreCommand(ctx, env, "create ikigenba account", "useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "ikigenba"); err != nil {
			return 0, 0, err
		}
		uid, gid, found, err = lookup()
		if err != nil || !found {
			if err == nil {
				err = errors.New("created ikigenba account is unavailable")
			}
			return 0, 0, err
		}
	}
	if err := runRestoreCommand(ctx, env, "harden ikigenba account", "usermod", "--home", "/nonexistent", "--shell", "/usr/sbin/nologin", "ikigenba"); err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func replaceServiceRestoreTrees(ctx context.Context, rootName, service string, entries []serviceRestoreEntry, identity restoreIdentity) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	stageName, err := os.MkdirTemp(rootName, ".opsctl-service-restore-")
	if err != nil {
		return 0, err
	}
	stage := path.Base(stageName)
	defer func() { _ = root.RemoveAll(stage) }()
	if err := populateServiceRestoreStage(root, rootName, stage, entries, identity); err != nil {
		return 0, err
	}
	if err := root.MkdirAll("opt", 0o755); err != nil {
		return 0, err
	}
	serviceRoot := path.Join("opt", service)
	if _, err := root.Lstat(serviceRoot); errors.Is(err, os.ErrNotExist) {
		if err := root.Mkdir(serviceRoot, 0o755); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	for _, tree := range []string{"etc", "state"} {
		if err := root.RemoveAll(path.Join(serviceRoot, tree)); err != nil {
			return 0, err
		}
		staged := path.Join(stage, tree)
		if _, err := root.Lstat(staged); err == nil {
			if err := root.Rename(staged, path.Join(serviceRoot, tree)); err != nil {
				return 0, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	count := 0
	for _, entry := range entries {
		if entry.typeflag != tar.TypeDir {
			count++
		}
	}
	return count, nil
}

func populateServiceRestoreStage(root *os.Root, rootName, stage string, entries []serviceRestoreEntry, identity restoreIdentity) error {
	ordered := append([]serviceRestoreEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool {
		di, dj := strings.Count(ordered[i].name, "/"), strings.Count(ordered[j].name, "/")
		if di != dj {
			return di < dj
		}
		if ordered[i].typeflag != ordered[j].typeflag {
			return ordered[i].typeflag == tar.TypeDir
		}
		return ordered[i].name < ordered[j].name
	})
	var directories []serviceRestoreEntry
	for _, entry := range ordered {
		name := path.Join(stage, entry.name)
		if err := root.MkdirAll(path.Dir(name), 0o700); err != nil {
			return err
		}
		switch entry.typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o700); err != nil {
				return err
			}
			directories = append(directories, entry)
		case tar.TypeReg:
			file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			_, writeErr := file.Write(entry.data)
			closeErr := file.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				return err
			}
			if err := root.Chmod(name, entry.mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := root.Symlink(entry.linkname, name); err != nil {
				return err
			}
		}
		uid, gid := mappedServiceRestoreOwnership(entry, identity)
		if err := os.Lchown(filepath.Join(rootName, filepath.FromSlash(name)), uid, gid); err != nil {
			return fmt.Errorf("set ownership on %q: %w", entry.name, err)
		}
	}
	for i := len(directories) - 1; i >= 0; i-- {
		entry := directories[i]
		if err := root.Chmod(path.Join(stage, entry.name), entry.mode); err != nil {
			return err
		}
	}
	return nil
}

func mappedServiceRestoreOwnership(entry serviceRestoreEntry, identity restoreIdentity) (int, int) {
	uid, gid := entry.uid, entry.gid
	if identity.needed && entry.uname == "ikigenba" {
		uid = identity.uid
	}
	if identity.needed && entry.gname == "ikigenba" {
		gid = identity.gid
	}
	return uid, gid
}

func validServiceArchiveName(headerName string) (string, error) {
	if headerName == "" || path.IsAbs(headerName) {
		return "", fmt.Errorf("unsafe archive entry %q", headerName)
	}
	name := strings.TrimSuffix(headerName, "/")
	if name == "" || path.Clean(name) != name {
		return "", fmt.Errorf("unsafe archive entry %q", headerName)
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." || component == ".." {
			return "", fmt.Errorf("unsafe archive entry %q", headerName)
		}
	}
	if name != "etc" && !strings.HasPrefix(name, "etc/") && name != "state" && !strings.HasPrefix(name, "state/") {
		return "", fmt.Errorf("archive entry %q is outside service restore trees", headerName)
	}
	return name, nil
}

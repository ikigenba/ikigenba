package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
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
}

type serviceRestoreSource struct {
	basename string
	size     int64
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
		return report, fmt.Errorf("open backup storage: %w", err)
	}
	if client == nil {
		return report, errors.New("open backup storage: cloud client is not configured")
	}
	if err := ctx.Err(); err != nil {
		return report, fmt.Errorf("restore %q: %w", service, err)
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
	return report, nil
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
	_, _, err = validateServiceRestoreArchive(archive, service)
	if err != nil {
		return serviceRestoreSource{}, fmt.Errorf("validate %q: %w", selected.URI, err)
	}
	if err := ctx.Err(); err != nil {
		return serviceRestoreSource{}, fmt.Errorf("validate %q: %w", selected.URI, err)
	}
	return serviceRestoreSource{
		basename: strings.TrimPrefix(selected.URI, servicePrefix),
		size:     int64(len(compressed)),
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
		entry := serviceRestoreEntry{name: name, typeflag: header.Typeflag, mode: restoredFileMode(header.Mode), linkname: header.Linkname}
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

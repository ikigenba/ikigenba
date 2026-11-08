package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// SnapshotResult describes one service snapshot attempt.
type SnapshotResult struct {
	Service string
	URI     string
	Size    int64
	Err     error
}

// Snapshot uploads service files and database copies rebuilt from their replicas.
func Snapshot(ctx context.Context, env host.Env, cloudEnv cloud.Env, store config.Store, service string) ([]SnapshotResult, error) {
	prefix, region, err := fileBackupConfiguration(store, service)
	if err != nil {
		return nil, err
	}
	plan, err := prepareFileBackup(ctx, env, cloudEnv, prefix, region, service)
	if err != nil {
		return nil, err
	}
	results := make([]SnapshotResult, 0, len(plan.services))
	for _, selected := range plan.services {
		if err := ctx.Err(); err != nil {
			return results, fmt.Errorf("snapshot: %w", err)
		}
		result := SnapshotResult{Service: selected.Name}
		var legacyErr error
		if !invalidFileServiceName(selected.Name) {
			legacyErr = legacyStateError(env.Root, selected.Name)
		}
		switch {
		case invalidFileServiceName(selected.Name):
			result.Err = fmt.Errorf("invalid service %q", selected.Name)
		case legacyErr != nil:
			result.Err = legacyErr
		case selected.ManifestError != nil:
			result.Err = snapshotReadError(env.Root, selected.ManifestError)
		default:
			uri := strings.TrimSuffix(plan.prefix, "/") + "/snapshots/" + selected.Name + "/" + plan.basename
			size, archiveErr := uploadHostArchive(ctx, env, plan.client, uri, selected.Name, func(ctx context.Context, env host.Env, writer io.Writer) error {
				return snapshotArchive(ctx, env, plan.prefix, region, selected, writer)
			})
			result.Err = archiveErr
			var uploadErr *archiveUploadError
			if errors.As(archiveErr, &uploadErr) {
				result.Err = uploadErr.Unwrap()
			}
			if archiveErr == nil {
				result.URI = uri
				result.Size = size
			}
		}
		results = append(results, result)
		if interrupted := interruption(ctx, result.Err); interrupted != nil {
			return results, fmt.Errorf("snapshot: %w", interrupted)
		}
	}
	return results, nil
}

func snapshotArchive(ctx context.Context, env host.Env, prefix, region string, service apps.Service, output io.Writer) (returnedErr error) {
	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	resolver := identityResolver{ctx: ctx, execute: env.Execute, users: make(map[uint32]string), groups: make(map[uint32]string)}
	var database *snapshotDatabaseCopy
	if service.Manifest != nil && service.Manifest.Database != nil {
		database, err = snapshotDatabase(ctx, env, filesystem, &resolver, prefix, region, service)
		if err != nil {
			return snapshotReadError(env.Root, err)
		}
		defer func() { returnedErr = errors.Join(returnedErr, database.close()) }()
	}
	writer := tar.NewWriter(output)
	if err := archiveTree(ctx, filesystem, writer, &resolver, service, "state"); err != nil {
		return snapshotReadError(env.Root, err)
	}
	if database != nil {
		if err := writer.WriteHeader(database.header); err != nil {
			return err
		}
		if _, err := io.Copy(writer, database.file); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return ctx.Err()
}

type snapshotDatabaseCopy struct {
	header    *tar.Header
	file      *os.File
	directory string
}

func (database *snapshotDatabaseCopy) close() error {
	return errors.Join(database.file.Close(), os.RemoveAll(database.directory))
}

func snapshotDatabase(ctx context.Context, env host.Env, filesystem *os.Root, resolver *identityResolver, prefix, region string, service apps.Service) (copyResult *snapshotDatabaseCopy, returnedErr error) {
	if env.Execute == nil {
		return nil, errors.New("snapshot database: host execution is not configured")
	}
	replica := strings.TrimSuffix(prefix, "/") + "/" + service.Name + "/?region=" + region
	result, err := env.Execute(ctx, host.Command{Name: "litestream", Args: []string{"ltx", "-level", "all", "-json", replica}})
	if err != nil || result.ExitCode != 0 {
		return nil, snapshotCommandError("list Litestream snapshot replica", result, err)
	}
	var files []json.RawMessage
	if err := json.Unmarshal(result.Stdout, &files); err != nil {
		return nil, fmt.Errorf("list Litestream snapshot replica: invalid JSON: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("no replica under the prefix")
	}
	directory, err := os.MkdirTemp(env.Root, ".opsctl-snapshot-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if copyResult == nil {
			returnedErr = errors.Join(returnedErr, os.RemoveAll(directory))
		}
	}()
	destination := filepath.Join(directory, "database")
	result, err = env.Execute(ctx, host.Command{Name: "litestream", Args: []string{"restore", "-o", destination, replica}})
	if err != nil || result.ExitCode != 0 {
		if bytes.Contains(result.Stderr, []byte("no matching backup files")) {
			return nil, errors.New("no replica under the prefix")
		}
		return nil, snapshotCommandError("rebuild snapshot database", result, err)
	}
	relative := service.Manifest.Database.Path
	info, err := filesystem.Stat(path.Join(apps.DataRoot[1:], service.Name, relative))
	if errors.Is(err, os.ErrNotExist) {
		info, err = filesystem.Stat(path.Dir(path.Join(apps.DataRoot[1:], service.Name, relative)))
	}
	if err != nil {
		return nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	uname, err := resolver.user(stat.Uid)
	if err != nil {
		return nil, err
	}
	gname, err := resolver.group(stat.Gid)
	if err != nil {
		return nil, err
	}
	// Open through the confined root even if a failed process left a symlink.
	file, err := filesystem.Open(filepath.ToSlash(filepath.Join(filepath.Base(directory), "database")))
	if err != nil {
		return nil, err
	}
	copyInfo, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	header := &tar.Header{Name: relative, Typeflag: tar.TypeReg, Mode: 0o600, Size: copyInfo.Size(), Uid: int(stat.Uid), Gid: int(stat.Gid), Uname: uname, Gname: gname}
	return &snapshotDatabaseCopy{header: header, file: file, directory: directory}, nil
}

func snapshotCommandError(label string, result host.Result, err error) error {
	var commandErr *host.CommandError
	if errors.As(err, &commandErr) {
		return err
	}
	return &host.CommandError{Label: label, Result: result, Err: err}
}

func snapshotReadError(root string, err error) error {
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		return err
	}
	name := filepath.ToSlash(pathErr.Path)
	name = strings.TrimPrefix(name, strings.TrimSuffix(filepath.ToSlash(root), "/")+"/")
	if strings.HasPrefix(name, "opt/") || strings.HasPrefix(name, "var/opt/") {
		return fmt.Errorf("/%s: %w", name, pathErr.Err)
	}
	return err
}

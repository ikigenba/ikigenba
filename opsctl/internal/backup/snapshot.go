package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		switch {
		case invalidFileServiceName(selected.Name):
			result.Err = fmt.Errorf("invalid service %q", selected.Name)
		case selected.ManifestError != nil:
			result.Err = snapshotReadError(env.Root, selected.ManifestError)
		default:
			compressed, archiveErr := snapshotArchive(ctx, env, plan.prefix, selected)
			result.Err = archiveErr
			if archiveErr == nil {
				if err := ctx.Err(); err != nil {
					result.Err = err
				} else {
					uri := strings.TrimSuffix(plan.prefix, "/") + "/snapshots/" + selected.Name + "/" + plan.basename
					result.Err = plan.client.PutObject(ctx, uri, bytes.NewReader(compressed))
					if result.Err == nil {
						result.Err = ctx.Err()
					}
					if result.Err == nil {
						result.URI = uri
						result.Size = int64(len(compressed))
					}
				}
			}
		}
		results = append(results, result)
		if interrupted := interruption(ctx, result.Err); interrupted != nil {
			return results, fmt.Errorf("snapshot: %w", interrupted)
		}
	}
	return results, nil
}

func snapshotArchive(ctx context.Context, env host.Env, prefix string, service apps.Service) ([]byte, error) {
	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()
	resolver := identityResolver{ctx: ctx, execute: env.Execute, users: make(map[uint32]string), groups: make(map[uint32]string)}
	var databaseHeader *tar.Header
	var databaseData []byte
	if service.Manifest != nil && service.Manifest.Database != nil {
		databaseHeader, databaseData, err = snapshotDatabase(ctx, env, filesystem, &resolver, prefix, service)
		if err != nil {
			return nil, snapshotReadError(env.Root, err)
		}
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	exclusions := append(databaseExclusions(service.Manifest), "etc/env")
	for _, tree := range []string{"etc", "state"} {
		if err := archiveTreeExcluding(ctx, filesystem, writer, &resolver, service, tree, exclusions); err != nil {
			_ = writer.Close()
			return nil, snapshotReadError(env.Root, err)
		}
	}
	if databaseHeader != nil {
		if err := writer.WriteHeader(databaseHeader); err != nil {
			return nil, err
		}
		if _, err := writer.Write(databaseData); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return compressArchive(ctx, env.Execute, service.Name, archive.Bytes())
}

func snapshotDatabase(ctx context.Context, env host.Env, filesystem *os.Root, resolver *identityResolver, prefix string, service apps.Service) (headerResult *tar.Header, dataResult []byte, returnedErr error) {
	if env.Execute == nil {
		return nil, nil, errors.New("snapshot database: host execution is not configured")
	}
	replica := strings.TrimSuffix(prefix, "/") + "/" + service.Name + "/"
	result, err := env.Execute(ctx, host.Command{Name: "litestream", Args: []string{"ltx", "-level", "all", "-json", replica}})
	if err != nil || result.ExitCode != 0 {
		return nil, nil, snapshotCommandError("list Litestream snapshot replica", result, err)
	}
	var files []json.RawMessage
	if err := json.Unmarshal(result.Stdout, &files); err != nil {
		return nil, nil, fmt.Errorf("list Litestream snapshot replica: invalid JSON: %w", err)
	}
	if len(files) == 0 {
		return nil, nil, errors.New("no replica under the prefix")
	}
	directory, err := os.MkdirTemp(env.Root, ".opsctl-snapshot-")
	if err != nil {
		return nil, nil, err
	}
	defer func() { returnedErr = errors.Join(returnedErr, os.RemoveAll(directory)) }()
	destination := filepath.Join(directory, "database")
	result, err = env.Execute(ctx, host.Command{Name: "litestream", Args: []string{"restore", "-o", destination, replica}})
	if err != nil || result.ExitCode != 0 {
		if bytes.Contains(result.Stderr, []byte("no matching backup files")) {
			return nil, nil, errors.New("no replica under the prefix")
		}
		return nil, nil, snapshotCommandError("rebuild snapshot database", result, err)
	}
	relative := service.Manifest.Database.Path
	info, err := filesystem.Stat(path.Join("opt", service.Name, relative))
	if errors.Is(err, os.ErrNotExist) {
		info, err = filesystem.Stat(path.Dir(path.Join("opt", service.Name, relative)))
	}
	if err != nil {
		return nil, nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	uname, err := resolver.user(stat.Uid)
	if err != nil {
		return nil, nil, err
	}
	gname, err := resolver.group(stat.Gid)
	if err != nil {
		return nil, nil, err
	}
	// Read through the confined root even if a failed process left a symlink.
	data, err := filesystem.ReadFile(filepath.ToSlash(filepath.Join(filepath.Base(directory), "database")))
	if err != nil {
		return nil, nil, err
	}
	header := &tar.Header{Name: relative, Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(data)), Uid: int(stat.Uid), Gid: int(stat.Gid), Uname: uname, Gname: gname}
	return header, data, nil
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
	if strings.HasPrefix(name, "opt/") {
		return fmt.Errorf("/%s: %w", name, pathErr.Err)
	}
	return err
}

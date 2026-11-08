package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// HostBackupResult describes the accounts and configuration archive attempts.
type HostBackupResult struct {
	Accounts FileResult
	Host     FileResult
}

// HostBackup archives the accounts before publishing their configuration.
func HostBackup(ctx context.Context, env host.Env, cloudEnv cloud.Env, store config.Store) (HostBackupResult, error) {
	var result HostBackupResult
	prefix, region, err := fileBackupConfiguration(store, "")
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("backup host: %w", err)
	}
	if env.Now == nil {
		return result, errors.New("backup host: host time is not configured")
	}
	if env.Execute == nil {
		return result, errors.New("backup host: host execution is not configured")
	}
	if cloudEnv.Open == nil {
		return result, errors.New("backup host: cloud access is not configured")
	}
	client, err := cloudEnv.Open(ctx, region)
	if err != nil {
		return result, fmt.Errorf("open backup storage: %w", err)
	}
	if client == nil {
		return result, errors.New("open backup storage: cloud client is not configured")
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("backup host: %w", err)
	}
	timestamp := env.Now().UTC().Format(time.RFC3339Nano)
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("backup host: %w", err)
	}
	result.Accounts, err = writeHostArchive(ctx, env, client, prefix, "accounts", timestamp+".accounts.tar.zst", accountsArchive)
	if err != nil {
		return result, err
	}
	if result.Accounts.Err != nil {
		result.Host = FileResult{Service: "host", Err: errors.New("accounts not written")}
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("backup host: %w", err)
	}
	result.Host, err = writeHostArchive(ctx, env, client, prefix, "host", timestamp+".tar.zst", hostArchive)
	return result, err
}

func writeHostArchive(ctx context.Context, env host.Env, client cloud.Client, prefix, service, basename string, archive func(context.Context, host.Env) ([]byte, error)) (FileResult, error) {
	result := FileResult{Service: service}
	compressed, err := archive(ctx, env)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		object := strings.TrimSuffix(prefix, "/") + "/host/" + basename
		err = client.PutObject(ctx, object, bytes.NewReader(compressed))
		if err != nil {
			err = fmt.Errorf("upload %q: %w", object, err)
		}
		if err == nil {
			err = ctx.Err()
		}
	}
	if err != nil {
		result.Err = err
		if interrupted := interruption(ctx, err); interrupted != nil {
			return result, fmt.Errorf("backup host: %w", interrupted)
		}
		return result, nil
	}
	result.Object = basename
	result.Size = int64(len(compressed))
	return result, nil
}

func hostArchive(ctx context.Context, env host.Env) ([]byte, error) {
	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return nil, fmt.Errorf("archive host: %w", err)
	}
	defer func() { _ = filesystem.Close() }()

	var archive bytes.Buffer
	tarWriter := tar.NewWriter(&archive)
	for _, tree := range []string{"etc/ikigenba", "etc/letsencrypt"} {
		if err := archiveHostTree(ctx, filesystem, tarWriter, tree); err != nil {
			_ = tarWriter.Close()
			return nil, fmt.Errorf("archive host: %w", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("archive host: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("archive host: %w", err)
	}
	return compressArchive(ctx, env.Execute, "host", archive.Bytes())
}

func archiveHostTree(ctx context.Context, filesystem *os.Root, writer *tar.Writer, tree string) error {
	if err := hostArchiveParents(filesystem, tree); err != nil {
		return err
	}
	rootInfo, err := filesystem.Lstat(tree)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return archiveHostEntry(filesystem, writer, tree, rootInfo)
	}

	return fs.WalkDir(filesystem.FS(), tree, func(name string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := filesystem.Lstat(name)
		if err != nil {
			return err
		}
		return archiveHostEntry(filesystem, writer, name, info)
	})
}

func archiveHostEntry(filesystem *os.Root, writer *tar.Writer, name string, info fs.FileInfo) error {
	link := ""
	var err error
	if info.Mode()&os.ModeSymlink != 0 {
		link, err = filesystem.Readlink(name)
		if err != nil {
			return err
		}
	}
	header, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	header.Name = path.Clean(name)
	if info.IsDir() {
		header.Name += "/"
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	file, err := filesystem.Open(name)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, file)
	closeErr := file.Close()
	return errors.Join(copyErr, closeErr)
}

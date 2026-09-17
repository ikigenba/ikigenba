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

// HostBackup archives and uploads the host configuration and certificate trees.
func HostBackup(ctx context.Context, env host.Env, cloudEnv cloud.Env, store config.Store) (FileResult, error) {
	prefix, region, err := fileBackupConfiguration(store, "")
	if err != nil {
		return FileResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileResult{}, fmt.Errorf("backup host: %w", err)
	}
	if env.Now == nil {
		return FileResult{}, errors.New("backup host: host time is not configured")
	}
	if env.Execute == nil {
		return FileResult{}, errors.New("backup host: host execution is not configured")
	}
	if cloudEnv.Open == nil {
		return FileResult{}, errors.New("backup host: cloud access is not configured")
	}
	client, err := cloudEnv.Open(ctx, region)
	if err != nil {
		return FileResult{}, fmt.Errorf("open backup storage: %w", err)
	}
	if client == nil {
		return FileResult{}, errors.New("open backup storage: cloud client is not configured")
	}
	if err := ctx.Err(); err != nil {
		return FileResult{}, fmt.Errorf("backup host: %w", err)
	}

	basename := env.Now().UTC().Format(time.RFC3339Nano) + ".tar.zst"
	result := FileResult{Service: "host"}
	compressed, archiveErr := hostArchive(ctx, env)
	if archiveErr != nil {
		result.Err = archiveErr
		if interrupted := interruption(ctx, archiveErr); interrupted != nil {
			return result, fmt.Errorf("backup host: %w", interrupted)
		}
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		result.Err = fmt.Errorf("archive host: %w", err)
		return result, fmt.Errorf("backup host: %w", err)
	}

	object := strings.TrimSuffix(prefix, "/") + "/host/" + basename
	if uploadErr := client.PutObject(ctx, object, bytes.NewReader(compressed)); uploadErr != nil {
		result.Err = fmt.Errorf("upload %q: %w", object, uploadErr)
		if interrupted := interruption(ctx, uploadErr); interrupted != nil {
			return result, fmt.Errorf("backup host: %w", interrupted)
		}
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		result.Err = fmt.Errorf("upload %q: %w", object, err)
		return result, fmt.Errorf("backup host: %w", err)
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

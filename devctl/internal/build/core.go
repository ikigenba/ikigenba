package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func executeStaged(ctx context.Context, deps seam.Deps, path, dir, label, argument string) ([]byte, error) {
	command := seam.Cmd{Path: path, Args: []string{argument}, Dir: dir}
	result, err := executeResult(ctx, deps, label, command)
	return result.Stdout, err
}

func execute(ctx context.Context, deps seam.Deps, label string, command seam.Cmd) error {
	_, err := executeResult(ctx, deps, label, command)
	return err
}

func executeResult(ctx context.Context, deps seam.Deps, label string, command seam.Cmd) (seam.Result, error) {
	result, err := deps.Exec(ctx, command)
	if err != nil {
		return seam.Result{}, fmt.Errorf("%s: %w", command.Path, err)
	}
	if result.ExitCode != 0 {
		return seam.Result{}, &ProcessError{
			Label:  label,
			Status: result.ExitCode,
			Stderr: string(result.Stderr),
		}
	}
	return result, nil
}

func checkManifest(app checkout.App, emitted []byte) (os.FileMode, error) {
	root, err := os.OpenRoot(app.Dir)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	committed, err := root.Open(checkout.ManifestFile)
	if err != nil {
		return 0, err
	}
	defer func() { _ = committed.Close() }()
	info, err := committed.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() != int64(len(emitted)) {
		return 0, &StaleManifestError{App: app.Name}
	}
	var buffer [4096]byte
	for len(emitted) > 0 {
		chunk := buffer[:min(len(buffer), len(emitted))]
		if _, err := io.ReadFull(committed, chunk); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return 0, &StaleManifestError{App: app.Name}
			}
			return 0, err
		}
		if !bytes.Equal(chunk, emitted[:len(chunk)]) {
			return 0, &StaleManifestError{App: app.Name}
		}
		emitted = emitted[len(chunk):]
	}
	return info.Mode().Perm(), nil
}

func copyArchiveDirectory(appDir, archiveRoot, directory string) error {
	sourceRoot := filepath.Join(appDir, directory)
	if _, err := os.Stat(sourceRoot); err != nil {
		if os.IsNotExist(err) && directory != "etc" {
			return nil
		}
		return err
	}

	return filepath.WalkDir(sourceRoot, func(source string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(appDir, source)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == checkout.ManifestFile {
			return nil
		}
		return copyArchiveFile(source, filepath.Join(archiveRoot, relative))
	})
}

func copyArchiveFile(source, destination string) error {
	sourceRoot, err := os.OpenRoot(filepath.Dir(source))
	if err != nil {
		return err
	}
	defer func() { _ = sourceRoot.Close() }()
	file, err := sourceRoot.Open(filepath.Base(source))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	return writeArchiveFile(destination, file, mode)
}

func writeArchiveFile(path string, contents io.Reader, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

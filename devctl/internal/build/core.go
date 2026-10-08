package build

import (
	"context"
	"fmt"
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

func copyArchiveDirectory(appDir, archiveRoot, directory string) ([]string, error) {
	sourceRoot := filepath.Join(appDir, directory)
	if _, err := os.Stat(sourceRoot); err != nil {
		if os.IsNotExist(err) && directory != "etc" {
			return nil, nil
		}
		return nil, err
	}

	var members []string
	err := filepath.WalkDir(sourceRoot, func(source string, entry os.DirEntry, walkErr error) error {
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
		if err := copyArchiveFile(source, filepath.Join(archiveRoot, relative)); err != nil {
			return err
		}
		members = append(members, filepath.ToSlash(relative))
		return nil
	})
	return members, err
}

func copyArchiveFile(source, destination string) error {
	sourceRoot, err := os.OpenRoot(filepath.Dir(source))
	if err != nil {
		return err
	}
	contents, err := sourceRoot.ReadFile(filepath.Base(source))
	closeErr := sourceRoot.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	return writeArchiveFile(destination, contents, mode)
}

func writeArchiveFile(path string, contents []byte, mode os.FileMode) error {
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
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

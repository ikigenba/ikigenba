package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// Write regenerates and publishes the launcher service listing.
func Write(ctx context.Context, env host.Env, hostName string) (Changes, error) {
	if hostName == "" {
		return nil, errors.New("host.name not set")
	}
	layout, err := apps.ReadLayout(env.Root)
	if err != nil {
		return nil, err
	}
	servicesPath := apps.ServicesPath
	if layout == apps.PerApp {
		servicesPath = apps.PerAppServicesPath
	}
	return write(ctx, env, hostName, servicesPath, false)
}

// Apply publishes the current release listing at the volatile services path.
func Apply(ctx context.Context, env host.Env, hostName string) error {
	_, err := write(ctx, env, hostName, apps.ServicesPath, true)
	return err
}

func write(ctx context.Context, env host.Env, hostName, servicesPath string, currentOnly bool) (Changes, error) {
	if hostName == "" {
		return nil, errors.New("host.name not set")
	}
	layout, err := apps.ReadLayout(env.Root)
	if err != nil {
		return nil, err
	}
	var candidate []byte
	var entries []entry
	if currentOnly && layout != apps.Released {
		candidate = encodeEntries(nil)
	} else {
		candidate, entries, err = render(ctx, env, hostName)
	}
	if err != nil {
		return nil, err
	}

	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()

	file := strings.TrimPrefix(servicesPath, "/")
	servicesDirectory := filepath.Dir(file)
	perApp := servicesPath == apps.PerAppServicesPath
	previous, readErr := filesystem.ReadFile(file)
	previousInfo, statErr := filesystem.Lstat(file)
	unchanged := readErr == nil && statErr == nil && previousInfo.Mode().IsRegular() && bytes.Equal(previous, candidate)

	if err := apps.EnsureAccount(ctx, env); err != nil {
		return nil, err
	}
	if err := createServicesDirectory(filesystem, servicesDirectory); err != nil {
		return nil, fmt.Errorf("create services directory: %w", err)
	}
	if perApp {
		if err := filesystem.Chmod(servicesDirectory, 0o750); err != nil {
			return nil, fmt.Errorf("mode services directory: %w", err)
		}
	}

	if unchanged {
		if err := chownServices(ctx, env, servicesDirectory, filepath.Join(env.Root, file), perApp); err != nil {
			return nil, err
		}
		if err := filesystem.Chmod(file, 0o640); err != nil {
			return nil, fmt.Errorf("mode services file: %w", err)
		}
		return classify(previous, entries), nil
	}

	temporary, err := createServicesTemporary(filesystem, servicesDirectory)
	if err != nil {
		return nil, fmt.Errorf("create services temporary file: %w", err)
	}
	defer func() { _ = filesystem.Remove(temporary.name) }()
	if _, err := temporary.file.Write(candidate); err != nil {
		_ = temporary.file.Close()
		return nil, fmt.Errorf("write services temporary file: %w", err)
	}
	if err := temporary.file.Chmod(0o640); err != nil {
		_ = temporary.file.Close()
		return nil, fmt.Errorf("mode services temporary file: %w", err)
	}
	if err := temporary.file.Close(); err != nil {
		return nil, fmt.Errorf("close services temporary file: %w", err)
	}
	if err := chownServices(ctx, env, servicesDirectory, filepath.Join(env.Root, temporary.name), perApp); err != nil {
		return nil, err
	}
	if err := filesystem.Rename(temporary.name, file); err != nil {
		return nil, fmt.Errorf("publish services file: %w", err)
	}
	return classify(previous, entries), nil
}

func createServicesDirectory(filesystem *os.Root, servicesDirectory string) error {
	var directories []string
	for directory := servicesDirectory; directory != "."; directory = filepath.Dir(directory) {
		directories = append(directories, directory)
	}
	slices.Reverse(directories)
	for _, directory := range directories {
		if err := filesystem.Mkdir(directory, 0o755); err == nil {
			if err := filesystem.Chmod(directory, 0o755); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrExist) {
			return err
		} else {
			info, statErr := filesystem.Stat(directory)
			if statErr != nil {
				return statErr
			}
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", directory)
			}
		}
	}
	return nil
}

type servicesTemporary struct {
	name string
	file *os.File
}

func createServicesTemporary(filesystem *os.Root, servicesDirectory string) (servicesTemporary, error) {
	for range 100 {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return servicesTemporary{}, err
		}
		name := filepath.Join(servicesDirectory, fmt.Sprintf(".services-%x", suffix))
		file, err := filesystem.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return servicesTemporary{name: name, file: file}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return servicesTemporary{}, err
		}
	}
	return servicesTemporary{}, errors.New("create unique services temporary file")
}

func chownServices(ctx context.Context, env host.Env, servicesDirectory, file string, perApp bool) error {
	args := []string{"root:ikigenba"}
	if perApp {
		args = append(args, filepath.Join(env.Root, servicesDirectory))
	}
	args = append(args, file)
	command := host.Command{
		Name: "chown",
		Args: args,
	}
	result, err := env.Execute(ctx, command)
	if err != nil || result.ExitCode != 0 {
		return &host.CommandError{Label: "own services file", Result: result, Err: err}
	}
	return nil
}

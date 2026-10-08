package apps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"

	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

// Layout describes how the host holds app packages.
type Layout string

// Fresh, PerApp and Released are the recognized host layouts.
const (
	Fresh    Layout = "fresh host"
	PerApp   Layout = "per app"
	Released Layout = "releases"
)

// ReadLayout classifies the host using the current entry and per-app packages.
func ReadLayout(root string) (Layout, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer func() { _ = filesystem.Close() }()
	_, err = filesystem.Lstat(release.CurrentLink[1:])
	if err == nil {
		return Released, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	entries, err := fs.ReadDir(filesystem.FS(), "opt")
	if errors.Is(err, os.ErrNotExist) {
		return Fresh, nil
	}
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Name() == "ikigenba" || !entry.IsDir() {
			continue
		}
		info, statErr := filesystem.Stat(path.Join("opt", entry.Name(), "etc"))
		if statErr == nil && info.IsDir() {
			return PerApp, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
	}
	return Fresh, nil
}

// Discover reads package capabilities and kept data for the current layout.
func Discover(root string) ([]Service, error) {
	layout, err := ReadLayout(root)
	if err != nil {
		return nil, err
	}
	directory := "/opt"
	if layout == Released {
		directory = release.CurrentLink
	}
	return discoverAt(root, directory, layout == Released, false)
}

// DiscoverRelease reads an explicit release alongside kept app data.
func DiscoverRelease(root, sha string) ([]Service, error) {
	if !release.ValidSHA(sha) {
		return nil, fmt.Errorf("invalid release sha %q", sha)
	}
	return discoverAt(root, release.ReleasesDir+"/"+sha, true, true)
}
func discoverAt(root, directory string, released, required bool) ([]Service, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("discover services: open root: %w", err)
	}
	defer func() { _ = filesystem.Close() }()
	names := map[string]string{}
	packageDirectory := directory[1:]
	entries, err := fs.ReadDir(filesystem.FS(), packageDirectory)
	if errors.Is(err, os.ErrNotExist) && !required {
		entries = nil
	} else if err != nil {
		return nil, fmt.Errorf("discover services: read %s: %w", directory, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || (!released && entry.Name() == "ikigenba") {
			continue
		}
		packagePath := path.Join(packageDirectory, entry.Name())
		if released {
			binary, statErr := filesystem.Stat(path.Join(packagePath, "bin", entry.Name()))
			_, manifestErr := filesystem.Lstat(path.Join(packagePath, "etc", "manifest.toml"))
			if statErr != nil || !binary.Mode().IsRegular() || manifestErr != nil {
				continue
			}
		} else if !isDirectory(filesystem, path.Join(packagePath, "etc")) {
			continue
		}
		names[entry.Name()] = path.Join(directory, entry.Name())
	}
	dataEntries, err := fs.ReadDir(filesystem.FS(), DataRoot[1:])
	if errors.Is(err, os.ErrNotExist) {
		dataEntries = nil
	} else if err != nil {
		return nil, fmt.Errorf("discover services: read %s: %w", DataRoot, err)
	}
	for _, entry := range dataEntries {
		if entry.IsDir() && isDirectory(filesystem, path.Join(DataRoot[1:], entry.Name(), "state")) {
			if _, ok := names[entry.Name()]; !ok {
				names[entry.Name()] = ""
			}
		}
	}
	services := make([]Service, 0, len(names))
	for name, dir := range names {
		service := Service{Name: name, Dir: dir}
		if dir != "" {
			data, readErr := filesystem.ReadFile(path.Join(dir[1:], "etc", "manifest.toml"))
			switch {
			case errors.Is(readErr, os.ErrNotExist):
			case readErr != nil:
				service.ManifestError = fmt.Errorf("read manifest for %q: %w", name, readErr)
			default:
				manifest, parseErr := ParseManifest(data)
				switch {
				case parseErr != nil:
					service.ManifestError = parseErr
				case manifest.App != "" && manifest.App != name:
					service.ManifestError = fmt.Errorf("manifest app %q does not match service directory %q", manifest.App, name)
				default:
					service.Manifest = &manifest
				}
			}
		}
		services = append(services, service)
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services, nil
}
func isDirectory(filesystem *os.Root, name string) bool {
	info, err := filesystem.Stat(name)
	return err == nil && info.IsDir()
}

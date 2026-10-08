package apps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
)

// Discover returns services represented by package or data directories.
func Discover(root string) ([]Service, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("discover services: open root: %w", err)
	}
	defer func() { _ = filesystem.Close() }()

	names := make(map[string]struct{})
	for _, location := range []struct{ directory, marker string }{
		{"opt", "etc"},
		{DataRoot[1:], "state"},
	} {
		entries, readErr := fs.ReadDir(filesystem.FS(), location.directory)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("discover services: read /%s: %w", location.directory, readErr)
		}
		for _, entry := range entries {
			if entry.IsDir() && isDirectory(filesystem, path.Join(location.directory, entry.Name(), location.marker)) {
				names[entry.Name()] = struct{}{}
			}
		}
	}

	services := make([]Service, 0, len(names))
	for name := range names {
		servicePath := path.Join("opt", name)
		service := Service{Name: name}
		manifestPath := path.Join(servicePath, "etc", "manifest.toml")
		data, readErr := filesystem.ReadFile(manifestPath)
		switch {
		case errors.Is(readErr, os.ErrNotExist):
		case readErr != nil:
			service.ManifestError = fmt.Errorf("read manifest for %q: %w", service.Name, readErr)
		default:
			manifest, parseErr := ParseManifest(data)
			switch {
			case parseErr != nil:
				service.ManifestError = parseErr
			case manifest.App != "" && manifest.App != service.Name:
				service.ManifestError = fmt.Errorf("manifest app %q does not match service directory %q", manifest.App, service.Name)
			default:
				service.Manifest = &manifest
			}
		}
		services = append(services, service)
	}

	sort.Slice(services, func(left, right int) bool {
		return services[left].Name < services[right].Name
	})
	return services, nil
}

func isDirectory(filesystem *os.Root, name string) bool {
	info, err := filesystem.Stat(name)
	return err == nil && info.IsDir()
}

package apps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
)

// Discover returns the services represented by directories below /opt.
func Discover(root string) ([]Service, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("discover services: open root: %w", err)
	}
	defer func() { _ = filesystem.Close() }()

	entries, err := fs.ReadDir(filesystem.FS(), "opt")
	if errors.Is(err, os.ErrNotExist) {
		return []Service{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("discover services: read /opt: %w", err)
	}

	services := make([]Service, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		servicePath := path.Join("opt", entry.Name())
		if !isDirectory(filesystem, path.Join(servicePath, "etc")) && !isDirectory(filesystem, path.Join(servicePath, "state")) {
			continue
		}

		service := Service{Name: entry.Name()}
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

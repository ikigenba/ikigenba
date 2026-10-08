package backup

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// installedRestoreManifest performs source preflight without invoking host tools.
func installedRestoreManifest(rootName, service string) (apps.Manifest, error) {
	notInstalled := fmt.Errorf("%s is not installed", service)
	if apps.ValidateName(service) != nil {
		return apps.Manifest{}, notInstalled
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return apps.Manifest{}, err
	}
	defer func() { _ = root.Close() }()
	for _, name := range []string{path.Join("opt", service, "bin", service), path.Join("opt", service, "etc/manifest.toml")} {
		info, err := root.Stat(name)
		if errors.Is(err, os.ErrNotExist) {
			return apps.Manifest{}, notInstalled
		}
		if err != nil {
			return apps.Manifest{}, err
		}
		if !info.Mode().IsRegular() {
			return apps.Manifest{}, notInstalled
		}
	}
	data, err := root.ReadFile(path.Join("opt", service, "etc/manifest.toml"))
	if err != nil {
		return apps.Manifest{}, err
	}
	if apps.ManifestDisowns(data, service) {
		return apps.Manifest{}, notInstalled
	}
	manifest, err := apps.ParseManifest(data)
	if err != nil {
		return apps.Manifest{}, fmt.Errorf("%s: etc/manifest.toml: %w", service, err)
	}
	_, err = root.Lstat(path.Join("opt", service, "state"))
	if err == nil {
		return apps.Manifest{}, fmt.Errorf("/opt/%s/state has not moved", service)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return apps.Manifest{}, err
	}
	_, err = root.Lstat(path.Join(strings.TrimPrefix(apps.EnvRoot, "/"), service, "env"))
	if errors.Is(err, os.ErrNotExist) {
		return apps.Manifest{}, fmt.Errorf("/opt/%s/etc/env has not moved", service)
	}
	if err != nil {
		return apps.Manifest{}, err
	}
	return manifest, nil
}

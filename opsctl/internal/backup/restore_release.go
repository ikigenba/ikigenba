package backup

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func restoreManifest(rootName, service string, layout apps.Layout, own *release.Release) (apps.Manifest, error) {
	if layout == apps.PerApp {
		return installedRestoreManifest(rootName, service)
	}
	missing := fmt.Errorf("%s is not in the current release", service)
	directory := path.Join("opt/ikigenba/current", service)
	if layout == apps.Fresh {
		missing = fmt.Errorf("%s is not in release %s", service, own.Short())
		directory = path.Join("opt/ikigenba/releases", own.SHA, service)
	}
	if apps.ValidateName(service) != nil {
		return apps.Manifest{}, missing
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return apps.Manifest{}, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(directory)
	if os.IsNotExist(err) {
		return apps.Manifest{}, missing
	}
	if err != nil {
		return apps.Manifest{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return apps.Manifest{}, missing
	}
	info, err = root.Stat(path.Join(directory, "bin", service))
	if os.IsNotExist(err) {
		return apps.Manifest{}, missing
	}
	if err != nil {
		return apps.Manifest{}, err
	}
	if !info.Mode().IsRegular() {
		return apps.Manifest{}, missing
	}
	name := path.Join(directory, "etc/manifest.toml")
	if _, err := root.Lstat(name); os.IsNotExist(err) {
		return apps.Manifest{}, missing
	} else if err != nil {
		return apps.Manifest{}, fmt.Errorf("%s: etc/manifest.toml: %w", service, err)
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return apps.Manifest{}, fmt.Errorf("%s: etc/manifest.toml: %w", service, err)
	}
	manifest, err := apps.ParseManifest(data)
	if err != nil {
		return apps.Manifest{}, fmt.Errorf("%s: etc/manifest.toml: %w", service, err)
	}
	if manifest.App != service {
		return apps.Manifest{}, fmt.Errorf("%s: etc/manifest.toml: app must be '%s'", service, service)
	}
	return manifest, nil
}

func prepareRestoreEnvironment(ctx context.Context, remote cloud.Env, client cloud.Client, store config.Store, region, hostName, service string, manifest apps.Manifest, own *release.Release) ([]byte, error) {
	if own == nil {
		return apps.PrepareEnvironment(ctx, client, store, hostName, service, manifest)
	}
	timeouts, err := apps.ReadTimeouts(store)
	if err != nil {
		return nil, err
	}
	secrets, err := apps.ReadSecrets(ctx, remote, region, hostName, manifest)
	if err != nil {
		return nil, err
	}
	return apps.ReleaseEnv(manifest, secrets, timeouts, *own)
}

func publishRestoreEnvironment(env host.Env, service string, data []byte, own *release.Release) error {
	if own == nil {
		return apps.PublishEnvironment(env.Root, service, data)
	}
	return apps.WriteEnv(env, service, data)
}

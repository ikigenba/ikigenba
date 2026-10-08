package apps

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

// PrepareEnvironment validates and renders current host settings without writing them.
func PrepareEnvironment(ctx context.Context, client cloud.Client, store config.Store, hostName, service string, manifest Manifest) ([]byte, error) {
	if !validEnvironmentService(service) {
		return nil, fmt.Errorf("unusable service name %q", service)
	}
	timeouts, err := ReadTimeouts(store)
	if err != nil {
		return nil, err
	}
	manifest.App = service
	values, err := obtainSecrets(ctx, client, hostName, manifest)
	if err != nil {
		return nil, err
	}
	return renderEnvironment(manifest, values, timeouts.DrainSeconds), nil
}

// PublishEnvironment atomically publishes prepared settings with mode 0600.
func PublishEnvironment(root, service string, data []byte) error {
	if !validEnvironmentService(service) {
		return fmt.Errorf("unusable service name %q", service)
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	directory := path.Join(strings.TrimPrefix(EnvRoot, "/"), service)
	for _, name := range []string{"etc", "etc/opt", strings.TrimPrefix(EnvRoot, "/"), directory} {
		info, err := filesystem.Lstat(name)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("destination %s is not a directory", name)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err := filesystem.Mkdir(name, 0o755); err != nil {
			return err
		}
		if err := filesystem.Chmod(name, 0o755); err != nil {
			return err
		}
	}
	return publishAtomicFile(filesystem, directory, path.Join(directory, "env"), data, 0o600)
}

func validEnvironmentService(service string) bool {
	return service != "" && service != "." && service != ".." && !strings.ContainsAny(service, "/\x00")
}

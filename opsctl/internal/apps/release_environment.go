package apps

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

// ServicesUnit writes the release services file at boot.
const ServicesUnit = "ikigenba-services.service"

// ReadSecrets obtains only the distinct keys the manifest requests.
func ReadSecrets(ctx context.Context, remote cloud.Env, region, hostName string, m Manifest) (map[string]string, error) {
	result := make(map[string]string)
	if len(m.Secrets) == 0 {
		return result, nil
	}
	if remote.Open == nil {
		return nil, errors.New("cloud access is not configured")
	}
	client, err := remote.Open(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("open secrets client: %w", err)
	}
	parameter := "/" + hostName + "/" + m.App
	values, err := client.ReadSecrets(ctx, parameter)
	if err != nil && !errors.Is(err, cloud.ErrNotFound) {
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	if errors.Is(err, cloud.ErrNotFound) {
		values = nil
	}
	for _, name := range distinctNames(m.Secrets) {
		value, present := values[name]
		if !present {
			return nil, fmt.Errorf("%s: no value for '%s' in %s", m.App, name, parameter)
		}
		result[name] = value
	}
	return result, nil
}

// ReleaseEnv renders the environment for one release app without host access.
func ReleaseEnv(m Manifest, secrets map[string]string, t Timeouts, r release.Release) ([]byte, error) {
	if err := validateEnvironment(m, secrets); err != nil {
		return nil, err
	}
	for _, name := range distinctNames(m.Secrets) {
		if name == "IKIGENBA_COMMIT" || name == "IKIGENBA_RELEASE" {
			return nil, fmt.Errorf("%s: secret name %s is reserved", m.App, name)
		}
	}
	names := make([]string, 0, len(m.Env))
	for name := range m.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "IKIGENBA_COMMIT" || name == "IKIGENBA_RELEASE" {
			return nil, fmt.Errorf("%s: setting name %s is reserved", m.App, name)
		}
	}
	data := string(renderEnvironment(m, secrets, t.DrainSeconds))
	data = strings.TrimSuffix(data, ServicesEnv+"="+PerAppServicesPath+"\n")
	data += ServicesEnv + "=" + ServicesPath + "\nIKIGENBA_COMMIT=" + r.SHA + "\n"
	if r.Label != "" {
		data += "IKIGENBA_RELEASE=" + r.Label + "\n"
	}
	return []byte(data), nil
}

// WriteEnv replaces an app's host environment without executing a command.
func WriteEnv(env host.Env, app string, data []byte) error {
	if err := ValidateName(app); err != nil {
		return err
	}
	return PublishEnvironment(env.Root, app, data)
}

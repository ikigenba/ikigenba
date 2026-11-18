package apps

import (
	"context"
	"errors"
	"fmt"
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
func ReleaseEnv(m Manifest, secrets map[string]string, t Timeouts, r release.Release, s SpaceEnv) ([]byte, error) {
	const commit = "IKIGENBA_COMMIT"
	const label = "IKIGENBA_RELEASE"
	settings := [...]struct{ name, value string }{
		{"AWS_REGION", s.Region},
		{"IKIGENBA_HOSTS_DOMAIN", s.HostsDomain},
		{"IKIGENBA_HOSTS_ZONE_ID", s.HostsZoneID},
		{"IKIGENBA_HOSTS_LAUNCH_TEMPLATE", s.HostsLaunchTemplate},
		{"IKIGENBA_HOSTS_INSTANCE_PROFILE", s.HostsInstanceProfile},
	}
	reserved := make([]string, 0, len(settings)+2)
	reserved = append(reserved, commit, label)
	for _, setting := range settings {
		reserved = append(reserved, setting.name)
	}
	if err := validateEnvironment(m, secrets, reserved...); err != nil {
		return nil, err
	}
	for _, setting := range settings {
		if invalidEnvironmentValue(setting.value) {
			return nil, fmt.Errorf("%s: %s holds a NUL, CR or LF", m.App, setting.name)
		}
	}
	var output strings.Builder
	writeAppEnvironment(&output, m, secrets, t.DrainSeconds, ServicesPath)
	writeBareEnvironmentEntry(&output, commit, r.SHA)
	if r.Label != "" {
		writeBareEnvironmentEntry(&output, label, r.Label)
	}
	for _, setting := range settings {
		if setting.value != "" {
			writeBareEnvironmentEntry(&output, setting.name, setting.value)
		}
	}
	return []byte(output.String()), nil
}

func writeBareEnvironmentEntry(output *strings.Builder, name, value string) {
	output.WriteString(name)
	output.WriteByte('=')
	output.WriteString(value)
	output.WriteByte('\n')
}

// WriteEnv replaces an app's host environment without executing a command.
func WriteEnv(env host.Env, app string, data []byte) error {
	if err := ValidateName(app); err != nil {
		return err
	}
	return PublishEnvironment(env.Root, app, data)
}

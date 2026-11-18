package apps

import (
	"fmt"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

// SpaceEnv contains the space-wide settings shared by release apps.
type SpaceEnv struct {
	Region               string
	HostsDomain          string
	HostsZoneID          string
	HostsLaunchTemplate  string
	HostsInstanceProfile string
}

// ReadSpaceEnv reads the stored settings together, leaving absent values empty.
func ReadSpaceEnv(store config.Store) (SpaceEnv, error) {
	entries, err := store.List()
	if err != nil {
		return SpaceEnv{}, fmt.Errorf("read space environment: %w", err)
	}
	var settings SpaceEnv
	for _, entry := range entries {
		switch entry.Key {
		case "aws.region":
			settings.Region = entry.Value
		case "hosts.domain":
			settings.HostsDomain = entry.Value
		case "hosts.zone_id":
			settings.HostsZoneID = entry.Value
		case "hosts.launch_template":
			settings.HostsLaunchTemplate = entry.Value
		case "hosts.instance_profile":
			settings.HostsInstanceProfile = entry.Value
		}
	}
	return settings, nil
}

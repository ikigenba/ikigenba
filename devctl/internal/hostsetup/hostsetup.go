package hostsetup

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/host"
)

const installLatestScript = "curl --fail --silent --show-error --location https://github.com/ikigenba/ikigenba/releases/latest/download/opsctl-install.sh | sh && opsctl version"

// Config is the complete devctl-owned configuration of an opsctl host.
type Config struct {
	Domain                    string
	ZoneName                  string
	ZoneID                    string
	Region                    string
	BackupBucket              string
	ACMEEmail                 string
	BackupHostFilesSeconds    int
	BackupServiceFilesSeconds int
	BackupServiceDBSeconds    int
	BackupServiceWALSeconds   int
}

// InstallLatest installs the release selected by the published latest-release
// interface and returns the version reported by the installed binary.
func InstallLatest(ctx context.Context, target host.Host) (string, error) {
	output, err := target.Sudo(ctx, "opsctl", "sh", "-c", installLatestScript)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(output.Stdout)
	if len(fields) == 0 {
		return "", fmt.Errorf("opsctl install returned no version")
	}
	return fields[len(fields)-1], nil
}

// Configure sets the ten keys devctl owns, preserving every other host key.
func Configure(ctx context.Context, target host.Host, config Config) error {
	values := []struct {
		key   string
		value string
	}{
		{"host.name", config.Domain},
		{"dns.provider", "route53"},
		{"dns.zones", config.ZoneName + ":" + config.ZoneID},
		{"aws.region", config.Region},
		{"backup.s3_uri", "s3://" + config.BackupBucket + "/" + config.Domain + "/"},
		{"acme.email", config.ACMEEmail},
		{"backup.host_files_seconds", strconv.Itoa(config.BackupHostFilesSeconds)},
		{"backup.service_files_seconds", strconv.Itoa(config.BackupServiceFilesSeconds)},
		{"backup.service_db_seconds", strconv.Itoa(config.BackupServiceDBSeconds)},
		{"backup.service_wal_seconds", strconv.Itoa(config.BackupServiceWALSeconds)},
	}
	for _, item := range values {
		if _, err := target.Sudo(ctx, "opsctl", "opsctl", "config", "set", item.key, item.value); err != nil {
			return err
		}
	}
	return nil
}

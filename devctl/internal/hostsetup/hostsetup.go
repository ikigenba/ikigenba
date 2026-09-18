package hostsetup

import (
	"context"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
)

// InstallLatest discovers and installs the newest published opsctl release.
func InstallLatest(ctx context.Context, target host.Host) (string, error) {
	release, err := Latest(ctx, target.Deps)
	if err != nil {
		return "", err
	}
	if _, err := target.Run(ctx, "opsctl", "curl", "-fsSL", "-o", InstallerPath, release.InstallerURL); err != nil {
		return "", err
	}
	if _, err := target.Sudo(ctx, "opsctl", "bash", InstallerPath, release.Version); err != nil {
		return "", err
	}
	return release.Version, nil
}

// Upgrade installs the requested opsctl release using the saved installer.
func Upgrade(ctx context.Context, target host.Host, version string) error {
	_, err := target.Sudo(ctx, "opsctl", "bash", SavedInstaller, version)
	return err
}

// Version returns the version text reported by the installed opsctl binary.
func Version(ctx context.Context, target host.Host) (string, error) {
	output, err := target.Sudo(ctx, "opsctl", "opsctl", "version")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(output.Stdout, "\r\n"), nil
}

// Configure sets the host keys devctl owns, preserving every other host key.
func Configure(
	ctx context.Context,
	target host.Host,
	props account.Properties,
	zone cloud.Zone,
	domain string,
	email *string,
) (int, error) {
	type setting struct {
		key   string
		value string
	}
	values := []setting{
		{"host.name", domain},
		{"dns.provider", DNSProvider},
		{"dns.zones", zone.Name + ":" + zone.ID},
		{"aws.region", props.Region},
		{"backup.s3_uri", "s3://" + props.BackupBucket + "/" + domain + "/"},
		{"backup.host_files_seconds", strconv.Itoa(props.BackupHostFilesSeconds)},
		{"backup.service_files_seconds", strconv.Itoa(props.BackupServiceFilesSeconds)},
		{"backup.service_db_seconds", strconv.Itoa(props.BackupServiceDBSeconds)},
		{"backup.service_wal_seconds", strconv.Itoa(props.BackupServiceWALSeconds)},
	}
	if email != nil {
		values = append(values, setting{"acme.email", *email})
	}
	for i, item := range values {
		if _, err := target.Sudo(ctx, "opsctl", "opsctl", "config", "set", item.key+"="+item.value); err != nil {
			return i, err
		}
	}
	return len(values), nil
}

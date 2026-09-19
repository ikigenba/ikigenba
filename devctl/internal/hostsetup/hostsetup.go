package hostsetup

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/spaceref"
)

// Configuration keys name the values in the opsctl configuration store.
const (
	KeyHostName                  = "host.name"
	KeyHostApex                  = "host.apex"
	KeyACMEEmail                 = "acme.email"
	KeyAWSRegion                 = "aws.region"
	KeyBackupS3URI               = "backup.s3_uri"
	KeyBackupServiceFilesSeconds = "backup.service_files_seconds"
	KeyBackupHostFilesSeconds    = "backup.host_files_seconds"
	KeyBackupServiceDBSeconds    = "backup.service_db_seconds"
	KeyBackupServiceWALSeconds   = "backup.service_wal_seconds"
	KeyDNSProvider               = "dns.provider"
	KeyDNSZones                  = "dns.zones"
)

// DefaultHostFilesSeconds and the other defaults apply to newly configured hosts.
const (
	DefaultHostFilesSeconds    = 86400
	DefaultServiceFilesSeconds = 86400
	DefaultServiceDBSeconds    = 86400
	DefaultServiceWALSeconds   = 300
)

// BackupPeriods contains the four opsctl backup intervals.
type BackupPeriods struct {
	HostFilesSeconds    int
	ServiceFilesSeconds int
	ServiceDBSeconds    int
	ServiceWALSeconds   int
}

// DefaultBackupPeriods returns the backup intervals used for new spaces.
func DefaultBackupPeriods() BackupPeriods {
	return BackupPeriods{
		HostFilesSeconds:    DefaultHostFilesSeconds,
		ServiceFilesSeconds: DefaultServiceFilesSeconds,
		ServiceDBSeconds:    DefaultServiceDBSeconds,
		ServiceWALSeconds:   DefaultServiceWALSeconds,
	}
}

// Config contains the plain values Configure writes to a host.
type Config struct {
	Root    string
	Region  string
	ZoneID  string
	Space   spaceref.Space
	Email   string
	Periods *BackupPeriods
}

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
	step string,
	cfg Config,
) (int, error) {
	type setting struct {
		key   string
		value string
	}
	values := []setting{
		{KeyHostName, cfg.Space.Domain},
		{KeyDNSProvider, DNSProvider},
		{KeyDNSZones, cfg.Root + ":" + cfg.ZoneID},
		{KeyAWSRegion, cfg.Region},
		{KeyBackupS3URI, "s3://" + cfg.Root + "/" + cfg.Space.Label + "/"},
	}
	if cfg.Periods != nil {
		values = append(values,
			setting{KeyBackupHostFilesSeconds, strconv.Itoa(cfg.Periods.HostFilesSeconds)},
			setting{KeyBackupServiceFilesSeconds, strconv.Itoa(cfg.Periods.ServiceFilesSeconds)},
			setting{KeyBackupServiceDBSeconds, strconv.Itoa(cfg.Periods.ServiceDBSeconds)},
			setting{KeyBackupServiceWALSeconds, strconv.Itoa(cfg.Periods.ServiceWALSeconds)},
		)
	}
	if cfg.Email != "" {
		values = append(values, setting{KeyACMEEmail, cfg.Email})
	}
	for i, item := range values {
		if err := SetKey(ctx, target, step, item.key, item.value); err != nil {
			return i, err
		}
	}
	return len(values), nil
}

// SetKey sets one opsctl configuration key.
func SetKey(ctx context.Context, target host.Host, step, key, value string) error {
	_, err := target.Sudo(ctx, step, "opsctl", "config", "set", key+"="+value)
	return err
}

// DelKey removes one opsctl configuration key.
func DelKey(ctx context.Context, target host.Host, step, key string) error {
	_, err := target.Sudo(ctx, step, "opsctl", "config", "del", key)
	return err
}

// GetKey returns one opsctl configuration value and whether it is set.
func GetKey(ctx context.Context, target host.Host, step, key string) (string, bool, error) {
	output, err := target.Sudo(ctx, step, "opsctl", "config", "get", key)
	if err == nil {
		return strings.TrimRight(output.Stdout, "\r\n"), true, nil
	}
	var commandErr *host.CommandError
	if errors.As(err, &commandErr) && commandErr.Status == 1 {
		return "", false, nil
	}
	return "", false, err
}

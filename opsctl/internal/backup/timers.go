package backup

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const systemdUnitDirectory = "etc/systemd/system"

type fileBackupTimer struct {
	name        string
	description string
	command     string
	period      string
}

type generatedUnit struct {
	name     string
	contents string
}

type timerSetup struct {
	backups      []fileBackupTimer
	packageTimer bool
}

// SetupTimers publishes and reconciles the file-backup and certificate-renewal
// systemd units derived from host configuration.
func SetupTimers(ctx context.Context, env host.Env, store config.Store) error {
	setup, err := prepareTimerSetup(ctx, env, store)
	if err != nil {
		return err
	}
	if err := publishTimerUnits(ctx, env.Root, timerUnits(setup.backups)); err != nil {
		return err
	}
	return reconcileTimerUnits(ctx, env, setup)
}

func prepareTimerSetup(ctx context.Context, env host.Env, store config.Store) (timerSetup, error) {
	if err := timerContextError(ctx); err != nil {
		return timerSetup{}, err
	}
	hostPeriod, err := configuredTimerPeriod(store, "backup.host_files_seconds")
	if err != nil {
		return timerSetup{}, err
	}
	servicePeriod, err := configuredTimerPeriod(store, "backup.service_files_seconds")
	if err != nil {
		return timerSetup{}, err
	}
	if err := timerContextError(ctx); err != nil {
		return timerSetup{}, err
	}
	if env.Execute == nil {
		return timerSetup{}, errors.New("setup timers: host execution is not configured")
	}

	packageTimer, err := packageRenewalTimerExists(env.Root)
	if err != nil {
		return timerSetup{}, fmt.Errorf("inspect certbot-renew.timer: %w", err)
	}
	return timerSetup{backups: []fileBackupTimer{
		{
			name:        "ikigenba-backup-host",
			description: "Ikigenba host file backup",
			command:     "/usr/local/bin/opsctl host backup",
			period:      hostPeriod,
		},
		{
			name:        "ikigenba-backup-services",
			description: "Ikigenba service file backup",
			command:     "/usr/local/bin/opsctl backup",
			period:      servicePeriod,
		},
	}, packageTimer: packageTimer}, nil
}

func timerUnits(backups []fileBackupTimer) []generatedUnit {
	units := make([]generatedUnit, 0, 6)
	for _, backup := range backups {
		units = append(units,
			generatedUnit{name: backup.name + ".service", contents: renderOneshotService(backup.description, backup.command)},
			generatedUnit{name: backup.name + ".timer", contents: renderBackupTimer(backup)},
		)
	}
	units = append(units,
		generatedUnit{
			name:     "ikigenba-renew-certificate.service",
			contents: renderOneshotService("Ikigenba certificate renewal", "certbot renew"),
		},
		generatedUnit{
			name:     "ikigenba-renew-certificate.timer",
			contents: renderRenewalTimer(),
		},
	)
	return units
}

func reconcileTimerUnits(ctx context.Context, env host.Env, setup timerSetup) error {
	if err := executeTimerCommand(ctx, env, "reload systemd units", "daemon-reload"); err != nil {
		return err
	}
	for _, backup := range setup.backups {
		unit := backup.name + ".timer"
		if backup.period == "" {
			if err := executeTimerCommand(ctx, env, "disable "+unit, "disable", unit); err != nil {
				return err
			}
			if err := executeTimerCommand(ctx, env, "stop "+unit, "stop", unit); err != nil {
				return err
			}
			continue
		}
		if err := executeTimerCommand(ctx, env, "enable "+unit, "enable", unit); err != nil {
			return err
		}
		if err := executeTimerCommand(ctx, env, "restart "+unit, "restart", unit); err != nil {
			return err
		}
	}
	const renewalTimer = "ikigenba-renew-certificate.timer"
	if err := executeTimerCommand(ctx, env, "enable "+renewalTimer, "enable", renewalTimer); err != nil {
		return err
	}
	if err := executeTimerCommand(ctx, env, "restart "+renewalTimer, "restart", renewalTimer); err != nil {
		return err
	}
	if setup.packageTimer {
		if err := executeTimerCommand(ctx, env, "stop certbot-renew.timer", "stop", "certbot-renew.timer"); err != nil {
			return err
		}
		if err := executeTimerCommand(ctx, env, "mask certbot-renew.timer", "mask", "certbot-renew.timer"); err != nil {
			return err
		}
	}
	return nil
}

func configuredTimerPeriod(store config.Store, key string) (string, error) {
	value, err := store.Get(key)
	if err != nil {
		if errors.Is(err, config.ErrNotSet) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", key, err)
	}
	if value == "" {
		return "", nil
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "", fmt.Errorf("%s: must be decimal whole seconds", key)
		}
	}
	normalized := strings.TrimLeft(value, "0")
	if normalized == "" {
		return "", nil
	}
	return normalized + "s", nil
}

func renderOneshotService(description, command string) string {
	return "[Unit]\nDescription=" + description + "\n\n" +
		"[Service]\nType=oneshot\nUser=root\nExecStart=" + command + "\n"
}

func renderBackupTimer(backup fileBackupTimer) string {
	var unit strings.Builder
	unit.WriteString("[Unit]\nDescription=Schedule ")
	unit.WriteString(backup.description)
	unit.WriteString("\n\n[Timer]\n")
	if backup.period != "" {
		unit.WriteString("OnBootSec=")
		unit.WriteString(backup.period)
		unit.WriteString("\nOnUnitActiveSec=")
		unit.WriteString(backup.period)
		unit.WriteByte('\n')
	}
	unit.WriteString("Unit=")
	unit.WriteString(backup.name)
	unit.WriteString(".service\n\n[Install]\nWantedBy=timers.target\n")
	return unit.String()
}

func renderRenewalTimer() string {
	return "[Unit]\nDescription=Schedule Ikigenba certificate renewal\n\n" +
		"[Timer]\n" +
		"OnCalendar=*-*-* 00,12:00:00\n" +
		"RandomizedDelaySec=1h\n" +
		"Persistent=true\n" +
		"Unit=ikigenba-renew-certificate.service\n\n" +
		"[Install]\nWantedBy=timers.target\n"
}

func packageRenewalTimerExists(root string) (bool, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return false, err
	}
	defer func() { _ = filesystem.Close() }()
	for _, candidate := range []string{
		"lib/systemd/system/certbot-renew.timer",
		"usr/lib/systemd/system/certbot-renew.timer",
	} {
		if _, statErr := filesystem.Lstat(candidate); statErr == nil {
			return true, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return false, statErr
		}
	}
	return false, nil
}

func publishTimerUnits(ctx context.Context, root string, units []generatedUnit) error {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("host root: %w", err)
	}
	defer func() { _ = filesystem.Close() }()
	if err := filesystem.MkdirAll(systemdUnitDirectory, 0o755); err != nil {
		return fmt.Errorf("create /%s: %w", systemdUnitDirectory, err)
	}
	for _, unit := range units {
		if err := timerContextError(ctx); err != nil {
			return err
		}
		destination := path.Join(systemdUnitDirectory, unit.name)
		if err := publishTimerUnit(filesystem, destination, []byte(unit.contents)); err != nil {
			return fmt.Errorf("write /%s: %w", destination, err)
		}
	}
	return timerContextError(ctx)
}

func publishTimerUnit(filesystem *os.Root, destination string, contents []byte) error {
	var temporary string
	var file *os.File
	for range 100 {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return err
		}
		temporary = path.Join(systemdUnitDirectory, fmt.Sprintf(".opsctl-timer-%x", suffix))
		var err error
		file, err = filesystem.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if file == nil {
		return errors.New("create unique unit staging file")
	}
	defer func() { _ = filesystem.Remove(temporary) }()
	if _, err := file.Write(contents); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Chmod(fs.FileMode(0o644)); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	return filesystem.Rename(temporary, destination)
}

func executeTimerCommand(ctx context.Context, env host.Env, label string, args ...string) error {
	if err := timerContextError(ctx); err != nil {
		return err
	}
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: args})
	if err != nil {
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			return err
		}
		return &host.CommandError{Label: label, Result: result, Err: err}
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result, Err: ctx.Err()}
	}
	return timerContextError(ctx)
}

func timerContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("setup timers: %w", err)
	}
	return nil
}

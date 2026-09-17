package backup_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var timerUnitNames = []string{
	"ikigenba-backup-host.service",
	"ikigenba-backup-host.timer",
	"ikigenba-backup-services.service",
	"ikigenba-backup-services.timer",
	"ikigenba-renew-certificate.service",
	"ikigenba-renew-certificate.timer",
}

func TestSetupTimersPublishesRootOneshotServices(t *testing.T) {
	// R-FJVO-5X9J R-FMBG-XGQX R-FPZ6-2RZ0 R-YYIY-T743
	requireSetupTimersAPI(backup.SetupTimers)
	root := t.TempDir()
	store := timerStore(t, root, "11", "22")
	wantUnits := expectedTimerUnits("11s", "22s")
	unitDirectory := filepath.Join(root, systemdUnitTestDirectory)
	if err := os.MkdirAll(unitDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range timerUnitNames {
		if err := os.WriteFile(filepath.Join(unitDirectory, name), []byte("stale generated content\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(root, "opt", "notes", "state", "marker")
	if err := os.MkdirAll(filepath.Dir(marker), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("ordinary service data"), 0o600); err != nil {
		t.Fatal(err)
	}

	publicationObservedAtReload := false
	var commands []host.Command
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		if reflect.DeepEqual(command.Args, []string{"daemon-reload"}) {
			if publicationObservedAtReload {
				t.Fatal("daemon-reload ran more than once")
			}
			assertPublishedTimerUnits(t, root, wantUnits)
			publicationObservedAtReload = true
		} else if !publicationObservedAtReload {
			t.Fatalf("systemd command before daemon-reload = %#v", command)
		}
		return host.Result{}, nil
	}}
	if err := backup.SetupTimers(context.Background(), env, store); err != nil {
		t.Fatal(err)
	}

	if !publicationObservedAtReload {
		t.Fatal("daemon-reload did not observe published units")
	}
	assertPublishedTimerUnits(t, root, wantUnits)
	entries, err := os.ReadDir(unitDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(timerUnitNames) {
		t.Fatalf("generated unit directory has %d entries, want %d: %v", len(entries), len(timerUnitNames), entries)
	}
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %o, want 644", entry.Name(), info.Mode().Perm())
		}
	}
	data, err := readRootedTimerFile(root, "opt/notes/state/marker")
	if err != nil || string(data) != "ordinary service data" {
		t.Fatalf("service data = %q, %v; want unchanged marker", data, err)
	}
	for _, command := range commands {
		if command.Name != "systemctl" {
			t.Errorf("executed non-systemctl command: %#v", command)
		}
		if len(command.Args) > 1 && strings.HasSuffix(command.Args[len(command.Args)-1], ".service") {
			t.Errorf("started or controlled a backup service: %#v", command)
		}
	}
}

func TestSetupTimersSchedulesBackupsAndRenewalIndependently(t *testing.T) {
	// R-FNJD-B8HM R-FOR9-P08B R-FPZ6-2RZ0 R-YYIY-T743
	root := t.TempDir()
	store := timerStore(t, root, "17", "")
	var commands []host.Command
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		return host.Result{}, nil
	}}

	if err := backup.SetupTimers(context.Background(), env, store); err != nil {
		t.Fatal(err)
	}
	wantHostTimer := "[Unit]\nDescription=Schedule Ikigenba host file backup\n\n" +
		"[Timer]\nOnBootSec=17s\nOnUnitActiveSec=17s\nUnit=ikigenba-backup-host.service\n\n" +
		"[Install]\nWantedBy=timers.target\n"
	if hostTimer := readTimerUnit(t, root, "ikigenba-backup-host.timer"); hostTimer != wantHostTimer {
		t.Errorf("host timer = %q, want %q", hostTimer, wantHostTimer)
	}
	wantServiceTimer := "[Unit]\nDescription=Schedule Ikigenba service file backup\n\n" +
		"[Timer]\nOnBootSec=infinity\nUnit=ikigenba-backup-services.service\n\n[Install]\nWantedBy=timers.target\n"
	if serviceTimer := readTimerUnit(t, root, "ikigenba-backup-services.timer"); serviceTimer != wantServiceTimer {
		t.Errorf("service timer = %q, want %q", serviceTimer, wantServiceTimer)
	}
	wantRenewalTimer := "[Unit]\nDescription=Schedule Ikigenba certificate renewal\n\n" +
		"[Timer]\nOnCalendar=*-*-* 00,12:00:00\nRandomizedDelaySec=1h\nPersistent=true\n" +
		"Unit=ikigenba-renew-certificate.service\n\n[Install]\nWantedBy=timers.target\n"
	if renewalTimer := readTimerUnit(t, root, "ikigenba-renew-certificate.timer"); renewalTimer != wantRenewalTimer {
		t.Errorf("renewal timer = %q, want %q", renewalTimer, wantRenewalTimer)
	}
	want := []host.Command{
		{Name: "systemctl", Args: []string{"daemon-reload"}},
		{Name: "systemctl", Args: []string{"enable", "ikigenba-backup-host.timer"}},
		{Name: "systemctl", Args: []string{"restart", "ikigenba-backup-host.timer"}},
		{Name: "systemctl", Args: []string{"disable", "ikigenba-backup-services.timer"}},
		{Name: "systemctl", Args: []string{"stop", "ikigenba-backup-services.timer"}},
		{Name: "systemctl", Args: []string{"enable", "ikigenba-renew-certificate.timer"}},
		{Name: "systemctl", Args: []string{"restart", "ikigenba-renew-certificate.timer"}},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestSetupTimersLeavesAbsentEmptyAndZeroPeriodsDisabled(t *testing.T) {
	// R-FNJD-B8HM
	for _, test := range []struct {
		name  string
		value *string
	}{
		{name: "absent"},
		{name: "empty", value: stringPointer("")},
		{name: "zero", value: stringPointer("0")},
		{name: "leading zeroes", value: stringPointer("000")},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := config.Store{Root: root}
			if test.value != nil {
				if err := store.Set("backup.host_files_seconds", *test.value); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Set("backup.service_files_seconds", "8"); err != nil {
				t.Fatal(err)
			}
			validatedAtReload := false
			var commands []host.Command
			env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				commands = append(commands, command)
				if reflect.DeepEqual(command.Args, []string{"daemon-reload"}) {
					assertValidGeneratedTimerUnit(t, readTimerUnit(t, root, "ikigenba-backup-host.timer"))
					validatedAtReload = true
				}
				return host.Result{}, nil
			}}
			if err := backup.SetupTimers(context.Background(), env, store); err != nil {
				t.Fatal(err)
			}
			wantTimer := "[Unit]\nDescription=Schedule Ikigenba host file backup\n\n" +
				"[Timer]\nOnBootSec=infinity\nUnit=ikigenba-backup-host.service\n\n[Install]\nWantedBy=timers.target\n"
			if contents := readTimerUnit(t, root, "ikigenba-backup-host.timer"); contents != wantTimer {
				t.Fatalf("disabled timer = %q, want %q", contents, wantTimer)
			}
			if !validatedAtReload {
				t.Fatal("daemon-reload did not validate disabled timer")
			}
			wantPrefix := []host.Command{
				{Name: "systemctl", Args: []string{"daemon-reload"}},
				{Name: "systemctl", Args: []string{"disable", "ikigenba-backup-host.timer"}},
				{Name: "systemctl", Args: []string{"stop", "ikigenba-backup-host.timer"}},
			}
			if len(commands) < len(wantPrefix) || !reflect.DeepEqual(commands[:len(wantPrefix)], wantPrefix) {
				t.Fatalf("commands = %#v, want prefix %#v", commands, wantPrefix)
			}
		})
	}
}

func TestSetupTimersRejectsInvalidPeriodsBeforeChanges(t *testing.T) {
	// R-FNJD-B8HM
	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "backup.host_files_seconds", value: "-1"},
		{key: "backup.host_files_seconds", value: "+1"},
		{key: "backup.service_files_seconds", value: "1.5"},
		{key: "backup.service_files_seconds", value: "ten"},
	} {
		t.Run(test.key+"="+test.value, func(t *testing.T) {
			root := t.TempDir()
			store := timerStore(t, root, "5", "6")
			if err := store.Set(test.key, test.value); err != nil {
				t.Fatal(err)
			}
			unitDirectory := filepath.Join(root, systemdUnitTestDirectory)
			if err := os.MkdirAll(unitDirectory, 0o750); err != nil {
				t.Fatal(err)
			}
			oldPath := filepath.Join(unitDirectory, "ikigenba-backup-host.service")
			if err := os.WriteFile(oldPath, []byte("old unit\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("systemctl ran for an invalid period")
				return host.Result{}, nil
			}}
			err := backup.SetupTimers(context.Background(), env, store)
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("SetupTimers() error = %v, want error naming %s", err, test.key)
			}
			contents := readTimerUnit(t, root, "ikigenba-backup-host.service")
			if contents != "old unit\n" {
				t.Fatalf("old unit = %q; want unchanged", contents)
			}
			entries, readDirErr := os.ReadDir(unitDirectory)
			if readDirErr != nil || len(entries) != 1 {
				t.Fatalf("unit directory = %v, %v; want only old unit", entries, readDirErr)
			}
		})
	}
}

func TestSetupTimersStopsAfterMidPublicationFailure(t *testing.T) {
	// R-FPZ6-2RZ0 R-FSEY-UBGE
	root := t.TempDir()
	store := timerStore(t, root, "11", "22")
	unitDirectory := filepath.Join(root, systemdUnitTestDirectory)
	if err := os.MkdirAll(unitDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	const stale = "stale generated content\n"
	const failureIndex = 2
	for index, name := range timerUnitNames {
		destination := filepath.Join(unitDirectory, name)
		if index == failureIndex {
			if err := os.Mkdir(destination, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, "blocks-replacement"), []byte("marker\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(destination, []byte(stale), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var commands []host.Command
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		return host.Result{}, nil
	}}
	err := backup.SetupTimers(context.Background(), env, store)
	if err == nil || !strings.Contains(err.Error(), timerUnitNames[failureIndex]) {
		t.Fatalf("SetupTimers() error = %v, want publication failure naming %s", err, timerUnitNames[failureIndex])
	}
	if len(commands) != 0 {
		t.Fatalf("systemd commands after publication failure = %#v, want none", commands)
	}

	wantUnits := expectedTimerUnits("11s", "22s")
	for _, name := range timerUnitNames[:failureIndex] {
		if got := readTimerUnit(t, root, name); got != wantUnits[name] {
			t.Errorf("earlier published %s = %q, want %q", name, got, wantUnits[name])
		}
	}
	marker, markerErr := readRootedTimerFile(root, filepath.ToSlash(filepath.Join(
		systemdUnitTestDirectory, timerUnitNames[failureIndex], "blocks-replacement",
	)))
	if markerErr != nil || string(marker) != "marker\n" {
		t.Fatalf("failed destination marker = %q, %v; want unchanged", marker, markerErr)
	}
	for _, name := range timerUnitNames[failureIndex+1:] {
		if got := readTimerUnit(t, root, name); got != stale {
			t.Errorf("later unpublished %s = %q, want stale content", name, got)
		}
	}
	entries, readDirErr := os.ReadDir(unitDirectory)
	if readDirErr != nil {
		t.Fatal(readDirErr)
	}
	if len(entries) != len(timerUnitNames) {
		t.Fatalf("unit directory after failure has %d entries, want %d: %v", len(entries), len(timerUnitNames), entries)
	}
}

func TestSetupTimersMasksPackageRenewalTimerOnlyWhenPresent(t *testing.T) {
	// R-FR72-GJPP R-YYIY-T743
	for _, test := range []struct {
		name       string
		vendorPath string
		wantTail   []host.Command
	}{
		{
			name:       "lib package timer",
			vendorPath: "lib/systemd/system/certbot-renew.timer",
			wantTail: []host.Command{
				{Name: "systemctl", Args: []string{"stop", "certbot-renew.timer"}},
				{Name: "systemctl", Args: []string{"mask", "certbot-renew.timer"}},
			},
		},
		{
			name:       "usr lib package timer",
			vendorPath: "usr/lib/systemd/system/certbot-renew.timer",
			wantTail: []host.Command{
				{Name: "systemctl", Args: []string{"stop", "certbot-renew.timer"}},
				{Name: "systemctl", Args: []string{"mask", "certbot-renew.timer"}},
			},
		},
		{name: "absent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := timerStore(t, root, "", "")
			if test.vendorPath != "" {
				vendor := filepath.Join(root, filepath.FromSlash(test.vendorPath))
				if err := os.MkdirAll(filepath.Dir(vendor), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(vendor, []byte("package timer\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var commands []host.Command
			env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				commands = append(commands, command)
				return host.Result{}, nil
			}}
			if err := backup.SetupTimers(context.Background(), env, store); err != nil {
				t.Fatal(err)
			}
			if len(test.wantTail) == 0 {
				for _, command := range commands {
					if len(command.Args) > 1 && command.Args[len(command.Args)-1] == "certbot-renew.timer" {
						t.Fatalf("absent package timer was controlled: %#v", command)
					}
				}
				return
			}
			if len(commands) < len(test.wantTail) || !reflect.DeepEqual(commands[len(commands)-len(test.wantTail):], test.wantTail) {
				t.Fatalf("commands = %#v, want tail %#v", commands, test.wantTail)
			}
		})
	}
}

func TestSetupTimersReturnsInputFilesystemAndContextFailures(t *testing.T) {
	// R-FSEY-UBGE
	t.Run("configuration read", func(t *testing.T) {
		root := t.TempDir()
		store := timerStore(t, root, "4", "5")
		configuration := filepath.Join(root, "etc", "ikigenba", "config.json")
		if err := os.WriteFile(configuration, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := failingUnexpectedTimerEnv(t, root)
		err := backup.SetupTimers(context.Background(), env, store)
		if err == nil || !strings.Contains(err.Error(), "read backup.host_files_seconds") {
			t.Fatalf("SetupTimers() error = %v", err)
		}
		assertNoGeneratedTimerUnits(t, root)
	})

	t.Run("filesystem", func(t *testing.T) {
		root := t.TempDir()
		store := timerStore(t, root, "4", "5")
		if err := os.WriteFile(filepath.Join(root, "etc", "systemd"), []byte("blocks unit directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := failingUnexpectedTimerEnv(t, root)
		err := backup.SetupTimers(context.Background(), env, store)
		if err == nil || !strings.Contains(err.Error(), "create /etc/systemd/system") {
			t.Fatalf("SetupTimers() error = %v", err)
		}
	})

	t.Run("cancelled before setup", func(t *testing.T) {
		root := t.TempDir()
		store := timerStore(t, root, "4", "5")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := backup.SetupTimers(ctx, failingUnexpectedTimerEnv(t, root), store)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SetupTimers() error = %v, want context.Canceled", err)
		}
		assertNoGeneratedTimerUnits(t, root)
	})

	t.Run("cancelled after publication", func(t *testing.T) {
		root := t.TempDir()
		store := timerStore(t, root, "4", "5")
		ctx, cancel := context.WithCancel(context.Background())
		var commands []host.Command
		env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
			commands = append(commands, command)
			cancel()
			return host.Result{}, nil
		}}
		err := backup.SetupTimers(ctx, env, store)
		if !errors.Is(err, context.Canceled) || len(commands) != 1 || !reflect.DeepEqual(commands[0].Args, []string{"daemon-reload"}) {
			t.Fatalf("SetupTimers() = %v, commands %#v; want cancellation after daemon-reload", err, commands)
		}
		for _, name := range timerUnitNames {
			_ = readTimerUnit(t, root, name)
		}
	})
}

func TestSetupTimersStopsAfterUnitOperationFailuresWithPackageTimer(t *testing.T) {
	// R-FSEY-UBGE
	for _, state := range []struct {
		name     string
		period   string
		commands []host.Command
	}{
		{name: "active backups", period: "9", commands: expectedTimerCommands(true, true)},
		{name: "disabled backups", period: "", commands: expectedTimerCommands(false, true)},
	} {
		for failureIndex := range state.commands {
			failureIndex := failureIndex
			t.Run(fmt.Sprintf("%s/%02d-%s", state.name, failureIndex, state.commands[failureIndex].Args[0]), func(t *testing.T) {
				root := t.TempDir()
				store := timerStore(t, root, state.period, state.period)
				vendor := filepath.Join(root, "lib", "systemd", "system", "certbot-renew.timer")
				if err := os.MkdirAll(filepath.Dir(vendor), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(vendor, []byte("package timer\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				transportFailure := errors.New("systemd transport failed")
				failureCommand := state.commands[failureIndex]
				var commands []host.Command
				env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
					commands = append(commands, command)
					if !reflect.DeepEqual(command, failureCommand) {
						return host.Result{}, nil
					}
					result := host.Result{ExitCode: 23, Stderr: []byte("unit operation failed")}
					if failureIndex%2 == 0 {
						return result, nil
					}
					return result, transportFailure
				}}

				err := backup.SetupTimers(context.Background(), env, store)
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) {
					t.Fatalf("error = %T %v, want *host.CommandError", err, err)
				}
				if failureIndex%2 == 1 && !errors.Is(err, transportFailure) {
					t.Fatalf("error %v does not wrap the expected sentinel", err)
				}
				wantCommands := state.commands[:failureIndex+1]
				if !reflect.DeepEqual(commands, wantCommands) {
					t.Fatalf("commands after failure = %#v, want %#v", commands, wantCommands)
				}
				for _, name := range timerUnitNames {
					_ = readTimerUnit(t, root, name)
				}
			})
		}
	}
}

func expectedTimerCommands(active, packageTimer bool) []host.Command {
	commands := []host.Command{{Name: "systemctl", Args: []string{"daemon-reload"}}}
	for _, unit := range []string{"ikigenba-backup-host.timer", "ikigenba-backup-services.timer"} {
		if active {
			commands = append(commands,
				host.Command{Name: "systemctl", Args: []string{"enable", unit}},
				host.Command{Name: "systemctl", Args: []string{"restart", unit}},
			)
		} else {
			commands = append(commands,
				host.Command{Name: "systemctl", Args: []string{"disable", unit}},
				host.Command{Name: "systemctl", Args: []string{"stop", unit}},
			)
		}
	}
	commands = append(commands,
		host.Command{Name: "systemctl", Args: []string{"enable", "ikigenba-renew-certificate.timer"}},
		host.Command{Name: "systemctl", Args: []string{"restart", "ikigenba-renew-certificate.timer"}},
	)
	if packageTimer {
		commands = append(commands,
			host.Command{Name: "systemctl", Args: []string{"stop", "certbot-renew.timer"}},
			host.Command{Name: "systemctl", Args: []string{"mask", "certbot-renew.timer"}},
		)
	}
	return commands
}

const systemdUnitTestDirectory = "etc/systemd/system"

func expectedTimerUnits(hostPeriod, servicePeriod string) map[string]string {
	backupTimer := func(description, service, period string) string {
		trigger := "OnBootSec=infinity\n"
		if period != "" {
			trigger = "OnBootSec=" + period + "\nOnUnitActiveSec=" + period + "\n"
		}
		return "[Unit]\nDescription=Schedule " + description + "\n\n[Timer]\n" + trigger +
			"Unit=" + service + "\n\n[Install]\nWantedBy=timers.target\n"
	}
	return map[string]string{
		"ikigenba-backup-host.service": "[Unit]\nDescription=Ikigenba host file backup\n\n" +
			"[Service]\nType=oneshot\nUser=root\nExecStart=/usr/local/bin/opsctl host backup\n",
		"ikigenba-backup-host.timer": backupTimer(
			"Ikigenba host file backup", "ikigenba-backup-host.service", hostPeriod,
		),
		"ikigenba-backup-services.service": "[Unit]\nDescription=Ikigenba service file backup\n\n" +
			"[Service]\nType=oneshot\nUser=root\nExecStart=/usr/local/bin/opsctl backup\n",
		"ikigenba-backup-services.timer": backupTimer(
			"Ikigenba service file backup", "ikigenba-backup-services.service", servicePeriod,
		),
		"ikigenba-renew-certificate.service": "[Unit]\nDescription=Ikigenba certificate renewal\n\n" +
			"[Service]\nType=oneshot\nUser=root\nExecStart=certbot renew\n",
		"ikigenba-renew-certificate.timer": "[Unit]\nDescription=Schedule Ikigenba certificate renewal\n\n" +
			"[Timer]\nOnCalendar=*-*-* 00,12:00:00\nRandomizedDelaySec=1h\nPersistent=true\n" +
			"Unit=ikigenba-renew-certificate.service\n\n[Install]\nWantedBy=timers.target\n",
	}
}

func assertPublishedTimerUnits(t *testing.T, root string, wantUnits map[string]string) {
	t.Helper()
	for _, name := range timerUnitNames {
		want, ok := wantUnits[name]
		if !ok {
			t.Fatalf("missing expected contents for %s", name)
		}
		got := readTimerUnit(t, root, name)
		if got != want {
			t.Errorf("%s at daemon-reload = %q, want %q", name, got, want)
		}
		if strings.HasSuffix(name, ".timer") {
			assertValidGeneratedTimerUnit(t, got)
		}
	}
}

func assertValidGeneratedTimerUnit(t *testing.T, contents string) {
	t.Helper()
	insideTimerSection := false
	hasTrigger := false
	hasTarget := false
	for _, line := range strings.Split(contents, "\n") {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			insideTimerSection = line == "[Timer]"
			continue
		}
		if !insideTimerSection {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || value == "" {
			continue
		}
		switch key {
		case "OnActiveSec", "OnBootSec", "OnStartupSec", "OnUnitActiveSec", "OnUnitInactiveSec", "OnCalendar":
			hasTrigger = true
		case "Unit":
			hasTarget = true
		}
	}
	if !hasTrigger || !hasTarget {
		t.Fatalf("invalid timer unit: trigger=%t target=%t contents=%q", hasTrigger, hasTarget, contents)
	}
}

func timerStore(t *testing.T, root, hostPeriod, servicePeriod string) config.Store {
	t.Helper()
	store := config.Store{Root: root}
	if err := store.Set("backup.host_files_seconds", hostPeriod); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("backup.service_files_seconds", servicePeriod); err != nil {
		t.Fatal(err)
	}
	return store
}

func readTimerUnit(t *testing.T, root, name string) string {
	t.Helper()
	data, err := readRootedTimerFile(root, filepath.ToSlash(filepath.Join(systemdUnitTestDirectory, name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readRootedTimerFile(root, name string) ([]byte, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()
	return filesystem.ReadFile(name)
}

func assertNoGeneratedTimerUnits(t *testing.T, root string) {
	t.Helper()
	for _, name := range timerUnitNames {
		_, err := os.Stat(filepath.Join(root, systemdUnitTestDirectory, name))
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s exists or stat failed unexpectedly: %v", name, err)
		}
	}
}

func failingUnexpectedTimerEnv(t *testing.T, root string) host.Env {
	t.Helper()
	return host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("systemctl ran after an earlier setup failure")
		return host.Result{}, nil
	}}
}

func stringPointer(value string) *string { return &value }

func requireSetupTimersAPI(func(context.Context, host.Env, config.Store) error) {}

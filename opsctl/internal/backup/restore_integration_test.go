package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// R-1EYH-INPP
var _ func(context.Context, host.Env, cloud.Env, config.Store, string, *time.Time, backup.NginxRegenerator) (backup.RestoreReport, error) = backup.Restore

func TestRestoreAtControlsArchiveAndDatabaseTogether(t *testing.T) {
	// R-1EYH-INPP R-RX15-3IAF
	const (
		older  = "2026-09-16T10:00:00Z"
		cutoff = "2026-09-16T10:30:00Z"
		newer  = "2026-09-16T11:00:00Z"
	)
	for _, test := range []struct {
		name        string
		at          *time.Time
		wantArchive string
		wantValue   string
		wantDB      string
		wantArg     string
	}{
		{name: "instant", at: restoreIntegrationTime(t, cutoff), wantArchive: older, wantValue: "older files", wantDB: "/opt/notes/state/app.db, at " + cutoff, wantArg: " -timestamp " + cutoff},
		{name: "newest", wantArchive: newer, wantValue: "newer files", wantDB: "/opt/notes/state/app.db, newest 2026-09-16T12:00:00Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			olderBody := restoreIntegrationDatabaseArchive(t, "older files")
			newerBody := restoreIntegrationDatabaseArchive(t, "newer files")
			olderURI := "s3://bucket/host/notes/" + older + ".tar.zst"
			newerURI := "s3://bucket/host/notes/" + newer + ".tar.zst"
			client := &restoreCloud{
				objects: []cloud.Object{{URI: olderURI}, {URI: newerURI}},
				bodies:  map[string][]byte{olderURI: olderBody, newerURI: newerBody},
			}
			executor := &restoreIntegrationExecutor{t: t, root: root, ltx: `[{"timestamp":"2026-09-16T10:00:00Z"},{"timestamp":"2026-09-16T12:00:00Z"}]`}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", test.at, func(context.Context) error {
				executor.events = append(executor.events, "nginx")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			selectedBody := newerBody
			if test.wantArchive == older {
				selectedBody = olderBody
			}
			wantReport := []backup.RestoreStep{
				{Name: "source", Detail: fmt.Sprintf("notes/%s.tar.zst, %.1f MiB", test.wantArchive, float64(len(selectedBody))/1048576)},
				{Name: "stop", Detail: "litestream.service, no ikigenba-notes.socket"},
				{Name: "files", Detail: "/opt/notes/etc, /opt/notes/state, 2 files"},
				{Name: "db", Detail: test.wantDB},
				{Name: "litestream", Detail: "state/app.db"},
				{Name: "start", Detail: "litestream.service"},
			}
			if !reflect.DeepEqual(report.Steps, wantReport) {
				t.Fatalf("report = %+v, want %+v", report.Steps, wantReport)
			}
			if got := string(readHostRestoreFile(t, root, "opt/notes/state/value")); got != test.wantValue {
				t.Fatalf("restored archive value = %q, want %q", got, test.wantValue)
			}
			restoreCommand := "litestream restore -o " + filepath.Join(root, "opt/notes/state/app.db") + test.wantArg + " s3://bucket/host/notes/"
			if !containsString(executor.events, restoreCommand) {
				t.Fatalf("events = %v, want %q", executor.events, restoreCommand)
			}
		})
	}
}

func TestRestoreValidArchiveIgnoresInstalledDatabaseAndOtherServices(t *testing.T) {
	// R-FWGV-MCZT R-FXOS-04QI R-XGQO-ZO65 R-1JU3-1QOH
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/notes/etc/manifest.toml", "[database]\nengine = \"sqlite\"\npath = \"state/installed.db\"\n", 0o600)
	writeFile(t, root, "opt/notes/bin/app", "keep binary", 0o700)
	writeFile(t, root, "opt/notes/share/asset", "keep asset", 0o600)
	writeFile(t, root, "opt/other/etc/manifest.toml", "app = \"other\"\n", 0o600)
	writeFile(t, root, "opt/other/state/private", "unrelated secret", 0o600)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc", typeflag: tar.TypeDir, mode: 0o750},
		restoreMember{name: "etc/env", data: []byte("TOKEN=restored\n"), mode: 0o640},
		restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0o710},
		restoreMember{name: "state/value", data: []byte("restored"), mode: 0o620},
		restoreMember{name: "state/current", typeflag: tar.TypeSymlink, linkname: "value"},
	)
	client := restoreClientFor(t, body)
	executor := &restoreIntegrationExecutor{t: t, root: root, installed: true}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error {
		executor.events = append(executor.events, "nginx")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantReport := []backup.RestoreStep{
		{Name: "source", Detail: fmt.Sprintf("notes/2026-09-16T00:00:00Z.tar.zst, %.1f MiB", float64(len(body))/1048576)},
		{Name: "stop", Detail: "ikigenba-notes.socket, ikigenba-notes.service already inactive"},
		{Name: "files", Detail: "/opt/notes/etc, /opt/notes/state, 3 files"},
		{Name: "start", Detail: "ikigenba-notes.socket, ikigenba-notes.service left inactive"},
	}
	if !reflect.DeepEqual(report.Steps, wantReport) {
		t.Fatalf("report = %+v, want %+v", report.Steps, wantReport)
	}
	wantEvents := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		"systemctl stop ikigenba-notes.socket", "systemctl stop ikigenba-notes.service",
		"nginx",
	}
	if !reflect.DeepEqual(executor.events, wantEvents) {
		t.Fatalf("events = %v, want %v", executor.events, wantEvents)
	}
	for name, want := range map[string]string{
		"opt/notes/etc/env": "TOKEN=restored\n", "opt/notes/state/value": "restored",
		"opt/notes/bin/app": "keep binary", "opt/notes/share/asset": "keep asset",
		"opt/other/etc/manifest.toml": "app = \"other\"\n", "opt/other/state/private": "unrelated secret",
	} {
		if got := string(readHostRestoreFile(t, root, name)); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "opt/notes/etc")); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("etc directory = %v, %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(root, "opt/notes/state/value")); err != nil || info.Mode().Perm() != 0o620 {
		t.Fatalf("state value = %v, %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(root, "opt/notes/state/current")); err != nil || target != "value" {
		t.Fatalf("state symlink = %q, %v", target, err)
	}
	if client.puts != 0 {
		t.Fatalf("restore wrote %d cloud objects", client.puts)
	}
}

func TestRestoreDatabaseRetryUsesDurableActivationIntent(t *testing.T) {
	// R-D4R8-7TVQ R-XFIS-LWFG R-G7FZ-2AO2 R-RX15-3IAF
	root := t.TempDir()
	store := configuredFileStore(t, root)
	body := restoreIntegrationDatabaseArchive(t, "published before database failure")
	client := restoreClientFor(t, body)
	executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, active: true, ltx: `[]`}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error {
		executor.events = append(executor.events, "unexpected nginx")
		return nil
	})
	var failure *backup.RestoreError
	if err == nil || !errors.As(err, &failure) || failure.Stage != "litestream restore" || !reflect.DeepEqual(failure.Stopped, []string{"ikigenba-notes.socket", "ikigenba-notes.service", "litestream.service"}) {
		t.Fatalf("first Restore() error = %#v", err)
	}
	if len(report.Steps) != 4 || report.Steps[3].Name != "db" || report.Steps[3].Detail != "" || report.Steps[3].Err == nil || containsEventFragment(executor.events, "systemctl start") || containsString(executor.events, "unexpected nginx") {
		t.Fatalf("first report/events = %+v / %v", report.Steps, executor.events)
	}
	if got := string(readHostRestoreFile(t, root, "opt/notes/state/value")); got != "published before database failure" {
		t.Fatalf("failed restore rolled files back: %q", got)
	}
	assertRestoreMarker(t, root)

	if changed, regenerateErr := backup.Regenerate(context.Background(), host.Env{Root: root}, store); regenerateErr != nil || !changed {
		t.Fatalf("seed Regenerate() = %v, %v", changed, regenerateErr)
	}
	executor.events = nil
	executor.ltx = `[{"timestamp":"2026-09-16T11:00:00Z"}]`
	report, err = backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error {
		executor.events = append(executor.events, "nginx")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"source", "stop", "files", "db", "litestream", "start"}
	for index, name := range wantNames {
		if report.Steps[index].Name != name || report.Steps[index].Err != nil {
			t.Fatalf("retry report = %+v", report.Steps)
		}
	}
	if report.Steps[1].Detail != "litestream.service; ikigenba-notes.socket, ikigenba-notes.service already inactive" || report.Steps[4].Detail != "unchanged" || report.Steps[5].Detail != "litestream.service, ikigenba-notes.socket, ikigenba-notes.service" {
		t.Fatalf("retry report details = %+v", report.Steps)
	}
	wantTail := []string{"nginx", "systemctl start litestream.service", "systemctl start ikigenba-notes.socket", "systemctl start ikigenba-notes.service"}
	if len(executor.events) < len(wantTail) || !reflect.DeepEqual(executor.events[len(executor.events)-len(wantTail):], wantTail) {
		t.Fatalf("retry events = %v, want tail %v", executor.events, wantTail)
	}
	if !executor.active {
		t.Fatal("retry did not restart app")
	}
	if _, statErr := os.Stat(filepath.Join(root, "run/opsctl/restore/notes.active")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("successful retry retained marker: %v", statErr)
	}
}

func TestRestoreNoDatabaseRetryUsesMarkerWithoutLitestream(t *testing.T) {
	// R-D4R8-7TVQ R-XGQO-ZO65
	root := t.TempDir()
	store := configuredFileStore(t, root)
	body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("restored")})
	client := restoreClientFor(t, body)
	executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, active: true}
	cause := errors.New("nginx failed")
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error { return cause })
	var failure *backup.RestoreError
	if !errors.Is(err, cause) || !errors.As(err, &failure) || failure.Stage != "nginx regeneration" || !reflect.DeepEqual(failure.Stopped, []string{"ikigenba-notes.socket", "ikigenba-notes.service"}) || len(report.Steps) != 3 {
		t.Fatalf("first Restore() = %+v, %#v", report.Steps, err)
	}
	assertRestoreMarker(t, root)
	executor.events = nil
	report, err = backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error {
		executor.events = append(executor.events, "nginx")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []backup.RestoreStep{
		{Name: "source", Detail: fmt.Sprintf("notes/2026-09-16T00:00:00Z.tar.zst, %.1f MiB", float64(len(body))/1048576)},
		{Name: "stop", Detail: "ikigenba-notes.socket, ikigenba-notes.service already inactive"},
		{Name: "files", Detail: "/opt/notes/etc, /opt/notes/state, 1 files"},
		{Name: "start", Detail: "ikigenba-notes.socket, ikigenba-notes.service"},
	}
	if !reflect.DeepEqual(report.Steps, want) || !reflect.DeepEqual(executor.events, []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		"systemctl stop ikigenba-notes.socket", "systemctl stop ikigenba-notes.service",
		"nginx",
		"systemctl start ikigenba-notes.socket", "systemctl start ikigenba-notes.service",
	}) {
		t.Fatalf("retry = %+v, events %v", report.Steps, executor.events)
	}
	if _, statErr := os.Stat(filepath.Join(root, "run/opsctl/restore/notes.active")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("successful retry retained marker: %v", statErr)
	}
}

func TestRestoreWithoutDatabaseHonorsEveryUnitState(t *testing.T) {
	// R-XEAW-84OR R-XGQO-ZO65
	for _, test := range []struct {
		name         string
		service      string
		installed    bool
		active       bool
		loadState    string
		wantStop     string
		wantStart    string
		wantCommands []string
	}{
		{
			name: "active app", service: "notes", installed: true, active: true,
			wantStop: "ikigenba-notes.socket, ikigenba-notes.service", wantStart: "ikigenba-notes.socket, ikigenba-notes.service",
			wantCommands: []string{"zstd --quiet --decompress --stdout", "systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket", "systemctl stop ikigenba-notes.socket", "systemctl stop ikigenba-notes.service", "nginx", "systemctl start ikigenba-notes.socket", "systemctl start ikigenba-notes.service"},
		},
		{
			name: "absent app", service: "notes", wantStop: "no ikigenba-notes.socket", wantStart: "no ikigenba-notes.socket",
			wantCommands: []string{"zstd --quiet --decompress --stdout", "systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket", "nginx"},
		},
		{
			name: "masked active socket", service: "notes", loadState: "masked", active: true,
			wantStop: "no ikigenba-notes.socket", wantStart: "no ikigenba-notes.socket",
			wantCommands: []string{"zstd --quiet --decompress --stdout", "systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket", "nginx"},
		},
		{
			name: "data only", service: "backup-host", wantStop: "no app unit", wantStart: "no app unit",
			wantCommands: []string{"zstd --quiet --decompress --stdout", "nginx"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("restored")})
			executor := &restoreIntegrationExecutor{t: t, root: root, installed: test.installed, active: test.active, socketLoadState: test.loadState}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientForService(t, body, test.service).open}, store, test.service, nil, func(context.Context) error {
				executor.events = append(executor.events, "nginx")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Steps) != 4 || report.Steps[1] != (backup.RestoreStep{Name: "stop", Detail: test.wantStop}) || report.Steps[3] != (backup.RestoreStep{Name: "start", Detail: test.wantStart}) || !reflect.DeepEqual(executor.events, test.wantCommands) {
				t.Fatalf("Restore() = %+v, events %v", report.Steps, executor.events)
			}
			if containsEventFragment(executor.events, "litestream") {
				t.Fatalf("database-free restore used Litestream: %v", executor.events)
			}
			if _, statErr := os.Stat(filepath.Join(root, "run/opsctl/restore", test.service+".active")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("successful restore retained marker: %v", statErr)
			}
		})
	}
}

func TestRestoreStartFailuresRetainOnlyUnitsStillStopped(t *testing.T) {
	// R-XFIS-LWFG R-G7FZ-2AO2 R-RX15-3IAF
	for _, test := range []struct {
		name        string
		failCommand string
		wantStopped []string
		forbid      string
	}{
		{name: "litestream start", failCommand: "systemctl start litestream.service", wantStopped: []string{"ikigenba-notes.socket", "ikigenba-notes.service", "litestream.service"}, forbid: "systemctl start ikigenba-notes.socket"},
		{name: "app start after litestream", failCommand: "systemctl start ikigenba-notes.socket", wantStopped: []string{"ikigenba-notes.socket", "ikigenba-notes.service"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			body := restoreIntegrationDatabaseArchive(t, "restored before restart failure")
			executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, active: true, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`, failCommand: test.failCommand}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, func(context.Context) error {
				executor.events = append(executor.events, "nginx")
				return nil
			})
			var failure *backup.RestoreError
			var commandErr *host.CommandError
			if err == nil || !errors.As(err, &failure) || !errors.As(err, &commandErr) || failure.Stage != "start" || !reflect.DeepEqual(failure.Stopped, test.wantStopped) {
				t.Fatalf("Restore() error = %#v", err)
			}
			last := report.Steps[len(report.Steps)-1]
			if len(report.Steps) != 6 || last.Name != "start" || last.Detail != "" || last.Err == nil {
				t.Fatalf("report = %+v", report.Steps)
			}
			if test.forbid != "" && containsString(executor.events, test.forbid) {
				t.Fatalf("later action %q ran: %v", test.forbid, executor.events)
			}
			if got := string(readHostRestoreFile(t, root, "opt/notes/state/value")); got != "restored before restart failure" {
				t.Fatalf("restart failure rolled back files: %q", got)
			}
			assertRestoreMarker(t, root)
		})
	}
}

func TestRestoreCloudAndCancellationFailuresStopTheirStages(t *testing.T) {
	// R-G7FZ-2AO2 R-RX15-3IAF
	t.Run("cloud open is failed source", func(t *testing.T) {
		root := t.TempDir()
		cause := errors.New("cloud credentials unavailable")
		used := false
		report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
			used = true
			return host.Result{}, nil
		}}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return nil, cause }}, configuredFileStore(t, root), "notes", nil, func(context.Context) error {
			used = true
			return nil
		})
		if !errors.Is(err, cause) || len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Detail != "" || report.Steps[0].Err == nil || used {
			t.Fatalf("Restore() = %+v, %#v; later boundary used %v", report.Steps, err, used)
		}
	})

	t.Run("cloud list is failed source", func(t *testing.T) {
		root := t.TempDir()
		cause := errors.New("list unavailable")
		client := &restoreCloud{listErr: cause}
		used := false
		report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
			used = true
			return host.Result{}, nil
		}}, cloud.Env{Open: client.open}, configuredFileStore(t, root), "notes", nil, func(context.Context) error {
			used = true
			return nil
		})
		if !errors.Is(err, cause) || len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Detail != "" || report.Steps[0].Err == nil || used {
			t.Fatalf("Restore() = %+v, %#v; later boundary used %v", report.Steps, err, used)
		}
	})

	t.Run("cancellation during database retains completed effects", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		body := restoreIntegrationDatabaseArchive(t, "published before cancellation")
		executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, active: true, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`}
		ctx, cancel := context.WithCancel(context.Background())
		execute := func(commandContext context.Context, command host.Command) (host.Result, error) {
			if command.Name == "litestream" && len(command.Args) > 0 && command.Args[0] == "ltx" {
				text := strings.Join(append([]string{command.Name}, command.Args...), " ")
				executor.events = append(executor.events, text)
				cancel()
				return host.Result{}, commandContext.Err()
			}
			return executor.execute(commandContext, command)
		}
		report, err := backup.Restore(ctx, host.Env{Root: root, Execute: execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, func(context.Context) error {
			executor.events = append(executor.events, "unexpected nginx")
			return nil
		})
		var failure *backup.RestoreError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &failure) || failure.Stage != "litestream restore" || !reflect.DeepEqual(failure.Stopped, []string{"ikigenba-notes.socket", "ikigenba-notes.service", "litestream.service"}) {
			t.Fatalf("Restore() error = %#v", err)
		}
		if len(report.Steps) != 4 || report.Steps[3].Name != "db" || report.Steps[3].Detail != "" || report.Steps[3].Err == nil || containsEventFragment(executor.events, "systemctl start") || containsString(executor.events, "unexpected nginx") {
			t.Fatalf("report/events = %+v / %v", report.Steps, executor.events)
		}
		if got := string(readHostRestoreFile(t, root, "opt/notes/state/value")); got != "published before cancellation" {
			t.Fatalf("cancellation rolled back files: %q", got)
		}
		assertRestoreMarker(t, root)
	})
}

func TestRestoreActivationMarkerFailuresPreserveIntentWithoutReportRows(t *testing.T) {
	// R-D4R8-7TVQ R-G7FZ-2AO2 R-RX15-3IAF
	body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("restored")})

	t.Run("lookup", func(t *testing.T) {
		root := t.TempDir()
		marker := filepath.Join(root, "run/opsctl/restore/notes.active")
		if err := os.MkdirAll(marker, 0o700); err != nil {
			t.Fatal(err)
		}
		executor := &restoreIntegrationExecutor{t: t, root: root, installed: true}
		report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, configuredFileStore(t, root), "notes", nil, func(context.Context) error {
			executor.events = append(executor.events, "unexpected nginx")
			return nil
		})
		assertActivationMarkerFailure(t, report, err, nil)
		if len(report.Steps) != 1 {
			t.Fatalf("report = %+v", report.Steps)
		}
		if !reflect.DeepEqual(executor.events, []string{
			"zstd --quiet --decompress --stdout",
			"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		}) {
			t.Fatalf("events = %v", executor.events)
		}
		if info, statErr := os.Stat(marker); statErr != nil || !info.IsDir() {
			t.Fatalf("failed lookup changed marker = %v, %v", info, statErr)
		}
	})

	t.Run("publication", func(t *testing.T) {
		root := t.TempDir()
		marker := filepath.Join(root, "run/opsctl/restore/notes.active")
		if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, nil, 0o400); err != nil {
			t.Fatal(err)
		}
		executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, active: true}
		report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, configuredFileStore(t, root), "notes", nil, func(context.Context) error {
			executor.events = append(executor.events, "unexpected nginx")
			return nil
		})
		assertActivationMarkerFailure(t, report, err, nil)
		if len(report.Steps) != 1 {
			t.Fatalf("report = %+v", report.Steps)
		}
		if !executor.active {
			t.Fatal("publication failure stopped the active app")
		}
		if !reflect.DeepEqual(executor.events, []string{
			"zstd --quiet --decompress --stdout",
			"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		}) {
			t.Fatalf("events = %v", executor.events)
		}
		if info, statErr := os.Stat(marker); statErr != nil || info.Mode().Perm() != 0o400 {
			t.Fatalf("failed publication changed marker = %v, %v", info, statErr)
		}
	})

	t.Run("removal", func(t *testing.T) {
		root := t.TempDir()
		store := configuredFileStore(t, root)
		databaseBody := restoreIntegrationDatabaseArchive(t, "restored before marker removal")
		executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, active: true, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`}
		markerDirectory := filepath.Join(root, "run/opsctl/restore")
		rootFS, openErr := os.OpenRoot(root)
		if openErr != nil {
			t.Fatal(openErr)
		}
		execute := func(ctx context.Context, command host.Command) (host.Result, error) {
			result, err := executor.execute(ctx, command)
			if err == nil && command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"start", "ikigenba-notes.service"}) {
				if chmodErr := rootFS.Chmod("run/opsctl/restore", 0o500); chmodErr != nil {
					t.Fatal(chmodErr)
				}
			}
			return result, err
		}
		t.Cleanup(func() {
			_ = rootFS.Chmod("run/opsctl/restore", 0o700)
			_ = rootFS.Close()
		})
		report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: execute}, cloud.Env{Open: restoreClientFor(t, databaseBody).open}, store, "notes", nil, func(context.Context) error {
			executor.events = append(executor.events, "nginx")
			return nil
		})
		assertActivationMarkerFailure(t, report, err, nil)
		if len(report.Steps) != 6 || report.Steps[5] != (backup.RestoreStep{Name: "start", Detail: "litestream.service, ikigenba-notes.socket, ikigenba-notes.service"}) {
			t.Fatalf("report = %+v", report.Steps)
		}
		if !executor.active || !reflect.DeepEqual(executor.events[len(executor.events)-4:], []string{
			"nginx", "systemctl start litestream.service", "systemctl start ikigenba-notes.socket", "systemctl start ikigenba-notes.service",
		}) {
			t.Fatalf("active = %v, events = %v", executor.active, executor.events)
		}
		if info, statErr := os.Stat(filepath.Join(markerDirectory, "notes.active")); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("failed removal lost activation intent: %v, %v", info, statErr)
		}
	})
}

func TestRestoreDatabaseLeavesInitiallyInactiveAppInactive(t *testing.T) {
	// R-XFIS-LWFG
	root := t.TempDir()
	body := restoreIntegrationDatabaseArchive(t, "restored while inactive")
	executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, configuredFileStore(t, root), "notes", nil, func(context.Context) error {
		executor.events = append(executor.events, "nginx")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Steps) != 6 || report.Steps[5] != (backup.RestoreStep{Name: "start", Detail: "litestream.service; ikigenba-notes.socket, ikigenba-notes.service left inactive"}) {
		t.Fatalf("report = %+v", report.Steps)
	}
	wantEvents := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		"systemctl stop ikigenba-notes.socket", "systemctl stop ikigenba-notes.service",
		"systemctl show --property=LoadState --property=ActiveState litestream.service", "systemctl stop litestream.service",
		"litestream ltx -level all -json s3://bucket/host/notes/",
		"litestream restore -o " + filepath.Join(root, "opt/notes/state/app.db") + " s3://bucket/host/notes/",
		"nginx",
		"systemctl start litestream.service",
	}
	if executor.active || !reflect.DeepEqual(executor.events, wantEvents) {
		t.Fatalf("active = %v, events = %v, want %v", executor.active, executor.events, wantEvents)
	}
	if _, statErr := os.Stat(filepath.Join(root, "run/opsctl/restore/notes.active")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("inactive restore created marker: %v", statErr)
	}
}

func TestRestoreDisabledAppNeverRestartsAndClearsMarker(t *testing.T) {
	// R-XEAW-84OR R-XFIS-LWFG R-XGQO-ZO65 R-XKEE-4ZE8
	for _, test := range []struct {
		name, manifest, wantStop, wantStart string
		wantSteps                           int
	}{
		{
			name:      "without database",
			wantStop:  "ikigenba-notes.socket, ikigenba-notes.service",
			wantStart: "ikigenba-notes.socket, ikigenba-notes.service left disabled",
			wantSteps: 4,
		},
		{
			name:      "with database",
			manifest:  "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n",
			wantStop:  "ikigenba-notes.socket, ikigenba-notes.service, litestream.service",
			wantStart: "litestream.service; ikigenba-notes.socket, ikigenba-notes.service left disabled",
			wantSteps: 6,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			entries := []restoreMember{{name: "state/value", data: []byte("restored")}}
			if test.manifest != "" {
				entries = append(entries, restoreMember{name: "etc/manifest.toml", data: []byte(test.manifest)})
			}
			body := hostRestoreArchive(t, entries...)
			writeFile(t, root, "run/opsctl/restore/notes.active", "", 0o600)
			executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, active: true, disabled: true, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, configuredFileStore(t, root), "notes", nil, func(context.Context) error { return nil })
			if err != nil || len(report.Steps) != test.wantSteps || report.Steps[1].Detail != test.wantStop || report.Steps[len(report.Steps)-1].Detail != test.wantStart {
				t.Fatalf("Restore() = %+v, %v", report, err)
			}
			if containsString(executor.events, "systemctl start ikigenba-notes.socket") || containsString(executor.events, "systemctl start ikigenba-notes.service") || containsEventFragment(executor.events, "systemctl enable") || containsEventFragment(executor.events, "systemctl disable") {
				t.Fatalf("disabled app unit changed: %v", executor.events)
			}
			if !containsString(executor.events, "systemctl stop ikigenba-notes.socket") || !containsString(executor.events, "systemctl stop ikigenba-notes.service") {
				t.Fatalf("disabled app units were not stopped: %v", executor.events)
			}
			if _, statErr := os.Stat(filepath.Join(root, "run/opsctl/restore/notes.active")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("disabled app retained activation marker: %v", statErr)
			}
		})
	}
}

func TestRestoreStoppedListsOnlyActiveUnitsWithIntent(t *testing.T) {
	// R-Y093-4019 R-XKEE-4ZE8
	for _, test := range []struct {
		name                                 string
		active, disabled, litestreamInactive bool
		litestreamLoadState                  string
		wantStopped                          []string
	}{
		{name: "active app inactive litestream", active: true, litestreamInactive: true, wantStopped: []string{"ikigenba-notes.socket", "ikigenba-notes.service"}},
		{name: "disabled app active litestream", active: true, disabled: true, wantStopped: []string{"litestream.service"}},
		{name: "inactive app active litestream", wantStopped: []string{"litestream.service"}},
		{name: "inactive app masked active litestream", litestreamLoadState: "masked", wantStopped: []string{"litestream.service"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			body := restoreIntegrationDatabaseArchive(t, "restored")
			executor := &restoreIntegrationExecutor{t: t, root: root, installed: true, active: test.active, disabled: test.disabled, litestreamInactive: test.litestreamInactive, litestreamLoadState: test.litestreamLoadState, ltx: `[]`}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, configuredFileStore(t, root), "notes", nil, func(context.Context) error { return nil })
			var failure *backup.RestoreError
			if !errors.As(err, &failure) || failure.Stage != "litestream restore" || !reflect.DeepEqual(failure.Stopped, test.wantStopped) || len(report.Steps) != 4 || report.Steps[3].Name != "db" {
				t.Fatalf("Restore() = %+v, %#v; stopped %v", report, err, test.wantStopped)
			}
		})
	}
}

func TestRestoreDatabaseDoesNotTouchOtherServiceOrCloud(t *testing.T) {
	// R-1JU3-1QOH
	root := t.TempDir()
	store := configuredFileStore(t, root)
	const (
		otherManifest = "app = \"other\"\n"
		otherSecret   = "unrelated secret"
	)
	writeFile(t, root, "opt/other/etc/manifest.toml", otherManifest, 0o600)
	writeFile(t, root, "opt/other/state/private", otherSecret, 0o600)
	body := restoreIntegrationDatabaseArchive(t, "database restore")
	client := restoreClientFor(t, body)
	executor := &restoreIntegrationExecutor{t: t, root: root, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error {
		executor.events = append(executor.events, "nginx")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Steps) != 6 || report.Steps[1].Detail != "litestream.service, no ikigenba-notes.socket" || report.Steps[5].Detail != "litestream.service" {
		t.Fatalf("report = %+v", report.Steps)
	}
	if got := string(readHostRestoreFile(t, root, "opt/other/etc/manifest.toml")); got != otherManifest {
		t.Fatalf("other manifest = %q", got)
	}
	if got := string(readHostRestoreFile(t, root, "opt/other/state/private")); got != otherSecret {
		t.Fatalf("other state = %q", got)
	}
	if client.puts != 0 {
		t.Fatalf("restore wrote %d cloud objects", client.puts)
	}
	wantEvents := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.socket", "systemctl show --property=LoadState --property=UnitFileState ikigenba-notes.socket",
		"systemctl show --property=LoadState --property=ActiveState litestream.service", "systemctl stop litestream.service",
		"litestream ltx -level all -json s3://bucket/host/notes/",
		"litestream restore -o " + filepath.Join(root, "opt/notes/state/app.db") + " s3://bucket/host/notes/",
		"nginx",
		"systemctl start litestream.service",
	}
	if !reflect.DeepEqual(executor.events, wantEvents) {
		t.Fatalf("events = %v, want %v", executor.events, wantEvents)
	}
}

func assertActivationMarkerFailure(t *testing.T, report backup.RestoreReport, err error, wantStopped []string) {
	t.Helper()
	var failure *backup.RestoreError
	if err == nil || !errors.As(err, &failure) || failure.Stage != "activation marker" || !reflect.DeepEqual(failure.Stopped, wantStopped) {
		t.Fatalf("Restore() error = %#v", err)
	}
	if len(report.Steps) == 0 || report.Steps[0].Name != "source" || report.Steps[0].Err != nil {
		t.Fatalf("report = %+v", report.Steps)
	}
	for _, step := range report.Steps {
		if strings.Contains(step.Name, "marker") || strings.Contains(step.Detail, "marker") {
			t.Fatalf("marker maintenance appeared in report: %+v", report.Steps)
		}
	}
}

type restoreIntegrationExecutor struct {
	t                   *testing.T
	root                string
	installed           bool
	active              bool
	socketLoadState     string
	disabled            bool
	litestreamInactive  bool
	litestreamLoadState string
	ltx                 string
	failCommand         string
	events              []string
}

func (executor *restoreIntegrationExecutor) execute(_ context.Context, command host.Command) (host.Result, error) {
	text := strings.Join(append([]string{command.Name}, command.Args...), " ")
	executor.events = append(executor.events, text)
	if text == executor.failCommand {
		return host.Result{ExitCode: 1, Stdout: []byte("partial\n"), Stderr: []byte("failure\n")}, nil
	}
	switch command.Name {
	case "zstd":
		compressed, err := io.ReadAll(command.Stdin)
		if err != nil {
			return host.Result{}, err
		}
		return host.Result{Stdout: decodeRawZstandardFrame(executor.t, compressed)}, nil
	case "systemctl":
		if len(command.Args) == 4 && command.Args[0] == "show" {
			if command.Args[2] == "--property=UnitFileState" {
				load, state := "not-found", "disabled"
				if executor.installed {
					load = "loaded"
				}
				if executor.socketLoadState != "" {
					load = executor.socketLoadState
				}
				if !executor.disabled {
					state = "enabled"
				}
				return host.Result{Stdout: []byte("LoadState=" + load + "\nUnitFileState=" + state + "\n")}, nil
			}
			load, active := "not-found", "inactive"
			if executor.installed || command.Args[3] == "litestream.service" {
				load = "loaded"
			}
			if executor.socketLoadState != "" && command.Args[3] != "litestream.service" {
				load = executor.socketLoadState
			}
			if executor.litestreamLoadState != "" && command.Args[3] == "litestream.service" {
				load = executor.litestreamLoadState
			}
			if executor.active && command.Args[3] != "litestream.service" || command.Args[3] == "litestream.service" && !executor.litestreamInactive {
				active = "active"
			}
			return host.Result{Stdout: []byte("LoadState=" + load + "\nActiveState=" + active + "\n")}, nil
		}
		if len(command.Args) == 2 && command.Args[0] == "stop" {
			if command.Args[1] == "ikigenba-notes.socket" {
				executor.active = false
			}
			return host.Result{}, nil
		}
		if len(command.Args) == 2 && command.Args[0] == "start" {
			if command.Args[1] == "ikigenba-notes.socket" {
				executor.active = true
			}
			return host.Result{}, nil
		}
	case "litestream":
		if len(command.Args) == 5 && command.Args[0] == "ltx" {
			return host.Result{Stdout: []byte(executor.ltx)}, nil
		}
		if len(command.Args) >= 4 && command.Args[0] == "restore" && command.Args[1] == "-o" {
			writeRestoreSQLite(executor.t, command.Args[2])
			return host.Result{}, nil
		}
	}
	return host.Result{}, fmt.Errorf("unexpected command %q", text)
}

func restoreIntegrationDatabaseArchive(t *testing.T, value string) []byte {
	t.Helper()
	return hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
		restoreMember{name: "state/value", data: []byte(value)},
	)
}

func restoreIntegrationTime(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return &parsed
}

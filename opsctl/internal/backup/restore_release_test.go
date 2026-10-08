package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

const restoreReleaseSHA = "c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18"

func restoreReleaseFixture(t *testing.T, root string, released, database bool) release.Release {
	t.Helper()
	dir := "opt/ikigenba/releases/" + restoreReleaseSHA
	writeFile(t, root, dir+"/release.json", `{"sha":"`+restoreReleaseSHA+`"}`, 0600)
	writeFile(t, root, dir+"/opsctl/bin/opsctl", "binary", 0700)
	writeFile(t, root, dir+"/notes/bin/notes", "release binary", 0700)
	manifest := "app='notes'\n[env]\nPLAIN='release value'\n"
	if database {
		manifest += "[database]\nengine='sqlite'\npath='state/app.db'\n"
	}
	writeFile(t, root, dir+"/notes/etc/manifest.toml", manifest, 0600)
	if database {
		writeFile(t, root, dir+"/other/bin/other", "binary", 0700)
		writeFile(t, root, dir+"/other/etc/manifest.toml", "app='other'\n[database]\nengine='sqlite'\npath='state/other.db'\n", 0600)
	}
	r := release.Release{SHA: restoreReleaseSHA}
	if released {
		r.Label = "deployment"
		writeFile(t, root, dir+"/label", r.Label+"\n", 0600)
		if err := os.Symlink("releases/"+restoreReleaseSHA, filepath.Join(root, "opt/ikigenba/current")); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestRestoreReleaseEnvironmentAndFreshUnitLifecycle(t *testing.T) {
	// R-GSZD-5A23 R-GU79-J1SS R-G892-N6GA R-G9GZ-0Y6Z R-FZPR-YS9F R-G4LD-HV87 R-G5T9-VMYW R-G3DH-43HI R-GAOV-EPXO R-LC1T-L8RR R-FEZH-GONM
	for _, released := range []bool{false, true} {
		for _, database := range []bool{false, true} {
			t.Run(map[bool]string{false: "fresh", true: "released"}[released]+map[bool]string{false: "opaque", true: "database"}[database], func(t *testing.T) {
				root := t.TempDir()
				store := restoreConfiguredStore(t, root)
				r := restoreReleaseFixture(t, root, released, database)
				// Per-app state must not affect a released host; an empty fresh host has no per-app tree.
				if released {
					writeFile(t, root, "opt/notes/etc/manifest.toml", "app='wrong'\n", 0600)
					writeFile(t, root, "opt/notes/state/stale", "legacy", 0600)
				}
				before := fileTreeSnapshot(t, filepath.Join(root, "opt"))
				body := hostRestoreArchive(t, restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0750}, restoreMember{name: "state/value", data: []byte("restored")})
				client := restoreClientFor(t, body)
				executor := &databaseRestoreExecutor{t: t, root: root, installed: released, active: released, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`}
				callbacks := 0
				stopped, recovered := false, false
				if database {
					writeFile(t, root, "etc/litestream.yml", "prior configuration\n", 0600)
				}
				execute := func(ctx context.Context, command host.Command) (host.Result, error) {
					text := strings.Join(append([]string{command.Name}, command.Args...), " ")
					if database && command.Name == "litestream" {
						if got := string(readHostRestoreFile(t, root, "etc/litestream.yml")); got != "prior configuration\n" {
							t.Fatalf("configuration changed before recovery: %q", got)
						}
						if !stopped {
							t.Fatal("recovery while Litestream running")
						}
					}
					if text == "systemctl start litestream.service" {
						if !stopped || !recovered {
							t.Fatal("Litestream start before stopped database recovery")
						}
						assertRestoreReleaseConfigurationAndWAL(t, root)
					}
					result, err := executor.execute(ctx, command)
					if err == nil && result.ExitCode == 0 {
						if text == "systemctl stop litestream.service" {
							stopped = true
						}
						if command.Name == "litestream" && command.Args[0] == "restore" {
							recovered = true
						}
						if text == "systemctl start litestream.service" {
							stopped = false
						}
					}
					return result, err
				}
				// The running opsctl's own release differs; current is authoritative once activated.
				own := r
				if released {
					own = release.Release{SHA: strings.Repeat("a", 40), Label: "other"}
				}
				report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: execute}, cloud.Env{Open: client.open}, store, "notes", nil, "", &own, func(context.Context) error { callbacks++; return nil })
				if err != nil {
					t.Fatal(err)
				}
				wantEnv := "PLAIN=\"release value\"\nDRAIN_SECONDS=5\nIKIGENBA_SERVICES=/run/ikigenba/services.json\nIKIGENBA_COMMIT=" + r.SHA + "\n"
				if released {
					wantEnv += "IKIGENBA_RELEASE=" + r.Label + "\n"
				}
				if got := string(readHostRestoreFile(t, root, "etc/opt/ikigenba/notes/env")); got != wantEnv {
					t.Fatalf("environment=%q want=%q", got, wantEnv)
				}
				if !reflect.DeepEqual(before, fileTreeSnapshot(t, filepath.Join(root, "opt"))) {
					t.Fatal("restore changed deployment tree")
				}
				if callbacks != map[bool]int{false: 0, true: 1}[released] {
					t.Fatalf("callbacks=%d", callbacks)
				}
				names := []string{"source", "secrets", "stop", "files"}
				if database {
					names = append(names, "db", "litestream")
				}
				names = append(names, "start")
				for i, name := range names {
					if report.Steps[i].Name != name || report.Steps[i].Err != nil {
						t.Fatalf("steps=%+v", report.Steps)
					}
				}
				if len(report.Steps) != len(names) {
					t.Fatalf("steps=%+v", report.Steps)
				}
				if !released {
					detail := "none"
					if database {
						detail = "litestream.service"
					}
					if report.Steps[2].Detail != detail || report.Steps[len(report.Steps)-1].Detail != detail {
						t.Fatalf("fresh details=%+v", report.Steps)
					}
					for _, event := range executor.events {
						if strings.Contains(event, "ikigenba-notes.") {
							t.Fatalf("fresh app-unit access=%s", event)
						}
					}
					if _, err := os.Lstat(filepath.Join(root, "run/opsctl/restore/notes.active")); !os.IsNotExist(err) {
						t.Fatalf("fresh marker: %v", err)
					}
				}
				if database {
					config := string(readHostRestoreFile(t, root, "etc/litestream.yml"))
					if !strings.Contains(config, "/var/opt/ikigenba/notes/state/app.db") || !containsString(executor.events, "systemctl start litestream.service") {
						t.Fatalf("configuration=%q events=%v", config, executor.events)
					}
				} else {
					for _, event := range executor.events {
						if strings.Contains(event, "litestream") {
							t.Fatalf("opaque restore=%s", event)
						}
					}
				}
			})
		}
	}
}

func TestRestoreFreshWithoutReleaseAndBrokenCurrentAreInert(t *testing.T) {
	// R-GSZD-5A23
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "no own", true: "broken current"}[broken], func(t *testing.T) {
			root := t.TempDir()
			store := restoreConfiguredStore(t, root)
			if broken {
				writeFile(t, root, "opt/ikigenba/current", "not a link", 0600)
			}
			before := fileTreeSnapshot(t, root)
			used := false
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) { used = true; return host.Result{}, nil }}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { used = true; return nil, nil }}, store, "notes", nil, "", nil, func(context.Context) error { used = true; return nil })
			if err == nil || len(report.Steps) != 0 || used || !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
				t.Fatalf("report=%+v err=%v used=%v", report, err, used)
			}
			if !broken && err.Error() != "restore needs a release; run /opt/ikigenba/releases/<sha>/opsctl/bin/opsctl restore notes" {
				t.Fatal(err)
			}
		})
	}
}

func TestRestoreReleaseMembershipAndManifestFaultsAreInert(t *testing.T) {
	// R-GU79-J1SS R-GI09-PCDU
	for _, released := range []bool{false, true} {
		for _, kind := range []string{"data only", "missing binary", "symlink app", "manifest directory", "dangling manifest", "disowned manifest", "faulted manifest", "reserved"} {
			t.Run(map[bool]string{false: "fresh", true: "released"}[released]+kind, func(t *testing.T) {
				root := t.TempDir()
				store := restoreConfiguredStore(t, root)
				r := restoreReleaseFixture(t, root, released, false)
				service := "crm"
				dir := "opt/ikigenba/releases/" + r.SHA + "/crm"
				writeFile(t, root, "var/opt/ikigenba/crm/state/keep", "data", 0600)
				want := "crm is not in release " + r.Short()
				if released {
					want = "crm is not in the current release"
				}
				switch kind {
				case "missing binary":
					writeFile(t, root, dir+"/etc/manifest.toml", "app='crm'\n", 0600)
				case "symlink app":
					if err := os.Symlink("notes", filepath.Join(root, dir)); err != nil {
						t.Fatal(err)
					}
				case "manifest directory", "dangling manifest", "disowned manifest", "faulted manifest":
					writeFile(t, root, dir+"/bin/crm", "binary", 0700)
					switch kind {
					case "manifest directory":
						if err := os.MkdirAll(filepath.Join(root, dir, "etc/manifest.toml"), 0700); err != nil {
							t.Fatal(err)
						}
						want = "crm: etc/manifest.toml: "
					case "dangling manifest":
						if err := os.MkdirAll(filepath.Join(root, dir, "etc"), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink("missing", filepath.Join(root, dir, "etc/manifest.toml")); err != nil {
							t.Fatal(err)
						}
						want = "crm: etc/manifest.toml: "
					case "disowned manifest":
						writeFile(t, root, dir+"/etc/manifest.toml", "app='other'\n", 0600)
						want = "crm: etc/manifest.toml: app must be 'crm'"
					case "faulted manifest":
						writeFile(t, root, dir+"/etc/manifest.toml", "app='crm'\n[resources]\nio_weight=50\n", 0600)
						want = "crm: etc/manifest.toml: "
					}
				case "reserved":
					service = "backup-host"
					want = "backup-host is not in release " + r.Short()
					if released {
						want = "backup-host is not in the current release"
					}
				}
				before := fileTreeSnapshot(t, root)
				used := false
				for _, from := range []string{"", "s3://other/snapshot"} {
					report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) { used = true; return host.Result{}, nil }}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { used = true; return nil, nil }}, store, service, nil, from, &r, func(context.Context) error { used = true; return nil })
					var failure *backup.RestoreError
					if !errors.As(err, &failure) || failure.Stage != "source" || len(failure.Stopped) != 0 || len(report.Steps) != 1 || report.Steps[0].Err == nil || !strings.HasPrefix(report.Steps[0].Err.Error(), want) || used || !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
						t.Fatalf("report=%+v err=%v used=%v want=%q", report, err, used, want)
					}
				}
			})
		}
	}
}

func TestRestoreReleaseSecretsValidationPrecedesUnits(t *testing.T) {
	// R-G892-N6GA R-G9GZ-0Y6Z
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "values", true: "missing"}[failure], func(t *testing.T) {
			root := t.TempDir()
			store := restoreConfiguredStore(t, root)
			r := restoreReleaseFixture(t, root, false, false)
			if err := store.Set("host.name", "Host.Example.Test."); err != nil {
				t.Fatal(err)
			}
			writeFile(t, root, "opt/ikigenba/releases/"+r.SHA+"/notes/etc/manifest.toml", "app='notes'\nsecrets=['TOKEN','TOKEN']\n[env]\nPLAIN='setting'\n", 0600)
			body := hostRestoreArchive(t, restoreMember{name: "etc/env", data: []byte("TOKEN=archive")}, restoreMember{name: "state/value", data: []byte("state")})
			client := &snapshotRestoreCloud{restoreCloud: restoreClientFor(t, body), secrets: map[string]string{"TOKEN": "host secret"}}
			if failure {
				client.secrets = map[string]string{}
			}
			executor := &databaseRestoreExecutor{t: t, root: root}
			before := fileTreeSnapshot(t, root)
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, "", &r, func(context.Context) error { t.Fatal("fresh callback"); return nil })
			if !reflect.DeepEqual(client.parameters, []string{"/host.example.test/notes"}) {
				t.Fatalf("parameters=%v", client.parameters)
			}
			if failure {
				var restoreErr *backup.RestoreError
				if !errors.As(err, &restoreErr) || restoreErr.Stage != "secrets" || len(restoreErr.Stopped) != 0 || len(report.Steps) != 2 || report.Steps[1].Err == nil || report.Steps[1].Err.Error() != "notes: no value for 'TOKEN' in /host.example.test/notes" || len(executor.events) != 1 || !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
					t.Fatalf("report=%+v err=%v events=%v", report, err, executor.events)
				}
			} else {
				if err != nil || report.Steps[1].Detail != "1 keys" {
					t.Fatalf("report=%+v err=%v", report, err)
				}
				want := "TOKEN=\"host secret\"\nPLAIN=\"setting\"\nDRAIN_SECONDS=5\nIKIGENBA_SERVICES=/run/ikigenba/services.json\nIKIGENBA_COMMIT=" + r.SHA + "\n"
				if got := string(readHostRestoreFile(t, root, "etc/opt/ikigenba/notes/env")); got != want {
					t.Fatalf("env=%q", got)
				}
				for _, step := range report.Steps {
					if strings.Contains(step.Detail, "host secret") {
						t.Fatal("secret in report")
					}
				}
			}
		})
	}
}

func TestRestoreFreshSnapshotIncludesDatabaseWithoutReplica(t *testing.T) {
	// R-FTMA-1XJY R-G3DH-43HI R-GGSD-BKN5 R-G4LD-HV87 R-FEZH-GONM
	for _, validDB := range []bool{true, false} {
		t.Run(map[bool]string{true: "database", false: "invalid database"}[validDB], func(t *testing.T) {
			root := t.TempDir()
			store := restoreConfiguredStore(t, root)
			r := restoreReleaseFixture(t, root, false, true)
			db := make([]byte, 100)
			if validDB {
				copy(db, "SQLite format 3\x00")
			}
			db[18], db[19] = 1, 1
			body := hostRestoreArchive(t, restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0750}, restoreMember{name: "state/app.db", data: db, mode: 0600})
			from := "s3://elsewhere/snapshot"
			client := &restoreCloud{bodies: map[string][]byte{from: body}}
			executor := &databaseRestoreExecutor{t: t, root: root}
			writeFile(t, root, "etc/litestream.yml", "prior configuration\n", 0600)
			stopped, started := false, false
			execute := func(ctx context.Context, command host.Command) (host.Result, error) {
				text := strings.Join(append([]string{command.Name}, command.Args...), " ")
				if command.Name == "getent" || command.Name == "chown" {
					if !stopped {
						t.Fatal("files published before Litestream stop")
					}
					if got := string(readHostRestoreFile(t, root, "etc/litestream.yml")); got != "prior configuration\n" {
						t.Fatalf("configuration changed before files/database: %q", got)
					}
				}
				if text == "systemctl start litestream.service" {
					if !stopped {
						t.Fatal("Litestream was not stopped before regeneration/start")
					}
					assertRestoreReleaseConfigurationAndWAL(t, root)
					started = true
				}
				result, err := executor.execute(ctx, command)
				if err == nil && result.ExitCode == 0 && text == "systemctl stop litestream.service" {
					stopped = true
				}
				return result, err
			}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: execute}, cloud.Env{Open: client.open}, store, "notes", nil, from, &r, func(context.Context) error { t.Fatal("fresh callback"); return nil })
			if !validDB {
				var failure *backup.RestoreError
				if !errors.As(err, &failure) || failure.Stage != "wal mode" || len(report.Steps) != 5 || report.Steps[4].Name != "db" || report.Steps[4].Err == nil || started {
					t.Fatalf("invalid DB report=%+v err=%v started=%v", report, err, started)
				}
				if got := string(readHostRestoreFile(t, root, "etc/litestream.yml")); got != "prior configuration\n" {
					t.Fatalf("regenerated before database success: %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if report.Steps[4].Detail != "/var/opt/ikigenba/notes/state/app.db, from snapshot" || report.Steps[5].Name != "litestream" || report.Steps[6].Detail != "litestream.service" || !started {
				t.Fatalf("report=%+v started=%v", report, started)
			}
			for _, event := range executor.events {
				if strings.HasPrefix(event, "litestream ") || strings.Contains(event, "ikigenba-notes.") {
					t.Fatalf("fresh snapshot command=%s", event)
				}
			}
			if len(client.listed) != 0 || !reflect.DeepEqual(client.got, []string{from}) {
				t.Fatalf("list=%v get=%v", client.listed, client.got)
			}
		})
	}
}

func assertRestoreReleaseConfigurationAndWAL(t *testing.T, root string) {
	t.Helper()
	configuration := string(readHostRestoreFile(t, root, "etc/litestream.yml"))
	for _, database := range []string{"/var/opt/ikigenba/notes/state/app.db", "/var/opt/ikigenba/other/state/other.db"} {
		if !strings.Contains(configuration, database) {
			t.Fatalf("release database %s missing before Litestream start: %q", database, configuration)
		}
	}
	restored := readHostRestoreFile(t, root, "var/opt/ikigenba/notes/state/app.db")
	if len(restored) < 20 || restored[18] != 2 || restored[19] != 2 {
		t.Fatal("database not restored in WAL mode before Litestream start")
	}
}

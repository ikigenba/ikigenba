package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type snapshotRestoreCloud struct {
	*restoreCloud
	secrets    map[string]string
	parameters []string
	secretErr  error
}

func (c *snapshotRestoreCloud) ReadSecrets(_ context.Context, parameter string) (map[string]string, error) {
	c.parameters = append(c.parameters, parameter)
	return c.secrets, c.secretErr
}
func (c *snapshotRestoreCloud) open(ctx context.Context, region string) (cloud.Client, error) {
	_, err := c.restoreCloud.open(ctx, region)
	return c, err
}
func snapshotRestoreExecute(t *testing.T, root string, events *[]string) func(context.Context, host.Command) (host.Result, error) {
	t.Helper()
	base := &restoreStageExecutor{t: t, root: root}
	return func(ctx context.Context, command host.Command) (host.Result, error) {
		*events = append(*events, strings.Join(append([]string{command.Name}, command.Args...), " "))
		if command.Name == "chown" {
			want := []string{"root:ikigenba", filepath.Join(root, "opt", "notes", "etc", "env")}
			if !reflect.DeepEqual(command.Args, want) {
				t.Fatalf("chown args %v", command.Args)
			}
			return host.Result{}, nil
		}
		return base.execute(ctx, command)
	}
}
func TestRestoreSnapshotRegeneratesEnvironmentAndUsesIncludedDatabase(t *testing.T) {
	// R-1W2L-4XPW R-1XAH-IPGL R-2263-1SFD R-23DZ-FK62 R-2BXA-3YCX R-6A1O-HC42 R-2FKZ-99L0 R-2GSV-N1BP R-6B9K-V3UR
	root := t.TempDir()
	store := configuredFileStore(t, root)
	if err := store.Set("host.name", "Host.Example.COM."); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "opt/notes/state/stale", "old", 0600)
	writeFile(t, root, "opt/notes/bin/notes", "binary", 0700)
	db := make([]byte, 512)
	copy(db, "SQLite format 3\x00")
	db[18] = 1
	db[19] = 1
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("secrets = [\"TOKEN\", \"TOKEN\"]\n[env]\nPLAIN = \"hello world\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
		restoreMember{name: "etc/env", data: []byte("archived secret")},
		restoreMember{name: "state/app.db", data: db, mode: 0640},
	)
	from := "s3://another-bucket/seed/object.tar.zst"
	client := &snapshotRestoreCloud{restoreCloud: &restoreCloud{bodies: map[string][]byte{from: body}}, secrets: map[string]string{"TOKEN": "new token", "IGNORED": "private"}}
	var events []string
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: snapshotRestoreExecute(t, root, &events)}, cloud.Env{Open: client.open}, store, "notes", nil, from, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("Restore = %+v, %v", report, err)
	}
	var names []string
	for _, s := range report.Steps {
		names = append(names, s.Name)
		if s.Err != nil {
			t.Fatal(s.Err)
		}
	}
	if !reflect.DeepEqual(names, []string{"source", "secrets", "stop", "files", "db", "litestream", "start"}) {
		t.Fatalf("steps %v", report.Steps)
	}
	if report.Steps[0].Detail != fmt.Sprintf("%s, %.1f MiB", from, float64(len(body))/1048576) || report.Steps[1].Detail != "1 keys" || report.Steps[3].Detail != "/opt/notes/etc, /opt/notes/state, 2 files" || report.Steps[4].Detail != "/opt/notes/state/app.db, from snapshot" {
		t.Fatalf("steps %+v", report.Steps)
	}
	if !reflect.DeepEqual(client.got, []string{from}) || len(client.listed) != 0 || !reflect.DeepEqual(client.parameters, []string{"/host.example.com/notes"}) || client.puts != 0 {
		t.Fatalf("cloud %+v", client)
	}
	for _, event := range events {
		if strings.HasPrefix(event, "litestream ") {
			t.Fatalf("invoked replica command %s", event)
		}
	}
	envData := string(readHostRestoreFile(t, root, "opt/notes/etc/env"))
	if !strings.Contains(envData, "TOKEN=\"new token\"\n") || !strings.Contains(envData, "PLAIN=\"hello world\"\n") || !strings.Contains(envData, "DRAIN_SECONDS=") || !strings.Contains(envData, "IKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n") || strings.Contains(envData, "IGNORED") || strings.Contains(envData, "archived") {
		t.Fatalf("environment unexpected %q", envData)
	}
	info, err := os.Stat(filepath.Join(root, "opt/notes/etc/env"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("environment mode %v %v", info, err)
	}
	db = readHostRestoreFile(t, root, "opt/notes/state/app.db")
	assertRestoreMetadata(t, root, "opt/notes/state/app.db", 0640, os.Getuid(), os.Getgid())
	if db[18] != 2 || db[19] != 2 {
		t.Fatal("snapshot database was not converted to WAL")
	}
	if _, err := os.Stat(filepath.Join(root, "opt/notes/state/stale")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale file retained")
	}
	if string(readHostRestoreFile(t, root, "opt/notes/bin/notes")) != "binary" {
		t.Fatal("binary changed")
	}
}
func TestRestoreSnapshotPreworkflowAndSourceFailures(t *testing.T) {
	// R-2APD-Q6M8 R-2BXA-3YCX R-1XAH-IPGL
	tests := []struct {
		name, from, hostName string
		at                   *time.Time
		body                 []byte
		getErr               error
		want                 string
		step                 bool
	}{
		{name: "combined", from: "s3://bucket/key", hostName: "host", at: new(time.Time), want: "--at and --from cannot be combined"},
		{name: "relative", from: "key", hostName: "host", want: "--from takes an s3:// URI"},
		{name: "empty key", from: "s3://bucket/", hostName: "host", want: "--from takes an s3:// URI"},
		{name: "empty fragment", from: "s3://bucket/key#", hostName: "host", want: "--from takes an s3:// URI"},
		{name: "empty host setting", from: "s3://bucket/key", hostName: "", want: "host.name not set"},
		{name: "not found", from: "s3://bucket/key", hostName: "host", want: "s3://bucket/key: no such object", step: true},
		{name: "fetch failure", from: "s3://bucket/key", hostName: "host", body: []byte("body"), getErr: errors.New("denied"), want: "s3://bucket/key: denied", step: true},
		{name: "missing database", from: "s3://bucket/key", hostName: "host", body: hostRestoreArchive(t, restoreMember{name: "etc/manifest.toml", data: []byte("[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")}), want: "s3://bucket/key: no state/app.db in the snapshot", step: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			if err := store.Set("host.name", tc.hostName); err != nil {
				t.Fatal(err)
			}
			client := &restoreCloud{bodies: map[string][]byte{}, getErr: tc.getErr}
			if tc.body != nil {
				client.bodies[tc.from] = tc.body
			}
			var events []string
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: snapshotRestoreExecute(t, root, &events)}, cloud.Env{Open: client.open}, store, "notes", tc.at, tc.from, func(context.Context) error { t.Fatal("callback invoked"); return nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("report %+v error %v", report, err)
			}
			if tc.step {
				if len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Detail != "" || report.Steps[0].Err == nil {
					t.Fatalf("steps %+v", report.Steps)
				}
			} else if len(report.Steps) != 0 || len(client.got) != 0 {
				t.Fatalf("preworkflow effects %+v %+v", report, client)
			}
			if len(client.listed) != 0 {
				t.Fatal("listed objects")
			}
			for _, event := range events {
				if !strings.HasPrefix(event, "zstd ") {
					t.Fatalf("host action %s", event)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "opt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("created service root")
			}
		})
	}
}

func TestRestoreSnapshotSecretsFailuresStopBeforeUnits(t *testing.T) {
	// R-6A1O-HC42 R-1XAH-IPGL R-6B9K-V3UR
	for _, tc := range []struct {
		name, manifest string
		values         map[string]string
		setting, want  string
		secretErr      error
	}{
		{name: "first missing", manifest: "secrets = [\"FIRST\",\"SECOND\"]\n", values: map[string]string{}, want: "notes: no value for 'FIRST' in /host.example/notes"},
		{name: "missing parameter", manifest: "secrets = [\"FIRST\"]\n", secretErr: cloud.ErrNotFound, want: "notes: no value for 'FIRST' in /host.example/notes"},
		{name: "parameter error", manifest: "secrets = [\"FIRST\"]\n", secretErr: errors.New("parameter denied"), want: "parameter denied"},
		{name: "invalid secret value", manifest: "secrets = [\"FIRST\"]\n", values: map[string]string{"FIRST": "hidden\nsecret"}, want: "forbidden character"},
		{name: "bad timing", manifest: "", setting: "bogus", want: "apps.drain_seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			if err := store.Set("host.name", "HOST.EXAMPLE."); err != nil {
				t.Fatal(err)
			}
			if tc.setting != "" {
				if err := store.Set("apps.drain_seconds", tc.setting); err != nil {
					t.Fatal(err)
				}
			}
			body := hostRestoreArchive(t, restoreMember{name: "etc/manifest.toml", data: []byte(tc.manifest)})
			from := "s3://bucket/snapshot"
			client := &snapshotRestoreCloud{restoreCloud: &restoreCloud{bodies: map[string][]byte{from: body}}, secrets: tc.values, secretErr: tc.secretErr}
			var events []string
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: snapshotRestoreExecute(t, root, &events)}, cloud.Env{Open: client.open}, store, "notes", nil, from, func(context.Context) error { t.Fatal("callback invoked"); return nil })
			var failure *backup.RestoreError
			if !errors.As(err, &failure) || failure.Stage != "secrets" || len(failure.Stopped) != 0 || len(report.Steps) != 2 || report.Steps[1].Name != "secrets" || report.Steps[1].Err == nil || report.Steps[1].Detail != "" || !strings.Contains(report.Steps[1].Err.Error(), tc.want) {
				t.Fatalf("Restore %+v error %#v", report, err)
			}
			if strings.Contains(fmt.Sprint(report, err), "hidden") {
				t.Fatal("leaked secret value")
			}
			if len(events) != 1 || !strings.HasPrefix(events[0], "zstd ") {
				t.Fatalf("host actions %v", events)
			}
			for _, name := range []string{"opt", "run/opsctl/restore"} {
				if _, err := os.Stat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("created %s", name)
				}
			}
		})
	}
}

func TestRestoreSnapshotEnvironmentReplacesArchivedDirectory(t *testing.T) {
	// R-2FKZ-99L0 R-23DZ-FK62 R-6A1O-HC42
	root := t.TempDir()
	store := configuredFileStore(t, root)
	if err := store.Set("host.name", "host"); err != nil {
		t.Fatal(err)
	}
	body := hostRestoreArchive(t, restoreMember{name: "etc/manifest.toml", data: []byte("[env]\nVALUE = \"new\"\n")}, restoreMember{name: "etc/env/old", data: []byte("old")})
	from := "s3://bucket/snapshot"
	client := &snapshotRestoreCloud{restoreCloud: &restoreCloud{bodies: map[string][]byte{from: body}}}
	var events []string
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: snapshotRestoreExecute(t, root, &events)}, cloud.Env{Open: client.open}, store, "notes", nil, from, func(context.Context) error { return nil })
	if err != nil || len(report.Steps) != 5 || report.Steps[1].Detail != "0 keys" || report.Steps[3].Detail != "/opt/notes/etc, /opt/notes/state, 1 files" {
		t.Fatalf("Restore %+v %v", report, err)
	}
	if len(client.parameters) != 0 {
		t.Fatal("read empty secrets")
	}
	if data := string(readHostRestoreFile(t, root, "opt/notes/etc/env")); !strings.Contains(data, "VALUE=\"new\"\n") {
		t.Fatalf("environment %q", data)
	}
}

func TestRestoreSnapshotWithoutManifestRetainsEnvironment(t *testing.T) {
	// R-2FKZ-99L0 R-6A1O-HC42 R-6B9K-V3UR
	root := t.TempDir()
	store := configuredFileStore(t, root)
	if err := store.Set("host.name", "host"); err != nil {
		t.Fatal(err)
	}
	body := hostRestoreArchive(t, restoreMember{name: "etc/env", data: []byte("archived"), mode: 0640})
	from := "s3://bucket/snapshot"
	client := &snapshotRestoreCloud{restoreCloud: &restoreCloud{bodies: map[string][]byte{from: body}}}
	var events []string
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: snapshotRestoreExecute(t, root, &events)}, cloud.Env{Open: client.open}, store, "notes", nil, from, func(context.Context) error { return nil })
	if err != nil || len(report.Steps) != 4 || len(client.parameters) != 0 {
		t.Fatalf("Restore %+v %v parameters %v", report, err, client.parameters)
	}
	if data := string(readHostRestoreFile(t, root, "opt/notes/etc/env")); data != "archived" {
		t.Fatalf("environment %q", data)
	}
	for _, event := range events {
		if strings.HasPrefix(event, "getent ") || strings.HasPrefix(event, "chown ") {
			t.Fatalf("account action without manifest %s", event)
		}
	}
}

func TestRestoreSnapshotEnvironmentFailuresRemainAtFiles(t *testing.T) {
	// R-2FKZ-99L0 R-1XAH-IPGL R-6B9K-V3UR
	for _, tc := range []struct{ name, failCommand, stage string }{
		{name: "account", failCommand: "id --user ikigenba", stage: "ownership"},
		{name: "publication ownership", failCommand: "chown", stage: "environment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			if err := store.Set("host.name", "host"); err != nil {
				t.Fatal(err)
			}
			body := hostRestoreArchive(t, restoreMember{name: "etc/manifest.toml", data: []byte("")})
			from := "s3://bucket/snapshot"
			client := &snapshotRestoreCloud{restoreCloud: &restoreCloud{bodies: map[string][]byte{from: body}}}
			var events []string
			execute := snapshotRestoreExecute(t, root, &events)
			cause := errors.New("injected failure")
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(ctx context.Context, command host.Command) (host.Result, error) {
				text := strings.Join(append([]string{command.Name}, command.Args...), " ")
				if text == tc.failCommand || command.Name == tc.failCommand {
					return host.Result{}, cause
				}
				return execute(ctx, command)
			}}, cloud.Env{Open: client.open}, store, "notes", nil, from, func(context.Context) error { t.Fatal("callback invoked"); return nil })
			var failure *backup.RestoreError
			if !errors.As(err, &failure) || failure.Stage != tc.stage || !errors.Is(err, cause) || len(report.Steps) != 4 || report.Steps[3].Name != "files" || report.Steps[3].Err == nil || report.Steps[3].Detail != "" {
				t.Fatalf("Restore %+v error %#v", report, err)
			}
			if tc.stage == "ownership" {
				if _, err := os.Stat(filepath.Join(root, "opt")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("account failure mutated target")
				}
			}
		})
	}
}

func TestRestoreSnapshotMapsDatabaseOwnership(t *testing.T) {
	// R-2GSV-N1BP R-ZBT9-WYGY
	root := t.TempDir()
	store := configuredFileStore(t, root)
	if err := store.Set("host.name", "host"); err != nil {
		t.Fatal(err)
	}
	db := make([]byte, 512)
	copy(db, "SQLite format 3\x00")
	db[18] = 1
	db[19] = 1
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/nested/app.db\"\n")},
		restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0750, uname: "ikigenba", gname: "ikigenba"},
		restoreMember{name: "state/nested", typeflag: tar.TypeDir, mode: 0710, uname: "ikigenba", gname: "ikigenba"},
		restoreMember{name: "state/nested/app.db", data: db, mode: 0640, uname: "ikigenba", gname: "ikigenba"},
		restoreMember{name: "state/nested/app.db-wal", data: []byte("sidecar"), mode: 0640, uname: "ikigenba", gname: "ikigenba"},
		restoreMember{name: "state/nested/app.db-shm", data: []byte("sidecar"), mode: 0640, uname: "ikigenba", gname: "ikigenba"},
	)
	from := "s3://bucket/snapshot"
	client := &snapshotRestoreCloud{restoreCloud: &restoreCloud{bodies: map[string][]byte{from: body}}}
	var events []string
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: snapshotRestoreExecute(t, root, &events)}, cloud.Env{Open: client.open}, store, "notes", nil, from, func(context.Context) error { return nil })
	if err != nil || len(report.Steps) != 7 {
		t.Fatalf("Restore %+v %v", report, err)
	}
	uid, gid := os.Getuid(), os.Getgid()
	assertDatabaseWALAndMetadata(t, root, "opt/notes/state/nested/app.db", uid, gid)
	assertRestoreMetadata(t, root, "opt/notes/state", 0750, uid, gid)
	assertRestoreMetadata(t, root, "opt/notes/state/nested", 0710, uid, gid)
	for _, event := range events {
		if strings.HasPrefix(event, "litestream ") {
			t.Fatalf("unexpected replica read %s", event)
		}
	}
}

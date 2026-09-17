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
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestRestoreDatabaseLifecycleUsesIndependentHistoryAndOrdersStarts(t *testing.T) {
	// R-G1CH-5FYL R-G2KD-J7PA R-GERD-CX48 R-ZBT9-WYGY
	root := t.TempDir()
	store := configuredFileStore(t, root)
	uid, gid := os.Getuid(), restoreAlternateGID(t)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/nested/app.db\"\n"), uname: "ikigenba", gname: "ikigenba"},
		restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0o750, uname: "ikigenba", gname: "ikigenba"},
		restoreMember{name: "state/nested", typeflag: tar.TypeDir, mode: 0o710, uname: "ikigenba", gname: "ikigenba"},
		restoreMember{name: "state/value", data: []byte("files at midnight"), uname: "ikigenba", gname: "ikigenba"},
	)
	executor := &databaseRestoreExecutor{
		t: t, root: root, installed: true, active: true, uid: uid, gid: gid,
		ltx:      `[{"timestamp":"2026-09-16T10:00:00Z"},{"timestamp":"2026-09-16T11:00:00Z"}]`,
		sidecars: true,
	}
	nginxCalls := 0
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, func(context.Context) error {
		nginxCalls++
		configuration, readErr := readRootedRestoreFile(root, "etc/litestream.yml")
		if readErr != nil || !strings.Contains(string(configuration), "/opt/notes/state/nested/app.db") {
			t.Fatalf("nginx ran before Litestream regeneration: %q, %v", configuration, readErr)
		}
		assertDatabaseWALAndMetadata(t, root, "opt/notes/state/nested/app.db", uid, gid)
		executor.events = append(executor.events, "nginx")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []backup.RestoreStep{
		{Name: "source", Detail: fmt.Sprintf("notes/2026-09-16T00:00:00Z.tar.zst, %.1f MiB", float64(len(body))/1048576)},
		{Name: "stop", Detail: "ikigenba-notes.service, litestream.service"},
		{Name: "files", Detail: "/opt/notes/etc, /opt/notes/state, 2 files"},
		{Name: "db", Detail: "/opt/notes/state/nested/app.db, newest 2026-09-16T11:00:00Z"},
		{Name: "litestream", Detail: "state/nested/app.db"},
		{Name: "start", Detail: "litestream.service, ikigenba-notes.service"},
	}
	if !reflect.DeepEqual(report.Steps, want) || nginxCalls != 1 {
		t.Fatalf("Restore() = %+v, %v; nginx calls %d", report, err, nginxCalls)
	}
	replica := "s3://bucket/host/notes/"
	destination := filepath.Join(root, "opt/notes/state/nested/app.db")
	wantEvents := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.service",
		"systemctl stop ikigenba-notes.service",
		"systemctl stop litestream.service",
		"getent passwd ikigenba",
		"usermod --home /nonexistent --shell /usr/sbin/nologin ikigenba",
		"litestream ltx -level all -json " + replica,
		"litestream restore -o " + destination + " " + replica,
		"nginx",
		"systemctl start litestream.service",
		"systemctl start ikigenba-notes.service",
	}
	if !reflect.DeepEqual(executor.events, wantEvents) {
		t.Fatalf("events = %v, want %v", executor.events, wantEvents)
	}
	assertRestoreMetadata(t, root, "opt/notes/state", 0o750, uid, gid)
	assertRestoreMetadata(t, root, "opt/notes/state/nested", 0o710, uid, gid)
}

func TestRestoreDatabaseAtRequestsInstantAndReportsRequestedTime(t *testing.T) {
	// R-G1CH-5FYL
	root := t.TempDir()
	store := configuredFileStore(t, root)
	body := databaseRestoreArchive(t, false)
	at := time.Date(2026, 9, 16, 10, 30, 0, 123000000, time.UTC)
	executor := &databaseRestoreExecutor{t: t, root: root, ltx: `[{"timestamp":"2026-09-16T09:00:00Z"},{"timestamp":"2026-09-16T11:00:00Z"}]`}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", &at, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "opt/notes/state/app.db")
	wantRestore := "litestream restore -o " + destination + " -timestamp 2026-09-16T10:30:00.123Z s3://bucket/host/notes/"
	if report.Steps[3].Detail != "/opt/notes/state/app.db, at 2026-09-16T10:30:00.123Z" || !containsString(executor.events, wantRestore) {
		t.Fatalf("report = %+v, events = %v", report, executor.events)
	}
}

func TestRestoreDatabaseAtRejectsHistoryEntirelyAfterCutoff(t *testing.T) {
	// R-G1CH-5FYL
	root := t.TempDir()
	store := configuredFileStore(t, root)
	at := time.Date(2026, 9, 16, 10, 30, 0, 123000000, time.UTC)
	executor := &databaseRestoreExecutor{t: t, root: root, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"},{"timestamp":"2026-09-16T12:00:00Z"}]`}
	body := databaseRestoreArchive(t, false)
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", &at, func(context.Context) error { return nil })
	var restoreErr *backup.RestoreError
	if err == nil || err.Error() != "notes: litestream restore: no snapshot under the prefix" || !errors.As(err, &restoreErr) || restoreErr.Stage != "litestream restore" || restoreErr.Err.Error() != "no snapshot under the prefix" {
		t.Fatalf("Restore() error = %#v", err)
	}
	wantReport := []backup.RestoreStep{
		{Name: "source", Detail: fmt.Sprintf("notes/2026-09-16T00:00:00Z.tar.zst, %.1f MiB", float64(len(body))/1048576)},
		{Name: "stop", Detail: "litestream.service, no ikigenba-notes.service"},
		{Name: "files", Detail: "/opt/notes/etc, /opt/notes/state, 2 files"},
		{Name: "db", Err: errors.New("no snapshot under the prefix")},
	}
	if len(report.Steps) != len(wantReport) || report.Steps[0].Name != wantReport[0].Name || report.Steps[0].Detail != wantReport[0].Detail || !reflect.DeepEqual(report.Steps[1:3], wantReport[1:3]) || report.Steps[3].Name != "db" || report.Steps[3].Detail != "" || report.Steps[3].Err == nil || report.Steps[3].Err.Error() != "no snapshot under the prefix" {
		t.Fatalf("report = %+v", report.Steps)
	}
	wantEvents := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.service",
		"systemctl stop litestream.service",
		"litestream ltx -level all -json s3://bucket/host/notes/",
	}
	if !reflect.DeepEqual(executor.events, wantEvents) {
		t.Fatalf("events = %v, want %v", executor.events, wantEvents)
	}
}

func TestRestoreDatabaseStartsLitestreamWhenConfigurationUnchanged(t *testing.T) {
	// R-G2KD-J7PA
	root := t.TempDir()
	store := configuredFileStore(t, root)
	manifest := "app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n"
	writeFile(t, root, "opt/notes/etc/manifest.toml", manifest, 0o600)
	if changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, store); err != nil || !changed {
		t.Fatalf("seed Regenerate() = %v, %v", changed, err)
	}
	body := hostRestoreArchive(t, restoreMember{name: "etc/manifest.toml", data: []byte(manifest)}, restoreMember{name: "state/value", data: []byte("restored")})
	executor := &databaseRestoreExecutor{t: t, root: root, installed: true, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, func(context.Context) error {
		executor.events = append(executor.events, "nginx")
		return nil
	})
	wantEvents := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.service",
		"systemctl stop litestream.service",
		"getent passwd ikigenba",
		"usermod --home /nonexistent --shell /usr/sbin/nologin ikigenba",
		"litestream ltx -level all -json s3://bucket/host/notes/",
		"litestream restore -o " + filepath.Join(root, "opt/notes/state/app.db") + " s3://bucket/host/notes/",
		"nginx",
		"systemctl start litestream.service",
	}
	if err != nil || report.Steps[4] != (backup.RestoreStep{Name: "litestream", Detail: "unchanged"}) || !reflect.DeepEqual(executor.events, wantEvents) {
		t.Fatalf("Restore() = %+v, %v; events %v", report, err, executor.events)
	}
	if got := readLitestream(t, root); !strings.Contains(got, "/opt/notes/state/app.db") {
		t.Fatalf("unchanged litestream configuration = %q", got)
	}
}

func TestRestoreDatabaseRemovesStaleSidecarsBeforeLitestream(t *testing.T) {
	// R-G1CH-5FYL
	root := t.TempDir()
	store := configuredFileStore(t, root)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
		restoreMember{name: "state/app.db-wal", data: []byte("stale wal")},
		restoreMember{name: "state/app.db-shm", data: []byte("stale shm")},
	)
	executor := &databaseRestoreExecutor{t: t, root: root, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`, requireAbsentSidecars: true}
	if _, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(filepath.Join(root, "opt/notes/state/app.db") + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale database sidecar %s remains after restore: %v", suffix, err)
		}
	}
}

func TestRestoreDatabaseMissingSnapshotStopsAtFailedDatabaseStep(t *testing.T) {
	// R-Y55N-UUDW
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "etc/litestream.yml", "original configuration\n", 0o600)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
		restoreMember{name: "state/value", data: []byte("restored")},
		restoreMember{name: "state/app.db", data: []byte("stale archive database")},
	)
	executor := &databaseRestoreExecutor{t: t, root: root, installed: true, active: true, ltx: `[]`}
	nginxCalls := 0
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, func(context.Context) error {
		nginxCalls++
		return nil
	})
	var restoreErr *backup.RestoreError
	if err == nil || !errors.As(err, &restoreErr) || restoreErr.Service != "notes" || restoreErr.Stage != "litestream restore" || restoreErr.Err.Error() != "no snapshot under the prefix" || !reflect.DeepEqual(restoreErr.Stopped, []string{"ikigenba-notes.service", "litestream.service"}) {
		t.Fatalf("Restore() error = %#v", err)
	}
	if len(report.Steps) != 4 || report.Steps[0].Name != "source" || report.Steps[1].Name != "stop" || report.Steps[2].Name != "files" || report.Steps[3].Name != "db" || report.Steps[3].Detail != "" || report.Steps[3].Err == nil || report.Steps[3].Err.Error() != "no snapshot under the prefix" {
		t.Fatalf("report = %+v", report)
	}
	if _, statErr := os.Stat(filepath.Join(root, "opt/notes/state/app.db")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("database exists after missing snapshot: %v", statErr)
	}
	if got := string(readHostRestoreFile(t, root, "etc/litestream.yml")); got != "original configuration\n" {
		t.Fatalf("litestream configuration = %q", got)
	}
	if nginxCalls != 0 || containsEventFragment(executor.events, "litestream restore") || containsEventFragment(executor.events, "systemctl start") {
		t.Fatalf("later effects: nginx %d, events %v", nginxCalls, executor.events)
	}
	if got := string(readHostRestoreFile(t, root, "opt/notes/state/value")); got != "restored" {
		t.Fatalf("completed files step rolled back: %q", got)
	}
}

func TestRestoreDatabaseRejectsNonSQLiteBeforeRegenerationOrStarts(t *testing.T) {
	// R-GERD-CX48
	root := t.TempDir()
	store := configuredFileStore(t, root)
	executor := &databaseRestoreExecutor{t: t, root: root, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`, invalidDatabase: true}
	nginxCalls := 0
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, databaseRestoreArchive(t, false)).open}, store, "notes", nil, func(context.Context) error { nginxCalls++; return nil })
	var restoreErr *backup.RestoreError
	if err == nil || !errors.As(err, &restoreErr) || restoreErr.Stage != "wal mode" || len(report.Steps) != 4 || report.Steps[3].Name != "db" || report.Steps[3].Err == nil || nginxCalls != 0 || containsEventFragment(executor.events, "systemctl start") {
		t.Fatalf("Restore() = %+v, %#v; nginx %d events %v", report, err, nginxCalls, executor.events)
	}
}

func TestRestoreDatabaseOwnershipFailurePreventsLaterLifecycle(t *testing.T) {
	// R-ZBT9-WYGY
	root := t.TempDir()
	store := configuredFileStore(t, root)
	executor := &databaseRestoreExecutor{t: t, root: root, uid: os.Getuid(), gid: os.Getgid(), ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`, invalidSidecar: true}
	nginxCalls := 0
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, databaseRestoreArchive(t, true)).open}, store, "notes", nil, func(context.Context) error { nginxCalls++; return nil })
	var restoreErr *backup.RestoreError
	if err == nil || !errors.As(err, &restoreErr) || restoreErr.Stage != "database ownership" || len(report.Steps) != 4 || report.Steps[3].Name != "db" || report.Steps[3].Err == nil || nginxCalls != 0 || containsEventFragment(executor.events, "systemctl start") {
		t.Fatalf("Restore() = %+v, %#v; nginx %d events %v", report, err, nginxCalls, executor.events)
	}
}

func TestRestoreDatabaseOwnershipUsesSymbolicArchiveMemberWithoutManifestApp(t *testing.T) {
	// R-ZBT9-WYGY
	root := t.TempDir()
	store := configuredFileStore(t, root)
	uid, gid := os.Getuid(), restoreAlternateGID(t)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
		restoreMember{name: "state/value", data: []byte("owned source member"), uid: uid + 100000, gid: gid + 100000, uname: "ikigenba", gname: "ikigenba"},
	)
	executor := &databaseRestoreExecutor{t: t, root: root, uid: uid, gid: gid, sidecars: true, ltx: `[{"timestamp":"2026-09-16T11:00:00Z"}]`}
	if _, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !containsString(executor.events, "getent passwd ikigenba") {
		t.Fatalf("destination account was not resolved: %v", executor.events)
	}
	assertDatabaseWALAndMetadata(t, root, "opt/notes/state/app.db", uid, gid)
}

type databaseRestoreExecutor struct {
	t                     *testing.T
	root                  string
	installed             bool
	active                bool
	uid                   int
	gid                   int
	ltx                   string
	sidecars              bool
	invalidDatabase       bool
	invalidSidecar        bool
	requireAbsentSidecars bool
	events                []string
}

func (executor *databaseRestoreExecutor) execute(_ context.Context, command host.Command) (host.Result, error) {
	text := strings.Join(append([]string{command.Name}, command.Args...), " ")
	executor.events = append(executor.events, text)
	switch command.Name {
	case "zstd":
		compressed, err := io.ReadAll(command.Stdin)
		if err != nil {
			return host.Result{}, err
		}
		return host.Result{Stdout: decodeRawZstandardFrame(executor.t, compressed)}, nil
	case "systemctl":
		if len(command.Args) == 4 && command.Args[0] == "show" {
			load, active := "not-found", "inactive"
			if executor.installed {
				load = "loaded"
			}
			if executor.active {
				active = "active"
			}
			return host.Result{Stdout: []byte("LoadState=" + load + "\nActiveState=" + active + "\n")}, nil
		}
		if len(command.Args) == 2 && (command.Args[0] == "stop" || command.Args[0] == "start") {
			return host.Result{}, nil
		}
	case "getent":
		uid, gid := executor.uid, executor.gid
		if uid == 0 {
			uid = os.Getuid()
		}
		if gid == 0 {
			gid = os.Getgid()
		}
		return host.Result{Stdout: []byte(fmt.Sprintf("ikigenba:x:%d:%d::/nonexistent:/usr/sbin/nologin\n", uid, gid))}, nil
	case "usermod":
		return host.Result{}, nil
	case "litestream":
		if len(command.Args) == 5 && command.Args[0] == "ltx" {
			return host.Result{Stdout: []byte(executor.ltx)}, nil
		}
		if len(command.Args) >= 4 && command.Args[0] == "restore" && command.Args[1] == "-o" {
			destination := command.Args[2]
			if executor.requireAbsentSidecars {
				for _, suffix := range []string{"-wal", "-shm"} {
					if _, err := os.Lstat(destination + suffix); !errors.Is(err, os.ErrNotExist) {
						executor.t.Fatalf("stale database sidecar %s present before Litestream restore: %v", suffix, err)
					}
				}
			}
			if executor.invalidDatabase {
				if err := os.WriteFile(destination, []byte("not sqlite"), 0o600); err != nil {
					return host.Result{}, err
				}
				return host.Result{}, nil
			}
			writeRestoreSQLite(executor.t, destination)
			if executor.sidecars {
				for _, suffix := range []string{"-wal", "-shm"} {
					if err := os.WriteFile(destination+suffix, []byte("sidecar"), 0o600); err != nil {
						return host.Result{}, err
					}
					if err := syscall.Chmod(destination+suffix, 0o644); err != nil {
						return host.Result{}, err
					}
				}
			}
			if executor.invalidSidecar {
				if err := os.Mkdir(destination+"-wal", 0o750); err != nil {
					return host.Result{}, err
				}
			}
			return host.Result{}, nil
		}
	}
	return host.Result{}, fmt.Errorf("unexpected command %q", text)
}

func databaseRestoreArchive(t *testing.T, app bool) []byte {
	t.Helper()
	appLine := ""
	if app {
		appLine = "app = \"notes\"\n"
	}
	return hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte(appLine + "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
		restoreMember{name: "state/value", data: []byte("restored")},
	)
}

func assertDatabaseWALAndMetadata(t *testing.T, root, name string, uid, gid int) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		full := filepath.Join(root, name+suffix)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if info.Mode().Perm() != 0o600 || int(stat.Uid) != uid || int(stat.Gid) != gid {
			t.Fatalf("%s metadata = %o %d:%d", name+suffix, info.Mode().Perm(), stat.Uid, stat.Gid)
		}
	}
	data, err := readRootedRestoreFile(root, name)
	if err != nil || len(data) < 20 || data[18] != 2 || data[19] != 2 {
		t.Fatalf("database WAL header = %v, %v", data, err)
	}
}

func readRootedRestoreFile(rootName, name string) ([]byte, error) {
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(name)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsEventFragment(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

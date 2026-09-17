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

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestRestoreStopsOwnersAndReplacesCompleteTrees(t *testing.T) {
	// R-DVW3-8CRI R-G04K-RO7W R-RY91-HA14
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/notes/etc/stale", "remove", 0o600)
	writeFile(t, root, "opt/notes/state/stale", "remove", 0o600)
	writeFile(t, root, "opt/notes/bin/app", "keep-bin", 0o700)
	writeFile(t, root, "opt/notes/share/asset", "keep-share", 0o600)
	writeFile(t, root, "opt/notes/cache/value", "keep-cache", 0o600)

	uid, gid := os.Getuid(), os.Getgid()
	accountGID := restoreAlternateGID(t)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc", typeflag: tar.TypeDir, mode: 0o750, uid: uid, gid: gid},
		restoreMember{name: "etc/env", data: []byte("TOKEN=restored\n"), mode: 0o640, uid: 4242, gid: 4343, uname: "ikigenba", gname: "ikigenba"},
		restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n"), mode: 0o600, uid: uid, gid: gid},
		restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0o710, uid: uid, gid: gid},
		restoreMember{name: "state/data", data: []byte("restored"), mode: 0o620, uid: uid, gid: gid},
		restoreMember{name: "state/current", typeflag: tar.TypeSymlink, linkname: "data", uid: 4242, gid: 4343, uname: "ikigenba", gname: "ikigenba"},
	)
	client := restoreClientFor(t, body)
	executor := &restoreStageExecutor{t: t, root: root, installed: true, active: true, accountUID: uid, accountGID: accountGID}

	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := []backup.RestoreStep{
		{Name: "source", Detail: fmt.Sprintf("notes/2026-09-16T00:00:00Z.tar.zst, %.1f MiB", float64(len(body))/1048576)},
		{Name: "stop", Detail: "ikigenba-notes.service, litestream.service"},
		{Name: "files", Detail: "/opt/notes/etc, /opt/notes/state, 4 files"},
		{Name: "db", Detail: "/opt/notes/state/app.db, newest 2026-09-16T11:00:00Z"},
		{Name: "litestream", Detail: "state/app.db"},
		{Name: "start", Detail: "litestream.service, ikigenba-notes.service"},
	}
	if !reflect.DeepEqual(report.Steps, want) {
		t.Fatalf("report = %+v, want %+v", report.Steps, want)
	}
	wantCommands := []string{
		"zstd --quiet --decompress --stdout",
		"systemctl show --property=LoadState --property=ActiveState ikigenba-notes.service",
		"systemctl stop ikigenba-notes.service",
		"systemctl stop litestream.service",
		"getent passwd ikigenba",
		"usermod --home /nonexistent --shell /usr/sbin/nologin ikigenba",
		"litestream ltx -level all -json s3://bucket/host/notes/",
		"litestream restore -o " + filepath.Join(root, "opt/notes/state/app.db") + " s3://bucket/host/notes/",
		"systemctl start litestream.service",
		"systemctl start ikigenba-notes.service",
	}
	if !reflect.DeepEqual(executor.commands, wantCommands) {
		t.Fatalf("commands = %v, want %v", executor.commands, wantCommands)
	}
	for _, name := range []string{"opt/notes/etc/stale", "opt/notes/state/stale"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale %s remains: %v", name, err)
		}
	}
	for name, wantData := range map[string]string{
		"opt/notes/etc/env": "TOKEN=restored\n", "opt/notes/state/data": "restored",
		"opt/notes/bin/app": "keep-bin", "opt/notes/share/asset": "keep-share", "opt/notes/cache/value": "keep-cache",
	} {
		data := readHostRestoreFile(t, root, name)
		if string(data) != wantData {
			t.Fatalf("%s = %q", name, data)
		}
	}
	assertRestoreMetadata(t, root, "opt/notes/etc", 0o750, uid, gid)
	assertRestoreMetadata(t, root, "opt/notes/etc/env", 0o640, uid, accountGID)
	assertRestoreMetadata(t, root, "opt/notes/state", 0o710, uid, gid)
	assertRestoreMetadata(t, root, "opt/notes/state/data", 0o620, uid, gid)
	linkInfo, err := os.Lstat(filepath.Join(root, "opt/notes/state/current"))
	if err != nil || linkInfo.Mode()&os.ModeSymlink == 0 || int(linkInfo.Sys().(*syscall.Stat_t).Uid) != uid || int(linkInfo.Sys().(*syscall.Stat_t).Gid) != accountGID {
		t.Fatalf("restored symlink metadata = %v, %v", linkInfo, err)
	}
	if target, err := os.Readlink(filepath.Join(root, "opt/notes/state/current")); err != nil || target != "data" {
		t.Fatalf("symlink target = %q, %v", target, err)
	}
	if _, err := os.Stat(filepath.Join(root, "run/opsctl/restore/notes.active")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful restore retained activation marker: %v", err)
	}
}

func TestRestoreAppGuardAndStopDetails(t *testing.T) {
	// R-DVW3-8CRI R-1PXK-YLDY
	for _, test := range []struct {
		name      string
		service   string
		installed bool
		active    bool
		wantStop  string
	}{
		{name: "inactive", service: "notes", installed: true, wantStop: "litestream.service, ikigenba-notes.service already inactive"},
		{name: "absent", service: "notes", wantStop: "litestream.service, no ikigenba-notes.service"},
		{name: "reserved data only", service: "backup-host", wantStop: "litestream.service, no app unit"},
		{name: "reserved service timer", service: "backup-services", wantStop: "litestream.service, no app unit"},
		{name: "reserved renewal", service: "renew-certificate", wantStop: "litestream.service, no app unit"},
		{name: "non-reserved invalid app name", service: "bad_name", wantStop: "litestream.service, no app unit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			body := hostRestoreArchive(t,
				restoreMember{name: "etc/manifest.toml", data: []byte("[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
				restoreMember{name: "state/value", data: []byte("restored")},
			)
			client := restoreClientForService(t, body, test.service)
			executor := &restoreStageExecutor{t: t, root: root, installed: test.installed, active: test.active}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, test.service, nil, func(context.Context) error { return nil })
			if err != nil || len(report.Steps) != 6 || report.Steps[1].Detail != test.wantStop {
				t.Fatalf("Restore() = %+v, %v, want stop %q", report, err, test.wantStop)
			}
			joined := strings.Join(executor.commands, "\n")
			if test.wantStop == "litestream.service, no app unit" {
				if strings.Contains(joined, "ikigenba-"+test.service+".service") {
					t.Fatalf("guarded name selected an app unit: %v", executor.commands)
				}
			}
			if !strings.Contains(joined, "systemctl stop litestream.service") {
				t.Fatalf("incoming database did not stop litestream: %v", executor.commands)
			}
		})
	}
}

func TestRestoreStopFailuresPreserveCauseStoppedUnitsAndTargets(t *testing.T) {
	// R-DVW3-8CRI R-G7FZ-2AO2 R-RX15-3IAF
	transport := errors.New("system bus unavailable")
	for _, test := range []struct {
		name         string
		failCommand  string
		failResult   host.Result
		wantStage    string
		wantStopped  []string
		wantCommands []string
	}{
		{
			name: "unit inspection", failCommand: "systemctl show --property=LoadState --property=ActiveState ikigenba-notes.service",
			failResult: host.Result{Stdout: []byte("partial state\n")}, wantStage: "unit inspection",
			wantCommands: []string{"zstd --quiet --decompress --stdout", "systemctl show --property=LoadState --property=ActiveState ikigenba-notes.service"},
		},
		{
			name: "active app stop", failCommand: "systemctl stop ikigenba-notes.service",
			failResult: host.Result{Stderr: []byte("app refused stop\n")}, wantStage: "stop",
			wantCommands: []string{"zstd --quiet --decompress --stdout", "systemctl show --property=LoadState --property=ActiveState ikigenba-notes.service", "systemctl stop ikigenba-notes.service"},
		},
		{
			name: "litestream stop", failCommand: "systemctl stop litestream.service",
			failResult: host.Result{Stderr: []byte("replicator refused stop\n")}, wantStage: "stop",
			wantStopped:  []string{"ikigenba-notes.service"},
			wantCommands: []string{"zstd --quiet --decompress --stdout", "systemctl show --property=LoadState --property=ActiveState ikigenba-notes.service", "systemctl stop ikigenba-notes.service", "systemctl stop litestream.service"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			writeFile(t, root, "opt/notes/etc/old", "old etc", 0o600)
			writeFile(t, root, "opt/notes/state/old", "old state", 0o600)
			body := hostRestoreArchive(t,
				restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n")},
				restoreMember{name: "state/new", data: []byte("new")},
			)
			client := restoreClientFor(t, body)
			executor := &restoreStageExecutor{
				t: t, root: root, installed: true, active: true,
				failCommand: test.failCommand, failResult: test.failResult, failErr: transport,
			}
			downstreamCalls := 0
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error {
				downstreamCalls++
				return nil
			})
			var restoreErr *backup.RestoreError
			var commandErr *host.CommandError
			if err == nil || !errors.As(err, &restoreErr) || !errors.As(err, &commandErr) || !errors.Is(err, transport) {
				t.Fatalf("Restore() error = %T %v, want RestoreError wrapping host.CommandError and cause", err, err)
			}
			if restoreErr.Stage != test.wantStage || !reflect.DeepEqual(restoreErr.Stopped, test.wantStopped) {
				t.Fatalf("RestoreError = %#v, want stage %q stopped %v", restoreErr, test.wantStage, test.wantStopped)
			}
			if len(report.Steps) != 2 || report.Steps[0].Name != "source" || report.Steps[1].Name != "stop" || report.Steps[1].Err == nil || report.Steps[1].Detail != "" {
				t.Fatalf("report = %+v, want completed source and one failed stop", report)
			}
			if !reflect.DeepEqual(executor.commands, test.wantCommands) || downstreamCalls != 0 {
				t.Fatalf("commands = %v, downstream calls %d", executor.commands, downstreamCalls)
			}
			for name, want := range map[string]string{"opt/notes/etc/old": "old etc", "opt/notes/state/old": "old state"} {
				if got := string(readHostRestoreFile(t, root, name)); got != want {
					t.Fatalf("%s = %q, want preserved %q", name, got, want)
				}
			}
			if _, statErr := os.Stat(filepath.Join(root, "opt/notes/state/new")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failed stop published restored target: %v", statErr)
			}
		})
	}
}

func TestRestoreCreatesAccountBeforePublishingAndRetainsMarkerOnFailure(t *testing.T) {
	// R-RY91-HA14
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/notes/state/existing", "unchanged", 0o600)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\n")},
		restoreMember{name: "state/value", data: []byte("new"), uname: "ikigenba"},
	)
	client := restoreClientFor(t, body)
	executor := &restoreStageExecutor{t: t, root: root, installed: true, active: true, accountLookupErr: errors.New("identity backend unavailable")}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error { return nil })
	var restoreErr *backup.RestoreError
	if err == nil || !errors.As(err, &restoreErr) || restoreErr.Stage != "ownership" || len(report.Steps) != 3 || report.Steps[2].Name != "files" || report.Steps[2].Err == nil {
		t.Fatalf("Restore() = %+v, %#v", report, err)
	}
	if data := readHostRestoreFile(t, root, "opt/notes/state/existing"); string(data) != "unchanged" {
		t.Fatalf("ownership failure replaced target: %q", data)
	}
	if _, statErr := os.Stat(filepath.Join(root, "opt/notes/state/value")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("ownership failure published new tree: %v", statErr)
	}
	assertRestoreMarker(t, root)
}

func TestRestoreCreatesMissingAccountWithNoLoginAndNoHome(t *testing.T) {
	// R-RY91-HA14
	root := t.TempDir()
	store := configuredFileStore(t, root)
	body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("new"), uname: "ikigenba", gname: "ikigenba"})
	client := restoreClientFor(t, body)
	accountGID := restoreAlternateGID(t)
	executor := &restoreStageExecutor{t: t, root: root, accountMissing: true, accountUID: os.Getuid(), accountGID: accountGID}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error { return nil })
	if err != nil || len(report.Steps) != 4 {
		t.Fatalf("Restore() = %+v, %v", report, err)
	}
	joined := strings.Join(executor.commands, "\n")
	for _, want := range []string{
		"useradd --system --no-create-home --shell /usr/sbin/nologin ikigenba",
		"usermod --home /nonexistent --shell /usr/sbin/nologin ikigenba",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("commands %v omitted %q", executor.commands, want)
		}
	}
	assertRestoreMetadata(t, root, "opt/notes/state/value", 0o600, os.Getuid(), accountGID)
	if info, statErr := os.Stat(filepath.Join(root, "opt/notes")); statErr != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("new service root = %v, %v, want mode 0755", info, statErr)
	}
}

func TestRestoreOwnershipApplicationFailurePreservesPublishedTrees(t *testing.T) {
	// R-RY91-HA14 R-G04K-RO7W R-G7FZ-2AO2 R-RX15-3IAF
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/notes/etc/old", "old etc", 0o600)
	writeFile(t, root, "opt/notes/state/old", "old state", 0o600)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/new", data: []byte("new etc"), uid: os.Getuid(), gid: os.Getgid()},
		restoreMember{name: "state/new", data: []byte("new state"), uid: os.Getuid() + 100000, gid: os.Getgid()},
	)
	client := restoreClientFor(t, body)
	executor := &restoreStageExecutor{t: t, root: root}
	downstreamCalls := 0
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error {
		downstreamCalls++
		return nil
	})
	var restoreErr *backup.RestoreError
	if err == nil || !errors.As(err, &restoreErr) || restoreErr.Stage != "files" || len(report.Steps) != 3 || report.Steps[2].Name != "files" || report.Steps[2].Err == nil || report.Steps[2].Detail != "" {
		t.Fatalf("Restore() = %+v, %#v; want failed files ownership application", report, err)
	}
	if !strings.Contains(err.Error(), "set ownership") || downstreamCalls != 0 {
		t.Fatalf("error = %v, downstream calls = %d", err, downstreamCalls)
	}
	for name, want := range map[string]string{"opt/notes/etc/old": "old etc", "opt/notes/state/old": "old state"} {
		if got := string(readHostRestoreFile(t, root, name)); got != want {
			t.Fatalf("%s = %q, want preserved %q", name, got, want)
		}
	}
	for _, name := range []string{"opt/notes/etc/new", "opt/notes/state/new"} {
		if _, statErr := os.Stat(filepath.Join(root, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("staging failure published %s: %v", name, statErr)
		}
	}
}

func TestRestoreManifestAppAloneTriggersAccountPreparation(t *testing.T) {
	// R-RY91-HA14
	root := t.TempDir()
	store := configuredFileStore(t, root)
	body := hostRestoreArchive(t,
		restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\n"), uid: os.Getuid(), gid: os.Getgid()},
		restoreMember{name: "state/value", data: []byte("numeric ownership"), uid: os.Getuid(), gid: os.Getgid()},
	)
	client := restoreClientFor(t, body)
	executor := &restoreStageExecutor{t: t, root: root, accountUID: os.Getuid(), accountGID: os.Getgid()}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, func(context.Context) error { return nil })
	if err != nil || len(report.Steps) != 4 {
		t.Fatalf("Restore() = %+v, %v", report, err)
	}
	joined := strings.Join(executor.commands, "\n")
	if !strings.Contains(joined, "getent passwd ikigenba") || !strings.Contains(joined, "usermod --home /nonexistent --shell /usr/sbin/nologin ikigenba") {
		t.Fatalf("manifest app did not trigger account preparation: %v", executor.commands)
	}
}

type restoreStageExecutor struct {
	t                *testing.T
	root             string
	installed        bool
	active           bool
	accountMissing   bool
	accountCreated   bool
	accountLookupErr error
	accountUID       int
	accountGID       int
	failCommand      string
	failResult       host.Result
	failErr          error
	commands         []string
}

func (executor *restoreStageExecutor) execute(_ context.Context, command host.Command) (host.Result, error) {
	text := strings.Join(append([]string{command.Name}, command.Args...), " ")
	executor.commands = append(executor.commands, text)
	if text == executor.failCommand {
		return executor.failResult, executor.failErr
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
			load, active := "not-found", "inactive"
			if executor.installed {
				load = "loaded"
			}
			if executor.active {
				active = "active"
			}
			return host.Result{Stdout: []byte("LoadState=" + load + "\nActiveState=" + active + "\n")}, nil
		}
		if len(command.Args) == 2 && command.Args[0] == "stop" {
			if _, err := os.Stat(filepath.Join(executor.root, "opt/notes/state/value")); !errors.Is(err, os.ErrNotExist) {
				executor.t.Fatalf("target was replaced before %q: %v", text, err)
			}
			if command.Args[1] == "ikigenba-notes.service" {
				if info, err := os.Stat(filepath.Join(executor.root, "run/opsctl/restore/notes.active")); err != nil || info.Mode().Perm() != 0o600 {
					executor.t.Fatalf("activation marker was not published before app stop: %v, %v", info, err)
				}
			}
			return host.Result{}, nil
		}
		if len(command.Args) == 2 && command.Args[0] == "start" {
			return host.Result{}, nil
		}
	case "litestream":
		if len(command.Args) == 5 && command.Args[0] == "ltx" {
			return host.Result{Stdout: []byte(`[{"timestamp":"2026-09-16T10:00:00Z"},{"timestamp":"2026-09-16T11:00:00Z"}]`)}, nil
		}
		if len(command.Args) >= 4 && command.Args[0] == "restore" && command.Args[1] == "-o" {
			writeRestoreSQLite(executor.t, command.Args[2])
			return host.Result{}, nil
		}
	case "getent":
		if executor.accountLookupErr != nil {
			return host.Result{}, executor.accountLookupErr
		}
		if executor.accountMissing && !executor.accountCreated {
			return host.Result{ExitCode: 2}, nil
		}
		uid, gid := executor.accountUID, executor.accountGID
		if uid == 0 {
			uid = os.Getuid()
		}
		if gid == 0 {
			gid = os.Getgid()
		}
		return host.Result{Stdout: []byte(fmt.Sprintf("ikigenba:x:%d:%d::/nonexistent:/usr/sbin/nologin\n", uid, gid))}, nil
	case "useradd":
		executor.accountCreated = true
		return host.Result{}, nil
	case "usermod":
		return host.Result{}, nil
	}
	return host.Result{}, fmt.Errorf("unexpected command %q", text)
}

func writeRestoreSQLite(t *testing.T, name string) {
	t.Helper()
	data := make([]byte, 512)
	copy(data, []byte("SQLite format 3\x00"))
	data[18], data[19] = 1, 1
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Chmod(name, 0o644); err != nil {
		t.Fatal(err)
	}
}

func restoreClientFor(t *testing.T, body []byte) *restoreCloud {
	return restoreClientForService(t, body, "notes")
}

func restoreClientForService(t *testing.T, body []byte, service string) *restoreCloud {
	t.Helper()
	uri := "s3://bucket/host/" + service + "/2026-09-16T00:00:00Z.tar.zst"
	return &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
}

func assertRestoreMetadata(t *testing.T, root, name string, mode os.FileMode, uid, gid int) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if info.Mode().Perm() != mode || int(stat.Uid) != uid || int(stat.Gid) != gid {
		t.Fatalf("%s metadata = %o %d:%d, want %o %d:%d", name, info.Mode().Perm(), stat.Uid, stat.Gid, mode, uid, gid)
	}
}

func assertRestoreMarker(t *testing.T, root string) {
	t.Helper()
	for name, mode := range map[string]os.FileMode{
		"run/opsctl": 0o700, "run/opsctl/restore": 0o700, "run/opsctl/restore/notes.active": 0o600,
	} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("marker path %s = %v, %v, want mode %o", name, info, err, mode)
		}
	}
}

func restoreAlternateGID(t *testing.T) int {
	t.Helper()
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group != os.Getgid() {
			return group
		}
	}
	t.Fatal("test requires a supplementary group to prove ownership mapping")
	return 0
}

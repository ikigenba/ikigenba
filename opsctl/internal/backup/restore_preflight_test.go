package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestRestoreInstalledIdentityAndFaultPrecedeMigrationAndCloud(t *testing.T) {
	// R-20O1-EI5C R-FMVM-I2DD
	for _, tc := range []struct {
		name, manifest       string
		binary, manifestFile bool
		fault                bool
	}{
		{name: "never installed"},
		{name: "only data remains", manifest: "app = 'notes'\n", manifestFile: true},
		{name: "binary alone", binary: true},
		{name: "absent app identity", binary: true, manifestFile: true, manifest: "[env]\nPLAIN='setting'\n"},
		{name: "nonstring app", binary: true, manifestFile: true, manifest: "app=7\n"},
		{name: "disown despite schema fault", binary: true, manifestFile: true, manifest: "app='other'\n[resources]\nio_weight=50\n"},
		{name: "matching manifest fault", binary: true, manifestFile: true, manifest: "app='notes'\n[resources]\nio_weight=50\n", fault: true},
		{name: "syntax manifest fault", binary: true, manifestFile: true, manifest: "app=\n", fault: true},
	} {
		for _, from := range []string{"", "s3://elsewhere/snapshot"} {
			t.Run(tc.name+from, func(t *testing.T) {
				root := t.TempDir()
				store := restoreConfiguredStore(t, root)
				if tc.binary {
					writeFile(t, root, "opt/notes/bin/notes", "binary", 0700)
				}
				if tc.manifestFile {
					writeFile(t, root, "opt/notes/etc/manifest.toml", tc.manifest, 0600)
				}
				writeFile(t, root, "opt/notes/state/stale", "unmoved", 0600)
				writeFile(t, root, "var/opt/ikigenba/notes/state/keep", "data", 0600)
				writeFile(t, root, "etc/litestream.yml", "configuration", 0600)
				before := fileTreeSnapshot(t, root)
				used := false
				env := host.Env{Root: root, Now: func() time.Time { return time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC) }, Execute: func(context.Context, host.Command) (host.Result, error) {
					used = true
					return host.Result{}, errors.New("unexpected process")
				}}
				report, err := backup.Restore(context.Background(), env, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
					used = true
					return nil, errors.New("unexpected cloud")
				}}, store, "notes", nil, from, func(context.Context) error { used = true; return nil })
				want := "notes is not installed; install notes first"
				if tc.fault {
					_, fault := apps.ParseManifest([]byte(tc.manifest))
					if fault == nil {
						t.Fatal("fixture has no manifest fault")
					}
					want = "notes: etc/manifest.toml: " + fault.Error()
				}
				var failure *backup.RestoreError
				if !errors.As(err, &failure) || failure.Stage != "source" || len(failure.Stopped) != 0 || len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Detail != "" || report.Steps[0].Err == nil || report.Steps[0].Err.Error() != want || used || !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
					t.Fatalf("preflight: %+v %v used=%v", report, err, used)
				}
			})
		}
	}
}

func TestRestoreMissingMigratedEnvironmentEntryIsInert(t *testing.T) {
	// R-233U-61MQ
	for _, legacyEnv := range []bool{false, true} {
		for _, from := range []string{"", "s3://elsewhere/snapshot"} {
			t.Run(from+map[bool]string{false: "absent old env", true: "old env"}[legacyEnv], func(t *testing.T) {
				root := t.TempDir()
				store := restoreConfiguredStore(t, root)
				writeFile(t, root, "opt/notes/bin/notes", "binary", 0700)
				writeFile(t, root, "opt/notes/etc/manifest.toml", "app='notes'\n", 0600)
				if legacyEnv {
					writeFile(t, root, "opt/notes/etc/env", "old", 0600)
				}
				before := fileTreeSnapshot(t, root)
				used := false
				report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) { used = true; return host.Result{}, nil }}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { used = true; return nil, nil }}, store, "notes", nil, from, func(context.Context) error { used = true; return nil })
				var failure *backup.RestoreError
				if !errors.As(err, &failure) || failure.Stage != "source" || len(failure.Stopped) != 0 || len(report.Steps) != 1 || report.Steps[0].Err == nil || report.Steps[0].Err.Error() != "/opt/notes/etc/env has not moved; install notes first" || used || !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
					t.Fatalf("environment preflight: %+v %v used=%v", report, err, used)
				}
			})
		}
	}
}

func TestRestoreMigratedEnvSymlinkAndOldCacheAreAcceptedWithoutFollowing(t *testing.T) {
	// R-233U-61MQ R-21VX-S9W1 R-FQJB-NDLG
	root := t.TempDir()
	restoreInstalled(t, root, false)
	store := restoreConfiguredStore(t, root)
	envName := filepath.Join(root, "etc/opt/ikigenba/notes/env")
	if err := os.Remove(envName); err != nil {
		t.Fatal(err)
	}
	outsideDirectory := t.TempDir()
	outside := filepath.Join(outsideDirectory, "untouched")
	outsideRoot, err := os.OpenRoot(outsideDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outsideRoot.Close() }()
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, envName); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "opt/notes/cache/keep", "cache", 0600)
	writeFile(t, root, "opt/notes/etc/env", "legacy env", 0600)
	body := hostRestoreArchive(t, restoreMember{name: "etc/manifest.toml", data: []byte("app=\n")}, restoreMember{name: "etc/env", data: []byte("BAD=archive")}, restoreMember{name: "state/link", typeflag: tar.TypeSymlink, linkname: outside})
	report, err := backup.Restore(context.Background(), restoreServiceHostEnv(t, root), cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", func(context.Context) error { return nil })
	if err != nil || len(report.Steps) != 5 || report.Steps[3].Detail != "/etc/opt/ikigenba/notes/env, /var/opt/ikigenba/notes/state, 1 files" {
		t.Fatalf("Restore: %+v %v", report, err)
	}
	for name, want := range map[string]string{"opt/notes/cache/keep": "cache", "opt/notes/etc/env": "legacy env"} {
		if got := string(readHostRestoreFile(t, root, name)); got != want {
			t.Fatalf("%s changed: %q", name, got)
		}
	}
	data, err := outsideRoot.ReadFile("untouched")
	if err != nil || string(data) != "outside" {
		t.Fatalf("outside changed %q %v", data, err)
	}
	if target, err := os.Readlink(filepath.Join(root, "var/opt/ikigenba/notes/state/link")); err != nil || target != outside {
		t.Fatalf("link: %q %v", target, err)
	}
}

func TestRestoreEnvironmentPublicationReplacesOnlySelectedFileWithoutChown(t *testing.T) {
	// R-GG57-OK61 R-FU70-SOTJ R-GDPE-X0ON R-GIL0-G3NF
	root := t.TempDir()
	restoreInstalled(t, root, false)
	store := restoreConfiguredStore(t, root)
	writeFile(t, root, "opt/notes/etc/manifest.toml", "app='notes'\nsecrets=['TOKEN','TOKEN']\n[env]\nPLAIN='hello world'\n", 0600)
	writeFile(t, root, "etc/opt/ikigenba/notes/keep", "other config", 0640)
	writeFile(t, root, "etc/opt/ikigenba/other/env", "other app", 0600)
	if err := store.Set("apps.drain_seconds", "7"); err != nil {
		t.Fatal(err)
	}
	beforeOpt := fileTreeSnapshot(t, filepath.Join(root, "opt"))
	body := hostRestoreArchive(t, restoreMember{name: "etc/env", data: []byte("TOKEN=archived")}, restoreMember{name: "state/value", data: []byte("state")})
	client := &snapshotRestoreCloud{restoreCloud: restoreClientFor(t, body), secrets: map[string]string{"TOKEN": "secret value", "UNREQUESTED": "hidden"}}
	executor := &restoreStageExecutor{t: t, root: root, installed: true, active: true}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, step := range report.Steps {
		names = append(names, step.Name)
	}
	if !reflect.DeepEqual(names, []string{"source", "secrets", "stop", "files", "start"}) || report.Steps[1].Detail != "1 keys" {
		t.Fatalf("report %+v", report)
	}
	want := "DRAIN_SECONDS=7\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\nPLAIN=\"hello world\"\nTOKEN=\"secret value\"\n"
	data := string(readHostRestoreFile(t, root, "etc/opt/ikigenba/notes/env"))
	gotEntries, wantEntries := strings.Split(data, "\n"), strings.Split(want, "\n")
	sort.Strings(gotEntries)
	sort.Strings(wantEntries)
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Fatalf("environment=%q want entries %q", data, want)
	}
	info, err := os.Lstat(filepath.Join(root, "etc/opt/ikigenba/notes/env"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("env mode %v %v", info, err)
	}
	for _, command := range executor.commands {
		if strings.HasPrefix(command, "chown ") && strings.Contains(command, filepath.Join(root, "etc/opt")) {
			t.Fatalf("environment ownership command %q", command)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "etc/opt/ikigenba/notes"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("publication leftovers %v %v", entries, err)
	}
	if !reflect.DeepEqual(beforeOpt, fileTreeSnapshot(t, filepath.Join(root, "opt"))) {
		t.Fatal("restore changed deployment files")
	}
	for name, want := range map[string]string{"etc/opt/ikigenba/notes/keep": "other config", "etc/opt/ikigenba/other/env": "other app"} {
		if got := string(readHostRestoreFile(t, root, name)); got != want {
			t.Fatalf("changed %s: %q", name, got)
		}
	}
	if strings.Contains(fmt.Sprint(report), "secret value") || strings.Contains(fmt.Sprint(report), "hidden") {
		t.Fatal("report revealed parameter values")
	}
}

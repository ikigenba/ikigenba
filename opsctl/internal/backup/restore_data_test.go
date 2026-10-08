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
)

func TestRestoreRefusesEveryLegacyStateEntryBeforeSecretsOrStops(t *testing.T) {
	// R-DDQQ-Z0P6
	for _, kind := range []string{"directory", "file", "dangling symlink"} {
		for _, snapshot := range []bool{false, true} {
			for _, dataExists := range []bool{false, true} {
				t.Run(kind+map[bool]string{false: " backup", true: " snapshot"}[snapshot]+map[bool]string{false: " no data", true: " data"}[dataExists], func(t *testing.T) {
					root := t.TempDir()
					store := configuredFileStore(t, root)
					if err := store.Set("host.name", "host.example.test"); err != nil {
						t.Fatal(err)
					}
					writeFile(t, root, "opt/notes/etc/existing", "unchanged", 0600)
					legacy := filepath.Join(root, "opt/notes/state")
					switch kind {
					case "directory":
						if err := os.Mkdir(legacy, 0700); err != nil {
							t.Fatal(err)
						}
					case "file":
						if err := os.WriteFile(legacy, []byte("unchanged"), 0600); err != nil {
							t.Fatal(err)
						}
					case "dangling symlink":
						if err := os.Symlink("missing", legacy); err != nil {
							t.Fatal(err)
						}
					}
					if dataExists {
						writeFile(t, root, "var/opt/ikigenba/notes/state/existing", "data unchanged", 0600)
					}
					body := hostRestoreArchive(t, restoreMember{name: "etc/manifest.toml", data: []byte("app = \"notes\"\nsecrets = [\"TOKEN\"]\n")}, restoreMember{name: "state/value", data: []byte("new")})
					from := ""
					base := restoreClientFor(t, body)
					if snapshot {
						from = "s3://bucket/snapshot"
						base.bodies[from] = body
					}
					client := &snapshotRestoreCloud{restoreCloud: base, secrets: map[string]string{"TOKEN": "never read"}}
					executor := &restoreStageExecutor{t: t, root: root, installed: true, active: true}
					before := fileTreeSnapshot(t, root)
					report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, from, func(context.Context) error { t.Fatal("callback called"); return nil })
					var failure *backup.RestoreError
					want := "/opt/notes/state has not moved; install notes first"
					if !errors.As(err, &failure) || failure.Stage != "source" || len(failure.Stopped) != 0 || len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Detail != "" || report.Steps[0].Err == nil || report.Steps[0].Err.Error() != want {
						t.Fatalf("Restore = %+v, %v", report, err)
					}
					if !reflect.DeepEqual(executor.commands, []string{"zstd --quiet --decompress --stdout"}) || len(client.parameters) != 0 || len(client.got) != 1 || !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
						t.Fatalf("legacy refusal changed state: commands %v, cloud %+v", executor.commands, client)
					}
				})
			}
		}
	}
}

func TestRestoreCreatesDataParentsAndPreservesCacheAndExistingModes(t *testing.T) {
	// R-S1AR-UPD9 R-S2IO-8H3Y
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new parents", true: "existing parents"}[existing], func(t *testing.T) {
			root := t.TempDir()
			store := configuredFileStore(t, root)
			if existing {
				for _, name := range []string{"var", "var/opt", "var/opt/ikigenba", "var/opt/ikigenba/notes"} {
					if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
						t.Fatal(err)
					}
				}
				writeFile(t, root, "var/opt/ikigenba/notes/state/stale", "old", 0600)
				writeFile(t, root, "var/opt/ikigenba/notes/cache/keep", "cache", 0600)
			}
			body := hostRestoreArchive(t, restoreMember{name: "etc/env", data: []byte("OLD=archive\n")}, restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0750}, restoreMember{name: "state/value", data: []byte("restored")})
			executor := &restoreStageExecutor{t: t, root: root}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if report.Steps[2].Detail != "/opt/notes/etc, /var/opt/ikigenba/notes/state, 2 files" {
				t.Fatalf("files %+v", report)
			}
			if got := string(readHostRestoreFile(t, root, "var/opt/ikigenba/notes/state/value")); got != "restored" {
				t.Fatal(got)
			}
			for name, want := range map[string]os.FileMode{"var/opt": 0755, "var/opt/ikigenba": 0755, "var/opt/ikigenba/notes": 0750, "opt/notes": 0755} {
				if existing && strings.HasPrefix(name, "var/") {
					want = 0700
				}
				info, err := os.Lstat(filepath.Join(root, name))
				if err != nil || info.Mode().Perm() != want {
					t.Fatalf("%s: %v %v want %o", name, info, err, want)
				}
			}
			chowns := []string{}
			for _, command := range executor.commands {
				if strings.HasPrefix(command, "chown ") {
					chowns = append(chowns, command)
				}
			}
			if existing {
				if len(chowns) != 0 {
					t.Fatal(chowns)
				}
				if got := string(readHostRestoreFile(t, root, "var/opt/ikigenba/notes/cache/keep")); got != "cache" {
					t.Fatal(got)
				}
			} else {
				if !reflect.DeepEqual(chowns, []string{"chown ikigenba:ikigenba " + filepath.Join(root, "var/opt/ikigenba/notes")}) {
					t.Fatal(chowns)
				}
				for dir, want := range map[string]string{"opt/notes": "etc", "var/opt/ikigenba/notes": "state"} {
					entries, err := os.ReadDir(filepath.Join(root, dir))
					if err != nil || len(entries) != 1 || entries[0].Name() != want {
						t.Fatalf("%s = %v %v", dir, entries, err)
					}
				}
				if !containsString(executor.commands, "id --user ikigenba") {
					t.Fatal("account not ensured")
				}
			}
			if _, err := os.Lstat(filepath.Join(root, "var/opt/ikigenba/notes/state/stale")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("stale remains", err)
			}
		})
	}
}

func TestRestoreDataDirectoryOwnershipFailureStopsAtFiles(t *testing.T) {
	// R-S2IO-8H3Y
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/notes/etc/old", "old", 0600)
	body := hostRestoreArchive(t, restoreMember{name: "etc/new", data: []byte("new")}, restoreMember{name: "state/value", data: []byte("new")})
	executor := &restoreStageExecutor{t: t, root: root, failCommand: "chown ikigenba:ikigenba " + filepath.Join(root, "var/opt/ikigenba/notes"), failErr: errors.New("ownership unavailable")}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", func(context.Context) error { t.Fatal("callback called"); return nil })
	if err == nil || len(report.Steps) != 3 || report.Steps[2].Name != "files" || report.Steps[2].Err == nil || report.Steps[2].Detail != "" {
		t.Fatalf("Restore = %+v %v", report, err)
	}
	if got := string(readHostRestoreFile(t, root, "opt/notes/etc/old")); got != "old" {
		t.Fatal(got)
	}
	if _, err := os.Lstat(filepath.Join(root, "var/opt/ikigenba/notes/state")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state published", err)
	}
}

func TestRestoreNewDataAccountFailureHasOwnershipStage(t *testing.T) {
	// R-S2IO-8H3Y R-KALQ-MZPK
	root := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, root, "opt/notes/etc/old", "unchanged", 0600)
	body := hostRestoreArchive(t, restoreMember{name: "etc/new", data: []byte("new")}, restoreMember{name: "state/value", data: []byte("new")})
	executor := &restoreStageExecutor{t: t, root: root, failCommand: "id --user ikigenba", failErr: errors.New("account unavailable")}
	before := fileTreeSnapshot(t, root)
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", func(context.Context) error { t.Fatal("callback called"); return nil })
	var failure *backup.RestoreError
	if !errors.As(err, &failure) || failure.Stage != "ownership" || len(report.Steps) != 3 || report.Steps[2].Name != "files" || report.Steps[2].Err == nil || report.Steps[2].Detail != "" {
		t.Fatalf("Restore = %+v %v", report, err)
	}
	if !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
		t.Fatal("account failure changed host files")
	}
}

func TestRestoreLegacyInspectionCannotFollowAncestorsOutsideRoot(t *testing.T) {
	// R-DDQQ-Z0P6 R-GU6L-Z0L7
	root := t.TempDir()
	outside := t.TempDir()
	store := configuredFileStore(t, root)
	writeFile(t, outside, "notes/state/private", "outside", 0600)
	if err := os.Symlink(outside, filepath.Join(root, "opt")); err != nil {
		t.Fatal(err)
	}
	body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("new")})
	executor := &restoreStageExecutor{t: t, root: root}
	before := fileTreeSnapshot(t, outside)
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", func(context.Context) error { t.Fatal("callback called"); return nil })
	var failure *backup.RestoreError
	if !errors.As(err, &failure) || failure.Stage != "source" || len(failure.Stopped) != 0 || len(report.Steps) != 1 || report.Steps[0].Err == nil || strings.Contains(err.Error(), "has not moved") {
		t.Fatalf("Restore traversed outside root = %+v %v", report, err)
	}
	if !reflect.DeepEqual(executor.commands, []string{"zstd --quiet --decompress --stdout"}) || !reflect.DeepEqual(before, fileTreeSnapshot(t, outside)) {
		t.Fatalf("effects outside root or after source: %v", executor.commands)
	}
}

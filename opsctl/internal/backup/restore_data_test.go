package backup_test

import (
	"archive/tar"
	"context"
	"errors"
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

func TestRestoreRefusesEveryLegacyStateEntryBeforeSecretsOrStops(t *testing.T) {
	// R-CPP0-WW9M
	// R-COH4-J4IX
	for _, kind := range []string{"directory", "file", "dangling symlink"} {
		for _, snapshot := range []bool{false, true} {
			for _, dataExists := range []bool{false, true} {
				t.Run(kind+map[bool]string{false: " backup", true: " snapshot"}[snapshot]+map[bool]string{false: " no data", true: " data"}[dataExists], func(t *testing.T) {
					root := t.TempDir()
					restoreInstalled(t, root, false)
					store := restoreConfiguredStore(t, root)
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
					report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: client.open}, store, "notes", nil, from, nil, func(context.Context) error { t.Fatal("callback called"); return nil })
					var failure *backup.RestoreError
					want := "/opt/notes/state has not moved"
					if !errors.As(err, &failure) || failure.Stage != "source" || len(failure.Stopped) != 0 || len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Detail != "" || report.Steps[0].Err == nil || report.Steps[0].Err.Error() != want {
						t.Fatalf("Restore = %+v, %v", report, err)
					}
					if len(executor.commands) != 0 || len(client.parameters) != 0 || len(client.got) != 0 || len(client.opened) != 0 || !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
						t.Fatalf("legacy refusal changed state: commands %v, cloud %+v", executor.commands, client)
					}
				})
			}
		}
	}
}

func TestRestoreCreatesDataParentsAndPreservesCacheAndExistingModes(t *testing.T) {
	// R-RP29-000R R-RIYR-35BA
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new parents", true: "existing parents"}[existing], func(t *testing.T) {
			root := t.TempDir()
			restoreInstalled(t, root, false)
			store := restoreConfiguredStore(t, root)
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
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", nil, func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if report.Steps[3].Detail != "/etc/opt/ikigenba/notes/env, /var/opt/ikigenba/notes/state, 1 files" {
				t.Fatalf("files %+v", report)
			}
			if got := string(readHostRestoreFile(t, root, "var/opt/ikigenba/notes/state/value")); got != "restored" {
				t.Fatal(got)
			}
			for name, want := range map[string]os.FileMode{"var/opt": 0755, "var/opt/ikigenba": 0750, "var/opt/ikigenba/notes": 0750} {
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
				if strings.HasPrefix(command, "chown ikigenba:ikigenba ") {
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
				for dir, want := range map[string]string{"var/opt/ikigenba/notes": "state"} {
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
	// R-RIYR-35BA
	root := t.TempDir()
	restoreInstalled(t, root, false)
	store := restoreConfiguredStore(t, root)
	writeFile(t, root, "opt/notes/etc/old", "old", 0600)
	body := hostRestoreArchive(t, restoreMember{name: "etc/new", data: []byte("new")}, restoreMember{name: "state/value", data: []byte("new")})
	executor := &restoreStageExecutor{t: t, root: root, failCommand: "chown ikigenba:ikigenba " + filepath.Join(root, "var/opt/ikigenba/notes"), failErr: errors.New("ownership unavailable")}
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", nil, func(context.Context) error { t.Fatal("callback called"); return nil })
	if err == nil || len(report.Steps) != 4 || report.Steps[3].Name != "files" || report.Steps[3].Err == nil || report.Steps[3].Detail != "" {
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
	// R-RIYR-35BA R-RK6N-GX1Z
	root := t.TempDir()
	restoreInstalled(t, root, false)
	store := restoreConfiguredStore(t, root)
	writeFile(t, root, "opt/notes/etc/old", "unchanged", 0600)
	body := hostRestoreArchive(t, restoreMember{name: "etc/new", data: []byte("new")}, restoreMember{name: "state/value", data: []byte("new")})
	executor := &restoreStageExecutor{t: t, root: root, failCommand: "id --user ikigenba", failErr: errors.New("account unavailable")}
	before := fileTreeSnapshot(t, root)
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", nil, func(context.Context) error { t.Fatal("callback called"); return nil })
	var failure *backup.RestoreError
	if !errors.As(err, &failure) || failure.Stage != "ownership" || len(report.Steps) != 4 || report.Steps[3].Name != "files" || report.Steps[3].Err == nil || report.Steps[3].Detail != "" {
		t.Fatalf("Restore = %+v %v", report, err)
	}
	if !reflect.DeepEqual(before, fileTreeSnapshot(t, root)) {
		t.Fatal("account failure changed host files")
	}
}

func TestRestoreLegacyInspectionCannotFollowAncestorsOutsideRoot(t *testing.T) {
	// R-GSZD-5A23 R-GU6L-Z0L7
	root := t.TempDir()
	outside := t.TempDir()
	store := restoreConfiguredStore(t, root)
	writeFile(t, outside, "notes/state/private", "outside", 0600)
	if err := os.Symlink(outside, filepath.Join(root, "opt")); err != nil {
		t.Fatal(err)
	}
	body := hostRestoreArchive(t, restoreMember{name: "state/value", data: []byte("new")})
	executor := &restoreStageExecutor{t: t, root: root}
	before := fileTreeSnapshot(t, outside)
	report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", nil, func(context.Context) error { t.Fatal("callback called"); return nil })
	var failure *backup.RestoreError
	if err == nil || errors.As(err, &failure) || len(report.Steps) != 0 || strings.Contains(err.Error(), "has not moved") {
		t.Fatalf("Restore traversed outside root = %+v %v", report, err)
	}
	if len(executor.commands) != 0 || !reflect.DeepEqual(before, fileTreeSnapshot(t, outside)) {
		t.Fatalf("effects outside root or after source: %v", executor.commands)
	}
}

func TestRestoreKeepsNewDataPrivateAfterSuccessAndFailedHostTools(t *testing.T) {
	// R-RQA5-DRRG R-RHQU-PDKL R-RIYR-35BA R-RK6N-GX1Z
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "tool failure"}[failed], func(t *testing.T) {
			root := t.TempDir()
			restoreInstalled(t, root, true)
			store := restoreConfiguredStore(t, root)
			writeFile(t, root, "var/opt/ikigenba/notes/state/app.db", "old database", 0o600)
			writeFile(t, root, "var/opt/ikigenba/notes/cache/keep", "cache", 0o777)
			filesystem, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = filesystem.Close() }()
			if err := filesystem.Chmod("var/opt/ikigenba/notes/cache/keep", 0o777); err != nil {
				t.Fatal(err)
			}
			before := filepath.Join(root, "var/opt/ikigenba")
			if err := filesystem.Chmod("var/opt/ikigenba", 0o777); err != nil {
				t.Fatal(err)
			}
			body := hostRestoreArchive(t, restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0o777}, restoreMember{name: "state/private", typeflag: tar.TypeDir, mode: 0o775}, restoreMember{name: "state/private/value", data: []byte("bytes"), mode: 0o647})
			executor := &restoreStageExecutor{t: t, root: root}
			base := executor.execute
			env := host.Env{Root: root, Execute: func(ctx context.Context, c host.Command) (host.Result, error) {
				if c.Name == "litestream" && len(c.Args) > 0 && c.Args[0] == "restore" {
					p := filepath.Join(root, "var/opt/ikigenba/notes/state/generated")
					if err := os.MkdirAll(p, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := filesystem.Chmod("var/opt/ikigenba/notes/state/generated", 0o777); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(p, "partial"), []byte("partial"), 0o600); err != nil {
						t.Fatal(err)
					}
					filesystem, err := os.OpenRoot(root)
					if err != nil {
						t.Fatal(err)
					}
					if err := filesystem.Chmod("var/opt/ikigenba/notes/state/generated/partial", 0o666); err != nil {
						t.Fatal(err)
					}
					_ = filesystem.Close()
					if failed {
						writeRestoreSQLite(t, c.Args[2])
						return host.Result{ExitCode: 8}, nil
					}
				}
				return base(ctx, c)
			}}
			_, err = backup.Restore(context.Background(), env, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", nil, func(context.Context) error { return nil })
			if failed && err == nil {
				t.Fatal("failed tool accepted")
			}
			if !failed && err != nil {
				t.Fatal(err)
			}
			if info, err := os.Lstat(before); err != nil || info.Mode().Perm() != 0o777 {
				t.Fatal("existing root mode changed")
			}
			if info, err := os.Lstat(filepath.Join(root, "var/opt/ikigenba/notes/cache/keep")); err != nil || info.Mode().Perm() != 0o777 {
				t.Fatal("existing cache changed")
			}
			err = filepath.WalkDir(filepath.Join(root, "var/opt/ikigenba/notes/state"), func(name string, d os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				if info.Mode().Perm()&7 != 0 {
					t.Fatalf("new public path %s %o", name, info.Mode().Perm())
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if info, err := os.Lstat(filepath.Join(root, "var/opt/ikigenba/notes/state/private/value")); err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("archive other bits changed %v %v", info, err)
			}
		})
	}
}

func TestRestoredArchiveSpecialBitsSurviveOwnershipMapping(t *testing.T) {
	// R-RK6N-GX1Z R-RQA5-DRRG
	root := t.TempDir()
	restoreInstalled(t, root, false)
	store := restoreConfiguredStore(t, root)
	body := hostRestoreArchive(t, restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0o2777, uname: "ikigenba", gname: "ikigenba"}, restoreMember{name: "state/executable", mode: 0o6757, data: []byte("opaque"), uname: "ikigenba", gname: "ikigenba"})
	executor := &restoreStageExecutor{t: t, root: root}
	_, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", nil, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{"var/opt/ikigenba/notes/state": os.ModeSetgid | 0o770, "var/opt/ikigenba/notes/state/executable": os.ModeSetuid | os.ModeSetgid | 0o750} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky|0o777) != want {
			t.Fatalf("special archive mode %s: %v %v", name, info, err)
		}
	}
}

func TestRestoreModesIgnoreOwnerRestrictingUmask(t *testing.T) {
	// R-76AM-ZRLD R-RK6N-GX1Z R-RP29-000R R-RQA5-DRRG
	for _, hostRestore := range []bool{false, true} {
		t.Run(map[bool]string{false: "service", true: "host"}[hostRestore], func(t *testing.T) {
			root := t.TempDir()
			var run func() error
			if hostRestore {
				store := configuredHostStore(t, root)
				body := hostRestoreArchive(t, restoreMember{name: "etc/ikigenba", typeflag: tar.TypeDir, mode: 0o777}, restoreMember{name: "etc/ikigenba/nested", typeflag: tar.TypeDir, mode: 0o775}, restoreMember{name: "etc/ikigenba/nested/value", data: []byte("value"), mode: 0o644}, restoreMember{name: "etc/ikigenba/implied/value", data: []byte("implied"), mode: 0o644})
				uri := "s3://bucket/project/host/2026-09-16T12:00:00Z.tar.zst"
				client := &restoreCloud{objects: []cloud.Object{{URI: uri}}, bodies: map[string][]byte{uri: body}}
				run = func() error {
					_, err := backup.HostRestore(context.Background(), restoreHostEnv(t, root), cloud.Env{Open: client.open}, store)
					return err
				}
			} else {
				restoreInstalled(t, root, false)
				store := restoreConfiguredStore(t, root)
				body := hostRestoreArchive(t, restoreMember{name: "state", typeflag: tar.TypeDir, mode: 0o777}, restoreMember{name: "state/nested", typeflag: tar.TypeDir, mode: 0o775}, restoreMember{name: "state/nested/value", data: []byte("value"), mode: 0o644}, restoreMember{name: "state/implied/value", data: []byte("implied"), mode: 0o644})
				executor := &restoreStageExecutor{t: t, root: root}
				run = func() error {
					_, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: executor.execute}, cloud.Env{Open: restoreClientFor(t, body).open}, store, "notes", nil, "", nil, func(context.Context) error { return nil })
					return err
				}
			}
			mask := syscall.Umask(0o777)
			defer syscall.Umask(mask)
			if err := run(); err != nil {
				t.Fatal(err)
			}
			base := "var/opt/ikigenba/notes/state"
			baseMode := os.FileMode(0o770)
			if hostRestore {
				base = "etc/ikigenba"
				baseMode = 0o700
			}
			for name, want := range map[string]os.FileMode{base: baseMode, base + "/nested": 0o770, base + "/nested/value": 0o640, base + "/implied": 0o700, base + "/implied/value": 0o640} {
				info, err := os.Lstat(filepath.Join(root, name))
				if err != nil || info.Mode().Perm() != want {
					t.Fatalf("umask mode %s: %v %v", name, info, err)
				}
			}
		})
	}
}

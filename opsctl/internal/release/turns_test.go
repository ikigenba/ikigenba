package release_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func seedTurns(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{release.TurnsService, release.TurnsTimer} {
		put(t, root, release.ReleasesDir+"/"+shaFixture()+"/"+release.TurnsUnitsPath+"/"+name, name+"\x00\nunit bytes")
	}
}

func TestTurnsPublicNames(t *testing.T) {
	// R-UCG1-HEWM R-UEVU-8YE0 R-UIJJ-E9M3 R-UKZC-5T3H
	got := []string{release.TurnsLink, release.CurrentTurns, release.TurnsDir, release.TurnsPath, release.TurnsUnitsPath, release.TurnsService, release.TurnsTimer, release.UserUnitDir, release.TurnsConfig}
	want := []string{"/usr/local/bin/turns", "/opt/ikigenba/current/turns/bin/turns", "turns", "turns/bin/turns", "turns/share/systemd", "turns.service", "turns.timer", "/etc/systemd/user", "/etc/xdg/turns/config"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	root := t.TempDir()
	seedTurns(t, root)
	if !release.HasTurns(root, shaFixture()) {
		t.Fatal("missing turns")
	}
	if err := release.LinkTurns(root, release.CurrentTurns); err != nil {
		t.Fatal(err)
	}
	if err := release.InstallTurns(root, shaFixture(), "space.example"); err != nil {
		t.Fatal(err)
	}
}

func TestHasTurnsOnlyRealTreeAndReadOnly(t *testing.T) {
	// R-UG3Q-MQ4P R-UHBN-0HVE R-UDNX-V6NB
	for _, kind := range []string{"directory", "absent", "file", "link", "dangling", "missing-release", "invalid-sha", "escape"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			sha := shaFixture()
			folder := release.ReleasesDir + "/" + sha
			switch kind {
			case "directory":
				seedTurns(t, root)
			case "file":
				put(t, root, folder+"/turns", "file")
			case "absent":
				put(t, root, folder+"/keep", "keep")
			case "link", "dangling":
				target := "/outside"
				if kind == "link" {
					put(t, root, "outside/keep", "keep")
				}
				link(t, root, folder+"/turns", target)
			case "invalid-sha":
				sha = "../outside"
				put(t, root, "opt/ikigenba/outside/turns/keep", "keep")
			case "escape":
				put(t, root, "outside/turns/keep", "keep")
				link(t, root, folder, "/outside")
			}
			before := tree(t, root)
			if got := release.HasTurns(root, sha); got != (kind == "directory") {
				t.Fatalf("HasTurns=%t", got)
			}
			if !reflect.DeepEqual(before, tree(t, root)) {
				t.Fatal("reader changed files")
			}
		})
	}
}

func TestLinkTurnsReplacesWithoutFollowing(t *testing.T) {
	// R-UJRF-S1CS R-12PG-NV8U
	for _, kind := range []string{"absent", "file", "link", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "keep", "unchanged")
			switch kind {
			case "file":
				put(t, root, release.TurnsLink, "old")
			case "link":
				link(t, root, release.TurnsLink, "/keep")
			case "directory":
				put(t, root, release.TurnsLink+"/keep", "old")
			}
			target := "../unresolved/../target"
			err := release.LinkTurns(root, target)
			if kind == "directory" {
				if err == nil || !strings.HasPrefix(err.Error(), release.TurnsLink+": ") {
					t.Fatal(err)
				}
			} else if got, e := os.Readlink(filepath.Join(root, release.TurnsLink)); err != nil || e != nil || got != target {
				t.Fatal(got, err, e)
			}
			entries, err := os.ReadDir(filepath.Join(root, "/usr/local/bin"))
			if err != nil || len(entries) != 1 || entries[0].Name() != "turns" {
				t.Fatal(entries, err)
			}
			data, _ := rootRead(t, root, "keep")
			if string(data) != "unchanged" {
				t.Fatal("followed link")
			}
			if kind == "absent" {
				for _, name := range []string{"usr", "usr/local", "usr/local/bin"} {
					checkTurnsMode(t, root, name, 0o755)
				}
			}
		})
	}
	root := t.TempDir()
	put(t, root, "usr", "blocked")
	if err := release.LinkTurns(root, "target"); err == nil || !strings.HasPrefix(err.Error(), release.TurnsLink+": ") {
		t.Fatal(err)
	}
}

func checkTurnsMode(t *testing.T, root, name string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(root, name))
	if err != nil || info.Mode().Perm() != mode {
		t.Fatalf("%s: %v %v", name, info, err)
	}
}

func chmodTurnsRoot(root, name string, mode os.FileMode) error {
	f, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Chmod(strings.TrimPrefix(name, "/"), mode)
}

func TestInstallTurnsExactBytesModesAndReplacement(t *testing.T) {
	// R-F2JC-882L R-VVXF-SH9O
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			root := t.TempDir()
			seedTurns(t, root)
			paths := []string{release.UserUnitDir + "/" + release.TurnsService, release.UserUnitDir + "/" + release.TurnsTimer, release.TurnsConfig}
			put(t, root, "keep", "untouched")
			if existing {
				for _, name := range paths {
					link(t, root, name, "/keep")
				}
				if err := chmodTurnsRoot(root, release.UserUnitDir, 0o711); err != nil {
					t.Fatal(err)
				}
			}
			if err := release.InstallTurns(root, shaFixture(), "My.Space."); err != nil {
				t.Fatal(err)
			}
			for i, name := range paths {
				data, err := rootRead(t, root, strings.TrimPrefix(name, "/"))
				want := "space=My.Space.\n"
				if i < 2 {
					want = filepath.Base(name) + "\x00\nunit bytes"
				}
				info, e := os.Lstat(filepath.Join(root, name))
				if err != nil || e != nil || string(data) != want || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
					t.Fatal(name, string(data), info, err, e)
				}
			}
			for _, name := range []string{release.UserUnitDir, "/etc/xdg/turns"} {
				mode := os.FileMode(0o755)
				if existing && name == release.UserUnitDir {
					mode = 0o711
				}
				checkTurnsMode(t, root, name, mode)
				entries, err := os.ReadDir(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".") {
						t.Fatal("temporary entry left behind")
					}
				}
			}
			if !existing {
				for _, name := range []string{"/etc", "/etc/systemd", "/etc/xdg"} {
					checkTurnsMode(t, root, name, 0o755)
				}
			}
			data, _ := rootRead(t, root, "keep")
			if string(data) != "untouched" {
				t.Fatal("followed destination symlink")
			}
		})
	}
}

func TestInstallTurnsPreflightLeavesRootUnchanged(t *testing.T) {
	// R-0XTV-4SA2 R-UTT7-QMG1
	for _, kind := range []string{"invalid", "absent-release", "absent-tree", "linked-tree", "file-tree", "service", "timer"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			sha := shaFixture()
			prefix := release.ReleasesDir + "/" + sha
			switch kind {
			case "invalid":
				sha = "../invalid"
				prefix = release.ReleasesDir + "/" + sha
			case "absent-tree":
				put(t, root, prefix+"/keep", "keep")
			case "file-tree":
				put(t, root, prefix+"/turns", "file")
			case "linked-tree":
				link(t, root, prefix+"/turns", "/outside")
			case "service", "timer":
				seedTurns(t, root)
				name := "turns." + kind
				if err := os.Remove(filepath.Join(root, prefix, release.TurnsUnitsPath, name)); err != nil {
					t.Fatal(err)
				}
				prefix += "/" + release.TurnsUnitsPath + "/" + name
			}
			before := tree(t, root)
			err := release.InstallTurns(root, sha, "space")
			if err == nil || !strings.HasPrefix(err.Error(), prefix+": ") || !reflect.DeepEqual(before, tree(t, root)) {
				t.Fatal(err, "preflight mutation or wrong error")
			}
		})
	}
}

func TestInstallTurnsStopsAtFirstDestinationFailure(t *testing.T) {
	// R-F3R8-LZTA R-VVXF-SH9O R-UTT7-QMG1
	paths := []string{release.UserUnitDir + "/" + release.TurnsService, release.UserUnitDir + "/" + release.TurnsTimer, release.TurnsConfig}
	for failAt := range paths {
		t.Run(paths[failAt], func(t *testing.T) {
			root := t.TempDir()
			seedTurns(t, root)
			for i, name := range paths {
				if i == failAt {
					put(t, root, name+"/keep", "blocked")
				} else {
					put(t, root, name, "old")
				}
			}
			if err := chmodTurnsRoot(root, "/etc/xdg/turns", 0o700); err != nil {
				t.Fatal(err)
			}
			err := release.InstallTurns(root, shaFixture(), "space")
			if err == nil || !strings.HasPrefix(err.Error(), paths[failAt]+": ") {
				t.Fatal(err)
			}
			for i, name := range paths {
				if i == failAt {
					continue
				}
				data, err := rootRead(t, root, strings.TrimPrefix(name, "/"))
				want := "old"
				if i < failAt {
					want = filepath.Base(name) + "\x00\nunit bytes"
				}
				if err != nil || string(data) != want {
					t.Fatal(name, string(data), err)
				}
			}
			mode := os.FileMode(0o700)
			if failAt == 2 {
				mode = 0o755
			}
			checkTurnsMode(t, root, "/etc/xdg/turns", mode)
			for name := range tree(t, root) {
				if strings.HasPrefix(filepath.Base(name), ".turns-") {
					t.Fatal("temporary entry left behind")
				}
			}
		})
	}
}

func TestInstallTurnsDirectoryFailuresAndRootedAncestors(t *testing.T) {
	// R-VVXF-SH9O R-F3R8-LZTA R-UTT7-QMG1
	for _, path := range []string{"/etc", "/etc/systemd", release.UserUnitDir, "/etc/xdg", "/etc/xdg/turns"} {
		for _, kind := range []string{"file", "dangling", "loop", "directory-link"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				seedTurns(t, root)
				final := path == release.UserUnitDir || path == "/etc/xdg/turns"
				switch kind {
				case "file":
					put(t, root, path, "blocked")
				case "dangling":
					link(t, root, path, "/missing")
				case "loop":
					link(t, root, path, path)
				case "directory-link":
					put(t, root, "/redirect/keep", "untouched")
					link(t, root, path, "/redirect")
				}
				err := release.InstallTurns(root, shaFixture(), "space")
				if kind == "directory-link" && !final {
					if err != nil {
						t.Fatal(err)
					}
					resolved, err := release.Resolve(root, release.TurnsConfig)
					if err != nil {
						t.Fatal(err)
					}
					relative, err := filepath.Rel(root, resolved)
					if err != nil {
						t.Fatal(err)
					}
					data, err := rootRead(t, root, relative)
					if err != nil || string(data) != "space=space\n" {
						t.Fatal(string(data), err)
					}
					return
				}
				concerned := release.UserUnitDir + "/" + release.TurnsService
				if strings.HasPrefix(path, "/etc/xdg") {
					concerned = release.TurnsConfig
				}
				if err == nil || !strings.HasPrefix(err.Error(), concerned+": ") {
					t.Fatal(err)
				}
				if !strings.HasPrefix(path, "/etc/xdg") {
					if _, err := os.Lstat(filepath.Join(root, "/etc/xdg")); !os.IsNotExist(err) && path != "/etc" {
						t.Fatal("config ancestors created early", err)
					}
				} else {
					data, err := rootRead(t, root, strings.TrimPrefix(release.UserUnitDir+"/"+release.TurnsTimer, "/"))
					if err != nil || string(data) != release.TurnsTimer+"\x00\nunit bytes" {
						t.Fatal("earlier timer lost", err)
					}
				}
				if kind == "directory-link" {
					entries, err := os.ReadDir(filepath.Join(root, "/redirect"))
					if err != nil || len(entries) != 1 || entries[0].Name() != "keep" {
						t.Fatal("followed final directory symlink", entries, err)
					}
				}
			})
		}
	}
}

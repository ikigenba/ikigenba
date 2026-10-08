package apps_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

func TestInstallDataRulesMoveDropAndRefuse(t *testing.T) {
	// R-YZWT-4U5W R-Z787-FGM2 R-ZAVW-KRU5
	//
	for _, tc := range []struct {
		name                                   string
		oldState, newState, oldCache, newCache bool
		detail                                 string
		refused                                bool
	}{
		{name: "empty", detail: "/var/opt/ikigenba/notes"},
		{name: "both moved", oldState: true, oldCache: true, detail: "/var/opt/ikigenba/notes; moved /opt/notes/state, /opt/notes/cache"},
		{name: "cache moved", oldCache: true, detail: "/var/opt/ikigenba/notes; moved /opt/notes/cache"},
		{name: "cache dropped", oldCache: true, newCache: true, detail: "/var/opt/ikigenba/notes; dropped /opt/notes/cache"},
		{name: "state moved cache dropped", oldState: true, oldCache: true, newCache: true, detail: "/var/opt/ikigenba/notes; moved /opt/notes/state; dropped /opt/notes/cache"},
		{name: "state refused", oldState: true, newState: true, oldCache: true, newCache: true, refused: true, detail: "/opt/notes/state and /var/opt/ikigenba/notes/state both exist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			old := filepath.Join(root, "opt", "notes")
			data := filepath.Join(root, "var", "opt", "ikigenba", "notes")
			for _, entry := range []struct {
				present bool
				name    string
				value   string
			}{{tc.oldState, filepath.Join(old, "state", "nested", "keep"), "old state"}, {tc.newState, filepath.Join(data, "state", "keep"), "new state"}, {tc.oldCache, filepath.Join(old, "cache", "keep"), "old cache"}, {tc.newCache, filepath.Join(data, "cache", "keep"), "new cache"}} {
				if entry.present {
					writeFixture(t, entry.name, []byte(entry.value), 0o400)
				}
			}
			if tc.oldState {
				if err := os.Symlink("nested/keep", filepath.Join(old, "state", "link")); err != nil {
					t.Fatal(err)
				}
			}
			var stateBefore map[string]treeSnapshotEntry
			if tc.oldState {
				stateBefore = snapshotTree(t, filepath.Join(old, "state"))
			}
			beforeEntries := map[string]os.FileInfo{}
			for _, entry := range []string{"state", "state/nested", "state/nested/keep", "state/link", "cache", "cache/keep"} {
				info, err := os.Lstat(filepath.Join(old, entry))
				if err == nil {
					beforeEntries[entry] = info
				} else if !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			}
			fixture := newCompletedInstallFixture(t, root, true)
			fixture.beforeStop = func(unit string) {
				for entry, info := range beforeEntries {
					now, err := os.Lstat(filepath.Join(old, entry))
					if err != nil || !os.SameFile(info, now) {
						t.Fatalf("%s moved before stop %s", entry, unit)
					}
				}
				for _, entry := range []struct {
					name string
					move bool
				}{{"state", tc.oldState && !tc.newState}, {"cache", tc.oldCache && !tc.newCache}} {
					if entry.move {
						if _, err := os.Lstat(filepath.Join(data, entry.name)); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("target %s exists before stop %s: %v", entry.name, unit, err)
						}
					}
				}
			}

			err := fixture.run()
			if tc.refused {
				var failure *apps.InstallError
				if !errors.As(err, &failure) || failure.Code != 1 {
					t.Fatalf("failure=%v", err)
				}
				if got := fixture.reports[len(fixture.reports)-1]; got != (installReport{"data", tc.detail, false}) {
					t.Fatalf("report=%#v", got)
				}
				for _, cmd := range fixture.commands {
					if cmd.name == "id" || cmd.name == "chown" || cmd.name == "systemctl" && cmd.args[0] == "stop" {
						t.Fatalf("mutation after refusal: %#v", cmd)
					}
				}
				if !reflect.DeepEqual(stateBefore, snapshotTree(t, filepath.Join(old, "state"))) {
					t.Fatal("refused state changed")
				}
				assertFile(t, filepath.Join(data, "state", "keep"), "new state")
				assertFile(t, filepath.Join(old, "cache", "keep"), "old cache")
				assertFile(t, filepath.Join(data, "cache", "keep"), "new cache")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !containsReport(fixture.reports, installReport{"data", tc.detail, true}) {
				t.Fatalf("reports=%#v", fixture.reports)
			}
			for entry, info := range beforeEntries {
				if strings.HasPrefix(entry, "cache") && tc.newCache {
					continue
				}
				now, err := os.Lstat(filepath.Join(data, entry))
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(info, now) || !sameFileOwner(info, now) {
					t.Fatalf("%s copied or owner changed", entry)
				}
			}
			if tc.oldState {
				target, err := os.Readlink(filepath.Join(data, "state", "link"))
				if err != nil || target != "nested/keep" {
					t.Fatalf("link target=%q err=%v", target, err)
				}
			}
			if tc.oldState {
				if !reflect.DeepEqual(stateBefore, snapshotTree(t, filepath.Join(data, "state"))) {
					t.Fatal("renamed state metadata/content changed")
				}
			}
			if tc.oldCache {
				want := "old cache"
				if tc.newCache {
					want = "new cache"
				}
				assertFile(t, filepath.Join(data, "cache", "keep"), want)
			}
			for _, entry := range []string{"state", "cache"} {
				if _, err := os.Lstat(filepath.Join(old, entry)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("old %s remains: %v", entry, err)
				}
			}
			var stops []string
			for _, cmd := range fixture.commands {
				if cmd.name == "systemctl" && cmd.args[0] == "stop" {
					stops = append(stops, cmd.args[1])
				}
			}
			var wantStops []string
			if tc.oldState || tc.oldCache && !tc.newCache {
				wantStops = []string{"ikigenba-notes.socket", "ikigenba-notes.service"}
			}
			if !reflect.DeepEqual(stops, wantStops) {
				t.Fatalf("stops=%v want %v", stops, wantStops)
			}
		})
	}
}

func TestDataEntriesAreJudgedWithoutFollowingLinks(t *testing.T) {
	// R-YZWT-4U5W
	//
	root := t.TempDir()
	old := filepath.Join(root, "opt", "notes")
	data := filepath.Join(root, "var", "opt", "ikigenba", "notes")
	if err := os.MkdirAll(old, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Join(old, "state"), filepath.Join(data, "state")} {
		if err := os.Symlink("missing", name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := apps.InspectData(root, "notes"); err == nil || err.Error() != "/opt/notes/state and /var/opt/ikigenba/notes/state both exist" {
		t.Fatalf("error=%v", err)
	}
	if err := os.Remove(filepath.Join(data, "state")); err != nil {
		t.Fatal(err)
	}
	plan, err := apps.InspectData(root, "notes")
	if err != nil {
		t.Fatal(err)
	}
	if err := apps.ApplyData(root, "notes", plan); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(data, "state")); err != nil || target != "missing" {
		t.Fatalf("moved target=%q %v", target, err)
	}
}

func TestDataRenameFailurePreservesAndRetryFinishes(t *testing.T) {
	// R-YZWT-4U5W
	//
	root := t.TempDir()
	old := filepath.Join(root, "opt", "notes")
	data := filepath.Join(root, "var", "opt", "ikigenba", "notes")
	writeFixture(t, filepath.Join(old, "state", "keep"), []byte("state"), 0o400)
	writeFixture(t, filepath.Join(old, "cache", "keep"), []byte("cache"), 0o600)
	if err := os.MkdirAll(data, 0o750); err != nil {
		t.Fatal(err)
	}
	plan, err := apps.InspectData(root, "notes")
	if err != nil {
		t.Fatal(err)
	}
	// A destination appearing after inspection makes the second rename fail.
	writeFixture(t, filepath.Join(data, "cache", "keep"), []byte("new cache"), 0o600)
	if err := apps.ApplyData(root, "notes", plan); err == nil {
		t.Fatal("rename succeeded onto nonempty directory")
	}
	assertFile(t, filepath.Join(data, "state", "keep"), "state")
	assertFile(t, filepath.Join(old, "cache", "keep"), "cache")
	if _, err := os.Lstat(filepath.Join(old, "state")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state rollback: %v", err)
	}
	plan, err = apps.InspectData(root, "notes")
	if err != nil {
		t.Fatal(err)
	}
	if err := apps.ApplyData(root, "notes", plan); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(data, "state", "keep"), "state")
	assertFile(t, filepath.Join(data, "cache", "keep"), "new cache")
	if _, err := os.Lstat(filepath.Join(old, "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache remains: %v", err)
	}
}

func TestInstallDataDirectoryAndAppRootOwnership(t *testing.T) {
	// R-Z2CL-WDNA
	//  R-WFP6-33XJ
	root := t.TempDir()
	fixture := newCompletedInstallFixture(t, root, false)
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"var/opt", "var/opt/ikigenba", "opt/notes"} {
		name := filepath.Join(root, directory)
		assertMode(t, name, 0o755)
	}
	data := filepath.Join(root, "var", "opt", "ikigenba", "notes")
	assertMode(t, data, 0o750)
	var dataCommands, appCommands int
	for _, cmd := range fixture.commands {
		if cmd.name != "chown" {
			continue
		}
		if reflect.DeepEqual(cmd.args, []string{"ikigenba:ikigenba", data}) {
			dataCommands++
		}
		if reflect.DeepEqual(cmd.args, []string{"root:root", filepath.Join(root, "opt", "notes")}) {
			appCommands++
		}
		for _, arg := range cmd.args {
			if strings.HasSuffix(arg, "/var/opt") || strings.HasSuffix(arg, "/var/opt/ikigenba") {
				t.Fatalf("parent chown: %#v", cmd)
			}
		}
	}
	if dataCommands != 1 || appCommands != 1 {
		t.Fatalf("data=%d app=%d", dataCommands, appCommands)
	}
}

func TestInstallFailuresBeforeDataDoNotCreateDataTree(t *testing.T) {
	// R-Z14P-ILWL
	//
	for _, stage := range []string{"configuration", "timing", "fetch", "file", "secrets"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			fixture := newCompletedInstallFixture(t, root, false)
			cause := errors.New("failed")
			switch stage {
			case "configuration":
				if err := (config.Store{Root: root}).Set("host.name", "."); err != nil {
					t.Fatal(err)
				}
			case "timing":
				if err := (config.Store{Root: root}).Set("apps.stop_seconds", "0"); err != nil {
					t.Fatal(err)
				}
			case "fetch":
				fixture.downloadFailure = cause
			case "file":
				fixture.fileFailure = cause
			case "secrets":
				fixture.archive = validInstallTar(t, "app = \"notes\"\nsecrets = [\"TOKEN\"]\n")
				fixture.secretsFailure = cause
			}
			if err := fixture.run(); err == nil {
				t.Fatal("success")
			}
			for _, name := range []string{"var/opt", "var/opt/ikigenba", "var/opt/ikigenba/notes"} {
				if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("created %s: %v", name, err)
				}
			}
		})
	}
}

func sameFileOwner(before, after os.FileInfo) bool {
	first, ok := before.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	second, ok := after.Sys().(*syscall.Stat_t)
	return ok && first.Uid == second.Uid && first.Gid == second.Gid
}

func TestInstallDataStopsEachActiveUnitIndependently(t *testing.T) {
	// R-ZAVW-KRU5
	//
	for _, tc := range []struct{ service, socket bool }{{true, false}, {false, true}, {false, false}} {
		t.Run(fmt.Sprintf("service=%t/socket=%t", tc.service, tc.socket), func(t *testing.T) {
			root := t.TempDir()
			old := filepath.Join(root, "opt", "notes", "state")
			writeFixture(t, filepath.Join(old, "keep"), []byte("data"), 0o600)
			fixture := newCompletedInstallFixture(t, root, tc.service)
			fixture.initiallySocketActive = &tc.socket
			fixture.beforeStop = func(unit string) {
				assertFile(t, filepath.Join(old, "keep"), "data")
				if _, err := os.Lstat(filepath.Join(root, "var", "opt", "ikigenba", "notes", "state")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("state moved before stop %s", unit)
				}
			}
			if err := fixture.run(); err != nil {
				t.Fatal(err)
			}
			var got, want []string
			for _, cmd := range fixture.commands {
				if cmd.name == "systemctl" && cmd.args[0] == "stop" {
					got = append(got, cmd.args[1])
				}
			}
			if tc.socket {
				want = append(want, "ikigenba-notes.socket")
			}
			if tc.service {
				want = append(want, "ikigenba-notes.service")
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("stops=%v want %v", got, want)
			}
		})
	}
}

func TestInstallNormalizesExistingDataDirectoryAndKeepsParents(t *testing.T) {
	// R-Z2CL-WDNA
	//
	root := t.TempDir()
	data := filepath.Join(root, "var", "opt", "ikigenba", "notes")
	keep := filepath.Join(data, "state", "keep")
	writeFixture(t, keep, []byte("data"), 0o400)
	for name, mode := range map[string]os.FileMode{filepath.Join(root, "var", "opt"): 0o700, filepath.Join(root, "var", "opt", "ikigenba"): 0o711, data: 0o700} {
		if err := os.Chmod(name, mode); err != nil {
			t.Fatal(err)
		}
	}
	parentBefore := map[string]os.FileInfo{}
	for _, name := range []string{filepath.Join(root, "var", "opt"), filepath.Join(root, "var", "opt", "ikigenba")} {
		info, err := os.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		parentBefore[name] = info
	}
	before, err := os.Lstat(keep)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newCompletedInstallFixture(t, root, false)
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}
	for name, info := range parentBefore {
		after, err := os.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		if after.Mode() != info.Mode() || !sameFileOwner(info, after) {
			t.Fatalf("parent metadata changed: %s", name)
		}
	}
	assertMode(t, data, 0o750)
	assertMode(t, keep, 0o400)
	after, err := os.Lstat(keep)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !sameFileOwner(before, after) {
		t.Fatal("state metadata changed")
	}
	var ownership []commandCall
	for _, cmd := range fixture.commands {
		if cmd.name == "chown" && cmd.args[0] == "ikigenba:ikigenba" {
			ownership = append(ownership, cmd)
		}
	}
	if !reflect.DeepEqual(ownership, []commandCall{{"chown", []string{"ikigenba:ikigenba", data}}}) {
		t.Fatalf("data ownership commands=%#v", ownership)
	}
}

package apps_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInstallLeavesDatabaseAndCacheToTheApp(t *testing.T) {
	// R-GCW4-EV9K
	for _, existing := range []bool{false, true} {
		root := t.TempDir()
		fixture := newCompletedInstallFixture(t, root, existing)
		fixture.archive = validInstallTar(t, "app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")
		var before map[string]treeSnapshotEntry
		if existing {
			writeFixture(t, filepath.Join(root, "var", "opt", "ikigenba", "notes", "state", "notes.db"), []byte("database bytes"), 0600)
			writeFixture(t, filepath.Join(root, "var", "opt", "ikigenba", "notes", "state", "migration.sql"), []byte("must not execute"), 0600)
			writeFixture(t, filepath.Join(root, "var", "opt", "ikigenba", "notes", "cache", "entry"), []byte("cache bytes"), 0600)
			before = snapshotTree(t, filepath.Join(root, "var", "opt", "ikigenba", "notes", "state"))
		}
		if err := fixture.run(); err != nil {
			t.Fatal(err)
		}
		if existing {
			after := snapshotTree(t, filepath.Join(root, "var", "opt", "ikigenba", "notes", "state"))
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("database state changed: %#v -> %#v", before, after)
			}
			assertFile(t, filepath.Join(root, "var", "opt", "ikigenba", "notes", "cache", "entry"), "cache bytes")
		} else {
			for _, directory := range []string{"state", "cache"} {
				if _, err := os.Lstat(filepath.Join(root, "var", "opt", "ikigenba", "notes", directory)); !os.IsNotExist(err) {
					t.Fatalf("install created %s: %v", directory, err)
				}
			}
		}
		// Only the installed executable's version query may execute an app binary.
		for _, command := range fixture.commands {
			switch command.name {
			case "xz", "systemctl", "id", "chown":
			case filepath.Join(root, "opt", "notes", "bin", "notes"):
				if !reflect.DeepEqual(command.args, []string{"--version"}) {
					t.Fatalf("app command %#v", command)
				}
			default:
				t.Fatalf("unexpected process %#v", command)
			}
		}
	}
}

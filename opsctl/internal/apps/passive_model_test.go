package apps_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// R-Y0Z4-ZBMR
func TestServiceModelIsPassiveAndStatusOwnsVersionQuery(t *testing.T) {
	root := t.TempDir()
	manifestData := []byte("app = \"ledger\"\nversion = \"manifest-version\"\n[database]\nengine = \"sqlite\"\npath = \"state/ledger.db\"\n")
	writeManifest(t, root, "ledger", string(manifestData))
	writeFile(t, filepath.Join(root, "opt", "ledger", "bin", "ledger"), []byte("installed executable"))
	writeFile(t, filepath.Join(root, "opt", "ledger", "state", "ledger.db"), []byte("unchanged database bytes"))
	writeFile(t, filepath.Join(root, "opt", "ledger", "state", "schema.sql"), []byte("must not migrate"))
	writeFile(t, filepath.Join(root, "opt", "ledger", "state", "seed.sql"), []byte("must not load"))
	before := snapshotTree(t, root)
	var commands []host.Command
	execute := func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		if command.Name == filepath.Join(root, "opt", "ledger", "bin", "ledger") {
			return host.Result{Stdout: []byte("executable-version\n")}, nil
		}
		return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=active\n")}, nil
	}

	if err := apps.ValidateName("ledger"); err != nil {
		t.Fatalf("ValidateName returned error: %v", err)
	}
	manifest, err := apps.ParseManifest(manifestData)
	if err != nil {
		t.Fatalf("ParseManifest returned error: %v", err)
	}
	services, err := apps.Discover(root)
	if err != nil || len(services) != 1 {
		t.Fatalf("Discover = %#v, %v", services, err)
	}
	if len(commands) != 0 {
		t.Fatalf("model operations executed commands: %#v", commands)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("model operations changed host state:\nbefore: %#v\nafter:  %#v", before, after)
	}
	manifestType := reflect.TypeFor[apps.Manifest]()
	if _, exists := manifestType.FieldByName("Version"); exists {
		t.Fatal("manifest carries an authoritative Version field")
	}
	if !reflect.DeepEqual(manifest, *services[0].Manifest) {
		t.Fatalf("parsed and discovered manifests differ: %#v, %#v", manifest, services[0].Manifest)
	}

	rows, err := apps.Status(context.Background(), host.Env{Root: root, Execute: execute})
	if err != nil || !reflect.DeepEqual(rows, []apps.StatusRow{{
		Name: "ledger", Version: "executable-version", State: "active", JournalMode: "-",
	}}) {
		t.Fatalf("Status = (%#v, %v), want executable version", rows, err)
	}
	wantCommands := []host.Command{
		{Name: filepath.Join(root, "opt", "ledger", "bin", "ledger"), Args: []string{"--version"}},
		{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-ledger.service"}},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("Status commands = %#v, want %#v", commands, wantCommands)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("status queries changed host state:\nbefore: %#v\nafter:  %#v", before, after)
	}
}

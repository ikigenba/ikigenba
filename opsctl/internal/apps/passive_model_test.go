package apps_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-92R8-ZFGB
func TestServiceModelIsPassiveAndIgnoresManifestVersion(t *testing.T) {
	root := t.TempDir()
	manifestData := []byte("app = \"ledger\"\nversion = \"manifest-version\"\n[database]\nengine = \"sqlite\"\npath = \"state/ledger.db\"\n")
	writeManifest(t, root, "ledger", string(manifestData))
	writeFile(t, filepath.Join(root, "opt", "ledger", "bin", "ledger"), []byte("installed executable"))
	writeFile(t, filepath.Join(root, "opt", "ledger", "state", "ledger.db"), []byte("unchanged database bytes"))
	writeFile(t, filepath.Join(root, "opt", "ledger", "state", "schema.sql"), []byte("must not migrate"))
	writeFile(t, filepath.Join(root, "opt", "ledger", "state", "seed.sql"), []byte("must not load"))
	before := snapshotTree(t, root)
	if _, err := apps.ReadLayout(root); err != nil {
		t.Fatal(err)
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
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("model operations changed host state:\nbefore: %#v\nafter:  %#v", before, after)
	}
	if !reflect.DeepEqual(manifest, *services[0].Manifest) {
		t.Fatalf("parsed and discovered manifests differ: %#v, %#v", manifest, services[0].Manifest)
	}

}

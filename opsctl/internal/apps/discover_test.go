package apps_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-XXBF-U0EO
func TestDiscoverSelectsImmediateServiceDirectoriesInBytewiseOrder(t *testing.T) {
	missingRoot := t.TempDir()
	services, err := apps.Discover(missingRoot)
	if err != nil || len(services) != 0 {
		t.Fatalf("Discover with missing /opt = %#v, %v; want empty success", services, err)
	}

	root := t.TempDir()
	for _, path := range []string{
		"opt/zeta/state",
		"opt/Alpha/etc",
		"opt/bad_name/state",
		"opt/data-only/state",
		"opt/both/etc",
		"opt/both/state",
		"opt/nested/child/etc",
	} {
		mkdirAll(t, filepath.Join(root, filepath.FromSlash(path)))
	}
	mkdirAll(t, filepath.Join(root, "opt", "empty"))
	writeFile(t, filepath.Join(root, "opt", "not-a-directory"), []byte("file"))
	writeFile(t, filepath.Join(root, "opt", "file-etc", "etc"), []byte("file"))

	services, err = apps.Discover(root)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if got, want := serviceNames(services), []string{"Alpha", "bad_name", "both", "data-only", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover names = %v, want bytewise ordered %v", got, want)
	}

	blocked := t.TempDir()
	writeFile(t, filepath.Join(blocked, "opt"), []byte("not a directory"))
	if services, err = apps.Discover(blocked); err == nil || services != nil {
		t.Fatalf("Discover with unreadable /opt = %#v, %v; want enumeration error", services, err)
	}

	escapeRoot := t.TempDir()
	outside := t.TempDir()
	mkdirAll(t, filepath.Join(outside, "escaped", "state"))
	if err := os.Symlink(filepath.Join(outside, "escaped"), filepath.Join(escapeRoot, "opt")); err != nil {
		t.Fatalf("create escaping /opt symlink: %v", err)
	}
	if services, err = apps.Discover(escapeRoot); err == nil || services != nil {
		t.Fatalf("Discover through /opt symlink outside root = %#v, %v; want root-containment error", services, err)
	}

	nestedRoot := t.TempDir()
	outsideEtc := t.TempDir()
	writeFile(t, filepath.Join(outsideEtc, "manifest.toml"), []byte("app = \"etc-escape\"\n"))
	mkdirAll(t, filepath.Join(nestedRoot, "opt", "etc-escape", "state"))
	if err := os.Symlink(outsideEtc, filepath.Join(nestedRoot, "opt", "etc-escape", "etc")); err != nil {
		t.Fatalf("create escaping etc symlink: %v", err)
	}

	outsideManifest := filepath.Join(t.TempDir(), "manifest.toml")
	writeFile(t, outsideManifest, []byte("app = \"manifest-escape\"\n"))
	mkdirAll(t, filepath.Join(nestedRoot, "opt", "manifest-escape", "etc"))
	mkdirAll(t, filepath.Join(nestedRoot, "opt", "manifest-escape", "state"))
	if err := os.Symlink(outsideManifest, filepath.Join(nestedRoot, "opt", "manifest-escape", "etc", "manifest.toml")); err != nil {
		t.Fatalf("create escaping manifest symlink: %v", err)
	}

	outsideState := t.TempDir()
	mkdirAll(t, filepath.Join(nestedRoot, "opt", "state-escape"))
	if err := os.Symlink(outsideState, filepath.Join(nestedRoot, "opt", "state-escape", "state")); err != nil {
		t.Fatalf("create escaping state symlink: %v", err)
	}

	services, err = apps.Discover(nestedRoot)
	if err != nil {
		t.Fatalf("Discover with nested escaping symlinks returned error: %v", err)
	}
	if got, want := serviceNames(services), []string{"etc-escape", "manifest-escape"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover with nested escaping symlinks names = %v, want %v", got, want)
	}
	for _, service := range services {
		if service.Manifest != nil || service.ManifestError == nil {
			t.Errorf("service through escaping manifest path = %#v, want contained read failure", service)
		}
	}
}

// R-XYJC-7S5D
func TestDiscoverKeepsManifestFailuresPerService(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "good", "app = \"good\"\n")
	writeManifest(t, root, "capabilities", "default = true\n")
	mkdirAll(t, filepath.Join(root, "opt", "missing", "etc"))
	writeManifest(t, root, "malformed", "port = [\n")
	mkdirAll(t, filepath.Join(root, "opt", "unreadable", "etc", "manifest.toml"))

	services, err := apps.Discover(root)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	byName := servicesByName(services)
	if len(byName) != 5 {
		t.Fatalf("Discover returned %d services: %#v", len(byName), services)
	}
	if got := byName["good"]; got.Manifest == nil || got.Manifest.App != "good" || got.ManifestError != nil {
		t.Errorf("good service = %#v", got)
	}
	if got := byName["capabilities"]; got.Manifest == nil || !got.Manifest.Default || got.ManifestError != nil {
		t.Errorf("capability-only service = %#v", got)
	}
	if got := byName["missing"]; got.Manifest != nil || got.ManifestError != nil {
		t.Errorf("missing-manifest service = %#v", got)
	}
	for _, name := range []string{"malformed", "unreadable"} {
		got := byName[name]
		if got.Manifest != nil || got.ManifestError == nil {
			t.Errorf("%s service = %#v, want retained per-service manifest error", name, got)
		}
	}
}

// R-XZR8-LJW2
func TestDiscoverUsesDirectoryIdentityAndRetainsStateOnlyServices(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "directory-name", "app = \"other-name\"\n")
	mkdirAll(t, filepath.Join(root, "opt", "state-only", "state"))
	writeFile(t, filepath.Join(root, "opt", "state-only", "state", "service.db"), []byte("data"))
	writeFile(t, filepath.Join(root, "opt", "directory-name", "unrelated-binary"), []byte("not executable"))

	services, err := apps.Discover(root)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	byName := servicesByName(services)
	if got := byName["directory-name"]; got.Name != "directory-name" || got.Manifest != nil || got.ManifestError == nil || !strings.Contains(got.ManifestError.Error(), "other-name") || !strings.Contains(got.ManifestError.Error(), "directory-name") {
		t.Errorf("mismatched service = %#v", got)
	}
	if got := byName["state-only"]; got.Name != "state-only" || got.Manifest != nil || got.ManifestError != nil {
		t.Errorf("state-only service = %#v", got)
	}
}

func TestServiceModelUsesHostLocalInputs(t *testing.T) {
	root := t.TempDir()
	manifestData := []byte("app = \"notes\"\n[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n")
	wantManifest := apps.Manifest{
		App:      "notes",
		Secrets:  []string{},
		Env:      map[string]string{},
		Database: &apps.Database{Engine: "sqlite", Path: "state/notes.db"},
	}
	writeManifest(t, root, "notes", string(manifestData))
	mkdirAll(t, filepath.Join(root, "opt", "notes", "state"))
	writeFile(t, filepath.Join(root, "opt", "notes", "state", "notes.db"), []byte("database"))
	writeFile(t, filepath.Join(root, "var", "lib", "opsctl", "registrations", "wrong"), []byte("remote-only"))

	if err := apps.ValidateName("notes"); err != nil {
		t.Fatalf("ValidateName rejected discovered service identity: %v", err)
	}
	decoded, err := apps.ParseManifest(manifestData)
	if err != nil {
		t.Fatalf("ParseManifest returned error: %v", err)
	}
	if !reflect.DeepEqual(decoded, wantManifest) {
		t.Fatalf("ParseManifest = %#v, want %#v", decoded, wantManifest)
	}
	services, err := apps.Discover(root)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(services) != 1 || services[0].Name != "notes" || services[0].ManifestError != nil || !reflect.DeepEqual(services[0].Manifest, &wantManifest) {
		t.Fatalf("shared exported contract result = %#v, want manifest %#v", services, wantManifest)
	}

	writeFile(t, filepath.Join(root, "var", "lib", "opsctl", "registrations", "wrong"), []byte("changed remote record"))
	again, err := apps.Discover(root)
	if err != nil || !reflect.DeepEqual(again, services) {
		t.Fatalf("non-host registration record influenced discovery: %#v, %v", again, err)
	}
}

func TestServiceModelOperationsLeaveFilesUnchangedAndManifestHasNoVersion(t *testing.T) {
	root := t.TempDir()
	manifestData := []byte("app = \"ledger\"\n[database]\nengine = \"sqlite\"\npath = \"state/ledger.db\"\n")
	writeManifest(t, root, "ledger", string(manifestData))
	mkdirAll(t, filepath.Join(root, "opt", "ledger", "state"))
	writeFile(t, filepath.Join(root, "opt", "ledger", "state", "ledger.db"), []byte("unchanged database bytes"))
	writeFile(t, filepath.Join(root, "opt", "ledger", "state", "seed.sql"), []byte("must not load"))
	before := snapshotTree(t, root)

	if err := apps.ValidateName("ledger"); err != nil {
		t.Fatalf("ValidateName returned error: %v", err)
	}
	if _, err := apps.ParseManifest(manifestData); err != nil {
		t.Fatalf("ParseManifest returned error: %v", err)
	}
	services, err := apps.Discover(root)
	if err != nil || len(services) != 1 {
		t.Fatalf("Discover = %#v, %v", services, err)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("model operations changed host state:\nbefore: %#v\nafter:  %#v", before, after)
	}
	manifestType := reflect.TypeFor[apps.Manifest]()
	if _, exists := manifestType.FieldByName("Version"); exists {
		t.Fatal("manifest carries an authoritative Version field")
	}
}

func writeManifest(t *testing.T, root, name, contents string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "opt", name, "etc", "manifest.toml"), []byte(contents))
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func serviceNames(services []apps.Service) []string {
	names := make([]string, len(services))
	for index, service := range services {
		names[index] = service.Name
	}
	return names
}

func servicesByName(services []apps.Service) map[string]apps.Service {
	byName := make(map[string]apps.Service, len(services))
	for _, service := range services {
		byName[service.Name] = service
	}
	return byName
}

type treeSnapshotEntry struct {
	mode     os.FileMode
	modified int64
	contents string
}

func snapshotTree(t *testing.T, root string) map[string]treeSnapshotEntry {
	t.Helper()
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open snapshot root %s: %v", root, err)
	}
	defer func() { _ = filesystem.Close() }()

	snapshot := make(map[string]treeSnapshotEntry)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entry := treeSnapshotEntry{mode: info.Mode(), modified: info.ModTime().UnixNano()}
		if info.Mode().IsRegular() {
			contents, err := filesystem.ReadFile(filepath.ToSlash(relative))
			if err != nil {
				return err
			}
			entry.contents = string(contents)
		}
		snapshot[relative] = entry
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snapshot
}

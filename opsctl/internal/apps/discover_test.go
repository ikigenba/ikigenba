package apps_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

// R-8Z3J-U488
// R-G34X-CPC0
func TestDiscoverSelectsImmediateServiceDirectoriesInBytewiseOrder(t *testing.T) {
	missingRoot := t.TempDir()
	services, err := apps.Discover(missingRoot)
	if err != nil || len(services) != 0 {
		t.Fatalf("Discover with missing /opt = %#v, %v; want empty success", services, err)
	}

	root := t.TempDir()
	const dataRoot = apps.DataRoot
	if dataRoot != "/var/opt/ikigenba" {
		t.Fatalf("DataRoot = %q", dataRoot)
	}
	for _, path := range []string{
		"var/opt/ikigenba/zeta/state",
		"opt/Alpha/etc",
		"var/opt/ikigenba/bad_name/state",
		"var/opt/ikigenba/data-only/state",
		"opt/both/etc",
		"var/opt/ikigenba/both/state",
		"opt/legacy/state",
		"var/opt/ikigenba/nested/child/state",
		"var/opt/ikigenba/no-state/etc",
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

	blockedData := t.TempDir()
	writeFile(t, filepath.Join(blockedData, apps.DataRoot), []byte("not a directory"))
	if services, err = apps.Discover(blockedData); err == nil || services != nil {
		t.Fatalf("Discover with non-directory DataRoot = %#v, %v; want enumeration error", services, err)
	}

	dataOnlyRoot := t.TempDir()
	mkdirAll(t, filepath.Join(dataOnlyRoot, apps.DataRoot, "crm", "state"))
	services, err = apps.Discover(dataOnlyRoot)
	if err != nil || !reflect.DeepEqual(serviceNames(services), []string{"crm"}) {
		t.Fatalf("Discover with only DataRoot = %#v, %v", services, err)
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

	dataEscapeRoot := t.TempDir()
	mkdirAll(t, filepath.Join(dataEscapeRoot, "var", "opt"))
	if err := os.Symlink(outside, filepath.Join(dataEscapeRoot, apps.DataRoot)); err != nil {
		t.Fatalf("create escaping DataRoot symlink: %v", err)
	}
	if services, err = apps.Discover(dataEscapeRoot); err == nil || services != nil {
		t.Fatalf("Discover through DataRoot symlink outside root = %#v, %v", services, err)
	}

	nestedRoot := t.TempDir()
	outsideEtc := t.TempDir()
	writeFile(t, filepath.Join(outsideEtc, "manifest.toml"), []byte("app = \"etc-escape\"\n"))
	mkdirAll(t, filepath.Join(nestedRoot, apps.DataRoot, "etc-escape", "state"))
	mkdirAll(t, filepath.Join(nestedRoot, "opt", "etc-escape"))
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
	mkdirAll(t, filepath.Join(nestedRoot, apps.DataRoot, "state-escape"))
	if err := os.Symlink(outsideState, filepath.Join(nestedRoot, apps.DataRoot, "state-escape", "state")); err != nil {
		t.Fatalf("create escaping state symlink: %v", err)
	}

	if _, err := apps.ReadLayout(nestedRoot); err == nil {
		t.Fatal("ReadLayout did not report failure to examine escaping etc")
	}

}

// R-90BG-7VYX
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

// R-91JC-LNPM
func TestDiscoverUsesDirectoryIdentityAndRetainsStateOnlyServices(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "directory-name", "app = \"other-name\"\n")
	mkdirAll(t, filepath.Join(root, apps.DataRoot, "state-only", "state"))
	writeFile(t, filepath.Join(root, apps.DataRoot, "state-only", "state", "service.db"), []byte("data"))
	writeFile(t, filepath.Join(root, "opt", "directory-name", "unrelated-binary"), []byte("not executable"))

	services, err := apps.Discover(root)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	byName := servicesByName(services)
	if got := byName["directory-name"]; got.Name != "directory-name" || got.Manifest != nil || got.ManifestError == nil {
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
		App:       "notes",
		Secrets:   []string{},
		Env:       map[string]string{},
		Database:  &apps.Database{Engine: "sqlite", Path: "state/notes.db"},
		Resources: apps.Resources{Slice: "apps", MemoryMax: 134217728, GoMemoryLimit: 100663296, CPUWeight: 100},
		Home:      apps.Home{Group: "application"},
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

func TestEnvironmentDirectoriesDoNotMakeServices(t *testing.T) {
	// R-9571-QYXP
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, "opt", "package", "etc"))
	mkdirAll(t, filepath.Join(root, apps.DataRoot, "data", "state"))
	before, err := apps.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package", "data", "environment-only"} {
		writeFile(t, filepath.Join(root, apps.EnvRoot, name, "env"), []byte("SECRET=value\n"))
	}
	after, err := apps.Discover(root)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("environment affected discovery: before %#v after %#v: %v", before, after, err)
	}
}

// R-8VFU-OT05 R-8WNR-2KQU
func TestLayoutUsesCurrentEntryBeforePerAppPackages(t *testing.T) {
	if string(apps.Fresh) != "fresh host" || string(apps.PerApp) != "per app" || string(apps.Released) != "releases" {
		t.Fatal("layout constants")
	}
	for _, kind := range []string{"absent", "package", "ikigenba", "data", "dangling", "regular", "directory"} {
		root := t.TempDir()
		want := apps.Fresh
		switch kind {
		case "package":
			mkdirAll(t, filepath.Join(root, "opt/notes/etc"))
			want = apps.PerApp
		case "ikigenba":
			mkdirAll(t, filepath.Join(root, "opt/ikigenba/etc"))
		case "data":
			mkdirAll(t, filepath.Join(root, apps.DataRoot, "notes/state"))
		case "dangling":
			mkdirAll(t, filepath.Join(root, "opt/ikigenba"))
			if err := os.Symlink("releases/missing", filepath.Join(root, "opt/ikigenba/current")); err != nil {
				t.Fatal(err)
			}
			want = apps.Released
		case "regular":
			writeFile(t, filepath.Join(root, "opt/ikigenba/current"), []byte("entry"))
			want = apps.Released
		case "directory":
			mkdirAll(t, filepath.Join(root, "opt/ikigenba/current"))
			want = apps.Released
		}
		layout, err := apps.ReadLayout(root)
		if err != nil || layout != want {
			t.Fatalf("%s => %q %v", kind, layout, err)
		}
	}
}

// R-8Z3J-U488 R-8T01-X9IR R-90BG-7VYX R-91JC-LNPM R-9PXC-92JI
func TestDiscoverReleasedPackagesAndDataOnlyServices(t *testing.T) {
	root := t.TempDir()
	sha := strings.Repeat("b", 40)
	folder := filepath.Join(root, release.ReleasesDir, sha)
	releasedPackage(t, folder, "valid", "app = \"valid\"\n")
	releasedPackage(t, folder, "broken", "broken TOML")
	mkdirAll(t, filepath.Join(folder, "opsctl/bin"))
	writeFile(t, filepath.Join(folder, "opsctl/bin/opsctl"), []byte("binary"))
	for _, name := range []string{"no-manifest", "no-binary", "directory-binary"} {
		mkdirAll(t, filepath.Join(folder, name, "bin"))
	}
	writeFile(t, filepath.Join(folder, "no-manifest/bin/no-manifest"), []byte("binary"))
	writeFile(t, filepath.Join(folder, "no-binary/etc/manifest.toml"), []byte(""))
	mkdirAll(t, filepath.Join(folder, "directory-binary/bin/directory-binary"))
	writeFile(t, filepath.Join(folder, "directory-binary/etc/manifest.toml"), []byte(""))
	if err := os.Symlink("valid", filepath.Join(folder, "linked")); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, root, "old-only", "app = \"old-only\"\n")
	writeManifest(t, root, "valid", "app = \"other\"\n")
	mkdirAll(t, filepath.Join(root, apps.DataRoot, "kept/state"))
	mkdirAll(t, filepath.Join(root, apps.DataRoot, "valid/state"))
	if err := os.Symlink("releases/"+sha, filepath.Join(root, release.CurrentLink)); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	services, err := apps.Discover(root)
	if err != nil || !reflect.DeepEqual(serviceNames(services), []string{"broken", "kept", "valid"}) {
		t.Fatalf("%#v %v", services, err)
	}
	byName := servicesByName(services)
	if got := byName["valid"]; got.Dir != release.CurrentLink+"/valid" || got.Manifest == nil || got.Manifest.App != "valid" {
		t.Fatal(got)
	}
	if got := byName["broken"]; got.Dir != release.CurrentLink+"/broken" || got.Manifest != nil || got.ManifestError == nil {
		t.Fatal(got)
	}
	if got := byName["kept"]; got.Dir != "" || got.Manifest != nil || got.ManifestError != nil {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, root)) {
		t.Fatal("discovery wrote filesystem")
	}
	dangling := t.TempDir()
	mkdirAll(t, filepath.Join(dangling, "opt/ikigenba"))
	if err := os.Symlink("releases/"+sha, filepath.Join(dangling, release.CurrentLink)); err != nil {
		t.Fatal(err)
	}
	if got, err := apps.Discover(dangling); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
func releasedPackage(t *testing.T, folder, name, manifest string) {
	t.Helper()
	writeFile(t, filepath.Join(folder, name, "bin", name), []byte("binary"))
	writeFile(t, filepath.Join(folder, name, "etc/manifest.toml"), []byte(manifest))
}

// R-U9YN-KYA0 R-UB6J-YQ0P
func TestDiscoverReleaseUsesExplicitReleaseWithoutChangingLinks(t *testing.T) {
	root := t.TempDir()
	sha := strings.Repeat("c", 40)
	folder := filepath.Join(root, release.ReleasesDir, sha)
	releasedPackage(t, folder, "explicit", "app = \"explicit\"\n")
	mkdirAll(t, filepath.Join(root, apps.DataRoot, "kept/state"))
	writeManifest(t, root, "per-app", "app = \"per-app\"\n")
	for _, current := range []bool{false, true} {
		if current {
			if err := os.Symlink("releases/"+strings.Repeat("d", 40), filepath.Join(root, release.CurrentLink)); err != nil {
				t.Fatal(err)
			}
		}
		before := snapshotTree(t, root)
		services, err := apps.DiscoverRelease(root, sha)
		if err != nil || !reflect.DeepEqual(serviceNames(services), []string{"explicit", "kept"}) {
			t.Fatal(services, err)
		}
		if services[0].Dir != release.ReleasesDir+"/"+sha+"/explicit" || services[0].Manifest == nil || services[1].Dir != "" {
			t.Fatal(services)
		}
		if !reflect.DeepEqual(before, snapshotTree(t, root)) {
			t.Fatal("explicit discovery wrote")
		}
	}
	if _, err := apps.DiscoverRelease(root, strings.Repeat("e", 40)); err == nil {
		t.Fatal("missing release accepted")
	}
}

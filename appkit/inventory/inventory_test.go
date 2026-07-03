package inventory

import (
	"os"
	"path/filepath"
	"testing"
)

// writeManifest creates <root>/<svc>/etc/current/manifest.env with the given contents.
func writeManifest(t *testing.T, root, svc, contents string) {
	t.Helper()
	dir := filepath.Join(root, svc, "etc", "current")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.env"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeSiblingManifest(t *testing.T, root, svc, contents string) {
	t.Helper()
	dir := filepath.Join(root, svc, "etc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.env"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write sibling manifest: %v", err)
	}
}

func writeVersionManifest(t *testing.T, root, svc, version, contents string) {
	t.Helper()
	dir := filepath.Join(root, svc, "etc", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.env"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write version manifest: %v", err)
	}
}

func pointCurrent(t *testing.T, root, svc, version string) {
	t.Helper()
	link := filepath.Join(root, svc, "etc", "current")
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove current: %v", err)
	}
	if err := os.Symlink(version, link); err != nil {
		t.Fatalf("symlink current to %s: %v", version, err)
	}
}

// R-YO06-9I18
func TestReadUsesCurrentManifestNotSiblingManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "crm", "APP=crm\nMOUNT=/srv/crm/\nPORT=3100\nMCP=true\n")
	writeSiblingManifest(t, root, "ledger", "APP=ledger\nMOUNT=/srv/ledger/\nPORT=3101\nMCP=true\n")

	got, err := Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d services, want 1: %+v", len(got), got)
	}
	if got[0].Name != "crm" {
		t.Errorf("Name = %q, want crm", got[0].Name)
	}
}

// R-YP82-N9RX
func TestReadFollowsRepointedCurrentSymlink(t *testing.T) {
	root := t.TempDir()
	writeVersionManifest(t, root, "crm", "verA", "APP=crm\nMOUNT=/srv/crm/\nPORT=3100\nMCP=true\n")
	writeVersionManifest(t, root, "crm", "verB", "APP=crm\nMOUNT=/srv/crm/\nPORT=3100\nMCP=false\n")
	pointCurrent(t, root, "crm", "verA")

	got, err := Read(root)
	if err != nil {
		t.Fatalf("Read verA: %v", err)
	}
	if len(got) != 1 || got[0].Name != "crm" {
		t.Fatalf("Read verA = %+v, want crm listed", got)
	}

	pointCurrent(t, root, "crm", "verB")
	got, err = Read(root)
	if err != nil {
		t.Fatalf("Read verB: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Read verB = %+v, want no services", got)
	}
}

// TestReadKeepsOnlyMCP: the dashboard manifest (no MCP) is omitted, a garbled
// manifest is skipped, and only the crm service (MCP=true) is returned.
func TestReadKeepsOnlyMCP(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "dashboard", "APP=dashboard\nMOUNT=/\nDEFAULT=true\nPORT=3000\n")
	writeManifest(t, root, "crm", "# crm service\nAPP=crm\nMOUNT=/srv/crm/\nPORT=3100\nMCP=true\nFEED=/feed\n")
	writeManifest(t, root, "broken", "this is not = = valid\n\x00garbage")

	got, err := Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d services, want 1: %+v", len(got), got)
	}
	if got[0].Name != "crm" {
		t.Errorf("Name = %q, want crm", got[0].Name)
	}
	if got[0].Mount != "/srv/crm/" {
		t.Errorf("Mount = %q, want /srv/crm/", got[0].Mount)
	}
	if got[0].Port != "3100" {
		t.Errorf("Port = %q, want 3100", got[0].Port)
	}
	if got[0].Feed != "/feed" {
		t.Errorf("Feed = %q, want /feed", got[0].Feed)
	}
}

// TestReadConsumerHasNoFeed: an MCP service without a FEED key (a consumer) is
// listed with an empty Feed but a populated Port.
func TestReadConsumerHasNoFeed(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "notify", "APP=notify\nMOUNT=/srv/notify/\nPORT=3201\nMCP=true\nCONSUMES=crm\n")

	got, err := Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d services, want 1: %+v", len(got), got)
	}
	if got[0].Port != "3201" {
		t.Errorf("Port = %q, want 3201", got[0].Port)
	}
	if got[0].Feed != "" {
		t.Errorf("Feed = %q, want empty", got[0].Feed)
	}
}

// TestReadSortsByName: multiple MCP services come back sorted by Name regardless
// of glob/filesystem order.
func TestReadSortsByName(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "wiki", "APP=wiki\nMOUNT=/srv/wiki/\nPORT=3001\nMCP=true\n")
	writeManifest(t, root, "crm", "APP=crm\nMOUNT=/srv/crm/\nPORT=3100\nMCP=true\n")
	writeManifest(t, root, "ledger", "APP=ledger\nMOUNT=/srv/ledger/\nPORT=3101\nMCP=true\n")

	got, err := Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []string{"crm", "ledger", "wiki"}
	if len(got) != len(want) {
		t.Fatalf("got %d services, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("services[%d].Name = %q, want %q", i, got[i].Name, name)
		}
	}
}

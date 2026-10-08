package apps_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestEnvironmentPublicationCreatesDirectoriesAndReplacesWholeFile(t *testing.T) {
	// R-02XL-DHW4 R-DOSL-YDU7 R-9571-QYXP
	root := t.TempDir()
	if apps.EnvRoot != "/etc/opt/ikigenba" {
		t.Fatalf("EnvRoot %q", apps.EnvRoot)
	}
	mask := syscall.Umask(0o077)
	defer syscall.Umask(mask)
	destination := filepath.Join(root, apps.EnvRoot, "notes", "env")
	if err := apps.PublishEnvironment(root, "notes", []byte("FIRST=one\n")); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"etc/opt", "etc/opt/ikigenba", "etc/opt/ikigenba/notes"} {
		assertMode(t, filepath.Join(root, directory), 0o755)
	}
	before, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	if err := filesystem.Chmod("etc/opt/ikigenba/notes", 0o711); err != nil {
		t.Fatal(err)
	}
	if err := apps.PublishEnvironment(root, "notes", []byte("SECOND=two\n")); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("environment edited in place")
	}
	assertFile(t, destination, "SECOND=two\n")
	assertMode(t, destination, 0o600)
	assertMode(t, filepath.Dir(destination), 0o711)
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 1 || entries[0].Name() != "env" {
		t.Fatalf("entries %v error %v", entries, err)
	}
}

func TestInstallMigratesLegacyEnvironmentWithoutStoppingSocket(t *testing.T) {
	// R-DQ0I-C5KW R-DUW3-V8JO R-9571-QYXP
	root := t.TempDir()
	fixture := newCompletedInstallFixture(t, root, true)
	unitPath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")
	writeFixture(t, unitPath, []byte("[Service]\nEnvironmentFile="+filepath.Join(root, "opt", "notes", "etc", "env")+"\n"), 0o644)
	writeFixture(t, filepath.Join(root, "opt", "notes", "bin", "notes"), []byte("old binary"), 0o750)
	writeFixture(t, filepath.Join(root, "opt", "notes", "etc", "manifest.toml"), []byte("app='notes'\n"), 0o640)
	if _, err := os.Lstat(filepath.Join(root, apps.EnvRoot, "notes")); !os.IsNotExist(err) {
		t.Fatalf("legacy fixture has external env directory: %v", err)
	}
	legacy := filepath.Join(root, "opt", "notes", "etc", "env")
	writeFixture(t, legacy, []byte("LEGACY=old\n"), 0o600)
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy environment survived: %v", err)
	}
	assertFile(t, filepath.Join(root, apps.EnvRoot, "notes", "env"), "DRAIN_SECONDS=5\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n")
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	unit, err := filesystem.ReadFile("etc/systemd/system/ikigenba-notes.service")
	if err != nil || !strings.Contains(string(unit), "EnvironmentFile="+filepath.Join(root, apps.EnvRoot, "notes", "env")+"\n") {
		t.Fatalf("published unit %q error %v", unit, err)
	}
	migratedRoot := t.TempDir()
	migrated := newCompletedInstallFixture(t, migratedRoot, true)
	writeFixture(t, filepath.Join(migratedRoot, "opt", "notes", "bin", "notes"), []byte("old binary"), 0o750)
	writeFixture(t, filepath.Join(migratedRoot, "opt", "notes", "etc", "manifest.toml"), []byte("app='notes'\n"), 0o640)
	writeFixture(t, filepath.Join(migratedRoot, apps.EnvRoot, "notes", "env"), []byte("LEGACY=old\n"), 0o600)
	writeFixture(t, filepath.Join(migratedRoot, "etc", "systemd", "system", "ikigenba-notes.service"), []byte("[Service]\nEnvironmentFile="+filepath.Join(migratedRoot, apps.EnvRoot, "notes", "env")+"\n"), 0o644)
	if err := migrated.run(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixture.reports, migrated.reports) {
		t.Fatalf("legacy reports %#v differ from migrated reports %#v", fixture.reports, migrated.reports)
	}
	unpack := false
	for _, report := range fixture.reports {
		if report.step == "unpack" && report.success && report.detail == "/opt/notes" {
			unpack = true
		}
	}
	if !unpack {
		t.Fatalf("missing unpack report: %#v", fixture.reports)
	}
	restart := false
	for _, command := range fixture.commands {
		if command.name == "systemctl" && len(command.args) > 1 {
			if command.args[0] == "stop" {
				t.Fatalf("unexpected stop: %v", command)
			}
			if command.args[0] == "restart" && command.args[1] == "ikigenba-notes.socket" {
				t.Fatal("socket restarted")
			}
			if command.args[0] == "restart" && command.args[1] == "ikigenba-notes.service" {
				restart = true
			}
		}
	}
	if !restart {
		t.Fatal("running app was not restarted")
	}
}

func TestSetupTimeoutsMigrationGuardPrecedesManifestErrors(t *testing.T) {
	// R-DSGB-3P2A R-ZI7A-VEAB
	root := t.TempDir()
	store := installStoreAt(t, root, nil)
	for _, name := range []string{"alpha", "zeta"} {
		writeFixture(t, filepath.Join(root, "opt", name, "bin", name), []byte("binary"), 0o750)
		writeFixture(t, filepath.Join(root, "opt", name, "etc", "manifest.toml"), []byte("app='"+name+"'\n[resources]\nio_weight=1"), 0o640)
	}
	writeFixture(t, filepath.Join(root, apps.EnvRoot, "alpha", "env"), []byte("KEEP=unchanged\n"), 0o600)
	err := apps.SetupTimeouts(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, _ host.Command) (host.Result, error) {
		t.Fatal("command on refusal")
		return host.Result{}, nil
	}}, store)
	if err == nil || err.Error() != "zeta: /opt/zeta/etc/env has not moved; install zeta first" {
		t.Fatalf("error %v", err)
	}
	assertFile(t, filepath.Join(root, apps.EnvRoot, "alpha", "env"), "KEEP=unchanged\n")
	if _, err := os.Stat(filepath.Join(root, apps.EnvRoot, "zeta")); !os.IsNotExist(err) {
		t.Fatalf("created missing env dir: %v", err)
	}
}

func TestInstallKeepsPackagedEtcEnvSeparateFromHostEnvironment(t *testing.T) {
	// R-EEEH-ZKES R-DUW3-V8JO R-DW40-90AD R-9571-QYXP
	root := t.TempDir()
	artifact := tarEntries(t, []installTarEntry{
		regularEntry("etc/manifest.toml", []byte("app='notes'\n"), 0o644),
		regularEntry("etc/env", []byte("PACKAGED=content\n"), 0o644),
		regularEntry("bin/notes", []byte("binary"), 0o755),
	})
	fixture := newCompletedInstallFixture(t, root, false)
	fixture.archive = artifact
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "opt", "notes", "etc", "env"), "PACKAGED=content\n")
	assertMode(t, filepath.Join(root, "opt", "notes", "etc", "env"), 0o640)
	assertFile(t, filepath.Join(root, apps.EnvRoot, "notes", "env"), "DRAIN_SECONDS=5\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n")
}

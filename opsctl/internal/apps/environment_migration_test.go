package apps_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestEnvironmentPublicationCreatesDirectoriesAndReplacesWholeFile(t *testing.T) {
	// R-02XL-DHW4 R-T9O0-8A6J R-9571-QYXP
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

func TestSetupTimeoutsMigrationGuardPrecedesManifestErrors(t *testing.T) {
	// R-CM1B-RL1J R-ZI7A-VEAB
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
	if err == nil || err.Error() != "zeta: /opt/zeta/etc/env has not moved" {
		t.Fatalf("error %v", err)
	}
	assertFile(t, filepath.Join(root, apps.EnvRoot, "alpha", "env"), "KEEP=unchanged\n")
	if _, err := os.Stat(filepath.Join(root, apps.EnvRoot, "zeta")); !os.IsNotExist(err) {
		t.Fatalf("created missing env dir: %v", err)
	}
}

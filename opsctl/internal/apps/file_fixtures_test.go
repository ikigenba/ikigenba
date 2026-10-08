package apps_test

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, name string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, mode); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, name, want string) {
	t.Helper()
	filesystem, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		t.Fatalf("open root for %s: %v", name, err)
	}
	defer func() { _ = filesystem.Close() }()
	data, err := filesystem.ReadFile(filepath.Base(name))
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", name, data, err, want)
	}
}

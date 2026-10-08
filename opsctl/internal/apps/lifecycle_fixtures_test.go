package apps_test

import (
	"os"
	"path/filepath"
	"testing"
)

type uninstallReport struct {
	step    string
	detail  string
	success bool
}

func writeFixturePath(t *testing.T, root, name, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func removeFixturePath(t *testing.T, root, name string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
}

func readFixturePath(root, name string) ([]byte, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()
	return filesystem.ReadFile(filepath.ToSlash(name))
}

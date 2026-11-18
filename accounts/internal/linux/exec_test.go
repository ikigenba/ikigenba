package linux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCommand(t *testing.T) {
	// R-AWMT-N6LM
	const name = "probe"
	base := t.TempDir()
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	dirs := []string{"local-sbin", "local-bin", "sbin", "bin"}
	var searchDirs []string
	for _, dir := range dirs {
		if err := root.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		searchDirs = append(searchDirs, filepath.Join(base, dir))
	}
	path := strings.Join(searchDirs, ":")
	first, second := filepath.Join(dirs[0], name), filepath.Join(dirs[1], name)
	if err := root.Mkdir(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(second, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveCommand(name, path); err == nil {
		t.Fatalf("resolveCommand found ineligible candidate %q", got)
	}
	check := func(want string) {
		t.Helper()
		want = filepath.Join(base, want)
		if got, err := resolveCommand(name, path); err != nil || got != want {
			t.Fatalf("resolveCommand = %q, %v, want %q, nil", got, err, want)
		}
	}
	for _, dir := range []string{dirs[3], dirs[2]} {
		candidate := filepath.Join(dir, name)
		if err := root.WriteFile(candidate, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := root.Chmod(candidate, 0o700); err != nil {
			t.Fatal(err)
		}
		check(candidate)
	}
	if err := root.Chmod(second, 0o700); err != nil {
		t.Fatal(err)
	}
	check(second)
	if err := root.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(first, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Chmod(first, 0o700); err != nil {
		t.Fatal(err)
	}
	check(first)
}

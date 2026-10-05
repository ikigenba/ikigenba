package scripts_test

import (
	"bytes"
	"embed"
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
)

var (
	_ func() fs.FS    = scripts.Assets
	_ func() embed.FS = scripts.Etc
)

// R-J4Q9-R1NN R-J5Y6-4TEC
func TestAssets(t *testing.T) {
	first := scripts.Assets()
	names := []string{"about.html", "landing.html", "notfound.html", "run.html", "script.html", "unavailable.html"}
	assertEntries(t, first, ".", names, false)
	contents := make(map[string][]byte)
	for _, name := range names {
		data, err := fs.ReadFile(first, name)
		if err != nil {
			t.Fatal(err)
		}
		contents[name] = data
	}
	t.Chdir(t.TempDir())
	for range 3 {
		next := scripts.Assets()
		assertEntries(t, next, ".", names, false)
		for _, name := range names {
			data, err := fs.ReadFile(next, name)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, contents[name]) {
				t.Fatalf("%s contents changed", name)
			}
		}
	}
}

// R-S0RY-EOPY R-J8DY-WCVQ R-S1ZU-SGGN
func TestEtc(t *testing.T) {
	first := scripts.Etc()
	assertEntries(t, first, ".", []string{"etc"}, true)
	assertEntries(t, first, "etc", []string{"manifest.toml"}, false)
	data, err := fs.ReadFile(first, "etc/manifest.toml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != cli.Manifest {
		t.Fatalf("embedded manifest differs: %q", data)
	}
	t.Chdir(t.TempDir())
	for range 3 {
		next := scripts.Etc()
		assertEntries(t, next, ".", []string{"etc"}, true)
		assertEntries(t, next, "etc", []string{"manifest.toml"}, false)
		again, err := fs.ReadFile(next, "etc/manifest.toml")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, again) {
			t.Fatal("manifest contents changed")
		}
	}
}

func assertEntries(t *testing.T, files fs.FS, dir string, want []string, directories bool) {
	t.Helper()
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if directories {
			if !info.IsDir() {
				t.Fatalf("%s is not a directory", entry.Name())
			}
		} else if !info.Mode().IsRegular() {
			t.Fatalf("%s is not regular", entry.Name())
		}
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("%s entries %v, want %v", dir, names, want)
	}
}

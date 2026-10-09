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

// R-RHW4-YAD2 R-RJ41-C23R
func TestAssets(t *testing.T) {
	first := scripts.Assets()
	names := []string{"about.html", "landing.html", "notfound.html", "run.html", "script.html", "tools.html", "unavailable.html"}
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

// R-6KM0-WA0K R-6LTX-A1R9 R-OO2Y-2753 R-S1ZU-SGGN
func TestEtc(t *testing.T) {
	first := scripts.Etc()
	assertEntries(t, first, ".", []string{"etc"}, true)
	assertEntries(t, first, "etc", []string{"manifest.toml", "nginx.conf"}, false)
	data, err := fs.ReadFile(first, "etc/manifest.toml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != cli.Manifest {
		t.Fatalf("embedded manifest differs: %q", data)
	}
	nginx, err := fs.ReadFile(first, "etc/nginx.conf")
	if err != nil {
		t.Fatal(err)
	}
	if string(nginx) != cli.NginxConf {
		t.Fatal("embedded nginx differs")
	}
	t.Chdir(t.TempDir())
	for range 3 {
		next := scripts.Etc()
		got, err := fs.ReadFile(next, "etc/nginx.conf")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, nginx) {
			t.Fatal("nginx changed")
		}
		assertEntries(t, next, ".", []string{"etc"}, true)
		assertEntries(t, next, "etc", []string{"manifest.toml", "nginx.conf"}, false)
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

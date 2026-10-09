package sites_test

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
)

// R-AV6W-UT0P R-XRL4-GF9Z R-XST0-U70O R-XU0X-7YRD
func TestEmbeddedFiles(t *testing.T) {
	assets, etc := sites.Assets(), sites.Etc()
	read := func(f fs.FS, names []string) map[string][]byte {
		t.Helper()
		entries, err := fs.ReadDir(f, ".")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != len(names) {
			t.Fatalf("entries: %v", entries)
		}
		result := make(map[string][]byte)
		for i, name := range names {
			if entries[i].Name() != name || !entries[i].Type().IsRegular() {
				t.Fatalf("entry: %v", entries[i])
			}
			body, err := fs.ReadFile(f, name)
			if err != nil {
				t.Fatal(err)
			}
			result[name] = body
		}
		return result
	}
	names := []string{"about.html", "landing.html", "notfound.html", "tools.html", "unavailable.html"}
	originalAssets := read(assets, names)
	originalEtc := read(etc, []string{"manifest.toml"})
	t.Chdir(t.TempDir())
	for i := 0; i < 2; i++ {
		for name, body := range read(sites.Assets(), names) {
			if !bytes.Equal(body, originalAssets[name]) {
				t.Fatalf("asset changed: %s", name)
			}
		}
		if !bytes.Equal(read(sites.Etc(), []string{"manifest.toml"})["manifest.toml"], originalEtc["manifest.toml"]) {
			t.Fatal("manifest changed")
		}
	}
}

// R-SP4Q-LUZT
func TestManifestCopy(t *testing.T) {
	body, err := fs.ReadFile(sites.Etc(), "manifest.toml")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != cli.Manifest {
		t.Fatal("embedded manifest differs")
	}
}

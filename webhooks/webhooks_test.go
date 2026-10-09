package webhooks_test

import (
	"bytes"
	"io/fs"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/cli"
)

func names(t *testing.T, files fs.FS) []string {
	t.Helper()
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("%s is not regular", entry.Name())
		}
		result = append(result, entry.Name())
	}
	return result
}

func readFiles(t *testing.T, files fs.FS) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for _, name := range names(t, files) {
		b, err := fs.ReadFile(files, name)
		if err != nil {
			t.Fatal(err)
		}
		result[name] = b
	}
	return result
}

// R-WMMS-CRX7 R-WNUO-QJNW R-WP2L-4BEL
func TestEmbeddedFileSets(t *testing.T) {
	cases := []struct {
		name   string
		source func() fs.FS
		want   []string
	}{
		{"assets", webhooks.Assets, []string{"about.html", "landing.html", "notfound.html", "tools.html"}},
		{"etc", webhooks.Etc, []string{"manifest.toml", "nginx.conf"}},
		{"migrations", webhooks.Migrations, []string{"0001_webhooks.sql"}},
	}
	expected := make([]map[string][]byte, len(cases))
	for i, tc := range cases {
		if got := names(t, tc.source()); !slices.Equal(got, tc.want) {
			t.Fatalf("%s files = %v, want %v", tc.name, got, tc.want)
		}
		expected[i] = readFiles(t, tc.source())
	}
	for range 2 {
		t.Chdir(t.TempDir())
		for i, tc := range cases {
			got := readFiles(t, tc.source())
			if len(got) != len(expected[i]) {
				t.Fatalf("%s file count changed", tc.name)
			}
			for name, want := range expected[i] {
				if !bytes.Equal(got[name], want) {
					t.Fatalf("contents changed for %s", name)
				}
			}
		}
	}
}

// R-XN7R-TW61
func TestEtcCopiesConstants(t *testing.T) {
	manifest, err := fs.ReadFile(webhooks.Etc(), "manifest.toml")
	if err != nil || string(manifest) != cli.Manifest {
		t.Fatalf("manifest.toml differs from cli.Manifest: %v", err)
	}
	nginx, err := fs.ReadFile(webhooks.Etc(), "nginx.conf")
	if err != nil || string(nginx) != cli.NginxConf {
		t.Fatalf("nginx.conf differs from cli.NginxConf: %v", err)
	}
}

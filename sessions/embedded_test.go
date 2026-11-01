package sessions_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
)

// R-1TTW-UGC1 R-1V1T-882Q R-1W9P-LZTF
func TestEmbeddedFilesIndependentOfWorkingDirectory(t *testing.T) {
	tests := []struct {
		name  string
		open  func() fs.FS
		names []string
	}{
		{"assets", sessions.Assets, []string{"about.html", "landing.html", "notfound.html", "tools.html"}},
		{"etc", sessions.Etc, []string{"manifest.toml", "nginx.conf"}},
		{"migrations", sessions.Migrations, []string{"0001_sessions.sql"}},
	}
	initial := make(map[string]string)
	for range 3 {
		t.Chdir(t.TempDir())
		for _, tc := range tests {
			entries, err := fs.ReadDir(tc.open(), ".")
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, entry := range entries {
				info, err := entry.Info()
				if err != nil {
					t.Fatal(err)
				}
				if !info.Mode().IsRegular() {
					t.Fatalf("%s/%s is not regular", tc.name, entry.Name())
				}
				names = append(names, entry.Name())
				content, err := fs.ReadFile(tc.open(), entry.Name())
				if err != nil {
					t.Fatal(err)
				}
				key := tc.name + "/" + entry.Name()
				if old, ok := initial[key]; ok && old != string(content) {
					t.Fatalf("%s changed between calls", key)
				}
				initial[key] = string(content)
			}
			if !slices.Equal(names, tc.names) {
				t.Fatalf("%s entries = %v; want %v", tc.name, names, tc.names)
			}
		}
	}
}

// R-6YZO-233K R-G51W-VUI1
func TestEmbeddedDeploymentConstants(t *testing.T) {
	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		got, err := fs.ReadFile(sessions.Etc(), name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s differs from its CLI constant", name)
		}
	}
}

// R-6XRR-OBCV R-G06B-CRJ9 R-G3U0-I2RC R-35DZ-RI94
func TestDeploymentConstants(t *testing.T) {
	const name = pages.ServiceName
	const manifest string = cli.Manifest
	const nginx string = cli.NginxConf
	if name != "sessions" {
		t.Fatalf("service name = %q", name)
	}
	want := "app = \"sessions\"\ndescription = \"" + pages.Description + "\"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/sessions.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n"
	if manifest != want {
		t.Fatalf("manifest = %q; want %q", manifest, want)
	}
	if nginx != "location = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatalf("nginx configuration = %q", nginx)
	}
}

// R-G1E7-QJ9Y
func TestDescriptionFitsManifest(t *testing.T) {
	if pages.Description == "" || !utf8.ValidString(pages.Description) || strings.ContainsAny(pages.Description, "\"\\") {
		t.Fatal("description is not a literal TOML string value")
	}
	for _, r := range pages.Description {
		if unicode.IsControl(r) {
			t.Fatalf("description contains control rune %U", r)
		}
	}
}

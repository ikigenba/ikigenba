package accounts_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cli"
)

// R-C6C9-TACZ R-C7K6-723O R-C8S2-KTUD
func TestEmbeddedResources(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func() fs.FS
		want []string
	}{
		{"assets", accounts.Assets, []string{"about.html", "landing.html", "notfound.html", "tools.html"}},
		{"etc", accounts.Etc, []string{"manifest.toml", "nginx.conf"}},
		{"migrations", accounts.Migrations, []string{"0001_accounts.sql"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 2 {
				t.Chdir(t.TempDir())
				files := tc.open()
				entries, err := fs.ReadDir(files, ".")
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
						t.Fatalf("not regular: %s", entry.Name())
					}
					names = append(names, entry.Name())
					if _, err := fs.ReadFile(files, entry.Name()); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(names, tc.want) {
					t.Fatalf("entries %v, want %v", names, tc.want)
				}
			}
		})
	}
}

// R-V633-24YQ R-M7N8-UER0
func TestEmbeddedDeploymentContents(t *testing.T) {
	for name, want := range map[string]string{"manifest.toml": cli.Manifest, "nginx.conf": cli.NginxConf} {
		got, err := fs.ReadFile(accounts.Etc(), name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("embedded %s = %q, want %q", name, got, want)
		}
	}
}

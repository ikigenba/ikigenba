package ledger_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/ledger"
)

// R-64ZP-XY7I R-667M-BPY7 R-67FI-PHOW
func TestEmbeddedResources(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func() fs.FS
		want []string
	}{
		{"assets", ledger.Assets, []string{"about.html", "landing.html", "notfound.html", "tools.html"}},
		{"etc", ledger.Etc, []string{"manifest.toml", "nginx.conf"}},
		{"migrations", ledger.Migrations, []string{"0001_ledger.sql"}},
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

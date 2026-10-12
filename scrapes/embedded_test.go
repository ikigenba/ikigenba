package scrapes_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes"
)

// R-Y6P8-IJ6F R-Y7X4-WAX4 R-Y951-A2NT
func TestEmbeddedRoots(t *testing.T) {
	for _, dir := range []string{"", t.TempDir()} {
		if dir != "" {
			t.Chdir(dir)
		}
		for _, tc := range []struct {
			f     func() fs.FS
			names []string
		}{{scrapes.Assets, []string{"about.html", "landing.html", "notfound.html", "row.html", "run.html", "tools.html"}}, {scrapes.Etc, []string{"manifest.toml", "nginx.conf"}}, {scrapes.Migrations, []string{"0001_runs.sql", "0002_rows.sql"}}} {
			for range 2 {
				entries, e := fs.ReadDir(tc.f(), ".")
				if e != nil {
					t.Fatal(e)
				}
				names := []string{}
				for _, entry := range entries {
					if !entry.Type().IsRegular() {
						t.Fatal(entry.Name())
					}
					names = append(names, entry.Name())
					b, e := fs.ReadFile(tc.f(), entry.Name())
					if e != nil || len(b) == 0 {
						t.Fatal(entry.Name(), e)
					}
				}
				if !reflect.DeepEqual(names, tc.names) {
					t.Fatal(names)
				}
			}
		}
	}
}

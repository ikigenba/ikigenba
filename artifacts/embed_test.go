package artifacts_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/artifacts"
)

// R-YNLV-967C R-YOTR-MXY1 R-YQ1O-0POQ
func TestInputs(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		get  func() fs.FS
		want []string
	}{{artifacts.Assets, []string{"about.html", "artifact.html", "landing.html", "notfound.html", "tools.html"}}, {artifacts.Etc, []string{"manifest.toml", "nginx.conf"}}, {artifacts.Migrations, []string{"0001_artifacts.sql"}}} {
		for range 2 {
			entries, e := fs.ReadDir(tc.get(), ".")
			if e != nil {
				t.Fatal(e)
			}
			var got []string
			for _, entry := range entries {
				info, e := entry.Info()
				if e != nil || !info.Mode().IsRegular() {
					t.Fatal(entry, e)
				}
				got = append(got, entry.Name())
				b, e := fs.ReadFile(tc.get(), entry.Name())
				if e != nil || len(b) == 0 {
					t.Fatal(e)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatal(got)
			}
		}
	}
}

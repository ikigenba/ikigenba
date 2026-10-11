package connections_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/connections"
)

// R-ZY8R-D9TV R-ZZGN-R1KK R-00OK-4TB9
func TestEmbeddedRoots(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tt := range []struct {
		open func() fs.FS
		want []string
	}{{connections.Assets, []string{"about.html", "landing.html", "notfound.html", "notice.html", "tools.html"}}, {connections.Etc, []string{"manifest.toml", "nginx.conf"}}, {connections.Migrations, []string{"0001_connections.sql"}}} {
		for range 2 {
			f := tt.open()
			entries, e := fs.ReadDir(f, ".")
			if e != nil {
				t.Fatal(e)
			}
			names := []string{}
			for _, entry := range entries {
				info, e := entry.Info()
				if e != nil || !info.Mode().IsRegular() {
					t.Fatalf("entry not regular: %v %v", entry, e)
				}
				names = append(names, entry.Name())
				if _, e := fs.ReadFile(f, entry.Name()); e != nil {
					t.Fatal(e)
				}
			}
			if !reflect.DeepEqual(names, tt.want) {
				t.Fatalf("got %v want %v", names, tt.want)
			}
		}
	}
}

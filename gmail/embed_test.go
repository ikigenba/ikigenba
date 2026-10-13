package gmail_test

import (
	"io/fs"
	"reflect"
	"testing"

	gmail "github.com/ikigenba/ikigenba/gmail"
)

// R-VETD-MHEM R-VG1A-095B R-3032-5GV6
func TestEmbeddedResources(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		get   func() fs.FS
		names []string
	}{{gmail.Assets, []string{"about.html", "draft.html", "landing.html", "notfound.html", "tools.html", "transfer.html"}}, {gmail.Etc, []string{"manifest.toml", "nginx.conf"}}, {gmail.Migrations, []string{"0001_transfers.sql", "0002_drafts.sql", "0003_list_indexes.sql"}}} {
		for range 2 {
			f := tc.get()
			entries, err := fs.ReadDir(f, ".")
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, e := range entries {
				info, err := e.Info()
				if err != nil || !info.Mode().IsRegular() {
					t.Fatalf("not regular: %v %v", e, err)
				}
				names = append(names, e.Name())
				if _, err = fs.ReadFile(f, e.Name()); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatal(names)
			}
		}
	}
}

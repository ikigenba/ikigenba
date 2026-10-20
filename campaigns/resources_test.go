package campaigns_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/campaigns"
)

// R-A8AJ-65EK R-A9IF-JX59 R-ABY8-BGMN
func TestEmbeddedResources(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func() fs.FS
		want []string
	}{
		{"assets", campaigns.Assets, []string{"about.html", "campaign.html", "landing.html", "notfound.html", "tools.html"}},
		{"etc", campaigns.Etc, []string{"manifest.toml", "nginx.conf"}},
		{"migrations", campaigns.Migrations, []string{"0001_campaigns.sql"}},
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

package gdrive_test

import (
	"io/fs"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/gdrive"
)

// R-QN01-1VGO R-QO7X-FN7D R-YWKB-PJS8
func TestEmbeddedInputs(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, c := range []struct {
		fs    func() fs.FS
		names []string
	}{
		{gdrive.Assets, []string{"about.html", "landing.html", "notfound.html", "tools.html", "transfer.html"}},
		{gdrive.Etc, []string{"manifest.toml", "nginx.conf"}},
		{gdrive.Migrations, []string{"0001_transfers.sql", "0002_transfers_user_started_id.sql"}},
	} {
		for range 2 {
			f := c.fs()
			entries, err := fs.ReadDir(f, ".")
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, e := range entries {
				info, err := e.Info()
				if err != nil || !info.Mode().IsRegular() {
					t.Fatalf("entry %s: %v", e.Name(), err)
				}
				names = append(names, e.Name())
				_, err = fs.ReadFile(f, e.Name())
				if err != nil {
					t.Fatalf("read %s: %v", e.Name(), err)
				}
			}
			if !slices.Equal(names, c.names) {
				t.Fatalf("names %v", names)
			}
		}
	}
}

// R-YVCF-BS1J
func TestTransferWindowMigration(t *testing.T) {
	f := gdrive.Migrations()
	info, err := fs.Stat(f, "0002_transfers_user_started_id.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatal("migration is not a regular file")
	}
	got, err := fs.ReadFile(f, "0002_transfers_user_started_id.sql")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "CREATE INDEX transfers_user_started_id ON transfers(user_id, started, id);\n" {
		t.Fatalf("migration bytes %q", got)
	}
}

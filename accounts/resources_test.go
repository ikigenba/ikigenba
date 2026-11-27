package accounts_test

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cli"
)

// R-7QD1-Z5PQ R-C7K6-723O R-822K-ILS5
func TestEmbeddedResources(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func() fs.FS
		want []string
	}{
		{"assets", accounts.Assets, []string{"about.html", "host.html", "join.html", "landing.html", "notfound.html", "tools.html"}},
		{"etc", accounts.Etc, []string{"manifest.toml", "nginx.conf"}},
		{"migrations", accounts.Migrations, []string{
			"0001_accounts.sql", "0002_hosts.sql", "0003_bans.sql", "0004_invites.sql",
			"0005_host_records.sql", "0006_kept_volumes.sql", "0007_host_backups.sql", "0008_weekly_retention.sql",
		}},
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

// R-ZZWT-ASA7
func TestEmbeddedSeed(t *testing.T) {
	want := map[string]fs.FileMode{
		".": fs.ModeDir, ".gitignore": 0, "AGENTS.md": 0,
		"data": fs.ModeDir, "data/.keep": 0, "setup.md": 0,
	}
	check := func(seed func() fs.FS) {
		t.Helper()
		got := make(map[string]fs.FileMode)
		if err := fs.WalkDir(seed(), ".", func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			got[path] = entry.Type()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Seed() entries = %v, want %v", got, want)
		}
	}
	for range 2 {
		t.Chdir(t.TempDir())
		check(accounts.Seed)
	}
}

// R-027W-U4P5
func TestSeedVersion(t *testing.T) {
	const version string = accounts.SeedVersion
	if version == "" || strings.ContainsAny(version, "\r\n") {
		t.Fatalf("SeedVersion = %q, want a non-empty string without CR or LF", version)
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

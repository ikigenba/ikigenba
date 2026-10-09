package events_test

import (
	"bytes"
	"embed"
	"io/fs"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/events"
)

func assertFiles(t *testing.T, f fs.FS, directory string, want []string) {
	t.Helper()
	entries, err := fs.ReadDir(f, directory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, statErr := entry.Info()
		if statErr != nil {
			t.Fatal(statErr)
		}
		if !info.Mode().IsRegular() {
			t.Errorf("%s is not regular", entry.Name())
		}
		names = append(names, entry.Name())
	}
	if !slices.Equal(names, want) {
		t.Fatalf("files = %v, want %v", names, want)
	}
}

func TestAssetsShape(t *testing.T) {
	// R-D7O5-IJ8J
	assertFiles(t, events.Assets(), ".", []string{"about.html", "landing.html", "notfound.html", "tools.html", "unavailable.html"})
}

func TestMigrationsShape(t *testing.T) {
	// R-ZOMY-SR8P
	assertFiles(t, events.Migrations(), ".", []string{"0001_log.sql"})
}

func TestEtcShape(t *testing.T) {
	// R-ZR2R-KAQ3
	assertEtc(t, events.Etc())
}

func assertEtc(t *testing.T, etc embed.FS) {
	t.Helper()
	if _, err := etc.ReadFile("etc/manifest.toml"); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(etc, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "etc" || !entries[0].IsDir() {
		t.Fatalf("root = %v", entries)
	}
	assertFiles(t, etc, "etc", []string{"manifest.toml", "nginx.conf"})
}

func TestEmbeddedFilesIndependentOfWorkingDirectory(t *testing.T) {
	// R-D8W1-WAZ8 R-ZPUV-6IZE R-ZSAN-Y2GS
	cases := []struct {
		open  func() fs.FS
		files []string
	}{
		{events.Assets, []string{"about.html", "landing.html", "notfound.html", "tools.html", "unavailable.html"}},
		{events.Migrations, []string{"0001_log.sql"}},
		{func() fs.FS { return events.Etc() }, []string{"etc/manifest.toml", "etc/nginx.conf"}},
	}
	original := make([][][]byte, len(cases))
	for i, c := range cases {
		for _, name := range c.files {
			contents, err := fs.ReadFile(c.open(), name)
			if err != nil {
				t.Fatal(err)
			}
			original[i] = append(original[i], contents)
		}
	}
	for range 2 {
		t.Chdir(t.TempDir())
		assertFiles(t, events.Assets(), ".", []string{"about.html", "landing.html", "notfound.html", "tools.html", "unavailable.html"})
		assertFiles(t, events.Migrations(), ".", []string{"0001_log.sql"})
		assertEtc(t, events.Etc())
		for i, c := range cases {
			for range 2 {
				for j, name := range c.files {
					contents, err := fs.ReadFile(c.open(), name)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(contents, original[i][j]) {
						t.Errorf("%s contents changed", name)
					}
				}
			}
		}
	}
}

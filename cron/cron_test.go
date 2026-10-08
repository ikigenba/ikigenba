package cron_test

import (
	"bytes"
	"io/fs"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/cron"
)

// R-7N9H-U9TX R-7PPA-LTBB R-7S53-DCSP
func TestEmbeddedFileSets(t *testing.T) {
	cases := []struct {
		name  string
		files fs.FS
		want  []string
	}{
		{"assets", cron.Assets(), []string{"about.html", "landing.html", "notfound.html"}},
		{"etc", cron.Etc(), []string{"manifest.toml", "nginx.conf"}},
		{"migrations", cron.Migrations(), []string{"0001_triggers.sql"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := fs.ReadDir(tc.files, ".")
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				info, err := entry.Info()
				if err != nil {
					t.Fatal(err)
				}
				if !info.Mode().IsRegular() {
					t.Fatalf("%s is not regular", entry.Name())
				}
				names = append(names, entry.Name())
				if _, err := fs.ReadFile(tc.files, entry.Name()); err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Equal(names, tc.want) {
				t.Fatalf("files = %v, want %v", names, tc.want)
			}
		})
	}
}

// R-7OHE-81KM R-7QX6-ZL20 R-7TCZ-R4JE
func TestEmbeddedFilesIndependentOfWorkingDirectory(t *testing.T) {
	sources := []func() fs.FS{cron.Assets, cron.Etc, cron.Migrations}
	expected := make([]map[string][]byte, len(sources))
	for i, source := range sources {
		expected[i] = readFiles(t, source())
	}
	for range 2 {
		t.Chdir(t.TempDir())
		for i, source := range sources {
			got := readFiles(t, source())
			if len(got) != len(expected[i]) {
				t.Fatalf("file count changed: %d", len(got))
			}
			for name, want := range expected[i] {
				if !bytes.Equal(got[name], want) {
					t.Fatalf("contents changed for %s", name)
				}
			}
		}
	}
}

func readFiles(t *testing.T, files fs.FS) map[string][]byte {
	t.Helper()
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		result[entry.Name()], err = fs.ReadFile(files, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}

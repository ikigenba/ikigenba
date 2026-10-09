package home_test

import (
	"bytes"
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/home"
	"github.com/ikigenba/ikigenba/home/internal/cli"
)

// R-3VQ5-O6M6 R-3WY2-1YCV R-4OZQ-UOEU R-4RFJ-M7W8 R-4Q7N-8G5J
func TestEmbeddedFiles(t *testing.T) {
	cases := []struct {
		name  string
		open  func() fs.FS
		names []string
	}{
		{"assets", home.Assets, []string{"about.html", "landing.html", "notfound.html"}},
		{"etc", home.Etc, []string{"manifest.toml"}},
	}
	snapshots := make(map[string]map[string][]byte)
	for _, tc := range cases {
		f := tc.open()
		entries, err := fs.ReadDir(f, ".")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		snapshots[tc.name] = make(map[string][]byte)
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				t.Fatalf("%s: nonregular entry %s", tc.name, entry.Name())
			}
			names = append(names, entry.Name())
			content, err := fs.ReadFile(f, entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			snapshots[tc.name][entry.Name()] = content
		}
		if !reflect.DeepEqual(names, tc.names) {
			t.Fatalf("%s entries: %v", tc.name, names)
		}
	}
	if !bytes.Equal(snapshots["etc"]["manifest.toml"], []byte(cli.Manifest)) {
		t.Fatal("embedded manifest differs from command manifest")
	}
	t.Chdir(t.TempDir())
	for range 2 {
		for _, tc := range cases {
			for name, want := range snapshots[tc.name] {
				got, err := fs.ReadFile(tc.open(), name)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("%s changed with working directory", name)
				}
			}
		}
	}
}

package dummy_test

import (
	"bytes"
	"io/fs"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/dummy"
)

// R-DKAC-F3TN R-DNY1-KF1Q
func TestAssets(t *testing.T) {
	assets := []func() fs.FS{dummy.Assets}[0]
	names := []string{"form.html", "page.html", "script.html", "table.html"}
	original := assets()
	entries, err := fs.ReadDir(original, ".")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(entries))
	contents := make(map[string][]byte)
	for i, entry := range entries {
		got[i] = entry.Name()
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("entry is not regular: %v %v", entry, err)
		}
		contents[entry.Name()], err = fs.ReadFile(original, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(got, names) {
		t.Fatalf("files: %v", got)
	}
	t.Chdir(t.TempDir())
	for range 2 {
		current := assets()
		for _, name := range names {
			data, err := fs.ReadFile(current, name)
			if err != nil || !bytes.Equal(data, contents[name]) {
				t.Fatalf("changed %s: %v", name, err)
			}
		}
	}
}

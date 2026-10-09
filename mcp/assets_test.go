package mcp_test

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/ikigenba/ikigenba/mcp"
)

// R-ZSAC-3MTK
func TestAssets(t *testing.T) {
	files := func(assets func() fs.FS) fs.FS { return assets() }(mcp.Assets)
	entries, err := fs.ReadDir(files, ".")
	if err != nil || len(entries) != 2 {
		t.Fatalf("embedded files: %v, %v", entries, err)
	}
	for i, name := range []string{"about.html", "connect.html"} {
		if entries[i].Name() != name || !entries[i].Type().IsRegular() {
			t.Fatalf("embedded entry %d: %v", i, entries[i])
		}
		data, err := fs.ReadFile(files, name)
		if err != nil || len(data) == 0 {
			t.Fatalf("embedded template %s: %q, %v", name, data, err)
		}
	}
}

// R-YBFI-EJBW
func TestAssetsIndependentOfWorkingDirectory(t *testing.T) {
	before := mcp.Assets()
	entries, err := fs.ReadDir(before, ".")
	if err != nil {
		t.Fatal(err)
	}
	contents := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		data, err := fs.ReadFile(before, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		contents[entry.Name()] = bytes.Clone(data)
	}
	t.Chdir(t.TempDir())
	for range 3 {
		after := mcp.Assets()
		afterEntries, err := fs.ReadDir(after, ".")
		if err != nil || len(afterEntries) != len(entries) {
			t.Fatalf("embedded files changed after chdir: %v, %v", afterEntries, err)
		}
		for i, entry := range entries {
			if afterEntries[i].Name() != entry.Name() || afterEntries[i].Type() != entry.Type() {
				t.Fatalf("embedded entry changed after chdir: %v", afterEntries[i])
			}
			got, err := fs.ReadFile(after, entry.Name())
			if err != nil || !bytes.Equal(contents[entry.Name()], got) {
				t.Fatalf("template %s changed after chdir: %v", entry.Name(), err)
			}
		}
	}
}

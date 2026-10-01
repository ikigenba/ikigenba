package mcp_test

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/ikigenba/ikigenba/mcp"
)

// R-V3US-1DWH
func TestAssets(t *testing.T) {
	files := mcp.Assets()
	entries, err := fs.ReadDir(files, ".")
	if err != nil || len(entries) != 1 || entries[0].Name() != "connect.html" || !entries[0].Type().IsRegular() {
		t.Fatalf("embedded files: %v, %v", entries, err)
	}
	data, err := fs.ReadFile(files, "connect.html")
	if err != nil || len(data) == 0 {
		t.Fatalf("embedded template: %q, %v", data, err)
	}
}

// R-V52O-F5N6
func TestAssetsIndependentOfWorkingDirectory(t *testing.T) {
	before, err := fs.ReadFile(mcp.Assets(), "connect.html")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for range 3 {
		after, err := fs.ReadFile(mcp.Assets(), "connect.html")
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("template changed after chdir: %v", err)
		}
	}
}

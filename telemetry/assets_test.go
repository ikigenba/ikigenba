package telemetry_test

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/ikigenba/ikigenba/telemetry"
)

func TestAssetsFiles(t *testing.T) {
	// R-U12W-VA8E
	assets := telemetry.Assets
	entries, err := fs.ReadDir(assets(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries: %v", entries)
	}
	for i, name := range []string{"about.html", "landing.html"} {
		info, err := entries[i].Info()
		if err != nil {
			t.Fatal(err)
		}
		if entries[i].Name() != name || !info.Mode().IsRegular() {
			t.Fatalf("entry: %v", entries[i])
		}
		if _, err := fs.ReadFile(assets(), name); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
	}
}

func TestAssetsIndependentOfDirectory(t *testing.T) {
	// R-U2AT-91Z3
	before := make(map[string][]byte)
	for _, name := range []string{"landing.html", "about.html"} {
		body, err := fs.ReadFile(telemetry.Assets(), name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = body
	}
	t.Chdir(t.TempDir())
	for range 3 {
		entries, err := fs.ReadDir(telemetry.Assets(), ".")
		if err != nil || len(entries) != 2 {
			t.Fatalf("entries: %v %v", entries, err)
		}
		for name, expected := range before {
			body, err := fs.ReadFile(telemetry.Assets(), name)
			if err != nil || !bytes.Equal(body, expected) {
				t.Fatalf("changed %s: %v", name, err)
			}
		}
	}
}

package telemetry_test

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/ikigenba/ikigenba/telemetry"
)

func TestAssetsFiles(t *testing.T) {
	// R-DI11-BYJQ
	assets := telemetry.Assets
	entries, err := fs.ReadDir(assets(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries: %v", entries)
	}
	for i, name := range []string{"about.html", "landing.html", "tools.html"} {
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
	// R-P95L-3A71
	before := make(map[string][]byte)
	for _, name := range []string{"landing.html", "about.html", "tools.html"} {
		body, err := fs.ReadFile(telemetry.Assets(), name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = body
	}
	t.Chdir(t.TempDir())
	for range 3 {
		entries, err := fs.ReadDir(telemetry.Assets(), ".")
		if err != nil || len(entries) != 3 {
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

func TestMigrationsFilesAndDirectoryIndependence(t *testing.T) {
	// R-HURG-AGD5 R-HVZC-O83U
	checkMigrations(t, telemetry.Migrations)
}

func checkMigrations(t *testing.T, migrations func() fs.FS) {
	t.Helper()
	before := make(map[string][]byte)
	names := []string{"0001_trail.sql", "0002_attrs_record.sql"}
	for _, name := range names {
		body, err := fs.ReadFile(migrations(), name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = body
	}
	t.Chdir(t.TempDir())
	for range 3 {
		entries, err := fs.ReadDir(migrations(), ".")
		if err != nil || len(entries) != len(names) {
			t.Fatalf("entries: %v %v", entries, err)
		}
		for i, name := range names {
			info, err := entries[i].Info()
			if err != nil || entries[i].Name() != name || !info.Mode().IsRegular() {
				t.Fatalf("entry: %v %v", entries[i], err)
			}
			body, err := fs.ReadFile(migrations(), name)
			if err != nil || !bytes.Equal(body, before[name]) {
				t.Fatalf("migration changed: %v", err)
			}
		}
	}
}

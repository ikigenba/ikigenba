package auth_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/auth"
)

var _ func() fs.FS = auth.Migrations

func TestEmbeddedMigrationsIgnoreWorkingDirectory(t *testing.T) {
	// R-G55N-V09H
	// R-G6DK-8S06
	files := auth.Migrations()
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	contents := map[string]string{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("migration is not regular: %s", entry.Name())
		}
		names = append(names, entry.Name())
		b, err := fs.ReadFile(files, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		contents[entry.Name()] = string(b)
	}
	if !reflect.DeepEqual(names, []string{"0001_baseline.sql", "0002_token_id_prefix.sql", "0003_mcp_clients.sql"}) {
		t.Fatalf("migrations: %v", names)
	}
	t.Chdir(t.TempDir())
	for range 2 {
		later := auth.Migrations()
		entries, err := fs.ReadDir(later, ".")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != len(names) {
			t.Fatalf("migration entries: %v", entries)
		}
		var laterNames []string
		for _, entry := range entries {
			laterNames = append(laterNames, entry.Name())
			b, err := fs.ReadFile(later, entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != contents[entry.Name()] {
				t.Fatalf("changed migration %s", entry.Name())
			}
		}
		if !reflect.DeepEqual(laterNames, names) {
			t.Fatalf("changed migration names: %v", laterNames)
		}
	}
}

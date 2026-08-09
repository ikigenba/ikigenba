package db

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

//go:embed migrations.sha256
var migrationManifestFS embed.FS

// R-NN1F-XWNT — the routed-outbox migration is restored byte-for-byte to the
// body originally committed under this version.
func TestFrozenOutboxRoutingMigrationHash(t *testing.T) {
	const (
		name = "20260712201504_outbox_routing.sql"
		want = "571384126d4486bcaa1c123c863146ee548b6a4eb9a5ebee8627514b46847ac4"
	)
	body, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(body))
	if got != want {
		t.Fatalf("%s sha256 = %s, want restored hash %s", name, got, want)
	}
}

// R-NFQ1-NA7N — the manifest is a total, byte-true inventory of the embedded
// migration set, catching edited, unmanifested, deleted, and renamed files.
func TestMigrationManifestIsTotalAndTrue(t *testing.T) {
	manifest, err := migrationManifestFS.ReadFile("migrations.sha256")
	if err != nil {
		t.Fatalf("read migrations.sha256: %v", err)
	}
	if len(manifest) == 0 || manifest[len(manifest)-1] != '\n' {
		t.Fatal("migrations.sha256 must be non-empty and newline-terminated")
	}

	want := make(map[string]string)
	for lineNumber, line := range strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n") {
		parts := strings.Split(line, "  ")
		if len(parts) != 2 || len(parts[0]) != sha256.Size*2 || parts[1] == "" {
			t.Fatalf("migrations.sha256 line %d is malformed: %q", lineNumber+1, line)
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			t.Fatalf("migrations.sha256 line %d has invalid hash for %s: %v", lineNumber+1, parts[1], err)
		}
		if _, exists := want[parts[1]]; exists {
			t.Fatalf("migrations.sha256 names %s more than once", parts[1])
		}
		want[parts[1]] = parts[0]
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read embedded migration %s: %v", name, err)
		}
		got := fmt.Sprintf("%x", sha256.Sum256(body))
		expected, exists := want[name]
		if !exists {
			t.Fatalf("migration %s is absent from migrations.sha256; add line %s  %s", name, got, name)
		}
		if got != expected {
			t.Fatalf("migration %s sha256 = %s, manifest has %s", name, got, expected)
		}
		delete(want, name)
	}
	for name := range want {
		t.Fatalf("migrations.sha256 names missing migration %s", name)
	}
}

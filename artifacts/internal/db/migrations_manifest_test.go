package db

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

//go:embed migrations.sha256
var migrationManifest []byte

// R-NFQ1-NA7N
func TestMigrationManifestIsTotalAndTrue(t *testing.T) {
	files := migrationFiles(t)
	if err := validateMigrationManifest(migrationManifest, files); err != nil {
		t.Fatal(err)
	}
	name := "20260811015546_create_artifacts_uploads.sql"
	files[name] = append(files[name], '\n')
	if err := validateMigrationManifest(migrationManifest, files); err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("drift error = %v, want offending filename %q", err, name)
	}
}

func migrationFiles(t *testing.T) map[string][]byte {
	t.Helper()
	entries, err := fs.ReadDir(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := FS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = body
	}
	return files
}

func validateMigrationManifest(manifest []byte, files map[string][]byte) error {
	if len(manifest) == 0 || manifest[len(manifest)-1] != '\n' {
		return fmt.Errorf("migrations.sha256 must be non-empty and newline-terminated")
	}
	lines := strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n")
	seen := make(map[string]bool)
	previousName := ""
	for _, line := range lines {
		parts := strings.Split(line, "  ")
		if len(parts) != 2 || len(parts[0]) != 64 || parts[1] == "" {
			return fmt.Errorf("invalid migrations.sha256 line %q", line)
		}
		name := parts[1]
		if previousName != "" && name <= previousName {
			return fmt.Errorf("migrations.sha256 filenames are not sorted: %s", name)
		}
		previousName = name
		body, ok := files[name]
		if !ok {
			return fmt.Errorf("manifest names missing migration %s", name)
		}
		digest := sha256.Sum256(body)
		if hex.EncodeToString(digest[:]) != parts[0] {
			return fmt.Errorf("migration checksum drift: %s", name)
		}
		if seen[name] {
			return fmt.Errorf("duplicate migration manifest entry: %s", name)
		}
		seen[name] = true
	}
	for name := range files {
		if !seen[name] {
			return fmt.Errorf("migration absent from manifest: %s", name)
		}
	}
	return nil
}

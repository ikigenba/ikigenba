package db

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

//go:embed migrations.sha256
var migrationManifest string

func TestMigrationManifestIsTotalAndTrue(t *testing.T) {
	// R-NFQ1-NA7N
	if err := verifyMigrationManifest(migrationsFS, "migrations", migrationManifest); err != nil {
		t.Fatal(err)
	}

	const original = "CREATE TABLE example (id INTEGER);\n"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(original)))
	manifest := digest + "  001_example.sql\n"
	tests := []struct {
		name     string
		files    fstest.MapFS
		manifest string
		offender string
	}{
		{
			name:     "changed migration bytes",
			files:    fstest.MapFS{"001_example.sql": {Data: []byte(original + "-- edited\n")}},
			manifest: manifest,
			offender: "001_example.sql",
		},
		{
			name: "migration absent from manifest",
			files: fstest.MapFS{
				"001_example.sql": {Data: []byte(original)},
				"002_extra.sql":   {Data: []byte("SELECT 1;\n")},
			},
			manifest: manifest,
			offender: "002_extra.sql",
		},
		{
			name:     "manifest names no migration",
			files:    fstest.MapFS{},
			manifest: manifest,
			offender: "001_example.sql",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifyMigrationManifest(test.files, ".", test.manifest)
			if err == nil {
				t.Fatalf("verifyMigrationManifest succeeded, want failure naming %s", test.offender)
			}
			if !strings.Contains(err.Error(), test.offender) {
				t.Fatalf("error %q does not name offending migration %s", err, test.offender)
			}
		})
	}
}

func verifyMigrationManifest(fsys fs.FS, dir, manifest string) error {
	expected := make(map[string]string)
	var manifestNames []string
	for lineNumber, line := range strings.Split(strings.TrimSuffix(manifest, "\n"), "\n") {
		digest, name, ok := strings.Cut(line, "  ")
		if !ok || len(digest) != sha256.Size*2 || name == "" {
			return fmt.Errorf("manifest line %d is malformed", lineNumber+1)
		}
		if _, exists := expected[name]; exists {
			return fmt.Errorf("%s: duplicate manifest line", name)
		}
		expected[name] = digest
		manifestNames = append(manifestNames, name)
	}
	if !slices.IsSorted(manifestNames) {
		return fmt.Errorf("migration manifest is not sorted by filename")
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return fmt.Errorf("%s: read embedded migration: %w", name, err)
		}
		actual := fmt.Sprintf("%x", sha256.Sum256(body))
		want, exists := expected[name]
		if !exists {
			return fmt.Errorf("%s: absent from manifest; add %s  %s", name, actual, name)
		}
		if actual != want {
			return fmt.Errorf("%s: sha256 %s, manifest has %s", name, actual, want)
		}
		delete(expected, name)
	}
	if len(expected) != 0 {
		missing := make([]string, 0, len(expected))
		for name := range expected {
			missing = append(missing, name)
		}
		slices.Sort(missing)
		return fmt.Errorf("%s: manifest names no existing migration", missing[0])
	}
	return nil
}

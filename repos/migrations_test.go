package repos_test

import (
	"bytes"
	"testing"

	"github.com/ikigenba/ikigenba/repos"
)

// R-XU1V-RXGP R-XV9S-5P7E: Embedded migration access survives an empty working directory.
func TestEmbeddedMigrations(t *testing.T) {
	first := files(t, repos.Migrations(), []string{"0001_catalog.sql"})
	t.Chdir(t.TempDir())
	second := files(t, repos.Migrations(), []string{"0001_catalog.sql"})
	if len(first["0001_catalog.sql"]) == 0 || !bytes.Equal(first["0001_catalog.sql"], second["0001_catalog.sql"]) {
		t.Fatal("migration contents changed")
	}
}

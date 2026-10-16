package store_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates"
)

// R-J19N-RJ4G
func TestEnrichmentMigrationBytes(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0011_enrichments.sql")
	contractOK(t, err)
	lines := []string{
		`CREATE TABLE enrichments (seq INTEGER PRIMARY KEY, candidate TEXT NOT NULL, kind TEXT NOT NULL, found TEXT NOT NULL, source TEXT NOT NULL, at TEXT NOT NULL, cost REAL NOT NULL);`,
		`CREATE INDEX enrichments_candidate ON enrichments(candidate,kind,seq);`,
		`CREATE INDEX enrichments_source ON enrichments(kind,source,seq);`,
		`ALTER TABLE assessments ADD COLUMN website TEXT NOT NULL DEFAULT 'unknown';`,
		`ALTER TABLE assessments ADD COLUMN website_url TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE assessments DROP COLUMN fleet;`,
		`ALTER TABLE assessments DROP COLUMN low;`,
		`ALTER TABLE assessments DROP COLUMN high;`,
		`ALTER TABLE passes ADD COLUMN due INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN fetched INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN https INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN http INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN "none" INTEGER NOT NULL DEFAULT 0;`,
	}
	contractEqual(t, string(b), strings.Join(lines, "\n")+"\n")
}

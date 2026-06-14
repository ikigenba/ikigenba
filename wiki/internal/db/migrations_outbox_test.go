package db

import (
	"strings"
	"testing"

	"eventplane/outbox"
)

// TestOutboxMigrationMatchesLibraryDDL guards design §12.1: the outbox table DDL
// is OWNED by the eventplane library (outbox.SchemaSQL); wiki's producer_outbox
// migration only applies it. If the two drift, wiki's outbox is no longer
// byte-identical to every other producer's — so this test fails loudly the moment
// they diverge. It mirrors crm's migrations_outbox_test.go (the established
// producer pattern).
func TestOutboxMigrationMatchesLibraryDDL(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/20260614050737_producer_outbox.sql")
	if err != nil {
		t.Fatalf("read producer_outbox migration: %v", err)
	}
	if !strings.Contains(string(body), outbox.SchemaSQL) {
		t.Fatalf("producer_outbox migration does not contain the library DDL verbatim.\n--- outbox.SchemaSQL ---\n%s\n--- migration file ---\n%s",
			outbox.SchemaSQL, string(body))
	}
}

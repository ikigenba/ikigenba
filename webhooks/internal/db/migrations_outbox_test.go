package db

import (
	"testing"

	"eventplane/outbox"
)

// R-T2W7-WFN1 — the outbox DDL is OWNED by the eventplane library
// (outbox.SchemaSQL); webhooks's 003_outbox.sql only applies it. The committed
// migration file must be exact-byte-equal to the library constant so every
// producer's outbox is byte-identical; any drift fails this test loudly.
func TestOutboxMigrationByteIdenticalToLibraryDDL(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/003_outbox.sql")
	if err != nil {
		t.Fatalf("read 003_outbox.sql: %v", err)
	}
	if string(body) != outbox.SchemaSQL {
		t.Fatalf("003_outbox.sql is not byte-identical to outbox.SchemaSQL.\n--- outbox.SchemaSQL (%d bytes) ---\n%q\n--- migration file (%d bytes) ---\n%q",
			len(outbox.SchemaSQL), outbox.SchemaSQL, len(body), string(body))
	}
}

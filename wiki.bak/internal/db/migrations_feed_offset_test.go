package db

import (
	"strings"
	"testing"

	"eventplane/consumer"
)

// TestFeedOffsetMigrationMatchesLibraryDDL guards Task 6.1: the feed_offset table
// DDL is OWNED by the eventplane library (consumer.SchemaSQL); wiki's
// 003_feed_offset.sql migration only applies it. If the two drift, wiki's offset
// store is no longer the schema the engine reads and writes — so this test fails
// loudly the moment they diverge. It mirrors notify's
// migrations_feed_offset_test.go (the worked consumer example).
func TestFeedOffsetMigrationMatchesLibraryDDL(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/003_feed_offset.sql")
	if err != nil {
		t.Fatalf("read 003_feed_offset.sql: %v", err)
	}
	if !strings.Contains(string(body), consumer.SchemaSQL) {
		t.Fatalf("003_feed_offset.sql does not contain the library DDL verbatim.\n--- consumer.SchemaSQL ---\n%s\n--- migration file ---\n%s",
			consumer.SchemaSQL, string(body))
	}
}

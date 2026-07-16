package db

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appdb "appkit/db"
	"eventplane/consumer"
)

func TestFeedOffsetMigrationMatchesConsumerSchemaAndCreatesTable(t *testing.T) {
	// R-TY2R-GFRU
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations and drift guard: %v", err)
	}

	var feedOffsetMigration appdb.Migration
	for _, migration := range migrations {
		if strings.Contains(migration.Name, "feed_offset") {
			feedOffsetMigration = migration
		}
	}
	if feedOffsetMigration.Name == "" {
		t.Fatal("embedded migrations have no feed_offset migration")
	}
	if !strings.Contains(feedOffsetMigration.SQL, consumer.SchemaSQL) {
		t.Fatalf("migration %s does not contain consumer.SchemaSQL verbatim", feedOffsetMigration.Name)
	}

	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatalf("open temp database: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("apply full embedded migration set: %v", err)
	}

	want := []string{"source", "cursor", "subscribed", "updated_at"}
	if got := tableColumns(t, conn, "feed_offset"); !reflect.DeepEqual(got, want) {
		t.Fatalf("feed_offset columns = %v, want %v", got, want)
	}
}

func TestFeedOffsetDriftGuardRejectsMissingOrDivergedSchema(t *testing.T) {
	tests := []struct {
		name       string
		migrations []appdb.Migration
	}{
		{name: "missing"},
		{
			name: "diverged",
			migrations: []appdb.Migration{{
				Name: "20260716013729_consumer_feed_offset.sql",
				SQL:  "CREATE TABLE feed_offset (source TEXT PRIMARY KEY);",
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := guardCopiedSchema(test.migrations, "feed_offset", consumer.SchemaSQL); err == nil {
				t.Fatal("drift guard accepted an absent or diverged feed_offset schema")
			}
		})
	}
}

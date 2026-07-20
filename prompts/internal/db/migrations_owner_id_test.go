package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

func TestOwnerIDMigrationRebuildsAndDropsLegacyRows(t *testing.T) {
	// R-E59O-RJC7
	ctx := context.Background()
	conn, err := appkitdb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var before []appkitdb.Migration
	for _, m := range migs {
		if strings.Contains(m.Name, "owner_id_keying") {
			break
		}
		before = append(before, m)
	}
	if err := appkitdb.Migrate(ctx, conn, before); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO prompts (id, owner_email, user_prompt, config_json, created_at, updated_at) VALUES ('p','same@x','x','{}','t','t')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO runs (id, prompt_id, owner_email, status, started_at, log_path) VALUES ('r','p','same@x','running','t','x')`); err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"prompts", "runs"} {
		for _, column := range []string{"owner_id", "owner_email"} {
			var notNull int
			if err := conn.QueryRow(`SELECT "notnull" FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&notNull); err != nil {
				t.Fatalf("%s.%s: %v", table, column, err)
			}
			if notNull != 1 {
				t.Fatalf("%s.%s notnull=%d", table, column, notNull)
			}
		}
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d legacy rows", table, count)
		}
	}
}

package db

import (
	"context"
	"reflect"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

func TestRetireContentColumnsPreservesEveryPromptMetadataRow(t *testing.T) {
	// R-SE0O-ZSWV
	ctx := context.Background()
	conn, err := appkitdb.Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var before []appkitdb.Migration
	for _, migration := range migrations {
		if strings.Contains(migration.Name, "retire_content_columns") {
			break
		}
		before = append(before, migration)
	}
	if len(before) == len(migrations) {
		t.Fatal("retire-content migration not found")
	}
	if err := appkitdb.Migrate(ctx, conn, before); err != nil {
		t.Fatalf("migrate to pre-retirement schema: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO prompts
		(id, owner_id, owner_email, name, name_key, user_prompt, system_prompt, config_json, source_path, created_at, updated_at)
		VALUES
		('p-1','owner-1','one@example.com','One','one','body-1','system-1','{"model":"one"}','/one','c1','u1'),
		('p-2','owner-2','two@example.com','Two','two','body-2',NULL,'{"model":"two"}',NULL,'c2','u2'),
		('p-3','owner-3','three@example.com','Three','three','body-3','system-3','{"model":"three"}','/three','c3','u3')`); err != nil {
		t.Fatalf("seed prompt rows: %v", err)
	}
	want := queryMigrationRows(t, conn, `SELECT id, name, name_key, COALESCE(source_path,''), created_at, updated_at FROM prompts ORDER BY id`, 6)
	if err := appkitdb.Migrate(ctx, conn, migrations); err != nil {
		t.Fatalf("apply full embedded migration set: %v", err)
	}
	columns := map[string]bool{}
	rows, err := conn.Query(`PRAGMA table_info(prompts)`)
	if err != nil {
		t.Fatalf("inspect prompt columns: %v", err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan prompt column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close prompt columns: %v", err)
	}
	for _, retired := range []string{"user_prompt", "system_prompt", "config_json"} {
		if columns[retired] {
			t.Errorf("retired column %q remains in %v", retired, columns)
		}
	}
	got := queryMigrationRows(t, conn, `SELECT id, name, name_key, COALESCE(source_path,''), created_at, updated_at FROM prompts ORDER BY id`, 6)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prompt metadata changed across retirement:\n before: %#v\n after:  %#v", want, got)
	}
}

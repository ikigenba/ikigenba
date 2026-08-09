package db

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

func TestNameKeyMigrationPreservesRowsAndAddsColumns(t *testing.T) {
	// R-RFVI-A85F
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
	var throughNameKey []appkitdb.Migration
	for _, migration := range migrations {
		throughNameKey = append(throughNameKey, migration)
		if strings.Contains(migration.Name, "name_key_and_definition_sha") {
			break
		}
		before = append(before, migration)
	}
	if len(before) == len(migrations) {
		t.Fatal("name-key migration not found in embedded migration set")
	}
	if err := appkitdb.Migrate(ctx, conn, before); err != nil {
		t.Fatalf("migrate to pre-name-key schema: %v", err)
	}

	if _, err := conn.Exec(`INSERT INTO prompts
		(id, owner_id, owner_email, name, user_prompt, system_prompt, config_json, source_path, created_at, updated_at)
		VALUES
		('p-1','owner-1','one@example.com','First','first body','first system','{"model":"one"}','/first','c1','u1'),
		('p-2','owner-2','two@example.com','Second','second body','second system','{"model":"two"}','/second','c2','u2')`); err != nil {
		t.Fatalf("seed prompts: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO runs
		(id, prompt_id, owner_id, owner_email, prompt_name, status, started_at, ended_at, usage_json, error, log_path)
		VALUES
		('r-1','p-1','owner-1','one@example.com','First','succeeded','s1','e1','{"tokens":1}',NULL,'log-1'),
		('r-2','p-2','owner-2','two@example.com','Second','failed','s2','e2',NULL,'boom','log-2')`); err != nil {
		t.Fatalf("seed runs: %v", err)
	}

	beforePrompts := queryMigrationRows(t, conn, `SELECT id, owner_id, owner_email, name, user_prompt, system_prompt, config_json, source_path, created_at, updated_at FROM prompts ORDER BY id`, 10)
	beforeRuns := queryMigrationRows(t, conn, `SELECT id, prompt_id, owner_id, owner_email, prompt_name, status, started_at, ended_at, COALESCE(usage_json,''), COALESCE(error,''), log_path FROM runs ORDER BY id`, 11)

	if err := appkitdb.Migrate(ctx, conn, throughNameKey); err != nil {
		t.Fatalf("apply full embedded migration set: %v", err)
	}
	for table, column := range map[string]string{"prompts": "name_key", "runs": "definition_sha"} {
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
			t.Fatalf("inspect %s.%s: %v", table, column, err)
		}
		if count != 1 {
			t.Fatalf("%s.%s column count = %d, want 1", table, column, count)
		}
	}
	var unique, partial int
	if err := conn.QueryRow(`SELECT "unique", partial FROM pragma_index_list('prompts') WHERE name = 'idx_prompts_name_key'`).Scan(&unique, &partial); err != nil {
		t.Fatalf("inspect idx_prompts_name_key: %v", err)
	}
	if unique != 1 || partial != 1 {
		t.Fatalf("idx_prompts_name_key unique/partial = %d/%d, want 1/1", unique, partial)
	}

	afterPrompts := queryMigrationRows(t, conn, `SELECT id, owner_id, owner_email, name, user_prompt, system_prompt, config_json, source_path, created_at, updated_at FROM prompts ORDER BY id`, 10)
	afterRuns := queryMigrationRows(t, conn, `SELECT id, prompt_id, owner_id, owner_email, prompt_name, status, started_at, ended_at, COALESCE(usage_json,''), COALESCE(error,''), log_path FROM runs ORDER BY id`, 11)
	if !reflect.DeepEqual(afterPrompts, beforePrompts) {
		t.Fatalf("prompt rows changed across migration:\nbefore: %#v\nafter:  %#v", beforePrompts, afterPrompts)
	}
	if !reflect.DeepEqual(afterRuns, beforeRuns) {
		t.Fatalf("run rows changed across migration:\nbefore: %#v\nafter:  %#v", beforeRuns, afterRuns)
	}
}

func queryMigrationRows(t *testing.T, conn interface {
	Query(string, ...any) (*sql.Rows, error)
}, query string, columns int) [][]string {
	t.Helper()
	rows, err := conn.Query(query)
	if err != nil {
		t.Fatalf("query preserved rows: %v", err)
	}
	defer rows.Close()
	var result [][]string
	for rows.Next() {
		values := make([]string, columns)
		dest := make([]any, columns)
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan preserved rows: %v", err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate preserved rows: %v", err)
	}
	return result
}

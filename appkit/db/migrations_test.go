package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

func migrationFiles(statements ...string) fs.FS {
	files := fstest.MapFS{}
	for i, statement := range statements {
		files[fmtMigrationName(i+1)] = &fstest.MapFile{Data: []byte(statement)}
	}
	return files
}
func fmtMigrationName(version int) string { return fmt.Sprintf("%04d_schema.sql", version) }
func migrationConfig(t *testing.T, statements ...string) db.Config {
	t.Helper()
	return db.Config{Path: filepath.Join(t.TempDir(), "state.db"), Migrations: migrationFiles(statements...), Now: func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 123456789, time.FixedZone("offset", 3600)) }}
}
func openMigrationDB(t *testing.T, cfg db.Config) *db.DB {
	t.Helper()
	handle, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	})
	return handle
}
func rawDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	})
	return conn
}
func migrationScalar(t *testing.T, handle *db.DB, query string) string {
	t.Helper()
	var value string
	if err := handle.Read(context.Background(), func(tx *sql.Tx) error { return tx.QueryRow(query).Scan(&value) }); err != nil {
		t.Fatal(err)
	}
	return value
}

// R-QCCD-GFCY R-QDK9-U73N
func TestInvalidMigrationsLeaveDiskUntouched(t *testing.T) {
	cases := map[string]fs.FS{
		"nil":       nil,
		"name":      fstest.MapFS{"README": {Data: []byte("bad")}},
		"directory": fstest.MapFS{"0001_folder.sql": {Mode: fs.ModeDir}},
		"symlink":   fstest.MapFS{"0001_link.sql": {Mode: fs.ModeSymlink, Data: []byte("target")}},
		"uppercase": fstest.MapFS{"0001_Name.sql": {Data: []byte("SELECT 1")}},
		"zero":      fstest.MapFS{"0000_zero.sql": {Data: []byte("SELECT 1")}},
		"gap":       fstest.MapFS{"0002_gap.sql": {Data: []byte("SELECT 1")}},
		"duplicate": fstest.MapFS{"0001_a.sql": {Data: []byte("SELECT 1")}, "0001_b.sql": {Data: []byte("SELECT 1")}},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			absent := filepath.Join(root, "missing", "state.db")
			cfg := db.Config{Path: absent, Migrations: files}
			handle, err := db.Open(context.Background(), cfg)
			if handle != nil || err == nil {
				t.Fatalf("Open = %v, %v", handle, err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("created entries: %v, %v", entries, err)
			}
			existing := filepath.Join(root, "existing.db")
			conn := rawDatabase(t, existing)
			if _, err := conn.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT); INSERT INTO schema_migrations VALUES(99,'original')"); err != nil {
				t.Fatal(err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Clean(existing))
			if err != nil {
				t.Fatal(err)
			}
			cfg.Path = existing
			handle, err = db.Open(context.Background(), cfg)
			if handle != nil || err == nil {
				t.Fatalf("Open = %v, %v", handle, err)
			}
			after, err := os.ReadFile(filepath.Clean(existing))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("invalid migrations changed database")
			}
			entries, err = os.ReadDir(root)
			if err != nil || len(entries) != 1 || entries[0].Name() != "existing.db" {
				t.Fatalf("invalid migrations created entries: %v, %v", entries, err)
			}
		})
	}
}

// R-N0OE-BC72 R-9YY6-OFZ0 R-QG02-LQL1
func TestMigrationSchemaOrderStatementsAndTimes(t *testing.T) {
	cfg := migrationConfig(t, "CREATE TABLE items(value TEXT); INSERT INTO items VALUES('first');", "UPDATE items SET value=value||'-second'; INSERT INTO items VALUES('third');")
	base := cfg.Now()
	times := []time.Time{base, base.Add(time.Hour)}
	observer := rawDatabase(t, cfg.Path)
	if _, err := observer.Exec("CREATE TABLE clock_fixture(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	cfg.Now = func() time.Time {
		var exists, previous int
		if err := observer.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='schema_migrations'").Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 0 {
			if err := observer.QueryRow("SELECT coalesce(max(version), 0) FROM schema_migrations").Scan(&previous); err != nil {
				t.Fatal(err)
			}
		}
		return base.Add(time.Duration(previous) * time.Hour)
	}
	handle := openMigrationDB(t, cfg)
	if got := migrationScalar(t, handle, "SELECT group_concat(value, ',') FROM items"); got != "first-second,third" {
		t.Fatal(got)
	}
	if err := handle.Read(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.Query("PRAGMA table_info(schema_migrations)")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		names := []string{"version", "applied_at"}
		types := []string{"INTEGER", "TEXT"}
		index := 0
		for rows.Next() {
			var cid, notnull, pk int
			var name, kind string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
				return err
			}
			if index >= 2 || name != names[index] || kind != types[index] || pk != 1-index {
				t.Fatalf("column %d %s %s pk=%d", index, name, kind, pk)
			}
			index++
		}
		if index != 2 {
			t.Fatalf("columns %d", index)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	for version, want := range times {
		query := fmt.Sprintf("SELECT applied_at FROM schema_migrations WHERE version=%d", version+1)
		if got := migrationScalar(t, handle, query); got != want.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z") {
			t.Fatal(got)
		}
	}
}

// R-QCCD-GFCY
func TestValidMigrationSets(t *testing.T) {
	for _, files := range []fs.FS{migrationFiles(), fstest.MapFS{"0001_a_2.sql": {Data: []byte("SELECT 1")}, "0002_3.sql": {Data: []byte("SELECT 2")}}} {
		cfg := migrationConfig(t)
		cfg.Migrations = files
		openMigrationDB(t, cfg)
	}
}

// R-N347-2VOG
func TestAppliedMigrationIsNeverReexecuted(t *testing.T) {
	cfg := migrationConfig(t, "CREATE TABLE original(value TEXT); INSERT INTO original VALUES('kept');")
	handle := openMigrationDB(t, cfg)
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Migrations = migrationFiles("THIS IS NO LONGER VALID SQL")
	reopened := openMigrationDB(t, cfg)
	if got := migrationScalar(t, reopened, "SELECT value FROM original"); got != "kept" {
		t.Fatal(got)
	}
}

// R-QH7Y-ZIBQ
func TestNilMigrationClockUsesCurrentTime(t *testing.T) {
	cfg := migrationConfig(t, "SELECT 1")
	cfg.Now = nil
	before := time.Now().UTC().Truncate(time.Microsecond)
	handle := openMigrationDB(t, cfg)
	after := time.Now().UTC()
	stamp, err := time.Parse("2006-01-02T15:04:05.000000Z", migrationScalar(t, handle, "SELECT applied_at FROM schema_migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if stamp.Before(before) || stamp.After(after) {
		t.Fatalf("timestamp %s outside %s..%s", stamp, before, after)
	}
}

// R-N5JZ-UF5U R-4YUM-515F R-502I-ISW4
func TestFailedMigrationRollsBackOnlyItsVersion(t *testing.T) {
	cfg := migrationConfig(t, "CREATE TABLE kept(value TEXT); INSERT INTO kept VALUES('before');", "INSERT INTO kept VALUES('lost'); CREATE TABLE lost(value TEXT); INVALID SQL;")
	handle, err := db.Open(context.Background(), cfg)
	if handle != nil || err == nil || !strings.Contains(err.Error(), "0002_schema.sql") {
		t.Fatalf("Open = %v, %v", handle, err)
	}
	conn := rawDatabase(t, cfg.Path)
	var value string
	if err := conn.QueryRow("SELECT group_concat(value) FROM kept").Scan(&value); err != nil || value != "before" {
		t.Fatalf("kept %s, %v", value, err)
	}
	var count int
	if err := conn.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='lost'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("lost table %d, %v", count, err)
	}
	if err := conn.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=2").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed row %d, %v", count, err)
	}
	if err := conn.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=1").Scan(&count); err != nil || count != 1 {
		t.Fatalf("previous row %d, %v", count, err)
	}
}

// R-XPWF-AFWA R-11H4-C5Y1 R-12P0-PXOQ R-N97O-ZQDX R-A1DZ-FZGE
func TestUnknownMigrationAcceptsOpenWithoutAnyMigration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		versions []int
		message  string
	}{
		{"single", []int{12}, "dummy: unknown migration version 0012: database is ahead of this binary\n"},
		{"multiple", []int{40, 12, 3}, "dummy: unknown migration version 0003: database is ahead of this binary\n"},
		{"zero", []int{12, 0, 4}, "dummy: unknown migration version 0000: database is ahead of this binary\n"},
		{"negative", []int{12, -3, 0}, "dummy: unknown migration version -003: database is ahead of this binary\n"},
		{"five digits", []int{12000, 10000}, "dummy: unknown migration version 10000: database is ahead of this binary\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := migrationConfig(t, "SELECT 1", "SELECT 2")
			seed := openMigrationDB(t, cfg)
			if err := seed.Close(); err != nil {
				t.Fatal(err)
			}
			cfg.Migrations = migrationFiles("CREATE TABLE must_not_exist(value TEXT)")
			cfg.Service = "dummy"
			diagnostics := &migrationRecordingWriter{}
			cfg.Stderr = diagnostics
			conn := rawDatabase(t, cfg.Path)
			if _, err := conn.Exec("DELETE FROM schema_migrations"); err != nil {
				t.Fatal(err)
			}
			for _, version := range tc.versions {
				if _, err := conn.Exec("INSERT INTO schema_migrations VALUES(?,'past')", version); err != nil {
					t.Fatal(err)
				}
			}
			handle, err := db.Open(context.Background(), cfg)
			if handle == nil || err != nil {
				t.Fatalf("Open = %v, %v", handle, err)
			}
			if len(diagnostics.writes) != 1 || diagnostics.writes[0] != tc.message {
				t.Fatalf("diagnostics %q; want one write %q", diagnostics.writes, tc.message)
			}
			if err := handle.Close(); err != nil {
				t.Fatal(err)
			}
			cfg.Stderr = nil
			handle = openMigrationDB(t, cfg)
			if err := handle.Close(); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := conn.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='must_not_exist'").Scan(&count); err != nil || count != 0 {
				t.Fatalf("table %d %v", count, err)
			}
			if err := conn.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != len(tc.versions) {
				t.Fatalf("rows %d %v", count, err)
			}
		})
	}
}

// R-N0OE-BC72 R-9YY6-OFZ0 R-QG02-LQL1
func TestMigrationMetadataSchemaRestoredAfterSuccessfulAlter(t *testing.T) {
	cfg := migrationConfig(t, "SELECT 1", "ALTER TABLE schema_migrations ADD COLUMN extra TEXT;")
	handle := openMigrationDB(t, cfg)
	if err := handle.Read(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.Query("PRAGMA table_info(schema_migrations)")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		names := []string{"version", "applied_at"}
		kinds := []string{"INTEGER", "TEXT"}
		index := 0
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, kind string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
				return err
			}
			if index >= len(names) || name != names[index] || kind != kinds[index] || primaryKey != 1-index {
				t.Fatalf("column %d %q %q pk=%d", index, name, kind, primaryKey)
			}
			index++
		}
		if index != len(names) {
			t.Fatalf("columns %d", index)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 2; version++ {
		query := fmt.Sprintf("SELECT applied_at FROM schema_migrations WHERE version=%d", version)
		if got := migrationScalar(t, handle, query); got != "2025-01-02T02:04:05.123456Z" {
			t.Fatalf("version %d timestamp %q", version, got)
		}
	}
}

// R-9YY6-OFZ0 R-QG02-LQL1
func TestMigrationBookkeepingIgnoresMetadataSchemaObjects(t *testing.T) {
	for name, statement := range map[string]string{
		"insert-trigger":  `CREATE TRIGGER "erase_record" AFTER INSERT ON schema_migrations BEGIN DELETE FROM schema_migrations; END;`,
		"update-trigger":  `CREATE TRIGGER "erase_update" AFTER UPDATE ON schema_migrations BEGIN DELETE FROM schema_migrations; END;`,
		"quoted-trigger":  `CREATE TRIGGER "erase""record" AFTER INSERT ON schema_migrations BEGIN DELETE FROM schema_migrations; END;`,
		"temp-trigger":    `CREATE TEMP TRIGGER temporary_erase AFTER INSERT ON main.schema_migrations BEGIN DELETE FROM schema_migrations; END;`,
		"unique-index":    `CREATE UNIQUE INDEX duplicate_times ON schema_migrations(applied_at);`,
		"dependent-table": `ALTER TABLE schema_migrations ADD COLUMN extra TEXT; CREATE TABLE dependent(v INTEGER REFERENCES schema_migrations(version)); INSERT INTO dependent VALUES(1);`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := migrationConfig(t, "SELECT 1", statement)
			handle := openMigrationDB(t, cfg)
			for version := 1; version <= 2; version++ {
				query := fmt.Sprintf("SELECT applied_at FROM schema_migrations WHERE version=%d", version)
				if got := migrationScalar(t, handle, query); got != "2025-01-02T02:04:05.123456Z" {
					t.Fatalf("version %d timestamp %q", version, got)
				}
			}
		})
	}
}

// R-N0OE-BC72 R-N347-2VOG
func TestMigrationBookkeepingRepairsMissingTimestampColumn(t *testing.T) {
	cfg := migrationConfig(t, "CREATE TABLE original(value TEXT); INSERT INTO original VALUES('kept');")
	handle := openMigrationDB(t, cfg)
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	conn := rawDatabase(t, cfg.Path)
	if _, err := conn.Exec("ALTER TABLE schema_migrations DROP COLUMN applied_at"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openMigrationDB(t, cfg)
	if got := migrationScalar(t, reopened, "SELECT value FROM original"); got != "kept" {
		t.Fatal(got)
	}
	if got := migrationScalar(t, reopened, "SELECT group_concat(name, ',') FROM pragma_table_info('schema_migrations')"); got != "version,applied_at" {
		t.Fatal(got)
	}
}

type migrationRecordingWriter struct{ writes []string }

func (w *migrationRecordingWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, string(p))
	return len(p), nil
}

// R-12P0-PXOQ R-A1DZ-FZGE
func TestOpenWritesNoDiagnosticsExceptAheadWarning(t *testing.T) {
	for _, kind := range []string{"fresh", "reopen", "invalid path", "invalid migrations", "failed migration"} {
		t.Run(kind, func(t *testing.T) {
			for _, withWriter := range []bool{true, false} {
				cfg := migrationConfig(t, "SELECT 1")
				if kind == "reopen" {
					handle := openMigrationDB(t, cfg)
					if err := handle.Close(); err != nil {
						t.Fatal(err)
					}
				}
				switch kind {
				case "invalid path":
					cfg.Path = ":memory:"
				case "invalid migrations":
					cfg.Migrations = nil
				case "failed migration":
					cfg.Migrations = migrationFiles("INVALID SQL")
				}
				diagnostics := &migrationRecordingWriter{}
				if withWriter {
					cfg.Stderr = diagnostics
				}
				handle, err := db.Open(context.Background(), cfg)
				wantSuccess := kind == "fresh" || kind == "reopen"
				if (err == nil) != wantSuccess || (handle != nil) != wantSuccess {
					t.Fatalf("Open = %v, %v", handle, err)
				}
				if handle != nil {
					if err := handle.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if len(diagnostics.writes) != 0 {
					t.Fatalf("unexpected diagnostics %q", diagnostics.writes)
				}
			}
		})
	}
}

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrUnknownVersion reports a database migrated by a newer binary.
var ErrUnknownVersion = errors.New("unknown migration version")

type migration struct {
	version int
	name    string
	sql     string
}

var migrationName = regexp.MustCompile(`^[0-9]{4}_[a-z0-9_]+\.sql$`)

func loadMigrations(files fs.FS) ([]migration, error) {
	if files == nil {
		return nil, errors.New("migrations filesystem is nil")
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	result := make([]migration, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect migration %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() || !migrationName.MatchString(entry.Name()) {
			return nil, fmt.Errorf("invalid migration %s", entry.Name())
		}
		version, err := strconv.Atoi(entry.Name()[:4])
		if err != nil {
			return nil, fmt.Errorf("invalid migration %s: %w", entry.Name(), err)
		}
		content, err := fs.ReadFile(files, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		result = append(result, migration{version: version, name: entry.Name(), sql: string(content)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for i, item := range result {
		if item.version != i+1 {
			return nil, fmt.Errorf("invalid migration sequence at %s", item.name)
		}
	}
	return result, nil
}

func appliedVersions(ctx context.Context, conn *sql.DB) (map[int]string, error) {
	var exists int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'schema_migrations'").Scan(&exists); err != nil {
		return nil, fmt.Errorf("inspect migration table: %w", err)
	}
	result := make(map[int]string)
	if exists == 0 {
		return result, nil
	}
	columns, err := conn.QueryContext(ctx, "PRAGMA table_info(schema_migrations)")
	if err != nil {
		return nil, err
	}
	hasVersion, hasTimestamp := false, false
	for columns.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err := columns.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			_ = columns.Close()
			return nil, err
		}
		hasVersion = hasVersion || name == "version"
		hasTimestamp = hasTimestamp || name == "applied_at"
	}
	if err := columns.Err(); err != nil {
		_ = columns.Close()
		return nil, err
	}
	if err := columns.Close(); err != nil {
		return nil, err
	}
	if !hasVersion {
		return result, nil
	}
	statement := "SELECT version, applied_at FROM schema_migrations ORDER BY version"
	if !hasTimestamp {
		statement = "SELECT version, '' FROM schema_migrations ORDER BY version"
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var version int
		var applied string
		if err := rows.Scan(&version, &applied); err != nil {
			return nil, fmt.Errorf("read migration: %w", err)
		}
		result[version] = applied
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	return result, nil
}

func applyMigrations(ctx context.Context, writer *sql.DB, migrations []migration, now func() time.Time) error {
	applied, err := appliedVersions(ctx, writer)
	if err != nil {
		return err
	}
	lowestUnknown, unknown := 0, false
	for version := range applied {
		if (version < 1 || version > len(migrations)) && (!unknown || version < lowestUnknown) {
			lowestUnknown, unknown = version, true
		}
	}
	if unknown {
		return fmt.Errorf("%w: %04d", ErrUnknownVersion, lowestUnknown)
	}
	if _, err := writer.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT)"); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	valid, err := migrationSchemaValid(ctx, writer)
	if err != nil {
		return err
	}
	if !valid {
		tx, err := writer.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := restoreMigrationSchema(ctx, tx, applied); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if now == nil {
		now = time.Now
	}
	for _, item := range migrations {
		if _, exists := applied[item.version]; exists {
			continue
		}
		timestamp, err := applyMigration(ctx, writer, item, now, applied)
		if err != nil {
			return fmt.Errorf("migration %s: %w", item.name, err)
		}
		applied[item.version] = timestamp
	}
	return nil
}

func applyMigration(ctx context.Context, writer *sql.DB, item migration, now func() time.Time, applied map[int]string) (string, error) {
	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, item.sql); err != nil {
		return "", err
	}
	timestamp := now().UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")
	// The bookkeeping schema is owned by appkit even when migration SQL changes it.
	if err := restoreMigrationSchema(ctx, tx, applied); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?) ON CONFLICT(version) DO UPDATE SET applied_at=excluded.applied_at", item.version, timestamp); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return timestamp, nil
}

func restoreMigrationSchema(ctx context.Context, tx *sql.Tx, applied map[int]string) error {
	if err := removeMigrationObjects(ctx, tx); err != nil {
		return err
	}
	valid, err := migrationSchemaValid(ctx, tx)
	if err != nil {
		return err
	}
	if !valid {
		if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
			return err
		}
		var objectType string
		err := tx.QueryRowContext(ctx, "SELECT type FROM sqlite_schema WHERE name='schema_migrations'").Scan(&objectType)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if objectType == "view" {
			if _, err := tx.ExecContext(ctx, "DROP VIEW schema_migrations"); err != nil {
				return err
			}
		} else if objectType != "" {
			if _, err := tx.ExecContext(ctx, "DROP TABLE schema_migrations"); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT)"); err != nil {
			return err
		}
	}
	versions := make([]int, 0, len(applied))
	for version := range applied {
		versions = append(versions, version)
	}
	sort.Ints(versions)
	for _, version := range versions {
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?) ON CONFLICT(version) DO UPDATE SET applied_at=excluded.applied_at", version, applied[version]); err != nil {
			return err
		}
	}
	return nil
}

type migrationQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func migrationSchemaValid(ctx context.Context, query migrationQuerier) (bool, error) {
	rows, err := query.QueryContext(ctx, "PRAGMA table_info(schema_migrations)")
	if err != nil {
		return false, fmt.Errorf("inspect migration schema: %w", err)
	}
	defer func() { _ = rows.Close() }()
	names := []string{"version", "applied_at"}
	types := []string{"INTEGER", "TEXT"}
	index := 0
	valid := true
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("inspect migration schema: %w", err)
		}
		if index >= len(names) || name != names[index] || kind != types[index] || primaryKey != 1-index {
			valid = false
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("inspect migration schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if !valid || index != len(names) {
		return false, nil
	}
	definitions, err := query.QueryContext(ctx, "SELECT sql FROM sqlite_schema WHERE type='table' AND name='schema_migrations'")
	if err != nil {
		return false, err
	}
	defer func() { _ = definitions.Close() }()
	var definition string
	if !definitions.Next() {
		return false, definitions.Err()
	}
	if err := definitions.Scan(&definition); err != nil {
		return false, err
	}
	canonical := "CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT)"
	return strings.EqualFold(strings.Join(strings.Fields(definition), " "), canonical), nil
}

func removeMigrationObjects(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT 'main', type, name FROM main.sqlite_schema WHERE tbl_name='schema_migrations' AND type IN ('trigger','index') AND sql IS NOT NULL UNION ALL SELECT 'temp', type, name FROM temp.sqlite_schema WHERE tbl_name='schema_migrations' AND type IN ('trigger','index') AND sql IS NOT NULL")
	if err != nil {
		return err
	}
	type schemaObject struct{ schema, kind, name string }
	var objects []schemaObject
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.schema, &object.kind, &object.name); err != nil {
			_ = rows.Close()
			return err
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, object := range objects {
		var statement strings.Builder
		if object.kind == "trigger" {
			statement.WriteString("DROP TRIGGER ")
		} else {
			statement.WriteString("DROP INDEX ")
		}
		statement.WriteString(object.schema)
		statement.WriteString(".\"")
		statement.WriteString(strings.ReplaceAll(object.name, "\"", "\"\""))
		statement.WriteString("\"")
		if _, err := tx.ExecContext(ctx, statement.String()); err != nil {
			return err
		}
	}
	return nil
}

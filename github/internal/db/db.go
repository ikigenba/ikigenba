// Package db holds github's embedded bootstrap migration and thin helpers over
// the shared appkit database runner.
package db

import (
	"context"
	"database/sql"
	"embed"

	"appkit/db"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// FS exposes the embedded migration set so cmd/github can hand it to
// appkit.Spec.Migrations.
var FS = migrationsFS

// Open opens github's SQLite database with the chassis pragmas.
func Open(dbPath string) (*sql.DB, error) {
	return db.Open(dbPath)
}

// Migrate applies github's embedded migrations against conn using appkit's
// forward-only runner and downgrade guard.
func Migrate(ctx context.Context, conn *sql.DB) error {
	migs, err := db.LoadMigrations(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	return db.Migrate(ctx, conn, migs)
}

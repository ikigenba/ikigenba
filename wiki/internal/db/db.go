// Package db owns wiki's embedded migration set and test helpers.
package db

import (
	"context"
	"database/sql"
	"embed"

	appdb "appkit/db"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// FS exposes the embedded migration set to the service composition root.
var FS = migrationsFS

// Open opens wiki's SQLite database with the shared appkit pragmas.
func Open(dbPath string) (*sql.DB, error) {
	return appdb.Open(dbPath)
}

// Migrate applies wiki's embedded migrations through the shared runner.
func Migrate(ctx context.Context, conn *sql.DB) error {
	migs, err := appdb.LoadMigrations(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	return appdb.Migrate(ctx, conn, migs)
}

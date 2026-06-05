// Package db holds the dashboard's embedded migration set and a thin Open helper
// for the domain stores and their tests. The SQLite handle pragmas and the
// forward-only migration runner + downgrade guard are the uniform chassis half
// and now live in appkit/db (PLAN §B / §E6) — this package keeps only what is
// app-side: the dashboard's own *.sql files (embedded for Spec.Migrations).
//
// On the box, serve/migrate go through appkit.Spec.Migrations directly; Open
// here (open + migrate, the old behavior the store tests rely on) delegates to
// appkit/db so there is one Open + one runner implementation across every service.
package db

import (
	"context"
	"database/sql"
	"embed"

	"appkit/db"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// FS exposes the embedded migration set so cmd/dashboard can hand it to
// appkit.Spec.Migrations (the binary is the source of truth for its own schema).
var FS = migrationFiles

// Open opens the dashboard's SQLite database with the chassis pragmas (WAL, FK,
// single-writer) and applies any pending migrations, then returns the handle.
// It delegates to appkit/db so there is one Open + one runner across every
// service. The migrate-on-open keeps the prior behavior the domain store tests
// depend on.
func Open(path string) (*sql.DB, error) {
	conn, err := db.Open(path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(context.Background(), conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// Migrate applies the dashboard's embedded migrations against conn using appkit's
// forward-only runner + downgrade guard.
func Migrate(ctx context.Context, conn *sql.DB) error {
	migs, err := db.LoadMigrations(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	return db.Migrate(ctx, conn, migs)
}

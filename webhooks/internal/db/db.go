// Package db holds webhooks's embedded migration set and concrete Store over
// *sql.DB. The SQLite handle and forward-only migration runner + downgrade guard
// live in appkit/db; this package keeps only the app-side schema embed, the
// byte-equality guard that 003_outbox.sql matches the library DDL, and the
// webhooks domain store.
package db

import (
	"embed"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// FS exposes the embedded migration set so cmd/webhooks can hand it to
// appkit.Spec.Migrations (the binary is the source of truth for its own schema).
var FS = migrationsFS

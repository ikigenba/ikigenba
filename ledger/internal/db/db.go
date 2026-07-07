// Package db holds ledger's embedded migration set. The SQLite handle and
// migration runner live in appkit/db; this package keeps only the app-side *.sql
// files for Spec.Migrations and migration byte-equality guards.
package db

import "embed"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// FS exposes the embedded migration set so cmd/ledger can hand it to
// appkit.Spec.Migrations (the binary is the source of truth for its own schema).
var FS = migrationsFS

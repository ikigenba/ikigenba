// Package db holds crm's embedded migration set. The SQLite handle and the
// forward-only migration runner + downgrade guard are the uniform chassis half
// and live in appkit/db; this package keeps only what is app-side: the *.sql
// files (embedded for Spec.Migrations) and the byte-equality guard that
// 003_outbox.sql matches the library DDL (migrations_outbox_test.go).
package db

import (
	"embed"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// FS exposes the embedded migration set so cmd/crm can hand it to
// appkit.Spec.Migrations (the binary is the source of truth for its own schema).
var FS = migrationsFS

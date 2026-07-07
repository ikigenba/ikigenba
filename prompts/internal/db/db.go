// Package db holds prompts' embedded migration set and app-side migration guard
// tests. SQLite open and the forward-only migration runner live in appkit/db;
// this package keeps only the *.sql files embedded for Spec.Migrations.
package db

import "embed"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// FS exposes the embedded migration set so cmd/prompts can hand it to
// appkit.Spec.Migrations (the binary is the source of truth for its own schema).
var FS = migrationsFS

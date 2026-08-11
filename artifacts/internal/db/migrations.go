// Package db owns artifacts' embedded SQLite migrations.
package db

import "embed"

// FS contains the immutable artifacts schema migrations.
//
//go:embed migrations/*.sql
var FS embed.FS

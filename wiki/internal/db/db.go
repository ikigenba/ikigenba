// Package db owns wiki's embedded migration set.
package db

import (
	"embed"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// FS exposes the embedded migration set to the service composition root.
var FS = migrationsFS

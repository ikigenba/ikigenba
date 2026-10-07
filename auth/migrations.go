// Package auth carries the service's embedded migrations and page assets.
package auth

import (
	"embed"
	"io/fs"
)

//go:embed migrations/0001_baseline.sql migrations/0002_token_id_prefix.sql migrations/0003_mcp_clients.sql
var migrations embed.FS

// Migrations returns the embedded migrations independently of the working directory.
func Migrations() fs.FS {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err)
	}
	return files
}

// Package telemetry provides the app's embedded page templates.
package telemetry

import (
	"embed"
	"io/fs"
)

//go:embed assets/landing.html assets/about.html
var assets embed.FS

// Assets returns the templates independently of the working directory.
func Assets() fs.FS {
	f, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return f
}

//go:embed migrations/0001_trail.sql
var migrations embed.FS

// Migrations returns the embedded baseline database migration.
func Migrations() fs.FS {
	f, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err)
	}
	return f
}

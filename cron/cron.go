// Package cron supplies the files carried by the scheduler binary.
package cron

import (
	"embed"
	"io/fs"
)

//go:embed assets/landing.html assets/about.html assets/notfound.html
var assets embed.FS

//go:embed etc/manifest.toml etc/nginx.conf
var etc embed.FS

//go:embed migrations/0001_triggers.sql
var migrations embed.FS

// Assets returns the embedded page templates.
func Assets() fs.FS { return subtree(assets, "assets") }

// Etc returns the embedded app manifest and nginx fragment.
func Etc() fs.FS { return subtree(etc, "etc") }

// Migrations returns the embedded database migrations.
func Migrations() fs.FS { return subtree(migrations, "migrations") }

func subtree(files embed.FS, dir string) fs.FS {
	result, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	return result
}

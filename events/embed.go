// Package events supplies the event bus's embedded templates and configuration.
package events

import (
	"embed"
	"io/fs"
)

//go:embed assets/landing.html assets/tools.html assets/about.html assets/notfound.html assets/unavailable.html
var assets embed.FS

//go:embed migrations/0001_log.sql
var migrations embed.FS

//go:embed etc/manifest.toml etc/nginx.conf
var etc embed.FS

// Assets returns the templates at the root of an embedded file system.
func Assets() fs.FS {
	f, _ := fs.Sub(assets, "assets")
	return f
}

// Migrations returns the log schema at the root of an embedded file system.
func Migrations() fs.FS {
	f, _ := fs.Sub(migrations, "migrations")
	return f
}

// Etc returns the embedded deployment configuration under etc/.
func Etc() embed.FS { return etc }

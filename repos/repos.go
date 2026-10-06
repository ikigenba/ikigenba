// Package repos supplies the embedded markup and deployment configuration.
package repos

import (
	"embed"
	"io/fs"
)

//go:embed assets/landing.html assets/about.html
var assets embed.FS

//go:embed etc/manifest.toml etc/nginx.conf
var etc embed.FS

// Assets returns the embedded page templates rooted at their file names.
func Assets() fs.FS {
	f, _ := fs.Sub(assets, "assets")
	return f
}

// Etc returns the embedded deployment configuration rooted at its file names.
func Etc() fs.FS {
	f, _ := fs.Sub(etc, "etc")
	return f
}

//go:embed migrations/*
var migrations embed.FS

// Migrations returns the embedded catalog migrations at their root.
func Migrations() fs.FS {
	f, _ := fs.Sub(migrations, "migrations")
	return f
}

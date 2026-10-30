// Package home exposes the binary's embedded page templates and host files.
package home

import (
	"embed"
	"io/fs"
)

//go:embed assets/landing.html assets/about.html assets/notfound.html assets/bookmarks.html
var assets embed.FS

// Assets returns the embedded page templates at the filesystem root.
func Assets() fs.FS {
	result, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return result
}

//go:embed etc/manifest.toml
var etc embed.FS

// Etc returns the embedded host manifest at the filesystem root.
func Etc() fs.FS {
	result, err := fs.Sub(etc, "etc")
	if err != nil {
		panic(err)
	}
	return result
}

//go:embed migrations/0001_bookmarks.sql migrations/0002_bookmark_order.sql
var migrations embed.FS

// Migrations returns the database migrations at the filesystem root.
func Migrations() fs.FS {
	result, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err)
	}
	return result
}

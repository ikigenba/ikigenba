// Package scripts supplies the service's embedded page and deployment assets.
package scripts

import (
	"embed"
	"io/fs"
)

//go:embed assets/landing.html assets/script.html assets/run.html assets/about.html assets/notfound.html assets/unavailable.html
var assets embed.FS

//go:embed etc/manifest.toml
var etc embed.FS

//go:embed migrations/*.sql
var migrations embed.FS

// Migrations returns the catalog migrations at the file system root.
func Migrations() fs.FS {
	files, _ := fs.Sub(migrations, "migrations")
	return files
}

// Assets returns the page templates at the file system root.
func Assets() fs.FS {
	files, _ := fs.Sub(assets, "assets")
	return files
}

// Etc returns the embedded deployment manifest.
func Etc() embed.FS { return etc }

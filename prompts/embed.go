// Package prompts carries the application's embedded resources.
package prompts

import (
	"embed"
	"io/fs"
)

//go:embed assets/*.html
var assets embed.FS

//go:embed etc/manifest.toml etc/nginx.conf
var etc embed.FS

//go:embed migrations/*.sql
var migrations embed.FS

// Assets returns the seven embedded page templates.
func Assets() fs.FS { f, _ := fs.Sub(assets, "assets"); return f }

// Etc returns the embedded deployment files.
func Etc() embed.FS { return etc }

// Migrations returns the embedded catalog migrations.
func Migrations() fs.FS { f, _ := fs.Sub(migrations, "migrations"); return f }

// Package gdrive provides the service's embedded inputs.
package gdrive

import (
	"embed"
	"io/fs"
)

//go:embed assets/*.html etc/manifest.toml etc/nginx.conf migrations/*.sql
var inputs embed.FS

// Assets returns the page templates.
func Assets() fs.FS { return subtree("assets") }

// Etc returns the deployment configuration.
func Etc() fs.FS { return subtree("etc") }

// Migrations returns the transfer schema migrations.
func Migrations() fs.FS         { return subtree("migrations") }
func subtree(name string) fs.FS { f, _ := fs.Sub(inputs, name); return f }

// Package sessions exposes the resources shipped inside the service binary.
package sessions

import (
	"embed"
	"io/fs"
)

//go:embed assets/about.html assets/landing.html assets/notfound.html assets/tools.html etc/manifest.toml etc/nginx.conf migrations/0001_sessions.sql
var resources embed.FS

// Assets returns the service's page templates.
func Assets() fs.FS { return subtree("assets") }

// Etc returns the deployment configuration files.
func Etc() fs.FS { return subtree("etc") }

// Migrations returns the database migrations.
func Migrations() fs.FS { return subtree("migrations") }

func subtree(dir string) fs.FS {
	f, err := fs.Sub(resources, dir)
	if err != nil {
		panic(err)
	}
	return f
}

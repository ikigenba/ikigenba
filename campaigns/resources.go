// Package campaigns exposes the service's embedded resources.
package campaigns

import (
	"embed"
	"io/fs"
)

//go:embed assets/about.html assets/campaign.html assets/landing.html assets/notfound.html assets/tools.html
var assets embed.FS

//go:embed etc/manifest.toml etc/nginx.conf
var etc embed.FS

//go:embed migrations/0001_campaigns.sql migrations/0002_prospect_lists.sql
var migrations embed.FS

// Assets returns page templates rooted at their directory.
func Assets() fs.FS { return sub(assets, "assets") }

// Etc returns deployment files rooted at their directory.
func Etc() fs.FS { return sub(etc, "etc") }

// Migrations returns database migrations rooted at their directory.
func Migrations() fs.FS { return sub(migrations, "migrations") }

func sub(files embed.FS, dir string) fs.FS {
	root, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	return root
}

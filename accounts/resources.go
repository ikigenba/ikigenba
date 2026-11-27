// Package accounts exposes the service's embedded resources.
package accounts

import (
	"embed"
	"io/fs"
)

//go:embed assets/about.html assets/host.html assets/join.html assets/landing.html assets/notfound.html assets/tools.html
var assets embed.FS

//go:embed etc/manifest.toml etc/nginx.conf
var etc embed.FS

//go:embed migrations/0001_accounts.sql migrations/0002_hosts.sql migrations/0003_bans.sql migrations/0004_invites.sql migrations/0005_host_records.sql migrations/0006_kept_volumes.sql migrations/0007_host_backups.sql
var migrations embed.FS

// SeedVersion identifies the team template tag embedded in Seed.
const SeedVersion = "v1"

//go:embed seed/.gitignore seed/AGENTS.md seed/setup.md seed/data/.keep
var seed embed.FS

// Assets returns page templates rooted at their directory.
func Assets() fs.FS { return sub(assets, "assets") }

// Etc returns deployment files rooted at their directory.
func Etc() fs.FS { return sub(etc, "etc") }

// Migrations returns database migrations rooted at their directory.
func Migrations() fs.FS { return sub(migrations, "migrations") }

// Seed returns the team template files rooted at their directory.
func Seed() fs.FS { return sub(seed, "seed") }

func sub(files embed.FS, dir string) fs.FS {
	root, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	return root
}

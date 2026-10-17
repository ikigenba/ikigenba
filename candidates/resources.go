// Package candidates exposes the service's embedded resources.
package candidates

import (
	"embed"
	"encoding/csv"
	"io/fs"
	"strconv"
	"strings"
)

//go:embed assets/about.html assets/candidate.html assets/cohort.html assets/landing.html assets/notfound.html assets/page.html assets/pass.html assets/tools.html
var assets embed.FS

//go:embed etc/manifest.toml etc/nginx.conf
var etc embed.FS

//go:embed migrations/0001_candidates.sql migrations/0002_links.sql migrations/0003_estimates.sql migrations/0004_assessments.sql migrations/0005_decisions.sql migrations/0006_passes.sql migrations/0007_zip_drive.sql migrations/0008_windows.sql migrations/0009_judgments.sql migrations/0010_cohorts.sql migrations/0011_enrichments.sql migrations/0012_facts.sql migrations/0013_site_text.sql
var migrations embed.FS

//go:embed share/zip_drive.csv
var zipDrive string

// Assets returns the embedded page templates, rooted at their directory.
func Assets() fs.FS {
	return sub(assets, "assets")
}

// Etc returns the embedded deployment files, rooted at their directory.
func Etc() fs.FS {
	return sub(etc, "etc")
}

// Migrations returns the embedded database migrations, rooted at their directory.
func Migrations() fs.FS {
	return sub(migrations, "migrations")
}

func sub(files embed.FS, dir string) fs.FS {
	root, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	return root
}

// ZipDrive returns an independent copy of the shipped ZIP drive-time table.
func ZipDrive() map[string]int64 {
	rows, err := csv.NewReader(strings.NewReader(zipDrive)).ReadAll()
	if err != nil {
		panic(err)
	}
	table := make(map[string]int64, len(rows)-1)
	for _, row := range rows[1:] {
		minutes, err := strconv.ParseInt(row[1], 10, 64)
		if err != nil {
			panic(err)
		}
		table[row[0]] = minutes
	}
	return table
}

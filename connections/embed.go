// Package connections holds the service's embedded inputs.
package connections

import (
	"embed"
	"io/fs"
)

//go:embed assets/*
var assets embed.FS

//go:embed etc/*
var etc embed.FS

//go:embed migrations/*
var migrations embed.FS

// Assets returns the page templates.
func Assets() fs.FS { f, _ := fs.Sub(assets, "assets"); return f }

// Etc returns deployment configuration.
func Etc() fs.FS { f, _ := fs.Sub(etc, "etc"); return f }

// Migrations returns database migrations.
func Migrations() fs.FS { f, _ := fs.Sub(migrations, "migrations"); return f }

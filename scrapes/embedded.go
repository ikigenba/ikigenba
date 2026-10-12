// Package scrapes provides the service's embedded inputs.
package scrapes

import (
	"embed"
	"io/fs"
)

//go:embed assets/* etc/* migrations/*
var files embed.FS

// Assets returns the page templates.
func Assets() fs.FS { f, _ := fs.Sub(files, "assets"); return f }

// Etc returns the deployment configuration.
func Etc() fs.FS { f, _ := fs.Sub(files, "etc"); return f }

// Migrations returns the database migrations.
func Migrations() fs.FS { f, _ := fs.Sub(files, "migrations"); return f }

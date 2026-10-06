// Package dummy embeds the control panel assets.
package dummy

import (
	"embed"
	"io/fs"
)

//go:embed assets/page.html assets/table.html assets/form.html assets/script.html
var assets embed.FS

// Assets returns the embedded human-authored panel templates.
func Assets() fs.FS {
	files, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return files
}

//go:embed migrations/0001_widgets.sql
var migrations embed.FS

// Migrations returns the embedded baseline database migration.
func Migrations() fs.FS {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err)
	}
	return files
}

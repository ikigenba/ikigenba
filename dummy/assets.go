// Package dummy embeds the control panel assets.
package dummy

import (
	"embed"
	"io/fs"
)

//go:embed assets/*.html
var assets embed.FS

// Assets returns the embedded human-authored panel templates.
func Assets() fs.FS {
	files, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return files
}

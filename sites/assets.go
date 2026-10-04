// Package sites provides the service's embedded assets and deployment manifest.
package sites

import (
	"embed"
	"io/fs"
)

//go:embed assets/landing.html assets/about.html assets/notfound.html assets/unavailable.html
var assets embed.FS

//go:embed etc/manifest.toml
var etc embed.FS

// Assets returns the four page templates rooted at their file names.
func Assets() fs.FS {
	f, _ := fs.Sub(assets, "assets")
	return f
}

// Etc returns the deployment manifest rooted at its file name.
func Etc() fs.FS {
	f, _ := fs.Sub(etc, "etc")
	return f
}

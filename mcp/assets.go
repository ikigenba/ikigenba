// Package mcp provides the gateway's embedded page templates.
package mcp

import (
	"embed"
	"io/fs"
)

//go:embed assets/about.html assets/connect.html
var assets embed.FS

// Assets returns the embedded gateway templates, rooted at their filenames.
func Assets() fs.FS {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return root
}

// Package mcp provides the gateway's embedded connect template.
package mcp

import (
	"embed"
	"io/fs"
)

//go:embed assets/connect.html
var assets embed.FS

// Assets returns the embedded gateway templates, rooted at their filenames.
func Assets() fs.FS {
	root, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return root
}

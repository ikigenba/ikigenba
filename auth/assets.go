package auth

import (
	"embed"
	"io/fs"
)

//go:embed assets/approve.html assets/mcp-clients.html
var assets embed.FS

// Assets returns the embedded page assets independently of the working directory.
func Assets() fs.FS {
	files, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return files
}

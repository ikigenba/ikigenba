package auth

import (
	"embed"
	"io/fs"
)

//go:embed assets/about.html assets/approve.html assets/mcp-clients.html assets/page.html assets/profile.html assets/sign-in.html assets/token-create.html assets/token-created.html
var assets embed.FS

// Assets returns the embedded page assets independently of the working directory.
func Assets() fs.FS {
	files, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return files
}

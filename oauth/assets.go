// Package oauth provides the embedded callback page assets.
package oauth

import (
	"embed"
	"io/fs"
)

//go:embed assets/failure.html assets/success.html
var callbackAssets embed.FS

// Assets returns the callback templates at the file system's root.
func Assets() fs.FS {
	assets, err := fs.Sub(callbackAssets, "assets")
	if err != nil {
		panic(err)
	}
	return assets
}

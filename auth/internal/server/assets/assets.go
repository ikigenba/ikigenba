package assets

import "embed"

// Files is compiled into the executable so serving auth never needs a runtime
// asset directory.
//
//go:embed index.html app.js style.css
var Files embed.FS

package web

import "embed"

//go:embed landing.html
//go:embed static
var embedded embed.FS

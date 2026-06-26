package web

import "embed"

//go:embed landing.tmpl static
var content embed.FS

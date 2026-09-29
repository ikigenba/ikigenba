// Package auth embeds the platform style distributed with auth.
package auth

import "embed"

// Assets carries the style files into the executable.
//
//go:embed assets/*
var Assets embed.FS

// Package dummy embeds the platform assets served by the control panel.
package dummy

import "embed"

// Assets holds every file in the assets directory.
//
//go:embed all:assets
var Assets embed.FS

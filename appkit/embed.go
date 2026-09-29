// Package appkit provides the shared Ikigenba banner and its browser assets.
package appkit

import "embed"

// assetsFS contains the human-authored assets compiled into the package.
// It is read-only and never replaced after initialization.
//
//go:embed assets/banner.html assets/launcher.js assets/theme.css assets/InterVariable.woff2 assets/InterVariable-Italic.woff2 assets/JetBrainsMono.woff2 assets/OFL.txt assets/TABLER-LICENSE.txt
var assetsFS embed.FS

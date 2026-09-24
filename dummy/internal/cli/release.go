// Package cli implements the dummy command.
package cli

import (
	// The server package implements the service that this command runs.
	_ "github.com/ikigenba/ikigenba/dummy/internal/server"
)

// Version is the dummy release version.
var Version = "v0.3.0"

// Manifest describes dummy to the Ikigenba host.
const Manifest = "app = \"dummy\"\ndefault = false\nsecrets = []\n"

// Package cli implements the dummy command.
package cli

import "github.com/ikigenba/ikigenba/dummy/internal/panel"

// Manifest describes dummy to the Ikigenba host.
const Manifest = "app = \"dummy\"\ndescription = \"" + panel.Description + "\"\ndefault = false\nmcp = true\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/dummy.db\"\n\n[resources]\nmemory_max = \"64M\"\n"

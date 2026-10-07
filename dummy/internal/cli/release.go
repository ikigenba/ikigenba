// Package cli implements the dummy command.
package cli

// Version is the dummy release version.
var Version = "v0.14.1"

// Manifest describes dummy to the Ikigenba host.
const Manifest = "app = \"dummy\"\ndescription = \"Demo widgets to list and create\"\ndefault = false\nmcp = true\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/dummy.db\"\n\n[resources]\nmemory_max = \"64M\"\n"

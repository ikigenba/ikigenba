// Package cli implements the dummy command.
package cli

// Manifest describes dummy to the Ikigenba host.
const Manifest = "app = \"dummy\"\ndescription = \"Demo widgets to list and create\"\ndefault = false\nmcp = true\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/dummy.db\"\n\n[resources]\nmemory_max = \"64M\"\n"

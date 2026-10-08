package checkout

import (
	"os"
	"strings"
)

// HasApp reports whether the named root entry has an application's files,
// without decoding its manifest.
func (checkout *Checkout) HasApp(name string) bool {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return false
	}
	entries, err := os.ReadDir(checkout.Root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() != name || !entry.IsDir() {
			continue
		}
		dir := checkout.Path(name)
		info, err := os.Stat(checkout.Path(name, ManifestFile))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		main, err := hasMainPackage(dir, name)
		return err == nil && main
	}
	return false
}

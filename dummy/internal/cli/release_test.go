package cli

import (
	"testing"
)

// R-SCNW-L802
func TestManifestConstant(t *testing.T) {
	t.Parallel()

	const compiledAsConstant = Manifest
	const want = "app = \"dummy\"\ndescription = \"Demo widgets to list and create\"\ndefault = false\nmcp = true\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/dummy.db\"\n\n[resources]\nmemory_max = \"64M\"\n"
	if compiledAsConstant != want {
		t.Errorf("Manifest = %q, want %q", compiledAsConstant, want)
	}
}

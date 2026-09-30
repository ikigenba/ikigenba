package cli

import (
	"regexp"
	"testing"
)

// R-ANRH-UBLX
func TestVersionIsSemanticVersion(t *testing.T) {
	t.Parallel()

	semver := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	if !semver.MatchString(Version) {
		t.Errorf("Version %q is not v-prefixed Semantic Versioning", Version)
	}
}

// R-LI0D-VJTO
func TestManifestConstant(t *testing.T) {
	t.Parallel()

	const compiledAsConstant = Manifest
	const want = "app = \"dummy\"\ndefault = false\nsecrets = []\n"
	if compiledAsConstant != want {
		t.Errorf("Manifest = %q, want %q", compiledAsConstant, want)
	}
}

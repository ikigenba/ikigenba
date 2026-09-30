package version

import (
	"regexp"
	"testing"
)

func TestVersionIsSourceLiteral(t *testing.T) {
	// R-3O47-G1QT: this package owns the release version and exports Version.
	if Version == "" {
		t.Fatal("Version is empty")
	}
}

// R-2C9G-ACYG: Version is an exported string variable; only a variable of
// type string has an address assignable to *string.
var _ *string = &Version

func TestVersionIsSemver(t *testing.T) {
	// R-2C9G-ACYG: its value is a leading v followed by a semantic version.
	if !regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`).MatchString(Version) {
		t.Fatalf("Version %q is not v<semver>", Version)
	}
}

package cli

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

// R-VO8R-HW15
func TestManifestConstant(t *testing.T) {
	t.Parallel()
	const compiledAsConstant string = Manifest
	prefix := "app = \"dummy\"\ndescription = "
	suffix := "\ndefault = false\nmcp = true\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/dummy.db\"\n\n[resources]\nmemory_max = \"64M\"\n"
	if !strings.HasPrefix(compiledAsConstant, prefix) || !strings.HasSuffix(compiledAsConstant, suffix) {
		t.Fatal("manifest contract")
	}
	description := strings.TrimSuffix(strings.TrimPrefix(compiledAsConstant, prefix), suffix)
	value, err := strconv.Unquote(description)
	basicString := regexp.MustCompile(`^"(?:[^"\\\x00-\x1f\x7f]|\\(?:[btnfr"\\]|u[0-9a-fA-F]{4}|U[0-9a-fA-F]{8}))*"$`)
	if err != nil || value == "" || !basicString.MatchString(description) {
		t.Fatalf("description is not a nonempty single-line basic string: %q", description)
	}
}

// R-YDR4-WIEE
func TestManifestDescription(t *testing.T) {
	t.Parallel()
	line := "description = \"" + panel.Description + "\""
	if !slices.Contains(strings.Split(Manifest, "\n"), line) {
		t.Fatal("manifest does not publish the panel description")
	}
}

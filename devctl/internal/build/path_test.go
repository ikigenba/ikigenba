package build_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
)

func TestArtifactPaths(t *testing.T) {
	// R-5MRW-F52S
	// R-5NZS-SWTH
	if got := build.DistDir("crm"); got != "crm/dist" {
		t.Fatalf("DistDir() = %q, want %q", got, "crm/dist")
	}
	for _, sha := range []string{"4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a", "verbatim-SHA"} {
		want := "crm/dist/crm-" + sha + ".tar.xz"
		if got := build.File("crm", sha); got != want {
			t.Fatalf("File(%q) = %q, want %q", sha, got, want)
		}
	}
}

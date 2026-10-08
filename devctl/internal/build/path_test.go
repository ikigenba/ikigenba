package build_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
)

func TestReleaseFile(t *testing.T) {
	// R-FNXL-RRZY
	// R-FP5I-5JQN
	for _, sha := range []string{"4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a", "verbatim-SHA"} {
		if got := build.ReleaseFile(sha); got != "dist/"+sha+".tar.xz" {
			t.Fatalf("ReleaseFile(%q) = %q", sha, got)
		}
	}
}

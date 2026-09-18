package build_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
)

func TestArtifactPaths(t *testing.T) {
	// R-F1YC-BLHM
	// R-F368-PD8B
	if got := build.DistDir("crm"); got != "crm/dist" {
		t.Fatalf("DistDir() = %q, want %q", got, "crm/dist")
	}
	for _, version := range []string{"v0.1.0", "v1.2.3-rc.1+build.7"} {
		want := "crm/dist/crm-" + version + ".tar.xz"
		if got := build.File("crm", version); got != want {
			t.Fatalf("File(%q) = %q, want %q", version, got, want)
		}
	}
}

package build_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
)

func TestProcessError(t *testing.T) {
	// R-EM3N-CKUL
	_ = build.ProcessError(struct {
		Label  string
		Status int
		Stderr string
	}{})

	err := &build.ProcessError{Label: "build crm", Status: 23, Stderr: "first\nsecond\n"}
	if got := err.Error(); got != "build crm: exit status 23" {
		t.Fatalf("Error() = %q", got)
	}
	if got := err.Detail(); got != "> first\n> second" {
		t.Fatalf("Detail() = %q", got)
	}
	if got := err.ExitCode(); got != 1 {
		t.Fatalf("ExitCode() = %d, want 1", got)
	}
}

func TestUsageError(t *testing.T) {
	// R-67JH-CUAG
	_ = build.UsageError(struct {
		Message string
		Help    string
	}{})

	err := &build.UsageError{Message: "bad input", Help: "devctl build --help"}
	if got := err.Error(); got != "bad input" {
		t.Fatalf("Error() = %q, want %q", got, "bad input")
	}
	if got := err.Detail(); got != "see 'devctl build --help' for usage" {
		t.Fatalf("Detail() = %q", got)
	}
	if got := err.ExitCode(); got != 2 {
		t.Fatalf("ExitCode() = %d, want 2", got)
	}
	err.Help = ""
	if got := err.Detail(); got != "" {
		t.Fatalf("Detail() with empty help = %q, want empty", got)
	}
}

func TestStaleManifestError(t *testing.T) {
	// R-69ZA-4DRU
	_ = build.StaleManifestError(struct{ App string }{})

	err := &build.StaleManifestError{App: "crm"}
	want := "crm: etc/manifest.toml does not match what the binary emits; run 'crm manifest > crm/etc/manifest.toml' and commit"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got := err.ExitCode(); got != 2 {
		t.Fatalf("ExitCode() = %d, want 2", got)
	}
}

//go:build !linux && !darwin

package browser

import "testing"

// R-GW4S-WP9L
func TestOpenReportsUnsupportedPlatformWithoutCommand(t *testing.T) {
	original := newCommand
	t.Cleanup(func() { newCommand = original })

	factoryCalls := 0
	newCommand = func(string, ...string) command {
		factoryCalls++
		t.Fatal("unsupported launcher attempted to construct a command")
		return nil
	}

	err := New().Open("https://authorize.example/unsupported")
	if err == nil {
		t.Fatal("Open() error = nil, want unsupported-platform error")
	}
	if factoryCalls != 0 {
		t.Fatalf("command factory calls = %d, want 0", factoryCalls)
	}
}

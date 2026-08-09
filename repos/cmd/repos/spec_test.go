package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveStateDirPrefersOverrideAndMakesItAbsolute(t *testing.T) {
	// R-QX3N-8GNH
	override := filepath.Join(t.TempDir(), "override")
	resolved, err := resolveStateDir(mapGetenv(map[string]string{
		"REPOS_STATE_DIR": override,
		"IKIGENBA_ROOT":   filepath.Join(t.TempDir(), "install"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if resolved != override {
		t.Fatalf("resolved state dir = %q, want override %q", resolved, override)
	}

	relative := filepath.Join("relative", "state")
	wantAbsolute, err := filepath.Abs(relative)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = resolveStateDir(mapGetenv(map[string]string{"REPOS_STATE_DIR": relative}))
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wantAbsolute {
		t.Fatalf("resolved state dir = %q, want absolute override %q", resolved, wantAbsolute)
	}
}

func TestResolveStateDirRequiresOverrideOrInstallRootWithoutCreatingState(t *testing.T) {
	// R-QZJG-004V
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	resolved, err := resolveStateDir(mapGetenv(nil))
	if err == nil {
		t.Fatalf("resolveStateDir returned %q without a configuration error", resolved)
	}
	for _, name := range []string{"REPOS_STATE_DIR", "IKIGENBA_ROOT"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
	entries, readErr := os.ReadDir(workingDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("state resolution created entries without a configured root: %#v", entries)
	}
}

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// confinePath is the sandbox security boundary. These tests pin its
// behavior hard: it must reject `..` escapes, absolute paths outside the
// root, and symlink escapes (a symlink inside the root pointing out),
// while accepting legitimate in-root relative and absolute paths. The
// symlink case must be caught even when the leaf file does not yet exist
// (write/edit create new files).
func TestConfinePath_Rejections(t *testing.T) {
	root := t.TempDir()

	// A directory outside the root, and a symlink inside root pointing to it.
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"dotdot_relative", "../outside"},
		{"absolute_outside", "/etc/passwd"},
		{"absolute_other_tmp", filepath.Join(outside, "x.txt")},
		{"symlink_escape_existing_target", filepath.Join("escape", "file.txt")},
		{"symlink_escape_nonexistent_leaf", filepath.Join("escape", "does-not-exist-yet.txt")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := confinePath(root, tc.path)
			if err == nil {
				t.Fatalf("confinePath(%q) = %q, want escape error", tc.path, got)
			}
			if !strings.Contains(err.Error(), "escapes sandbox") {
				t.Fatalf("error = %v, want 'escapes sandbox'", err)
			}
		})
	}
}

func TestConfinePath_Accepts(t *testing.T) {
	root := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	// A real subdirectory so symlink resolution of the ancestor succeeds.
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"relative_in_root", "notes.txt", filepath.Join(root, "notes.txt")},
		{"relative_nested", filepath.Join("sub", "a.txt"), filepath.Join(root, "sub", "a.txt")},
		{"absolute_in_root", filepath.Join(realRoot, "abs.txt"), filepath.Join(realRoot, "abs.txt")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := confinePath(root, tc.path)
			if err != nil {
				t.Fatalf("confinePath(%q): unexpected error: %v", tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("confinePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// Empty root is legacy/unconfined mode: paths pass through unchanged.
func TestConfinePath_EmptyRootPassesThrough(t *testing.T) {
	got, err := confinePath("", "/anywhere/at/all.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/anywhere/at/all.txt" {
		t.Fatalf("got %q, want passthrough", got)
	}
}

func TestEffectiveSearchPath(t *testing.T) {
	root := t.TempDir()

	// Legacy mode: passthrough including empty.
	if got, _ := effectiveSearchPath("", ""); got != "" {
		t.Errorf("legacy empty: got %q, want empty", got)
	}
	if got, _ := effectiveSearchPath("", "/x"); got != "/x" {
		t.Errorf("legacy abs: got %q, want /x", got)
	}

	// Under a root: empty path defaults to the root.
	if got, _ := effectiveSearchPath(root, ""); got != root {
		t.Errorf("rooted empty: got %q, want %q", got, root)
	}

	// Under a root: in-root path is confined.
	want := filepath.Join(root, "sub")
	if got, err := effectiveSearchPath(root, "sub"); err != nil || got != want {
		t.Errorf("rooted sub: got %q,%v want %q", got, err, want)
	}

	// Under a root: escape is rejected.
	if _, err := effectiveSearchPath(root, "../out"); err == nil {
		t.Error("rooted escape: want error, got nil")
	}
}

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// confinePath resolves p against the sandbox root and verifies the
// result stays inside root, defending against symlink escapes even when
// the leaf does not yet exist (write/edit create new files).
//
// When root is empty, p is returned unchanged (legacy/unconfined mode);
// agent always supplies a non-empty root, but the engine's own tests may
// run unconfined.
//
// On success it returns the cleaned absolute path, suitable to hand to
// read/write/edit (which require absolute paths). On a containment
// violation it returns an error whose message names the offending path.
func confinePath(root, p string) (string, error) {
	if root == "" {
		return p, nil
	}

	// Resolve p to an absolute, cleaned path.
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)

	// Real root: resolve symlinks if it exists, else clean it.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = filepath.Clean(root)
	}

	// Resolve symlinks on the longest existing ancestor of abs, then
	// re-append the non-existing remainder. This catches a symlink
	// anywhere in the chain that escapes the root even when the leaf
	// does not exist yet.
	resolved := resolveLongestExisting(abs)

	rel, err := filepath.Rel(realRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes sandbox: %q", p)
	}

	return abs, nil
}

// effectiveSearchPath computes the directory/path glob and grep should
// search under. In legacy mode (root == "") the caller's path is passed
// through unchanged. Under a root, an empty path defaults to the root
// itself; a non-empty path is confined to the root.
func effectiveSearchPath(root, p string) (string, error) {
	if root == "" {
		return p, nil
	}
	if p == "" {
		return root, nil
	}
	return confinePath(root, p)
}

// resolveLongestExisting walks up abs until an existing ancestor is
// found, EvalSymlinks-resolves that prefix, and re-appends the
// non-existing remainder.
func resolveLongestExisting(abs string) string {
	existing := abs
	var remainder string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			// Reached the filesystem root without finding an existing
			// ancestor; nothing to resolve.
			return abs
		}
		remainder = filepath.Join(filepath.Base(existing), remainder)
		existing = parent
	}
	resolvedPrefix, err := filepath.EvalSymlinks(existing)
	if err != nil {
		resolvedPrefix = existing
	}
	if remainder == "" {
		return resolvedPrefix
	}
	return filepath.Join(resolvedPrefix, remainder)
}

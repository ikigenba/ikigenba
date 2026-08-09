package sites

import (
	"errors"
	"strings"
	"unicode"
)

// ErrInvalidPath identifies a publish root outside the repository-relative
// slash-separated grammar.
var ErrInvalidPath = errors.New("invalid publish path")

// ValidatePath normalizes and validates a repository-relative publish root.
func ValidatePath(raw string) (string, error) {
	if strings.Contains(raw, `\`) {
		return "", ErrInvalidPath
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", ErrInvalidPath
		}
	}
	path := strings.Trim(raw, "/")
	if path == "" {
		if raw == "" || raw == "/" {
			return "", nil
		}
		return "", ErrInvalidPath
	}
	if len(path) > 255 {
		return "", ErrInvalidPath
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.EqualFold(segment, ".git") {
			return "", ErrInvalidPath
		}
	}
	return path, nil
}

// Subtree keeps entries beneath root and strips that root from their paths.
func Subtree(entries []TreeEntry, root string) []TreeEntry {
	if root == "" {
		return entries
	}
	prefix := root + "/"
	mapped := make([]TreeEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Path, prefix) {
			entry.Path = strings.TrimPrefix(entry.Path, prefix)
			mapped = append(mapped, entry)
		}
	}
	return mapped
}

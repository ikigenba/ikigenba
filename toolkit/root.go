package toolkit

import (
	"fmt"
	"os"
	"path/filepath"
)

func resolveRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("root %q is empty", root)
	}

	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("root %q could not be resolved: %w", root, err)
	}
	if _, err := os.Stat(absolute); err != nil {
		return "", fmt.Errorf("root %q could not be resolved: %w", root, err)
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("root %q could not be resolved: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("root %q could not be resolved: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root %q is not a directory", root)
	}
	return resolved, nil
}

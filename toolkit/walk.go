package toolkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/boyter/gocodewalker"
)

func walkTree(root, searchDir string, skipPatterns []string) ([]string, error) {
	queue := make(chan *gocodewalker.File, 100)
	walker := gocodewalker.NewFileWalker(searchDir, queue)
	walker.IncludeHidden = true
	walker.CustomIgnorePatterns = skipPatterns
	walker.SetErrorHandler(func(error) bool { return false })

	walkErr := make(chan error, 1)
	go func() {
		walkErr <- walker.Start()
	}()

	var paths []string
	for file := range queue {
		path := filepath.Clean(file.Location)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}

		target := path
		isSymlink := info.Mode()&os.ModeSymlink != 0
		if isSymlink {
			target, err = filepath.EvalSymlinks(path)
			if err != nil {
				continue
			}
		}
		targetInfo, err := os.Stat(target)
		if err != nil || !targetInfo.Mode().IsRegular() {
			continue
		}
		if isSymlink && !pathWithinRoot(root, target) {
			continue
		}
		paths = append(paths, path)
	}
	if err := <-walkErr; err != nil {
		return nil, err
	}
	return paths, nil
}

func resolveSearchPath(root, argName, path string) (string, error) {
	if path == "" {
		return root, nil
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)

	existing := candidate
	var suffix []string
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("%s %q could not be resolved: %w", argName, path, err)
		}

		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("%s %q could not be resolved", argName, path)
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("%s %q could not be resolved: %w", argName, path, err)
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	resolved = filepath.Clean(resolved)
	if !pathWithinRoot(root, resolved) {
		return "", fmt.Errorf("%s %q resolves outside the tool root", argName, path)
	}
	return resolved, nil
}

func pathWithinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

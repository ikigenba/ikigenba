package toolkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/ikigenba/ikigenba/agentkit"
)

type globInput struct {
	Pattern string `json:"pattern"        jsonschema:"required,minLength=1,description=Doublestar glob matched against file paths relative to the search directory"`
	Path    string `json:"path,omitempty" jsonschema:"description=Directory to search relative to the tool root"`
}

type globMatch struct {
	path    string
	modTime time.Time
}

// Glob creates a tool that finds files whose relative paths match a doublestar glob.
func Glob(root string, opts ...GlobOption) (agentkit.Tool, error) {
	config := globConfig{}
	for _, opt := range opts {
		opt.applyGlob(&config)
	}

	return agentkit.NewTool[globInput]("Glob", "Find files matching a doublestar glob", func(_ context.Context, input globInput) (string, error) {
		if !doublestar.ValidatePattern(input.Pattern) {
			return "", fmt.Errorf("pattern %q is not a valid glob", input.Pattern)
		}

		searchDir, err := resolveSearchPath(root, "path", input.Path)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(searchDir)
		if err != nil {
			return "", fmt.Errorf("path %q could not be resolved: %w", input.Path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("path %q is not a directory", input.Path)
		}

		matches, err := collectGlobMatches(root, searchDir, input.Pattern, config.skipPatterns)
		if err != nil {
			return "", err
		}
		sortGlobMatches(matches)
		return renderGlobMatches(matches), nil
	})
}

func collectGlobMatches(root, searchDir, pattern string, skipPatterns []string) ([]globMatch, error) {
	paths, err := walkTree(root, searchDir, skipPatterns)
	if err != nil {
		return nil, err
	}

	var matches []globMatch
	for _, path := range paths {
		rel, err := filepath.Rel(searchDir, path)
		if err != nil {
			return nil, err
		}
		matched, _ := doublestar.Match(pattern, filepath.ToSlash(rel))
		if !matched {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		matches = append(matches, globMatch{path: path, modTime: info.ModTime()})
	}
	return matches, nil
}

func sortGlobMatches(matches []globMatch) {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].modTime.Equal(matches[j].modTime) {
			return matches[i].path < matches[j].path
		}
		return matches[i].modTime.After(matches[j].modTime)
	})
}

func renderGlobMatches(matches []globMatch) string {
	if len(matches) == 0 {
		return "No files found"
	}
	paths := make([]string, len(matches))
	for i := range matches {
		paths[i] = matches[i].path
	}
	return strings.Join(paths, "\n")
}

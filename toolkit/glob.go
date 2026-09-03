package toolkit

import (
	"context"
	"fmt"
	"io/fs"
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

		// Path confinement is deliberately added by a later build phase.
		matches, err := collectGlobMatches(root, input.Pattern)
		if err != nil {
			return "", err
		}
		sortGlobMatches(matches)
		return renderGlobMatches(matches), nil
	})
}

func collectGlobMatches(root, pattern string) ([]globMatch, error) {
	var matches []globMatch
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		matched, _ := doublestar.Match(pattern, filepath.ToSlash(rel))
		if !matched {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		matches = append(matches, globMatch{path: path, modTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
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

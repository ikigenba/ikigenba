package toolkit

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
)

type grepInput struct {
	Pattern    string `json:"pattern"               jsonschema:"required,minLength=1,description=Regular expression to search for in file contents"`
	Path       string `json:"path,omitempty"        jsonschema:"description=File or directory to search relative to the tool root"`
	Glob       string `json:"glob,omitempty"        jsonschema:"description=Doublestar glob used to filter searched files"`
	OutputMode string `json:"output_mode,omitempty" jsonschema:"enum=files_with_matches|content|count,description=Format for matching search results"`
	IgnoreCase bool   `json:"-i,omitempty"          jsonschema:"description=Match the pattern case-insensitively"`
	LineNumber *bool  `json:"-n,omitempty"          jsonschema:"description=Include line numbers in content output"`
	After      *int   `json:"-A,omitempty"          jsonschema:"minimum=0,description=Number of lines to show after each match"`
	Before     *int   `json:"-B,omitempty"          jsonschema:"minimum=0,description=Number of lines to show before each match"`
	Context    *int   `json:"-C,omitempty"          jsonschema:"minimum=0,description=Number of lines to show before and after each match"`
	Multiline  bool   `json:"multiline,omitempty"   jsonschema:"description=Allow matches to span multiple lines"`
	HeadLimit  *int   `json:"head_limit,omitempty"  jsonschema:"minimum=1,description=Maximum number of results to return"`
}

// Grep creates a tool that searches regular files for a Go regular expression.
func Grep(root string, opts ...GrepOption) (agentkit.Tool, error) {
	config := grepConfig{}
	for _, opt := range opts {
		opt.applyGrep(&config)
	}

	return agentkit.NewTool[grepInput]("Grep", "Search file contents with a regular expression", func(_ context.Context, input grepInput) (string, error) {
		return runGrep(root, input)
	})
}

func runGrep(root string, input grepInput) (string, error) {
	pattern, err := compileGrepPattern(input.Pattern, input.IgnoreCase)
	if err != nil {
		return "", err
	}

	// Path confinement is deliberately added by a later build phase.
	matches, err := collectGrepMatches(root, pattern)
	if err != nil {
		return "", err
	}
	return renderGrepMatches(matches), nil
}

func compileGrepPattern(pattern string, ignoreCase bool) (*regexp.Regexp, error) {
	source := pattern
	if ignoreCase {
		source = "(?i)" + source
	}
	compiled, err := regexp.Compile(source)
	if err != nil {
		return nil, fmt.Errorf("pattern %q is not a valid regular expression: %w", pattern, err)
	}
	return compiled, nil
}

func collectGrepMatches(root string, pattern *regexp.Regexp) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		paths = append(paths, filepath.Clean(path))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("searching %q stopped early: %w", root, err)
	}

	var matches []string
	for _, path := range paths {
		path = filepath.Clean(path)
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %q failed; search in %q was truncated: %w", path, root, err)
		}
		if pattern.Match(contents) {
			matches = append(matches, path)
		}
	}
	sort.Strings(matches)
	return matches, nil
}

func renderGrepMatches(matches []string) string {
	if len(matches) == 0 {
		return "No matches found"
	}
	// Count and content output modes are deliberately added by a later build phase.
	return strings.Join(matches, "\n")
}

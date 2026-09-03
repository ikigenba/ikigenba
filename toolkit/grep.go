package toolkit

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	switch input.OutputMode {
	case "count":
		return renderGrepCounts(matches), nil
	case "content":
		return renderGrepContent(matches, input), nil
	default:
		return renderGrepMatches(matches), nil
	}
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

type grepMatch struct {
	path    string
	lines   []string
	matched []bool
}

func collectGrepMatches(root string, pattern *regexp.Regexp) ([]grepMatch, error) {
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

	var matches []grepMatch
	for _, path := range paths {
		path = filepath.Clean(path)
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %q failed; search in %q was truncated: %w", path, root, err)
		}
		lines := strings.Split(string(contents), "\n")
		matched := make([]bool, len(lines))
		hasMatch := false
		for i, line := range lines {
			matched[i] = pattern.MatchString(line)
			hasMatch = hasMatch || matched[i]
		}
		if hasMatch {
			matches = append(matches, grepMatch{path: path, lines: lines, matched: matched})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].path < matches[j].path })
	return matches, nil
}

func renderGrepMatches(matches []grepMatch) string {
	if len(matches) == 0 {
		return "No matches found"
	}
	paths := make([]string, len(matches))
	for i, match := range matches {
		paths[i] = match.path
	}
	return strings.Join(paths, "\n")
}

func renderGrepCounts(matches []grepMatch) string {
	if len(matches) == 0 {
		return "No matches found"
	}
	results := make([]string, len(matches))
	for i, match := range matches {
		count := 0
		for _, lineMatched := range match.matched {
			if lineMatched {
				count++
			}
		}
		results[i] = match.path + ":" + strconv.Itoa(count)
	}
	return strings.Join(results, "\n")
}

type grepRange struct {
	start int
	end   int
}

func renderGrepContent(matches []grepMatch, input grepInput) string {
	if len(matches) == 0 {
		return "No matches found"
	}
	lineNumbers := input.LineNumber == nil || *input.LineNumber
	before := contextSize(input.Before, input.Context)
	after := contextSize(input.After, input.Context)
	var groups []string
	for _, match := range matches {
		for _, group := range grepContextRanges(match.matched, before, after) {
			lines := make([]string, 0, group.end-group.start+1)
			for i := group.start; i <= group.end; i++ {
				lines = append(lines, renderGrepContentLine(match, i, lineNumbers))
			}
			groups = append(groups, strings.Join(lines, "\n"))
		}
	}
	return strings.Join(groups, "\n--\n")
}

func contextSize(specific, common *int) int {
	if specific != nil {
		return *specific
	}
	if common != nil {
		return *common
	}
	return 0
}

func grepContextRanges(matched []bool, before, after int) []grepRange {
	var ranges []grepRange
	for line, lineMatched := range matched {
		if !lineMatched {
			continue
		}
		current := grepRange{start: max(0, line-before), end: min(len(matched)-1, line+after)}
		if len(ranges) > 0 && current.start <= ranges[len(ranges)-1].end+1 {
			ranges[len(ranges)-1].end = max(ranges[len(ranges)-1].end, current.end)
			continue
		}
		ranges = append(ranges, current)
	}
	return ranges
}

func renderGrepContentLine(match grepMatch, line int, lineNumbers bool) string {
	separator := "-"
	if match.matched[line] {
		separator = ":"
	}
	if lineNumbers {
		return match.path + separator + strconv.Itoa(line+1) + separator + match.lines[line]
	}
	return match.path + separator + match.lines[line]
}

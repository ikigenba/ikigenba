package toolkit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/ikigenba/ikigenba/agentkit"
)

const defaultGrepHeadLimit = 250

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
		return runGrep(root, config.skipPatterns, input)
	})
}

func runGrep(root string, skipPatterns []string, input grepInput) (string, error) {
	pattern, err := compileGrepPattern(input.Pattern, input.IgnoreCase, input.Multiline)
	if err != nil {
		return "", err
	}
	if input.Glob != "" && !doublestar.ValidatePattern(input.Glob) {
		return "", fmt.Errorf("glob %q is not valid", input.Glob)
	}

	matches, err := collectGrepMatches(root, skipPatterns, pattern, input)
	if err != nil {
		return "", err
	}
	limit := defaultGrepHeadLimit
	if input.HeadLimit != nil {
		limit = *input.HeadLimit
	}
	switch input.OutputMode {
	case "count":
		return renderLimitedGrepEntries(renderGrepCounts(matches), limit), nil
	case "content":
		return renderGrepContent(matches, input, limit), nil
	default:
		return renderLimitedGrepEntries(renderGrepMatches(matches), limit), nil
	}
}

func compileGrepPattern(pattern string, ignoreCase, multiline bool) (*regexp.Regexp, error) {
	source := pattern
	var flags string
	if ignoreCase {
		flags += "i"
	}
	if multiline {
		flags += "s"
	}
	if flags != "" {
		source = "(?" + flags + ")" + source
	}
	compiled, err := regexp.Compile(source)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", pattern, err)
	}
	return compiled, nil
}

type grepMatch struct {
	path  string
	lines []string
	spans []grepSpan
}

type grepSpan struct {
	start int
	end   int
}

func collectGrepMatches(root string, skipPatterns []string, pattern *regexp.Regexp, input grepInput) ([]grepMatch, error) {
	searchPath, err := resolveSearchPath(root, "path", input.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(searchPath)
	if err != nil {
		return nil, fmt.Errorf("path %q could not be resolved: %w", input.Path, err)
	}

	searchBase := searchPath
	if info.Mode().IsRegular() {
		searchBase = filepath.Dir(searchPath)
		match, err := matchGrepFile(searchPath, searchBase, pattern, input)
		if err != nil {
			return nil, err
		}
		if match == nil {
			return nil, nil
		}
		return []grepMatch{*match}, nil
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path %q is not a regular file or directory", input.Path)
	}

	paths, err := walkTree(root, searchPath, skipPatterns)
	if err != nil {
		return nil, fmt.Errorf("searching %q stopped early: %w", searchPath, err)
	}

	var matches []grepMatch
	for _, path := range paths {
		match, err := matchGrepFile(path, searchBase, pattern, input)
		if err != nil {
			return nil, fmt.Errorf("reading %q failed; search in %q was truncated: %w", path, searchPath, err)
		}
		if match != nil {
			matches = append(matches, *match)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].path < matches[j].path })
	return matches, nil
}

func matchGrepFile(path, searchBase string, pattern *regexp.Regexp, input grepInput) (*grepMatch, error) {
	if input.Glob != "" {
		rel, err := filepath.Rel(searchBase, path)
		if err != nil {
			return nil, err
		}
		matched, _ := doublestar.Match(input.Glob, filepath.ToSlash(rel))
		if !matched {
			return nil, nil
		}
	}

	path = filepath.Clean(path)
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	prefixLength := min(8192, len(contents))
	if bytes.IndexByte(contents[:prefixLength], 0) >= 0 {
		return nil, nil
	}

	text := string(contents)
	lines := strings.Split(text, "\n")
	var spans []grepSpan
	if input.Multiline {
		for _, offsets := range pattern.FindAllStringIndex(text, -1) {
			start := strings.Count(text[:offsets[0]], "\n")
			end := start + strings.Count(text[offsets[0]:offsets[1]], "\n")
			spans = append(spans, grepSpan{start: start, end: end})
		}
	} else {
		for line, contents := range lines {
			if pattern.MatchString(contents) {
				spans = append(spans, grepSpan{start: line, end: line})
			}
		}
	}
	if len(spans) == 0 {
		return nil, nil
	}
	return &grepMatch{path: path, lines: lines, spans: spans}, nil
}

func renderGrepMatches(matches []grepMatch) []string {
	paths := make([]string, len(matches))
	for i, match := range matches {
		paths[i] = match.path
	}
	return paths
}

func renderGrepCounts(matches []grepMatch) []string {
	results := make([]string, len(matches))
	for i, match := range matches {
		results[i] = strings.Join([]string{match.path, strconv.Itoa(len(match.spans))}, ":")
	}
	return results
}

type grepRange struct {
	start int
	end   int
}

func renderGrepContent(matches []grepMatch, input grepInput, limit int) string {
	if len(matches) == 0 {
		return "No matches found"
	}
	lineNumbers := input.LineNumber == nil || *input.LineNumber
	before := contextSize(input.Before, input.Context)
	after := contextSize(input.After, input.Context)
	var lines []string
	var groupIDs []int
	groupID := 0
	for _, match := range matches {
		for _, group := range grepContextRanges(match.spans, len(match.lines), before, after) {
			for i := group.start; i <= group.end; i++ {
				lines = append(lines, renderGrepContentLine(match, i, lineNumbers))
				groupIDs = append(groupIDs, groupID)
			}
			groupID++
		}
	}

	truncated := len(lines) > limit
	if truncated {
		lines = lines[:limit]
		groupIDs = groupIDs[:limit]
	}
	result := make([]string, 0, len(lines)+groupID)
	for i, line := range lines {
		if i > 0 && groupIDs[i] != groupIDs[i-1] {
			result = append(result, "--")
		}
		result = append(result, line)
	}
	if truncated {
		result = append(result, truncationMessage(limit))
	}
	return strings.Join(result, "\n")
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

func grepContextRanges(spans []grepSpan, lineCount, before, after int) []grepRange {
	var ranges []grepRange
	for _, span := range spans {
		current := grepRange{start: max(0, span.start-before), end: min(lineCount-1, span.end+after)}
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
	if lineInSpans(match.spans, line) {
		separator = ":"
	}
	if lineNumbers {
		return strings.Join([]string{match.path, strconv.Itoa(line + 1), match.lines[line]}, separator)
	}
	return strings.Join([]string{match.path, match.lines[line]}, separator)
}

func lineInSpans(spans []grepSpan, line int) bool {
	for _, span := range spans {
		if line < span.start {
			return false
		}
		if line <= span.end {
			return true
		}
	}
	return false
}

func renderLimitedGrepEntries(entries []string, limit int) string {
	if len(entries) == 0 {
		return "No matches found"
	}
	if len(entries) <= limit {
		return strings.Join(entries, "\n")
	}
	limited := append([]string(nil), entries[:limit]...)
	limited = append(limited, truncationMessage(limit))
	return strings.Join(limited, "\n")
}

func truncationMessage(limit int) string {
	return fmt.Sprintf("[truncated to first %d entries]", limit)
}

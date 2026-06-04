// Package grep implements the Grep tool exposed to the model.
package grep

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ralph/internal/engine/wire"
)

// R-YNXM-CVXI: the tool's exposed name and input JSON schema match
// Claude Code's built-in Grep tool.
const Name = "Grep"

// maxOutputBytes is the default per-call output cap (R-NRBC-OMJ6).
const maxOutputBytes = 50000

// InputSchema is the JSON Schema advertised to the model for the Grep tool.
// R-W2KP-TYRG: full set of input properties matching Claude Code's Grep shape.
var InputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {
      "type": "string",
      "description": "The regular expression pattern to search for in file contents"
    },
    "path": {
      "type": "string",
      "description": "File or directory to search in. Defaults to current working directory. Must be an absolute path."
    },
    "glob": {
      "type": "string",
      "description": "Glob pattern to filter files (e.g. \"*.js\", \"**/*.tsx\")"
    },
    "type": {
      "type": "string",
      "description": "File type to search (go, py, js, ts, rust, java, c, cpp, md, sh, json, yaml, toml)"
    },
    "output_mode": {
      "type": "string",
      "enum": ["content", "files_with_matches", "count"],
      "description": "Output mode: files_with_matches (default), content, or count"
    },
    "-i": {
      "type": "boolean",
      "description": "Case insensitive search"
    },
    "-n": {
      "type": "boolean",
      "description": "Show line numbers in output (content mode only)"
    },
    "-A": {
      "type": "integer",
      "description": "Number of lines to show after each match (content mode only)"
    },
    "-B": {
      "type": "integer",
      "description": "Number of lines to show before each match (content mode only)"
    },
    "-C": {
      "type": "integer",
      "description": "Number of lines to show before and after each match (content mode only)"
    },
    "multiline": {
      "type": "boolean",
      "description": "Enable multiline mode where . matches newlines and patterns can span lines"
    },
    "head_limit": {
      "type": "integer",
      "description": "Limit output to first N entries"
    }
  },
  "required": ["pattern"]
}`)

// typeExtensions maps type filter names to their file extensions (R-T1YQ-V7BN).
var typeExtensions = map[string][]string{
	"go":   {".go"},
	"py":   {".py", ".pyw"},
	"js":   {".js", ".mjs", ".cjs"},
	"ts":   {".ts", ".tsx"},
	"rust": {".rs"},
	"java": {".java"},
	"c":    {".c", ".h"},
	"cpp":  {".cc", ".cpp", ".cxx", ".hpp", ".hxx"},
	"md":   {".md", ".markdown"},
	"sh":   {".sh", ".bash"},
	"json": {".json"},
	"yaml": {".yaml", ".yml"},
	"toml": {".toml"},
}

// deniedDirs is the fixed traversal skip set (R-K4UP-DSXC).
var deniedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
}

// Input holds the parsed parameters for a Grep invocation (R-W2KP-TYRG).
// Dash-prefixed json tags (-i, -n, -A, -B, -C) are valid and match the
// schema field names that Claude Code uses.
type Input struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path"`
	Glob            string `json:"glob"`
	TypeName        string `json:"type"`
	OutputMode      string `json:"output_mode"`
	CaseInsensitive bool   `json:"-i"`
	LineNumbers     bool   `json:"-n"`
	AfterContext    int    `json:"-A"`
	BeforeContext   int    `json:"-B"`
	AroundContext   int    `json:"-C"`
	Multiline       bool   `json:"multiline"`
	HeadLimit       int    `json:"head_limit"`
}

// Grep searches file contents for a regex pattern and returns a tool_result.
//
// R-W2KP-TYRG: schema with all optional properties.
// R-DV6B-4XLA: path defaults to cwd; absolute required when supplied.
// R-PHCN-83RU: RE2 regex; compile error → is_error.
// R-MO9F-AKEW: output_mode selects result shape.
// R-Z5JW-CG1H: glob filter restricts file walk.
// R-T1YQ-V7BN: type filter maps names to extensions.
// R-A8DI-RLSF: multiline feeds full file to matcher.
// R-NRBC-OMJ6: head_limit + 50KB cap with visible truncation notice.
// R-K4UP-DSXC: traversal skips hidden entries and denylist.
// R-MGY7-WFLI: empty match → is_error:false with "No matches found".
func Grep(toolUseID string, in Input) (wire.ToolResultBlock, error) {
	// Compile regex with flags (R-PHCN-83RU, R-A8DI-RLSF).
	patStr := in.Pattern
	if in.CaseInsensitive {
		patStr = "(?i)" + patStr
	}
	if in.Multiline {
		patStr = "(?s)(?m)" + patStr
	}
	re, err := regexp.Compile(patStr)
	if err != nil {
		return wire.NewToolResultBlock(toolUseID, true,
			fmt.Sprintf("Grep: invalid regex %q: %v", in.Pattern, err))
	}

	// Validate type filter (R-T1YQ-V7BN).
	var typeExts []string
	if in.TypeName != "" {
		exts, ok := typeExtensions[in.TypeName]
		if !ok {
			return wire.NewToolResultBlock(toolUseID, true,
				fmt.Sprintf("Grep: unknown type %q; supported types: %s",
					in.TypeName, strings.Join(supportedTypes(), ", ")))
		}
		typeExts = exts
	}

	// Resolve output mode (R-MO9F-AKEW).
	outMode := in.OutputMode
	if outMode == "" {
		outMode = "files_with_matches"
	}

	// Resolve search root (R-DV6B-4XLA).
	searchPath, isSingleFile, err := resolvePath(in.Path)
	if err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Grep: %v", err))
	}

	// Effective context lines (only meaningful in content mode).
	before := in.BeforeContext
	after := in.AfterContext
	if in.AroundContext > 0 {
		if in.AroundContext > before {
			before = in.AroundContext
		}
		if in.AroundContext > after {
			after = in.AroundContext
		}
	}

	// Collect files to search.
	var files []string
	if isSingleFile {
		files = []string{searchPath}
	} else {
		files, err = collectFiles(searchPath, in.Glob, typeExts)
		if err != nil {
			return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Grep: %v", err))
		}
	}

	// Search files and build output.
	var sb strings.Builder
	entryCount := 0
	truncated := false

	for _, f := range files {
		if truncated {
			break
		}
		var t bool
		if in.Multiline {
			t = searchMultiline(f, re, outMode, in.HeadLimit, &sb, &entryCount)
		} else {
			t = searchLines(f, re, outMode, in.LineNumbers, before, after, in.HeadLimit, &sb, &entryCount)
		}
		if t || sb.Len() >= maxOutputBytes {
			truncated = true
		}
	}

	// R-MGY7-WFLI: empty match is success.
	if entryCount == 0 {
		return wire.NewToolResultBlock(toolUseID, false, "No matches found")
	}

	body := strings.TrimRight(sb.String(), "\n")
	if truncated {
		// R-NRBC-OMJ6: truncation is visible to the model.
		body += "\n[truncated: output capped; narrow your search or use head_limit to limit results]"
	}
	return wire.NewToolResultBlock(toolUseID, false, body)
}

// resolvePath validates and resolves the path argument (R-DV6B-4XLA).
// Returns (absPath, isSingleFile, error).
func resolvePath(path string) (string, bool, error) {
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false, fmt.Errorf("getwd: %v", err)
		}
		return cwd, false, nil
	}
	if !filepath.IsAbs(path) {
		// R-0GKA-MQ8B: relative paths are rejected.
		return "", false, fmt.Errorf("path must be absolute, got relative: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, fmt.Errorf("path does not exist: %s", path)
		}
		return "", false, err
	}
	return path, info.Mode().IsRegular(), nil
}

// collectFiles walks root, skipping hidden and denied directories,
// and returns regular files that pass glob and type filters (R-K4UP-DSXC).
func collectFiles(root, globFilter string, typeExts []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries silently
		}
		if p == root {
			return nil
		}
		base := d.Name()
		// R-K4UP-DSXC: skip hidden entries (. prefix) and denylist.
		if strings.HasPrefix(base, ".") || deniedDirs[base] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if !matchGlobFilter(globFilter, rel) {
			return nil
		}
		if !matchTypeFilter(typeExts, p) {
			return nil
		}
		files = append(files, p)
		return nil
	})
	return files, err
}

// searchLines searches a file line-by-line and appends output to sb.
// Returns true if truncation occurred.
func searchLines(absPath string, re *regexp.Regexp, outMode string, lineNums bool, before, after, headLimit int, sb *strings.Builder, entryCount *int) bool {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}
	raw := string(data)
	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	switch outMode {
	case "files_with_matches":
		for _, line := range lines {
			if re.MatchString(line) {
				if headLimit > 0 && *entryCount >= headLimit {
					return true
				}
				if sb.Len() >= maxOutputBytes {
					return true
				}
				sb.WriteString(absPath)
				sb.WriteByte('\n')
				*entryCount++
				return false
			}
		}

	case "count":
		count := 0
		for _, line := range lines {
			if re.MatchString(line) {
				count++
			}
		}
		if count > 0 {
			if headLimit > 0 && *entryCount >= headLimit {
				return true
			}
			if sb.Len() >= maxOutputBytes {
				return true
			}
			fmt.Fprintf(sb, "%s:%d\n", absPath, count)
			*entryCount++
		}

	case "content":
		var matchIdxs []int
		for i, line := range lines {
			if re.MatchString(line) {
				matchIdxs = append(matchIdxs, i)
			}
		}
		if len(matchIdxs) == 0 {
			return false
		}

		type rng struct{ start, end int }
		var ranges []rng
		for _, mi := range matchIdxs {
			s := mi - before
			if s < 0 {
				s = 0
			}
			e := mi + after
			if e >= len(lines) {
				e = len(lines) - 1
			}
			if len(ranges) > 0 && s <= ranges[len(ranges)-1].end+1 {
				if e > ranges[len(ranges)-1].end {
					ranges[len(ranges)-1].end = e
				}
			} else {
				ranges = append(ranges, rng{s, e})
			}
		}

		for _, r := range ranges {
			for i := r.start; i <= r.end; i++ {
				var entry string
				if lineNums {
					entry = fmt.Sprintf("%s:%d:%s\n", absPath, i+1, lines[i])
				} else {
					entry = fmt.Sprintf("%s:%s\n", absPath, lines[i])
				}
				if headLimit > 0 && *entryCount >= headLimit {
					return true
				}
				if sb.Len()+len(entry) >= maxOutputBytes {
					return true
				}
				sb.WriteString(entry)
				*entryCount++
			}
		}
	}
	return false
}

// searchMultiline reads the full file and searches using multiline regex (R-A8DI-RLSF).
func searchMultiline(absPath string, re *regexp.Regexp, outMode string, headLimit int, sb *strings.Builder, entryCount *int) bool {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}
	content := string(data)
	matches := re.FindAllString(content, -1)
	if len(matches) == 0 {
		return false
	}

	switch outMode {
	case "files_with_matches":
		if headLimit > 0 && *entryCount >= headLimit {
			return true
		}
		if sb.Len() >= maxOutputBytes {
			return true
		}
		sb.WriteString(absPath)
		sb.WriteByte('\n')
		*entryCount++

	case "count":
		if headLimit > 0 && *entryCount >= headLimit {
			return true
		}
		if sb.Len() >= maxOutputBytes {
			return true
		}
		fmt.Fprintf(sb, "%s:%d\n", absPath, len(matches))
		*entryCount++

	case "content":
		for _, m := range matches {
			entry := fmt.Sprintf("%s:%s\n", absPath, m)
			if headLimit > 0 && *entryCount >= headLimit {
				return true
			}
			if sb.Len()+len(entry) >= maxOutputBytes {
				return true
			}
			sb.WriteString(entry)
			*entryCount++
		}
	}
	return false
}

// matchGlobFilter reports whether relPath matches the glob filter.
// R-Z5JW-CG1H: if the glob has no '/', match against the basename only;
// otherwise match the full relative path (supporting ** for any-depth descent).
func matchGlobFilter(globFilter, relPath string) bool {
	if globFilter == "" {
		return true
	}
	relSlash := filepath.ToSlash(relPath)
	if !strings.Contains(globFilter, "/") {
		base := filepath.Base(relSlash)
		matched, err := filepath.Match(globFilter, base)
		return err == nil && matched
	}
	patSegs := strings.Split(filepath.ToSlash(globFilter), "/")
	pathSegs := strings.Split(relSlash, "/")
	return matchSegments(patSegs, pathSegs)
}

// matchSegments recursively matches pattern segments, handling ** wildcards.
func matchSegments(patSegs, pathSegs []string) bool {
	for len(patSegs) > 0 {
		seg := patSegs[0]
		if seg == "**" {
			rest := patSegs[1:]
			for i := 0; i <= len(pathSegs); i++ {
				if matchSegments(rest, pathSegs[i:]) {
					return true
				}
			}
			return false
		}
		if len(pathSegs) == 0 {
			return false
		}
		matched, err := filepath.Match(seg, pathSegs[0])
		if err != nil || !matched {
			return false
		}
		patSegs = patSegs[1:]
		pathSegs = pathSegs[1:]
	}
	return len(pathSegs) == 0
}

// matchTypeFilter reports whether absPath has an extension in typeExts (R-T1YQ-V7BN).
func matchTypeFilter(typeExts []string, absPath string) bool {
	if len(typeExts) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(absPath))
	for _, e := range typeExts {
		if ext == e {
			return true
		}
	}
	return false
}

// supportedTypes returns a sorted list of supported type filter names.
func supportedTypes() []string {
	names := make([]string, 0, len(typeExtensions))
	for k := range typeExtensions {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

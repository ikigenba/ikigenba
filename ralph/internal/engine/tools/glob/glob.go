// Package glob implements the Glob tool exposed to the model.
package glob

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ralph/internal/engine/wire"
)

// R-YNXM-CVXI: the tool's exposed name and input JSON schema match
// Claude Code's built-in Glob tool.
const Name = "Glob"

// maxResults is the per-call cap on returned paths (R-EJSO-1HUK).
const maxResults = 100

// InputSchema is the JSON Schema advertised to the model for the Glob tool.
// R-Q4PX-7KJN: pattern (required) and path (optional, absolute directory).
var InputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {
      "type": "string",
      "description": "The glob pattern to match files against"
    },
    "path": {
      "type": "string",
      "description": "The directory to search in. If not specified, the current working directory will be used. Must be an absolute path."
    }
  },
  "required": ["pattern"]
}`)

type fileEntry struct {
	absPath string
	mtime   int64
}

// Glob finds files matching pattern under searchPath and returns a
// tool_result correlated to toolUseID.
//
// R-NMVL-8WCR: when searchPath is empty, Glob searches from the session cwd
// (os.Getwd). When supplied it must be absolute and an existing directory.
// R-3BHF-CKQT: pattern supports *, ?, [...] within a single segment and **
// for recursive descent across zero or more directory segments.
// R-Y8ZE-5DPM: only regular files are returned, sorted by mtime descending.
// R-LGRA-2VTW: every returned path is absolute.
// R-X7CN-9MFB: empty match set is a success with a short "No files found" body.
// R-EJSO-1HUK: result is capped at maxResults paths; truncation is visible.
func Glob(toolUseID, pattern, searchPath string) (wire.ToolResultBlock, error) {
	root, err := resolveRoot(searchPath)
	if err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Glob: %v", err))
	}

	var matches []fileEntry
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries silently
		}
		if p == root {
			return nil
		}
		// R-Y8ZE-5DPM: regular files only; directories, symlinks, etc. excluded.
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if !matchGlob(pattern, filepath.ToSlash(rel)) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		matches = append(matches, fileEntry{absPath: p, mtime: info.ModTime().UnixNano()})
		return nil
	})
	if walkErr != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Glob: %v", walkErr))
	}

	// R-X7CN-9MFB: empty match is a success.
	if len(matches) == 0 {
		return wire.NewToolResultBlock(toolUseID, false, "No files found")
	}

	// R-Y8ZE-5DPM: sort newest first.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].mtime > matches[j].mtime
	})

	// R-EJSO-1HUK: cap at maxResults.
	truncated := len(matches) > maxResults
	if truncated {
		matches = matches[:maxResults]
	}

	var sb strings.Builder
	for _, m := range matches {
		sb.WriteString(m.absPath)
		sb.WriteByte('\n')
	}
	body := strings.TrimRight(sb.String(), "\n")
	if truncated {
		body += fmt.Sprintf("\n[truncated: result capped at %d paths; narrow the pattern or split across subdirectories]", maxResults)
	}
	return wire.NewToolResultBlock(toolUseID, false, body)
}

// resolveRoot returns the absolute search root after validation.
func resolveRoot(searchPath string) (string, error) {
	if searchPath == "" {
		return os.Getwd()
	}
	if !filepath.IsAbs(searchPath) {
		return "", fmt.Errorf("path must be absolute, got relative: %s", searchPath)
	}
	info, err := os.Stat(searchPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("path does not exist: %s", searchPath)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", searchPath)
	}
	return searchPath, nil
}

// matchGlob reports whether the slash-separated relPath matches the glob
// pattern. Pattern uses *, ?, [...] for single-segment matching and **
// for recursive descent (R-3BHF-CKQT).
func matchGlob(pattern, relPath string) bool {
	patSegs := strings.Split(filepath.ToSlash(pattern), "/")
	pathSegs := strings.Split(relPath, "/")
	return matchSegments(patSegs, pathSegs)
}

// matchSegments recursively matches pattern segments against path segments,
// handling ** as zero-or-more segment wildcard.
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

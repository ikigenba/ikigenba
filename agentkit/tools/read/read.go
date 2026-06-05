// Package read implements the Read tool exposed to the model.
package read

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"agentkit/wire"
)

// R-YNXM-CVXI: the tool's exposed name and input JSON schema match
// Claude Code's built-in Read tool for the supported MVP subset
// (file_path, offset, limit). PDF/notebook/image-specific arguments
// are intentionally absent — ikigai-cli's Read returns only textual
// `cat -n` content.
const Name = "Read"

// InputSchema is the JSON Schema advertised to the model for the
// Read tool. Shape matches Claude Code's Read schema for the
// supported subset.
var InputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "The absolute path to the file to read"
    },
    "offset": {
      "type": "integer",
      "description": "The line number to start reading from. Only provide if the file is too large to read at once."
    },
    "limit": {
      "type": "integer",
      "description": "The number of lines to read. Only provide if the file is too large to read at once."
    }
  },
  "required": ["file_path"]
}`)

// R-3JJ5-FUPI: default offset (1-based) and max line count when the
// model does not supply explicit values.
const (
	defaultOffset = 1
	defaultLimit  = 2000
)

// Read attempts to read the file at absPath and returns a
// tool_result block correlated to toolUseID.
//
// offset is the 1-based line number to start reading from; limit is
// the maximum number of lines to return. A value of 0 for either
// selects the default (offset=1, limit=2000). Lines outside the
// requested window are silently omitted (R-3JJ5-FUPI).
//
// R-0GKA-MQ8B: filesystem-touching tools require absolute paths;
// relative paths are rejected with an error tool_result.
//
// R-516Q-9RC2: when absPath does not exist, Read returns an error
// tool_result.
//
// R-21VK-LY2Y: on success Read returns the file's textual content.
//
// R-2XKY-JZD0: success content is `cat -n` form — each line prefixed
// with its 1-based line number followed by a tab, then the line.
func Read(toolUseID, absPath string, offset, limit int) (wire.ToolResultBlock, error) {
	if !filepath.IsAbs(absPath) {
		msg := fmt.Sprintf("Read: path must be absolute, got relative path: %s", absPath)
		return wire.NewToolResultBlock(toolUseID, true, msg)
	}
	if offset <= 0 {
		offset = defaultOffset
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	f, err := os.Open(absPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			msg := fmt.Sprintf("Read: file does not exist: %s", absPath)
			return wire.NewToolResultBlock(toolUseID, true, msg)
		}
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Read: %v", err))
	}
	defer f.Close()

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	lineNo := 0
	emitted := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		if emitted >= limit {
			break
		}
		fmt.Fprintf(&b, "%d\t%s\n", lineNo, scanner.Text())
		emitted++
	}
	if err := scanner.Err(); err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Read: %v", err))
	}
	return wire.NewToolResultBlock(toolUseID, false, b.String())
}

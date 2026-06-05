// Package edit implements the Edit tool exposed to the model.
package edit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agentkit/wire"
)

// R-YNXM-CVXI: the tool's exposed name and input JSON schema match
// Claude Code's built-in Edit tool.
const Name = "Edit"

// InputSchema is the JSON Schema advertised to the model for the Edit tool.
// R-RTG4-Q9VK: four properties — file_path, old_string, new_string (all
// required), and replace_all (optional boolean, default false).
var InputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "The absolute path to the file to modify"
    },
    "old_string": {
      "type": "string",
      "description": "The literal text to find and replace"
    },
    "new_string": {
      "type": "string",
      "description": "The literal replacement text"
    },
    "replace_all": {
      "type": "boolean",
      "description": "Replace all occurrences instead of requiring exactly one (default false)"
    }
  },
  "required": ["file_path", "old_string", "new_string"]
}`)

// Edit performs an exact-string replacement within the file at filePath and
// returns a tool_result correlated to toolUseID.
//
// R-RTG4-Q9VK: exact-string replacement, no regex/fuzzy mode.
// R-8XBN-MP2C: filePath must be absolute and name a regular file.
// R-LFJD-7HRO: replacement is byte-exact; no whitespace normalization,
// line-ending fuzzing, or encoding conversion.
// R-3CWS-EAYI: when replaceAll is false, oldString must occur exactly once.
// R-VK0M-5BTL: when replaceAll is true, every occurrence is replaced; zero
// occurrences is still an error.
// R-NJZH-1XPE: newString must differ from oldString.
// R-O6QT-FAUR: oldString must be non-empty.
// R-DM8K-9SCG: the edit is atomic via temp-file-plus-rename; the temp file
// is removed on any error path before returning.
// R-HEFY-4WJN: the target file's mode is preserved across the edit.
// R-MZWT-K8VR: no "Read before Edit" precondition at the tool layer.
// R-PQXB-J5LC: success message includes the absolute path and replacement count.
func Edit(toolUseID, filePath, oldString, newString string, replaceAll bool) (wire.ToolResultBlock, error) {
	// R-O6QT-FAUR: reject empty old_string.
	if oldString == "" {
		return wire.NewToolResultBlock(toolUseID, true, "Edit: old_string must be non-empty")
	}

	// R-NJZH-1XPE: reject no-op edits.
	if oldString == newString {
		return wire.NewToolResultBlock(toolUseID, true, "Edit: old_string and new_string are identical; no change would be made")
	}

	// R-8XBN-MP2C: absolute path required.
	if !filepath.IsAbs(filePath) {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: path must be absolute, got relative path: %s", filePath))
	}

	// R-8XBN-MP2C: must exist as a regular file.
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: file does not exist: %s", filePath))
		}
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: %v", err))
	}
	if !info.Mode().IsRegular() {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: not a regular file: %s", filePath))
	}

	// R-LFJD-7HRO: read raw bytes; no normalization.
	data, err := os.ReadFile(filePath)
	if err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: %v", err))
	}
	content := string(data)

	count := strings.Count(content, oldString)

	if count == 0 {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: old_string not found in %s", filePath))
	}

	// R-3CWS-EAYI: when replace_all is false, require exactly one occurrence.
	if !replaceAll && count > 1 {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf(
			"Edit: old_string occurs %d times in %s; either enlarge old_string with more surrounding context to make the match unique, or set replace_all=true",
			count, filePath,
		))
	}

	// R-VK0M-5BTL / R-3CWS-EAYI: perform the replacement(s).
	var updated string
	var replacements int
	if replaceAll {
		updated = strings.ReplaceAll(content, oldString, newString)
		replacements = count
	} else {
		updated = strings.Replace(content, oldString, newString, 1)
		replacements = 1
	}

	// R-DM8K-9SCG: write atomically via temp file in the same directory.
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, ".iki-tmp-*")
	if err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: %v", err))
	}
	tmpPath := tmp.Name()

	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.WriteString(updated); err != nil {
		tmp.Close()
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: %v", err))
	}
	if err := tmp.Close(); err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: %v", err))
	}

	// R-HEFY-4WJN: preserve the original file's mode bits.
	if err := os.Chmod(tmpPath, info.Mode().Perm()); err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: %v", err))
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Edit: %v", err))
	}
	cleanup = false

	// R-PQXB-J5LC: success message with path and replacement count.
	noun := "replacement"
	if replacements != 1 {
		noun = "replacements"
	}
	msg := fmt.Sprintf("File edited successfully at %s (%d %s)", filePath, replacements, noun)
	return wire.NewToolResultBlock(toolUseID, false, msg)
}

// Package write implements the Write tool exposed to the model.
package write

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ralph/internal/engine/wire"
)

// R-YNXM-CVXI: the tool's exposed name and input JSON schema match
// Claude Code's built-in Write tool — a model that has used Claude Code's
// Write must be able to call ikigai-cli's version with the same arguments.
const Name = "Write"

// InputSchema is the JSON Schema advertised to the model for the Write tool.
// R-JR8E-92QM: two required properties — file_path and content.
var InputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "file_path": {
      "type": "string",
      "description": "The absolute path to the file to write (must be absolute, not relative)"
    },
    "content": {
      "type": "string",
      "description": "The content to write to the file"
    }
  },
  "required": ["file_path", "content"]
}`)

// Write writes content to filePath and returns a tool_result correlated to
// toolUseID.
//
// R-0GKA-MQ8B: absolute paths only; relative paths produce an error
// tool_result.
// R-K3XF-PB1Y: if the parent directory does not exist, Write returns an
// error tool_result without creating any directories.
// R-CMWG-58TR: the write is atomic — content lands via a temp file in the
// same directory that is renamed over the target; a failure mid-write does
// not leave a partially-written or truncated target. The temp file is
// removed on any error path before returning.
// R-2DHN-A6VK: the materialized file has mode 0644 regardless of whether
// it is new or replaces an existing file.
// R-W0PK-4XBN: on success the file's bytes are exactly the bytes supplied as
// content — no trailing-newline addition, line-ending normalization, or
// encoding conversion.
// R-7VLS-NHCD: if a file already exists at filePath its contents are
// discarded; there is no backup or confirmation.
// R-PE9Q-1JFL: on success Write returns a human-readable message that
// distinguishes file creation from file replacement and includes the
// absolute path.
func Write(toolUseID, filePath, content string) (wire.ToolResultBlock, error) {
	if !filepath.IsAbs(filePath) {
		msg := fmt.Sprintf("Write: path must be absolute, got relative path: %s", filePath)
		return wire.NewToolResultBlock(toolUseID, true, msg)
	}

	dir := filepath.Dir(filePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		msg := fmt.Sprintf("Write: parent directory does not exist: %s", dir)
		return wire.NewToolResultBlock(toolUseID, true, msg)
	}

	// Determine whether the target exists now, before writing, so the success
	// message can distinguish create from replace (R-PE9Q-1JFL).
	_, statErr := os.Stat(filePath)
	existed := statErr == nil

	// R-CMWG-58TR: write to a temp file in the same directory so the final
	// rename is atomic on the same filesystem.
	tmp, err := os.CreateTemp(dir, ".iki-tmp-*")
	if err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Write: %v", err))
	}
	tmpPath := tmp.Name()

	// Ensure the temp file is cleaned up on any failure path.
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	// R-W0PK-4XBN: write exactly the bytes the model supplied.
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Write: %v", err))
	}
	if err := tmp.Close(); err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Write: %v", err))
	}

	// R-2DHN-A6VK: set mode 0644 before renaming so the materialized file
	// has the correct permissions regardless of the process umask.
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Write: %v", err))
	}

	// Atomic rename over the target (R-CMWG-58TR).
	if err := os.Rename(tmpPath, filePath); err != nil {
		return wire.NewToolResultBlock(toolUseID, true, fmt.Sprintf("Write: %v", err))
	}
	cleanup = false // rename succeeded; nothing to clean up

	var msg string
	if existed {
		msg = fmt.Sprintf("File updated successfully at %s", filePath)
	} else {
		msg = fmt.Sprintf("File created successfully at %s", filePath)
	}
	return wire.NewToolResultBlock(toolUseID, false, msg)
}

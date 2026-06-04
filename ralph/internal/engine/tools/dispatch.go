package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"ralph/internal/engine/tools/bash"
	"ralph/internal/engine/tools/edit"
	"ralph/internal/engine/tools/glob"
	"ralph/internal/engine/tools/grep"
	"ralph/internal/engine/tools/read"
	"ralph/internal/engine/tools/write"
	"ralph/internal/engine/wire"
)

// Dispatch routes a tool_use block to its implementation and returns the
// tool_result and an optional sidecar. Unknown tool names produce an
// is_error result rather than a Go error, so the caller always receives a
// correlatable answer. R-8293-8LCI.
//
// The sidecar is tool-specific (R-DPI6-73NQ): Bash returns a BashSidecar;
// tools that have no Claude Code sidecar return nil.
func Dispatch(ctx context.Context, sandboxRoot string, block wire.ToolUseBlock) (wire.ToolResultBlock, any, error) {
	switch block.Name {
	case bash.Name:
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(block.Input, &in); err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, fmt.Sprintf("Bash: invalid input: %v", err))
			return b, nil, e
		}
		result, err := bash.Run(ctx, sandboxRoot, block.ID, in.Command)
		sidecar := bash.BashSidecar{
			Stdout:      result.Stdout,
			Stderr:      result.Stderr,
			Interrupted: result.Interrupted,
		}
		return result.Block, sidecar, err
	case read.Name:
		var in struct {
			FilePath string `json:"file_path"`
			Offset   int    `json:"offset"`
			Limit    int    `json:"limit"`
		}
		if err := json.Unmarshal(block.Input, &in); err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, fmt.Sprintf("Read: invalid input: %v", err))
			return b, nil, e
		}
		path, err := confinePath(sandboxRoot, in.FilePath)
		if err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, "Read: "+err.Error())
			return b, nil, e
		}
		b, e := read.Read(block.ID, path, in.Offset, in.Limit)
		return b, nil, e
	case write.Name:
		var in struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(block.Input, &in); err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, fmt.Sprintf("Write: invalid input: %v", err))
			return b, nil, e
		}
		path, err := confinePath(sandboxRoot, in.FilePath)
		if err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, "Write: "+err.Error())
			return b, nil, e
		}
		b, e := write.Write(block.ID, path, in.Content)
		return b, nil, e
	case glob.Name:
		var in struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if err := json.Unmarshal(block.Input, &in); err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, fmt.Sprintf("Glob: invalid input: %v", err))
			return b, nil, e
		}
		searchPath, err := effectiveSearchPath(sandboxRoot, in.Path)
		if err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, "Glob: "+err.Error())
			return b, nil, e
		}
		b, e := glob.Glob(block.ID, in.Pattern, searchPath)
		return b, nil, e
	case grep.Name:
		var in grep.Input
		if err := json.Unmarshal(block.Input, &in); err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, fmt.Sprintf("Grep: invalid input: %v", err))
			return b, nil, e
		}
		searchPath, err := effectiveSearchPath(sandboxRoot, in.Path)
		if err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, "Grep: "+err.Error())
			return b, nil, e
		}
		in.Path = searchPath
		b, e := grep.Grep(block.ID, in)
		return b, nil, e
	case edit.Name:
		var in struct {
			FilePath   string `json:"file_path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(block.Input, &in); err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, fmt.Sprintf("Edit: invalid input: %v", err))
			return b, nil, e
		}
		path, err := confinePath(sandboxRoot, in.FilePath)
		if err != nil {
			b, e := wire.NewToolResultBlock(block.ID, true, "Edit: "+err.Error())
			return b, nil, e
		}
		b, e := edit.Edit(block.ID, path, in.OldString, in.NewString, in.ReplaceAll)
		return b, nil, e
	default:
		b, e := wire.NewToolResultBlock(block.ID, true, fmt.Sprintf("unknown tool: %q", block.Name))
		return b, nil, e
	}
}

package tools

import (
	"encoding/json"

	"github.com/ikigenba/ikigenba/appkit/mcp"
)

// ListArgs is the public MCP argument type.
type ListArgs struct {
}

// ShowArgs is the public MCP argument type.
type ShowArgs struct {
	Name string `json:"name" mcp:"required" description:"The name value."`
}

// CreateArgs is the public MCP argument type.
type CreateArgs struct {
	Name   string          `json:"name" mcp:"required" description:"The name value."`
	Model  string          `json:"model" mcp:"required" description:"The model value."`
	Prompt string          `json:"prompt" mcp:"required" description:"The prompt value."`
	System *string         `json:"system" description:"The system value."`
	Tools  *[]string       `json:"tools" description:"The tools value."`
	Schema json.RawMessage `json:"schema" description:"The schema value."`
}

// UpdateArgs is the public MCP argument type.
type UpdateArgs struct {
	Name   string                        `json:"name" mcp:"required" description:"The name value."`
	Model  *string                       `json:"model" description:"The model value."`
	Prompt *string                       `json:"prompt" description:"The prompt value."`
	System *string                       `json:"system" description:"The system value."`
	Tools  *[]string                     `json:"tools" description:"The tools value."`
	Schema mcp.Nullable[json.RawMessage] `json:"schema" description:"The schema value."`
}

// DeleteArgs is the public MCP argument type.
type DeleteArgs struct {
	Name string `json:"name" mcp:"required" description:"The name value."`
}

// SubscribeArgs is the public MCP argument type.
type SubscribeArgs struct {
	Name  string `json:"name" mcp:"required" description:"The name value."`
	Event string `json:"event" mcp:"required" description:"The event value."`
}

// UnsubscribeArgs is the public MCP argument type.
type UnsubscribeArgs struct {
	Name  string `json:"name" mcp:"required" description:"The name value."`
	Event string `json:"event" mcp:"required" description:"The event value."`
}

// RunArgs is the public MCP argument type.
type RunArgs struct {
	Name  string          `json:"name" mcp:"required" description:"The name value."`
	Input json.RawMessage `json:"input" description:"The input value."`
}

// RunsArgs is the public MCP argument type.
type RunsArgs struct {
	Name string `json:"name" mcp:"required" description:"The name value."`
}

// ResultArgs is the public MCP argument type.
type ResultArgs struct {
	Run string `json:"run" mcp:"required" description:"The run value."`
}

// CancelArgs is the public MCP argument type.
type CancelArgs struct {
	Run string `json:"run" mcp:"required" description:"The run value."`
}

// LastRun is the public MCP result type.
type LastRun struct {
	ID       string `json:"id" mcp:"required"`
	Status   string `json:"status" mcp:"required"`
	ExitCode *int   `json:"exit_code"`
	Started  string `json:"started" mcp:"required"`
}

// Subscription is the public MCP result type.
type Subscription struct {
	Event   string `json:"event" mcp:"required"`
	Created string `json:"created" mcp:"required"`
}

// Prompt is the public MCP result type.
type Prompt struct {
	ID            string          `json:"id" mcp:"required"`
	Name          string          `json:"name" mcp:"required"`
	Model         string          `json:"model" mcp:"required"`
	Prompt        string          `json:"prompt" mcp:"required"`
	System        string          `json:"system" mcp:"required"`
	Tools         []string        `json:"tools" mcp:"required"`
	Schema        json.RawMessage `json:"schema"`
	Created       string          `json:"created" mcp:"required"`
	Subscriptions []Subscription  `json:"subscriptions" mcp:"required"`
	LastRun       *LastRun        `json:"last_run"`
}

// ListedPrompt is the public MCP result type.
type ListedPrompt struct {
	ID            string   `json:"id" mcp:"required"`
	Name          string   `json:"name" mcp:"required"`
	Model         string   `json:"model" mcp:"required"`
	Tools         []string `json:"tools" mcp:"required"`
	Subscriptions int      `json:"subscriptions" mcp:"required"`
	LastRun       *LastRun `json:"last_run"`
}

// PromptList is the public MCP result type.
type PromptList struct {
	Prompts []ListedPrompt `json:"prompts" mcp:"required"`
}

// Deleted is the public MCP result type.
type Deleted struct {
	Deleted bool   `json:"deleted" mcp:"required"`
	ID      string `json:"id" mcp:"required"`
}

// Started is the public MCP result type.
type Started struct {
	ID     string  `json:"id" mcp:"required"`
	Status string  `json:"status" mcp:"required"`
	Reason *string `json:"reason"`
}

// RunEntry is the public MCP result type.
type RunEntry struct {
	ID              string  `json:"id" mcp:"required"`
	Model           string  `json:"model" mcp:"required"`
	Trigger         string  `json:"trigger" mcp:"required"`
	Event           *string `json:"event"`
	Status          string  `json:"status" mcp:"required"`
	ExitCode        *int    `json:"exit_code"`
	Started         string  `json:"started" mcp:"required"`
	Finished        *string `json:"finished"`
	Truncated       bool    `json:"truncated" mcp:"required"`
	Reason          *string `json:"reason"`
	Calls           *int64  `json:"calls"`
	ToolCalls       *int64  `json:"tool_calls"`
	InputTokens     *int64  `json:"input_tokens"`
	CachedTokens    *int64  `json:"cached_tokens"`
	OutputTokens    *int64  `json:"output_tokens"`
	ReasoningTokens *int64  `json:"reasoning_tokens"`
	CostNanos       *int64  `json:"cost_nanos"`
}

// RunList is the public MCP result type.
type RunList struct {
	Runs []RunEntry `json:"runs" mcp:"required"`
}

// File is the public MCP result type.
type File struct {
	Path string `json:"path" mcp:"required"`
	Size int64  `json:"size" mcp:"required"`
}

// RunResult is the public MCP result type.
type RunResult struct {
	ID              string  `json:"id" mcp:"required"`
	Prompt          string  `json:"prompt" mcp:"required"`
	Model           string  `json:"model" mcp:"required"`
	User            string  `json:"user" mcp:"required"`
	RequestID       string  `json:"request_id" mcp:"required"`
	Trigger         string  `json:"trigger" mcp:"required"`
	Event           *string `json:"event"`
	Status          string  `json:"status" mcp:"required"`
	ExitCode        *int    `json:"exit_code"`
	Started         string  `json:"started" mcp:"required"`
	Finished        *string `json:"finished"`
	StdoutBytes     int64   `json:"stdout_bytes" mcp:"required"`
	StderrBytes     int64   `json:"stderr_bytes" mcp:"required"`
	Truncated       bool    `json:"truncated" mcp:"required"`
	Reason          *string `json:"reason"`
	Calls           *int64  `json:"calls"`
	ToolCalls       *int64  `json:"tool_calls"`
	InputTokens     *int64  `json:"input_tokens"`
	CachedTokens    *int64  `json:"cached_tokens"`
	OutputTokens    *int64  `json:"output_tokens"`
	ReasoningTokens *int64  `json:"reasoning_tokens"`
	CostNanos       *int64  `json:"cost_nanos"`
	Stdout          *string `json:"stdout"`
	Stderr          *string `json:"stderr"`
	TranscriptBytes *int64  `json:"transcript_bytes"`
	Files           *[]File `json:"files"`
	FilesGone       *bool   `json:"files_gone"`
}

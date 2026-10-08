// Package tools offers scripts' catalog and runs through MCP.
package tools

import "encoding/json"

// ListArgs is an MCP argument.
type ListArgs struct {
}

// ShowArgs is an MCP argument.
type ShowArgs struct {
	Name string `json:"name" mcp:"required" description:"The script's name."`
}

// CreateArgs is an MCP argument.
type CreateArgs struct {
	Name string  `json:"name" mcp:"required" description:"The new script's name: 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, not about, mcp, events, or declarations, and not already a script's name in the space."`
	Repo string  `json:"repo" mcp:"required" description:"The id of one of your repositories in repos (rep_ and 16 hexadecimal digits)."`
	Ref  *string `json:"ref" description:"The branch, tag, or commit a run uses; main unless given."`
}

// UpdateArgs is an MCP argument.
type UpdateArgs struct {
	Name string `json:"name" mcp:"required" description:"The script's name."`
	Ref  string `json:"ref" mcp:"required" description:"The branch, tag, or commit the script runs from now on."`
}

// DeleteArgs is an MCP argument.
type DeleteArgs struct {
	Name string `json:"name" mcp:"required" description:"The script's name."`
}

// SubscribeArgs is an MCP argument.
type SubscribeArgs struct {
	Name  string `json:"name" mcp:"required" description:"The script's name."`
	Event string `json:"event" mcp:"required" description:"The event pattern, such as repo.pushed or cron.*.fired."`
}

// UnsubscribeArgs is an MCP argument.
type UnsubscribeArgs struct {
	Name  string `json:"name" mcp:"required" description:"The script's name."`
	Event string `json:"event" mcp:"required" description:"The event pattern exactly as the script is subscribed to it."`
}

// RunArgs is an MCP argument.
type RunArgs struct {
	Name  string          `json:"name" mcp:"required" description:"The script's name."`
	Ref   *string         `json:"ref" description:"The branch, tag, or commit to run, for this run only; the script's own ref unless given."`
	Input json.RawMessage `json:"input" description:"A JSON object the run's script reads as its input; {} unless given."`
}

// RunsArgs is an MCP argument.
type RunsArgs struct {
	Name string `json:"name" mcp:"required" description:"The script's name."`
}

// ResultArgs is an MCP argument.
type ResultArgs struct {
	Run string `json:"run" mcp:"required" description:"The run's id (run_ and 16 hexadecimal digits)."`
}

// CancelArgs is an MCP argument.
type CancelArgs struct {
	Run string `json:"run" mcp:"required" description:"The run's id (run_ and 16 hexadecimal digits)."`
}

// LastRun is an MCP result.
type LastRun struct {
	ID       string `json:"id" mcp:"required"`
	Status   string `json:"status" mcp:"required"`
	ExitCode *int   `json:"exit_code"`
	Started  string `json:"started" mcp:"required"`
}

// Subscription is an MCP result.
type Subscription struct {
	Event   string `json:"event" mcp:"required"`
	Created string `json:"created" mcp:"required"`
}

// Script is an MCP result.
type Script struct {
	ID            string         `json:"id" mcp:"required"`
	Name          string         `json:"name" mcp:"required"`
	Repo          string         `json:"repo" mcp:"required"`
	Ref           string         `json:"ref" mcp:"required"`
	Created       string         `json:"created" mcp:"required"`
	Subscriptions []Subscription `json:"subscriptions" mcp:"required"`
	LastRun       *LastRun       `json:"last_run"`
}

// ListedScript is an MCP result.
type ListedScript struct {
	ID            string   `json:"id" mcp:"required"`
	Name          string   `json:"name" mcp:"required"`
	Repo          string   `json:"repo" mcp:"required"`
	Ref           string   `json:"ref" mcp:"required"`
	Subscriptions int      `json:"subscriptions" mcp:"required"`
	LastRun       *LastRun `json:"last_run"`
}

// ScriptList is an MCP result.
type ScriptList struct {
	Scripts []ListedScript `json:"scripts" mcp:"required"`
}

// Deleted is an MCP result.
type Deleted struct {
	Deleted bool   `json:"deleted" mcp:"required"`
	ID      string `json:"id" mcp:"required"`
}

// Started is an MCP result.
type Started struct {
	ID     string  `json:"id" mcp:"required"`
	Status string  `json:"status" mcp:"required"`
	SHA    *string `json:"sha"`
	Reason *string `json:"reason"`
}

// RunEntry is an MCP result.
type RunEntry struct {
	ID        string  `json:"id" mcp:"required"`
	SHA       *string `json:"sha"`
	Ref       string  `json:"ref" mcp:"required"`
	Trigger   string  `json:"trigger" mcp:"required"`
	Event     *string `json:"event"`
	Status    string  `json:"status" mcp:"required"`
	ExitCode  *int    `json:"exit_code"`
	Started   string  `json:"started" mcp:"required"`
	Finished  *string `json:"finished"`
	Truncated bool    `json:"truncated" mcp:"required"`
	Reason    *string `json:"reason"`
}

// RunList is an MCP result.
type RunList struct {
	Runs []RunEntry `json:"runs" mcp:"required"`
}

// File is an MCP result.
type File struct {
	Path string `json:"path" mcp:"required"`
	Size int64  `json:"size" mcp:"required"`
}

// RunResult is an MCP result.
type RunResult struct {
	ID          string  `json:"id" mcp:"required"`
	Script      string  `json:"script" mcp:"required"`
	SHA         *string `json:"sha"`
	Ref         string  `json:"ref" mcp:"required"`
	User        string  `json:"user" mcp:"required"`
	RequestID   string  `json:"request_id" mcp:"required"`
	Trigger     string  `json:"trigger" mcp:"required"`
	Event       *string `json:"event"`
	Status      string  `json:"status" mcp:"required"`
	ExitCode    *int    `json:"exit_code"`
	Started     string  `json:"started" mcp:"required"`
	Finished    *string `json:"finished"`
	StdoutBytes int64   `json:"stdout_bytes" mcp:"required"`
	StderrBytes int64   `json:"stderr_bytes" mcp:"required"`
	Truncated   bool    `json:"truncated" mcp:"required"`
	Reason      *string `json:"reason"`
	Stdout      *string `json:"stdout"`
	Stderr      *string `json:"stderr"`
	Files       *[]File `json:"files"`
	FilesGone   *bool   `json:"files_gone"`
}

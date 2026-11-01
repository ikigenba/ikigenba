// Package session defines the content-free session data shared by readers and clients.
package session

import "time"

// Tool identifies the application that recorded a session.
type Tool string

const (
	// ToolClaude identifies Claude Code.
	ToolClaude Tool = "claude"
	// ToolCodex identifies Codex CLI.
	ToolCodex Tool = "codex"
	// ToolGrok identifies Grok CLI.
	ToolGrok Tool = "grok"
)

// Tools returns the supported tools in their canonical order.
func Tools() []Tool {
	return []Tool{ToolClaude, ToolCodex, ToolGrok}
}

// Whose identifies who took a turn.
type Whose string

const (
	// WhosePerson identifies a person's turn.
	WhosePerson Whose = "person"
	// WhoseAgent identifies a model's turn.
	WhoseAgent Whose = "agent"
	// WhoseTool identifies a tool result.
	WhoseTool Whose = "tool"
)

// Ref identifies a session within a tool.
type Ref struct {
	Tool Tool
	ID   string
}

// Command is the first slash command recorded for a session.
type Command struct {
	Name     string
	Argument string
}

// Tokens holds the usage counts of a turn.
type Tokens struct {
	Input     int64
	Cached    int64
	Output    int64
	Reasoning int64
}

// Turn is one content-free step of a conversation.
type Turn struct {
	Moment time.Time
	Whose  Whose
	Model  string
	Tokens Tokens
}

// Session is one conversation recorded by a tool.
type Session struct {
	Ref       Ref
	Parent    *Ref
	Directory string
	Headless  bool
	Command   *Command
	Turns     []Turn
}

// Found holds a reader's sessions and count of unreadable session sources.
type Found struct {
	Sessions []Session
	Skipped  int
}

// Mention records a dispatch job path seen in a Claude session.
type Mention struct {
	Path   string
	By     Ref
	Moment time.Time
}

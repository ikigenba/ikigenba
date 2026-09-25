// Package chat defines transcript entries, usage, and their printed form.
package chat

import (
	"errors"
	"io/fs"
	"time"
)

// Kind identifies one of the seven kinds of chat entry.
type Kind string

// The entry kinds used by chat transcripts.
const (
	KindUser        Kind = "user"
	KindAssistant   Kind = "assistant"
	KindReasoning   Kind = "reasoning"
	KindAgent       Kind = "agent"
	KindTool        Kind = "tool"
	KindResultOK    Kind = "result ok"
	KindResultError Kind = "result error"
)

// Entry is one item said or done in a chat transcript.
type Entry struct {
	Time    time.Time
	HasTime bool
	Kind    Kind
	Tool    string
	Text    string
}

// Usage counts an agent's tokens and model calls.
type Usage struct {
	In         int64
	CacheWrite int64
	CacheRead  int64
	Out        int64
	Reasoning  int64
	Calls      int64
}

// Recorded identifies the usage counts a harness provides.
type Recorded struct {
	In         bool
	CacheWrite bool
	CacheRead  bool
	Out        bool
	Reasoning  bool
	Calls      bool
}

// Decoder turns one transcript record into entries and usage.
type Decoder interface {
	Decode(fsys fs.FS, record []byte) ([]Entry, Usage)
}

// ErrAgentNotFound reports that a requested agent was absent from a session.
var ErrAgentNotFound error

func init() {
	ErrAgentNotFound = errors.New("agent not found")
}

package agentkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

// RecordType is the closed set of event-log record kinds. There is deliberately
// no "warning" kind: fail-loud (D4) removed the concept — an unresolved cost is
// Cost.Known=false, an unexpressible request fails at Send.
type RecordType string

// RecordType values enumerate the event-log record kinds.
const (
	RecordTurnStart  RecordType = "turn_start"
	RecordMessage    RecordType = "message"
	RecordToolUse    RecordType = "tool_use"
	RecordToolResult RecordType = "tool_result"
	RecordUsage      RecordType = "usage"
	RecordError      RecordType = "error"
	RecordRetry      RecordType = "retry"
	RecordTurnEnd    RecordType = "turn_end"
	RecordSummary    RecordType = "summary"
)

// LogRecord is one line of the log. Type selects which payload pointer is set;
// the rest are nil and omitted. Time is the injected clock's reading; Seq is
// monotonic within a turn. The payloads reuse the canonical types verbatim — no
// log-only shadow structs — so the log and the live stream never drift.
type LogRecord struct {
	Type RecordType `json:"type"`
	Time time.Time  `json:"time"`
	Seq  int        `json:"seq"`

	Identity   *Identity   `json:"identity,omitempty"`    // turn_start
	Message    *Message    `json:"message,omitempty"`     // message (one completed Message, D2/D13)
	ToolUse    *ToolUse    `json:"tool_use,omitempty"`    // tool_use
	ToolResult *ToolResult `json:"tool_result,omitempty"` // tool_result
	Usage      *Usage      `json:"usage,omitempty"`       // usage, summary
	Cost       *Cost       `json:"cost,omitempty"`        // usage, summary
	Err        *Error      `json:"error,omitempty"`       // error
	Retry      *RetryInfo  `json:"retry,omitempty"`       // retry
}

// RetryInfo records one backoff wait emitted by the retry driver (D14).
type RetryInfo struct {
	Attempt int           `json:"attempt"`
	Delay   time.Duration `json:"delay"`
	Reason  string        `json:"reason"` // the retried error's Error() text
}

type logRecordJSON struct {
	Type       RecordType      `json:"type"`
	Time       time.Time       `json:"time"`
	Seq        int             `json:"seq"`
	Identity   *Identity       `json:"identity,omitempty"`
	Message    json.RawMessage `json:"message,omitempty"`
	ToolUse    *ToolUse        `json:"tool_use,omitempty"`
	ToolResult *ToolResult     `json:"tool_result,omitempty"`
	Usage      *Usage          `json:"usage,omitempty"`
	Cost       *Cost           `json:"cost,omitempty"`
	Err        *Error          `json:"error,omitempty"`
	Retry      *RetryInfo      `json:"retry,omitempty"`
}

// MarshalJSON uses History's canonical tagged Block representation for the
// single Message payload while retaining LogRecord's one-object shape.
func (r LogRecord) MarshalJSON() ([]byte, error) {
	encoded := logRecordJSON{
		Type: r.Type, Time: r.Time, Seq: r.Seq, Identity: r.Identity,
		ToolUse: r.ToolUse, ToolResult: r.ToolResult, Usage: r.Usage,
		Cost: r.Cost, Err: r.Err, Retry: r.Retry,
	}
	if r.Message != nil {
		history, err := json.Marshal(History{*r.Message})
		if err != nil {
			return nil, err
		}
		var messages []json.RawMessage
		if err := json.Unmarshal(history, &messages); err != nil {
			return nil, err
		}
		encoded.Message = messages[0]
	}
	return json.Marshal(encoded)
}

// UnmarshalJSON reconstructs Message's sealed Block values from their canonical
// discriminators so independently decoded log lines retain canonical payloads.
func (r *LogRecord) UnmarshalJSON(data []byte) error {
	var encoded logRecordJSON
	if err := json.Unmarshal(data, &encoded); err != nil {
		return err
	}
	*r = LogRecord{
		Type: encoded.Type, Time: encoded.Time, Seq: encoded.Seq,
		Identity: encoded.Identity, ToolUse: encoded.ToolUse,
		ToolResult: encoded.ToolResult, Usage: encoded.Usage,
		Cost: encoded.Cost, Err: encoded.Err, Retry: encoded.Retry,
	}
	if len(encoded.Message) != 0 {
		var history History
		wrapped := append([]byte{'['}, encoded.Message...)
		wrapped = append(wrapped, ']')
		if err := json.Unmarshal(wrapped, &history); err != nil {
			return err
		}
		if len(history) != 1 {
			return io.ErrUnexpectedEOF
		}
		r.Message = &history[0]
	}
	normalizeLogPayloads(r)
	return nil
}

func normalizeLogPayloads(record *LogRecord) {
	if record.ToolUse != nil {
		record.ToolUse.Input = nilJSON(record.ToolUse.Input)
		record.ToolUse.Provider = nilJSON(record.ToolUse.Provider)
	}
	if record.ToolResult != nil {
		record.ToolResult.Provider = nilJSON(record.ToolResult.Provider)
	}
	if record.Message == nil {
		return
	}
	for index, block := range record.Message.Blocks {
		switch value := block.(type) {
		case Text:
			value.Provider = nilJSON(value.Provider)
			record.Message.Blocks[index] = value
		case Reasoning:
			value.Provider = nilJSON(value.Provider)
			record.Message.Blocks[index] = value
		case ToolUse:
			value.Input = nilJSON(value.Input)
			value.Provider = nilJSON(value.Provider)
			record.Message.Blocks[index] = value
		case ToolResult:
			value.Provider = nilJSON(value.Provider)
			record.Message.Blocks[index] = value
		}
	}
}

func nilJSON(value json.RawMessage) json.RawMessage {
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil
	}
	return value
}

// Log is the opaque destination for a conversation's durable event trace.
type Log struct {
	mu        sync.Mutex
	w         io.Writer
	now       func() time.Time
	seq       int
	closed    bool
	writeErr  error
	total     Usage
	totalCost Cost
}

// NewLog builds a log over w, timestamping with now. A nil w yields a nil-
// behaving log. now is injected for determinism (D3).
func NewLog(w io.Writer, now func() time.Time) *Log {
	return &Log{w: w, now: now, totalCost: Cost{Known: true}}
}

// Close emits exactly one cumulative summary record — total Usage and total
// Cost across the conversation's turns — and marks the log closed. It is
// idempotent: a second Close writes nothing and returns nil. After Close, any
// Send on the owning Conversation returns ErrClosed (D4).
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	usage, cost := l.total, l.totalCost
	l.write(LogRecord{Type: RecordSummary, Usage: &usage, Cost: &cost})
	return l.writeErr
}

func (l *Log) isClosed() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func (l *Log) start(identity Identity) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq = 0
	l.write(LogRecord{Type: RecordTurnStart, Identity: &identity})
}

func (l *Log) record(record eventRecord) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := LogRecord{}
	switch record.kind {
	case eventRecordMessage:
		value := record.value.(Message)
		entry.Type, entry.Message = RecordMessage, &value
	case eventRecordToolUse:
		value := record.value.(ToolUse)
		entry.Type, entry.ToolUse = RecordToolUse, &value
	case eventRecordToolResult:
		value := record.value.(ToolResult)
		entry.Type, entry.ToolResult = RecordToolResult, &value
	default:
		panic("agentkit: invalid event record kind")
	}
	l.write(entry)
}

func (l *Log) recordError(err error) {
	if l == nil || err == nil {
		return
	}
	var canonical *Error
	if !errors.As(err, &canonical) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.write(LogRecord{Type: RecordError, Err: canonical})
}

func (l *Log) finish(usage Usage, cost Cost) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total = addUsage(l.total, usage)
	l.totalCost = aggregateCosts(l.totalCost, cost)
	l.write(LogRecord{Type: RecordUsage, Usage: &usage, Cost: &cost})
	l.write(LogRecord{Type: RecordTurnEnd})
}

func (l *Log) write(record LogRecord) {
	if l.w == nil || l.writeErr != nil {
		return
	}
	if l.now != nil {
		record.Time = l.now()
	}
	record.Seq = l.seq
	l.seq++
	line, err := json.Marshal(record)
	if err == nil {
		line = append(line, '\n')
		var written int
		written, err = l.w.Write(line)
		if err == nil && written != len(line) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		l.writeErr = err
	}
}

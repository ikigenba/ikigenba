package wire

import (
	"errors"
	"fmt"
	"io"
	"sort"
)

// R-0I14-J4N2: every iteration ends with exactly one `result` event
// emitted on stdout. No assistant or user events follow the result
// event within the same iteration.
//
// R-5ZKU-HYRK: every `tool_use` block emitted in an assistant event
// MUST be answered by exactly one `tool_result` block in a subsequent
// user event, with matching `tool_use_id`, before the iteration's
// `result` event.
//
// Session wraps an io.Writer and enforces both invariants at the
// emission point: assistant/user events are rejected once a result
// has been written, a second result is rejected as well, and a result
// is rejected while any tool_use is still unanswered. Callers drive
// an iteration through Session rather than calling Encode directly so
// the contract cannot be violated by ordering bugs in the driver.

// ErrAfterResult is returned when an assistant or user event is
// emitted after the iteration's terminal result event.
var ErrAfterResult = errors.New("wire: event emitted after result")

// ErrResultAlreadyEmitted is returned when a second result event is
// emitted within the same iteration.
var ErrResultAlreadyEmitted = errors.New("wire: result already emitted")

// ErrPendingToolUse is returned when a result event is emitted while
// one or more tool_use blocks remain unanswered by tool_result blocks.
var ErrPendingToolUse = errors.New("wire: result emitted with pending tool_use")

// R-12IH-ILKT: tool execution is synchronous from the model's point of
// view — each tool_use is followed by exactly one tool_result before
// the next assistant turn. Session refuses a second assistant event
// while any prior tool_use remains unanswered, and refuses a
// tool_result whose tool_use_id is not currently pending (covers both
// duplicate answers and unsolicited tool_results).

// ErrAssistantWithPending is returned when an assistant event is
// emitted while one or more prior tool_use blocks are still
// unanswered.
var ErrAssistantWithPending = errors.New("wire: assistant emitted with pending tool_use")

// ErrUnsolicitedToolResult is returned when a tool_result block is
// emitted whose tool_use_id does not match a currently-pending
// tool_use. Includes duplicate tool_results for an already-answered
// id and tool_results for ids that were never emitted.
var ErrUnsolicitedToolResult = errors.New("wire: tool_result for unknown or already-answered tool_use_id")

// Session is the per-iteration emitter. Construct one with
// NewSession at iteration start; do not reuse across iterations.
type Session struct {
	w        io.Writer
	finished bool
	pending  map[string]struct{}
}

// NewSession returns a Session that writes events to w.
func NewSession(w io.Writer) *Session {
	return &Session{w: w, pending: make(map[string]struct{})}
}

// EmitAssistant writes an assistant event. Fails with ErrAfterResult
// if the iteration's result event has already been written. Records
// any tool_use ids in the event's content as pending answers.
func (s *Session) EmitAssistant(ev AssistantEvent) error {
	if s.finished {
		return ErrAfterResult
	}
	if len(s.pending) > 0 {
		ids := make([]string, 0, len(s.pending))
		for id := range s.pending {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return fmt.Errorf("%w: %v", ErrAssistantWithPending, ids)
	}
	if err := Encode(s.w, ev); err != nil {
		return err
	}
	for _, b := range ev.Message.Content {
		if id, ok := toolUseID(b); ok {
			s.pending[id] = struct{}{}
		}
	}
	return nil
}

// EmitUser writes a user event. Fails with ErrAfterResult if the
// iteration's result event has already been written. Clears any
// pending tool_use ids answered by tool_result blocks in the event.
func (s *Session) EmitUser(ev UserEvent) error {
	if s.finished {
		return ErrAfterResult
	}
	seen := make(map[string]struct{})
	for _, b := range ev.Message.Content {
		if id, ok := toolResultID(b); ok {
			if _, pending := s.pending[id]; !pending {
				return fmt.Errorf("%w: %s", ErrUnsolicitedToolResult, id)
			}
			if _, dup := seen[id]; dup {
				return fmt.Errorf("%w: %s", ErrUnsolicitedToolResult, id)
			}
			seen[id] = struct{}{}
		}
	}
	if err := Encode(s.w, ev); err != nil {
		return err
	}
	for _, b := range ev.Message.Content {
		if id, ok := toolResultID(b); ok {
			delete(s.pending, id)
		}
	}
	return nil
}

// EmitResult writes the iteration's terminal result event. Fails
// with ErrResultAlreadyEmitted if called more than once, and with
// ErrPendingToolUse if any tool_use block has not yet been answered.
func (s *Session) EmitResult(ev ResultEvent) error {
	if s.finished {
		return ErrResultAlreadyEmitted
	}
	if len(s.pending) > 0 {
		ids := make([]string, 0, len(s.pending))
		for id := range s.pending {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return fmt.Errorf("%w: %v", ErrPendingToolUse, ids)
	}
	if err := Encode(s.w, ev); err != nil {
		return err
	}
	s.finished = true
	return nil
}

// Finished reports whether the iteration's result event has been
// emitted.
func (s *Session) Finished() bool { return s.finished }

// PendingToolUseIDs returns the set of tool_use ids that have been
// emitted but not yet answered by a tool_result, sorted.
func (s *Session) PendingToolUseIDs() []string {
	ids := make([]string, 0, len(s.pending))
	for id := range s.pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// toolUseID extracts a tool_use block's id from a content entry,
// accepting either a typed ToolUseBlock or a generic map carrying
// {"type":"tool_use","id":...}.
func toolUseID(block any) (string, bool) {
	switch b := block.(type) {
	case ToolUseBlock:
		if b.Type == "tool_use" && b.ID != "" {
			return b.ID, true
		}
	case *ToolUseBlock:
		if b != nil && b.Type == "tool_use" && b.ID != "" {
			return b.ID, true
		}
	case map[string]any:
		if t, _ := b["type"].(string); t == "tool_use" {
			if id, _ := b["id"].(string); id != "" {
				return id, true
			}
		}
	}
	return "", false
}

// toolResultID extracts a tool_result block's tool_use_id from a
// content entry, accepting either a typed ToolResultBlock or a
// generic map carrying {"type":"tool_result","tool_use_id":...}.
func toolResultID(block any) (string, bool) {
	switch b := block.(type) {
	case ToolResultBlock:
		if b.Type == "tool_result" && b.ToolUseID != "" {
			return b.ToolUseID, true
		}
	case *ToolResultBlock:
		if b != nil && b.Type == "tool_result" && b.ToolUseID != "" {
			return b.ToolUseID, true
		}
	case map[string]any:
		if t, _ := b["type"].(string); t == "tool_result" {
			if id, _ := b["tool_use_id"].(string); id != "" {
				return id, true
			}
		}
	}
	return "", false
}

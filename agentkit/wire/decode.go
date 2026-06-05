package wire

import (
	"encoding/json"
	"fmt"
)

// ParseStdinUserEvent parses one NDJSON line as a stdin user event.
//
// R-UDBB-ANFD: ikigai-cli reads exactly one event type from stdin in v1:
// `user` events with shape
// {"type":"user","message":{"role":"user","content":[{"type":"text","text":"<prompt>"}]}}.
// Other event types on stdin are rejected.
//
// On success the returned UserEvent has TextBlock values in Message.Content
// so callers can range over them with a type assertion.
func ParseStdinUserEvent(line []byte) (UserEvent, error) {
	var raw struct {
		Type    string `json:"type"`
		Message struct {
			Role    string            `json:"role"`
			Content []json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return UserEvent{}, fmt.Errorf("stdin user event: invalid JSON: %w", err)
	}
	if raw.Type != "user" {
		return UserEvent{}, fmt.Errorf("stdin user event: type = %q, want \"user\"", raw.Type)
	}
	if raw.Message.Role != "user" {
		return UserEvent{}, fmt.Errorf("stdin user event: message.role = %q, want \"user\"", raw.Message.Role)
	}

	blocks := make([]any, 0, len(raw.Message.Content))
	for i, b := range raw.Message.Content {
		var blk struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(b, &blk); err != nil {
			return UserEvent{}, fmt.Errorf("stdin user event: content[%d]: %w", i, err)
		}
		if blk.Type != "text" {
			return UserEvent{}, fmt.Errorf("stdin user event: content[%d].type = %q, want \"text\"", i, blk.Type)
		}
		blocks = append(blocks, NewTextBlock(blk.Text))
	}
	return NewUserEvent(blocks...), nil
}

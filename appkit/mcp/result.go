package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Result is an opaque, ordered tool answer.
type Result struct{ members []jsonMember }

// PanicText is the public error text for an internal tool failure.
const PanicText = "The tool failed with an internal error."

// ErrorResult returns a text block marked as a tool error.
func ErrorResult(text string) Result {
	r := TextResult(text)
	r.members = append(r.members, jsonMember{name: "isError", value: json.RawMessage("true")})
	return r
}

// TextResult returns one text block.
func TextResult(text string) Result {
	value, _ := json.Marshal([]struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{"text", text}})
	return Result{members: []jsonMember{{name: "content", value: value}}}
}

// IsError reports whether the result holds the JSON boolean isError: true.
func (r Result) IsError() bool {
	for _, m := range r.members {
		if m.name == "isError" {
			return bytes.Equal(bytes.TrimSpace(m.value), []byte("true"))
		}
	}
	return false
}

// MarshalJSON encodes the result members in their retained order.
func (r Result) MarshalJSON() ([]byte, error) {
	if r.members == nil {
		return []byte(`{"content":[]}`), nil
	}
	return marshalJSONObject(r.members)
}

// UnmarshalJSON atomically decodes an ordered object, removing resultType.
func (r *Result) UnmarshalJSON(data []byte) error {
	members, err := parseJSONObject(data)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(members))
	retained := make([]jsonMember, 0, len(members))
	for _, m := range members {
		if seen[m.name] {
			return fmt.Errorf("mcp: duplicate result member %q", m.name)
		}
		seen[m.name] = true
		if m.name == "resultType" {
			continue
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, m.value); err != nil {
			return err
		}
		retained = append(retained, jsonMember{name: m.name, value: compact.Bytes()})
	}
	r.members = retained
	return nil
}

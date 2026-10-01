package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type jsonMember struct {
	name  string
	value json.RawMessage
}

func parseJSONObject(data []byte) ([]jsonMember, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("mcp: expected JSON object")
	}
	members := make([]jsonMember, 0)
	for d.More() {
		name, err := d.Token()
		if err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, err
		}
		members = append(members, jsonMember{name: name.(string), value: raw})
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("mcp: trailing JSON data")
	}
	return members, nil
}

func marshalJSONObject(members []jsonMember) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, member := range members {
		if !json.Valid(member.value) {
			return nil, fmt.Errorf("mcp: invalid JSON member %q", member.name)
		}
		if i != 0 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(member.name)
		b.Write(name)
		b.WriteByte(':')
		b.Write(member.value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

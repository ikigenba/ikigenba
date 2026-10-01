// Package services reads the host's published services file.
package services

import (
	"encoding/json"
	"errors"
	"html/template"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// Variable names the environment variable containing the services file path.
const Variable = "IKIGENBA_SERVICES"

// Entry describes an installed service.
type Entry struct {
	Name, URL, Description, Socket string
	Enabled, MCP                   bool
	Icon                           template.HTML
	HasIcon                        bool
}

// List holds services in their published order.
type List []Entry

// Read reads the services file afresh, skipping unusable entries.
func Read(path string) (List, error) {
	cleanPath := filepath.Clean(path)
	if path == "" || path != cleanPath {
		return nil, errors.New("services path must be non-empty and clean")
	}
	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("services path is not a regular file")
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, errors.New("services file is not valid UTF-8")
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, errors.New("services file must contain an object")
	}
	raw := document["services"]
	var entries []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' {
		return nil, errors.New("services member must be an array")
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	list := make(List, 0, len(entries))
	for _, raw := range entries {
		if entry, ok := decodeEntry(raw); ok {
			list = append(list, entry)
		}
	}
	return list, nil
}

// Find returns the first service with the given name.
func (l List) Find(name string) (Entry, bool) {
	for _, entry := range l {
		if entry.Name == name {
			return entry, true
		}
	}
	return Entry{}, false
}

func decodeEntry(raw json.RawMessage) (Entry, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return Entry{}, false
	}
	var entry Entry
	for key, value := range map[string]*string{
		"name": &entry.Name, "url": &entry.URL,
		"description": &entry.Description, "socket": &entry.Socket,
	} {
		if !decodeString(fields[key], value) {
			return Entry{}, false
		}
	}
	if entry.Name == "" || !decodeBool(fields["enabled"], &entry.Enabled) || !decodeBool(fields["mcp"], &entry.MCP) {
		return Entry{}, false
	}
	icon := fields["icon"]
	entry.HasIcon = len(icon) > 0 && icon[0] == '"' && json.Unmarshal(icon, &entry.Icon) == nil
	return entry, true
}

func decodeString(raw json.RawMessage, value *string) bool {
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, value) == nil
}

func decodeBool(raw json.RawMessage, value *bool) bool {
	return (string(raw) == "true" || string(raw) == "false") && json.Unmarshal(raw, value) == nil
}

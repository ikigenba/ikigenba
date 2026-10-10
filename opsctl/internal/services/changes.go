// Package services publishes the host's launcher service entries.
package services

import (
	"encoding/json"
	"reflect"
)

// classify compares the entries from the previous file with the entries that
// will be published. Invalid previous content has no entries.
func classify(previous []byte, current []entry) Changes {
	old := previousEntries(previous)
	next := make(map[string]entry, len(current))
	for _, item := range current {
		next[item.Name] = item
	}

	changes := make(Changes)
	for name, item := range next {
		prior, exists := old[name]
		if !exists {
			changes[name] = Added
			continue
		}
		if sameEntry(prior, item) {
			continue
		}
		switch {
		case prior["enabled"] == true && !item.Enabled:
			changes[name] = Disabled
		case prior["enabled"] == false && item.Enabled:
			changes[name] = Enabled
		default:
			changes[name] = Updated
		}
	}
	for name := range old {
		if _, exists := next[name]; !exists {
			changes[name] = Removed
		}
	}
	return changes
}

func previousEntries(data []byte) map[string]map[string]any {
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil
	}
	items, ok := object["services"].([]any)
	if !ok {
		return nil
	}
	entries := make(map[string]map[string]any, len(items))
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, ok := fields["name"].(string)
		if !ok {
			continue
		}
		if _, exists := entries[name]; !exists {
			entries[name] = fields
		}
	}
	return entries
}

func sameEntry(prior map[string]any, item entry) bool {
	fields := map[string]any{
		"url": item.URL, "description": item.Description, "socket": item.Socket,
		"enabled": item.Enabled, "mcp": item.MCP, "group": item.Group,
	}
	for name, value := range fields {
		if !reflect.DeepEqual(prior[name], value) {
			return false
		}
	}
	icon, present := prior["icon"].(string)
	return present == item.HasIcon && (!present || icon == item.Icon)
}

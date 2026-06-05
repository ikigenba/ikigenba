// Package tools is the registry of model-facing tool descriptors.
//
// R-B9P4-41S7: every tool ikigai-cli implements is offered to the
// underlying model on every request. The driver iterates this
// registry when building each provider request, so adding or
// removing entries here is the only knob that controls the surface
// the model sees. R-AQ6C-0C5B fixes that surface to Read and Bash
// in v1.
package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"agentkit/tools/bash"
	"agentkit/tools/edit"
	"agentkit/tools/glob"
	"agentkit/tools/grep"
	"agentkit/tools/read"
	"agentkit/tools/write"
)

// Descriptor is the model-facing advertisement of a tool: its
// stable name and the JSON Schema describing its input shape.
type Descriptor struct {
	Name        string
	InputSchema json.RawMessage
}

// All returns the registry in a stable order. The driver MUST
// advertise every entry on every provider request.
func All() []Descriptor {
	return []Descriptor{
		{Name: read.Name, InputSchema: read.InputSchema},
		{Name: bash.Name, InputSchema: bash.InputSchema},
		{Name: write.Name, InputSchema: write.InputSchema},
		{Name: edit.Name, InputSchema: edit.InputSchema},
		{Name: glob.Name, InputSchema: glob.InputSchema},
		{Name: grep.Name, InputSchema: grep.InputSchema},
	}
}

// Select filters All() to the tools named in the comma-separated s.
// An empty s (the --tools default) means all tools. Whitespace around
// commas and empty elements are tolerated and ignored. An unknown name
// is a fatal error whose message lists the registered tool names.
// R-YFCR-J9IL.
func Select(s string) ([]Descriptor, error) {
	if strings.TrimSpace(s) == "" {
		return All(), nil
	}
	all := All()
	byName := make(map[string]Descriptor, len(all))
	for _, d := range all {
		byName[d.Name] = d
	}
	var out []Descriptor
	seen := map[string]bool{}
	for _, tok := range strings.Split(s, ",") {
		name := strings.TrimSpace(tok)
		if name == "" {
			continue
		}
		d, ok := byName[name]
		if !ok {
			registered := make([]string, 0, len(all))
			for _, d := range all {
				registered = append(registered, d.Name)
			}
			return nil, fmt.Errorf("unknown tool %q; registered tools: %s", name, strings.Join(registered, ", "))
		}
		if !seen[name] {
			out = append(out, d)
			seen[name] = true
		}
	}
	return out, nil
}

package read_test

import (
	"encoding/json"
	"testing"

	"agentkit/tools/read"
)

// R-YNXM-CVXI: the Read tool's exposed name and input JSON schema
// match Claude Code's built-in Read for the supported MVP subset.
// A model that has used Claude Code's Read must be able to call
// ikigai-cli's Read with the same arguments.
func TestR_YNXM_CVXI_ReadNameAndSchemaMatchClaudeCode(t *testing.T) {
	if read.Name != "Read" {
		t.Errorf("Name = %q, want %q", read.Name, "Read")
	}

	var schema map[string]any
	if err := json.Unmarshal(read.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}

	if got := schema["type"]; got != "object" {
		t.Errorf(`schema["type"] = %v, want "object"`, got)
	}

	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "file_path" {
		t.Errorf("required = %v, want [\"file_path\"]", required)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %T", schema["properties"])
	}

	cases := []struct {
		field    string
		wantType string
	}{
		{"file_path", "string"},
		{"offset", "integer"},
		{"limit", "integer"},
	}
	for _, c := range cases {
		p, ok := props[c.field].(map[string]any)
		if !ok {
			t.Errorf("properties[%q] missing", c.field)
			continue
		}
		if p["type"] != c.wantType {
			t.Errorf("properties[%q].type = %v, want %q", c.field, p["type"], c.wantType)
		}
		if _, ok := p["description"].(string); !ok {
			t.Errorf("properties[%q].description missing or not string", c.field)
		}
	}

	// Properties outside the MVP subset (pages, notebook-specific args,
	// etc.) must not be advertised — they are unsupported by our Read.
	for k := range props {
		switch k {
		case "file_path", "offset", "limit":
		default:
			t.Errorf("unexpected property %q in InputSchema (MVP subset is file_path/offset/limit)", k)
		}
	}
}

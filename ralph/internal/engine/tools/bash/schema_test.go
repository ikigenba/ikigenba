package bash_test

import (
	"encoding/json"
	"testing"

	"ralph/internal/engine/tools/bash"
)

// R-YNXM-CVXI: the Bash tool's exposed name and input JSON schema
// match Claude Code's built-in Bash for the supported MVP subset.
// A model that has used Claude Code's Bash must be able to call
// ikigai-cli's Bash with the same arguments.
func TestR_YNXM_CVXI_BashNameAndSchemaMatchClaudeCode(t *testing.T) {
	if bash.Name != "Bash" {
		t.Errorf("Name = %q, want %q", bash.Name, "Bash")
	}

	var schema map[string]any
	if err := json.Unmarshal(bash.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}

	if got := schema["type"]; got != "object" {
		t.Errorf(`schema["type"] = %v, want "object"`, got)
	}

	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "command" {
		t.Errorf("required = %v, want [\"command\"]", required)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %T", schema["properties"])
	}

	cmd, ok := props["command"].(map[string]any)
	if !ok {
		t.Fatalf("properties.command missing")
	}
	if cmd["type"] != "string" {
		t.Errorf("properties.command.type = %v, want \"string\"", cmd["type"])
	}
	if _, ok := cmd["description"].(string); !ok {
		t.Errorf("properties.command.description missing or not string")
	}

	// MVP Bash supports only `command`. Background execution, custom
	// timeouts, sandbox, and description fields are intentionally not
	// advertised — they would mislead the model about behavior we
	// don't implement.
	for k := range props {
		if k != "command" {
			t.Errorf("unexpected property %q in InputSchema (MVP subset is command-only)", k)
		}
	}
}

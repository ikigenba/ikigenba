package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-1OPL-X3LD: structured_output must validate against the schema
// supplied via --json-schema. The canonical ralph-loops schema is
// {"status":"DONE"|"CONTINUE"}; verify accept/reject behavior plus a
// few neighbors that exercise the supported keyword set (type,
// properties, required, enum, additionalProperties).
func TestR_1OPL_X3LD_StructuredOutputValidates(t *testing.T) {
	const ralphSchema = `{
		"type": "object",
		"properties": {
			"status": {"type": "string", "enum": ["DONE", "CONTINUE"]}
		},
		"required": ["status"],
		"additionalProperties": false
	}`

	s, err := Parse([]byte(ralphSchema))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	type tc struct {
		name      string
		input     string
		wantError bool
		wantSubs  string
	}
	cases := []tc{
		{"done", `{"status":"DONE"}`, false, ""},
		{"continue", `{"status":"CONTINUE"}`, false, ""},
		{"missing_status", `{}`, true, "required"},
		{"bad_enum", `{"status":"MAYBE"}`, true, "enum"},
		{"wrong_type_status", `{"status":7}`, true, "type"},
		{"extra_property", `{"status":"DONE","extra":1}`, true, "unexpected"},
		{"not_an_object", `"DONE"`, true, "type"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(c.input), &v); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := s.Validate(v)
			if c.wantError && err == nil {
				t.Fatalf("expected error for %s, got nil", c.input)
			}
			if !c.wantError && err != nil {
				t.Fatalf("unexpected error for %s: %v", c.input, err)
			}
			if c.wantSubs != "" && err != nil && !strings.Contains(err.Error(), c.wantSubs) {
				t.Errorf("error %q does not contain %q", err.Error(), c.wantSubs)
			}
		})
	}

	t.Run("load_from_file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "schema.json")
		if err := os.WriteFile(path, []byte(ralphSchema), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		var v any
		_ = json.Unmarshal([]byte(`{"status":"DONE"}`), &v)
		if err := loaded.Validate(v); err != nil {
			t.Fatalf("loaded schema rejected DONE: %v", err)
		}
	})
}

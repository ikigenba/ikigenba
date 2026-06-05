// Package schema implements the minimal JSON Schema subset needed to
// validate ikigai-cli's `structured_output` against the schema supplied
// via `--json-schema` (R-1OPL-X3LD).
//
// Supported keywords:
//   - type: "object" | "string" | "number" | "integer" | "boolean" | "null" | "array"
//   - properties (for objects)
//   - required (for objects)
//   - enum (any leaf type)
//   - items (for arrays)
//   - additionalProperties=false (for objects)
//
// This is enough for the ralph-loops schema
// {"type":"object","properties":{"status":{"enum":["DONE","CONTINUE"]}},"required":["status"]}
// and any near neighbors. We deliberately do not vendor a full
// JSON-Schema library; the contract surface is small.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type Schema struct {
	Type                 string             `json:"type,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Enum                 []json.RawMessage  `json:"enum,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *bool              `json:"additionalProperties,omitempty"`
}

func Load(path string) (*Schema, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (*Schema, error) {
	var s Schema
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("schema: parse: %w", err)
	}
	return &s, nil
}

// Validate checks that v (the result of unmarshalling JSON into any)
// satisfies the schema. Errors describe the first failure encountered.
func (s *Schema) Validate(v any) error {
	return s.validateAt("", v)
}

func (s *Schema) validateAt(path string, v any) error {
	if s == nil {
		return nil
	}
	if s.Type != "" {
		if err := checkType(path, s.Type, v); err != nil {
			return err
		}
	}
	if len(s.Enum) > 0 {
		if err := checkEnum(path, s.Enum, v); err != nil {
			return err
		}
	}
	switch s.Type {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		for _, key := range s.Required {
			if _, present := obj[key]; !present {
				return fmt.Errorf("%s: missing required property %q", displayPath(path), key)
			}
		}
		if s.AdditionalProperties != nil && !*s.AdditionalProperties {
			keys := sortedKeys(obj)
			for _, k := range keys {
				if _, declared := s.Properties[k]; !declared {
					return fmt.Errorf("%s: unexpected property %q", displayPath(path), k)
				}
			}
		}
		for name, sub := range s.Properties {
			if val, present := obj[name]; present {
				if err := sub.validateAt(joinPath(path, name), val); err != nil {
					return err
				}
			}
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return nil
		}
		if s.Items != nil {
			for i, item := range arr {
				if err := s.Items.validateAt(fmt.Sprintf("%s[%d]", displayPath(path), i), item); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkType(path, want string, v any) error {
	got := jsonType(v)
	if got == want {
		return nil
	}
	if want == "number" && got == "integer" {
		return nil
	}
	return fmt.Errorf("%s: expected type %s, got %s", displayPath(path), want, got)
}

func jsonType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if x == float64(int64(x)) {
			return "integer"
		}
		return "number"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func checkEnum(path string, allowed []json.RawMessage, v any) error {
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("%s: %w", displayPath(path), err)
	}
	for _, raw := range allowed {
		if jsonEqual(raw, encoded) {
			return nil
		}
	}
	return fmt.Errorf("%s: value %s not in enum", displayPath(path), encoded)
}

func jsonEqual(a, b []byte) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	ae, _ := json.Marshal(av)
	be, _ := json.Marshal(bv)
	return string(ae) == string(be)
}

func displayPath(p string) string {
	if p == "" {
		return "(root)"
	}
	return p
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

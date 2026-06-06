package outbox

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// EventType declares one published event type. Sample is the single source for
// BOTH the JSON Schema (reflected from its type) and the worked example
// (marshaled from its value), so the two cannot drift.
type EventType struct {
	Type        string // wire type, e.g. "contact.created" (§8.5)
	Description string // what this fact means and when it fires
	Sample      any    // a filled-in instance of the payload struct
}

// Registry is the ordered set of event types a service publishes. Order is
// preserved so reflection output is stable.
type Registry []EventType

// has reports whether eventType is declared in the registry.
func (r Registry) has(eventType string) bool {
	for _, et := range r {
		if et.Type == eventType {
			return true
		}
	}
	return false
}

// types returns the declared type strings, in registry order.
func (r Registry) types() []string {
	out := make([]string, len(r))
	for i, et := range r {
		out[i] = et.Type
	}
	return out
}

// Index renders the registry as the reflection index: one {type, description}
// entry per declared type, in registry order.
func (r Registry) Index() []map[string]any {
	out := make([]map[string]any, len(r))
	for i, et := range r {
		out[i] = map[string]any{
			"type":        et.Type,
			"description": et.Description,
		}
	}
	return out
}

// UnknownEventTypeError is the typed error Detail returns for a type not in the
// registry. It carries the valid type list so a caller can build a corrective
// message (the ledger bad_root pattern).
type UnknownEventTypeError struct {
	Type  string   // the unknown type requested
	Valid []string // the declared types, in registry order
}

func (e *UnknownEventTypeError) Error() string {
	return fmt.Sprintf("outbox: unknown event type %q; valid types: %s", e.Type, strings.Join(e.Valid, ", "))
}

// Detail renders the publish detail for one declared event type:
// {type, description, schema, example}. The schema is reflected from the
// sample's type and the example is marshaled from the sample's value, so the
// two cannot drift. An unknown type yields a *UnknownEventTypeError carrying the
// valid-type list.
func (r Registry) Detail(eventType string) (map[string]any, error) {
	for _, et := range r {
		if et.Type != eventType {
			continue
		}
		example, err := exampleOf(et.Sample)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type":        et.Type,
			"description": et.Description,
			"schema":      schema(et.Sample),
			"example":     example,
		}, nil
	}
	return nil, &UnknownEventTypeError{Type: eventType, Valid: r.types()}
}

// exampleOf marshals the sample value and re-decodes it into a generic value, so
// the worked example renders as plain JSON honoring the sample's json tags.
func exampleOf(sample any) (any, error) {
	b, err := json.Marshal(sample)
	if err != nil {
		return nil, fmt.Errorf("outbox: marshal example: %w", err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("outbox: decode example: %w", err)
	}
	return v, nil
}

// schema reflects a JSON Schema from sample's type. It covers exactly the shapes
// the suite's payload structs use: string/bool/int*/float*, pointers (optional),
// slices (array with items), and nested structs (inline object). An unsupported
// kind panics — a silently-wrong schema is never emitted (fail loudly).
func schema(sample any) map[string]any {
	return schemaOf(reflect.TypeOf(sample))
}

func schemaOf(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Ptr:
		return schemaOf(t.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaOf(t.Elem())}
	case reflect.Struct:
		props := map[string]any{}
		var required []string
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue // unexported
			}
			name, optional, skip := fieldJSON(f)
			if skip {
				continue
			}
			props[name] = schemaOf(f.Type)
			if !optional {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	default:
		panic(fmt.Sprintf("outbox: schema: unsupported kind %s", t.Kind()))
	}
}

// fieldJSON resolves a struct field's JSON property name and whether it is
// optional (pointer or omitempty → not required). A json:"-" field is skipped.
func fieldJSON(f reflect.StructField) (name string, optional, skip bool) {
	tag := f.Tag.Get("json")
	name = f.Name
	if tag != "" {
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			return "", false, true
		}
		if parts[0] != "" {
			name = parts[0]
		}
		for _, opt := range parts[1:] {
			if opt == "omitempty" {
				optional = true
			}
		}
	}
	if f.Type.Kind() == reflect.Ptr {
		optional = true
	}
	return name, optional, false
}

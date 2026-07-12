package outbox

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"eventplane/routing"
)

// Family declares one published event family. Sample is the single source for
// BOTH the JSON Schema (reflected from its type) and the worked example
// (marshaled from its value), so the two cannot drift.
type Family struct {
	Kind        string
	Subject     string
	Description string // what this fact means and when it fires
	Sample      any    // a filled-in instance of the payload struct
}

// Registry is the ordered set of event families a service publishes. Order is
// preserved so reflection output is stable.
type Registry []Family

// has reports whether eventType is declared in the registry.
func (r Registry) has(kind string) bool {
	for _, family := range r {
		if family.Kind == kind {
			return true
		}
	}
	return false
}

// types returns the declared type strings, in registry order.
func (r Registry) kinds() []string {
	out := make([]string, len(r))
	for i, family := range r {
		out[i] = family.Kind
	}
	return out
}

// Index renders the registry as the reflection index: one {type, description}
// entry per declared type, in registry order.
func (r Registry) Index() []map[string]any {
	out := make([]map[string]any, len(r))
	for i, family := range r {
		out[i] = map[string]any{
			"kind":        family.Kind,
			"subject":     family.Subject,
			"description": family.Description,
		}
	}
	return out
}

// UnknownKindError is the typed error Detail returns for a kind not in the
// registry. It carries the valid type list so a caller can build a corrective
// message (the ledger bad_root pattern).
type UnknownKindError struct {
	Kind  string
	Valid []string
}

func (e *UnknownKindError) Error() string {
	return fmt.Sprintf("outbox: unknown event kind %q; valid kinds: %s", e.Kind, strings.Join(e.Valid, ", "))
}

// Detail renders the publish detail for one declared family. The schema is reflected from the
// sample's type and the example is marshaled from the sample's value, so the
// two cannot drift. An unknown kind yields a *UnknownKindError carrying the
// valid-kind list.
func (r Registry) Detail(kind string) (map[string]any, error) {
	for _, family := range r {
		if family.Kind != kind {
			continue
		}
		example, err := exampleOf(family.Sample)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":        family.Kind,
			"subject":     family.Subject,
			"description": family.Description,
			"schema":      schema(family.Sample),
			"example":     example,
		}, nil
	}
	return nil, &UnknownKindError{Kind: kind, Valid: r.kinds()}
}

// CouldMatch reports whether filter can match any event in a declared family.
// Subjects are deliberately open rather than inferred from their descriptions.
func (r Registry) CouldMatch(source, filter string) (bool, error) {
	for _, family := range r {
		ok, err := routing.CouldMatchSubject(filter, routing.Key(source, family.Kind, ""))
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	// Validate malformed filters even for an empty registry.
	_, err := routing.Match(filter, "")
	return false, err
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

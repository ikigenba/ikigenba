package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Tool is a callable the model may invoke. It is sealed so every construction
// passes through agentkit's validated constructors.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Call(ctx context.Context, args json.RawMessage) (string, error)
	isTool()
}

type concreteTool struct {
	name        string
	description string
	schema      json.RawMessage
	call        func(context.Context, json.RawMessage) (string, error)
}

func (t concreteTool) Name() string            { return t.name }
func (t concreteTool) Description() string     { return t.description }
func (t concreteTool) Schema() json.RawMessage { return t.schema }
func (t concreteTool) isTool()                 {}
func (t concreteTool) Call(ctx context.Context, args json.RawMessage) (string, error) {
	return t.call(ctx, args)
}

// NewTool builds a Tool from a Go input type and a typed callback.
func NewTool[In any](name, description string, fn func(ctx context.Context, in In) (string, error)) (Tool, error) {
	schemaValue, err := deriveToolSchema(reflect.TypeFor[In](), make(map[reflect.Type]bool))
	if err != nil {
		return nil, fmt.Errorf("agentkit: derive tool schema: %w", err)
	}
	schema, err := json.Marshal(schemaValue)
	if err != nil {
		return nil, fmt.Errorf("agentkit: derive tool schema: %w", err)
	}
	return newTool(name, description, schema, func(ctx context.Context, args json.RawMessage) (string, error) {
		var input In
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("agentkit: decode tool arguments: %w", err)
		}
		return fn(ctx, input)
	})
}

func deriveToolSchema(typeOf reflect.Type, visiting map[reflect.Type]bool) (map[string]any, error) {
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if visiting[typeOf] {
		return nil, fmt.Errorf("recursive input type %s is not supported", typeOf)
	}
	visiting[typeOf] = true
	defer delete(visiting, typeOf)

	switch typeOf.Kind() {
	case reflect.Struct:
		properties := make(map[string]any)
		required := make([]string, 0)
		for index := range typeOf.NumField() {
			field := typeOf.Field(index)
			if !field.IsExported() {
				continue
			}
			jsonName, omitEmpty, skip := toolJSONField(field)
			if skip {
				continue
			}
			property, err := deriveToolSchema(field.Type, visiting)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", field.Name, err)
			}
			if description := field.Tag.Get("jsonschema_description"); description != "" {
				property["description"] = description
			}
			properties[jsonName] = property
			if !omitEmpty && toolSchemaRequired(field.Tag.Get("jsonschema")) {
				required = append(required, jsonName)
			}
		}
		schema := map[string]any{"type": "object", "properties": properties}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema, nil
	case reflect.Slice, reflect.Array:
		items, err := deriveToolSchema(typeOf.Elem(), visiting)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	case reflect.Map:
		if typeOf.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map key type %s is not supported", typeOf.Key())
		}
		values, err := deriveToolSchema(typeOf.Elem(), visiting)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "object", "additionalProperties": values}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.Interface:
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("input type %s is not supported", typeOf)
	}
}

func toolJSONField(field reflect.StructField) (name string, omitEmpty, skip bool) {
	name = field.Name
	if tag, ok := field.Tag.Lookup("json"); ok {
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			return "", false, true
		}
		if parts[0] != "" {
			name = parts[0]
		}
		for _, option := range parts[1:] {
			omitEmpty = omitEmpty || option == "omitempty"
		}
	}
	return name, omitEmpty, false
}

func toolSchemaRequired(tag string) bool {
	for _, option := range strings.Split(tag, ",") {
		if option == "required" {
			return true
		}
	}
	return false
}

// MustTool is the panicking sibling of NewTool.
func MustTool[In any](name, description string, fn func(ctx context.Context, in In) (string, error)) Tool {
	tool, err := NewTool(name, description, fn)
	if err != nil {
		panic(err)
	}
	return tool
}

// NewToolFromSchema builds a Tool from a raw input schema and callback.
func NewToolFromSchema(name, description string, schema json.RawMessage, fn func(ctx context.Context, args json.RawMessage) (string, error)) (Tool, error) {
	return newTool(name, description, schema, fn)
}

func newTool(name, description string, schema json.RawMessage, fn func(context.Context, json.RawMessage) (string, error)) (Tool, error) {
	if err := ValidateToolSchema(schema); err != nil {
		return nil, fmt.Errorf("agentkit: tool %q: %w", name, err)
	}
	return concreteTool{name: name, description: description, schema: schema, call: fn}, nil
}

// ValidateToolSchema reports whether a raw schema lies within the canonical
// subset every wire can render.
func ValidateToolSchema(schema json.RawMessage) error {
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return fmt.Errorf("invalid JSON schema: %w", err)
	}
	object, ok := root.(map[string]any)
	if !ok || object["type"] != "object" {
		return errors.New("schema root must have type object")
	}
	return rejectUnsupportedSchemaKeywords(object)
}

func rejectUnsupportedSchemaKeywords(value any) error {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			switch key {
			case "$ref", "$defs", "definitions", "allOf", "anyOf", "oneOf", "not", "if", "then", "else":
				return fmt.Errorf("unsupported schema keyword %q", key)
			}
			if err := rejectUnsupportedSchemaKeywords(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := rejectUnsupportedSchemaKeywords(child); err != nil {
				return err
			}
		}
	}
	return nil
}

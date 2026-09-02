package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
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
//
// Exported struct fields may use a comma-separated jsonschema string tag. The
// flag "required" and the key=value forms "enum=a|b", "description=text",
// "minimum=n", "maximum=n", "exclusiveMinimum=n", "exclusiveMaximum=n",
// "multipleOf=n", "minLength=n", "maxLength=n", "pattern=expr",
// "format=name", "minItems=n", "maxItems=n", and "uniqueItems=true|false"
// are recognized. Unknown keys and malformed values make NewTool return an
// error. JSON field names continue to come from the field's json tag.
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
			jsonName, skip := toolJSONField(field)
			if skip {
				continue
			}
			property, err := deriveToolSchema(field.Type, visiting)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", field.Name, err)
			}
			isRequired, err := applyToolSchemaTag(property, field.Tag.Get("jsonschema"))
			if err != nil {
				return nil, fmt.Errorf("field %s jsonschema tag: %w", field.Name, err)
			}
			// Retain the precursor spelling while the documented jsonschema tag is
			// the preferred string contract.
			if description := field.Tag.Get("jsonschema_description"); description != "" {
				property["description"] = description
			}
			properties[jsonName] = property
			if isRequired {
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

func toolJSONField(field reflect.StructField) (name string, skip bool) {
	name = field.Name
	if tag, ok := field.Tag.Lookup("json"); ok {
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			return "", true
		}
		if parts[0] != "" {
			name = parts[0]
		}
	}
	return name, false
}

func applyToolSchemaTag(schema map[string]any, tag string) (bool, error) {
	if tag == "" {
		return false, nil
	}
	required := false
	seen := make(map[string]bool)
	for _, entry := range strings.Split(tag, ",") {
		key, raw, hasValue := strings.Cut(entry, "=")
		if key == "" {
			return false, errors.New("empty option")
		}
		if seen[key] {
			return false, fmt.Errorf("duplicate option %q", key)
		}
		seen[key] = true
		switch key {
		case "required":
			if hasValue {
				return false, errors.New("required does not take a value")
			}
			required = true
		case "enum":
			if !hasValue || raw == "" {
				return false, errors.New("enum requires a non-empty value")
			}
			values := strings.Split(raw, "|")
			converted := make([]any, len(values))
			for index, value := range values {
				if value == "" {
					return false, errors.New("enum contains an empty value")
				}
				parsed, err := parseToolEnumValue(schema["type"], value)
				if err != nil {
					return false, fmt.Errorf("enum value %q: %w", value, err)
				}
				converted[index] = parsed
			}
			schema[key] = converted
		case "description", "pattern", "format":
			if !hasValue || raw == "" {
				return false, fmt.Errorf("%s requires a non-empty value", key)
			}
			schema[key] = raw
		case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf":
			value, err := parseToolNumber(key, raw, hasValue)
			if err != nil {
				return false, err
			}
			schema[key] = value
		case "minLength", "maxLength", "minItems", "maxItems":
			value, err := parseToolNonNegativeInteger(key, raw, hasValue)
			if err != nil {
				return false, err
			}
			schema[key] = value
		case "uniqueItems":
			if !hasValue {
				return false, errors.New("uniqueItems requires true or false")
			}
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return false, fmt.Errorf("uniqueItems requires true or false: %w", err)
			}
			schema[key] = value
		default:
			return false, fmt.Errorf("unknown option %q", key)
		}
	}
	return required, nil
}

func parseToolEnumValue(typeName any, raw string) (any, error) {
	switch typeName {
	case "string":
		return raw, nil
	case "integer":
		value, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			return value, nil
		}
	case "number":
		value, err := strconv.ParseFloat(raw, 64)
		if _, finite := finiteToolNumber(value); err == nil && finite {
			return value, nil
		}
	case "boolean":
		value, err := strconv.ParseBool(raw)
		if err == nil {
			return value, nil
		}
	default:
		return nil, fmt.Errorf("is not supported for schema type %q", typeName)
	}
	return nil, fmt.Errorf("must match schema type %q", typeName)
}

func parseToolNumber(key, raw string, hasValue bool) (float64, error) {
	if !hasValue || raw == "" {
		return 0, fmt.Errorf("%s requires a number", key)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s requires a number", key)
	}
	if _, finite := finiteToolNumber(value); !finite {
		return 0, fmt.Errorf("%s requires a finite number", key)
	}
	return value, nil
}

func parseToolNonNegativeInteger(key, raw string, hasValue bool) (int64, error) {
	if !hasValue || raw == "" {
		return 0, fmt.Errorf("%s requires a non-negative integer", key)
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if valid := nonNegativeToolInteger(value); err != nil || !valid {
		return 0, fmt.Errorf("%s requires a non-negative integer", key)
	}
	return value, nil
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
	decoder := json.NewDecoder(bytes.NewReader(schema))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return fmt.Errorf("invalid JSON schema: %w", err)
	}
	if err := ensureToolSchemaEOF(decoder); err != nil {
		return err
	}
	object, ok := root.(map[string]any)
	if !ok {
		return errors.New("schema root must be an object")
	}
	if object["type"] != "object" {
		return errors.New("schema root must have type object")
	}
	return validateToolSchemaNode(object, "$")
}

func ensureToolSchemaEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid JSON schema: multiple JSON values")
		}
		return fmt.Errorf("invalid JSON schema: %w", err)
	}
	return nil
}

func validateToolSchemaNode(schema map[string]any, path string) error {
	for _, keyword := range []string{"$ref", "$defs", "definitions", "additionalProperties", "allOf", "not", "if", "then", "else"} {
		if _, present := schema[keyword]; present {
			return fmt.Errorf("%s: %s not permitted", path, keyword)
		}
	}
	if _, anyOf := schema["anyOf"]; anyOf {
		return validateNullableToolSchema(schema, path, "anyOf")
	}
	if _, oneOf := schema["oneOf"]; oneOf {
		return validateNullableToolSchema(schema, path, "oneOf")
	}

	typeName, ok := schema["type"].(string)
	if !ok {
		return fmt.Errorf("%s: type must be one supported string", path)
	}
	if typeName == "null" {
		return fmt.Errorf("%s: type %q not permitted", path, typeName)
	}
	allowed := map[string]bool{"type": true, "description": true, "enum": true}
	switch typeName {
	case "object":
		allowed["properties"] = true
		allowed["required"] = true
	case "array":
		allowed["items"] = true
		allowed["minItems"] = true
		allowed["maxItems"] = true
		allowed["uniqueItems"] = true
	case "string":
		allowed["minLength"] = true
		allowed["maxLength"] = true
		allowed["pattern"] = true
		allowed["format"] = true
	case "number", "integer":
		allowed["minimum"] = true
		allowed["maximum"] = true
		allowed["exclusiveMinimum"] = true
		allowed["exclusiveMaximum"] = true
		allowed["multipleOf"] = true
	case "boolean":
	default:
		return fmt.Errorf("%s: type %q not permitted", path, typeName)
	}
	if err := validateToolSchemaKeywords(schema, allowed, path, typeName); err != nil {
		return err
	}
	switch typeName {
	case "object":
		return validateToolObjectSchema(schema, path)
	case "array":
		return validateToolArraySchema(schema, path)
	case "string":
		return validateToolStringSchema(schema, path)
	case "number", "integer":
		return validateToolNumericSchema(schema, path)
	default:
		return nil
	}
}

func validateNullableToolSchema(schema map[string]any, path, keyword string) error {
	if len(schema) != 1 {
		return fmt.Errorf("%s: %s nullable form may not have sibling keywords", path, keyword)
	}
	branches, ok := schema[keyword].([]any)
	if !ok || len(branches) != 2 {
		return fmt.Errorf("%s: %s only permits one schema plus null", path, keyword)
	}
	regular := -1
	null := -1
	for index, branch := range branches {
		object, branchOK := branch.(map[string]any)
		if !branchOK {
			return fmt.Errorf("%s.%s[%d]: schema must be an object", path, keyword, index)
		}
		if object["type"] == "null" && len(object) == 1 {
			null = index
		} else {
			regular = index
		}
	}
	if regular < 0 || null < 0 {
		return fmt.Errorf("%s: %s only permits one schema plus null", path, keyword)
	}
	return validateToolSchemaNode(branches[regular].(map[string]any), fmt.Sprintf("%s.%s[%d]", path, keyword, regular))
}

func validateToolSchemaKeywords(schema map[string]any, allowed map[string]bool, path, typeName string) error {
	keys := make([]string, 0, len(schema))
	for key := range schema {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !allowed[key] {
			return fmt.Errorf("%s: keyword %q not permitted for type %s", path, key, typeName)
		}
	}
	if description, present := schema["description"]; present {
		if _, ok := description.(string); !ok {
			return fmt.Errorf("%s: description must be a string", path)
		}
	}
	if enum, present := schema["enum"]; present {
		values, ok := enum.([]any)
		if !ok || len(values) == 0 {
			return fmt.Errorf("%s: enum must be a non-empty array", path)
		}
		for index, value := range values {
			if !toolEnumMatchesType(value, typeName) {
				return fmt.Errorf("%s.enum[%d]: value does not match type %s", path, index, typeName)
			}
		}
	}
	return nil
}

func toolEnumMatchesType(value any, typeName string) bool {
	switch typeName {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := finiteToolNumber(value)
		return ok
	case "integer":
		return isToolInteger(value)
	default:
		return false
	}
}

func isToolInteger(value any) bool {
	switch value := value.(type) {
	case int64:
		return true
	case json.Number:
		_, err := strconv.ParseInt(value.String(), 10, 64)
		return err == nil
	default:
		return false
	}
}

func finiteToolNumber(value any) (float64, bool) {
	var (
		parsed float64
		err    error
	)
	switch value := value.(type) {
	case float64:
		parsed = value
	case json.Number:
		parsed, err = value.Float64()
	default:
		return 0, false
	}
	return parsed, err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed)
}

func nonNegativeToolInteger(value any) bool {
	var (
		parsed int64
		err    error
	)
	switch value := value.(type) {
	case int64:
		parsed = value
	case json.Number:
		parsed, err = strconv.ParseInt(value.String(), 10, 64)
	default:
		return false
	}
	return err == nil && parsed >= 0
}

func validateToolObjectSchema(schema map[string]any, path string) error {
	properties := make(map[string]any)
	if raw, present := schema["properties"]; present {
		var ok bool
		properties, ok = raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: properties must be an object", path)
		}
		propertyNames := make([]string, 0, len(properties))
		for name := range properties {
			propertyNames = append(propertyNames, name)
		}
		sort.Strings(propertyNames)
		for _, name := range propertyNames {
			property, ok := properties[name].(map[string]any)
			if !ok {
				return fmt.Errorf("%s.properties.%s: property schema must be an object", path, name)
			}
			if err := validateToolSchemaNode(property, path+".properties."+name); err != nil {
				return err
			}
		}
	}
	if raw, present := schema["required"]; present {
		required, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("%s: required must be an array of property names", path)
		}
		seen := make(map[string]bool)
		for index, value := range required {
			name, ok := value.(string)
			if !ok || name == "" {
				return fmt.Errorf("%s.required[%d]: required name must be a non-empty string", path, index)
			}
			if seen[name] {
				return fmt.Errorf("%s: required name %q is duplicated", path, name)
			}
			if _, exists := properties[name]; !exists {
				return fmt.Errorf("%s: required name %q has no property", path, name)
			}
			seen[name] = true
		}
	}
	return nil
}

func validateToolArraySchema(schema map[string]any, path string) error {
	raw, present := schema["items"]
	if !present {
		return fmt.Errorf("%s: array items schema is required", path)
	}
	items, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: array items must be one schema; tuple or unconstrained items not permitted", path)
	}
	if err := validateToolSchemaNode(items, path+".items"); err != nil {
		return err
	}
	if err := validateNonNegativeSchemaInteger(schema, path, "minItems"); err != nil {
		return err
	}
	if err := validateNonNegativeSchemaInteger(schema, path, "maxItems"); err != nil {
		return err
	}
	if unique, present := schema["uniqueItems"]; present {
		if _, ok := unique.(bool); !ok {
			return fmt.Errorf("%s: uniqueItems must be a boolean", path)
		}
	}
	return nil
}

func validateToolStringSchema(schema map[string]any, path string) error {
	if err := validateNonNegativeSchemaInteger(schema, path, "minLength"); err != nil {
		return err
	}
	if err := validateNonNegativeSchemaInteger(schema, path, "maxLength"); err != nil {
		return err
	}
	if pattern, present := schema["pattern"]; present {
		value, ok := pattern.(string)
		if !ok {
			return fmt.Errorf("%s: pattern must be a string", path)
		}
		if _, err := regexp.Compile(value); err != nil {
			return fmt.Errorf("%s: pattern is invalid: %w", path, err)
		}
	}
	if format, present := schema["format"]; present {
		if _, ok := format.(string); !ok {
			return fmt.Errorf("%s: format must be a string", path)
		}
	}
	return nil
}

func validateToolNumericSchema(schema map[string]any, path string) error {
	for _, keyword := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
		if value, present := schema[keyword]; present {
			parsed, ok := finiteToolNumber(value)
			if !ok {
				return fmt.Errorf("%s: %s must be a finite number", path, keyword)
			}
			if keyword == "multipleOf" && parsed <= 0 {
				return fmt.Errorf("%s: multipleOf must be greater than zero", path)
			}
		}
	}
	return nil
}

func validateNonNegativeSchemaInteger(schema map[string]any, path, keyword string) error {
	value, present := schema[keyword]
	if !present {
		return nil
	}
	if ok := nonNegativeToolInteger(value); !ok {
		return fmt.Errorf("%s: %s must be a non-negative integer", path, keyword)
	}
	return nil
}

func validateToolArgumentNode(schema map[string]any, value any, path string) error {
	for _, nullable := range []string{"anyOf", "oneOf"} {
		if raw, ok := schema[nullable]; ok {
			branches := raw.([]any)
			if value == nil {
				return nil
			}
			for _, branch := range branches {
				node := branch.(map[string]any)
				if node["type"] != "null" {
					return validateToolArgumentNode(node, value, path)
				}
			}
		}
	}

	typeName := schema["type"].(string)
	if !toolArgumentMatchesType(value, typeName) {
		return fmt.Errorf("%s: value must have type %s", path, typeName)
	}
	if enum, ok := schema["enum"].([]any); ok {
		matched := false
		for _, allowed := range enum {
			matched = matched || reflect.DeepEqual(normalizeJSONNumber(value), normalizeJSONNumber(allowed))
		}
		if !matched {
			return fmt.Errorf("%s: value is not in enum", path)
		}
	}

	switch typed := value.(type) {
	case map[string]any:
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]any); ok {
			for _, raw := range required {
				name := raw.(string)
				if _, present := typed[name]; !present {
					return fmt.Errorf("%s: required property %q is missing", path, name)
				}
			}
		}
		for name, child := range typed {
			if property, ok := properties[name].(map[string]any); ok {
				if err := validateToolArgumentNode(property, child, path+"."+name); err != nil {
					return err
				}
			}
		}
	case []any:
		if minimum, ok := schemaInteger(schema["minItems"]); ok && int64(len(typed)) < minimum {
			return fmt.Errorf("%s: array has fewer than %d items", path, minimum)
		}
		if maximum, ok := schemaInteger(schema["maxItems"]); ok && int64(len(typed)) > maximum {
			return fmt.Errorf("%s: array has more than %d items", path, maximum)
		}
		if unique, _ := schema["uniqueItems"].(bool); unique {
			for left := range typed {
				for right := left + 1; right < len(typed); right++ {
					if reflect.DeepEqual(typed[left], typed[right]) {
						return fmt.Errorf("%s: array items must be unique", path)
					}
				}
			}
		}
		items := schema["items"].(map[string]any)
		for index, child := range typed {
			if err := validateToolArgumentNode(items, child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case string:
		length := int64(len([]rune(typed)))
		if minimum, ok := schemaInteger(schema["minLength"]); ok && length < minimum {
			return fmt.Errorf("%s: string is shorter than %d characters", path, minimum)
		}
		if maximum, ok := schemaInteger(schema["maxLength"]); ok && length > maximum {
			return fmt.Errorf("%s: string is longer than %d characters", path, maximum)
		}
		if pattern, ok := schema["pattern"].(string); ok {
			matched, _ := regexp.MatchString(pattern, typed)
			if !matched {
				return fmt.Errorf("%s: string does not match pattern", path)
			}
		}
	case json.Number:
		value, _ := typed.Float64()
		for _, bound := range []struct {
			name      string
			exclusive bool
			minimum   bool
		}{{"minimum", false, true}, {"maximum", false, false}, {"exclusiveMinimum", true, true}, {"exclusiveMaximum", true, false}} {
			limit, ok := schemaNumber(schema[bound.name])
			if ok && ((bound.minimum && (value < limit || bound.exclusive && value == limit)) ||
				(!bound.minimum && (value > limit || bound.exclusive && value == limit))) {
				return fmt.Errorf("%s: number violates %s", path, bound.name)
			}
		}
		if multiple, ok := schemaNumber(schema["multipleOf"]); ok {
			quotient := value / multiple
			if math.Abs(quotient-math.Round(quotient)) > 1e-9 {
				return fmt.Errorf("%s: number is not a multiple of %v", path, multiple)
			}
		}
	}
	return nil
}

func toolArgumentMatchesType(value any, typeName string) bool {
	switch typeName {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		return isToolInteger(value)
	default:
		return false
	}
}

func schemaInteger(value any) (int64, bool) {
	if value == nil {
		return 0, false
	}
	switch value := value.(type) {
	case int64:
		return value, true
	case json.Number:
		parsed, err := strconv.ParseInt(value.String(), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func schemaNumber(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	return finiteToolNumber(value)
}

func normalizeJSONNumber(value any) any {
	if number, ok := value.(json.Number); ok {
		parsed, _ := number.Float64()
		return parsed
	}
	return value
}

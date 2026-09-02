package agentkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var supportedOutputFormats = map[string]func(string) bool{
	"date-time": validOutputDateTime,
	"date":      validOutputDate,
	"time":      validOutputTime,
	"email":     validOutputEmail,
	"uri":       validOutputURI,
	"uuid":      validOutputUUID,
	"ipv4":      validOutputIPv4,
	"ipv6":      validOutputIPv6,
	"hostname":  validOutputHostname,
}

// OutputSchema derives an output-subset schema from T's jsonschema struct
// tags. Every represented field is required, and pointer fields are nullable.
// llm-lint:ignore god-entrypoint
func OutputSchema[T any]() (json.RawMessage, error) {
	typeOf := reflect.TypeFor[T]()
	if typeOf.Kind() != reflect.Struct {
		return nil, fmt.Errorf("agentkit: derive output schema: root type %s is not an object", typeOf)
	}

	schemaValue, err := deriveOutputSchema(typeOf, make(map[reflect.Type]bool))
	if err != nil {
		return nil, fmt.Errorf("agentkit: derive output schema: %w", err)
	}
	schema, err := json.Marshal(schemaValue)
	if err != nil {
		return nil, fmt.Errorf("agentkit: derive output schema: %w", err)
	}
	if err := ValidateOutputSchema(schema); err != nil {
		return nil, fmt.Errorf("agentkit: derive output schema: %w", err)
	}
	return schema, nil
}

func deriveOutputSchema(typeOf reflect.Type, visiting map[reflect.Type]bool) (map[string]any, error) {
	if typeOf.Kind() == reflect.Pointer {
		ordinaryType := typeOf
		for ordinaryType.Kind() == reflect.Pointer {
			ordinaryType = ordinaryType.Elem()
		}
		ordinary, err := deriveOutputSchema(ordinaryType, visiting)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"anyOf": []any{ordinary, map[string]any{"type": "null"}},
		}, nil
	}
	if visiting[typeOf] {
		return nil, fmt.Errorf("recursive output type %s is not supported", typeOf)
	}
	visiting[typeOf] = true
	defer delete(visiting, typeOf)

	switch typeOf.Kind() {
	case reflect.Struct:
		properties := make(map[string]any)
		required := make([]string, 0, typeOf.NumField())
		for index := range typeOf.NumField() {
			field := typeOf.Field(index)
			if !field.IsExported() {
				continue
			}
			jsonName, skip := toolJSONField(field)
			if skip {
				continue
			}
			property, err := deriveOutputSchema(field.Type, visiting)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", field.Name, err)
			}
			tagTarget := property
			if field.Type.Kind() == reflect.Pointer {
				tagTarget = property["anyOf"].([]any)[0].(map[string]any)
			}
			if _, err := applyToolSchemaTag(tagTarget, field.Tag.Get("jsonschema")); err != nil {
				return nil, fmt.Errorf("field %s jsonschema tag: %w", field.Name, err)
			}
			properties[jsonName] = property
			required = append(required, jsonName)
		}
		return map[string]any{
			"type":                 "object",
			"properties":           properties,
			"required":             required,
			"additionalProperties": false,
		}, nil
	case reflect.Slice, reflect.Array:
		items, err := deriveOutputSchema(typeOf.Elem(), visiting)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	default:
		return nil, fmt.Errorf("output type %s is not supported", typeOf)
	}
}

// ValidateOutputSchema reports whether schema lies within the output subset.
func ValidateOutputSchema(schema json.RawMessage) error {
	document, err := decodeSingleJSON(schema)
	if err != nil {
		// llm-lint:ignore error-context-restated-by-caller
		return fmt.Errorf("invalid JSON schema: %w", err)
	}
	root, ok := document.(map[string]any)
	if !ok {
		return errors.New("schema root must be an object")
	}
	if root["type"] != "object" {
		return errors.New("schema root must have type object")
	}
	validator := outputSchemaValidator{root: root}
	if err := validator.validateNode(root, "$"); err != nil {
		return err
	}
	return validator.detectReferenceCycles(root, "#", map[string]uint8{})
}

type outputSchemaValidator struct {
	root map[string]any
}

func (v outputSchemaValidator) validateNode(schema map[string]any, path string) error {
	if reference, present := schema["$ref"]; present {
		if len(schema) != 1 {
			return fmt.Errorf("%s: $ref may not have sibling keywords", path)
		}
		_, _, err := v.resolveReference(reference, path)
		return err
	}
	if _, present := schema["allOf"]; present {
		return fmt.Errorf("%s: allOf not permitted", path)
	}
	if _, present := schema["oneOf"]; present {
		return fmt.Errorf("%s: oneOf not permitted", path)
	}
	if _, present := schema["anyOf"]; present {
		return v.validateNullable(schema, path)
	}

	typeName, ok := schema["type"].(string)
	if !ok {
		return fmt.Errorf("%s: type must be one supported string", path)
	}
	allowed := map[string]bool{
		"type": true, "description": true, "enum": true, "const": true, "$defs": true,
	}
	switch typeName {
	case "object":
		allowed["properties"] = true
		allowed["required"] = true
		allowed["additionalProperties"] = true
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
	case "boolean", "null":
	default:
		return fmt.Errorf("%s: type %q not permitted", path, typeName)
	}
	if err := validateOutputKeywords(schema, allowed, path, typeName); err != nil {
		return err
	}
	if definitions, present := schema["$defs"]; present {
		objects, ok := definitions.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.$defs: $defs must be an object of schemas", path)
		}
		for name, definition := range objects {
			object, ok := definition.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.$defs[%q]: schema must be an object", path, name)
			}
			if err := v.validateNode(object, path+".$defs."+name); err != nil {
				return err
			}
		}
	}
	switch typeName {
	case "object":
		return v.validateObject(schema, path)
	case "array":
		return v.validateArray(schema, path)
	}
	return nil
}

func validateOutputKeywords(schema map[string]any, allowed map[string]bool, path, typeName string) error {
	for keyword := range schema {
		if !allowed[keyword] {
			return fmt.Errorf("%s: keyword %q is not permitted for type %q", path, keyword, typeName)
		}
	}
	if description, present := schema["description"]; present {
		if _, ok := description.(string); !ok {
			return fmt.Errorf("%s: description must be a string", path)
		}
	}
	if values, present := schema["enum"]; present {
		list, ok := values.([]any)
		if !ok || len(list) == 0 {
			return fmt.Errorf("%s: enum must be a non-empty array", path)
		}
	}
	if value, present := schema["const"]; present && !outputValueMatchesType(value, typeName) {
		return fmt.Errorf("%s: const is inconsistent with type %q", path, typeName)
	}
	for _, keyword := range []string{"minLength", "maxLength", "minItems", "maxItems"} {
		if value, present := schema[keyword]; present && !isOutputNonNegativeInteger(value) {
			return fmt.Errorf("%s: %s must be a non-negative integer", path, keyword)
		}
	}
	for _, keyword := range []string{"pattern", "format"} {
		if value, present := schema[keyword]; present {
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s: %s must be a string", path, keyword)
			}
			if keyword == "pattern" {
				if _, err := regexp.Compile(text); err != nil {
					return fmt.Errorf("%s: pattern is not a valid regular expression: %w", path, err)
				}
			}
			if keyword == "format" {
				if _, supported := supportedOutputFormats[text]; !supported {
					return fmt.Errorf("%s: format %q is not supported", path, text)
				}
			}
		}
	}
	for _, keyword := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
		if value, present := schema[keyword]; present {
			number, ok := outputFiniteNumber(value)
			if !ok {
				return fmt.Errorf("%s: %s must be a finite number", path, keyword)
			}
			if keyword == "multipleOf" && number.Sign() <= 0 {
				return fmt.Errorf("%s: multipleOf must be positive", path)
			}
		}
	}
	if value, present := schema["uniqueItems"]; present {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: uniqueItems must be a boolean", path)
		}
	}
	return nil
}

func (v outputSchemaValidator) validateObject(schema map[string]any, path string) error {
	properties := map[string]any{}
	if value, present := schema["properties"]; present {
		var ok bool
		properties, ok = value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: properties must be an object of schemas", path)
		}
	}
	required := map[string]bool{}
	if value, present := schema["required"]; present {
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: required must be an array of unique property names", path)
		}
		for _, item := range list {
			name, ok := item.(string)
			if !ok || required[name] {
				return fmt.Errorf("%s: required must be an array of unique property names", path)
			}
			required[name] = true
			if _, exists := properties[name]; !exists {
				return fmt.Errorf("%s: required property %q is missing from properties", path, name)
			}
		}
	}
	for name, value := range properties {
		if !required[name] {
			return fmt.Errorf("%s: property %q is not listed in required", path, name)
		}
		child, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.properties[%q]: schema must be an object", path, name)
		}
		if err := v.validateNode(child, path+".properties."+name); err != nil {
			return err
		}
	}
	if value, present := schema["additionalProperties"]; present {
		closed, ok := value.(bool)
		if !ok || closed {
			return fmt.Errorf("%s: additionalProperties must be false", path)
		}
	}
	return nil
}

func (v outputSchemaValidator) validateArray(schema map[string]any, path string) error {
	value, present := schema["items"]
	if !present {
		return fmt.Errorf("%s: items is required for array schemas", path)
	}
	item, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: items must be one schema object", path)
	}
	return v.validateNode(item, path+".items")
}

func (v outputSchemaValidator) validateNullable(schema map[string]any, path string) error {
	if len(schema) != 1 {
		return fmt.Errorf("%s: anyOf nullable form may not have sibling keywords", path)
	}
	branches, ok := schema["anyOf"].([]any)
	if !ok || len(branches) != 2 {
		return fmt.Errorf("%s: anyOf only permits one schema plus null", path)
	}
	nullCount := 0
	for index, branch := range branches {
		object, ok := branch.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.anyOf[%d]: schema must be an object", path, index)
		}
		if object["type"] == "null" {
			nullCount++
		}
	}
	if nullCount != 1 {
		return fmt.Errorf("%s: anyOf only permits one schema plus null", path)
	}
	for index, branch := range branches {
		if err := v.validateNode(branch.(map[string]any), fmt.Sprintf("%s.anyOf[%d]", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func (v outputSchemaValidator) resolveReference(value any, path string) (map[string]any, string, error) {
	reference, ok := value.(string)
	if !ok {
		return nil, "", fmt.Errorf("%s: $ref must be a string", path)
	}
	if !strings.HasPrefix(reference, "#/$defs/") {
		return nil, "", fmt.Errorf("%s: $ref %q must be an internal pointer through $defs", path, reference)
	}
	parts := strings.Split(strings.TrimPrefix(reference, "#/"), "/")
	decoded := make([]string, 0, len(parts))
	for _, part := range parts {
		name, err := decodeOutputPointerToken(part)
		if err != nil {
			return nil, "", fmt.Errorf("%s: malformed $ref %q: %w", path, reference, err)
		}
		decoded = append(decoded, name)
	}
	target, err := resolveOutputSchemaLocation(v.root, decoded)
	if err != nil {
		return nil, "", fmt.Errorf("%s: $ref %q %w", path, reference, err)
	}
	canonical := make([]string, 0, len(decoded))
	for _, token := range decoded {
		canonical = append(canonical, encodeOutputPointerToken(token))
	}
	return target, "#/" + strings.Join(canonical, "/"), nil
}

func resolveOutputSchemaLocation(root map[string]any, tokens []string) (map[string]any, error) {
	current := root
	for index := 0; index < len(tokens); {
		keyword := tokens[index]
		index++
		switch keyword {
		case "$defs", "properties":
			if index == len(tokens) {
				return nil, errors.New("target is a schema container, not a schema")
			}
			children, ok := current[keyword].(map[string]any)
			if !ok {
				return nil, errors.New("target does not exist")
			}
			child, ok := children[tokens[index]].(map[string]any)
			if !ok {
				return nil, errors.New("target does not exist or is not a schema object")
			}
			current = child
			index++
		case "items":
			child, ok := current[keyword].(map[string]any)
			if !ok {
				return nil, errors.New("target does not exist or is not a schema object")
			}
			current = child
		case "anyOf":
			if index == len(tokens) {
				return nil, errors.New("target is a schema container, not a schema")
			}
			branches, ok := current[keyword].([]any)
			branch, parseErr := parseOutputArrayIndex(tokens[index])
			if !ok || parseErr != nil || branch >= len(branches) {
				return nil, errors.New("target does not exist")
			}
			child, ok := branches[branch].(map[string]any)
			if !ok {
				return nil, errors.New("target is not a schema object")
			}
			current = child
			index++
		default:
			return nil, fmt.Errorf("target traverses non-schema keyword %q", keyword)
		}
	}
	return current, nil
}

func parseOutputArrayIndex(token string) (int, error) {
	if token == "" || (token != "0" && (token[0] < '1' || token[0] > '9')) {
		return 0, errors.New("invalid array index")
	}
	for index := 1; index < len(token); index++ {
		if token[index] < '0' || token[index] > '9' {
			return 0, errors.New("invalid array index")
		}
	}
	return strconv.Atoi(token)
}

func decodeOutputPointerToken(token string) (string, error) {
	var result strings.Builder
	for index := 0; index < len(token); index++ {
		if token[index] != '~' {
			result.WriteByte(token[index])
			continue
		}
		if index+1 >= len(token) || (token[index+1] != '0' && token[index+1] != '1') {
			return "", errors.New("invalid JSON pointer escape")
		}
		index++
		if token[index] == '0' {
			result.WriteByte('~')
		} else {
			result.WriteByte('/')
		}
	}
	return result.String(), nil
}

func encodeOutputPointerToken(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}

func (v outputSchemaValidator) detectReferenceCycles(schema map[string]any, path string, states map[string]uint8) error {
	if states[path] == 1 {
		return fmt.Errorf("%s: recursive $ref cycle reaches %s", path, path)
	}
	if states[path] == 2 {
		return nil
	}
	states[path] = 1
	if value, present := schema["$ref"]; present {
		target, targetPath, err := v.resolveReference(value, path)
		if err != nil {
			return err
		}
		if states[targetPath] == 1 {
			return fmt.Errorf("%s: recursive $ref %q reaches %s", path, value, targetPath)
		}
		if err := v.detectReferenceCycles(target, targetPath, states); err != nil {
			return err
		}
	}
	for keyword, value := range schema {
		switch keyword {
		case "properties", "$defs":
			if children, ok := value.(map[string]any); ok {
				for name, child := range children {
					if object, ok := child.(map[string]any); ok {
						if err := v.detectReferenceCycles(object, path+"/"+keyword+"/"+encodeOutputPointerToken(name), states); err != nil {
							return err
						}
					}
				}
			}
		case "items":
			if child, ok := value.(map[string]any); ok {
				if err := v.detectReferenceCycles(child, path+"/items", states); err != nil {
					return err
				}
			}
		case "anyOf":
			if children, ok := value.([]any); ok {
				for index, child := range children {
					if object, ok := child.(map[string]any); ok {
						if err := v.detectReferenceCycles(object, fmt.Sprintf("%s/anyOf/%d", path, index), states); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	states[path] = 2
	return nil
}

func outputValueMatchesType(value any, typeName string) bool {
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
	case "number":
		_, ok := outputFiniteNumber(value)
		return ok
	case "integer":
		return isOutputInteger(value)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func outputFiniteNumber(value any) (*big.Rat, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return nil, false
	}
	parsed, ok := new(big.Rat).SetString(number.String())
	return parsed, ok
}

func isOutputInteger(value any) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	rational, ok := new(big.Rat).SetString(number.String())
	return ok && rational.IsInt()
}

func isOutputNonNegativeInteger(value any) bool {
	if !isOutputInteger(value) {
		return false
	}
	number := value.(json.Number)
	rational, _ := new(big.Rat).SetString(number.String())
	return rational.Sign() >= 0
}

type outputViolation struct {
	Path      string
	Rule      string
	Offending any
	Present   bool
}

func (v *outputViolation) Error() string {
	return v.Path + ": " + v.Rule
}

type outputValidationResult struct {
	Violations []outputViolation
}

func newOutputValidationResult(violations []outputViolation) *outputValidationResult {
	if len(violations) == 0 {
		return nil
	}
	return &outputValidationResult{Violations: violations}
}

func (r *outputValidationResult) Error() string {
	if r == nil || len(r.Violations) == 0 {
		return ""
	}
	return (&r.Violations[0]).Error()
}

func validateOutputDocument(schema, document json.RawMessage) *outputValidationResult {
	rootValue, err := decodeSingleJSON(schema)
	if err != nil {
		return newOutputValidationResult([]outputViolation{{Path: "$", Rule: "retained schema must be valid: " + err.Error(), Offending: string(schema), Present: true}})
	}
	root, ok := rootValue.(map[string]any)
	if !ok {
		return newOutputValidationResult([]outputViolation{{Path: "$", Rule: "retained schema root must be an object", Offending: rootValue, Present: true}})
	}
	value, err := decodeSingleJSON(document)
	if err != nil {
		return newOutputValidationResult([]outputViolation{{
			Path: "$", Rule: "must be valid JSON containing exactly one document: " + err.Error(),
			Offending: string(document), Present: true,
		}})
	}
	validator := outputDocumentValidator{root: root}
	return newOutputValidationResult(validator.validate(root, value, "$"))
}

func decodeSingleJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		// llm-lint:ignore io-error-mapped-to-data-error
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return value, nil
}

type outputDocumentValidator struct {
	root map[string]any
}

func (v outputDocumentValidator) validate(schema map[string]any, value any, path string) []outputViolation {
	if reference, present := schema["$ref"]; present {
		target, _, err := outputSchemaValidator(v).resolveReference(reference, path)
		if err != nil {
			return []outputViolation{{Path: path, Rule: err.Error(), Offending: value, Present: true}}
		}
		return v.validate(target, value, path)
	}
	if branches, present := schema["anyOf"].([]any); present {
		if value == nil {
			for _, branch := range branches {
				if branch.(map[string]any)["type"] == "null" {
					return nil
				}
			}
		}
		for _, branch := range branches {
			candidate := branch.(map[string]any)
			if candidate["type"] != "null" {
				return v.validate(candidate, value, path)
			}
		}
	}
	typeName := schema["type"].(string)
	if !outputValueMatchesType(value, typeName) {
		return []outputViolation{{Path: path, Rule: fmt.Sprintf("must be %s", typeName), Offending: value, Present: true}}
	}
	var violations []outputViolation
	if expected, present := schema["const"]; present && !equalOutputJSON(value, expected) {
		violations = append(violations, outputViolation{Path: path, Rule: "must equal const", Offending: value, Present: true})
	}
	if choices, present := schema["enum"].([]any); present {
		matched := false
		for _, choice := range choices {
			matched = matched || equalOutputJSON(value, choice)
		}
		if !matched {
			violations = append(violations, outputViolation{Path: path, Rule: "must be one of the enum values", Offending: value, Present: true})
		}
	}
	switch typeName {
	case "object":
		violations = append(violations, v.validateObjectValue(schema, value.(map[string]any), path)...)
	case "array":
		violations = append(violations, v.validateArrayValue(schema, value.([]any), path)...)
	case "string":
		violations = append(violations, validateOutputString(schema, value.(string), path)...)
	case "number", "integer":
		violations = append(violations, validateOutputNumber(schema, value, path)...)
	}
	return violations
}

func (v outputDocumentValidator) validateObjectValue(schema map[string]any, value map[string]any, path string) []outputViolation {
	properties, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	var violations []outputViolation
	for _, item := range required {
		name := item.(string)
		if _, present := value[name]; !present {
			violations = append(violations, outputViolation{Path: outputPropertyPath(path, name), Rule: "is required", Present: false})
		}
	}
	names := make([]string, 0, len(value))
	for name := range value {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		childValue := value[name]
		child, declared := properties[name]
		if !declared {
			violations = append(violations, outputViolation{Path: outputPropertyPath(path, name), Rule: "property must be declared by the schema", Offending: childValue, Present: true})
			continue
		}
		violations = append(violations, v.validate(child.(map[string]any), childValue, outputPropertyPath(path, name))...)
	}
	return violations
}

func (v outputDocumentValidator) validateArrayValue(schema map[string]any, value []any, path string) []outputViolation {
	violations := validateOutputCardinality(schema, value, path)
	if unique, _ := schema["uniqueItems"].(bool); unique {
		for index := range value {
			for prior := range index {
				if equalOutputJSON(value[index], value[prior]) {
					violations = append(violations, outputViolation{Path: fmt.Sprintf("%s[%d]", path, index), Rule: "must be unique within the array", Offending: value[index], Present: true})
					break
				}
			}
		}
	}
	items := schema["items"].(map[string]any)
	for index, item := range value {
		violations = append(violations, v.validate(items, item, fmt.Sprintf("%s[%d]", path, index))...)
	}
	return violations
}

func validateOutputCardinality(schema map[string]any, value []any, path string) []outputViolation {
	var violations []outputViolation
	if minimum, present := outputSchemaInt(schema["minItems"]); present && len(value) < minimum {
		violations = append(violations, outputViolation{Path: path, Rule: fmt.Sprintf("must contain at least %d items", minimum), Offending: value, Present: true})
	}
	if maximum, present := outputSchemaInt(schema["maxItems"]); present && len(value) > maximum {
		violations = append(violations, outputViolation{Path: path, Rule: fmt.Sprintf("must contain at most %d items", maximum), Offending: value, Present: true})
	}
	return violations
}

func validateOutputString(schema map[string]any, value, path string) []outputViolation {
	var violations []outputViolation
	length := utf8.RuneCountInString(value)
	if minimum, present := outputSchemaInt(schema["minLength"]); present && length < minimum {
		violations = append(violations, outputViolation{Path: path, Rule: fmt.Sprintf("length must be at least %d", minimum), Offending: value, Present: true})
	}
	if maximum, present := outputSchemaInt(schema["maxLength"]); present && length > maximum {
		violations = append(violations, outputViolation{Path: path, Rule: fmt.Sprintf("length must be at most %d", maximum), Offending: value, Present: true})
	}
	if pattern, present := schema["pattern"].(string); present && !regexp.MustCompile(pattern).MatchString(value) {
		violations = append(violations, outputViolation{Path: path, Rule: fmt.Sprintf("must match pattern %q", pattern), Offending: value, Present: true})
	}
	if format, present := schema["format"].(string); present && !supportedOutputFormats[format](value) {
		violations = append(violations, outputViolation{Path: path, Rule: fmt.Sprintf("must satisfy format %q", format), Offending: value, Present: true})
	}
	return violations
}

func validateOutputNumber(schema map[string]any, value any, path string) []outputViolation {
	var violations []outputViolation
	number, _ := outputFiniteNumber(value)
	checks := []struct {
		keyword string
		valid   func(int) bool
		text    string
	}{
		{"minimum", func(c int) bool { return c >= 0 }, "must be >="},
		{"maximum", func(c int) bool { return c <= 0 }, "must be <="},
		{"exclusiveMinimum", func(c int) bool { return c > 0 }, "must be >"},
		{"exclusiveMaximum", func(c int) bool { return c < 0 }, "must be <"},
	}
	for _, check := range checks {
		if limitValue, present := schema[check.keyword]; present {
			limit, _ := outputFiniteNumber(limitValue)
			if !check.valid(number.Cmp(limit)) {
				violations = append(violations, outputViolation{Path: path, Rule: check.text + " " + limitValue.(json.Number).String(), Offending: value, Present: true})
			}
		}
	}
	if divisorValue, present := schema["multipleOf"]; present {
		divisor, _ := outputFiniteNumber(divisorValue)
		quotient := new(big.Rat).Quo(number, divisor)
		if !quotient.IsInt() {
			violations = append(violations, outputViolation{Path: path, Rule: "must be a multiple of " + divisorValue.(json.Number).String(), Offending: value, Present: true})
		}
	}
	return violations
}

func equalOutputJSON(left, right any) bool {
	if leftNumber, ok := outputFiniteNumber(left); ok {
		rightNumber, rightOK := outputFiniteNumber(right)
		return rightOK && leftNumber.Cmp(rightNumber) == 0
	}
	switch leftValue := left.(type) {
	case map[string]any:
		rightValue, ok := right.(map[string]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for key, value := range leftValue {
			other, present := rightValue[key]
			if !present || !equalOutputJSON(value, other) {
				return false
			}
		}
		return true
	case []any:
		rightValue, ok := right.([]any)
		if !ok || len(leftValue) != len(rightValue) {
			return false
		}
		for index := range leftValue {
			if !equalOutputJSON(leftValue[index], rightValue[index]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(left, right)
	}
}

func outputSchemaInt(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	integer, err := strconv.Atoi(number.String())
	return integer, err == nil
}

func outputPropertyPath(path, name string) string {
	if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
		return path + "." + name
	}
	encoded, _ := json.Marshal(name)
	return path + "[" + string(encoded) + "]"
}

func validOutputDateTime(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}
func validOutputDate(value string) bool { _, err := time.Parse("2006-01-02", value); return err == nil }
func validOutputTime(value string) bool {
	_, err := time.Parse(time.RFC3339, "2000-01-01T"+value)
	return err == nil
}
func validOutputEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}
func validOutputURI(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.IsAbs()
}
func validOutputUUID(value string) bool {
	matched, _ := regexp.MatchString(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, value)
	return matched
}
func validOutputIPv4(value string) bool {
	address, err := netip.ParseAddr(value)
	return err == nil && address.Is4()
}
func validOutputIPv6(value string) bool {
	address := net.ParseIP(value)
	return address != nil && strings.Contains(value, ":")
}
func validOutputHostname(value string) bool {
	if len(value) == 0 || len(value) > 253 || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !isOutputHostnameCharacter(character) {
				return false
			}
		}
	}
	return true
}

func isOutputHostnameCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '-'
}

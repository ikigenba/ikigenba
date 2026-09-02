package agentkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
)

// ValidateOutputSchema reports whether schema lies within the output subset.
func ValidateOutputSchema(schema json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(schema))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("invalid JSON schema: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid JSON schema: multiple JSON values")
		}
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
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s: %s must be a string", path, keyword)
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

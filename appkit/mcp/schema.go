package mcp

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var propertyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var rawMessageType = reflect.TypeFor[json.RawMessage]()

// Nullable distinguishes an omitted input member from null and a value.
type Nullable[T any] struct {
	Present, Null bool
	Value         T
}

func (Nullable[T]) nullableType() reflect.Type { return reflect.TypeFor[Nullable[T]]() }

type nullable interface {
	nullableType() reflect.Type
}

func nullableElement(t reflect.Type) (reflect.Type, bool) {
	if t.Kind() != reflect.Struct || !t.Implements(reflect.TypeFor[nullable]()) {
		return nil, false
	}
	if reflect.Zero(t).Interface().(nullable).nullableType() != t {
		return nil, false
	}
	return t.Field(2).Type, true
}

type schemaProperty struct {
	name     string
	index    int
	required bool
	schema   *valueSchema
}

type valueSchema struct {
	goType      reflect.Type
	kind        string
	properties  []schemaProperty
	element     *valueSchema
	optional    bool
	nullable    bool
	raw         bool
	values      []string
	description string
}

func deriveSchema(t reflect.Type, output bool) (*valueSchema, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("root must be a struct")
	}
	return deriveValue(t, output, false, make(map[reflect.Type]bool))
}

func hasCustomJSON(t reflect.Type) bool {
	interfaces := []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[json.Unmarshaler](), reflect.TypeFor[encoding.TextMarshaler](), reflect.TypeFor[encoding.TextUnmarshaler]()}
	for _, iface := range interfaces {
		if t.Implements(iface) || reflect.PointerTo(t).Implements(iface) {
			return true
		}
	}
	return false
}

func deriveValue(t reflect.Type, output, field bool, visiting map[reflect.Type]bool) (*valueSchema, error) {
	s := &valueSchema{goType: t}
	if inner, ok := nullableElement(t); ok {
		if output || !field {
			return nil, fmt.Errorf("Nullable must be an input struct field")
		}
		_, nested := nullableElement(inner)
		if inner.Kind() == reflect.Pointer || nested {
			return nil, fmt.Errorf("Nullable cannot wrap a pointer or Nullable")
		}
		element, err := deriveValue(inner, false, true, visiting)
		if err != nil {
			return nil, err
		}
		s.kind, s.nullable, s.element = element.kind, true, element
		return s, nil
	}
	if t == rawMessageType {
		if !field {
			return nil, fmt.Errorf("RawMessage must be a struct field")
		}
		s.kind, s.raw = "object", true
		return s, nil
	}
	if hasCustomJSON(t) {
		return nil, fmt.Errorf("custom JSON or text encoding is unsupported: %v", t)
	}
	if t.Kind() == reflect.Pointer {
		if !field || t.Elem().Kind() == reflect.Pointer {
			return nil, fmt.Errorf("pointer must be a struct field and point to a non-pointer")
		}
		element, err := deriveValue(t.Elem(), output, false, visiting)
		if err != nil {
			return nil, err
		}
		s.kind, s.optional, s.element = element.kind, true, element
		return s, nil
	}
	enumType := reflect.TypeFor[Enumerator]()
	var enumerator Enumerator
	if t.Implements(enumType) {
		enumerator = reflect.Zero(t).Interface().(Enumerator)
	} else if reflect.PointerTo(t).Implements(enumType) {
		enumerator = reflect.New(t).Interface().(Enumerator)
	}
	if enumerator != nil {
		if t.Kind() != reflect.String {
			return nil, fmt.Errorf("Enumerator must be a string type")
		}
		s.values = append([]string(nil), enumerator.Enum()...)
		if len(s.values) == 0 {
			return nil, fmt.Errorf("empty enumeration")
		}
		seen := make(map[string]bool)
		for _, value := range s.values {
			if seen[value] {
				return nil, fmt.Errorf("duplicate enumeration value")
			}
			seen[value] = true
		}
	}
	switch t.Kind() {
	case reflect.Struct:
		if visiting[t] {
			return nil, fmt.Errorf("recursive struct: %v", t)
		}
		visiting[t] = true
		defer delete(visiting, t)
		s.kind = "object"
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous {
				return nil, fmt.Errorf("embedded field: %s", f.Name)
			}
			tag, exists := f.Tag.Lookup("json")
			marker, marked := f.Tag.Lookup("mcp")
			if strings.Contains(tag, ",") {
				return nil, fmt.Errorf("json tag must contain only a name: %s", f.Name)
			}
			property := f.IsExported() && tag != "-"
			if marked && (marker != "required" || !property) {
				return nil, fmt.Errorf("invalid mcp tag: %s", f.Name)
			}
			if !property {
				continue
			}
			name := tag
			if !exists || !propertyPattern.MatchString(name) {
				return nil, fmt.Errorf("invalid property name: %s", name)
			}
			required := marked
			if output && required && f.Type.Kind() == reflect.Pointer {
				return nil, fmt.Errorf("required output pointer: %s", name)
			}
			child, err := deriveValue(f.Type, output, true, visiting)
			if err != nil {
				return nil, err
			}
			child.description = f.Tag.Get("description")
			s.properties = append(s.properties, schemaProperty{name: name, index: i, required: required, schema: child})
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return nil, fmt.Errorf("byte slice is unsupported")
		}
		child, err := deriveValue(t.Elem(), output, false, visiting)
		if err != nil {
			return nil, err
		}
		s.kind, s.element = "array", child
	case reflect.String:
		s.kind = "string"
	case reflect.Bool:
		s.kind = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s.kind = "integer"
	case reflect.Float64:
		s.kind = "number"
	default:
		return nil, fmt.Errorf("unsupported type: %v", t)
	}
	return s, nil
}

func jsonString(s string) json.RawMessage { b, _ := json.Marshal(s); return b }

func (s *valueSchema) json() json.RawMessage {
	if s.nullable {
		members, _ := parseJSONObject(s.element.json())
		for i := range members {
			switch members[i].name {
			case "type":
				members[i].value = json.RawMessage(`[` + string(jsonString(s.kind)) + `,"null"]`)
			case "enum":
				value := members[i].value
				members[i].value = append(append(json.RawMessage(nil), value[:len(value)-1]...), []byte(",null]")...)
			}
		}
		if s.description != "" {
			members = append(members, jsonMember{name: "description", value: jsonString(s.description)})
		}
		b, _ := marshalJSONObject(members)
		return b
	}
	if s.optional {
		copySchema := *s.element
		copySchema.description = s.description
		return copySchema.json()
	}
	members := []jsonMember{{name: "type", value: jsonString(s.kind)}}
	if s.kind == "object" && !s.raw {
		properties := make([]jsonMember, 0, len(s.properties))
		required := make([]string, 0)
		for _, p := range s.properties {
			properties = append(properties, jsonMember{name: p.name, value: p.schema.json()})
			if p.required {
				required = append(required, p.name)
			}
		}
		if len(properties) != 0 {
			b, _ := marshalJSONObject(properties)
			members = append(members, jsonMember{name: "properties", value: b})
		}
		if len(required) != 0 {
			b, _ := json.Marshal(required)
			members = append(members, jsonMember{name: "required", value: b})
		}
		members = append(members, jsonMember{name: "additionalProperties", value: json.RawMessage("false")})
	}
	if s.kind == "array" {
		members = append(members, jsonMember{name: "items", value: s.element.json()})
	}
	if s.values != nil {
		b, _ := json.Marshal(s.values)
		members = append(members, jsonMember{name: "enum", value: b})
	}
	if s.goType.Kind() >= reflect.Uint && s.goType.Kind() <= reflect.Uint64 {
		members = append(members, jsonMember{name: "minimum", value: json.RawMessage("0")})
	}
	if s.description != "" {
		members = append(members, jsonMember{name: "description", value: jsonString(s.description)})
	}
	b, _ := marshalJSONObject(members)
	return b
}

func jsonKind(data []byte) string {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return "null"
	}
	switch data[0] {
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	case '"':
		return "string"
	case '[':
		return "array"
	case '{':
		return "object"
	default:
		return "number"
	}
}

func memberPath(path, name string) string {
	if !propertyPattern.MatchString(name) {
		return path + "[" + string(jsonString(name)) + "]"
	}
	if path == "" {
		return name
	}
	return path + "." + name
}

func (s *valueSchema) decodeArguments(data json.RawMessage) (reflect.Value, string) {
	v, offences := s.decode(data, "")
	if len(offences) != 0 {
		return reflect.Value{}, "invalid arguments:\n" + strings.Join(offences, "\n")
	}
	return v, ""
}

func offence(path, reason string) []string {
	if path == "" {
		path = "arguments"
	}
	return []string{path + ": " + reason}
}

func (s *valueSchema) decode(data json.RawMessage, path string) (reflect.Value, []string) {
	v := reflect.New(s.goType).Elem()
	got := jsonKind(data)
	if s.nullable {
		v.Field(0).SetBool(true)
		if got == "null" {
			v.Field(1).SetBool(true)
			return v, nil
		}
		match := got == s.kind || (s.kind == "integer" && got == "number")
		if !match {
			return v, offence(path, "expected "+s.kind+" or null, got "+got)
		}
		element, errs := s.element.decode(data, path)
		v.Field(2).Set(element)
		return v, errs
	}
	if s.optional {
		if got == "null" {
			return v, nil
		}
		element, errs := s.element.decode(data, path)
		if len(errs) != 0 {
			return v, errs
		}
		v.Set(reflect.New(s.goType.Elem()))
		v.Elem().Set(element)
		return v, nil
	}
	match := got == s.kind || (s.kind == "integer" && got == "number")
	if !match {
		return v, offence(path, "expected "+s.kind+", got "+got)
	}
	if s.raw {
		v.SetBytes(append([]byte(nil), data...))
		return v, nil
	}
	switch s.kind {
	case "object":
		return s.decodeObject(data, path, v)
	case "array":
		var elements []json.RawMessage
		if err := json.Unmarshal(data, &elements); err != nil {
			return v, offence(path, "invalid JSON")
		}
		v.Set(reflect.MakeSlice(s.goType, len(elements), len(elements)))
		var errs []string
		for i, element := range elements {
			child, failures := s.element.decode(element, path+"["+strconv.Itoa(i)+"]")
			v.Index(i).Set(child)
			errs = append(errs, failures...)
		}
		return v, errs
	case "string":
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return v, offence(path, "invalid JSON")
		}
		if s.values != nil && !s.contains(str) {
			values := make([]string, len(s.values))
			for i, value := range s.values {
				values[i] = string(jsonString(value))
			}
			return v, offence(path, "must be one of "+strings.Join(values, ", ")+", got "+string(jsonString(str)))
		}
		v.SetString(str)
	case "boolean":
		v.SetBool(bytes.Equal(bytes.TrimSpace(data), []byte("true")))
	case "integer":
		return s.decodeInteger(data, path, v)
	case "number":
		number := string(bytes.TrimSpace(data))
		f, err := strconv.ParseFloat(number, 64)
		if err != nil || math.IsInf(f, 0) || exceedsFiniteFloat(number, f) {
			return v, offence(path, "out of range for a 64-bit float, got "+number)
		}
		v.SetFloat(f)
	}
	return v, nil
}

func exceedsFiniteFloat(number string, rounded float64) bool {
	if math.Abs(rounded) != math.MaxFloat64 {
		return false
	}
	exact, ok := new(big.Rat).SetString(number)
	if !ok {
		return true
	}
	exact.Abs(exact)
	maximum := new(big.Rat).SetFloat64(math.MaxFloat64)
	return exact.Cmp(maximum) > 0
}

func (s *valueSchema) contains(value string) bool {
	for _, item := range s.values {
		if item == value {
			return true
		}
	}
	return false
}

func (s *valueSchema) decodeObject(data []byte, path string, v reflect.Value) (reflect.Value, []string) {
	members, err := parseJSONObject(data)
	if err != nil {
		return v, offence(path, "invalid JSON")
	}
	byName := make(map[string][]json.RawMessage)
	for _, m := range members {
		byName[m.name] = append(byName[m.name], m.value)
	}
	known := make(map[string]bool)
	var errs []string
	for _, p := range s.properties {
		known[p.name] = true
		values := byName[p.name]
		childPath := memberPath(path, p.name)
		if len(values) == 0 {
			if p.required {
				errs = append(errs, offence(childPath, "missing required field")...)
			}
			continue
		}
		if len(values) > 1 {
			errs = append(errs, offence(childPath, "duplicate field")...)
			continue
		}
		child, failures := p.schema.decode(values[0], childPath)
		v.Field(p.index).Set(child)
		errs = append(errs, failures...)
	}
	for _, m := range members {
		if !known[m.name] {
			errs = append(errs, offence(memberPath(path, m.name), "unknown field")...)
		}
	}
	return v, errs
}

// integerDigits normalizes decimal JSON notation without rounding or allocating
// memory proportional to an exponent supplied by a caller.
func integerDigits(number string) (string, bool) {
	negative := strings.HasPrefix(number, "-")
	mantissa := strings.TrimPrefix(number, "-")
	exponent := int64(0)
	if i := strings.IndexAny(mantissa, "eE"); i >= 0 {
		exp := mantissa[i+1:]
		mantissa = mantissa[:i]
		parsed, err := strconv.ParseInt(exp, 10, 64)
		if err != nil {
			if strings.HasPrefix(exp, "-") {
				exponent = -math.MaxInt64
			} else {
				exponent = math.MaxInt64
			}
		} else {
			exponent = parsed
		}
	}
	fraction := 0
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		fraction = len(mantissa) - i - 1
		mantissa = mantissa[:i] + mantissa[i+1:]
	}
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" {
		return "0", true
	}
	trailing := len(digits) - len(strings.TrimRight(digits, "0"))
	digits = strings.TrimRight(digits, "0")
	// Saturating exponents need only distinguish fractional, bounded and huge.
	if exponent < -int64(len(number)) {
		return "", false
	}
	if exponent > int64(len(number))+20 {
		return "huge", true
	}
	shift := exponent - int64(fraction) + int64(trailing)
	if shift < 0 {
		return "", false
	}
	if int64(len(digits))+shift > 20 {
		return "huge", true
	}
	digits += strings.Repeat("0", int(shift))
	if negative {
		digits = "-" + digits
	}
	return digits, true
}

func (s *valueSchema) decodeInteger(data []byte, path string, v reflect.Value) (reflect.Value, []string) {
	number := string(bytes.TrimSpace(data))
	digits, integral := integerDigits(number)
	if !integral {
		return v, offence(path, "expected integer, got "+number)
	}
	bits := s.goType.Bits()
	unsigned := s.goType.Kind() >= reflect.Uint && s.goType.Kind() <= reflect.Uint64
	maximum := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	minimum := new(big.Int)
	if !unsigned {
		maximum.Rsh(maximum, 1)
		minimum.Neg(new(big.Int).Set(maximum))
	}
	maximum.Sub(maximum, big.NewInt(1))
	integer, ok := new(big.Int).SetString(digits, 10)
	if !ok || integer.Cmp(minimum) < 0 || integer.Cmp(maximum) > 0 {
		return v, offence(path, "must be between "+minimum.String()+" and "+maximum.String()+", got "+number)
	}
	if unsigned {
		v.SetUint(integer.Uint64())
	} else {
		v.SetInt(integer.Int64())
	}
	return v, nil
}

func (s *valueSchema) encode(v reflect.Value) ([]byte, error) {
	if s.optional {
		return s.element.encode(v.Elem())
	}
	if s.raw {
		data := v.Bytes()
		if _, err := parseJSONObject(data); err != nil {
			return nil, fmt.Errorf("RawMessage must contain exactly one object")
		}
		return append([]byte(nil), data...), nil
	}
	switch s.kind {
	case "object":
		members := make([]jsonMember, 0, len(s.properties))
		for _, p := range s.properties {
			field := v.Field(p.index)
			if p.schema.optional && field.IsNil() {
				continue
			}
			if p.schema.raw && field.IsNil() && !p.required {
				continue
			}
			b, err := p.schema.encode(field)
			if err != nil {
				return nil, err
			}
			members = append(members, jsonMember{name: p.name, value: b})
		}
		return marshalJSONObject(members)
	case "array":
		var b bytes.Buffer
		b.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i != 0 {
				b.WriteByte(',')
			}
			element, err := s.element.encode(v.Index(i))
			if err != nil {
				return nil, err
			}
			b.Write(element)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	case "string":
		if s.values != nil && !s.contains(v.String()) {
			return nil, fmt.Errorf("value outside enumeration")
		}
		return json.Marshal(v.String())
	case "boolean":
		return json.Marshal(v.Bool())
	case "integer":
		if v.Kind() >= reflect.Uint && v.Kind() <= reflect.Uint64 {
			return []byte(strconv.FormatUint(v.Uint(), 10)), nil
		}
		return []byte(strconv.FormatInt(v.Int(), 10)), nil
	case "number":
		if math.IsNaN(v.Float()) || math.IsInf(v.Float(), 0) {
			return nil, fmt.Errorf("non-finite float")
		}
		return json.Marshal(v.Float())
	default:
		return nil, fmt.Errorf("unsupported schema")
	}
}

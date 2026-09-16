package apps

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type tomlKind uint8

const (
	tomlOther tomlKind = iota
	tomlString
	tomlInteger
	tomlFloat
	tomlBoolean
	tomlOffsetDateTime
	tomlLocalDateTime
	tomlLocalDate
	tomlLocalTime
	tomlArray
	tomlTable
	tomlImplicitTable
)

type tomlValue struct {
	kind    tomlKind
	text    string
	integer int64
	boolean bool
	array   []tomlValue
	table   map[string]tomlValue
}

type manifestDecoder struct {
	data     []byte
	position int
	table    []string
	seen     map[string]struct{}
	tables   map[string]tomlKind
	result   Manifest
}

// ParseManifest decodes and validates an app manifest.
func ParseManifest(data []byte) (Manifest, error) {
	if !utf8.Valid(data) {
		return Manifest{}, fmt.Errorf("invalid manifest: input is not UTF-8")
	}
	if err := validateLineEndings(data); err != nil {
		return Manifest{}, fmt.Errorf("invalid manifest: %w", err)
	}
	return newManifestDecoder(data).manifest()
}

func newManifestDecoder(data []byte) *manifestDecoder {
	return &manifestDecoder{
		data:   data,
		seen:   make(map[string]struct{}),
		tables: make(map[string]tomlKind),
		result: Manifest{
			Secrets: []string{},
			Env:     map[string]string{},
		},
	}
}

func (decoder *manifestDecoder) manifest() (Manifest, error) {
	if err := decoder.decode(); err != nil {
		return Manifest{}, fmt.Errorf("invalid manifest: %w", err)
	}
	if decoder.result.Database != nil {
		if err := validateDatabase(decoder.result.Database); err != nil {
			return Manifest{}, fmt.Errorf("invalid manifest: %w", err)
		}
	}
	return decoder.result, nil
}

func (decoder *manifestDecoder) decode() error {
	for {
		if err := decoder.skipDocumentSpace(); err != nil {
			return err
		}
		if decoder.position == len(decoder.data) {
			return nil
		}
		if decoder.data[decoder.position] == '[' {
			if err := decoder.decodeTable(); err != nil {
				return err
			}
			continue
		}
		if err := decoder.decodeAssignment(); err != nil {
			return err
		}
	}
}

func (decoder *manifestDecoder) decodeTable() error {
	decoder.position++
	arrayTable := decoder.consume('[')
	decoder.skipHorizontalSpace()
	keys, err := decoder.decodeKeyPath()
	if err != nil {
		return err
	}
	decoder.skipHorizontalSpace()
	if !decoder.consume(']') || arrayTable && !decoder.consume(']') {
		return decoder.errorf("unterminated table header")
	}
	if err := decoder.finishLine(); err != nil {
		return err
	}
	if len(keys) > 0 {
		if isRecognizedScalarRoot(keys[0]) {
			return decoder.scalarRootTypeError(keys[0])
		}
		switch keys[0] {
		case "env":
			if arrayTable {
				return decoder.errorf("env must be a table")
			}
			if len(keys) > 1 {
				return decoder.errorf("env key %q must be a string", keys[1])
			}
		case "database":
			if arrayTable {
				return decoder.errorf("database must be a table")
			}
		}
	}
	identity := keyIdentity(keys)
	if kind, exists := decoder.tables[identity]; exists {
		if arrayTable && kind != tomlArray || !arrayTable && kind != tomlImplicitTable {
			return decoder.errorf("duplicate table %q", strings.Join(keys, "."))
		}
	}
	if _, exists := decoder.seen[identity]; exists || decoder.pathHasValuePrefix(keys) {
		return decoder.errorf("table %q conflicts with a value", strings.Join(keys, "."))
	}
	for length := 1; length < len(keys); length++ {
		prefix := keyIdentity(keys[:length])
		if _, exists := decoder.tables[prefix]; !exists {
			decoder.tables[prefix] = tomlImplicitTable
		}
	}
	if arrayTable {
		decoder.tables[identity] = tomlArray
		decoder.forgetArrayElement(keys)
	} else {
		decoder.tables[identity] = tomlTable
	}
	if len(keys) > 0 && keys[0] == "database" && decoder.result.Database == nil {
		decoder.result.Database = &Database{}
	}
	decoder.table = keys
	return nil
}

func (decoder *manifestDecoder) decodeAssignment() error {
	keys, err := decoder.decodeKeyPath()
	if err != nil {
		return err
	}
	decoder.skipHorizontalSpace()
	if !decoder.consume('=') {
		return decoder.errorf("expected '='")
	}
	decoder.skipHorizontalSpace()
	value, err := decoder.decodeValue(false)
	if err != nil {
		return err
	}
	if err := decoder.finishLine(); err != nil {
		return err
	}

	fullPath := append(append([]string{}, decoder.table...), keys...)
	identity := keyIdentity(fullPath)
	if _, duplicate := decoder.seen[identity]; duplicate {
		return decoder.errorf("duplicate key %q", strings.Join(fullPath, "."))
	}
	if _, declaredTable := decoder.tables[identity]; declaredTable {
		return decoder.errorf("key %q conflicts with an existing table", strings.Join(fullPath, "."))
	}
	if decoder.pathHasValuePrefix(fullPath) || decoder.pathHasDescendant(fullPath) {
		return decoder.errorf("key %q conflicts with an existing value", strings.Join(fullPath, "."))
	}
	for length := len(decoder.table) + 1; length < len(fullPath); length++ {
		kind, exists := decoder.tables[keyIdentity(fullPath[:length])]
		if exists && (kind == tomlTable || kind == tomlArray) {
			return decoder.errorf("dotted key %q extends an explicitly declared table", strings.Join(fullPath[:length], "."))
		}
	}
	decoder.seen[identity] = struct{}{}
	for length := len(decoder.table) + 1; length < len(fullPath); length++ {
		prefix := keyIdentity(fullPath[:length])
		if _, exists := decoder.tables[prefix]; !exists {
			decoder.tables[prefix] = tomlOther
		}
	}
	return decoder.apply(fullPath, value)
}

func (decoder *manifestDecoder) apply(path []string, value tomlValue) error {
	if len(path) > 1 {
		if isRecognizedScalarRoot(path[0]) {
			return decoder.scalarRootTypeError(path[0])
		}
		if path[0] == "database" {
			if decoder.result.Database == nil {
				decoder.result.Database = &Database{}
			}
		}
	}
	if len(path) > 2 && path[0] == "env" {
		return decoder.errorf("env key %q must be a direct string value", strings.Join(path[1:], "."))
	}
	if len(path) == 1 {
		switch path[0] {
		case "app":
			if value.kind != tomlString {
				return decoder.errorf("app must be a string")
			}
			if err := ValidateName(value.text); err != nil {
				return err
			}
			decoder.result.App = value.text
		case "port":
			if value.kind != tomlInteger || value.integer < 1 || value.integer > 65535 {
				return decoder.errorf("port must be an integer from 1 through 65535")
			}
			decoder.result.Port = int(value.integer)
		case "default":
			if value.kind != tomlBoolean {
				return decoder.errorf("default must be a Boolean")
			}
			decoder.result.Default = value.boolean
		case "secrets":
			if value.kind != tomlArray {
				return decoder.errorf("secrets must be an array of strings")
			}
			secrets := make([]string, len(value.array))
			for index, item := range value.array {
				if item.kind != tomlString {
					return decoder.errorf("secrets must be an array of strings")
				}
				secrets[index] = item.text
			}
			decoder.result.Secrets = secrets
		case "env":
			if value.kind != tomlTable {
				return decoder.errorf("env must be a table")
			}
			for key, item := range value.table {
				if item.kind != tomlString {
					return decoder.errorf("env key %q must be a string", key)
				}
				decoder.result.Env[key] = item.text
			}
		case "database":
			if value.kind != tomlTable {
				return decoder.errorf("database must be a table")
			}
			decoder.result.Database = &Database{}
			for key, item := range value.table {
				if err := decoder.apply([]string{"database", key}, item); err != nil {
					return err
				}
			}
		}
	}
	if len(path) == 2 && path[0] == "env" {
		if value.kind != tomlString {
			return decoder.errorf("env key %q must be a string", path[1])
		}
		decoder.result.Env[path[1]] = value.text
	}
	if len(path) == 2 && path[0] == "database" {
		switch path[1] {
		case "engine":
			if value.kind != tomlString {
				return decoder.errorf("database.engine must be a string")
			}
			decoder.result.Database.Engine = value.text
		case "path":
			if value.kind != tomlString {
				return decoder.errorf("database.path must be a string")
			}
			decoder.result.Database.Path = value.text
		}
	}
	return nil
}

func (decoder *manifestDecoder) decodeValue(inArray bool) (tomlValue, error) {
	if decoder.position == len(decoder.data) {
		return tomlValue{}, decoder.errorf("missing value")
	}
	switch decoder.data[decoder.position] {
	case '"', '\'':
		text, err := decoder.decodeString()
		return tomlValue{kind: tomlString, text: text}, err
	case '[':
		return decoder.decodeArray()
	case '{':
		return decoder.decodeInlineTable()
	}
	return decoder.decodeBareValue(inArray)
}

func (decoder *manifestDecoder) decodeBareValue(inArray bool) (tomlValue, error) {
	start := decoder.position
	for decoder.position < len(decoder.data) {
		current := decoder.data[decoder.position]
		if current == '#' || current == '\n' || current == '\r' || inArray && (current == ',' || current == ']' || current == '}') {
			break
		}
		decoder.position++
	}
	token := strings.TrimRight(string(decoder.data[start:decoder.position]), " \t")
	if token == "" {
		return tomlValue{}, decoder.errorf("missing value")
	}
	if token == "true" || token == "false" {
		return tomlValue{kind: tomlBoolean, boolean: token == "true"}, nil
	}
	if integer, ok := parseTOMLInteger(token); ok {
		return tomlValue{kind: tomlInteger, integer: integer}, nil
	}
	if kind, valid := otherScalarKind(token); valid {
		return tomlValue{kind: kind}, nil
	}
	return tomlValue{}, decoder.errorf("invalid value %q", token)
}

func (decoder *manifestDecoder) decodeArray() (tomlValue, error) {
	decoder.position++
	items := []tomlValue{}
	for {
		if err := decoder.skipArraySpace(); err != nil {
			return tomlValue{}, err
		}
		if decoder.consume(']') {
			return tomlValue{kind: tomlArray, array: items}, nil
		}
		item, err := decoder.decodeValue(true)
		if err != nil {
			return tomlValue{}, err
		}
		items = append(items, item)
		if err := decoder.skipArraySpace(); err != nil {
			return tomlValue{}, err
		}
		if decoder.consume(']') {
			return tomlValue{kind: tomlArray, array: items}, nil
		}
		if !decoder.consume(',') {
			return tomlValue{}, decoder.errorf("expected ',' or ']' in array")
		}
	}
}

func (decoder *manifestDecoder) decodeInlineTable() (tomlValue, error) {
	decoder.position++
	decoder.skipHorizontalSpace()
	if decoder.consume('}') {
		return tomlValue{kind: tomlTable, table: map[string]tomlValue{}}, nil
	}
	values := make(map[string]tomlValue)
	defined := make(map[string]struct{})
	for {
		keys, err := decoder.decodeKeyPath()
		if err != nil {
			return tomlValue{}, err
		}
		decoder.skipHorizontalSpace()
		if !decoder.consume('=') {
			return tomlValue{}, decoder.errorf("expected '=' in inline table")
		}
		decoder.skipHorizontalSpace()
		value, err := decoder.decodeValue(true)
		if err != nil {
			return tomlValue{}, err
		}
		for length := 1; length < len(keys); length++ {
			if _, exists := defined[keyIdentity(keys[:length])]; exists {
				return tomlValue{}, decoder.errorf("inline table key %q extends an explicitly defined value", strings.Join(keys[:length], "."))
			}
		}
		if err := addInlineValue(values, keys, value); err != nil {
			return tomlValue{}, decoder.errorf("%v", err)
		}
		defined[keyIdentity(keys)] = struct{}{}
		decoder.skipHorizontalSpace()
		if decoder.consume('}') {
			return tomlValue{kind: tomlTable, table: values}, nil
		}
		if !decoder.consume(',') {
			return tomlValue{}, decoder.errorf("expected ',' or '}' in inline table")
		}
		decoder.skipHorizontalSpace()
	}
}

func (decoder *manifestDecoder) decodeKeyPath() ([]string, error) {
	keys := []string{}
	for {
		decoder.skipHorizontalSpace()
		key, err := decoder.decodeKey()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		decoder.skipHorizontalSpace()
		if !decoder.consume('.') {
			return keys, nil
		}
	}
}

func (decoder *manifestDecoder) decodeKey() (string, error) {
	if decoder.position == len(decoder.data) {
		return "", decoder.errorf("missing key")
	}
	if decoder.data[decoder.position] == '"' || decoder.data[decoder.position] == '\'' {
		if decoder.position+2 < len(decoder.data) && decoder.data[decoder.position+1] == decoder.data[decoder.position] && decoder.data[decoder.position+2] == decoder.data[decoder.position] {
			return "", decoder.errorf("multiline string cannot be a key")
		}
		return decoder.decodeString()
	}
	start := decoder.position
	for decoder.position < len(decoder.data) && isBareKeyByte(decoder.data[decoder.position]) {
		decoder.position++
	}
	if start == decoder.position {
		return "", decoder.errorf("invalid key")
	}
	return string(decoder.data[start:decoder.position]), nil
}

func (decoder *manifestDecoder) decodeString() (string, error) {
	quote := decoder.data[decoder.position]
	decoder.position++
	multiline := decoder.position+1 < len(decoder.data) && decoder.data[decoder.position] == quote && decoder.data[decoder.position+1] == quote
	if multiline {
		decoder.position += 2
		decoder.consumeNewline()
	}
	var result strings.Builder
	for decoder.position < len(decoder.data) {
		current := decoder.data[decoder.position]
		if current == quote {
			if !multiline {
				decoder.position++
				return result.String(), nil
			}
			quoteCount := 1
			for decoder.position+quoteCount < len(decoder.data) && decoder.data[decoder.position+quoteCount] == quote {
				quoteCount++
			}
			if quoteCount >= 3 {
				if quoteCount > 5 {
					return "", decoder.errorf("too many consecutive quote characters")
				}
				result.WriteString(strings.Repeat(string(quote), quoteCount-3))
				decoder.position += quoteCount
				return result.String(), nil
			}
			result.WriteString(strings.Repeat(string(quote), quoteCount))
			decoder.position += quoteCount
			continue
		}
		if !multiline && (current == '\n' || current == '\r') {
			return "", decoder.errorf("unterminated string")
		}
		if current == '\n' || current == '\r' {
			decoder.consumeNewline()
			result.WriteByte('\n')
			continue
		}
		decoder.position++
		if isForbiddenTOMLControl(current) {
			return "", decoder.errorf("control character in string")
		}
		if quote == '\'' || current != '\\' {
			result.WriteByte(current)
			continue
		}
		continuation := decoder.position
		for continuation < len(decoder.data) && (decoder.data[continuation] == ' ' || decoder.data[continuation] == '\t') {
			continuation++
		}
		if multiline && continuation < len(decoder.data) && (decoder.data[continuation] == '\n' || decoder.data[continuation] == '\r') {
			decoder.position = continuation
			decoder.consumeNewline()
			for decoder.position < len(decoder.data) {
				switch decoder.data[decoder.position] {
				case ' ', '\t':
					decoder.position++
				case '\n', '\r':
					decoder.consumeNewline()
				default:
					goto continuedString
				}
			}
		continuedString:
			continue
		}
		if decoder.position == len(decoder.data) {
			return "", decoder.errorf("unterminated escape")
		}
		escape := decoder.data[decoder.position]
		decoder.position++
		switch escape {
		case 'b':
			result.WriteByte('\b')
		case 't':
			result.WriteByte('\t')
		case 'n':
			result.WriteByte('\n')
		case 'f':
			result.WriteByte('\f')
		case 'r':
			result.WriteByte('\r')
		case '"', '\\':
			result.WriteByte(escape)
		case 'u', 'U':
			digits := 4
			if escape == 'U' {
				digits = 8
			}
			if decoder.position+digits > len(decoder.data) {
				return "", decoder.errorf("invalid Unicode escape")
			}
			value, err := strconv.ParseUint(string(decoder.data[decoder.position:decoder.position+digits]), 16, 32)
			if err != nil || value > utf8.MaxRune {
				return "", decoder.errorf("invalid Unicode escape")
			}
			character := rune(value)
			if !utf8.ValidRune(character) {
				return "", decoder.errorf("invalid Unicode escape")
			}
			result.WriteRune(character)
			decoder.position += digits
		default:
			return "", decoder.errorf("invalid escape %q", escape)
		}
	}
	return "", decoder.errorf("unterminated string")
}

func (decoder *manifestDecoder) finishLine() error {
	decoder.skipHorizontalSpace()
	if decoder.position < len(decoder.data) && decoder.data[decoder.position] == '#' {
		if err := decoder.skipComment(); err != nil {
			return err
		}
	}
	if decoder.position == len(decoder.data) {
		return nil
	}
	if !decoder.consumeNewline() {
		return decoder.errorf("unexpected content after value")
	}
	return nil
}

func (decoder *manifestDecoder) skipDocumentSpace() error {
	for decoder.position < len(decoder.data) {
		switch decoder.data[decoder.position] {
		case ' ', '\t':
			decoder.position++
		case '\n', '\r':
			decoder.consumeNewline()
		case '#':
			if err := decoder.skipComment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

func (decoder *manifestDecoder) skipHorizontalSpace() {
	for decoder.position < len(decoder.data) && (decoder.data[decoder.position] == ' ' || decoder.data[decoder.position] == '\t') {
		decoder.position++
	}
}

func (decoder *manifestDecoder) skipArraySpace() error {
	for decoder.position < len(decoder.data) {
		switch decoder.data[decoder.position] {
		case ' ', '\t':
			decoder.position++
		case '\n', '\r':
			decoder.consumeNewline()
		case '#':
			if err := decoder.skipComment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

func (decoder *manifestDecoder) skipComment() error {
	for decoder.position < len(decoder.data) {
		character := decoder.data[decoder.position]
		if character == '\n' || character == '\r' {
			return nil
		}
		if isForbiddenTOMLControl(character) {
			return decoder.errorf("control character in comment")
		}
		decoder.position++
	}
	return nil
}

func (decoder *manifestDecoder) consume(want byte) bool {
	if decoder.position < len(decoder.data) && decoder.data[decoder.position] == want {
		decoder.position++
		return true
	}
	return false
}

func (decoder *manifestDecoder) consumeNewline() bool {
	if decoder.consume('\n') {
		return true
	}
	if !decoder.consume('\r') {
		return false
	}
	decoder.consume('\n')
	return true
}

func (decoder *manifestDecoder) errorf(format string, arguments ...any) error {
	line := 1 + strings.Count(string(decoder.data[:decoder.position]), "\n")
	return fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, arguments...))
}

func (decoder *manifestDecoder) scalarRootTypeError(root string) error {
	switch root {
	case "app":
		return decoder.errorf("app must be a string")
	case "port":
		return decoder.errorf("port must be an integer from 1 through 65535")
	case "default":
		return decoder.errorf("default must be a Boolean")
	case "secrets":
		return decoder.errorf("secrets must be an array of strings")
	default:
		return decoder.errorf("unrecognized scalar root %q", root)
	}
}

func isRecognizedScalarRoot(root string) bool {
	switch root {
	case "app", "port", "default", "secrets":
		return true
	default:
		return false
	}
}

func isForbiddenTOMLControl(character byte) bool {
	return character < 0x20 && character != '\t' || character == 0x7f
}

func parseTOMLInteger(token string) (int64, bool) {
	if !tomlIntegerPattern.MatchString(token) {
		return 0, false
	}
	clean := strings.ReplaceAll(token, "_", "")
	base := 10
	digits := clean
	sign := ""
	if strings.HasPrefix(digits, "+") || strings.HasPrefix(digits, "-") {
		sign, digits = digits[:1], digits[1:]
	}
	if sign == "" && len(digits) > 2 && digits[0] == '0' {
		switch digits[1] {
		case 'x':
			base = 16
		case 'o':
			base = 8
		case 'b':
			base = 2
		}
		if base != 10 {
			digits = digits[2:]
		}
	}
	if digits == "" || base == 10 && len(digits) > 1 && digits[0] == '0' {
		return 0, false
	}
	value, err := strconv.ParseInt(sign+digits, base, 64)
	return value, err == nil
}

func otherScalarKind(token string) (tomlKind, bool) {
	if token == "inf" || token == "+inf" || token == "-inf" || token == "nan" || token == "+nan" || token == "-nan" {
		return tomlFloat, true
	}
	if tomlFloatPattern.MatchString(token) {
		return tomlFloat, true
	}
	if strings.Contains(token, ",") {
		return tomlOther, false
	}
	for index, layout := range tomlTimeLayouts {
		if !tomlTimePatterns[index].MatchString(token) {
			continue
		}
		normalizedTime := normalizeTOMLDateTime(token)
		if _, err := time.Parse(layout, normalizedTime); err == nil {
			return tomlTimeKinds[index], true
		}
	}
	return tomlOther, false
}

func normalizeTOMLDateTime(token string) string {
	var normalized []byte
	if len(token) > len("2006-01-02") && token[len("2006-01-02")] == 't' {
		normalized = []byte(token)
		normalized[len("2006-01-02")] = 'T'
	}
	if token[len(token)-1] == 'z' {
		if normalized == nil {
			normalized = []byte(token)
		}
		normalized[len(normalized)-1] = 'Z'
	}
	if normalized != nil {
		return string(normalized)
	}
	return token
}

var (
	tomlIntegerPattern = regexp.MustCompile(`^[+-]?(?:0|[1-9](?:_?[0-9])*)$|^0x[0-9A-Fa-f](?:_?[0-9A-Fa-f])*$|^0o[0-7](?:_?[0-7])*$|^0b[01](?:_?[01])*$`)
	tomlFloatPattern   = regexp.MustCompile(`^[+-]?(?:(?:0|[1-9](?:_?[0-9])*)\.[0-9](?:_?[0-9])*(?:[eE][+-]?[0-9](?:_?[0-9])*)?|(?:0|[1-9](?:_?[0-9])*)[eE][+-]?[0-9](?:_?[0-9])*)$`)
	tomlTimePatterns   = []*regexp.Regexp{
		regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:[Zz]|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])$`),
		regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:[Zz]|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])$`),
		regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?$`),
		regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?$`),
		regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`),
		regexp.MustCompile(`^[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?$`),
	}
	tomlTimeLayouts = []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02",
		"15:04:05.999999999",
	}
	tomlTimeKinds = []tomlKind{
		tomlOffsetDateTime,
		tomlOffsetDateTime,
		tomlLocalDateTime,
		tomlLocalDateTime,
		tomlLocalDate,
		tomlLocalTime,
	}
)

func keyIdentity(path []string) string {
	var identity strings.Builder
	for _, component := range path {
		identity.WriteString(strconv.Itoa(len(component)))
		identity.WriteByte(':')
		identity.WriteString(component)
	}
	return identity.String()
}

func (decoder *manifestDecoder) pathHasValuePrefix(path []string) bool {
	for length := 1; length < len(path); length++ {
		if _, exists := decoder.seen[keyIdentity(path[:length])]; exists {
			return true
		}
	}
	return false
}

func (decoder *manifestDecoder) pathHasDescendant(path []string) bool {
	prefix := keyIdentity(path)
	for identity := range decoder.seen {
		if len(identity) > len(prefix) && strings.HasPrefix(identity, prefix) {
			return true
		}
	}
	return false
}

func (decoder *manifestDecoder) forgetArrayElement(path []string) {
	prefix := keyIdentity(path)
	for identity := range decoder.seen {
		if len(identity) > len(prefix) && strings.HasPrefix(identity, prefix) {
			delete(decoder.seen, identity)
		}
	}
	for identity := range decoder.tables {
		if len(identity) > len(prefix) && strings.HasPrefix(identity, prefix) {
			delete(decoder.tables, identity)
		}
	}
}

func validateLineEndings(data []byte) error {
	for position, character := range data {
		if character == '\r' && (position+1 == len(data) || data[position+1] != '\n') {
			return fmt.Errorf("bare carriage return")
		}
	}
	return nil
}

func addInlineValue(table map[string]tomlValue, path []string, value tomlValue) error {
	key := path[0]
	if len(path) == 1 {
		if _, exists := table[key]; exists {
			return fmt.Errorf("duplicate inline table key %q", key)
		}
		table[key] = value
		return nil
	}

	child, exists := table[key]
	if exists && child.kind != tomlTable {
		return fmt.Errorf("inline table key %q conflicts with a value", key)
	}
	if !exists {
		child = tomlValue{kind: tomlTable, table: make(map[string]tomlValue)}
	}
	if err := addInlineValue(child.table, path[1:], value); err != nil {
		return err
	}
	table[key] = child
	return nil
}

func isBareKeyByte(character byte) bool {
	return isASCIIAlphanumeric(character) || character == '_' || character == '-'
}

func validateDatabase(database *Database) error {
	if database.Engine != "sqlite" {
		return fmt.Errorf("database.engine must be %q", "sqlite")
	}
	components := strings.Split(database.Path, "/")
	if len(components) < 2 || components[0] != "state" {
		return fmt.Errorf("database.path must name a file below state/")
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.ContainsRune(component, '\x00') {
			return fmt.Errorf("database.path must name a file below state/")
		}
	}
	return nil
}

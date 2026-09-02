package agentkit

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type outputSchemaContractFixture struct {
	Name string `json:"name"`
}

type outputSchemaNestedFixture struct {
	Code string `json:"code"`
	Rank int    `json:"rank,omitempty"`
}

type outputSchemaPrimitivesFixture struct {
	String  string
	Bool    bool
	Int     int
	Int8    int8
	Int16   int16
	Int32   int32
	Int64   int64
	Uint    uint
	Uint8   uint8
	Uint16  uint16
	Uint32  uint32
	Uint64  uint64
	Float32 float32
	Float64 float64
}

type outputSchemaDerivationFixture struct {
	Text       string                        `json:"label" jsonschema:"required,enum=red|green,description=display color,minLength=1,maxLength=10,pattern=^[a-z]+$,format=hostname"`
	Enabled    bool                          `jsonschema:"enum=true|false"`
	Signed     int8                          `json:"signed,omitempty" jsonschema:"enum=-2|3,minimum=-2,maximum=3,exclusiveMinimum=-3,exclusiveMaximum=4,multipleOf=1"`
	Unsigned   uint64                        `json:"unsigned"`
	Fraction   float32                       `json:"fraction" jsonschema:"minimum=0.5,maximum=9.5,exclusiveMinimum=0,exclusiveMaximum=10,multipleOf=0.5"`
	Nested     outputSchemaNestedFixture     `json:"nested"`
	Rows       []outputSchemaNestedFixture   `json:"rows" jsonschema:"minItems=1,maxItems=4,uniqueItems=true"`
	Pair       [2]bool                       `json:"pair"`
	Primitives outputSchemaPrimitivesFixture `json:"primitives"`
	Optional   *string                       `json:"optional,omitempty" jsonschema:"enum=yes|no,description=nullable choice,minLength=2,maxLength=3,pattern=^[a-z]+$,format=hostname"`
	Deep       **outputSchemaNestedFixture   `json:"deep"`
	Skipped    string                        `json:"-"`
	unexported string                        //nolint:unused // Reflection is the behavior under test.
}

type outputSchemaRecursiveFixture struct {
	Next *outputSchemaRecursiveFixture `json:"next"`
}

func TestOutputSchemaHasExactPublicSignature(t *testing.T) {
	// R-TL71-NKE8
	// The explicit conversion proves the exact public function type.
	derive := (func() (json.RawMessage, error))(OutputSchema[outputSchemaContractFixture]) //nolint:unconvert
	schema, err := derive()
	if err != nil {
		t.Fatalf("OutputSchema returned error: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(schema, &document); err != nil {
		t.Fatalf("OutputSchema returned invalid JSON: %v", err)
	}
	if document["type"] != "object" {
		t.Fatalf("root type = %v, want object", document["type"])
	}
	if err := ValidateOutputSchema(schema); err != nil {
		t.Fatalf("OutputSchema returned unusable schema: %v", err)
	}
}

func TestOutputSchemaDerivesOutputSubset(t *testing.T) {
	// R-TUY8-PQBS
	schema, err := OutputSchema[outputSchemaDerivationFixture]()
	if err != nil {
		t.Fatalf("OutputSchema returned error: %v", err)
	}
	if err := ValidateOutputSchema(schema); err != nil {
		t.Fatalf("ValidateOutputSchema rejected derived schema: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(schema, &root); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	properties := root["properties"].(map[string]any)
	wantRootNames := []string{"Enabled", "deep", "fraction", "label", "nested", "optional", "pair", "primitives", "rows", "signed", "unsigned"}
	assertOutputRequired(t, root, wantRootNames)
	if root["additionalProperties"] != false {
		t.Errorf("root additionalProperties = %v, want false", root["additionalProperties"])
	}
	if _, present := properties["Skipped"]; present {
		t.Error("json-skipped field was emitted")
	}
	if _, present := properties["unexported"]; present {
		t.Error("unexported field was emitted")
	}

	assertOutputValues(t, properties["label"].(map[string]any), map[string]any{
		"type": "string", "enum": []any{"red", "green"}, "description": "display color",
		"minLength": float64(1), "maxLength": float64(10), "pattern": "^[a-z]+$", "format": "hostname",
	})
	assertOutputValues(t, properties["Enabled"].(map[string]any), map[string]any{
		"type": "boolean", "enum": []any{true, false},
	})
	assertOutputValues(t, properties["signed"].(map[string]any), map[string]any{
		"type": "integer", "enum": []any{float64(-2), float64(3)}, "minimum": float64(-2), "maximum": float64(3),
		"exclusiveMinimum": float64(-3), "exclusiveMaximum": float64(4), "multipleOf": float64(1),
	})
	assertOutputValues(t, properties["unsigned"].(map[string]any), map[string]any{"type": "integer"})
	assertOutputValues(t, properties["fraction"].(map[string]any), map[string]any{
		"type": "number", "minimum": 0.5, "maximum": 9.5, "exclusiveMinimum": float64(0),
		"exclusiveMaximum": float64(10), "multipleOf": 0.5,
	})

	nested := properties["nested"].(map[string]any)
	assertOutputRequired(t, nested, []string{"code", "rank"})
	rows := properties["rows"].(map[string]any)
	assertOutputValues(t, rows, map[string]any{"type": "array", "minItems": float64(1), "maxItems": float64(4), "uniqueItems": true})
	assertOutputRequired(t, rows["items"].(map[string]any), []string{"code", "rank"})
	if got := properties["pair"].(map[string]any)["items"].(map[string]any)["type"]; got != "boolean" {
		t.Errorf("array item type = %v, want boolean", got)
	}
	primitiveProperties := properties["primitives"].(map[string]any)["properties"].(map[string]any)
	wantPrimitiveTypes := map[string]string{
		"String": "string", "Bool": "boolean", "Int": "integer", "Int8": "integer",
		"Int16": "integer", "Int32": "integer", "Int64": "integer", "Uint": "integer",
		"Uint8": "integer", "Uint16": "integer", "Uint32": "integer", "Uint64": "integer",
		"Float32": "number", "Float64": "number",
	}
	for name, wantType := range wantPrimitiveTypes {
		if got := primitiveProperties[name].(map[string]any)["type"]; got != wantType {
			t.Errorf("primitive %s type = %v, want %s", name, got, wantType)
		}
	}
	primitiveNames := make([]string, 0, len(wantPrimitiveTypes))
	for name := range wantPrimitiveTypes {
		primitiveNames = append(primitiveNames, name)
	}
	assertOutputRequired(t, properties["primitives"].(map[string]any), primitiveNames)

	optional := properties["optional"].(map[string]any)
	branches := optional["anyOf"].([]any)
	if len(branches) != 2 || !reflect.DeepEqual(branches[1], map[string]any{"type": "null"}) {
		t.Fatalf("optional anyOf = %#v, want ordinary schema plus null", branches)
	}
	assertOutputValues(t, branches[0].(map[string]any), map[string]any{
		"type": "string", "enum": []any{"yes", "no"}, "description": "nullable choice",
		"minLength": float64(2), "maxLength": float64(3), "pattern": "^[a-z]+$", "format": "hostname",
	})
	deepBranches := properties["deep"].(map[string]any)["anyOf"].([]any)
	assertOutputRequired(t, deepBranches[0].(map[string]any), []string{"code", "rank"})

	failures := []struct {
		name string
		call func() error
		want string
	}{
		{"unknown tag", outputSchemaError[struct {
			Value string `jsonschema:"mystery=x"`
		}], "unknown option"},
		{"empty tag option", outputSchemaError[struct {
			Value string `jsonschema:","`
		}], "empty option"},
		{"duplicate tag option", outputSchemaError[struct {
			Value string `jsonschema:"format=a,format=b"`
		}], "duplicate option"},
		{"malformed enum", outputSchemaError[struct {
			Value int `jsonschema:"enum=word"`
		}], "enum value"},
		{"non-finite number", outputSchemaError[struct {
			Value float64 `jsonschema:"maximum=NaN"`
		}], "finite number"},
		{"negative length", outputSchemaError[struct {
			Value string `jsonschema:"minLength=-1"`
		}], "non-negative integer"},
		{"malformed uniqueItems", outputSchemaError[struct {
			Value []string `jsonschema:"uniqueItems=yes"`
		}], "true or false"},
		{"unsupported type", outputSchemaError[struct{ Value map[string]string }], "not supported"},
		{"recursive type", outputSchemaError[outputSchemaRecursiveFixture], "recursive"},
		{"non-object root", outputSchemaError[string], "not an object"},
	}
	for _, test := range failures {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("OutputSchema error = %v, want diagnostic containing %q", err, test.want)
			}
		})
	}
}

func outputSchemaError[T any]() error {
	_, err := OutputSchema[T]()
	return err
}

func assertOutputRequired(t *testing.T, schema map[string]any, want []string) {
	t.Helper()
	gotValues := schema["required"].([]any)
	got := make([]string, len(gotValues))
	for index, value := range gotValues {
		got[index] = value.(string)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("required = %v, want %v", got, want)
	}
	if schema["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false", schema["additionalProperties"])
	}
}

func assertOutputValues(t *testing.T, schema, want map[string]any) {
	t.Helper()
	for key, wantValue := range want {
		if got := schema[key]; !reflect.DeepEqual(got, wantValue) {
			t.Errorf("%s = %#v, want %#v", key, got, wantValue)
		}
	}
}

func TestValidateOutputSchemaHasExactPublicSignature(t *testing.T) {
	// R-TMEY-1C4X
	want := reflect.TypeOf(func(json.RawMessage) error { return nil })
	if got := reflect.TypeOf(ValidateOutputSchema); got != want {
		t.Fatalf("ValidateOutputSchema type = %v, want %v", got, want)
	}
	if err := ValidateOutputSchema(json.RawMessage(`{"type":"object","properties":{},"required":[]}`)); err != nil {
		t.Fatalf("ValidateOutputSchema rejected a valid schema: %v", err)
	}
}

func TestValidateOutputSchemaAcceptsRecursiveOutputSubset(t *testing.T) {
	// R-TSIF-Y6UE
	schemas := []struct {
		name   string
		schema json.RawMessage
	}{
		{
			name: "all grammar and constraint keywords",
			schema: json.RawMessage(`{
				"type":"object",
				"description":"complete result",
				"additionalProperties":false,
				"$defs":{
					"Address":{"type":"object","additionalProperties":false,"properties":{"city":{"type":"string","const":"Oslo"}},"required":["city"]}
				},
				"properties":{
					"address":{"$ref":"#/$defs/Address"},
					"label":{"type":"string","description":"short label","enum":["alpha","beta"],"minLength":1,"maxLength":8,"pattern":"^[a-z]+$","format":"hostname"},
					"score":{"type":"number","minimum":0,"maximum":10,"exclusiveMinimum":-1,"exclusiveMaximum":11,"multipleOf":0.5,"const":2.5},
					"count":{"type":"integer","minimum":0,"const":2},
					"enabled":{"type":"boolean","const":true},
					"values":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"}},"required":["id"]},"minItems":1,"maxItems":3,"uniqueItems":true},
					"note":{"anyOf":[{"type":"string"},{"type":"null"}]},
					"nothing":{"type":"null","const":null}
				},
				"required":["address","label","score","count","enabled","values","note","nothing"]
			}`),
		},
		{
			name:   "escaped internal definition pointer",
			schema: json.RawMessage(`{"type":"object","$defs":{"a/b~c":{"type":"array","items":{"type":"string"}}},"properties":{"value":{"$ref":"#/$defs/a~1b~0c"}},"required":["value"]}`),
		},
		{
			name:   "finite numbers beyond float64 range",
			schema: json.RawMessage(`{"type":"object","properties":{"huge":{"type":"number","maximum":1e9999,"const":1e9999},"tinyStep":{"type":"number","multipleOf":1e-9999}},"required":["huge","tinyStep"]}`),
		},
	}
	for _, test := range schemas {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateOutputSchema(test.schema); err != nil {
				t.Errorf("valid output schema rejected: %v", err)
			}
		})
	}
}

func TestValidateOutputSchemaRejectsOutsideSubsetWithDiagnostics(t *testing.T) {
	// R-TTQC-BYL3
	tests := []struct {
		name   string
		schema json.RawMessage
		want   string
	}{
		{"malformed JSON", json.RawMessage(`{"type":"object"`), "JSON"},
		{"trailing JSON value", json.RawMessage(`{"type":"object"} {}`), "multiple JSON values"},
		{"non-object JSON root", json.RawMessage(`[]`), "root"},
		{"non-object schema root", json.RawMessage(`{"type":"string"}`), "type object"},
		{"additional properties true", json.RawMessage(`{"type":"object","additionalProperties":true}`), "additionalProperties"},
		{"additional properties schema", json.RawMessage(`{"type":"object","additionalProperties":{"type":"string"}}`), "additionalProperties"},
		{"additional properties null", json.RawMessage(`{"type":"object","additionalProperties":null}`), "additionalProperties"},
		{"external reference", json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"https://example.test/schema"}},"required":["x"]}`), "$ref"},
		{"missing reference", json.RawMessage(`{"type":"object","properties":{"x":{"$ref":"#/$defs/Missing"}},"required":["x"]}`), "#/$defs/Missing"},
		{"reference to schema container", json.RawMessage(`{"type":"object","$defs":{"X":{"type":"object","properties":{},"required":[]}},"properties":{"x":{"$ref":"#/$defs/X/properties"}},"required":["x"]}`), "$ref"},
		{"reference to object-valued const", json.RawMessage(`{"type":"object","$defs":{"X":{"type":"object","const":{"type":"string"},"properties":{},"required":[]}},"properties":{"x":{"$ref":"#/$defs/X/const"}},"required":["x"]}`), "$ref"},
		{"malformed reference pointer", json.RawMessage(`{"type":"object","$defs":{"X":{"type":"string"}},"properties":{"x":{"$ref":"#/$defs/~2"}},"required":["x"]}`), "$ref"},
		{"malformed reference array index", json.RawMessage(`{"type":"object","$defs":{"X":{"anyOf":[{"type":"string"},{"type":"null"}]}},"properties":{"x":{"$ref":"#/$defs/X/anyOf/+0"}},"required":["x"]}`), "$ref"},
		{"direct recursive reference", json.RawMessage(`{"type":"object","$defs":{"Node":{"$ref":"#/$defs/Node"}},"properties":{"node":{"$ref":"#/$defs/Node"}},"required":["node"]}`), "recursive $ref"},
		{"indirect recursive reference", json.RawMessage(`{"type":"object","$defs":{"A":{"$ref":"#/$defs/B"},"B":{"$ref":"#/$defs/A"}},"properties":{"value":{"$ref":"#/$defs/A"}},"required":["value"]}`), "recursive $ref"},
		{"allOf", json.RawMessage(`{"type":"object","properties":{"x":{"allOf":[{"type":"string"}]}},"required":["x"]}`), "allOf"},
		{"oneOf", json.RawMessage(`{"type":"object","properties":{"x":{"oneOf":[{"type":"string"},{"type":"null"}]}},"required":["x"]}`), "oneOf"},
		{"arbitrary anyOf", json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[{"type":"string"},{"type":"number"}]}},"required":["x"]}`), "anyOf"},
		{"two null anyOf branches", json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[{"type":"null","description":"first null"},{"type":"null"}]}},"required":["x"]}`), "anyOf"},
		{"unknown keyword", json.RawMessage(`{"type":"object","title":"result"}`), "title"},
		{"unsupported type", json.RawMessage(`{"type":"object","properties":{"x":{"type":"date"}},"required":["x"]}`), "date"},
		{"property absent from required", json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":[]}`), "x"},
		{"required absent from properties", json.RawMessage(`{"type":"object","properties":{},"required":["x"]}`), "x"},
		{"duplicate required", json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["x","x"]}`), "required"},
		{"nested array object property absent from required", json.RawMessage(`{"type":"object","properties":{"rows":{"type":"array","items":{"type":"object","properties":{"x":{"type":"string"}},"required":[]}}},"required":["rows"]}`), "x"},
		{"definition property absent from required", json.RawMessage(`{"type":"object","$defs":{"X":{"type":"object","properties":{"x":{"type":"string"}},"required":[]}}}`), "x"},
		{"wrong constraint type", json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","minimum":0}},"required":["x"]}`), "minimum"},
		{"description value", json.RawMessage(`{"type":"object","description":7}`), "description"},
		{"empty enum", json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","enum":[]}},"required":["x"]}`), "enum"},
		{"inconsistent const", json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer","const":1.5}},"required":["x"]}`), "const"},
		{"properties value", json.RawMessage(`{"type":"object","properties":[]}`), "properties"},
		{"definition value", json.RawMessage(`{"type":"object","$defs":{"X":false}}`), "schema"},
		{"items missing", json.RawMessage(`{"type":"object","properties":{"x":{"type":"array"}},"required":["x"]}`), "items"},
		{"tuple items", json.RawMessage(`{"type":"object","properties":{"x":{"type":"array","items":[]}},"required":["x"]}`), "items"},
		{"negative cardinality", json.RawMessage(`{"type":"object","properties":{"x":{"type":"array","items":{"type":"string"},"minItems":-1}},"required":["x"]}`), "minItems"},
		{"fractional length", json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","maxLength":1.5}},"required":["x"]}`), "maxLength"},
		{"pattern value", json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","pattern":false}},"required":["x"]}`), "pattern"},
		{"format value", json.RawMessage(`{"type":"object","properties":{"x":{"type":"string","format":1}},"required":["x"]}`), "format"},
		{"non-positive multiple", json.RawMessage(`{"type":"object","properties":{"x":{"type":"number","multipleOf":0}},"required":["x"]}`), "multipleOf"},
		{"unique items value", json.RawMessage(`{"type":"object","properties":{"x":{"type":"array","items":{"type":"string"},"uniqueItems":"yes"}},"required":["x"]}`), "uniqueItems"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateOutputSchema(test.schema)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("ValidateOutputSchema(%s) = %v, want diagnostic containing %q", test.schema, err, test.want)
			}
		})
	}
}

func TestValidateOutputSchemaGatesFormatsAndPatterns(t *testing.T) {
	// R-UKK4-QWWD
	formats := []string{"date-time", "date", "time", "email", "uri", "uuid", "ipv4", "ipv6", "hostname"}
	for _, format := range formats {
		schema := json.RawMessage(`{"type":"object","properties":{"value":{"type":"string","format":"` + format + `"}},"required":["value"]}`)
		if err := ValidateOutputSchema(schema); err != nil {
			t.Errorf("supported format %q rejected: %v", format, err)
		}
	}
	for name, schema := range map[string]string{
		"unsupported format": `{"type":"object","properties":{"value":{"type":"string","format":"future"}},"required":["value"]}`,
		"malformed pattern":  `{"type":"object","properties":{"value":{"type":"string","pattern":"["}},"required":["value"]}`,
	} {
		if err := ValidateOutputSchema(json.RawMessage(schema)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestOutputDocumentValidatorEnforcesFullSchema(t *testing.T) {
	// R-U758-JFQQ
	schema := json.RawMessage(`{
		"type":"object","additionalProperties":false,
		"$defs":{"row":{"type":"object","additionalProperties":false,"properties":{"line":{"type":"number","minimum":0.1,"multipleOf":0.1}},"required":["line"]}},
		"properties":{
			"rows":{"type":"array","items":{"$ref":"#/$defs/row"},"minItems":1,"maxItems":2,"uniqueItems":true},
			"label":{"type":"string","minLength":2,"maxLength":3,"pattern":"^[αβ]+$"},
			"choice":{"enum":[{"x":1}],"type":"object","additionalProperties":false,"properties":{"x":{"type":"number"}},"required":["x"]},
			"fixed":{"const":[1,2],"type":"array","items":{"type":"integer"}},
			"note":{"anyOf":[{"type":"string"},{"type":"null"}]}
		},"required":["rows","label","choice","fixed","note"]}`)
	valid := json.RawMessage(`{"rows":[{"line":0.3}],"label":"αβ","choice":{"x":1.0},"fixed":[1.0,2],"note":null}`)
	if violation := validateOutputDocument(schema, valid); violation != nil {
		t.Fatalf("valid full document rejected: %v", violation)
	}
	tests := []struct {
		name string
		doc  string
		path string
	}{
		{"malformed", `{"rows":`, "$"},
		{"trailing", string(valid) + ` {}`, "$"},
		{"wrong type", `{"rows":[{"line":0.3}],"label":7,"choice":{"x":1},"fixed":[1,2],"note":null}`, "$.label"},
		{"missing required", `{"rows":[{"line":0.3}],"choice":{"x":1},"fixed":[1,2],"note":null}`, "$.label"},
		{"closed nested object", `{"rows":[{"line":0.3,"extra":true}],"label":"αβ","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.rows[0].extra"},
		{"items reference", `{"rows":[{"line":"bad"}],"label":"αβ","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.rows[0].line"},
		{"exact decimal multiple", `{"rows":[{"line":0.3000000000000000000000000000000000000001}],"label":"αβ","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.rows[0].line"},
		{"array minimum", `{"rows":[],"label":"αβ","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.rows"},
		{"array maximum", `{"rows":[{"line":0.1},{"line":0.2},{"line":0.3}],"label":"αβ","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.rows"},
		{"rune min length", `{"rows":[{"line":0.3}],"label":"α","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.label"},
		{"rune max length", `{"rows":[{"line":0.3}],"label":"αβαβ","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.label"},
		{"pattern", `{"rows":[{"line":0.3}],"label":"αx","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.label"},
		{"compound enum", `{"rows":[{"line":0.3}],"label":"αβ","choice":{"x":2},"fixed":[1,2],"note":null}`, "$.choice"},
		{"compound const", `{"rows":[{"line":0.3}],"label":"αβ","choice":{"x":1},"fixed":[1,3],"note":null}`, "$.fixed"},
		{"nullable mismatch", `{"rows":[{"line":0.3}],"label":"αβ","choice":{"x":1},"fixed":[1,2],"note":4}`, "$.note"},
		{"unique compound items", `{"rows":[{"line":0.3},{"line":0.30}],"label":"αβ","choice":{"x":1},"fixed":[1,2],"note":null}`, "$.rows[1]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			violation := validateOutputDocument(schema, json.RawMessage(test.doc))
			if violation == nil || !strings.Contains(violation.Violations[0].Path, test.path) {
				t.Fatalf("violation = %#v, want path %q", violation, test.path)
			}
		})
	}
}

func TestOutputDocumentValidatorEnforcesNumericConstraintFamilies(t *testing.T) {
	// R-UJC8-D55O
	schema := json.RawMessage(`{"type":"object","properties":{"value":{"type":"number","minimum":-2,"maximum":10,"exclusiveMinimum":-3,"exclusiveMaximum":11,"multipleOf":0.0000000000000000001}},"required":["value"]}`)
	for _, value := range []string{"-3", "-2.1", "11", "10.0000000000000000001", "0.00000000000000000015"} {
		violation := validateOutputDocument(schema, json.RawMessage(`{"value":`+value+`}`))
		if violation == nil || violation.Violations[0].Path != "$.value" {
			t.Errorf("constraint-only value %s violation = %#v", value, violation)
		}
	}
	if violation := validateOutputDocument(schema, json.RawMessage(`{"value":0.0000000000000000003}`)); violation != nil {
		t.Fatalf("exact rational multiple rejected: %v", violation)
	}
}

func TestOutputDocumentValidatorEnforcesSupportedFormats(t *testing.T) {
	// R-UKK4-QWWD
	tests := []struct{ format, valid, invalid string }{
		{"date-time", "2024-02-29T23:59:59Z", "2023-02-29T23:59:59Z"},
		{"date", "2024-02-29", "2023-02-29"},
		{"time", "23:59:59+05:30", "23:59:59"},
		{"email", "name@example.com", "Name <name@example.com>"},
		{"uri", "https://example.com/a?b=c", "/relative"},
		{"uuid", "123e4567-e89b-42d3-a456-426614174000", "123e4567e89b-42d3-a456-426614174000"},
		{"ipv4", "192.0.2.1", "::ffff:192.0.2.1"},
		{"ipv6", "2001:db8::1", "192.0.2.1"},
		{"hostname", "api.example-1.com", "-api.example.com"},
	}
	for _, test := range tests {
		schema := json.RawMessage(`{"type":"object","properties":{"value":{"type":"string","format":"` + test.format + `"}},"required":["value"]}`)
		if violation := validateOutputDocument(schema, json.RawMessage(`{"value":"`+test.valid+`"}`)); violation != nil {
			t.Errorf("%s valid value rejected: %v", test.format, violation)
		}
		if violation := validateOutputDocument(schema, json.RawMessage(`{"value":"`+test.invalid+`"}`)); violation == nil {
			t.Errorf("%s invalid value accepted", test.format)
		}
	}
}

func TestOutputDocumentValidatorCollectsStableExactViolations(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object","additionalProperties":false,
		"properties":{
			"items":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"line":{"type":"number","minimum":0}},"required":["line"]}},
			"name":{"type":"string","minLength":2,"pattern":"^[a-z]+$"},
			"needed":{"type":"boolean"}
		},"required":["items","name","needed"]}`)
	document := json.RawMessage(`{"items":[{"line":-0.00000000000000000000001},{"line":-2}],"name":"","z":"a\nb"}`)
	result := validateOutputDocument(schema, document)
	if result == nil {
		t.Fatal("multi-violation document accepted")
	}
	want := []outputViolation{
		{Path: "$.needed", Rule: "is required", Present: false},
		{Path: "$.items[0].line", Rule: "must be >= 0", Offending: json.Number("-0.00000000000000000000001"), Present: true},
		{Path: "$.items[1].line", Rule: "must be >= 0", Offending: json.Number("-2"), Present: true},
		{Path: "$.name", Rule: "length must be at least 2", Offending: "", Present: true},
		{Path: "$.name", Rule: `must match pattern "^[a-z]+$"`, Offending: "", Present: true},
		{Path: "$.z", Rule: "property must be declared by the schema", Offending: "a\nb", Present: true},
	}
	// R-UASX-OQYT
	if !reflect.DeepEqual(result.Violations, want) {
		t.Fatalf("violations = %#v, want stable exact %#v", result.Violations, want)
	}
	malformed := validateOutputDocument(schema, json.RawMessage(`{"items":`))
	if malformed == nil || len(malformed.Violations) != 1 || malformed.Violations[0].Path != "$" ||
		malformed.Violations[0].Offending != `{"items":` || !strings.Contains(malformed.Violations[0].Rule, "exactly one document") {
		t.Fatalf("malformed diagnostic = %#v", malformed)
	}
}

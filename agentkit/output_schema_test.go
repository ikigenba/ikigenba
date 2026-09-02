package agentkit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

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
					"label":{"type":"string","description":"short label","enum":["alpha","beta"],"minLength":1,"maxLength":8,"pattern":"^[a-z]+$","format":"future-format-name"},
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

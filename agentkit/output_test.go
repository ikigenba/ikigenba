package agentkit

import (
	"encoding/json"
	"reflect"
	"testing"
)

const compileTimeDefaultOutputAttempts = DefaultOutputAttempts

func TestOutputContractExactShape(t *testing.T) {
	// R-TIR8-W0WU
	contractType := reflect.TypeOf(OutputContract{})
	want := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Schema", typeOf: reflect.TypeFor[json.RawMessage]()},
		{name: "MaxAttempts", typeOf: reflect.TypeFor[int]()},
	}
	if contractType.NumField() != len(want) {
		t.Fatalf("OutputContract has %d fields, want exactly %d", contractType.NumField(), len(want))
	}
	for index, expected := range want {
		actual := contractType.Field(index)
		if actual.Name != expected.name || actual.Type != expected.typeOf || !actual.IsExported() {
			t.Fatalf("OutputContract field %d = (%s, %v, exported=%t), want (%s, %v, exported=true)", index, actual.Name, actual.Type, actual.IsExported(), expected.name, expected.typeOf)
		}
	}
}

func TestDefaultOutputAttempts(t *testing.T) {
	// R-TJZ5-9SNJ
	var constantSizedArray [compileTimeDefaultOutputAttempts]int
	if got := len(constantSizedArray); got != 3 {
		t.Fatalf("DefaultOutputAttempts = %d, want 3", got)
	}
}

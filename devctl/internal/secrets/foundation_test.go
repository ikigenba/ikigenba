package secrets

import (
	"reflect"
	"testing"
)

func TestEntryHasExactFields(t *testing.T) {
	// R-FQGR-F925
	want := []reflect.StructField{
		{Name: "App", Type: reflect.TypeFor[string]()},
		{Name: "Keys", Type: reflect.TypeFor[[]string]()},
	}
	assertExactFields(t, reflect.TypeFor[Entry](), want)
}

func TestParameterPaths(t *testing.T) {
	// R-0ATG-O3LP
	if got, want := Parameter("sbx1.ikigenba.dev", "crm"), "/sbx1.ikigenba.dev/crm"; got != want {
		t.Fatalf("Parameter() = %q, want %q", got, want)
	}
	if got, want := Prefix("sbx1.ikigenba.dev"), "/sbx1.ikigenba.dev"; got != want {
		t.Fatalf("Prefix() = %q, want %q", got, want)
	}
}

func TestObjectErrorHasExactFieldsAndMessage(t *testing.T) {
	// R-FWK9-C3RM
	want := []reflect.StructField{
		{Name: "Parameter", Type: reflect.TypeFor[string]()},
		{Name: "Reason", Type: reflect.TypeFor[string]()},
	}
	assertExactFields(t, reflect.TypeFor[ObjectError](), want)

	err := ObjectError{Parameter: "/sbx1.ikigenba.dev/crm", Reason: "invalid object"}
	if got, want := err.Error(), "/sbx1.ikigenba.dev/crm: invalid object"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func assertExactFields(t *testing.T, gotType reflect.Type, want []reflect.StructField) {
	t.Helper()
	if gotType.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", gotType, gotType.NumField(), len(want))
	}
	for index, wantField := range want {
		gotField := gotType.Field(index)
		if gotField.Name != wantField.Name || gotField.Type != wantField.Type {
			t.Errorf("%s field %d = %s %s, want %s %s", gotType, index, gotField.Name, gotField.Type, wantField.Name, wantField.Type)
		}
	}
}

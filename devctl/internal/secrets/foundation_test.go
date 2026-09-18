package secrets

import (
	"context"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/account"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestEntryHasExactFields(t *testing.T) {
	// R-FQGR-F925
	want := []reflect.StructField{
		{Name: "App", Type: reflect.TypeFor[string]()},
		{Name: "Keys", Type: reflect.TypeFor[[]string]()},
	}
	assertExactFields(t, reflect.TypeFor[Entry](), want)
}

func TestExportedOperationSignatures(t *testing.T) {
	// R-FSWK-6SJJ
	type pushSignature func(context.Context, seam.Deps, *account.Account, string, []checkout.App) ([]Entry, error)
	type listSignature func(context.Context, *account.Account, string) ([]Entry, error)
	type namesSignature func(context.Context, *account.Account, string, string) ([]string, error)

	var push pushSignature = Push
	var list listSignature = List
	var names namesSignature = Names

	if push == nil || list == nil || names == nil {
		t.Fatal("exported secrets operation is nil")
	}
}

func TestParameterPaths(t *testing.T) {
	// R-FU4G-KKA8
	if got, want := Parameter("foo.sbx.ikigenba.dev", "crm"), "/ikigenba/foo.sbx.ikigenba.dev/crm"; got != want {
		t.Fatalf("Parameter() = %q, want %q", got, want)
	}
	if got, want := Prefix("foo.sbx.ikigenba.dev"), "/ikigenba/foo.sbx.ikigenba.dev"; got != want {
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

	err := ObjectError{Parameter: "/ikigenba/example/app", Reason: "invalid object"}
	if got, want := err.Error(), "/ikigenba/example/app: invalid object"; got != want {
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

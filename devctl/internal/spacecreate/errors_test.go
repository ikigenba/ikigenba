package spacecreate

import (
	"reflect"
	"testing"
)

func TestRefusedError(t *testing.T) {
	// R-XS6W-S2VB
	typeOf := reflect.TypeOf(RefusedError{})
	if typeOf.NumField() != 1 || typeOf.Field(0).Name != "Message" || typeOf.Field(0).Type.Kind() != reflect.String {
		t.Fatalf("RefusedError fields = %v, want only Message string", reflect.VisibleFields(typeOf))
	}
	err := &RefusedError{Message: "cannot proceed"}
	if got := err.Error(); got != err.Message {
		t.Fatalf("Error() = %q, want %q", got, err.Message)
	}
	if got := err.ExitCode(); got != 2 {
		t.Fatalf("ExitCode() = %d, want 2", got)
	}
}

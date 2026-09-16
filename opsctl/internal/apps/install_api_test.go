package apps_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallHooksAPI(t *testing.T) {
	// R-YNIK-9QOD
	want := map[string]reflect.Type{
		"Report":    reflect.TypeFor[func(string, string, bool) error](),
		"Configure": reflect.TypeFor[func(context.Context, apps.Manifest) error](),
	}
	assertExactExportedFields(t, reflect.TypeFor[apps.InstallHooks](), want)
}

func TestInstallAPI(t *testing.T) {
	// R-OLR2-NBFZ
	want := reflect.TypeFor[func(context.Context, host.Env, cloud.Env, config.Store, string, apps.InstallHooks) error]()
	if got := reflect.TypeOf(apps.Install); got != want {
		t.Fatalf("apps.Install has type %v, want %v", got, want)
	}
}

func TestInstallErrorAPI(t *testing.T) {
	// R-OMYZ-136O
	want := map[string]reflect.Type{
		"Code":    reflect.TypeFor[int](),
		"Message": reflect.TypeFor[string](),
		"Cause":   reflect.TypeFor[error](),
	}
	assertExactExportedFields(t, reflect.TypeFor[apps.InstallError](), want)

	cause := errors.New("cause")
	failure := &apps.InstallError{Code: 2, Message: "message", Cause: cause}
	if got := failure.Error(); got != "message" {
		t.Fatalf("Error() = %q, want %q", got, "message")
	}
	if got := failure.Unwrap(); !reflect.DeepEqual(got, cause) {
		t.Fatalf("Unwrap() = %v, want cause", got)
	}
	if !errors.Is(failure, cause) {
		t.Fatal("errors.Is does not reach InstallError.Cause")
	}
}

func assertExactExportedFields(t *testing.T, typ reflect.Type, want map[string]reflect.Type) {
	t.Helper()
	if typ.Kind() != reflect.Struct {
		t.Fatalf("%v is a %v, want struct", typ, typ.Kind())
	}
	if typ.NumField() != len(want) {
		t.Fatalf("%v has %d fields, want exactly %d", typ, typ.NumField(), len(want))
	}
	for name, fieldType := range want {
		field, ok := typ.FieldByName(name)
		if !ok || !field.IsExported() || field.Type != fieldType {
			t.Errorf("%v.%s = %v, want exported %v", typ, name, field.Type, fieldType)
		}
	}
}

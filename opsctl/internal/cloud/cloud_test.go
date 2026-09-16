package cloud_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
)

func TestEnvFields(t *testing.T) {
	// R-5NVA-9DP6
	assertFields(t, reflect.TypeFor[cloud.Env](), map[string]reflect.Type{
		"Open": reflect.TypeFor[func(context.Context, string) (cloud.Client, error)](),
	})
}

func TestClientMethods(t *testing.T) {
	// R-5P36-N5FV
	typ := reflect.TypeFor[cloud.Client]()
	if typ.Kind() != reflect.Interface {
		t.Fatalf("Client kind = %v, want interface", typ.Kind())
	}
	want := map[string]reflect.Type{
		"GetObject":   reflect.TypeFor[func(context.Context, string) (io.ReadCloser, error)](),
		"PutObject":   reflect.TypeFor[func(context.Context, string, io.Reader) error](),
		"ListObjects": reflect.TypeFor[func(context.Context, string) ([]cloud.Object, error)](),
		"ReadSecrets": reflect.TypeFor[func(context.Context, string) (map[string]string, error)](),
	}
	if typ.NumMethod() != len(want) {
		t.Fatalf("Client has %d methods, want %d", typ.NumMethod(), len(want))
	}
	for name, signature := range want {
		method, ok := typ.MethodByName(name)
		if !ok || method.Type != signature {
			t.Errorf("Client.%s = %v, want %v", name, method.Type, signature)
		}
	}
}

func TestObjectFields(t *testing.T) {
	// R-5QB3-0X6K
	assertFields(t, reflect.TypeFor[cloud.Object](), map[string]reflect.Type{
		"URI":      reflect.TypeFor[string](),
		"Size":     reflect.TypeFor[int64](),
		"Modified": reflect.TypeFor[time.Time](),
	})
}

func TestErrorSentinels(t *testing.T) {
	// R-AOUN-W2JU
	missing := cloud.ErrNotFound
	collision := cloud.ErrAlreadyExists
	if missing == nil || collision == nil {
		t.Fatal("cloud sentinels must be non-nil errors")
	}
	for _, sentinel := range []error{missing, collision} {
		wrapped := fmt.Errorf("object operation: %w", sentinel)
		if !errors.Is(wrapped, sentinel) {
			t.Errorf("wrapped error does not match %v", sentinel)
		}
		for _, distinct := range []error{errors.New("access denied"), errors.New("transport failed")} {
			if errors.Is(distinct, sentinel) {
				t.Errorf("%v matches sentinel %v", distinct, sentinel)
			}
		}
	}
	if errors.Is(missing, collision) || errors.Is(collision, missing) {
		t.Fatal("not-found and collision sentinels must be distinct")
	}
}

func assertFields(t *testing.T, typ reflect.Type, want map[string]reflect.Type) {
	t.Helper()
	if typ.Kind() != reflect.Struct {
		t.Fatalf("%s kind = %v, want struct", typ.Name(), typ.Kind())
	}
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typ.Name(), typ.NumField(), len(want))
	}
	for name, signature := range want {
		field, ok := typ.FieldByName(name)
		if !ok || field.Type != signature || !field.IsExported() {
			t.Errorf("%s.%s = %v, want exported %v", typ.Name(), name, field.Type, signature)
		}
	}
}

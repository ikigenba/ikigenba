package retry

import (
	"bytes"
	"context"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
	"time"
)

func TestClockHasExactMethodSet(t *testing.T) {
	// R-0HAQ-WTJK
	typ := reflect.TypeOf((*Clock)(nil)).Elem()
	if typ.Name() != "Clock" || typ.Kind() != reflect.Interface {
		t.Fatalf("Clock is %q of kind %v, want defined interface Clock", typ.Name(), typ.Kind())
	}

	want := []struct {
		name string
		in   []reflect.Type
		out  []reflect.Type
	}{
		{name: "Now", out: []reflect.Type{reflect.TypeOf(time.Time{})}},
		{
			name: "Sleep",
			in: []reflect.Type{
				reflect.TypeOf((*context.Context)(nil)).Elem(),
				reflect.TypeOf(time.Duration(0)),
			},
			out: []reflect.Type{reflect.TypeOf((*error)(nil)).Elem()},
		},
	}
	if typ.NumMethod() != len(want) {
		t.Fatalf("Clock has %d methods, want %d", typ.NumMethod(), len(want))
	}
	for i, expected := range want {
		method := typ.Method(i)
		if method.Name != expected.name {
			t.Errorf("method %d name = %q, want %q", i, method.Name, expected.name)
		}
		assertFunctionSignature(t, method.Type, expected.in, expected.out)
	}
}

func TestPolicyHasExactFieldsInOrder(t *testing.T) {
	// R-0IIN-ALA9
	typ := reflect.TypeOf(Policy{})
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{name: "MaxAttempts", typ: reflect.TypeOf(int(0))},
		{name: "Base", typ: reflect.TypeOf(time.Duration(0))},
		{name: "Max", typ: reflect.TypeOf(time.Duration(0))},
		{name: "Jitter", typ: reflect.TypeOf(float64(0))},
		{name: "Clock", typ: reflect.TypeOf((*Clock)(nil)).Elem()},
		{name: "Rand", typ: reflect.TypeOf((func() float64)(nil))},
		{name: "Retryable", typ: reflect.TypeOf((func(error) bool)(nil))},
		{name: "RetryAfter", typ: reflect.TypeOf((func(error) time.Duration)(nil))},
	}
	if typ.Name() != "Policy" || typ.Kind() != reflect.Struct {
		t.Fatalf("Policy is %q of kind %v, want defined struct Policy", typ.Name(), typ.Kind())
	}
	if typ.NumField() != len(want) {
		t.Fatalf("Policy has %d fields, want %d", typ.NumField(), len(want))
	}
	for i, expected := range want {
		field := typ.Field(i)
		if field.Name != expected.name || field.Type != expected.typ || !field.IsExported() {
			t.Errorf("field %d = exported=%v %s %v, want exported %s %v",
				i, field.IsExported(), field.Name, field.Type, expected.name, expected.typ)
		}
	}
}

func TestDoHasExactGenericDeclaration(t *testing.T) {
	// R-0JQJ-OD0Y
	assertCallable := func(func(
		context.Context,
		Policy,
		func(context.Context) (int, error),
		func(int, error, time.Duration),
	) (int, error)) {
	}
	assertCallable(Do[int])

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "retry.go", nil, 0)
	if err != nil {
		t.Fatalf("parse retry.go: %v", err)
	}
	var declaration *ast.FuncDecl
	for _, item := range file.Decls {
		if function, ok := item.(*ast.FuncDecl); ok && function.Name.Name == "Do" {
			declaration = function
			break
		}
	}
	if declaration == nil {
		t.Fatal("Do declaration not found")
	}
	if declaration.Recv != nil {
		t.Fatal("Do is a method, want package function")
	}
	assertASTFields(t, fileSet, declaration.Type.TypeParams, []astField{
		{names: []string{"T"}, typ: "any"},
	})
	assertASTFields(t, fileSet, declaration.Type.Params, []astField{
		{names: []string{"ctx"}, typ: "context.Context"},
		{names: []string{"p"}, typ: "Policy"},
		{names: []string{"op"}, typ: "func(ctx context.Context) (T, error)"},
		{names: []string{"onRetry"}, typ: "func(attempt int, err error, delay time.Duration)"},
	})
	assertASTFields(t, fileSet, declaration.Type.Results, []astField{
		{typ: "T"},
		{typ: "error"},
	})
}

func assertFunctionSignature(t *testing.T, got reflect.Type, in, out []reflect.Type) {
	t.Helper()
	if got.NumIn() != len(in) || got.NumOut() != len(out) || got.IsVariadic() {
		t.Errorf("signature = %v, want %d inputs, %d outputs, non-variadic", got, len(in), len(out))
		return
	}
	for i, expected := range in {
		if got.In(i) != expected {
			t.Errorf("input %d = %v, want %v", i, got.In(i), expected)
		}
	}
	for i, expected := range out {
		if got.Out(i) != expected {
			t.Errorf("output %d = %v, want %v", i, got.Out(i), expected)
		}
	}
}

type astField struct {
	names []string
	typ   string
}

func assertASTFields(t *testing.T, fileSet *token.FileSet, got *ast.FieldList, want []astField) {
	t.Helper()
	if got == nil || len(got.List) != len(want) {
		if got == nil {
			t.Fatalf("field list is nil, want %d fields", len(want))
		}
		t.Fatalf("field list has %d fields, want %d", len(got.List), len(want))
	}
	for i, expected := range want {
		field := got.List[i]
		names := make([]string, len(field.Names))
		for j, name := range field.Names {
			names[j] = name.Name
		}
		if len(names) != len(expected.names) {
			t.Errorf("field %d names = %v, want %v", i, names, expected.names)
		} else {
			for j := range names {
				if names[j] != expected.names[j] {
					t.Errorf("field %d names = %v, want %v", i, names, expected.names)
					break
				}
			}
		}
		var rendered bytes.Buffer
		if err := format.Node(&rendered, fileSet, field.Type); err != nil {
			t.Fatalf("format field %d type: %v", i, err)
		}
		if rendered.String() != expected.typ {
			t.Errorf("field %d type = %q, want %q", i, rendered.String(), expected.typ)
		}
	}
}

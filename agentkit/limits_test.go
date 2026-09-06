package agentkit

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

func TestLimitsShape(t *testing.T) {
	// R-TILY-AHD5
	typeOfLimits := reflect.TypeOf(Limits{})
	if typeOfLimits.NumField() != 2 {
		t.Fatalf("Limits has %d fields, want exactly 2", typeOfLimits.NumField())
	}
	want := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "MaxToolCalls", typeOf: reflect.TypeOf(int(0))},
		{name: "MaxContextTokens", typeOf: reflect.TypeOf(int64(0))},
	}
	for index, expected := range want {
		field := typeOfLimits.Field(index)
		if field.Name != expected.name || field.Type != expected.typeOf {
			t.Fatalf("Limits field %d = (%s, %s), want (%s, %s)", index, field.Name, field.Type, expected.name, expected.typeOf)
		}
	}
}

func TestLimitKindValues(t *testing.T) {
	// R-TJTU-O93U
	if reflect.TypeOf(LimitKind("")).Kind() != reflect.String {
		t.Fatalf("LimitKind underlying kind = %s, want string", reflect.TypeOf(LimitKind("")).Kind())
	}
	if got := string(LimitToolCalls); got != "tool_calls" {
		t.Fatalf("LimitToolCalls = %q, want %q", got, "tool_calls")
	}
	if got := string(LimitContextTokens); got != "context_tokens" {
		t.Fatalf("LimitContextTokens = %q, want %q", got, "context_tokens")
	}
}

func TestLimitInfoShapeAndJSON(t *testing.T) {
	// R-TL1R-20UJ
	typeOfInfo := reflect.TypeOf(LimitInfo{})
	want := []struct {
		name    string
		typeOf  reflect.Type
		jsonTag string
	}{
		{name: "Kind", typeOf: reflect.TypeOf(LimitKind("")), jsonTag: "kind"},
		{name: "Max", typeOf: reflect.TypeOf(int64(0)), jsonTag: "max"},
		{name: "Actual", typeOf: reflect.TypeOf(int64(0)), jsonTag: "actual"},
	}
	if typeOfInfo.NumField() != len(want) {
		t.Fatalf("LimitInfo has %d fields, want exactly %d", typeOfInfo.NumField(), len(want))
	}
	for index, expected := range want {
		field := typeOfInfo.Field(index)
		if field.Name != expected.name || field.Type != expected.typeOf || field.Tag.Get("json") != expected.jsonTag {
			t.Fatalf("LimitInfo field %d = (%s, %s, json:%q), want (%s, %s, json:%q)", index, field.Name, field.Type, field.Tag.Get("json"), expected.name, expected.typeOf, expected.jsonTag)
		}
	}

	wantInfo := LimitInfo{Kind: LimitContextTokens, Max: 100, Actual: 101}
	encoded, err := json.Marshal(wantInfo)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatal(err)
	}
	wantKeys := map[string]bool{"kind": true, "max": true, "actual": true}
	if len(keys) != len(wantKeys) {
		t.Fatalf("marshaled LimitInfo keys = %v, want exactly %v", keys, wantKeys)
	}
	for key := range wantKeys {
		if _, ok := keys[key]; !ok {
			t.Fatalf("marshaled LimitInfo is missing key %q: %s", key, encoded)
		}
	}
	var decoded LimitInfo
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != wantInfo {
		t.Fatalf("LimitInfo JSON round trip = %#v, want %#v", decoded, wantInfo)
	}
}

func TestErrLimitExceededDeclarationAndBehavior(t *testing.T) {
	// R-TNHJ-TKBX
	if ErrLimitExceeded == nil {
		t.Fatal("ErrLimitExceeded is nil")
	}
	if got := ErrLimitExceeded.Error(); got != "agentkit: limit exceeded" {
		t.Fatalf("ErrLimitExceeded.Error() = %q, want %q", got, "agentkit: limit exceeded")
	}
	if !errors.Is(ErrLimitExceeded, ErrLimitExceeded) {
		t.Fatal("errors.Is(ErrLimitExceeded, ErrLimitExceeded) = false")
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "limits.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value := specification.(*ast.ValueSpec)
			if len(value.Names) != 1 || value.Names[0].Name != "ErrLimitExceeded" {
				continue
			}
			found++
			if !ast.IsExported(value.Names[0].Name) || !isDirectErrorsNew(value, "agentkit: limit exceeded") {
				t.Fatal("ErrLimitExceeded is not declared directly with the exact errors.New message")
			}
		}
	}
	if found != 1 {
		t.Fatalf("ErrLimitExceeded package declarations = %d, want exactly one", found)
	}

	var providerError *Error
	if errors.As(ErrLimitExceeded, &providerError) {
		t.Fatal("errors.As(ErrLimitExceeded, *Error) = true")
	}
	if Retryable(ErrLimitExceeded) {
		t.Fatal("Retryable(ErrLimitExceeded) = true")
	}
}

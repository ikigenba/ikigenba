package agentkit

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestEndpointFieldsAreUnexported(t *testing.T) {
	// R-YEPA-QILV
	// R-OBV1-6HH7
	typeOfEndpoint := reflect.TypeFor[Endpoint]()
	for index := range typeOfEndpoint.NumField() {
		if typeOfEndpoint.Field(index).IsExported() {
			t.Fatalf("Endpoint field %q is assignable by consumers", typeOfEndpoint.Field(index).Name)
		}
	}
}

func TestEndpointExportsNoOptionFunctions(t *testing.T) {
	// R-OBV1-6HH7
	parsed, err := parser.ParseFile(token.NewFileSet(), "endpoint.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var exportedFunctions []string
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.IsExported() {
			exportedFunctions = append(exportedFunctions, function.Name.Name)
		}
	}
	if !reflect.DeepEqual(exportedFunctions, []string{"NewEndpoint"}) {
		t.Fatalf("endpoint exports functions %v, want only NewEndpoint", exportedFunctions)
	}
}

func TestEndpointOwnsOnlyBaseURLAndAuth(t *testing.T) {
	// R-OD2X-K97W
	wantFields := map[string]reflect.Type{
		"baseURL": reflect.TypeFor[*url.URL](),
		"auth":    reflect.TypeFor[AuthApplier](),
	}
	configType := reflect.TypeFor[endpointConfig]()
	if configType.NumField() != len(wantFields) {
		t.Fatalf("endpoint config has %d fields, want only baseURL and auth", configType.NumField())
	}
	for name, wantType := range wantFields {
		field, ok := configType.FieldByName(name)
		if !ok || field.Type != wantType {
			t.Fatalf("endpoint config field %q = %v (present=%t), want %v", name, field.Type, ok, wantType)
		}
	}
}

func TestNewEndpointHasExactConstructorAndValidation(t *testing.T) {
	// R-OBV1-6HH7
	// R-OEAT-Y0YL
	wantSignature := reflect.TypeOf(func(string, AuthApplier) (Endpoint, error) { return Endpoint{}, nil })
	if got := reflect.TypeOf(NewEndpoint); got != wantSignature || got.IsVariadic() {
		t.Fatalf("NewEndpoint = %s variadic=%t, want %s non-variadic", got, got.IsVariadic(), wantSignature)
	}
	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpoint, err := NewEndpoint("https://example.test/v1/models/model:stream?alt=sse", auth)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.config.baseURL.String() != "https://example.test/v1/models/model:stream?alt=sse" || endpoint.config.auth == nil {
		t.Fatalf("endpoint did not retain constructor inputs: %+v", endpoint.config)
	}
	for _, baseURL := range []string{"", "not a URL", "/relative", "ftp://example.test/path", "https:///missing-host"} {
		if _, constructErr := NewEndpoint(baseURL, auth); !errors.Is(constructErr, ErrInvalidConfig) {
			t.Errorf("NewEndpoint(%q) error = %v, want ErrInvalidConfig", baseURL, constructErr)
		}
	}
	if _, err := NewEndpoint("https://example.test", nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil auth error = %v, want ErrInvalidConfig", err)
	}
}

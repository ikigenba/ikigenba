package agentkit

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"reflect"
	"testing"
)

// R-JVTF-4LVV
func TestEndpointFieldsAreUnexported(t *testing.T) {
	// R-YEPA-QILV
	typeOfEndpoint := reflect.TypeFor[Endpoint]()
	for index := range typeOfEndpoint.NumField() {
		if typeOfEndpoint.Field(index).IsExported() {
			t.Fatalf("Endpoint field %q is assignable by consumers", typeOfEndpoint.Field(index).Name)
		}
	}
}

// R-JVTF-4LVV
// R-JY97-W5D9
func TestEndpointExportsNoOptionFunctions(t *testing.T) {
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
	if !reflect.DeepEqual(exportedFunctions, []string{"NewEndpoint", "WithBaseURL"}) {
		t.Fatalf("endpoint exports functions %v, want exactly NewEndpoint, WithBaseURL", exportedFunctions)
	}

	if reflect.TypeFor[EndpointOption]().Kind() != reflect.Func {
		t.Fatalf("EndpointOption kind = %s, want Func", reflect.TypeFor[EndpointOption]().Kind())
	}
	wantOptionSignature := reflect.TypeOf(func(string) EndpointOption { return nil })
	if got := reflect.TypeOf(WithBaseURL); got != wantOptionSignature {
		t.Fatalf("WithBaseURL = %s, want %s", got, wantOptionSignature)
	}
}

// R-JX1B-IDMK
// R-K0P0-NOUN
// R-KE5C-F60Q
func TestNewEndpointHasExactConstructorAndValidation(t *testing.T) {
	wantSignature := reflect.TypeOf(func(Authenticator, ...EndpointOption) (Endpoint, error) { return Endpoint{}, nil })
	if got := reflect.TypeOf(NewEndpoint); got != wantSignature || !got.IsVariadic() {
		t.Fatalf("NewEndpoint = %s variadic=%t, want %s variadic", got, got.IsVariadic(), wantSignature)
	}
	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpoint, err := NewEndpoint(auth, WithBaseURL("https://example.test/v1/models/model:stream?alt=sse"))
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.config.baseURL.String() != "https://example.test/v1/models/model:stream?alt=sse" || endpoint.config.auth == nil {
		t.Fatalf("endpoint did not retain constructor inputs: %+v", endpoint.config)
	}
	for _, baseURL := range []string{"", "not a URL", "/relative", "ftp://example.test/path", "https:///missing-host"} {
		if _, constructErr := NewEndpoint(auth, WithBaseURL(baseURL)); !errors.Is(constructErr, ErrInvalidConfig) {
			t.Errorf("NewEndpoint(WithBaseURL(%q)) error = %v, want ErrInvalidConfig", baseURL, constructErr)
		}
	}
	if _, err := NewEndpoint(nil, WithBaseURL("https://example.test")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil auth error = %v, want ErrInvalidConfig", err)
	}
}

// authWithDefaultBaseURL is a test-only Authenticator that also satisfies
// endpointDefaultBaseURL, standing in for the offering-derived appliers
// (apiKeyApplier, oauthApplier) without needing a full catalog fixture.
type authWithDefaultBaseURL struct {
	authFunc
	base string
}

func (a authWithDefaultBaseURL) defaultBaseURL() string { return a.base }

// R-K0P0-NOUN
func TestNewEndpointWithBaseURLOverridesDefaultAndLastCallWins(t *testing.T) {
	auth := authWithDefaultBaseURL{
		authFunc: authFunc(func(context.Context, *http.Request, []byte) error { return nil }),
		base:     "https://default-from-offering.invalid/v1",
	}

	// WithBaseURL overrides the authenticator's own default.
	endpoint, err := NewEndpoint(auth, WithBaseURL("https://override.invalid/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if got := endpoint.config.baseURL.String(); got != "https://override.invalid/v1" {
		t.Fatalf("baseURL = %q, want override %q, not the authenticator default %q", got, "https://override.invalid/v1", auth.base)
	}

	// Repeated WithBaseURL: the last call wins.
	endpoint, err = NewEndpoint(auth,
		WithBaseURL("https://first.invalid/v1"),
		WithBaseURL("https://second.invalid/v1"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := endpoint.config.baseURL.String(); got != "https://second.invalid/v1" {
		t.Fatalf("baseURL = %q, want last WithBaseURL %q to win", got, "https://second.invalid/v1")
	}
}

// R-JZH4-9X3Y
func TestNewEndpointDefaultsToOfferingBaseURLForAuthenticatorsAuthMode(t *testing.T) {
	const wantBaseURL = "https://api.anthropic.com/v1/messages"
	offering, err := Lookup("claude-sonnet-5", HostAnthropic, WireMessages)
	if err != nil {
		t.Fatal(err)
	}
	rotator := APIKeyRotator("test-key")
	matchedFixture := false
	for _, spec := range offering.Endpoints {
		if spec.AuthMode == rotator.AuthMode() {
			matchedFixture = true
			if spec.BaseURL != wantBaseURL {
				t.Fatalf("fixture endpoint BaseURL = %q, want %q", spec.BaseURL, wantBaseURL)
			}
			break
		}
	}
	if !matchedFixture {
		t.Fatalf("fixture offering %s has no %s EndpointSpec", offering.ID, rotator.AuthMode())
	}
	auth, err := offering.Authenticator(rotator)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := NewEndpoint(auth)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.config.baseURL.String() != wantBaseURL {
		t.Fatalf("default baseURL = %q, want %q", endpoint.config.baseURL.String(), wantBaseURL)
	}
}

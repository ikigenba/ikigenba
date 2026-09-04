package agentkit

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// R-P5PA-T4A1
func TestAuthModeContract(t *testing.T) {
	if reflect.TypeFor[AuthMode]().Kind() != reflect.String {
		t.Fatalf("AuthMode underlying kind = %s, want string", reflect.TypeFor[AuthMode]().Kind())
	}
	if AuthModeAPIKey != AuthMode("api_key") || AuthModeOAuth != AuthMode("oauth") {
		t.Fatalf("auth modes = %q, %q; want api_key, oauth", AuthModeAPIKey, AuthModeOAuth)
	}
}

// R-P6X7-6W0Q
func TestCredentialIsSealedWithExactConstructors(t *testing.T) {
	checkCredentialInterface(t)
	checkCredentialConstructorSignatures(t)
	checkCredentialConstructorBehavior(t)
	checkCredentialExports(t)
}

func checkCredentialInterface(t *testing.T) {
	t.Helper()
	credentialType := reflect.TypeFor[Credential]()
	if credentialType.Kind() != reflect.Interface || credentialType.NumMethod() != 2 {
		t.Fatalf("Credential = %s with %d methods, want interface with 2 methods", credentialType, credentialType.NumMethod())
	}
	for index, name := range []string{"isCredential", "mode"} {
		method := credentialType.Method(index)
		if method.Name != name || method.PkgPath == "" {
			t.Fatalf("Credential method %d = %s (package %q), want unexported %s", index, method.Name, method.PkgPath, name)
		}
	}
}

func checkCredentialConstructorSignatures(t *testing.T) {
	t.Helper()
	wantAPIKeySignature := reflect.TypeOf(func(string) Credential { return nil })
	wantOAuthSignature := reflect.TypeOf(func(TokenSource) Credential { return nil })
	if got := reflect.TypeOf(APIKey); got != wantAPIKeySignature || got.IsVariadic() {
		t.Fatalf("APIKey = %s variadic=%t, want %s non-variadic", got, got.IsVariadic(), wantAPIKeySignature)
	}
	if got := reflect.TypeOf(OAuth); got != wantOAuthSignature || got.IsVariadic() {
		t.Fatalf("OAuth = %s variadic=%t, want %s non-variadic", got, got.IsVariadic(), wantOAuthSignature)
	}
}

func checkCredentialConstructorBehavior(t *testing.T) {
	t.Helper()
	source := &tokenSourceStub{}
	if credential := APIKey("secret"); credential.mode() != AuthModeAPIKey {
		t.Fatalf("APIKey mode = %q, want %q", credential.mode(), AuthModeAPIKey)
	}
	if credential := OAuth(source); credential.mode() != AuthModeOAuth {
		t.Fatalf("OAuth mode = %q, want %q", credential.mode(), AuthModeOAuth)
	}
	if got := OAuth(source).(oauthCredential).source; got != source {
		t.Fatal("OAuth did not retain its TokenSource")
	}
}

func checkCredentialExports(t *testing.T) {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), "credential.go", nil, 0)
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
	if !reflect.DeepEqual(exportedFunctions, []string{"APIKey", "OAuth"}) {
		t.Fatalf("credential exports functions %v, want only APIKey and OAuth", exportedFunctions)
	}
}

// R-KGL5-6PI4
func TestOfferingAuthenticatorDeclarationIsExact(t *testing.T) {
	offeringType := reflect.TypeFor[Offering]()
	method, ok := offeringType.MethodByName("Authenticator")
	wantType := reflect.TypeOf(func(Offering, Credential) (Authenticator, error) { return nil, nil })
	if !ok || method.Type != wantType {
		t.Fatalf("Offering.Authenticator = %v (present=%t), want %s", method.Type, ok, wantType)
	}
	if oldMethod, exists := offeringType.MethodByName("Auth"); exists {
		t.Fatalf("Offering.Auth still exported with type %s", oldMethod.Type)
	}

	offering := Offering{ID: OfferingAnthropicMessages, AuthModes: []AuthMode{AuthModeAPIKey}}
	if _, err := offering.Authenticator(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Authenticator(nil) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := offering.Authenticator(OAuth(&tokenSourceStub{})); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Authenticator(OAuth) error = %v, want ErrInvalidConfig for unsupported mode", err)
	}
	authenticator, err := offering.Authenticator(APIKey("secret"))
	if err != nil || authenticator == nil {
		t.Fatalf("Authenticator(APIKey) = %v, %v; want non-nil authenticator and nil error", authenticator, err)
	}
}

// R-KK8U-C0Q7
func TestConversationIdentityFollowsOfferingAndCredential(t *testing.T) {
	offering := Offering{
		ID:         OfferingID("identity-test-offering"),
		WireFormat: ChatWire(),
		AuthModes:  []AuthMode{AuthModeAPIKey, AuthModeOAuth},
	}
	credentials := []struct {
		name     string
		value    Credential
		wantMode string
	}{
		{name: "API key", value: APIKey("secret"), wantMode: "api_key"},
		{name: "OAuth", value: OAuth(&tokenSourceStub{}), wantMode: "oauth"},
	}
	urls := []string{
		"https://example.test",
		"https://proxy.test/v1/custom-path",
	}

	for _, credential := range credentials {
		t.Run(credential.name, func(t *testing.T) {
			for _, rawURL := range urls {
				t.Run(rawURL, func(t *testing.T) {
					authenticator, err := offering.Authenticator(credential.value)
					if err != nil {
						t.Fatal(err)
					}
					endpoint, err := NewEndpoint(rawURL, authenticator)
					if err != nil {
						t.Fatal(err)
					}
					conversation, err := New(offering.WireFormat, endpoint, "identity-model", Config{})
					if err != nil {
						t.Fatal(err)
					}
					if got := conversation.identity.Endpoint; got != string(offering.ID) {
						t.Fatalf("Identity.Endpoint = %q, want %q for URL %q", got, offering.ID, rawURL)
					}
					if got := conversation.identity.AuthMode; got != credential.wantMode {
						t.Fatalf("Identity.AuthMode = %q, want %q for URL %q", got, credential.wantMode, rawURL)
					}
				})
			}
		})
	}
}

// R-KHT1-KH8T
func TestAPIKeyPlacementFollowsWireFormat(t *testing.T) {
	checks := []struct {
		name         string
		wire         WireFormat
		wantHeader   string
		wantQueryKey string
	}{
		{name: "anthropic", wire: AnthropicMessagesWire(), wantHeader: "x-api-key"},
		{name: "gemini", wire: GeminiGenerateContentWire(), wantQueryKey: "secret"},
		{name: "chat", wire: ChatWire(), wantHeader: "Authorization"},
		{name: "responses", wire: ResponsesWire(), wantHeader: "Authorization"},
		{name: "OpenAI chat", wire: OpenAIChatWire(), wantHeader: "Authorization"},
		{name: "OpenAI responses", wire: OpenAIResponsesWire(), wantHeader: "Authorization"},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			offering := Offering{
				ID:         OfferingID("custom-offering"),
				WireFormat: check.wire,
				AuthModes:  []AuthMode{AuthModeAPIKey},
			}
			authenticator, err := offering.Authenticator(APIKey("secret"))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "https://example.test?alt=sse", nil)
			if err := authenticator.Authenticate(context.Background(), request, nil); err != nil {
				t.Fatal(err)
			}

			if got := request.URL.Query().Get("alt"); got != "sse" {
				t.Fatalf("existing alt query = %q, want sse", got)
			}
			if got := request.URL.Query().Get("key"); got != check.wantQueryKey {
				t.Fatalf("key query = %q, want %q", got, check.wantQueryKey)
			}
			wantHeaderValue := "secret"
			if check.wantHeader == "Authorization" {
				wantHeaderValue = "Bearer secret"
			}
			for _, header := range []string{"x-api-key", "Authorization"} {
				want := ""
				if header == check.wantHeader {
					want = wantHeaderValue
				}
				if got := request.Header.Get(header); got != want {
					t.Fatalf("%s = %q, want %q", header, got, want)
				}
			}
		})
	}
}

// R-KJ0X-Y8ZI
func TestOAuthApplicationFollowsWireFormat(t *testing.T) {
	t.Run("token is resolved for every request", func(t *testing.T) {
		source := &tokenSourceStub{token: Token{Bearer: "token", AccountID: "ignored"}}
		authenticator, err := (Offering{
			ID:         OfferingID("custom-offering"),
			WireFormat: ChatWire(),
			AuthModes:  []AuthMode{AuthModeOAuth},
		}).Authenticator(OAuth(source))
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			request := httptest.NewRequest(http.MethodPost, "https://example.test", nil)
			if err := authenticator.Authenticate(context.Background(), request, nil); err != nil {
				t.Fatal(err)
			}
			if got := request.Header.Get("Authorization"); got != "Bearer token" {
				t.Fatalf("Authorization = %q, want Bearer token", got)
			}
			if got := request.Header.Get("ChatGPT-Account-Id"); got != "" {
				t.Fatalf("ChatGPT-Account-Id = %q, want empty", got)
			}
		}
		if source.calls != 2 {
			t.Fatalf("Token calls = %d, want 2", source.calls)
		}
	})

	for _, check := range []struct {
		name string
		wire WireFormat
	}{
		{name: "OpenAI chat", wire: OpenAIChatWire()},
		{name: "OpenAI responses", wire: OpenAIResponsesWire()},
	} {
		t.Run(check.name, func(t *testing.T) {
			for _, accountID := range []string{"account", ""} {
				source := &tokenSourceStub{token: Token{Bearer: "token", AccountID: accountID}}
				authenticator, err := (Offering{
					ID:         OfferingID("custom-offering"),
					WireFormat: check.wire,
					AuthModes:  []AuthMode{AuthModeOAuth},
				}).Authenticator(OAuth(source))
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest(http.MethodPost, "https://example.test", nil)
				authErr := authenticator.Authenticate(context.Background(), request, nil)
				if accountID == "" {
					if !errors.Is(authErr, ErrInvalidConfig) {
						t.Fatalf("Authenticate error = %v, want ErrInvalidConfig", authErr)
					}
					continue
				}
				if authErr != nil {
					t.Fatal(authErr)
				}
				if got := request.Header.Get("Authorization"); got != "Bearer token" {
					t.Fatalf("Authorization = %q, want Bearer token", got)
				}
				if got := request.Header.Get("ChatGPT-Account-Id"); got != accountID {
					t.Fatalf("ChatGPT-Account-Id = %q, want %q", got, accountID)
				}
			}
		})
	}

	t.Run("nil source", func(t *testing.T) {
		authenticator, err := (Offering{
			ID:         OfferingID("custom-offering"),
			WireFormat: ChatWire(),
			AuthModes:  []AuthMode{AuthModeOAuth},
		}).Authenticator(OAuth(nil))
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "https://example.test", nil)
		if err := authenticator.Authenticate(context.Background(), request, nil); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("Authenticate error = %v, want ErrInvalidConfig", err)
		}
	})

	t.Run("token error is unchanged", func(t *testing.T) {
		tokenErr := errors.New("token failed")
		authenticator, err := (Offering{
			ID:         OfferingID("custom-offering"),
			WireFormat: ChatWire(),
			AuthModes:  []AuthMode{AuthModeOAuth},
		}).Authenticator(OAuth(&tokenSourceStub{err: tokenErr}))
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "https://example.test", nil)
		if err := authenticator.Authenticate(context.Background(), request, nil); !errors.Is(err, tokenErr) {
			t.Fatalf("Authenticate error = %v, want %v", err, tokenErr)
		}
	})
}

type tokenSourceStub struct {
	token        Token
	err          error
	calls        int
	refreshToken Token
	refreshErr   error
}

func (s *tokenSourceStub) Token(context.Context) (Token, error) {
	s.calls++
	return s.token, s.err
}

func (s *tokenSourceStub) Refresh(context.Context) (Token, error) {
	if s.refreshErr != nil {
		return s.refreshToken, s.refreshErr
	}
	if s.err != nil {
		return s.token, s.err
	}
	if s.refreshToken != (Token{}) {
		s.token = s.refreshToken
	}
	return s.token, nil
}

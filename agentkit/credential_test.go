package agentkit

import (
	"context"
	"errors"
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

// R-K1WX-1GLC
func TestTokenAndRotatorContractExcludeRetiredCredentialTypes(t *testing.T) {
	tokenType := reflect.TypeFor[Token]()
	if tokenType.NumField() != 2 {
		t.Fatalf("Token has %d fields, want 2", tokenType.NumField())
	}
	if field, ok := tokenType.FieldByName("Bearer"); !ok || field.Type.Kind() != reflect.String {
		t.Fatalf("Token.Bearer missing or wrong type: %+v, ok=%v", field, ok)
	}
	if field, ok := tokenType.FieldByName("AccountID"); !ok || field.Type.Kind() != reflect.String {
		t.Fatalf("Token.AccountID missing or wrong type: %+v, ok=%v", field, ok)
	}

	rotatorType := reflect.TypeFor[Rotator]()
	if rotatorType.Kind() != reflect.Interface {
		t.Fatalf("Rotator kind = %s, want Interface", rotatorType.Kind())
	}
	if rotatorType.NumMethod() != 3 {
		t.Fatalf("Rotator has %d methods, want 3 (AuthMode, Token, Rotate)", rotatorType.NumMethod())
	}
	for _, name := range []string{"AuthMode", "Token", "Rotate"} {
		if _, ok := rotatorType.MethodByName(name); !ok {
			t.Fatalf("Rotator missing method %s", name)
		}
	}
}

type tokenSourceStub struct {
	token        Token
	err          error
	refreshToken Token
	refreshErr   error
}

func (s *tokenSourceStub) AuthMode() AuthMode { return AuthModeOAuth }

func (s *tokenSourceStub) Token(context.Context) (Token, error) {
	return s.token, s.err
}

func (s *tokenSourceStub) Rotate(context.Context, Rotation) (Token, error) {
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

type rotatorStub struct {
	mode   AuthMode
	tokens []Token
	err    error
	calls  int
}

func (s *rotatorStub) AuthMode() AuthMode { return s.mode }

func (s *rotatorStub) Token(context.Context) (Token, error) {
	s.calls++
	if s.err != nil {
		return Token{}, s.err
	}
	return s.tokens[(s.calls-1)%len(s.tokens)], nil
}

func (*rotatorStub) Rotate(context.Context, Rotation) (Token, error) { return Token{}, nil }

// R-K5KM-6RTF
func TestOfferingAuthenticatorRequiresAcceptedRotator(t *testing.T) {
	wantSignature := reflect.TypeOf(func(Offering, Rotator) (Authenticator, error) { return nil, nil })
	if got := reflect.TypeOf(Offering.Authenticator); got != wantSignature {
		t.Fatalf("Offering.Authenticator type = %s, want %s", got, wantSignature)
	}

	offering := Offering{ID: OfferingAnthropicMessages, Endpoints: []EndpointSpec{{AuthMode: AuthModeAPIKey}}}
	if _, err := offering.Authenticator(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Authenticator(nil) error = %v, want ErrInvalidConfig", err)
	}
	unmatched := &rotatorStub{mode: AuthModeOAuth}
	if _, err := offering.Authenticator(unmatched); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Authenticator(unmatched) error = %v, want ErrInvalidConfig", err)
	}

	offeringType := reflect.TypeFor[Offering]()
	for _, retired := range []string{"Auth", "TokenSource"} {
		if _, exists := offeringType.MethodByName(retired); exists {
			t.Fatalf("Offering still exports retired method %s", retired)
		}
	}
}

// R-K6SI-KJK4
func TestAPIKeyAuthenticatorUsesRotatorTokenForSpecifiedWiresAndEachRequest(t *testing.T) {
	tests := []struct {
		name       string
		wire       WireFormat
		wantHeader string
		wantQuery  string
	}{
		{name: "anthropic", wire: AnthropicMessagesWire(), wantHeader: "x-api-key"},
		{name: "gemini", wire: GeminiGenerateContentWire(), wantQuery: "key"},
		{name: "chat", wire: ChatWire(), wantHeader: "Authorization"},
		{name: "responses", wire: ResponsesWire(), wantHeader: "Authorization"},
		{name: "openai chat", wire: OpenAIChatWire(), wantHeader: "Authorization"},
		{name: "openai responses", wire: OpenAIResponsesWire(), wantHeader: "Authorization"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rotator := &rotatorStub{mode: AuthModeAPIKey, tokens: []Token{{Bearer: "first"}, {Bearer: "second"}}}
			offering := Offering{ID: OfferingAnthropicMessages, WireFormat: test.wire, Endpoints: []EndpointSpec{{AuthMode: AuthModeAPIKey}}}
			authenticator, err := offering.Authenticator(rotator)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("GET", "https://example.test/path?keep=yes", nil)
			for call, bearer := range []string{"first", "second"} {
				if err := authenticator.Authenticate(context.Background(), request, nil); err != nil {
					t.Fatalf("Authenticate call %d: %v", call+1, err)
				}
				if test.wantQuery != "" && request.URL.Query().Get(test.wantQuery) != bearer {
					t.Fatalf("query %s after call %d = %q, want %q", test.wantQuery, call+1, request.URL.Query().Get(test.wantQuery), bearer)
				}
				if test.wantHeader != "" {
					want := bearer
					if test.wantHeader == "Authorization" {
						want = "Bearer " + bearer
					}
					if got := request.Header.Get(test.wantHeader); got != want {
						t.Fatalf("header %s after call %d = %q, want %q", test.wantHeader, call+1, got, want)
					}
				}
			}
			if rotator.calls != 2 {
				t.Fatalf("Token calls = %d, want 2", rotator.calls)
			}
		})
	}

	tokenErr := errors.New("token failed")
	rotator := &rotatorStub{mode: AuthModeAPIKey, err: tokenErr}
	authenticator, err := (Offering{WireFormat: ChatWire(), Endpoints: []EndpointSpec{{AuthMode: AuthModeAPIKey}}}).Authenticator(rotator)
	if err != nil {
		t.Fatal(err)
	}
	if got := authenticator.Authenticate(context.Background(), httptest.NewRequest("GET", "https://example.test", nil), nil); !errors.Is(got, tokenErr) {
		t.Fatalf("Authenticate error = %v, want exact Token error %v", got, tokenErr)
	}
}

// R-K98B-C31I
func TestOAuthAuthenticatorUsesRotatorTokenAndOpenAIAccountID(t *testing.T) {
	rotator := &rotatorStub{mode: AuthModeOAuth, tokens: []Token{
		{Bearer: "first", AccountID: "account-1"},
		{Bearer: "second", AccountID: "account-2"},
	}}
	offering := Offering{ID: OfferingOpenAIResponses, WireFormat: OpenAIResponsesWire(), Endpoints: []EndpointSpec{{AuthMode: AuthModeOAuth}}}
	authenticator, err := offering.Authenticator(rotator)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "https://example.test", nil)
	for call, token := range rotator.tokens {
		if err := authenticator.Authenticate(context.Background(), request, nil); err != nil {
			t.Fatalf("Authenticate call %d: %v", call+1, err)
		}
		if got, want := request.Header.Get("Authorization"), "Bearer "+token.Bearer; got != want {
			t.Fatalf("Authorization after call %d = %q, want %q", call+1, got, want)
		}
		if got := request.Header.Get("ChatGPT-Account-Id"); got != token.AccountID {
			t.Fatalf("ChatGPT-Account-Id after call %d = %q, want %q", call+1, got, token.AccountID)
		}
	}
	if rotator.calls != 2 {
		t.Fatalf("Token calls = %d, want 2", rotator.calls)
	}

	t.Run("generic OAuth wires use bearer header", func(t *testing.T) {
		value := "oauth-token"
		for _, wire := range []WireFormat{AnthropicMessagesWire(), GeminiGenerateContentWire(), ChatWire(), ResponsesWire()} {
			r := &rotatorStub{mode: AuthModeOAuth, tokens: []Token{{Bearer: value}}}
			auth, authErr := (Offering{WireFormat: wire, Endpoints: []EndpointSpec{{AuthMode: AuthModeOAuth}}}).Authenticator(r)
			if authErr != nil {
				t.Fatal(authErr)
			}
			req := httptest.NewRequest("GET", "https://example.test", nil)
			if authErr := auth.Authenticate(context.Background(), req, nil); authErr != nil {
				t.Fatal(authErr)
			}
			if got, want := req.Header.Get("Authorization"), "Bearer "+value; got != want {
				t.Fatalf("Authorization = %q, want %q", got, want)
			}
		}
	})

	t.Run("OpenAI account ID is required", func(t *testing.T) {
		value := "oauth-token"
		r := &rotatorStub{mode: AuthModeOAuth, tokens: []Token{{Bearer: value}}}
		auth, authErr := offering.Authenticator(r)
		if authErr != nil {
			t.Fatal(authErr)
		}
		if got := auth.Authenticate(context.Background(), httptest.NewRequest("GET", "https://example.test", nil), nil); !errors.Is(got, ErrInvalidConfig) {
			t.Fatalf("Authenticate error = %v, want ErrInvalidConfig", got)
		}
	})

	t.Run("Token error is unchanged", func(t *testing.T) {
		tokenErr := errors.New("token failed")
		r := &rotatorStub{mode: AuthModeOAuth, err: tokenErr}
		auth, authErr := offering.Authenticator(r)
		if authErr != nil {
			t.Fatal(authErr)
		}
		if got := auth.Authenticate(context.Background(), httptest.NewRequest("GET", "https://example.test", nil), nil); !errors.Is(got, tokenErr) {
			t.Fatalf("Authenticate error = %v, want exact Token error %v", got, tokenErr)
		}
	})
}

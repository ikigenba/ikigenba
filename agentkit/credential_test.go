package agentkit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// R-P5PA-T4A1
func TestAuthModeContract(t *testing.T) {
	mode := AuthMode("custom")
	if string(mode) != "custom" {
		t.Fatalf("string(AuthMode(%q)) = %q, want custom", "custom", string(mode))
	}
	switch any(mode).(type) {
	case AuthMode:
	case string:
		t.Fatal("AuthMode value matched case string, want case AuthMode")
	default:
		t.Fatalf("AuthMode value has dynamic type %T, want AuthMode", mode)
	}
	modes := []AuthMode{inferredType(AuthModeAPIKey), inferredType(AuthModeOAuth)}
	if modes[0] != AuthMode("api_key") || modes[1] != AuthMode("oauth") {
		t.Fatalf("auth modes = %q, %q; want api_key, oauth", AuthModeAPIKey, AuthModeOAuth)
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
	mode        AuthMode
	tokens      []Token
	err         error
	calls       int
	rotateToken Token
	rotateErr   error
	rotateCalls int
	gotRotation Rotation
}

func (s *rotatorStub) AuthMode() AuthMode { return s.mode }

func (s *rotatorStub) Token(context.Context) (Token, error) {
	s.calls++
	if s.err != nil {
		return Token{}, s.err
	}
	return s.tokens[(s.calls-1)%len(s.tokens)], nil
}

func (s *rotatorStub) Rotate(_ context.Context, rotation Rotation) (Token, error) {
	s.rotateCalls++
	s.gotRotation = rotation
	if s.rotateErr != nil {
		return Token{}, s.rotateErr
	}
	return s.rotateToken, nil
}

// R-O9D5-TK1H
func TestOfferingAuthenticatorRequiresAcceptedRotator(t *testing.T) {
	offering := Offering{ID: OfferingAnthropicMessages, Endpoints: []EndpointSpec{{AuthMode: AuthModeAPIKey}}}
	if _, err := offering.Authenticator(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Authenticator(nil) error = %v, want ErrInvalidConfig", err)
	}
	unmatched := &rotatorStub{mode: AuthModeOAuth}
	if _, err := offering.Authenticator(unmatched); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Authenticator(unmatched) error = %v, want ErrInvalidConfig", err)
	}
}

// R-OS5I-G567
func TestAPIKeyAuthenticatorUsesRotatorTokenForSpecifiedWiresAndEachRequest(t *testing.T) {
	tests := []struct {
		name       string
		wire       WireFormat
		wantHeader string
	}{
		{name: "anthropic", wire: AnthropicMessagesWire(), wantHeader: "x-api-key"},
		{name: "gemini", wire: GeminiGenerateContentWire(), wantHeader: "x-goog-api-key"},
		{name: "chat", wire: ChatWire(), wantHeader: "Authorization"},
		{name: "responses", wire: ResponsesWire(), wantHeader: "Authorization"},
		{name: "openai chat", wire: OpenAIChatWire(), wantHeader: "Authorization"},
		{name: "openai responses", wire: OpenAIResponsesWire(), wantHeader: "Authorization"},
		{name: "xai chat", wire: XAIChatWire(), wantHeader: "Authorization"},
		{name: "xai responses", wire: XAIResponsesWire(), wantHeader: "Authorization"},
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
	if got := authenticator.Authenticate(context.Background(), httptest.NewRequest("GET", "https://example.test", nil), nil); any(got) != any(tokenErr) {
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

// R-J6FV-1PBM
func TestOAuthRefreshWindow(t *testing.T) {
	const window = OAuthRefreshWindow
	var _ = func() time.Duration { return inferredType(window) }
	if window != 5*time.Minute {
		t.Fatalf("OAuthRefreshWindow = %s, want %s", window, 5*time.Minute)
	}
}

// inferredType returns its argument as the type Go infers for it, so an
// untyped constant comes back as its default type rather than converting to
// the variable it is assigned to.
func inferredType[T any](v T) T { return v }

// R-J7NR-FH2B
func TestOAuthAuthenticatorProactivelyRotatesExpiringTokens(t *testing.T) {
	rotation := Rotation{RefreshURL: "https://unused.test", ClientID: "client-id"}
	tests := []struct {
		name       string
		expiresAt  func() time.Time
		wire       WireFormat
		wantRotate bool
	}{
		{name: "unknown expiry", expiresAt: func() time.Time { return time.Time{} }, wire: ChatWire()},
		{name: "after window", expiresAt: func() time.Time { return time.Now().Add(OAuthRefreshWindow + time.Minute) }, wire: ChatWire()},
		{name: "inside window", expiresAt: func() time.Time { return time.Now().Add(OAuthRefreshWindow - time.Minute) }, wire: OpenAIResponsesWire(), wantRotate: true},
		{name: "at boundary", expiresAt: func() time.Time { return time.Now().Add(OAuthRefreshWindow) }, wire: ChatWire(), wantRotate: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stored := Token{Bearer: "stored-bearer", AccountID: "stored-account", ExpiresAt: test.expiresAt()}
			rotated := Token{Bearer: "rotated-bearer", AccountID: "rotated-account"}
			rotator := &rotatorStub{mode: AuthModeOAuth, tokens: []Token{stored}, rotateToken: rotated}
			offering := Offering{
				ID:         OfferingOpenAIResponses,
				WireFormat: test.wire,
				Endpoints:  []EndpointSpec{{AuthMode: AuthModeOAuth, Rotation: rotation}},
			}
			authenticator, err := offering.Authenticator(rotator)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "https://example.test", nil)
			if err := authenticator.Authenticate(context.Background(), request, nil); err != nil {
				t.Fatal(err)
			}

			wantRotateCalls := 0
			wantToken := stored
			if test.wantRotate {
				wantRotateCalls = 1
				wantToken = rotated
			}
			if rotator.calls != 1 {
				t.Fatalf("Token calls = %d, want 1", rotator.calls)
			}
			if rotator.rotateCalls != wantRotateCalls {
				t.Fatalf("Rotate calls = %d, want %d", rotator.rotateCalls, wantRotateCalls)
			}
			if test.wantRotate && rotator.gotRotation != rotation {
				t.Fatalf("Rotate rotation = %#v, want %#v", rotator.gotRotation, rotation)
			}
			if got, want := request.Header.Get("Authorization"), "Bearer "+wantToken.Bearer; got != want {
				t.Fatalf("Authorization = %q, want %q", got, want)
			}
			if _, openAIWire := test.wire.(*openAIResponsesWire); openAIWire {
				if got := request.Header.Get("ChatGPT-Account-Id"); got != wantToken.AccountID {
					t.Fatalf("ChatGPT-Account-Id = %q, want %q", got, wantToken.AccountID)
				}
			}
		})
	}
}

// R-EBV0-BHS5
func TestOAuthProactiveRotationFailureStopsRequest(t *testing.T) {
	rotation := Rotation{RefreshURL: "https://unused.test", ClientID: "client-id"}
	rotateErr := &Error{Category: CategoryAuth, Status: http.StatusBadRequest, Code: "invalid_grant"}
	newRotator := func() *rotatorStub {
		return &rotatorStub{
			mode:      AuthModeOAuth,
			tokens:    []Token{{Bearer: "stored-bearer", ExpiresAt: time.Now().Add(time.Minute)}},
			rotateErr: rotateErr,
		}
	}

	t.Run("Authenticate returns error without applying credentials", func(t *testing.T) {
		rotator := newRotator()
		offering := Offering{WireFormat: ChatWire(), Endpoints: []EndpointSpec{{AuthMode: AuthModeOAuth, Rotation: rotation}}}
		authenticator, err := offering.Authenticator(rotator)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "https://example.test", nil)
		if got := authenticator.Authenticate(context.Background(), request, nil); !errors.Is(got, rotateErr) {
			t.Fatalf("Authenticate error = %v, want exact error %v", got, rotateErr)
		}
		if rotator.calls != 1 || rotator.rotateCalls != 1 {
			t.Fatalf("Token calls, Rotate calls = %d, %d; want 1, 1", rotator.calls, rotator.rotateCalls)
		}
		if got := request.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
	})

	t.Run("Conversation sends no request and preserves provider error", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			t.Error("Conversation sent a request after proactive rotation failed")
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		rotator := newRotator()
		offering := Offering{
			WireFormat: ChatWire(),
			Endpoints: []EndpointSpec{{
				AuthMode: AuthModeOAuth,
				BaseURL:  server.URL,
				Rotation: rotation,
			}},
		}
		authenticator, err := offering.Authenticator(rotator)
		if err != nil {
			t.Fatal(err)
		}
		endpoint, err := NewEndpoint(authenticator)
		if err != nil {
			t.Fatal(err)
		}
		conversation, err := New(ChatWire(), endpoint, "test-model", Config{})
		if err != nil {
			t.Fatal(err)
		}
		stream := conversation.Send(context.Background(), Text{Text: "hello"})
		for event := range stream.Events() {
			t.Errorf("Send emitted unexpected event after authentication failure: %#v", event)
		}
		var providerErr *Error
		if !errors.As(stream.Err(), &providerErr) || providerErr != rotateErr {
			t.Fatalf("Send error = %v, want preserved error %v", stream.Err(), rotateErr)
		}
		if got := requests.Load(); got != 0 {
			t.Fatalf("Conversation sent %d requests, want none", got)
		}
	})
}

var _ func(Offering, Rotator) (Authenticator, error) = Offering.Authenticator

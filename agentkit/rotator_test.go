package agentkit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// R-K34T-F8C1
func TestAPIKeyRotator(t *testing.T) {
	rotator := APIKeyRotator("secret-key")

	if got, want := rotator.AuthMode(), AuthMode("api_key"); got != want {
		t.Fatalf("AuthMode() = %q, want %q", got, want)
	}

	token, err := rotator.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if want := (Token{Bearer: "secret-key"}); token != want {
		t.Fatalf("Token() = %#v, want %#v", token, want)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)

	_, err = rotator.Rotate(context.Background(), Rotation{
		RefreshURL: server.URL,
		ClientID:   "client",
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Rotate() error = %v, want ErrInvalidConfig", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Rotate() made %d network requests, want 0", got)
	}
}

// R-K4CP-T02Q
func TestOAuthRotatorAuthMode(t *testing.T) {
	rotator := OAuthRotator(nil)

	if got, want := rotator.AuthMode(), AuthMode("oauth"); got != want {
		t.Fatalf("AuthMode() = %q, want %q", got, want)
	}
}

// fakeTokenStore uses a mutex so read-count assertions remain race-safe.
type fakeTokenStore struct {
	mu       sync.Mutex
	data     []byte
	err      error
	writeErr error
	reads    int
	writes   int
}

func (s *fakeTokenStore) Read(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	return append([]byte(nil), s.data...), s.err
}

func (s *fakeTokenStore) Write(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.writeErr != nil {
		return s.writeErr
	}
	s.data = append(s.data[:0], data...)
	return nil
}

func (s *fakeTokenStore) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

func (s *fakeTokenStore) writeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

func (s *fakeTokenStore) storedData() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.data...)
}

// R-KP30-B3OJ
func TestOAuthRotatorTokenReadsOnceAndCaches(t *testing.T) {
	ctx := context.Background()
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-1"}}`))
	jwt := "header." + payload + ".signature"
	store := &fakeTokenStore{data: []byte(`{"access_token":"` + jwt + `","refresh_token":"r"}`)}
	rotator := OAuthRotator(store)

	if got := store.readCount(); got != 0 {
		t.Fatalf("OAuthRotator() made %d store reads, want 0", got)
	}
	for i := 0; i < 2; i++ {
		token, err := rotator.Token(ctx)
		if err != nil {
			t.Fatalf("Token() call %d error = %v", i+1, err)
		}
		if want := (Token{Bearer: jwt, AccountID: "acct-1"}); token != want {
			t.Fatalf("Token() call %d = %#v, want %#v", i+1, token, want)
		}
	}
	if got := store.readCount(); got != 1 {
		t.Fatalf("two Token() calls made %d store reads, want 1", got)
	}

	sentinel := errors.New("read failed")
	_, err := OAuthRotator(&fakeTokenStore{err: sentinel}).Token(ctx)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Token() error = %v, want unchanged sentinel error", err)
	}

	for name, data := range map[string][]byte{
		"missing access token": []byte(`{}`),
		"invalid JSON":         []byte(`not-json`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := OAuthRotator(&fakeTokenStore{data: data}).Token(ctx)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Token() error = %v, want ErrInvalidConfig", err)
			}
		})
	}

	token, err := OAuthRotator(&fakeTokenStore{data: []byte(`{"access_token":"opaque"}`)}).Token(ctx)
	if err != nil {
		t.Fatalf("Token() with opaque bearer error = %v", err)
	}
	if want := (Token{Bearer: "opaque"}); token != want {
		t.Fatalf("Token() with opaque bearer = %#v, want %#v", token, want)
	}
}

// R-KQAW-OVF8
func TestOAuthRotatorRotateSendsRefreshGrant(t *testing.T) {
	type recordedRequest struct {
		method      string
		contentType string
		accept      string
		form        url.Values
	}
	recorded := make(chan recordedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		recorded <- recordedRequest{
			method:      r.Method,
			contentType: r.Header.Get("Content-Type"),
			accept:      r.Header.Get("Accept"),
			form:        r.PostForm,
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	store := &fakeTokenStore{data: []byte(`{"access_token":"a","refresh_token":"r-1"}`)}
	rotator := OAuthRotator(store)
	_, _ = rotator.Rotate(context.Background(), Rotation{RefreshURL: server.URL, ClientID: "client-1"})

	if got := len(recorded); got != 1 {
		t.Fatalf("Rotate() made %d requests, want exactly 1", got)
	}
	request := <-recorded
	if request.method != http.MethodPost {
		t.Errorf("request method = %q, want POST", request.method)
	}
	if request.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", request.contentType)
	}
	if request.accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", request.accept)
	}
	wantForm := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"r-1"},
		"client_id":     {"client-1"},
	}
	if !reflect.DeepEqual(request.form, wantForm) {
		t.Errorf("request form = %#v, want exactly %#v", request.form, wantForm)
	}
}

// R-KRIT-2N5X
func TestOAuthRotatorRotatePreconditions(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name     string
		data     []byte
		rotation Rotation
	}{
		{
			name:     "missing refresh token",
			data:     []byte(`{"access_token":"a"}`),
			rotation: Rotation{RefreshURL: server.URL, ClientID: "c"},
		},
		{
			name:     "missing refresh URL",
			data:     []byte(`{"access_token":"a","refresh_token":"r"}`),
			rotation: Rotation{ClientID: "c"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rotator := OAuthRotator(&fakeTokenStore{data: test.data})
			_, err := rotator.Rotate(context.Background(), test.rotation)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Rotate() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid Rotate() calls made %d network requests, want 0", got)
	}
}

// R-KSQP-GEWM
func TestOAuthRotatorRotateWritesStoreOnSuccess(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		refresh     string
		wantAccess  string
		wantRefresh string
	}{
		{
			name:        "rotated refresh token",
			response:    `{"access_token":"new-access","refresh_token":"new-refresh"}`,
			refresh:     "old-refresh",
			wantAccess:  "new-access",
			wantRefresh: "new-refresh",
		},
		{
			name:        "refresh token carried forward",
			response:    `{"access_token":"new-access-2"}`,
			refresh:     "keep-me",
			wantAccess:  "new-access-2",
			wantRefresh: "keep-me",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			t.Cleanup(server.Close)

			store := &fakeTokenStore{data: []byte(`{"access_token":"old","refresh_token":"` + test.refresh + `"}`)}
			rotator := OAuthRotator(store)
			token, err := rotator.Rotate(context.Background(), Rotation{RefreshURL: server.URL, ClientID: "c"})
			if err != nil {
				t.Fatalf("Rotate() error = %v", err)
			}
			if want := (Token{Bearer: test.wantAccess}); token != want {
				t.Fatalf("Rotate() token = %#v, want %#v", token, want)
			}
			if got := store.writeCount(); got != 1 {
				t.Fatalf("Rotate() made %d store writes, want exactly 1", got)
			}

			var stored map[string]string
			if err := json.Unmarshal(store.storedData(), &stored); err != nil {
				t.Fatalf("stored token JSON error = %v", err)
			}
			if got := stored["access_token"]; got != test.wantAccess {
				t.Errorf("stored access_token = %q, want %q", got, test.wantAccess)
			}
			if got := stored["refresh_token"]; got != test.wantRefresh {
				t.Errorf("stored refresh_token = %q, want %q", got, test.wantRefresh)
			}

			cached, err := rotator.Token(context.Background())
			if err != nil {
				t.Fatalf("Token() after Rotate() error = %v", err)
			}
			if cached != token {
				t.Fatalf("Token() after Rotate() = %#v, want %#v", cached, token)
			}
			if got := store.readCount(); got != 1 {
				t.Fatalf("Rotate() then Token() made %d store reads, want 1", got)
			}
		})
	}
}

// R-KTYL-U6NB
func TestOAuthRotatorRotateNon2xxAndTransportErrors(t *testing.T) {
	t.Run("OAuth error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"token expired"}`))
		}))
		t.Cleanup(server.Close)

		store := &fakeTokenStore{data: []byte(`{"access_token":"old","refresh_token":"refresh"}`)}
		_, err := OAuthRotator(store).Rotate(context.Background(), Rotation{RefreshURL: server.URL, ClientID: "c"})
		var providerErr *Error
		if !errors.As(err, &providerErr) {
			t.Fatalf("Rotate() error = %T %v, want *Error", err, err)
		}
		if providerErr.Category != CategoryAuth || providerErr.Status != http.StatusUnauthorized ||
			providerErr.Code != "invalid_grant" || providerErr.Message != "token expired" {
			t.Errorf("Rotate() error = %#v, want auth/401/invalid_grant/token expired", providerErr)
		}
		if got := store.writeCount(); got != 0 {
			t.Errorf("failed Rotate() made %d store writes, want 0", got)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		refreshURL := server.URL
		server.Close()

		store := &fakeTokenStore{data: []byte(`{"access_token":"old","refresh_token":"refresh"}`)}
		_, err := OAuthRotator(store).Rotate(context.Background(), Rotation{RefreshURL: refreshURL, ClientID: "c"})
		var providerErr *Error
		if !errors.As(err, &providerErr) {
			t.Fatalf("Rotate() error = %T %v, want *Error", err, err)
		}
		if providerErr.Category != CategoryTransport || providerErr.Status != 0 {
			t.Errorf("Rotate() error = %#v, want transport category and status 0", providerErr)
		}
	})

	t.Run("store write error unchanged", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"new-refresh"}`))
		}))
		t.Cleanup(server.Close)

		sentinel := errors.New("write failed")
		store := &fakeTokenStore{
			data:     []byte(`{"access_token":"old","refresh_token":"refresh"}`),
			writeErr: sentinel,
		}
		_, err := OAuthRotator(store).Rotate(context.Background(), Rotation{RefreshURL: server.URL, ClientID: "c"})
		if !errors.Is(err, sentinel) {
			t.Fatalf("Rotate() error = %v, want unchanged sentinel error", err)
		}
	})
}

// R-KV6I-7YE0
func TestOAuthRotatorRotateConcurrentCallsCollapse(t *testing.T) {
	var requests atomic.Int32
	release := make(chan struct{})
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		select {
		case requestStarted <- struct{}{}:
		default:
		}
		<-release
		_, _ = w.Write([]byte(`{"access_token":"shared-access","refresh_token":"shared-refresh"}`))
	}))
	t.Cleanup(server.Close)

	store := &fakeTokenStore{data: []byte(`{"access_token":"old","refresh_token":"refresh"}`)}
	rotator := OAuthRotator(store)
	type result struct {
		token Token
		err   error
	}
	results := make(chan result, 5)
	rotate := func() {
		token, err := rotator.Rotate(context.Background(), Rotation{RefreshURL: server.URL, ClientID: "c"})
		results <- result{token: token, err: err}
	}
	go rotate()
	<-requestStarted
	var followersStarted sync.WaitGroup
	followersStarted.Add(4)
	for range 4 {
		go func() {
			followersStarted.Done()
			rotate()
		}()
	}
	followersStarted.Wait()
	for range 100 {
		runtime.Gosched()
	}
	close(release)

	for range 5 {
		got := <-results
		if got.err != nil {
			t.Errorf("concurrent Rotate() error = %v", got.err)
		}
		if want := (Token{Bearer: "shared-access"}); got.token != want {
			t.Errorf("concurrent Rotate() token = %#v, want %#v", got.token, want)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("concurrent Rotate() calls made %d requests, want exactly 1", got)
	}
}

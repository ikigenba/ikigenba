package agentkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

// R-KMON-3K7L
func TestOfferingTokenSourceTokenUsesCachedBearerAndOpenAIAccountClaim(t *testing.T) {
	accessToken := "header." + base64.RawURLEncoding.EncodeToString([]byte(
		`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-123"}}`,
	)) + ".signature"

	for _, id := range []OfferingID{OfferingOpenAIResponses, OfferingOpenAIChat} {
		t.Run(string(id), func(t *testing.T) {
			store := &recordingTokenStore{data: []byte(`{"access_token":"` + accessToken + `"}`)}
			source, err := (Offering{ID: id, AuthModes: []AuthMode{AuthModeOAuth}}).TokenSource(store)
			if err != nil {
				t.Fatal(err)
			}

			store.data = nil
			store.err = errors.New("unexpected cached-token read")
			for range 2 {
				got, err := source.Token(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				want := Token{Bearer: accessToken, AccountID: "acct-123"}
				if got != want {
					t.Fatalf("Token() = %+v, want %+v", got, want)
				}
			}
			if store.readCalls != 1 || store.writeCalls != 0 {
				t.Fatalf("store calls after Token() = %d reads, %d writes; want 1 read, 0 writes", store.readCalls, store.writeCalls)
			}
		})
	}

	nonOpenAIStore := &recordingTokenStore{data: []byte(`{"access_token":"` + accessToken + `"}`)}
	nonOpenAI, err := (Offering{ID: OfferingXAIResponses, AuthModes: []AuthMode{AuthModeOAuth}}).TokenSource(nonOpenAIStore)
	if err != nil {
		t.Fatal(err)
	}
	got, err := nonOpenAI.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Bearer != accessToken || got.AccountID != "" {
		t.Fatalf("non-OpenAI Token() = %+v, want bearer with empty AccountID", got)
	}

	for _, malformed := range []string{"opaque", "a.%%%.c", "a.bm90LWpzb24.c", "a.e30.c"} {
		store := &recordingTokenStore{data: []byte(`{"access_token":"` + malformed + `"}`)}
		source, err := (Offering{ID: OfferingOpenAIResponses, AuthModes: []AuthMode{AuthModeOAuth}}).TokenSource(store)
		if err != nil {
			t.Fatal(err)
		}
		got, err := source.Token(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got.Bearer != malformed || got.AccountID != "" {
			t.Errorf("Token() for malformed JWT %q = %+v, want bearer with empty AccountID", malformed, got)
		}
	}
}

type recordingTokenStore struct {
	data       []byte
	err        error
	writeErr   error
	readCalls  int
	writeCalls int
	writes     [][]byte
}

type tokenStoreReadError struct {
	marker int
}

func (*tokenStoreReadError) Error() string { return "read failed" }

type tokenStoreWriteError struct {
	marker int
}

func (*tokenStoreWriteError) Error() string { return "write failed" }

func (s *recordingTokenStore) Read(context.Context) ([]byte, error) {
	s.readCalls++
	return s.data, s.err
}

func (s *recordingTokenStore) Write(_ context.Context, data []byte) error {
	s.writeCalls++
	s.writes = append(s.writes, bytes.Clone(data))
	return s.writeErr
}

// R-ZTL6-6Z6K
func TestOfferingTokenSourceSignature(t *testing.T) {
	want := reflect.TypeOf(func(Offering, TokenStore) (TokenSource, error) { return nil, nil })
	method, ok := reflect.TypeFor[Offering]().MethodByName("TokenSource")
	if !ok {
		t.Fatal("Offering.TokenSource method is missing")
	}
	if method.Type != want {
		t.Fatalf("Offering.TokenSource signature = %s, want %s", method.Type, want)
	}
}

// R-ZUT2-KQX9
func TestOfferingTokenSourceValidatesAndReadsStoreOnce(t *testing.T) {
	offering := Offering{ID: OfferingXAIResponses, AuthModes: []AuthMode{AuthModeOAuth}}

	if _, err := offering.TokenSource(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("TokenSource(nil) error = %v, want ErrInvalidConfig", err)
	}

	unlistedStore := &recordingTokenStore{data: []byte(`{"access_token":"secret"}`)}
	unlisted := Offering{ID: OfferingXAIResponses, AuthModes: []AuthMode{AuthModeAPIKey}}
	if _, err := unlisted.TokenSource(unlistedStore); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("TokenSource() unlisted OAuth error = %v, want ErrInvalidConfig", err)
	}
	if unlistedStore.readCalls != 0 {
		t.Fatalf("unlisted OAuth store reads = %d, want 0", unlistedStore.readCalls)
	}

	readError := &tokenStoreReadError{marker: 1}
	failingStore := &recordingTokenStore{err: readError}
	_, err := offering.TokenSource(failingStore)
	var gotReadError *tokenStoreReadError
	if !errors.As(err, &gotReadError) || reflect.TypeOf(err) != reflect.TypeOf(readError) || gotReadError != readError {
		t.Fatalf("TokenSource() error = %v (%T), want unchanged %p", err, err, readError)
	}
	if failingStore.readCalls != 1 {
		t.Fatalf("failing store reads = %d, want 1", failingStore.readCalls)
	}

	invalidValues := []string{
		`not json`,
		`null`,
		`[]`,
		`{}`,
		`{"Access_Token":"secret"}`,
		`{"access_token":""}`,
		`{"access_token":42}`,
	}
	for _, value := range invalidValues {
		store := &recordingTokenStore{data: []byte(value)}
		if _, err := offering.TokenSource(store); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("TokenSource() for %q error = %v, want ErrInvalidConfig", value, err)
		}
		if store.readCalls != 1 {
			t.Errorf("store reads for %q = %d, want 1", value, store.readCalls)
		}
	}
}

// R-ZX8V-CAEN
func TestOfferingTokenSourceRefreshPostsRefreshGrant(t *testing.T) {
	request := make(chan *http.Request, 1)
	body := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request <- r
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		body <- data
		_, _ = io.WriteString(w, `{"access_token":"new","refresh_token":"rotated"}`)
	}))
	defer server.Close()

	store := &recordingTokenStore{data: []byte(`{"access_token":"old","refresh_token":"stored"}`)}
	offering := Offering{
		ID:        OfferingXAIResponses,
		AuthModes: []AuthMode{AuthModeOAuth},
		OAuth:     OAuthClient{TokenURL: server.URL, ClientID: "client-123"},
	}
	source, err := offering.TokenSource(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	gotRequest := <-request
	if gotRequest.Method != http.MethodPost {
		t.Errorf("request method = %q, want POST", gotRequest.Method)
	}
	if gotRequest.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", gotRequest.Header.Get("Content-Type"))
	}
	if gotRequest.Header.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q", gotRequest.Header.Get("Accept"))
	}
	values, err := url.ParseQuery(string(<-body))
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"stored"},
		"client_id":     {"client-123"},
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("refresh form = %#v, want %#v", values, want)
	}
}

// R-ZYGR-Q25C
func TestOfferingTokenSourceRefreshRejectsMissingRefreshToken(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	for _, stored := range []string{
		`{"access_token":"old"}`,
		`{"access_token":"old","refresh_token":""}`,
		`{"access_token":"old","refresh_token":42}`,
	} {
		store := &recordingTokenStore{data: []byte(stored)}
		source, err := (Offering{
			ID:        OfferingXAIResponses,
			AuthModes: []AuthMode{AuthModeOAuth},
			OAuth:     OAuthClient{TokenURL: server.URL, ClientID: "client"},
		}).TokenSource(store)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.Refresh(context.Background()); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("Refresh() for %q error = %v, want ErrInvalidConfig", stored, err)
		}
		if store.writeCalls != 0 {
			t.Errorf("Refresh() for %q writes = %d, want 0", stored, store.writeCalls)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("token endpoint requests = %d, want 0", requests.Load())
	}
}

// R-ZZOO-3TW1
func TestOfferingTokenSourceRefreshWritesAndCachesToken(t *testing.T) {
	responses := make(chan string, 2)
	responses <- `{"access_token":"new","token_type":"bearer"}`
	responses <- `{"access_token":"newer","refresh_token":"rotated","scope":"chat"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, <-responses)
	}))
	defer server.Close()

	store := &recordingTokenStore{data: []byte(`{"access_token":"old","refresh_token":"original"}`)}
	source, err := (Offering{
		ID:        OfferingXAIResponses,
		AuthModes: []AuthMode{AuthModeOAuth},
		OAuth:     OAuthClient{TokenURL: server.URL, ClientID: "client"},
	}).TokenSource(store)
	if err != nil {
		t.Fatal(err)
	}

	got, err := source.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != (Token{Bearer: "new"}) {
		t.Fatalf("first Refresh() = %+v, want new bearer", got)
	}
	var firstWrite map[string]any
	if err := json.Unmarshal(store.writes[0], &firstWrite); err != nil {
		t.Fatal(err)
	}
	if firstWrite["refresh_token"] != "original" {
		t.Fatalf("carried refresh_token = %#v, want original", firstWrite["refresh_token"])
	}

	got, err = source.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantBody := []byte(`{"access_token":"newer","refresh_token":"rotated","scope":"chat"}`)
	if store.writeCalls != 2 || !bytes.Equal(store.writes[1], wantBody) {
		t.Fatalf("writes = %d, latest %s; want exact response body", store.writeCalls, store.writes[1])
	}
	if got != (Token{Bearer: "newer"}) {
		t.Fatalf("second Refresh() = %+v, want newer bearer", got)
	}
	cached, err := source.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cached != got {
		t.Fatalf("Token() = %+v, want latest Refresh token %+v", cached, got)
	}
}

// R-00WK-HLMQ
func TestOfferingTokenSourceRefreshReturnsEndpointAndStoreErrors(t *testing.T) {
	t.Run("OAuth endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"refresh token expired"}`)
		}))
		defer server.Close()
		store := &recordingTokenStore{data: []byte(`{"access_token":"old","refresh_token":"refresh"}`)}
		source := newTestTokenSource(t, server.URL, store)

		_, err := source.Refresh(context.Background())
		var got *Error
		if !errors.As(err, &got) {
			t.Fatalf("Refresh() error = %v (%T), want *Error", err, err)
		}
		if got.Category != CategoryAuth || got.Status != http.StatusBadRequest || got.Code != "invalid_grant" || got.Message != "refresh token expired" {
			t.Fatalf("Refresh() error = %+v", got)
		}
		if store.writeCalls != 0 {
			t.Fatalf("failed endpoint writes = %d, want 0", store.writeCalls)
		}
	})

	t.Run("transport", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		server.Close()
		store := &recordingTokenStore{data: []byte(`{"access_token":"old","refresh_token":"refresh"}`)}
		source := newTestTokenSource(t, server.URL, store)

		_, err := source.Refresh(context.Background())
		var got *Error
		if !errors.As(err, &got) || got.Category != CategoryTransport || got.Status != 0 {
			t.Fatalf("Refresh() error = %+v (%T), want zero-status transport *Error", err, err)
		}
		if store.writeCalls != 0 {
			t.Fatalf("transport failure writes = %d, want 0", store.writeCalls)
		}
	})

	t.Run("store write", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"access_token":"new","refresh_token":"rotated"}`)
		}))
		defer server.Close()
		writeErr := &tokenStoreWriteError{marker: 1}
		store := &recordingTokenStore{
			data:     []byte(`{"access_token":"old","refresh_token":"refresh"}`),
			writeErr: writeErr,
		}
		source := newTestTokenSource(t, server.URL, store)

		_, err := source.Refresh(context.Background())
		var gotWriteError *tokenStoreWriteError
		if !errors.As(err, &gotWriteError) || reflect.TypeOf(err) != reflect.TypeOf(writeErr) || gotWriteError != writeErr {
			t.Fatalf("Refresh() error = %v, want unchanged %p", err, writeErr)
		}
		if store.writeCalls != 1 {
			t.Fatalf("store writes = %d, want 1", store.writeCalls)
		}
	})
}

// R-024G-VDDF
func TestOfferingTokenSourceConcurrentRefreshCollapsesRequest(t *testing.T) {
	const callers = 12
	var requests atomic.Int32
	var entered atomic.Int32
	allEntered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		<-allEntered
		<-release
		_, _ = io.WriteString(w, `{"access_token":"new","refresh_token":"rotated"}`)
	}))
	defer server.Close()

	store := &recordingTokenStore{data: []byte(`{"access_token":"old","refresh_token":"refresh"}`)}
	source := newTestTokenSource(t, server.URL, store)
	start := make(chan struct{})
	results := make(chan Token, callers)
	errors := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if entered.Add(1) == callers {
				close(allEntered)
			}
			token, err := source.Refresh(context.Background())
			results <- token
			errors <- err
		}()
	}
	close(start)
	<-allEntered
	close(release)
	wg.Wait()
	close(results)
	close(errors)

	if requests.Load() != 1 {
		t.Fatalf("token endpoint requests = %d, want 1", requests.Load())
	}
	if store.writeCalls != 1 {
		t.Fatalf("store writes = %d, want 1", store.writeCalls)
	}
	for err := range errors {
		if err != nil {
			t.Errorf("Refresh() error = %v", err)
		}
	}
	for token := range results {
		if token != (Token{Bearer: "new"}) {
			t.Errorf("Refresh() token = %+v, want new bearer", token)
		}
	}
}

func newTestTokenSource(t *testing.T, tokenURL string, store TokenStore) TokenSource {
	t.Helper()
	source, err := (Offering{
		ID:        OfferingXAIResponses,
		AuthModes: []AuthMode{AuthModeOAuth},
		OAuth:     OAuthClient{TokenURL: tokenURL, ClientID: "client"},
	}).TokenSource(store)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

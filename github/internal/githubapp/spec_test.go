package githubapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"appkit/server"

	"github/internal/gh"
)

func TestSpecHandlersAssembleTokenRouteJSONGuardAndFailureR_GTQ4_30E7(t *testing.T) {
	// R-GTQ4-30E7
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	t.Setenv("IKIGENBA_APP_ID", "12345")
	t.Setenv("IKIGENBA_GITHUB_ORG", "acme")
	t.Setenv("IKIGENBA_APP_PRIVATE_KEY", keyPEM)

	var calls int
	successHTTP := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch req.URL.Path {
		case "/orgs/acme/installation":
			return githubResponse(http.StatusOK, `{"id":42}`), nil
		case "/app/installations/42/access_tokens":
			return githubResponse(http.StatusCreated, `{"token":"route-token","expires_at":"2026-07-04T12:10:00Z"}`), nil
		default:
			t.Fatalf("unexpected GitHub path %s", req.URL.Path)
			return nil, nil
		}
	})}
	handler := assembledSpecHandler(t, successHTTP)

	forwarded := httptest.NewRequest(http.MethodGet, "/token", nil)
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, forwarded)
	if rr.Code != http.StatusNotFound || rr.Body.String() != "404 page not found\n" || calls != 0 {
		t.Fatalf("forwarded request = %d %q with %d GitHub calls; want bare 404 before handler", rr.Code, rr.Body.String(), calls)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/token", nil))
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("loopback response = %d, Content-Type %q, body %q", rr.Code, rr.Header().Get("Content-Type"), rr.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if len(body) != 2 || body["token"] != "route-token" || body["expires_at"] != "2026-07-04T12:10:00Z" {
		t.Fatalf("token JSON = %#v, want exactly token and expires_at", body)
	}

	failureMaterial := "must-not-escape"
	failingHTTP := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/orgs/acme/installation" {
			return githubResponse(http.StatusOK, `{"id":42}`), nil
		}
		return githubResponse(http.StatusUnauthorized, `{"message":"`+failureMaterial+`"}`), nil
	})}
	rr = httptest.NewRecorder()
	assembledSpecHandler(t, failingHTTP).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/token", nil))
	if rr.Code != http.StatusBadGateway || strings.Contains(rr.Body.String(), failureMaterial) || strings.Contains(rr.Body.String(), "route-token") {
		t.Fatalf("failure response = %d %q, want generic 502 without token material", rr.Code, rr.Body.String())
	}
}

func assembledSpecHandler(t *testing.T, httpClient *http.Client) http.Handler {
	t.Helper()
	previous := newGitHubClient
	newGitHubClient = func(cfg gh.Config, _ *http.Client) (*gh.Client, error) {
		return gh.NewClient(cfg, httpClient)
	}
	t.Cleanup(func() { newGitHubClient = previous })

	spec := Spec()
	srv, err := server.New(server.Options{
		Addr: "127.0.0.1:0", Logger: slog.Default(), Apex: true, Version: "test", Service: "github",
		Register: spec.Handlers,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func githubResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appkit/server"
	"appkit/telemetry"

	"github/internal/gh"
)

func TestSpecHandlersInstrumentRESTAndTokenExchangeWithThirtySecondClient(t *testing.T) {
	// R-01NE-H6K8
	// R-02VA-UYAX
	// R-06J0-09J0
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

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/orgs/acme/installation":
			_, _ = io.WriteString(w, `{"id":42}`)
		case "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"token":"recorded-token","expires_at":"2099-07-04T12:10:00Z"}`)
		case "/orgs/acme/repos":
			_, _ = io.WriteString(w, `[{"name":"widgets"}]`)
		default:
			http.NotFound(w, req)
		}
	}))
	defer githubServer.Close()
	target, err := url.Parse(githubServer.URL)
	if err != nil {
		t.Fatal(err)
	}

	requests := make(chan []byte, 1)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			t.Errorf("read telemetry request: %v", readErr)
		}
		requests <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	recorder := telemetry.New(telemetry.Options{
		Service: "github", IngestURL: sink.URL, Enabled: true,
		Capacity: 20, BatchMax: 20, FlushEvery: time.Hour, Client: sink.Client(),
	})

	var client *gh.Client
	previous := newGitHubClient
	newGitHubClient = func(cfg gh.Config, hc *http.Client) (*gh.Client, error) {
		if hc == nil {
			t.Fatal("Handlers passed a nil HTTP client")
		}
		if hc.Timeout != 30*time.Second {
			t.Fatalf("Handlers client timeout = %v, want 30s", hc.Timeout)
		}
		instrumented := hc.Transport
		hc.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			return instrumented.RoundTrip(req)
		})
		var buildErr error
		client, buildErr = gh.NewClient(cfg, hc)
		return client, buildErr
	}
	t.Cleanup(func() { newGitHubClient = previous })

	_, err = server.New(server.Options{
		Addr: "127.0.0.1:0", Logger: slog.Default(), Apex: true,
		Version: "test", Service: "github", Recorder: recorder, Register: Spec().Handlers,
	})
	if err != nil {
		t.Fatalf("assemble handlers: %v", err)
	}
	if _, err := client.ReposList(context.Background()); err != nil {
		t.Fatalf("ReposList() error = %v", err)
	}
	if err := recorder.Close(context.Background()); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	var batch struct {
		Records []telemetry.Record `json:"records"`
	}
	select {
	case body := <-requests:
		if err := json.Unmarshal(body, &batch); err != nil {
			t.Fatalf("decode telemetry batch %q: %v", body, err)
		}
	case <-time.After(time.Second):
		t.Fatal("telemetry sink received no records")
	}
	wantOps := map[string]bool{
		http.MethodPost + " " + target.Host + "/app/installations/42/access_tokens": false,
		http.MethodGet + " " + target.Host + "/orgs/acme/repos":                     false,
	}
	for _, record := range batch.Records {
		if record.Kind == telemetry.KindOutbound {
			if _, ok := wantOps[record.Op]; ok {
				wantOps[record.Op] = true
			}
		}
	}
	for op, found := range wantOps {
		if !found {
			t.Fatalf("outbound telemetry missing %q; records = %+v", op, batch.Records)
		}
	}
}

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

func TestEveryServedDocumentCarriesBrandIcons(t *testing.T) {
	// R-RYDN-YNR5
	documents, err := filepath.Glob(filepath.Join("..", "web", "*.html"))
	if err != nil {
		t.Fatalf("enumerate HTML documents: %v", err)
	}
	if len(documents) == 0 {
		t.Fatal("no HTML documents found")
	}

	handler := assembledSpecHandler(t, &http.Client{})
	wantLinks := []string{
		`<link rel="icon" sizes="any" href="static/favicon.ico">`,
		`<link rel="icon" type="image/png" href="static/favicon-32.png">`,
		`<link rel="apple-touch-icon" href="static/apple-touch-icon.png">`,
	}
	for _, document := range documents {
		document := document
		t.Run(filepath.Base(document), func(t *testing.T) {
			route := "/" + strings.TrimSuffix(filepath.Base(document), ".html")
			if filepath.Base(document) == "landing.html" {
				route = "/"
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, route, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", route, rr.Code)
			}

			body := rr.Body.String()
			headStart := strings.Index(body, "<head>")
			headEnd := strings.Index(body, "</head>")
			if headStart < 0 || headEnd <= headStart {
				t.Fatalf("GET %s response has no complete head", route)
			}
			head := body[headStart:headEnd]
			for _, link := range wantLinks {
				if !strings.Contains(head, link) {
					t.Errorf("GET %s head missing %s", route, link)
				}
			}
		})
	}
}

func TestAssembledSpecServesBrandIconAssets(t *testing.T) {
	// R-RZLK-CFHU
	handler := assembledSpecHandler(t, &http.Client{})
	assets := map[string]string{
		"/static/favicon.ico":          "image/x-icon",
		"/static/favicon-32.png":       "image/png",
		"/static/apple-touch-icon.png": "image/png",
	}
	for asset, wantContentType := range assets {
		asset, wantContentType := asset, wantContentType
		t.Run(filepath.Base(asset), func(t *testing.T) {
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, asset, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", asset, rr.Code)
			}
			if rr.Body.Len() == 0 {
				t.Fatalf("GET %s returned an empty body", asset)
			}
			if got := rr.Header().Get("Content-Type"); got != wantContentType {
				t.Fatalf("GET %s Content-Type = %q, want %q", asset, got, wantContentType)
			}
		})
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

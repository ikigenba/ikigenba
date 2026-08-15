package gmail

import (
	"appkit/server"
	"appkit/telemetry"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordSink struct {
	mu      sync.Mutex
	records []telemetry.Record
}

func (s *recordSink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var batch struct {
		Records []telemetry.Record `json:"records"`
	}
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.records = append(s.records, batch.Records...)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *recordSink) snapshot() []telemetry.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]telemetry.Record(nil), s.records...)
}

func TestRuntimeAssembledClientRecordsRESTAndTokenRefresh(t *testing.T) {
	// R-ZUC0-6K42
	// R-ZWRS-Y3LG
	sink := &recordSink{}
	sinkServer := httptest.NewServer(sink)
	t.Cleanup(sinkServer.Close)
	recorder := telemetry.New(telemetry.Options{
		Service:   "gmail",
		IngestURL: sinkServer.URL,
		Enabled:   true,
		Client:    sinkServer.Client(),
	})

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"access","expires_in":3600}`)
		case "/gmail/v1/users/me/profile":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"emailAddress":"owner@example.com","historyId":"42"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	oldOAuth, oldREST := hostOAuth, hostREST
	hostOAuth = api.URL + "/token"
	hostREST = api.URL + "/gmail/v1/users/me"
	t.Cleanup(func() { hostOAuth, hostREST = oldOAuth, oldREST })

	var hc *http.Client
	_, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/gmail/",
		AuthServer: "https://example.test/",
		Service:    "gmail",
		Version:    "test",
		Recorder:   recorder,
		Register: func(rt *server.Router) error {
			hc = rt.HTTPClient(100 * time.Second)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("assemble runtime router: %v", err)
	}
	client := NewClient(Config{ClientID: "id", ClientSecret: "secret", RefreshToken: "refresh"}, hc)
	if _, err := client.GetProfile(context.Background()); err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if err := recorder.Close(context.Background()); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	host := strings.TrimPrefix(api.URL, "http://")
	want := map[string]bool{
		http.MethodPost + " " + host + "/token":                    false,
		http.MethodGet + " " + host + "/gmail/v1/users/me/profile": false,
	}
	for _, rec := range sink.snapshot() {
		if rec.Kind == telemetry.KindOutbound {
			if _, ok := want[rec.Op]; ok {
				want[rec.Op] = true
			}
		}
	}
	for op, found := range want {
		if !found {
			t.Errorf("missing outbound record %q; records=%#v", op, sink.snapshot())
		}
	}
}

func TestRuntimeAssembledClientPreservesGmailTimeout(t *testing.T) {
	// R-ZZ7L-PN2U
	var hc *http.Client
	_, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/gmail/",
		AuthServer: "https://example.test/",
		Service:    "gmail",
		Version:    "test",
		Register: func(rt *server.Router) error {
			hc = rt.HTTPClient(100 * time.Second)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("assemble runtime router: %v", err)
	}
	if hc == nil || hc.Timeout != 100*time.Second {
		t.Fatalf("runtime client timeout = %v, want 100s", hc.Timeout)
	}
}

func TestInjectedHTTPClientCarriesBothRESTAndRefresh(t *testing.T) {
	// R-ZXZP-BVC5
	oldOAuth, oldREST := hostOAuth, hostREST
	hostOAuth = "https://oauth.invalid/token"
	hostREST = "https://gmail.invalid/gmail/v1/users/me"
	t.Cleanup(func() { hostOAuth, hostREST = oldOAuth, oldREST })

	var paths []string
	hc := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.Method+" "+req.URL.String())
		body := `{"emailAddress":"owner@example.com","historyId":"42"}`
		if req.URL.Path == "/token" {
			body = `{"access_token":"access","expires_in":3600}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
	client := NewClient(Config{ClientID: "id", ClientSecret: "secret", RefreshToken: "refresh"}, hc)
	if client.http != hc || client.token.httpClient != hc {
		t.Fatal("injected HTTP client was not shared by REST and token source")
	}
	if _, err := client.GetProfile(context.Background()); err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	want := []string{
		"POST https://oauth.invalid/token",
		"GET https://gmail.invalid/gmail/v1/users/me/profile",
	}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("stub round trips = %v, want %v", paths, want)
	}
}

package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"appkit/server"

	"eventplane/outbox"
)

const (
	testResourceID = "https://int.ikigenba.com/srv/ledger/mcp"
	testAuthServer = "https://int.ikigenba.com"
	testVersion    = "v1.2.3 (abc1234)"
	testService    = "ledger"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// newStandardServer builds a path-routed service server whose Register hook
// mounts a gated /mcp echo and an unauthenticated /open route via the Router.
func newStandardServer(t *testing.T) http.Handler {
	t.Helper()
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Version:    testVersion,
		Service:    testService,
		Register: func(rt *server.Router) error {
			rt.Handle("POST /mcp", rt.RequireIdentity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, ok := server.IdentityFrom(r.Context())
				if !ok {
					t.Error("gated handler reached without identity on context")
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"owner": id.OwnerEmail})
			})))
			rt.HandleFunc("GET /open", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv.Handler
}

func TestNew_RequiresConfig(t *testing.T) {
	logger := discardLogger()
	cases := []struct {
		name string
		opts server.Options
	}{
		{"no logger", server.Options{ResourceID: testResourceID, AuthServer: testAuthServer}},
		{"no resource", server.Options{Logger: logger, AuthServer: testAuthServer}},
		{"no auth server", server.Options{Logger: logger, ResourceID: testResourceID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := server.New(tc.opts); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestPRMetadata_Unauthenticated(t *testing.T) {
	h := newStandardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if doc["resource"] != testResourceID {
		t.Errorf("resource = %v, want %q", doc["resource"], testResourceID)
	}
}

// TestHealth_Ungated asserts /health is reachable WITHOUT identity headers
// (DECISIONS §5 — liveness, ungated) and renders the fixed envelope with details
// present and empty when no Health reporter is set.
func TestHealth_Ungated(t *testing.T) {
	h := newStandardServer(t)
	// No identity headers: /health is ungated.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (ungated liveness)", rr.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc["status"] != "ok" {
		t.Errorf("status = %v, want ok", doc["status"])
	}
	if doc["version"] != testVersion {
		t.Errorf("version = %v, want %q", doc["version"], testVersion)
	}
	if doc["service"] != testService {
		t.Errorf("service = %v, want %q", doc["service"], testService)
	}
	details, ok := doc["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing or not an object: %v", doc["details"])
	}
	if len(details) != 0 {
		t.Errorf("details = %v, want empty {} with no reporter", details)
	}
	// No identity must leak into the ungated liveness body.
	if _, present := doc["owner_email"]; present {
		t.Errorf("owner_email present on ungated /health: %v", doc["owner_email"])
	}
}

// TestHealth_ReporterUnderDetails asserts a Health reporter's map lands under
// details and does NOT splat at the top level.
func TestHealth_ReporterUnderDetails(t *testing.T) {
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Version:    testVersion,
		Service:    testService,
		Health: func(ctx context.Context) (map[string]any, error) {
			return map[string]any{"mirror_bytes": float64(42)}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	details, ok := doc["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing or not an object: %v", doc["details"])
	}
	if details["mirror_bytes"] != float64(42) {
		t.Errorf("details.mirror_bytes = %v, want 42", details["mirror_bytes"])
	}
	// The reporter's keys must NOT splat at the top level.
	if _, splatted := doc["mirror_bytes"]; splatted {
		t.Errorf("mirror_bytes splatted at top level, want only under details")
	}
}

func TestIdentityGate_RejectsWithoutOwnerEmail(t *testing.T) {
	h := newStandardServer(t)
	// X-Client-Id present but X-Owner-Email absent: did not transit nginx. Use the
	// gated /mcp route — /health is now ungated (DECISIONS §5).
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("X-Client-Id", "client-abc")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	wa := rr.Header().Get("WWW-Authenticate")
	if wa == "" {
		t.Fatal("missing WWW-Authenticate challenge on 401")
	}
	// The challenge must point at this resource's PRM (resource_metadata), so an
	// MCP client can discover the AS.
	if want := testResourceID + "/.well-known/oauth-protected-resource"; !strings.Contains(wa, want) {
		t.Errorf("WWW-Authenticate = %q, want it to carry resource_metadata %q", wa, want)
	}
}

func TestIdentityGate_AllowsServiceRouteWithHeaders(t *testing.T) {
	h := newStandardServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("X-Owner-Email", "owner@example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("gated /mcp status = %d, want 200 with headers", rr.Code)
	}
	var doc map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc["owner"] != "owner@example.com" {
		t.Errorf("gated handler saw owner = %q", doc["owner"])
	}
}

func TestRouter_UnauthenticatedRoute(t *testing.T) {
	h := newStandardServer(t)
	// /open is registered without RequireIdentity, so it answers with no headers.
	req := httptest.NewRequest(http.MethodGet, "/open", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("/open status = %d, want 200 unauthenticated", rr.Code)
	}
}

// TestRun_ReleasesParkedHandlerOnShutdown proves Run cancels in-flight request
// contexts at shutdown (via srv.BaseContext) so a long-lived handler parked on
// r.Context().Done() (the SSE /feed shape) returns promptly. Without that, the
// parked handler would block srv.Shutdown for the full shutdownTimeout (10s) and
// Run would return a deadline error; here Run must return nil well within ~2s.
func TestRun_ReleasesParkedHandlerOnShutdown(t *testing.T) {
	started := make(chan struct{})
	srv, err := server.New(server.Options{
		Addr:       freeAddr(t),
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Version:    testVersion,
		Service:    testService,
		Register: func(rt *server.Router) error {
			// A parked long-lived handler simulating the SSE /feed stream: it
			// signals it is in-flight, then blocks on its request context until
			// shutdown cancels it.
			rt.HandleFunc("GET /park", func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
			})
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, srv, discardLogger())
	}()

	// Issue the parking request in its own goroutine: it will not return until
	// shutdown releases the handler. Poll the dial since the server may take a
	// moment to come up.
	url := "http://" + srv.Addr + "/park"
	go func() {
		client := &http.Client{}
		for i := 0; i < 50; i++ {
			req, _ := http.NewRequest(http.MethodGet, url, nil)
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// Confirm the request is genuinely in-flight before cancelling.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("parked handler never started; request did not reach the server")
	}

	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v, want nil (parked handler not released?)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s; parked handler blocked Shutdown to shutdownTimeout")
	}
}

// freeAddr reserves an ephemeral loopback port and returns its address. Run
// calls srv.ListenAndServe(), which takes srv.Addr (not a pre-made listener), so
// the test grabs a free port by opening and closing a listener and reusing the
// addr.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve ephemeral port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close reservation listener: %v", err)
	}
	return addr
}

func TestApex_BypassesPRMAndOwnsRouteTable(t *testing.T) {
	srv, err := server.New(server.Options{
		Addr:   "127.0.0.1:0",
		Logger: discardLogger(),
		Apex:   true, // dashboard: no ResourceID/AuthServer required
		Register: func(rt *server.Router) error {
			rt.HandleFunc("GET /index", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("apex"))
			})
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New apex: %v", err)
	}
	h := srv.Handler

	// The standard PRM route must NOT be mounted in apex mode: appkit added no
	// route, and the apex registered only /index, so the PRM path 404s.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("apex PRM status = %d, want 404 (apex owns its own table)", rr.Code)
	}
	// The apex's own route answers.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/index", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "apex" {
		t.Errorf("apex /index = (%d, %q), want (200, apex)", rr.Code, rr.Body.String())
	}
}

// TestRouter_PublishesPreferredOverEvents wires both the static Events and a live
// Publishes provider, then exercises the precedence rule a producer's reflection
// tool applies at the rt seam: rt.Publishes() is preferred when non-nil and only
// then does the service fall back to rt.Events(). It also proves Publishes is the
// LIVE provider (a second call after a mutation reports the new list).
func TestRouter_PublishesPreferredOverEvents(t *testing.T) {
	static := outbox.Registry{{Type: "contact.created", Description: "static"}}
	dynamic := []outbox.EventType{{Type: "cron.foo", Description: "live"}}

	var gotPreferred, gotFallback []string
	_, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Events:     static,
		Publishes:  func() outbox.Registry { return outbox.Registry(dynamic) },
		Register: func(rt *server.Router) error {
			// Precedence: prefer the live provider when set.
			if pub := rt.Publishes(); pub != nil {
				for _, et := range pub() {
					gotPreferred = append(gotPreferred, et.Type)
				}
			} else {
				for _, et := range rt.Events() {
					gotPreferred = append(gotPreferred, et.Type)
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(gotPreferred) != 1 || gotPreferred[0] != "cron.foo" {
		t.Fatalf("preferred publishes = %v, want [cron.foo] (Publishes wins over Events)", gotPreferred)
	}

	// Live: the provider reflects a runtime change.
	dynamic = append(dynamic, outbox.EventType{Type: "cron.bar"})
	_, err = server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Events:     static,
		Publishes:  func() outbox.Registry { return outbox.Registry(dynamic) },
		Register: func(rt *server.Router) error {
			for _, et := range rt.Publishes()() {
				gotFallback = append(gotFallback, et.Type)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(gotFallback) != 2 {
		t.Fatalf("live publishes = %v, want 2 types after mutation", gotFallback)
	}

	// Backward compat: with no Publishes set, rt.Publishes() is nil and the
	// service falls back to the static Events exactly as today.
	var gotStatic []string
	_, err = server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Events:     static,
		Register: func(rt *server.Router) error {
			if rt.Publishes() != nil {
				t.Error("rt.Publishes() must be nil when Spec.Publishes unset")
			}
			for _, et := range rt.Events() {
				gotStatic = append(gotStatic, et.Type)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(gotStatic) != 1 || gotStatic[0] != "contact.created" {
		t.Fatalf("fallback events = %v, want [contact.created]", gotStatic)
	}
}

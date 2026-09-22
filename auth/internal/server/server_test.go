package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/server/assets"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func TestConfigAndNew(t *testing.T) {
	// R-KTXG-XSI4: Config is exactly the process dependencies handlers need.
	configType := reflect.TypeFor[Config]()
	wantFields := []struct {
		name string
		typ  reflect.Type
	}{
		{name: "Store", typ: reflect.TypeFor[*store.Store]()},
		{name: "Google", typ: reflect.TypeFor[*google.Client]()},
		{name: "Now", typ: reflect.TypeFor[func() time.Time]()},
		{name: "Rand", typ: reflect.TypeFor[io.Reader]()},
		{name: "Stderr", typ: reflect.TypeFor[io.Writer]()},
		{name: "WorkspaceDomain", typ: reflect.TypeFor[string]()},
	}
	if configType.NumField() != len(wantFields) {
		t.Fatalf("Config has %d fields, want %d", configType.NumField(), len(wantFields))
	}
	for i, want := range wantFields {
		field := configType.Field(i)
		if field.Name != want.name || field.Type != want.typ {
			t.Fatalf("Config field %d = %s %s, want %s %s", i, field.Name, field.Type, want.name, want.typ)
		}
	}

	// R-KWD9-PBZI: New is func(Config) *Server and the result's type is the exported Server.
	fn := reflect.TypeOf(New)
	wantFn := reflect.TypeOf(func(Config) *Server { return nil })
	if fn != wantFn {
		t.Fatalf("New has type %s, want %s", fn, wantFn)
	}
	serverType := reflect.TypeFor[*Server]().Elem()
	if serverType.Name() != "Server" || serverType.PkgPath() != "github.com/ikigenba/ikigenba/auth/internal/server" {
		t.Fatalf("Server type = %s pkg=%s", serverType.Name(), serverType.PkgPath())
	}

	constructor := pinNew(New)
	now := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	gc := google.NewClient("client", "secret", "space.example", "http://127.0.0.1:1")
	var stderr bytes.Buffer
	cfg := Config{
		Store:           nil,
		Google:          gc,
		Now:             now,
		Rand:            bytes.NewReader(nil),
		Stderr:          &stderr,
		WorkspaceDomain: "space.example",
	}
	s := pinServer(constructor(cfg))
	if s == nil || s.httpServer == nil || s.httpServer.Handler == nil {
		t.Fatal("New did not initialize the HTTP server and router")
	}
	if s.now() != now() || s.gc != gc || s.rand != cfg.Rand || s.stderr != cfg.Stderr || s.cfg.WorkspaceDomain != "space.example" {
		t.Fatal("New did not retain its injected handler dependencies")
	}
}

func pinNew(newFn func(Config) *Server) func(Config) *Server { return newFn }

func pinServer(s *Server) *Server { return s }

func TestServeAndShutdown(t *testing.T) {
	// R-IEJ9-SABB: Serve binds the requested loopback address and Shutdown
	// gracefully closes that listener, making Serve return without ErrServerClosed.
	s := New(Config{Now: fixedNow})
	addr := freeLoopbackAddress(t)
	served := make(chan error, 1)
	go func() { served <- s.Serve(addr) }()

	conn := waitForLoopback(t, addr)
	if err := conn.Close(); err != nil {
		t.Fatalf("close readiness connection: %v", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", addr); err == nil {
		_ = conn.Close()
		t.Fatal("address is still listening after Shutdown")
	}
}

func TestRouterRegistersContractRoutesAndEmbeddedAssets(t *testing.T) {
	s := New(Config{Now: fixedNow})
	for _, target := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/"},
		{method: http.MethodPost, path: "/login/google"},
		{method: http.MethodPost, path: "/login/google/callback"},
		{method: http.MethodGet, path: "/logout"},
		{method: http.MethodPost, path: "/check"},
		{method: http.MethodPost, path: "/me"},
		{method: http.MethodGet, path: "/tokens"},
		{method: http.MethodGet, path: "/tokens/token-id/enable"},
	} {
		t.Run(target.method+" "+target.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			s.httpServer.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), target.method, target.path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
		})
	}
	missing := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(missing, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/not-a-route", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing route status = %d, want %d", missing.Code, http.StatusNotFound)
	}

	// Serving the files from this package directory would still succeed if the
	// handler read them off disk. A temp working directory has none of them.
	t.Chdir(t.TempDir())

	for _, asset := range []string{"/assets/index.html", "/assets/app.js", "/assets/style.css"} {
		t.Run(asset, func(t *testing.T) {
			response := httptest.NewRecorder()
			s.httpServer.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, asset, nil))
			if response.Code != http.StatusOK || response.Body.Len() == 0 {
				t.Fatalf("asset response = status %d, %d bytes", response.Code, response.Body.Len())
			}
		})
	}

	// R-YNFB-36HN: HTML, JavaScript, and CSS bytes are the embedded filesystem's,
	// still served when the process working directory has no asset files.
	embedded := map[string]string{}
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		body, err := assets.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		embedded[name] = string(body)
	}
	wantType := map[string]string{
		"index.html": "text/html; charset=utf-8",
		"app.js":     "text/javascript; charset=utf-8",
		"style.css":  "text/css; charset=utf-8",
	}
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		response := httptest.NewRecorder()
		s.httpServer.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/assets/"+name, nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != wantType[name] || response.Body.String() != embedded[name] {
			t.Fatalf("served %s status=%d type=%q (%d bytes), want 200 %s and the embedded bytes", name, response.Code, response.Header().Get("Content-Type"), response.Body.Len(), wantType[name])
		}
		if _, err := os.ReadFile(filepath.Clean(filepath.Join("assets", name))); err == nil {
			t.Fatalf("asset %s is readable from the working directory; the serve above would not prove the embed", name)
		}
	}
}

// R-IVLV-52P1: each D05, D06, and D07 method and path is this server's route.
func TestContractRoutesServed(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"), bytes.NewReader(bytes.Repeat([]byte{4}, 64)))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer.URL,
			"authorization_endpoint":                issuer.URL + "/authorize",
			"token_endpoint":                        issuer.URL + "/token",
			"jwks_uri":                              issuer.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(issuer.Close)

	s := New(Config{
		Store:           st,
		Google:          google.NewClient("client-id", "client-secret", "example.test", issuer.URL),
		Now:             fixedNow,
		Rand:            bytes.NewReader(bytes.Repeat([]byte{5}, 64)),
		Stderr:          &bytes.Buffer{},
		WorkspaceDomain: "example.test",
	})

	root := serveRoute(s, http.MethodGet, "/", nil)
	if root.Code != http.StatusOK || root.Header().Get("Content-Type") != "text/html; charset=utf-8" || root.Body.String() != `<!doctype html><html><body><a href="/login/google">Sign in with Google</a></body></html>` {
		t.Fatalf("GET / = %d %q %q", root.Code, root.Header().Get("Content-Type"), root.Body.String())
	}

	login := serveRoute(s, http.MethodGet, "/login/google", nil)
	location := login.Header().Get("Location")
	if login.Code != http.StatusFound || !bytes.Contains([]byte(location), []byte(issuer.URL+"/authorize")) {
		t.Fatalf("GET /login/google = %d location %q", login.Code, location)
	}

	callback := serveRoute(s, http.MethodGet, "/login/google/callback", nil)
	if callback.Code != http.StatusBadRequest || callback.Header().Get("Content-Type") != "text/plain; charset=utf-8" || callback.Body.String() != "invalid login state\n" {
		t.Fatalf("GET /login/google/callback = %d %q %q", callback.Code, callback.Header().Get("Content-Type"), callback.Body.String())
	}

	logout := serveRoute(s, http.MethodPost, "/logout", map[string]string{"Origin": "https://evil.example"})
	if logout.Code != http.StatusForbidden || logout.Body.String() != "forbidden\n" || logout.Header().Get("Set-Cookie") != "" {
		t.Fatalf("POST /logout = %d body %q cookie %q", logout.Code, logout.Body.String(), logout.Header().Get("Set-Cookie"))
	}

	check := serveRoute(s, http.MethodGet, "/check", nil)
	if check.Code != http.StatusUnauthorized || check.Header().Get("Content-Type") != "text/plain; charset=utf-8" || check.Body.String() != "sign in required\n" || check.Header().Get("X-User-Id") != "" || check.Header().Get("X-User-Email") != "" {
		t.Fatalf("GET /check = %d %q id=%q email=%q body %q", check.Code, check.Header().Get("Content-Type"), check.Header().Get("X-User-Id"), check.Header().Get("X-User-Email"), check.Body.String())
	}

	me := serveRoute(s, http.MethodGet, "/me", nil)
	if me.Code != http.StatusUnauthorized || me.Header().Get("Content-Type") != "text/plain; charset=utf-8" || me.Body.String() != "sign in required\n" || bytes.Contains(me.Body.Bytes(), []byte("{")) {
		t.Fatalf("GET /me = %d %q body %q", me.Code, me.Header().Get("Content-Type"), me.Body.String())
	}

	for _, target := range []string{"/tokens", "/tokens/token-id/enable", "/tokens/token-id/disable", "/tokens/token-id/delete"} {
		response := serveRoute(s, http.MethodPost, target, map[string]string{"Origin": "https://evil.example"})
		if response.Code != http.StatusForbidden || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Body.String() != "forbidden\n" {
			t.Fatalf("POST %s = %d %q %q", target, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
	}
}

func serveRoute(s *Server, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(response, request)
	return response
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release loopback port: %v", err)
	}
	return addr
}

func waitForLoopback(t *testing.T, addr string) net.Conn {
	t.Helper()
	for attempt := 0; attempt < 10000; attempt++ {
		conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", addr)
		if err == nil {
			return conn
		}
	}
	t.Fatalf("server did not listen on %s", addr)
	return nil
}

func fixedNow() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

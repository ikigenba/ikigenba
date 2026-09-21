package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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

	// R-KWD9-PBZI: New takes only Config and returns a routed *Server.
	var constructor func(Config) *Server = New
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
	s := constructor(cfg)
	if s == nil || s.httpServer == nil || s.httpServer.Handler == nil {
		t.Fatal("New did not initialize the HTTP server and router")
	}
	if s.now() != now() || s.gc != gc || s.rand != cfg.Rand || s.stderr != cfg.Stderr || s.cfg.WorkspaceDomain != "space.example" {
		t.Fatal("New did not retain its injected handler dependencies")
	}
}

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
	if conn, err := net.Dial("tcp", addr); err == nil {
		_ = conn.Close()
		t.Fatal("address is still listening after Shutdown")
	}
}

func TestRouterRegistersContractRoutesAndEmbeddedAssets(t *testing.T) {
	// R-IVLV-52P1: all D05/D06/D07 method/path shapes reach this server's
	// router. Wrong methods must receive mux's 405 rather than the 404 reserved
	// for no matching route, proving method dispatch before any handler runs.
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
			s.httpServer.Handler.ServeHTTP(response, httptest.NewRequest(target.method, target.path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
		})
	}
	missing := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/not-a-route", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing route status = %d, want %d", missing.Code, http.StatusNotFound)
	}

	for _, asset := range []string{"/assets/index.html", "/assets/app.js", "/assets/style.css"} {
		t.Run(asset, func(t *testing.T) {
			response := httptest.NewRecorder()
			s.httpServer.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, asset, nil))
			if response.Code != http.StatusOK || response.Body.Len() == 0 {
				t.Fatalf("asset response = status %d, %d bytes", response.Code, response.Body.Len())
			}
		})
	}

	// R-YNFB-36HN: HTML, JavaScript, and CSS are served from the embedded
	// filesystem, the same bytes the binary compiled in, with no asset path read.
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		embedded, err := assets.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		response := httptest.NewRecorder()
		s.httpServer.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/assets/"+name, nil))
		if response.Body.String() != string(embedded) {
			t.Fatalf("served %s differs from the embedded asset", name)
		}
	}
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
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
		conn, err := net.Dial("tcp", addr)
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

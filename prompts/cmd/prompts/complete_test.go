package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	appkitserver "appkit/server"
)

func TestCompleteMountAllowsHeaderlessLoopbackRequest(t *testing.T) {
	// R-5YWL-7BZN
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(source), `rt.HandleLoopback("POST /complete", completion.CompleteHandler())`) {
		t.Fatal("registerRoutes does not mount the completion handler through HandleLoopback")
	}
	calls := 0
	srv, err := appkitserver.New(appkitserver.Options{
		Addr: "127.0.0.1:0", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResourceID: "https://example.test/srv/prompts/mcp", AuthServer: "https://example.test",
		Version: "test", Service: "prompts",
		Register: func(rt *appkitserver.Router) error {
			rt.HandleLoopback("POST /complete", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(http.StatusNoContent)
			}))
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	loopback := httptest.NewRecorder()
	srv.Handler.ServeHTTP(loopback, httptest.NewRequest(http.MethodPost, "/complete", nil))
	if loopback.Code != http.StatusNoContent || calls != 1 {
		t.Fatalf("headerless loopback response = %d, calls = %d; want 204, 1", loopback.Code, calls)
	}
	forwarded := httptest.NewRequest(http.MethodPost, "/complete", nil)
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	frontDoor := httptest.NewRecorder()
	srv.Handler.ServeHTTP(frontDoor, forwarded)
	if frontDoor.Code != http.StatusNotFound || calls != 1 {
		t.Fatalf("front-door response = %d, calls = %d; want 404, 1", frontDoor.Code, calls)
	}
}

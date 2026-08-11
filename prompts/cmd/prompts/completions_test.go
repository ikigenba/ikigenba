package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	appkitdb "appkit/db"
	appkitserver "appkit/server"
	"appkit/telemetry"

	"prompts/internal/completion"
	promptsdb "prompts/internal/db"
)

// R-JQ0Y-6BID
func TestCompletionQueueVerbsAreHeaderlessLoopbackRoutes(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, mount := range []string{
		`rt.HandleLoopback("POST /completions", completionHTTP.EnsureHandler())`,
		`rt.HandleLoopback("GET /completions", completionHTTP.InboxHandler())`,
		`rt.HandleLoopback("GET /completions/{id}", completionHTTP.GetHandler())`,
		`rt.HandleLoopback("DELETE /completions/{id}", completionHTTP.AckHandler())`,
	} {
		if !bytes.Contains(source, []byte(mount)) {
			t.Fatalf("main.go does not contain loopback mount %q", mount)
		}
	}
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "completions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := appkitdb.LoadMigrations(promptsdb.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(t.Context(), database, migrations); err != nil {
		t.Fatal(err)
	}
	httpQueue := completion.NewHTTP(completion.NewStore(database, time.Now), func(string) string { return "test-key" }, func() bool { return false })
	srv, err := appkitserver.New(appkitserver.Options{
		Addr: "127.0.0.1:0", Logger: completionDiscardLogger(), ResourceID: "https://example.test/srv/prompts/mcp",
		AuthServer: "https://example.test", Version: "test", Service: "prompts",
		Register: func(rt *appkitserver.Router) error {
			rt.HandleLoopback("POST /completions", httpQueue.EnsureHandler())
			rt.HandleLoopback("GET /completions", httpQueue.InboxHandler())
			rt.HandleLoopback("GET /completions/{id}", httpQueue.GetHandler())
			rt.HandleLoopback("DELETE /completions/{id}", httpQueue.AckHandler())
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ensureBody := `{"consumer":"service:wiki","origin":"service:wiki","key":"x","name":"wiki.extract","model":"not-a-model","messages":[{"role":"user","text":"work"}]}`
	tests := []struct {
		method string
		path   string
		body   string
		status int
		proof  string
	}{
		{http.MethodPost, "/completions", ensureBody, http.StatusBadRequest, "unknown prompt model"},
		{http.MethodGet, "/completions?consumer=service:wiki", "", http.StatusOK, "[]"},
		{http.MethodGet, "/completions/missing", "", http.StatusNotFound, completion.ErrNotFound.Error()},
		{http.MethodDelete, "/completions/missing", "", http.StatusNotFound, completion.ErrNotFound.Error()},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		recorder := httptest.NewRecorder()
		srv.Handler.ServeHTTP(recorder, request)
		if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.proof) {
			t.Errorf("%s %s = %d %s, want %d containing %q", test.method, test.path, recorder.Code, recorder.Body.String(), test.status, test.proof)
		}
	}
	forwarded := httptest.NewRequest(http.MethodGet, "/completions?consumer=service:wiki", nil)
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	frontDoor := httptest.NewRecorder()
	srv.Handler.ServeHTTP(frontDoor, forwarded)
	if frontDoor.Code != http.StatusNotFound || strings.Contains(frontDoor.Body.String(), "[]") {
		t.Fatalf("front-door inbox = %d %s, want bare loopback-guard 404", frontDoor.Code, frontDoor.Body.String())
	}
}

// R-JSGQ-XUZR
func TestSynchronousCompleteRouteIsAbsentFromComposition(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(source, []byte(`POST /complete"`)) || bytes.Contains(source, []byte("CompleteHandler")) {
		t.Fatalf("main.go still contains synchronous completion wiring")
	}
	srv, err := appkitserver.New(appkitserver.Options{
		Addr: "127.0.0.1:0", Logger: completionDiscardLogger(), ResourceID: "https://example.test/srv/prompts/mcp",
		AuthServer: "https://example.test", Version: "test", Service: "prompts",
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/complete", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("POST /complete = %d, want 404", recorder.Code)
	}
}

// R-JR8U-K392
func TestCompletionReadRoutesAreTheOnlyQueueTelemetryExclusions(t *testing.T) {
	wantExclude := []string{"GET /completions", "GET /completions/{id}"}
	if got := promptsSpec().TelemetryExclude; !reflect.DeepEqual(got, wantExclude) {
		t.Fatalf("TelemetryExclude = %#v, want exactly %#v", got, wantExclude)
	}

	recorder, sink, cancel := newCompletionTelemetryRecorder(t)
	srv, err := appkitserver.New(appkitserver.Options{
		Addr: "127.0.0.1:0", Logger: completionDiscardLogger(), ResourceID: "https://example.test/srv/prompts/mcp",
		AuthServer: "https://example.test", Version: "test", Service: "prompts", Recorder: recorder,
		RecordExclude: promptsSpec().TelemetryExclude,
		Register: func(rt *appkitserver.Router) error {
			rt.HandleLoopback("POST /completions", statusHandler(http.StatusAccepted))
			rt.HandleLoopback("GET /completions", statusHandler(http.StatusOK))
			rt.HandleLoopback("GET /completions/{id}", statusHandler(http.StatusOK))
			rt.HandleLoopback("DELETE /completions/{id}", statusHandler(http.StatusNoContent))
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/completions", nil),
		httptest.NewRequest(http.MethodGet, "/completions?consumer=service:wiki", nil),
		httptest.NewRequest(http.MethodGet, "/completions/item-1", nil),
		httptest.NewRequest(http.MethodDelete, "/completions/item-1", nil),
	} {
		srv.Handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	records := finishCompletionTelemetry(t, recorder, sink, cancel, 2)
	var operations []string
	for _, record := range records {
		operations = append(operations, record.Op)
	}
	sort.Strings(operations)
	wantOperations := []string{"DELETE /completions/item-1", "POST /completions"}
	if !reflect.DeepEqual(operations, wantOperations) {
		t.Fatalf("recorded operations = %#v, want Ensure and Ack only %#v", operations, wantOperations)
	}
}

func completionDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func statusHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
}

type completionTelemetrySink struct {
	mu      sync.Mutex
	records []telemetry.Record
}

func newCompletionTelemetryRecorder(t *testing.T) (*telemetry.Recorder, *completionTelemetrySink, context.CancelFunc) {
	t.Helper()
	sink := &completionTelemetrySink{}
	ingest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var batch struct {
			Records []telemetry.Record `json:"records"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("decode telemetry batch: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sink.mu.Lock()
		sink.records = append(sink.records, batch.Records...)
		sink.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(ingest.Close)
	recorder := telemetry.New(telemetry.Options{Service: "prompts", IngestURL: ingest.URL, Enabled: true, FlushEvery: time.Millisecond, Logger: completionDiscardLogger()})
	ctx, cancel := context.WithCancel(context.Background())
	recorder.Start(ctx)
	return recorder, sink, cancel
}

func finishCompletionTelemetry(t *testing.T, recorder *telemetry.Recorder, sink *completionTelemetrySink, cancel context.CancelFunc, want int) []telemetry.Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		sink.mu.Lock()
		got := len(sink.records)
		sink.mu.Unlock()
		if got >= want || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	if err := recorder.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]telemetry.Record(nil), sink.records...)
}

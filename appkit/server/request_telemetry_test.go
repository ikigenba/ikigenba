package server_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"appkit/mcp"
	"appkit/server"
	"appkit/telemetry"
)

type phase20Sink struct {
	mu      sync.Mutex
	records []telemetry.Record
}

func phase20Recorder(t *testing.T) (*telemetry.Recorder, *phase20Sink, context.CancelFunc) {
	t.Helper()
	sink := &phase20Sink{}
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
	recorder := telemetry.New(telemetry.Options{
		Service:    testService,
		IngestURL:  ingest.URL,
		Enabled:    true,
		FlushEvery: time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	recorder.Start(ctx)
	return recorder, sink, cancel
}

func phase20Finish(t *testing.T, recorder *telemetry.Recorder, sink *phase20Sink, cancel context.CancelFunc, want int) []telemetry.Record {
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
		t.Fatalf("close recorder: %v", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]telemetry.Record(nil), sink.records...)
}

func TestNewRecordsHealthAndSuppressesDuplicateMCPRequest(t *testing.T) {
	recorder, sink, cancel := phase20Recorder(t)
	mcpHandler, err := mcp.New(mcp.Options{Service: testService, Recorder: recorder, Tools: []mcp.Tool{{
		Name:        "probe",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
			return mcp.TextResult("ready"), nil
		},
	}}})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Version:    testVersion,
		Service:    testService,
		Recorder:   recorder,
		MCP:        mcpHandler,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	healthResponse := httptest.NewRecorder()
	srv.Handler.ServeHTTP(healthResponse, httptest.NewRequest(http.MethodGet, "/health", nil))
	mcpRequest := httptest.NewRequest(http.MethodPost, "/mcp", jsonReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"probe","arguments":{}}}`))
	mcpRequest.Header.Set("X-Owner-Id", "owner-1")
	mcpRequest.Header.Set("X-Owner-Email", "owner@example.com")
	mcpRequest.Header.Set("X-Client-Id", "desktop")
	mcpResponse := httptest.NewRecorder()
	srv.Handler.ServeHTTP(mcpResponse, mcpRequest)
	records := phase20Finish(t, recorder, sink, cancel, 2)

	// R-1UCJ-J6PC
	if healthResponse.Code != http.StatusOK || mcpResponse.Code != http.StatusOK {
		t.Fatalf("health/MCP status = %d/%d, want 200/200", healthResponse.Code, mcpResponse.Code)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want health plus exactly one MCP record", len(records))
	}
	byOp := map[string][]telemetry.Record{}
	for _, record := range records {
		byOp[record.Op] = append(byOp[record.Op], record)
	}
	if len(byOp["POST /mcp"]) != 0 || len(byOp["mcp:probe"]) != 1 {
		t.Fatalf("MCP operation counts = %#v, want one mcp:probe and no POST /mcp", byOp)
	}
	healthRecords := byOp["GET /health"]
	if len(healthRecords) != 1 {
		t.Fatalf("GET /health records = %d, want 1", len(healthRecords))
	}
	healthRecord := healthRecords[0]
	digest := sha256.Sum256(healthResponse.Body.Bytes())
	if healthRecord.Kind != telemetry.KindRequest || healthRecord.Outcome == nil || healthRecord.Outcome.Status != "200" || healthRecord.Outcome.Bytes != healthResponse.Body.Len() || healthRecord.Outcome.SHA256 != hex.EncodeToString(digest[:]) {
		t.Errorf("health record = %#v, want real response status and digest", healthRecord)
	}
}

func TestNewRecordExcludeSuppressesOnlyExactListedPath(t *testing.T) {
	recorder, sink, cancel := phase20Recorder(t)
	srv, err := server.New(server.Options{
		Addr:          "127.0.0.1:0",
		Logger:        discardLogger(),
		ResourceID:    testResourceID,
		AuthServer:    testAuthServer,
		Version:       testVersion,
		Service:       testService,
		Recorder:      recorder,
		RecordExclude: []string{"/ingest"},
		Register: func(rt *server.Router) error {
			rt.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
			rt.HandleFunc("POST /ingest-sibling", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) })
			return nil
		},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	for _, path := range []string{"/ingest", "/ingest-sibling"} {
		rr := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, path, nil))
	}
	records := phase20Finish(t, recorder, sink, cancel, 1)

	// R-1VKF-WYG1
	if len(records) != 1 || records[0].Op != "POST /ingest-sibling" {
		t.Fatalf("records = %#v, want exactly the non-excluded sibling", records)
	}
}

func jsonReader(s string) io.Reader { return strings.NewReader(s) }

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"appkit/logging"
	"appkit/server"
	"appkit/telemetry"

	"eventplane/correlation"
)

type recordSink struct {
	mu      sync.Mutex
	records []telemetry.Record
}

func newTestRecorder(t *testing.T) (*telemetry.Recorder, *recordSink, context.CancelFunc) {
	t.Helper()
	sink := &recordSink{}
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
		Service:    "crm",
		IngestURL:  ingest.URL,
		Enabled:    true,
		FlushEvery: time.Millisecond,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	recorder.Start(ctx)
	return recorder, sink, cancel
}

func finishRecords(t *testing.T, recorder *telemetry.Recorder, sink *recordSink, cancel context.CancelFunc, want int) []telemetry.Record {
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

func telemetryRPC(t *testing.T, h http.Handler, body string, headers map[string]string) map[string]any {
	t.Helper()
	wrapped := logging.CorrelationMiddleware(slog.New(slog.NewTextHandler(io.Discard, nil)), h)
	return rpc(t, wrapped, body, headers)
}

func TestDispatchToolRecordsIdentityCorrelationAndDigestedResult(t *testing.T) {
	recorder, sink, cancel := newTestRecorder(t)
	const marker = "response-content-must-never-enter-telemetry-93a8"
	result := TextResult(marker)
	h := newHandler(t, Options{Service: "crm", Recorder: recorder, Tools: []Tool{{
		Name:        "lookup",
		InputSchema: map[string]any{"type": "object"},
		Handler: func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
			return result, nil
		},
	}}})
	requestID := "01KZ56WZ3FW4P31CHSVE1SG0JB"
	telemetryRPC(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup","arguments":{"query":"alpha"}}}`, map[string]string{
		"X-Owner-Email":    "owner@example.com",
		"X-Client-Id":      "desktop-client",
		correlation.Header: requestID,
	})
	records := finishRecords(t, recorder, sink, cancel, 1)

	// R-1PGY-03QK
	if len(records) != 1 {
		t.Fatalf("records = %d, want exactly 1", len(records))
	}
	record := records[0]
	if record.Kind != telemetry.KindRequest || record.Op != "mcp:lookup" || record.Service != "crm" {
		t.Errorf("record kind/op/service = %q/%q/%q", record.Kind, record.Op, record.Service)
	}
	if record.Actor == nil || record.Actor.OwnerEmail != "owner@example.com" || record.Actor.ClientID != "desktop-client" {
		t.Errorf("actor = %#v, want request identity", record.Actor)
	}
	if record.CorrelationID != requestID {
		t.Errorf("correlation_id = %q, want %q", record.CorrelationID, requestID)
	}

	// R-1QOU-DVH9
	marshaled, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(marshaled)
	if record.Outcome == nil || record.Outcome.DurationMS < 0 || record.Outcome.Status != "ok" || record.Outcome.Bytes != len(marshaled) || record.Outcome.SHA256 != hex.EncodeToString(digest[:]) {
		t.Errorf("outcome = %#v, want ok with result digest", record.Outcome)
	}
	fullRecord, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(fullRecord), marker) {
		t.Fatalf("record contains tool response marker: %s", fullRecord)
	}
}

func TestDispatchToolRecordsClosedErrorClassesWithoutRawGoError(t *testing.T) {
	recorder, sink, cancel := newTestRecorder(t)
	const rawMessage = "database password leaked in handler error"
	h := newHandler(t, Options{Service: "crm", Recorder: recorder, Tools: []Tool{
		{
			Name:        "coded",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
				return ErrorResult(ErrConflict, "safe client message"), nil
			},
		},
		{
			Name:        "broken",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
				return nil, errors.New(rawMessage)
			},
		},
	}})
	telemetryRPC(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"coded","arguments":{}}}`, nil)
	telemetryRPC(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"broken","arguments":{}}}`, nil)
	telemetryRPC(t, h, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"unknown","arguments":{}}}`, nil)
	records := finishRecords(t, recorder, sink, cancel, 3)

	// R-1RWQ-RN7Y
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3", len(records))
	}
	byOp := map[string]telemetry.Record{}
	for _, record := range records {
		byOp[record.Op] = record
	}
	if got := byOp["mcp:coded"].Outcome; got == nil || got.Status != "error" || got.Error != string(ErrConflict) {
		t.Errorf("coded outcome = %#v", got)
	}
	if got := byOp["mcp:broken"].Outcome; got == nil || got.Status != "error" || got.Error != string(ErrInternal) {
		t.Errorf("Go-error outcome = %#v", got)
	}
	if got := byOp["mcp:unknown"].Outcome; got == nil || got.Status != "error" || got.Error != string(ErrValidation) {
		t.Errorf("unknown-tool outcome = %#v", got)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), rawMessage) {
		t.Fatalf("records contain raw Go error: %s", encoded)
	}
}

func TestDispatchToolElidesDeclaredAndOversizedArguments(t *testing.T) {
	recorder, sink, cancel := newTestRecorder(t)
	const secret = "distinct-sensitive-value"
	large := strings.Repeat("z", 1025)
	h := newHandler(t, Options{Service: "crm", Recorder: recorder, Tools: []Tool{{
		Name:            "save",
		InputSchema:     map[string]any{"type": "object"},
		SensitiveParams: []string{"token"},
		Handler: func(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
			return TextResult("ok"), nil
		},
	}}})
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "save", "arguments": map[string]any{"token": secret, "visible": "literal", "large": large}},
	})
	if err != nil {
		t.Fatal(err)
	}
	telemetryRPC(t, h, string(body), nil)
	records := finishRecords(t, recorder, sink, cancel, 1)

	// R-1T4N-5EYN
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].Params["visible"] != "literal" {
		t.Errorf("visible param = %#v, want literal", records[0].Params["visible"])
	}
	params, err := json.Marshal(records[0].Params)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(params), secret) || strings.Contains(string(params), large) {
		t.Fatalf("params do not safely elide sensitive and oversized values: %s", params)
	}
	for _, name := range []string{"token", "large"} {
		elided, exists := records[0].Params[name]
		encoded, marshalErr := json.Marshal(elided)
		if !exists || marshalErr != nil || !strings.Contains(string(encoded), "$elided") {
			t.Errorf("param %q = %#v, want an $elided marker", name, elided)
		}
	}
}

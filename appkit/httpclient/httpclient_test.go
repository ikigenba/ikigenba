package httpclient

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"appkit/telemetry"
	"eventplane/correlation"
)

type recordSink struct {
	mu      sync.Mutex
	records []telemetry.Record
}

func captureRecorder(t *testing.T) (*telemetry.Recorder, *recordSink) {
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
	return telemetry.New(telemetry.Options{
		Service:   "test-service",
		IngestURL: ingest.URL,
		Enabled:   true,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}), sink
}

func finishRecords(t *testing.T, recorder *telemetry.Recorder, sink *recordSink) []telemetry.Record {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatalf("close recorder: %v", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]telemetry.Record(nil), sink.records...)
}

func TestClientRecordsCompletedOutboundRoundTrip(t *testing.T) {
	requestBody := "real request body"
	responseBody := "real response body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		if string(body) != requestBody {
			t.Errorf("request body = %q, want %q", body, requestBody)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer server.Close()
	recorder, sink := captureRecorder(t)
	client := New(Options{Recorder: recorder})

	request, err := http.NewRequest(http.MethodPost, server.URL+"/upload?token=secret", strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	gotBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
	records := finishRecords(t, recorder, sink)

	// R-22VU-7KW7
	if response.StatusCode != http.StatusCreated || string(gotBody) != responseBody {
		t.Fatalf("response = %d %q, want 201 %q", response.StatusCode, gotBody, responseBody)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	wantResponseBytes, wantResponseSHA := telemetry.Digest([]byte(responseBody))
	wantRequestBytes, wantRequestSHA := telemetry.Digest([]byte(requestBody))
	wantURL, _ := url.Parse(server.URL)
	if record.Kind != telemetry.KindOutbound || record.Op != "POST "+wantURL.Host+"/upload" || record.Params != nil {
		t.Errorf("record identity = kind %q op %q params %#v", record.Kind, record.Op, record.Params)
	}
	if record.Outcome == nil || record.Outcome.Status != "201" || record.Outcome.DurationMS < 0 || record.Outcome.Bytes != wantResponseBytes || record.Outcome.SHA256 != wantResponseSHA {
		t.Errorf("outcome = %#v, want status, duration, and response digest", record.Outcome)
	}
	if record.Detail["request_bytes"] != float64(wantRequestBytes) || record.Detail["request_sha256"] != wantRequestSHA {
		t.Errorf("detail = %#v, want request size and digest", record.Detail)
	}
}

func TestClientPropagatesCorrelationToLoopbackIPLiteral(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get(correlation.Header)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	correlationID := "01JZZZZZZZZZZZZZZZZZZZZZZZ"
	request, _ := http.NewRequestWithContext(correlation.WithContext(context.Background(), correlationID), http.MethodGet, server.URL, nil)
	response, err := New(Options{}).Do(request)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_ = response.Body.Close()

	// R-25BM-Z4DL
	if got := <-received; got != correlationID {
		t.Errorf("server received correlation id %q, want %q", got, correlationID)
	}
}

func TestClientDoesNotPropagateCorrelationToLocalhostName(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get(correlation.Header)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	serverURL, _ := url.Parse(server.URL)
	localhostURL := "http://localhost:" + serverURL.Port() + "/peer"
	request, _ := http.NewRequestWithContext(correlation.WithContext(context.Background(), "01JZZZZZZZZZZZZZZZZZZZZZZZ"), http.MethodGet, localhostURL, nil)
	response, err := New(Options{}).Do(request)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_ = response.Body.Close()

	// R-26JJ-CW4A
	if got := <-received; got != "" {
		t.Errorf("server received correlation id %q, want none", got)
	}
}

func TestClientRecordsSanitizedConnectionRefusedFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	recorder, sink := captureRecorder(t)
	request, _ := http.NewRequest(http.MethodDelete, "http://"+address+"/gone?credential=secret", nil)
	_, requestErr := New(Options{Recorder: recorder}).Do(request)
	records := finishRecords(t, recorder, sink)

	// R-27RF-QNUZ
	if requestErr == nil {
		t.Fatal("client.Do returned nil error for closed port")
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Kind != telemetry.KindOutbound || record.Op != "DELETE "+address+"/gone" || record.Outcome == nil || record.Outcome.Status != "error" || record.Outcome.Error != "connection_refused" {
		t.Errorf("failure record = %#v", record)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if strings.Contains(string(encoded), requestErr.Error()) || strings.Contains(string(encoded), "connection refused") || strings.Contains(string(encoded), "credential=secret") {
		t.Errorf("record contains raw transport error or query: %s", encoded)
	}
}

func TestClientWithoutRecorderCompletesRealRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, "accepted")
	}))
	defer server.Close()
	response, err := New(Options{Recorder: nil}).Get(server.URL)
	if err != nil {
		t.Fatalf("client.Get: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	_ = response.Body.Close()

	// R-28ZC-4FLO
	if response.StatusCode != http.StatusAccepted || string(body) != "accepted" {
		t.Errorf("response = %d %q, want 202 accepted", response.StatusCode, body)
	}
}

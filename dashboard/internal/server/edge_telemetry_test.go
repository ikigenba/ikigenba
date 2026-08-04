package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"appkit/telemetry"

	"dashboard/internal/audit"
	"dashboard/internal/oauth"
	"dashboard/internal/ratelimit"
)

type edgeSink struct {
	mu      sync.Mutex
	records []telemetry.Record
	raw     bytes.Buffer
}

func liveEdgeRecorder(t *testing.T) (*telemetry.Recorder, *edgeSink) {
	t.Helper()
	sink := &edgeSink{}
	ingest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read telemetry batch: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var batch struct {
			Records []telemetry.Record `json:"records"`
		}
		if err := json.Unmarshal(body, &batch); err != nil {
			t.Errorf("decode telemetry batch: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sink.mu.Lock()
		sink.raw.Write(body)
		sink.records = append(sink.records, batch.Records...)
		sink.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(ingest.Close)
	return telemetry.New(telemetry.Options{
		Service:    "dashboard",
		IngestURL:  ingest.URL,
		Enabled:    true,
		FlushEvery: time.Hour,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}), sink
}

func finishEdgeRecorder(t *testing.T, recorder *telemetry.Recorder, sink *edgeSink) ([]telemetry.Record, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Close(ctx); err != nil {
		t.Fatalf("close telemetry recorder: %v", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]telemetry.Record(nil), sink.records...), append([]byte(nil), sink.raw.Bytes()...)
}

func edgeAuthnServer(t *testing.T, d serverDeps, recorder *telemetry.Recorder, limiter *ratelimit.Limiter) http.Handler {
	t.Helper()
	d.recorder = recorder
	return authnServer(t, d, limiter)
}

// R-XDJH-4Y8Q
// R-XERD-IPZF
// R-XIF2-O17I
// R-XM2R-TCFL
func TestAuthnDeliversJoinableSanitizedEdgeRecordsPerDecision(t *testing.T) {
	d := newServerDeps(t)
	recorder, sink := liveEdgeRecorder(t)
	h := edgeAuthnServer(t, d, recorder, nil)
	token := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
	const cookieSecret = "session-cookie-must-not-leak"
	allow := doAuthn(h, map[string]string{
		"Authorization":     "Bearer " + token,
		"Cookie":            sessionCookieName + "=" + cookieSecret,
		"X-Original-Method": http.MethodGet,
		"X-Original-URI":    "/srv/crm/mcp",
	})
	if allow.Code != http.StatusOK {
		t.Fatalf("allow status = %d, want 200", allow.Code)
	}
	invalidPAT := "ms_pat_presented-secret-value"
	denyHeaders := map[string]string{
		"Authorization":  "Bearer " + invalidPAT,
		"Cookie":         sessionCookieName + "=" + cookieSecret,
		"X-Original-URI": "/srv/crm/mcp",
	}
	deny1 := doAuthn(h, denyHeaders)
	deny2 := doAuthn(h, denyHeaders)
	if deny1.Code != http.StatusUnauthorized || deny2.Code != http.StatusUnauthorized {
		t.Fatalf("deny statuses = %d/%d, want 401/401", deny1.Code, deny2.Code)
	}

	records, raw := finishEdgeRecorder(t, recorder, sink)
	if len(records) != 3 {
		t.Fatalf("edge records = %d, want exactly 3", len(records))
	}
	allowRecord := records[0]
	if allowRecord.Kind != telemetry.KindEdge || allowRecord.Detail["decision"] != "allow" {
		t.Errorf("allow kind/decision = %q/%v, want edge/allow", allowRecord.Kind, allowRecord.Detail["decision"])
	}
	if allowRecord.CorrelationID == "" || allowRecord.CorrelationID != allow.Header().Get("X-Correlation-Id") {
		t.Errorf("allow correlation_id = %q, response header = %q", allowRecord.CorrelationID, allow.Header().Get("X-Correlation-Id"))
	}
	if allowRecord.Op != "GET /srv/crm/mcp" || allowRecord.Detail["service"] != "crm" {
		t.Errorf("allow op/service = %q/%v, want GET /srv/crm/mcp and crm", allowRecord.Op, allowRecord.Detail["service"])
	}
	for i, record := range records[1:] {
		if record.Detail["decision"] != "deny" || record.Outcome == nil || record.Outcome.Status != "401" {
			t.Errorf("deny %d decision/outcome = %v/%+v, want deny/401", i+1, record.Detail["decision"], record.Outcome)
		}
	}
	if records[1].CorrelationID == "" || records[2].CorrelationID == "" || records[1].CorrelationID == records[2].CorrelationID {
		t.Errorf("deny correlation ids = %q/%q, want distinct non-empty values", records[1].CorrelationID, records[2].CorrelationID)
	}
	for _, secret := range []string{token, invalidPAT, cookieSecret, "Bearer " + token, "Bearer " + invalidPAT, "Authorization"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("telemetry payload leaked %q", secret)
		}
	}
}

// R-XFZ9-WHQ4
func TestAuthnRateLimitDeliversDistinctEdgeDecision(t *testing.T) {
	d := newServerDeps(t)
	recorder, sink := liveEdgeRecorder(t)
	h := edgeAuthnServer(t, d, recorder, ratelimit.New(1, time.Hour))
	token := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
	headers := map[string]string{"Authorization": "Bearer " + token, "X-Original-URI": "/srv/crm/mcp"}
	if got := doAuthn(h, headers).Code; got != http.StatusOK {
		t.Fatalf("first status = %d, want 200", got)
	}
	if got := doAuthn(h, headers).Code; got != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", got)
	}
	records, _ := finishEdgeRecorder(t, recorder, sink)
	if len(records) != 2 {
		t.Fatalf("edge records = %d, want 2", len(records))
	}
	got := records[1]
	if got.Detail["decision"] != "rate_limited" || got.Outcome == nil || got.Outcome.Status != "429" {
		t.Fatalf("rate-limit decision/outcome = %v/%+v, want rate_limited/429", got.Detail["decision"], got.Outcome)
	}
}

// R-XJMZ-1SY7
func TestSessionAuthnAllowAndDenyEachDeliverEdgeRecord(t *testing.T) {
	d := newServerDeps(t)
	recorder, sink := liveEdgeRecorder(t)
	h := edgeAuthnServer(t, d, recorder, nil)
	cookie := liveSessionCookie(t, d, "owner@int.ikigenba.com")
	if got := doSessionAuthn(h, cookie).Code; got != http.StatusOK {
		t.Fatalf("allow status = %d, want 200", got)
	}
	if got := doSessionAuthn(h, "bad-session").Code; got != http.StatusUnauthorized {
		t.Fatalf("deny status = %d, want 401", got)
	}
	records, _ := finishEdgeRecorder(t, recorder, sink)
	if len(records) != 2 {
		t.Fatalf("edge records = %d, want 2", len(records))
	}
	if records[0].Detail["decision"] != "allow" || records[1].Detail["decision"] != "deny" {
		t.Fatalf("decisions = %v/%v, want allow/deny", records[0].Detail["decision"], records[1].Detail["decision"])
	}
}

type fixedCorrelationMinter string

func (m fixedCorrelationMinter) NewCorrelationID() string { return string(m) }

func closedIngestRecorder(t *testing.T) *telemetry.Recorder {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return telemetry.New(telemetry.Options{
		Service: "dashboard", IngestURL: "http://" + address, Enabled: true,
		Client: &http.Client{Timeout: 20 * time.Millisecond},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// R-XKUV-FKOW
func TestUnreachableTelemetryDoesNotChangeOrDelayAuthnDecision(t *testing.T) {
	d := newServerDeps(t)
	token := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
	recorder, sink := liveEdgeRecorder(t)
	healthyOpts := d.opts()
	healthyOpts.TelemetryRecorder = recorder
	healthyOpts.CorrelationMinter = fixedCorrelationMinter("same-correlation")
	healthy, err := New(healthyOpts)
	if err != nil {
		t.Fatalf("New healthy: %v", err)
	}
	unreachableRecorder := closedIngestRecorder(t)
	unreachableOpts := healthyOpts
	unreachableOpts.TelemetryRecorder = unreachableRecorder
	unreachable, err := New(unreachableOpts)
	if err != nil {
		t.Fatalf("New unreachable: %v", err)
	}
	headers := map[string]string{"Authorization": "Bearer " + token, "X-Original-URI": "/srv/crm/mcp"}
	healthyAllow := doAuthn(healthy.Handler, headers)
	started := time.Now()
	unreachableAllow := doAuthn(unreachable.Handler, headers)
	allowLatency := time.Since(started)
	if healthyAllow.Code != http.StatusOK || unreachableAllow.Code != http.StatusOK {
		t.Fatalf("allow statuses = %d/%d, want 200/200", healthyAllow.Code, unreachableAllow.Code)
	}
	if !reflect.DeepEqual(healthyAllow.Header(), unreachableAllow.Header()) || !bytes.Equal(healthyAllow.Body.Bytes(), unreachableAllow.Body.Bytes()) {
		t.Errorf("unreachable allow response differs: healthy=%v/%q unreachable=%v/%q", healthyAllow.Header(), healthyAllow.Body.Bytes(), unreachableAllow.Header(), unreachableAllow.Body.Bytes())
	}
	denyHeaders := map[string]string{"Authorization": "Bearer " + oauth.AccessPrefix + "invalid", "X-Original-URI": "/srv/crm/mcp"}
	healthyDeny := doAuthn(healthy.Handler, denyHeaders)
	started = time.Now()
	unreachableDeny := doAuthn(unreachable.Handler, denyHeaders)
	denyLatency := time.Since(started)
	if healthyDeny.Code != http.StatusUnauthorized || unreachableDeny.Code != http.StatusUnauthorized {
		t.Fatalf("deny statuses = %d/%d, want 401/401", healthyDeny.Code, unreachableDeny.Code)
	}
	if !reflect.DeepEqual(healthyDeny.Header(), unreachableDeny.Header()) || !bytes.Equal(healthyDeny.Body.Bytes(), unreachableDeny.Body.Bytes()) {
		t.Errorf("unreachable deny response differs: healthy=%v/%q unreachable=%v/%q", healthyDeny.Header(), healthyDeny.Body.Bytes(), unreachableDeny.Header(), unreachableDeny.Body.Bytes())
	}
	if allowLatency > 50*time.Millisecond || denyLatency > 50*time.Millisecond {
		t.Errorf("unreachable handler latency allow/deny = %v/%v, want both under 50ms", allowLatency, denyLatency)
	}
	finishEdgeRecorder(t, recorder, sink)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = unreachableRecorder.Close(ctx)
}

// R-XNAO-746A
func TestAuthnAllowKeepsDurableAuditAlongsideBestEffortEdge(t *testing.T) {
	d := newServerDeps(t)
	recorder, sink := liveEdgeRecorder(t)
	h := edgeAuthnServer(t, d, recorder, nil)
	token := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
	vt, err := d.tokens.ValidateAccess(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateAccess: %v", err)
	}
	if got := doAuthn(h, map[string]string{"Authorization": "Bearer " + token, "X-Original-URI": "/srv/crm/mcp"}).Code; got != http.StatusOK {
		t.Fatalf("healthy allow status = %d, want 200", got)
	}
	records, _ := finishEdgeRecorder(t, recorder, sink)
	if len(records) != 1 || records[0].Detail["decision"] != "allow" {
		t.Fatalf("healthy edge records = %+v, want one allow", records)
	}
	var eventType, ownerEmail, clientID, chainID string
	if err := d.db.QueryRow(`SELECT event_type, owner_email, client_id, chain_id FROM audit_log ORDER BY id LIMIT 1`).Scan(&eventType, &ownerEmail, &clientID, &chainID); err != nil {
		t.Fatalf("query authn allow audit: %v", err)
	}
	if eventType != string(audit.EventAuthnAllow) || ownerEmail != vt.Chain.OwnerEmail || clientID != vt.Chain.ClientID || chainID != vt.Chain.ID {
		t.Errorf("audit row = %q/%q/%q/%q, want authn.allow/%q/%q/%q", eventType, ownerEmail, clientID, chainID, vt.Chain.OwnerEmail, vt.Chain.ClientID, vt.Chain.ID)
	}

	unreachable := closedIngestRecorder(t)
	h = edgeAuthnServer(t, d, unreachable, nil)
	secondToken := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
	if got := doAuthn(h, map[string]string{"Authorization": "Bearer " + secondToken, "X-Original-URI": "/srv/crm/mcp"}).Code; got != http.StatusOK {
		t.Fatalf("unreachable-sink allow status = %d, want 200", got)
	}
	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE event_type = ?`, audit.EventAuthnAllow).Scan(&count); err != nil {
		t.Fatalf("count authn allow audits: %v", err)
	}
	if count != 2 {
		t.Errorf("authn.allow audit rows = %d, want 2 after unreachable-sink allow", count)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = unreachable.Close(ctx)
}

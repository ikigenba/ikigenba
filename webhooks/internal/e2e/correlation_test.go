package e2e

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"appkit/logging"
	"eventplane/outbox"
	"webhooks/internal/webhooks"
)

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// R-L1A1-XMRN
func TestAcceptedIngressMintsUniqueCorrelationID(t *testing.T) {
	handler, conn, _, name, secret := newCorrelationFixture(t)
	var observed []string
	handler = correlationMiddleware(t, handler, &observed)

	first := postDelivery(t, handler, name, secret, "")
	assertCrockfordULID(t, first)
	if len(observed) != 1 || observed[0] != first {
		t.Fatalf("handler context correlation IDs = %q, want [%q]", observed, first)
	}
	if stored := latestCorrelationID(t, conn); stored != first {
		t.Fatalf("first outbox correlation_id = %q, want middleware ID %q", stored, first)
	}

	second := postDelivery(t, handler, name, secret, "")
	assertCrockfordULID(t, second)
	if len(observed) != 2 || observed[1] != second {
		t.Fatalf("handler context correlation IDs = %q, second want %q", observed, second)
	}
	if stored := latestCorrelationID(t, conn); stored != second {
		t.Fatalf("second outbox correlation_id = %q, want middleware ID %q", stored, second)
	}
	if second == first {
		t.Fatalf("two deliveries received the same correlation ID %q", first)
	}
}

// R-L2HY-BEIC
func TestAcceptedIngressPropagatesTrustedCorrelationIDVerbatim(t *testing.T) {
	handler, conn, _, name, secret := newCorrelationFixture(t)
	var observed []string
	handler = correlationMiddleware(t, handler, &observed)
	const supplied = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

	echoed := postDelivery(t, handler, name, secret, supplied)
	if echoed != supplied {
		t.Fatalf("response correlation ID = %q, want supplied %q", echoed, supplied)
	}
	if len(observed) != 1 || observed[0] != supplied {
		t.Fatalf("handler context correlation IDs = %q, want [%q]", observed, supplied)
	}
	if stored := latestCorrelationID(t, conn); stored != supplied {
		t.Fatalf("outbox correlation_id = %q, want supplied %q", stored, supplied)
	}
}

// R-L3PU-P691
func TestFeedEnvelopeCarriesDeliveringRequestCorrelationID(t *testing.T) {
	handler, conn, ob, name, secret := newCorrelationFixture(t)
	handler = correlationMiddleware(t, handler, nil)
	deliveryID := postDelivery(t, handler, name, secret, "")
	if stored := latestCorrelationID(t, conn); stored != deliveryID {
		t.Fatalf("outbox correlation_id = %q, want delivery ID %q", stored, deliveryID)
	}

	server := httptest.NewServer(ob.FeedHandler())
	t.Cleanup(server.Close)
	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("GET feed: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET feed status = %d, want 200", resp.StatusCode)
	}

	event, data := firstEventFrame(t, resp.Body)
	wantEvent := "webhooks:received/" + name
	if event != wantEvent {
		t.Fatalf("SSE event = %q, want %q", event, wantEvent)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		t.Fatalf("decode SSE envelope: %v; data=%q", err, data)
	}
	var correlationID, kind, subject string
	for key, dst := range map[string]*string{
		"correlation_id": &correlationID,
		"kind":           &kind,
		"subject":        &subject,
	} {
		raw, ok := envelope[key]
		if !ok {
			t.Fatalf("SSE envelope missing %q: %s", key, data)
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			t.Fatalf("decode SSE envelope %q: %v", key, err)
		}
	}
	if correlationID != deliveryID {
		t.Fatalf("SSE correlation_id = %q, want delivery ID %q", correlationID, deliveryID)
	}
	if kind != "received" || subject != "/"+name {
		t.Fatalf("SSE routing fields kind=%q subject=%q, want received and /%s", kind, subject, name)
	}
	if _, exists := envelope["type"]; exists {
		t.Fatalf("SSE envelope unexpectedly has type key: %s", data)
	}
}

func newCorrelationFixture(t *testing.T) (http.Handler, *sql.DB, *outbox.Outbox, string, string) {
	t.Helper()
	conn := migratedDB(t)
	clk := fixedClock{t: time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)}
	ob, err := outbox.New(conn, outbox.Options{Source: "webhooks", Registry: webhooks.Events, Now: clk.Now})
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}
	svc := webhooks.NewService(conn, clk)
	svc.Outbox = ob
	wh, secret, err := svc.Create(context.Background(), "correlation-owner", "correlation@example.com", "correlation-ingress", "bearer")
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return webhooks.NewIngressHandler(svc, logger), conn, ob, wh.Name, secret
}

func correlationMiddleware(t *testing.T, next http.Handler, observed *[]string) http.Handler {
	t.Helper()
	if observed != nil {
		inner := next
		next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*observed = append(*observed, logging.RequestID(r.Context()))
			inner.ServeHTTP(w, r)
		})
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return logging.CorrelationMiddleware(logger, next)
}

func postDelivery(t *testing.T, handler http.Handler, name, secret, correlationID string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/in/"+name, strings.NewReader(`{"delivery":true}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	if correlationID != "" {
		req.Header.Set("X-Correlation-Id", correlationID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /in/%s status = %d, want 202; body=%q", name, rec.Code, rec.Body.String())
	}
	echoed := rec.Header().Get("X-Correlation-Id")
	if requestID := rec.Header().Get("X-Request-ID"); requestID != echoed {
		t.Fatalf("X-Request-ID = %q, X-Correlation-Id = %q; want equal", requestID, echoed)
	}
	return echoed
}

func latestCorrelationID(t *testing.T, conn *sql.DB) string {
	t.Helper()
	var id string
	if err := conn.QueryRow(`SELECT correlation_id FROM outbox WHERE kind = 'received' ORDER BY seq DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("read latest outbox correlation_id: %v", err)
	}
	return id
}

func assertCrockfordULID(t *testing.T, id string) {
	t.Helper()
	if id == "" || len(id) != 26 {
		t.Fatalf("correlation ID = %q (length %d), want non-empty 26-character ULID", id, len(id))
	}
	for i := range id {
		if !strings.ContainsRune(crockfordAlphabet, rune(id[i])) {
			t.Fatalf("correlation ID %q contains non-Crockford character %q at byte %d", id, id[i], i)
		}
	}
}

func firstEventFrame(t *testing.T, body io.Reader) (string, string) {
	t.Helper()
	scanner := bufio.NewScanner(body)
	var event, data string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && event != "" && event != "status":
			return event, data
		case line == "" && event == "status":
			event, data = "", ""
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SSE feed: %v", err)
	}
	t.Fatalf("SSE feed ended before an event frame")
	return "", ""
}

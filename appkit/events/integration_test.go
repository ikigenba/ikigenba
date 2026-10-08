package events_test

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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func integrationEmitter(t *testing.T, service string, sink events.Sink) *events.Emitter {
	t.Helper()
	var clockMu sync.Mutex
	clock := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e := events.New(events.Config{Service: service, Sink: sink, Stderr: io.Discard,
		Now:  func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock },
		Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 64)), Sleep: func(context.Context, time.Duration) { clockMu.Lock(); clock = clock.Add(time.Minute); clockMu.Unlock() },
		Emits: []events.Emission{{Event: "repo.pushed", Attrs: []string{"revision"}}},
	})
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	return e
}

// R-KVO8-YJ33: delivery and sibling contexts both stamp follow-up causes.
func TestIntegrationDeliveredCauseThroughSibling(t *testing.T) {
	localCapture, siblingCapture := &events.Capture{}, &events.Capture{}
	local := integrationEmitter(t, "scripts", localCapture)
	sibling := integrationEmitter(t, "repos", siblingCapture)
	caller := identity.Caller{UserID: "user", Email: "user@example.test", RequestID: "request"}
	siblingServer := httptest.NewServer(events.Middleware(identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sibling.Emit(r.Context(), "repo.pushed", events.Attrs{"revision": "sibling"})
		w.WriteHeader(http.StatusNoContent)
	}))))
	t.Cleanup(siblingServer.Close)
	delivered := events.Event{ID: "evt_0123456789abcdef", Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Service: "upstream", Name: "repo.pushed", RequestID: "original-request", User: "original-user", Attrs: events.Attrs{}, Cause: "evt_aaaaaaaaaaaaaaaa", Depth: 2, Seq: 7, Received: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)}
	body, err := delivered.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	body = append(body[:len(body)-1], []byte(`,"attempt":3}`)...)
	handler := events.DeliveryHandler(events.Handlers{"repo.pushed": func(ctx context.Context, d events.Delivery) events.Outcome {
		gotCaller, ok := identity.FromContext(ctx)
		cause, hasCause := events.FromContext(ctx)
		if !ok || gotCaller != caller || !hasCause || cause != (events.Cause{ID: delivered.ID, Depth: delivered.Depth}) || d.Attempt != 3 || !reflect.DeepEqual(d.Event, delivered) {
			return events.Fail("delivery context changed")
		}
		local.Emit(ctx, "repo.pushed", events.Attrs{"revision": "local"})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, siblingServer.URL, nil)
		if err != nil {
			return events.Fail(err.Error())
		}
		identity.Forward(gotCaller, req)
		events.Forward(ctx, req)
		resp, err := siblingServer.Client().Do(req)
		if err != nil {
			return events.Fail(err.Error())
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNoContent {
			return events.Fail("sibling request failed")
		}
		return events.OK()
	}})
	req := httptest.NewRequest(http.MethodPost, events.EventsPath, bytes.NewReader(body)).WithContext(identity.NewContext(context.Background(), caller))
	req.Header.Set("Content-Type", "application/json")
	// The delivered record, rather than these unrelated headers, supplies the cause.
	req.Header.Set(events.CauseHeader, "evt_bbbbbbbbbbbbbbbb")
	req.Header.Set(events.DepthHeader, "99")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || response.Body.String() != `{"outcome":"ok"}` {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, e := range []*events.Emitter{local, sibling} {
		if err := e.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for i, capture := range []*events.Capture{localCapture, siblingCapture} {
		got := capture.Events()
		if len(got) != 1 {
			t.Fatalf("hop %d: %+v", i, got)
		}
		if got[0].Cause != delivered.ID || got[0].Depth != delivered.Depth+1 || got[0].User != caller.UserID || got[0].RequestID != caller.RequestID {
			t.Fatalf("hop %d: %+v", i, got[0])
		}
	}
}

// R-XY0U-3KAS: actual delivery handler responses decode as matching outcomes.
func TestIntegrationSendDeliveryOutcomes(t *testing.T) {
	for _, outcome := range []events.Outcome{events.OK(), events.Skip(), events.Fail("failure \"message\"\n"), {}} {
		t.Run(outcome.Kind()+outcome.Message(), func(t *testing.T) {
			delivered := events.Event{ID: "evt_0123456789abcdef", Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Service: "repos", Name: "repo.pushed", Attrs: events.Attrs{"revision": "abc"}, Seq: 1, Received: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)}
			seen := make(chan events.Delivery, 1)
			server := httptest.NewServer(events.DeliveryHandler(events.Handlers{"repo.pushed": func(_ context.Context, d events.Delivery) events.Outcome { seen <- d; return outcome }}))
			t.Cleanup(server.Close)
			want := events.Delivery{Event: delivered, Attempt: 2}
			result, err := events.Send(context.Background(), server.Client(), strings.TrimPrefix(server.URL, "http://"), want)
			if err != nil {
				t.Fatal(err)
			}
			status := http.StatusOK
			if outcome.Kind() == events.OutcomeError {
				status = http.StatusInternalServerError
			}
			if result.Status != status || result.Outcome != outcome || result.RetryAfter != 0 {
				t.Fatal(result)
			}
			if got := <-seen; !reflect.DeepEqual(got, want) {
				t.Fatal(got, want)
			}
			b, err := delivered.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var roundtrip events.Event
			if err := json.Unmarshal(b, &roundtrip); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(roundtrip, delivered) {
				t.Fatal(roundtrip)
			}
		})
	}
}

// R-FEY1-9LUS: the default sink reaches the broker ingest handler.
func TestIntegrationEmitterSocketIngest(t *testing.T) {
	dir, err := os.MkdirTemp("", "ei-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	capture := &events.Capture{}
	server := &http.Server{Handler: events.EmitHandler(capture), ReadHeaderTimeout: time.Second}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	servicepath := filepath.Join(dir, "services.json")
	data, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "events", "url": "https://events.test", "enabled": true, "description": "bus", "mcp": false, "socket": socket}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicepath, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IKIGENBA_SERVICES", servicepath)
	emitter := integrationEmitter(t, "repos", nil)
	ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "user", RequestID: "request"})
	ctx = events.NewContext(ctx, events.Cause{ID: "evt_aaaaaaaaaaaaaaaa", Depth: 4})
	emitter.Emit(ctx, "repo.pushed", events.Attrs{"revision": "abc"})
	if err := emitter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := capture.Events()
	want := events.Event{ID: "evt_0101010101010101", Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Service: "repos", Name: "repo.pushed", RequestID: "request", User: "user", Attrs: events.Attrs{"revision": "abc"}, Cause: "evt_aaaaaaaaaaaaaaaa", Depth: 5}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatal(got, want)
	}
}

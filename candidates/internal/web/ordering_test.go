package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/pages"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/web"
)

func assertRequestTrail(t *testing.T, all []telemetry.Event, id, user, method, path string, status int) {
	t.Helper()
	list := []telemetry.Event{}
	for _, e := range all {
		if e.RequestID == id {
			list = append(list, e)
		}
	}
	if len(list) < 2 || list[0].Name != "request.started" || list[len(list)-1].Name != "request.finished" {
		t.Fatalf("request bounds %s: %#v", id, list)
	}
	started, finished := 0, 0
	for _, e := range list {
		if e.User != user {
			t.Fatalf("%s user %q want %q", e.Name, e.User, user)
		}
		if e.Name == "request.started" {
			started++
			if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"method": method, "path": path}) {
				t.Fatal(e.Attrs)
			}
		}
		if e.Name == "request.finished" {
			finished++
			if e.Attrs["status"] != int64(status) {
				t.Fatal(e.Attrs)
			}
		}
	}
	if started != 1 || finished != 1 {
		t.Fatal(started, finished)
	}
}

// R-SGAG-EJU7
func TestHandlerRequestEventOrdering(t *testing.T) {
	cfg, _, capture, _ := fixture(t)
	h := web.Handler(cfg)
	for i, tc := range []struct{ method, path, user string }{{"GET", "/", "reader"}, {"HEAD", "/about", "reader"}, {"POST", "/tools", "reader"}, {"GET", "/unknown", "reader"}, {"GET", "/_appkit/theme.css", "reader"}, {"POST", "/mcp", ""}, {"GET", "/declarations", ""}, {"POST", "/events", ""}, {"GET", "/events", "reader"}, {"GET", "/mcp/", ""}} {
		id := fmt.Sprintf("ordering-%d", i)
		r := httptest.NewRequest(tc.method, "https://candidates.example.test"+tc.path, nil)
		r.Header.Set("X-Request-Id", id)
		r.Header.Set("X-User-Id", tc.user)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if err := cfg.Telemetry.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertRequestTrail(t, capture.Events(), id, tc.user, tc.method, tc.path, w.Code)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
	caller := identity.Caller{UserID: "tool-user", RequestID: "ordering-tool"}
	if result, err := client.CallTool(context.Background(), caller, "passes", json.RawMessage(`{}`)); err != nil || result.IsError() {
		t.Fatal(result, err)
	}
	if err := cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRequestTrail(t, capture.Events(), caller.RequestID, caller.UserID, "POST", "/mcp", 200)
	found := false
	for _, e := range capture.Events() {
		if e.RequestID == caller.RequestID && e.Name == "tool.called" {
			found = true
		}
	}
	if !found {
		t.Fatal("tool event missing")
	}
	r := httptest.NewRequest("GET", "/unknown", nil)
	r.Header.Set("X-User-Id", "minted-user")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if err := cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	all := capture.Events()
	last := all[len(all)-1]
	if last.RequestID == "" {
		t.Fatal(last)
	}
	assertRequestTrail(t, all, last.RequestID, "minted-user", "GET", "/unknown", 404)
}

type orderingEventSink struct {
	capture  events.Capture
	finished chan struct{}
}

func (s *orderingEventSink) Deliver(ctx context.Context, e events.Event) error {
	if err := s.capture.Deliver(ctx, e); err != nil {
		return err
	}
	if e.Name == trail.Finished {
		select {
		case s.finished <- struct{}{}:
		default:
		}
	}
	return nil
}

// R-SGAG-EJU7: delivery record and background rows use the service identity.
func TestDeliveryReadIdentityAndOrdering(t *testing.T) {
	cfg, _, capture, _ := fixture(t)
	dir, err := os.MkdirTemp("", "candidate-order-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	socket := filepath.Join(dir, "scrapes.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	rowsEntered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblockRows := func() { releaseOnce.Do(func() { close(release) }) }
	received := make(chan identity.Caller, 2)
	fake := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- identity.Caller{UserID: r.Header.Get("X-User-Id"), Email: r.Header.Get("X-User-Email"), RequestID: r.Header.Get("X-Request-Id")}
		switch r.URL.Path {
		case "/runs/run_order/record":
			if err := json.NewEncoder(w).Encode(map[string]any{"id": "run_order", "scraper": "fmcsa", "status": "done", "started": "2026-10-05T09:00:07Z", "rows": 0, "new": 0, "changed": 0, "unchanged": 0, "gone": 0}); err != nil {
				t.Error(err)
			}
		case "/runs/run_order/rows":
			rowsEntered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(200)
		default:
			t.Errorf("unexpected read %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})}
	done := make(chan error, 1)
	go func() { done <- fake.Serve(ln) }()
	t.Cleanup(func() {
		if err := fake.Close(); err != nil {
			t.Error(err)
		}
		err := <-done
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	})
	sink := &orderingEventSink{finished: make(chan struct{}, 1)}
	em := events.New(events.Config{Service: pages.ServiceName, Sink: sink, Telemetry: cfg.Telemetry, Now: cfg.Now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x6a}, 1000)), Stderr: io.Discard, Emits: trail.Emits()})
	sc := scrapes.NewClient(scrapes.ClientConfig{Service: services.Entry{Name: "scrapes", Socket: socket}, Telemetry: cfg.Telemetry})
	eng := passes.New(passes.Config{Store: cfg.Store, Sources: sources.New(fmcsa.New()), Scrapes: sc, Telemetry: cfg.Telemetry, Events: em})
	cfg.Passes = eng
	cfg.Events = em
	t.Cleanup(func() { eng.Drain(context.Background()); em.Shutdown(context.Background()) })
	// Cleanups run in reverse order, so unblock the fake before draining the engine.
	t.Cleanup(unblockRows)
	h := web.Handler(cfg)
	event := events.Event{ID: "evt_1212121212121212", Time: cfg.Now(), Received: cfg.Now(), Seq: 1, Service: "scrapes", Name: trail.Accepted, Attrs: events.Attrs{"run": "run_order", "scraper": "fmcsa", "status": "done"}}
	body, err := event.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	body = append(body[:len(body)-1], []byte(`,"attempt":1}`)...)
	r := httptest.NewRequest("POST", "/events", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-Id", "delivery-request")
	r.Header.Set("X-User-Id", "delivery-caller")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-rowsEntered:
	case <-ctx.Done():
		t.Fatal("rows read not begun")
	}
	if err := cfg.Telemetry.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	before := capture.Events()
	var synchronous []telemetry.Event
	for _, e := range before {
		if e.RequestID == "delivery-request" {
			synchronous = append(synchronous, e)
		}
	}
	if len(synchronous) < 4 || synchronous[0].Name != "request.started" || synchronous[len(synchronous)-1].Name != "request.finished" {
		t.Fatal(synchronous)
	}
	recordCalls, starts, finishes := 0, 0, 0
	for _, e := range synchronous {
		wantUser := "delivery-caller"
		if e.Name == "sibling.called" {
			recordCalls++
			wantUser = pages.ServiceName
		}
		if e.User != wantUser {
			t.Fatal(e)
		}
		if e.Name == "request.started" {
			starts++
			if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"method": "POST", "path": "/events"}) {
				t.Fatal(e)
			}
		}
		if e.Name == "request.finished" {
			finishes++
			if e.Attrs["status"] != int64(200) {
				t.Fatal(e)
			}
		}
	}
	if recordCalls != 1 || starts != 1 || finishes != 1 {
		t.Fatal(recordCalls, starts, finishes)
	}
	unblockRows()
	select {
	case <-sink.finished:
	case <-ctx.Done():
		t.Fatal("pass did not finish")
	}
	if err := cfg.Telemetry.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	all := capture.Events()
	calls := 0
	finishIndex := -1
	rowIndex := -1
	for i, e := range all {
		if e.RequestID != "delivery-request" {
			continue
		}
		if e.Name == "request.finished" {
			finishIndex = i
		}
		if e.Name == "sibling.called" {
			calls++
			if e.User != pages.ServiceName {
				t.Fatal(e)
			}
			if calls == 2 {
				rowIndex = i
			}
		}
	}
	if calls != 2 || rowIndex <= finishIndex {
		t.Fatal("background read not after response", calls, rowIndex, finishIndex)
	}
	for range 2 {
		select {
		case c := <-received:
			if c != (identity.Caller{UserID: pages.ServiceName, RequestID: "delivery-request"}) {
				t.Fatal(c)
			}
		case <-ctx.Done():
			t.Fatal("fake missing request")
		}
	}
	for _, e := range sink.capture.Events() {
		if e.RequestID != "delivery-request" || e.User != "delivery-caller" {
			t.Fatal(e)
		}
	}
}

package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/cli"
)

type trailWrites struct {
	mu    sync.Mutex
	calls [][]byte
}

func (w *trailWrites) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, bytes.Clone(p))
	return len(p), nil
}
func (w *trailWrites) snapshot() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([][]byte, len(w.calls))
	for i, p := range w.calls {
		result[i] = bytes.Clone(p)
	}
	return result
}

type trailSinkFunc func(context.Context, telemetry.Event) error

func (f trailSinkFunc) Deliver(c context.Context, e telemetry.Event) error { return f(c, e) }
func trailTake[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for signal")
		var z T
		return z
	}
}
func trailSibling(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	path := filepath.Join(shortDir(t), "trail-sibling")
	l, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return path
}
func trailService(name, socket string) map[string]any {
	return map[string]any{"name": name, "description": "", "url": "https://" + name + ".test", "socket": socket, "enabled": true, "mcp": false}
}
func trailWaitPaused(t *testing.T, f *runFixture) {
	t.Helper()
	limit := time.Now().Add(5 * time.Second)
	for {
		result := call(t, f, "subscribers", map[string]any{}, "wait-paused")
		data, e := json.Marshal(result)
		if e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(data, []byte(`"status":"paused"`)) {
			return
		}
		if time.Now().After(limit) {
			t.Fatal("subscriber never paused", string(data))
		}
	}
}

func TestCompleteToolTrailAndJSONRPCError(t *testing.T) {
	// R-9FOL-TQG6 R-HH81-3BX6
	socket, received := sibling(t, false)
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{trailService("repos", socket)})
	f := startRun(t, t.TempDir(), map[string]string{"IKIGENBA_SERVICES": path, "EVENTS_DELIVERY_ATTEMPTS": "1"})
	cases := []struct {
		name, id, kind, outcome string
		args                    map[string]any
	}{
		{"catalog", "catalog-full", "read", "ok", map[string]any{}},
		{"search", "search-full", "read", "ok", map[string]any{}},
		{"subscribers", "subscribers-full", "read", "ok", map[string]any{}},
		{"skip", "skip-error", "destructive", "error", map[string]any{"service": "absent"}},
		{"resume", "resume-error", "additive", "error", map[string]any{"service": "absent"}},
		{"skip", "skip-invalid", "destructive", "invalid_arguments", map[string]any{}},
		{"resume", "resume-invalid", "additive", "invalid_arguments", map[string]any{}},
	}
	invoke := func(tc struct {
		name, id, kind, outcome string
		args                    map[string]any
	}) {
		data, e := json.Marshal(tc.args)
		if e != nil {
			t.Fatal(e)
		}
		result, e := f.mcp().CallTool(context.Background(), identity.Caller{UserID: "tool-user-" + tc.id, Email: "private-tool-email", RequestID: tc.id}, tc.name, data)
		if e != nil {
			t.Fatal(e)
		}
		if result.IsError() != (tc.outcome != "ok") {
			t.Fatalf("%s result error %v", tc.id, result.IsError())
		}
	}
	for _, tc := range cases {
		invoke(tc)
	}
	emit(t, f, emitted(f, 11, 0), 204)
	trailTake(t, received)
	trailWaitPaused(t, f)
	resume := struct {
		name, id, kind, outcome string
		args                    map[string]any
	}{"resume", "resume-ok", "additive", "ok", map[string]any{"service": "repos"}}
	invoke(resume)
	cases = append(cases, resume)
	trailTake(t, received)
	trailWaitPaused(t, f)
	skip := struct {
		name, id, kind, outcome string
		args                    map[string]any
	}{"skip", "skip-ok", "destructive", "ok", map[string]any{"service": "repos"}}
	invoke(skip)
	cases = append(cases, skip)
	if _, e := f.mcp().ListTools(context.Background(), identity.Caller{UserID: "list-user", RequestID: "list-no-tool"}); e != nil {
		t.Fatal(e)
	}
	_, rpcErr := f.mcp().CallTool(context.Background(), identity.Caller{UserID: "unknown-user", RequestID: "unknown-no-tool"}, "missing_tool", json.RawMessage(`{}`))
	var protocolError *mcp.RPCError
	if !errors.As(rpcErr, &protocolError) {
		t.Fatalf("unknown tool returned %v instead of JSON-RPC error", rpcErr)
	}
	f.stop(t)
	for _, tc := range cases {
		var recorded []telemetry.Event
		for _, e := range f.capture.Events() {
			if e.RequestID == tc.id && e.Name == "tool.called" {
				recorded = append(recorded, e)
			}
		}
		if len(recorded) != 1 {
			t.Fatalf("%s tool records %#v", tc.id, recorded)
		}
		e := recorded[0]
		duration, ok := e.Attrs["duration_us"].(int64)
		if e.User != "tool-user-"+tc.id || !ok || duration < 0 || (tc.outcome == "invalid_arguments" && duration != 0) || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"tool": tc.name, "kind": tc.kind, "outcome": tc.outcome, "duration_us": duration}) {
			t.Fatalf("%s full tool record %#v", tc.id, e)
		}
	}
	for _, id := range []string{"list-no-tool", "unknown-no-tool"} {
		var names []string
		for _, e := range f.capture.Events() {
			if e.RequestID == id {
				names = append(names, e.Name)
			}
		}
		if !reflect.DeepEqual(names, []string{"request.started", "request.finished"}) {
			t.Fatalf("%s records %v", id, names)
		}
	}
}

func TestSuccessfulDeliveryTrailOrdering(t *testing.T) {
	// R-HKVQ-8N59
	received := make(chan appEvents.Event, 1)
	socket := trailSibling(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/declarations":
			_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":[]}],"accepts":["repo.pushed"]}`)
		case "/events":
			var e struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
				t.Error(err)
			}
			received <- appEvents.Event{ID: e.ID}
			_, _ = io.WriteString(w, `{"outcome":"ok"}`)
		default:
			w.WriteHeader(404)
		}
	})
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{trailService("repos", socket)})
	f := startRun(t, t.TempDir(), map[string]string{"IKIGENBA_SERVICES": path})
	e := emitted(f, 12, 0)
	emit(t, f, e, 204)
	if got := trailTake(t, received); got.ID != e.ID {
		t.Fatal(got)
	}
	limit := time.Now().Add(5 * time.Second)
	for {
		seen := false
		for _, record := range f.capture.Events() {
			if record.Name == "event.delivered" && record.Attrs["event"] == e.ID {
				seen = true
			}
		}
		if seen {
			break
		}
		if time.Now().After(limit) {
			t.Fatal("delivery outcome never recorded")
		}
	}
	f.stop(t)
	accepted, attempt, delivered := -1, -1, -1
	for i, record := range f.capture.Events() {
		switch {
		case record.Name == "event.accepted" && record.Attrs["event"] == e.ID:
			if accepted != -1 {
				t.Fatal("duplicate acceptance")
			}
			accepted = i
		case record.Name == "sibling.called" && record.Attrs["path"] == "/events":
			if attempt != -1 {
				t.Fatal("duplicate delivery attempt")
			}
			attempt = i
		case record.Name == "event.delivered" && record.Attrs["event"] == e.ID:
			if delivered != -1 {
				t.Fatal("duplicate outcome")
			}
			delivered = i
		}
	}
	if accepted < 0 || attempt <= accepted || delivered <= attempt {
		t.Fatalf("record indexes accepted=%d attempt=%d delivered=%d", accepted, attempt, delivered)
	}
}

func TestUndeliveredTrailPrivacyAndOneWritePerRecord(t *testing.T) {
	// R-F9VJ-ZS40 R-9XI6-OAME
	received := make(chan struct{}, 1)
	socket := trailSibling(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/declarations":
			_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":[]}],"accepts":["repo.pushed"]}`)
		case "/events":
			_, _ = io.Copy(io.Discard, r.Body)
			received <- struct{}{}
			_, _ = io.WriteString(w, `{"outcome":"ok"}`)
		default:
			w.WriteHeader(404)
		}
	})
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{trailService("repos", socket)})
	diagnostics := &trailWrites{}
	f := startRun(t, t.TempDir(), map[string]string{"IKIGENBA_SERVICES": path}, func(f *runFixture) {
		f.p.Stderr = diagnostics
		f.p.Sink = trailSinkFunc(func(c context.Context, e telemetry.Event) error {
			if err := f.capture.Deliver(c, e); err != nil {
				return err
			}
			return telemetry.ErrRejected
		})
		f.p.Sleep = func(context.Context, time.Duration) { t.Error("rejected record retried") }
	})
	event := emitted(f, 13, 0)
	event.Attrs = appEvents.Attrs{"bus_private_key_token": "bus_private_value_token"}
	emit(t, f, event, 204)
	trailTake(t, received)
	r, e := http.NewRequest("GET", "http://events.test/?query_private_token", nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("X-User-Id", "privacy-user")
	r.Header.Set("X-User-Email", "email_private_token")
	r.Header.Set("X-Request-Id", "privacy-page")
	r.Header.Set("X-Forwarded-Proto", "https")
	resp, e := f.client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	_ = body(t, resp)
	args := json.RawMessage(`{"attrs":{"tool_private_key_token":"tool_private_value_token"}}`)
	result, e := f.mcp().CallTool(context.Background(), identity.Caller{UserID: "privacy-user", Email: "email_private_token", RequestID: "privacy-tool"}, "search", args)
	if e != nil || result.IsError() {
		t.Fatal(result, e)
	}
	f.stop(t)
	records := f.capture.Events()
	calls := diagnostics.snapshot()
	if len(records) == 0 || len(calls) != len(records) {
		t.Fatalf("records %d writes %d", len(records), len(calls))
	}
	started := 0
	accepted := 0
	tool := 0
	for i, record := range records {
		encoded, e := record.MarshalJSON()
		if e != nil {
			t.Fatal(e)
		}
		want := append([]byte("events: undelivered event: "), encoded...)
		want = append(want, '\n')
		if !bytes.Equal(calls[i], want) {
			t.Fatalf("write %d = %q want %q", i, calls[i], want)
		}
		if record.Service != appEvents.ServiceName || !record.Time.Equal(f.fixed.UTC().Truncate(time.Microsecond)) {
			t.Fatalf("identity/time %#v", record)
		}
		switch record.Name {
		case "service.started":
			started++
			if !reflect.DeepEqual(record.Attrs, telemetry.Attrs{"version": f.p.Version}) {
				t.Fatal(record)
			}
		case "event.accepted":
			accepted++
			if record.Attrs["event"] != event.ID {
				t.Fatal(record)
			}
		case "tool.called":
			if record.RequestID == "privacy-tool" {
				tool++
			}
		}
		for _, private := range []string{"bus_private_key_token", "bus_private_value_token", "email_private_token", "query_private_token", "tool_private_key_token", "tool_private_value_token"} {
			if bytes.Contains(calls[i], []byte(private)) {
				t.Fatalf("undelivered record leaked %s: %s", private, calls[i])
			}
		}
	}
	if started != 1 || accepted != 1 || tool != 1 || f.out.Len() != 0 {
		t.Fatalf("start=%d accepted=%d tool=%d stdout=%q", started, accepted, tool, f.out.String())
	}
}

func TestCutoffDiagnosticIsOneLastWrite(t *testing.T) {
	// R-9L5L-PXWY
	diagnostics := &trailWrites{}
	f := startRun(t, t.TempDir(), map[string]string{"DRAIN_SECONDS": "1"}, func(f *runFixture) { f.p.Stderr = diagnostics })
	conn, e := f.client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = conn.Close() }()
	_, e = io.WriteString(conn, "POST /emit HTTP/1.1\r\nHost: events.test\r\nContent-Type: application/json\r\nContent-Length: 200\r\nExpect: 100-continue\r\n\r\n")
	if e != nil {
		t.Fatal(e)
	}
	if e := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); e != nil {
		t.Fatal(e)
	}
	reader := bufio.NewReader(conn)
	status, e := reader.ReadString('\n')
	if e != nil || status != "HTTP/1.1 100 Continue\r\n" {
		t.Fatal(status, e)
	}
	if _, e := reader.ReadString('\n'); e != nil {
		t.Fatal(e)
	}
	if _, e := io.WriteString(conn, "{"); e != nil {
		t.Fatal(e)
	}
	began := time.Now()
	f.cancel(errors.New("SIGTERM"))
	if code := trailTake(t, f.done); code != cli.ExitFailure {
		t.Fatal(code)
	}
	if time.Since(began) >= 2*time.Second {
		t.Fatal("late cutoff")
	}
	f.client.CloseIdleConnections()
	calls := diagnostics.snapshot()
	want := []byte("events: stopped with 1 request unfinished\n")
	if len(calls) < 2 || !bytes.Equal(calls[len(calls)-1], want) {
		t.Fatalf("diagnostic writes %q", calls)
	}
	count := 0
	for _, call := range calls {
		if bytes.Equal(call, want) {
			count++
		} else if !bytes.HasPrefix(call, []byte("events: undelivered event: ")) {
			t.Fatalf("unexpected write %q", call)
		}
	}
	if count != 1 || f.out.Len() != 0 {
		t.Fatalf("diagnostic count %d stdout %q", count, f.out.String())
	}
}

func TestGlobalLoggerSilentForDeliveryAndBadRefreshes(t *testing.T) {
	// R-AZ4W-FGNR
	logs := &trailWrites{}
	previous := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(previous)
	refreshes := make(chan chan time.Time, 8)
	var firstCalls, secondCalls atomic.Int64
	received := make(chan struct{}, 1)
	first := trailSibling(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/declarations":
			if firstCalls.Add(1) > 1 {
				w.WriteHeader(500)
				return
			}
			_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":[]}],"accepts":["repo.pushed"]}`)
		case "/events":
			_, _ = io.Copy(io.Discard, r.Body)
			received <- struct{}{}
			w.WriteHeader(500)
			_, _ = io.WriteString(w, "delivery failed")
		default:
			w.WriteHeader(404)
		}
	})
	second := trailSibling(t, func(w http.ResponseWriter, _ *http.Request) {
		if secondCalls.Add(1) > 1 {
			_, _ = io.WriteString(w, "not JSON")
			return
		}
		_, _ = io.WriteString(w, `{"emits":[],"accepts":[]}`)
	})
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{trailService("repos", first), trailService("other", second)})
	f := startRun(t, t.TempDir(), map[string]string{"IKIGENBA_SERVICES": path, "EVENTS_DEPTH_MAX": "1", "EVENTS_DELIVERY_ATTEMPTS": "1"}, func(f *runFixture) {
		f.p.RefreshAfter = func(time.Duration) <-chan time.Time { tick := make(chan time.Time, 1); refreshes <- tick; return tick }
	})
	tick := trailTake(t, refreshes)
	for _, path := range []string{"/", "/nope"} {
		_ = body(t, f.request(t, "GET", path, "", "logger-page"))
	}
	if _, e := f.mcp().ListTools(context.Background(), identity.Caller{UserID: "user", RequestID: "logger-list"}); e != nil {
		t.Fatal(e)
	}
	emit(t, f, emitted(f, 14, 2), 422)
	emit(t, f, emitted(f, 15, 0), 204)
	trailTake(t, received)
	trailWaitPaused(t, f)
	tick <- f.fixed
	trailTake(t, refreshes)
	limit := time.Now().Add(5 * time.Second)
	for {
		seen := map[string]int{}
		for _, record := range f.capture.Events() {
			if record.Name == "sibling.called" && record.Attrs["path"] == "/declarations" {
				target, _ := record.Attrs["target"].(string)
				seen[target]++
			}
		}
		if seen["repos"] >= 2 && seen["other"] >= 2 {
			break
		}
		if time.Now().After(limit) {
			t.Fatal("refresh asks did not complete", seen)
		}
	}
	if firstCalls.Load() != 2 || secondCalls.Load() != 2 {
		t.Fatalf("refresh calls %d %d", firstCalls.Load(), secondCalls.Load())
	}
	f.cancel(errors.New("SIGTERM"))
	if code := trailTake(t, f.done); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	f.client.CloseIdleConnections()
	if calls := logs.snapshot(); len(calls) != 0 {
		t.Fatalf("global logger wrote %q", calls)
	}
}

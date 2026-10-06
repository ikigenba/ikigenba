package events_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/services"
)

type eventWireSink func(context.Context, events.Event) error

func (f eventWireSink) Deliver(ctx context.Context, e events.Event) error { return f(ctx, e) }
func eventWireEvent() events.Event {
	return events.Event{ID: "evt_0123456789abcdef", Time: time.Date(2024, 2, 29, 12, 34, 56, 123456000, time.UTC), Service: "unit", Name: "thing.finished", RequestID: "req", User: "usr", Attrs: events.Attrs{}}
}
func eventWireJSON(t *testing.T, e events.Event) string {
	t.Helper()
	body, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func eventWireRequest(h http.Handler, method, media, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, events.EmitPath, strings.NewReader(body))
	if media != "" {
		r.Header.Set("Content-Type", media)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func eventWireStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want || w.Body.Len() != 0 {
		t.Fatalf("status/body: %d %q, want %d and empty", w.Code, w.Body.String(), want)
	}
}
func eventWireDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "evw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}
func eventWireSocket(t *testing.T, h http.Handler) string {
	t.Helper()
	path := filepath.Join(eventWireDir(t), "sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(h)
	if err := server.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return path
}
func eventWireServices(t *testing.T, path string, sockets ...string) {
	t.Helper()
	entries := make([]map[string]any, 0, len(sockets))
	for _, socket := range sockets {
		entries = append(entries, map[string]any{"name": "events", "url": "http://events", "description": "bus", "socket": socket, "enabled": false, "mcp": false})
	}
	body, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
}
func eventWireDiscover(t *testing.T, socket string) string {
	t.Helper()
	path := filepath.Join(eventWireDir(t), "services.json")
	eventWireServices(t, path, socket)
	t.Setenv(services.Variable, path)
	return path
}

func eventWireFactory(f func() events.Sink) func() events.Sink { return f }
func eventWireHandlerFactory(f func(events.Sink) http.Handler) func(events.Sink) http.Handler {
	return f
}

// R-GLMI-NKT3 R-GMUF-1CJS R-GO2B-F4AH R-GZ1E-V1YQ
func TestEventWireExports(t *testing.T) {
	factory := eventWireFactory(events.NewSocketSink)
	handler := eventWireHandlerFactory(events.EmitHandler)
	const integer uint64 = events.MaxEventBytes
	const floating float64 = events.MaxEventBytes
	if events.ServiceName != "events" || events.EmitPath != "/emit" || events.EventsPath != "/events" || events.DeclarationsPath != "/declarations" || integer != 65536 || floating != 65536 {
		t.Fatal("wire constants")
	}
	if factory() == nil {
		t.Fatal("nil socket sink")
	}
	defer func() {
		p := recover()
		if p == nil || !strings.Contains(fmt.Sprint(p), "sink") || !strings.Contains(fmt.Sprint(p), "nil") {
			t.Fatalf("panic: %v", p)
		}
	}()
	handler(nil)
}

// R-H09B-8TPF R-H1H7-MLG4 R-H2P4-0D6T R-H7KP-JG5L
func TestEventEmitValidationOrder(t *testing.T) {
	calls := 0
	h := events.EmitHandler(eventWireSink(func(context.Context, events.Event) error { calls++; return nil }))
	large := strings.Repeat("x", events.MaxEventBytes+1)
	for _, method := range []string{"GET", "HEAD", "OPTIONS", "PUT", "PATCH", "DELETE", "CUSTOM"} {
		w := eventWireRequest(h, method, "bad;", large)
		eventWireStatus(t, w, 405)
		if !reflect.DeepEqual(w.Header().Values("Allow"), []string{"POST"}) {
			t.Fatal(w.Header())
		}
	}
	for _, media := range []string{"", "text/plain", "application/jsonx", "application/json; broken"} {
		eventWireStatus(t, eventWireRequest(h, "POST", media, large), 415)
	}
	r := httptest.NewRequest("POST", events.EmitPath, strings.NewReader(eventWireJSON(t, eventWireEvent())))
	r.Header["Content-Type"] = []string{"text/plain", "application/json"}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	eventWireStatus(t, w, 415)
	eventWireStatus(t, eventWireRequest(h, "POST", "application/json; charset=utf-8", large), 413)
	if calls != 0 {
		t.Fatal(calls)
	}
	body := eventWireJSON(t, eventWireEvent())
	body += strings.Repeat(" ", events.MaxEventBytes-len(body))
	r = httptest.NewRequest("POST", events.EmitPath, strings.NewReader(body))
	r.Header["Content-Type"] = []string{"application/json; charset=utf-8", "text/plain"}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	eventWireStatus(t, w, 204)
	if calls != 1 {
		t.Fatal(calls)
	}
}

// R-H3X0-E4XI R-H7KP-JG5L
func TestEventEmitInvalidText(t *testing.T) {
	body := eventWireJSON(t, eventWireEvent())
	cases := []string{"", "null", "[]", body + body, body + " x", body[:len(body)-1]}
	for _, field := range []string{"id", "time", "service", "event", "request_id", "user", "attrs", "cause", "depth"} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &object); err != nil {
			t.Fatal(err)
		}
		delete(object, field)
		changed, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, string(changed), strings.TrimSuffix(body, "}")+`,"`+field+`":null}`)
	}
	for _, pair := range [][2]string{
		{`"id":"evt_0123456789abcdef"`, `"id":"bad"`},
		{`"service":"unit"`, `"service":""`}, {`"event":"thing.finished"`, `"event":"Bad.Name"`},
		{`"request_id":"req"`, `"request_id":null`}, {`"user":"usr"`, `"user":true`},
		{`"time":"2024-02-29T12:34:56.123456Z"`, `"time":"2024-02-29T12:34:56.12345Z"`},
		{`"attrs":{}`, `"attrs":null`}, {`"attrs":{}`, `"attrs":{"x":null}`}, {`"attrs":{}`, `"attrs":{"x":{}}`}, {`"attrs":{}`, `"attrs":{"x":[]}`},
		{`"attrs":{}`, `"attrs":{"Bad":1}`}, {`"attrs":{}`, `"attrs":{"x":1,"\u0078":2}`}, {`"attrs":{}`, `"attrs":{"x":1e999}`},
		{`"depth":0`, `"depth":1`}, {`"depth":0`, `"depth":-1`}, {`"depth":0`, `"depth":0.0`}, {`"cause":""`, `"cause":"bad"`},
		{`"cause":""`, `"cause":"evt_ffffffffffffffff"`},
	} {
		cases = append(cases, strings.Replace(body, pair[0], pair[1], 1))
	}
	cases = append(cases, strings.TrimSuffix(body, "}")+`,"seq":0}`, strings.TrimSuffix(body, "}")+`,"received":"2024-02-29T12:34:56.123456Z"}`, strings.TrimSuffix(body, "}")+`,"extra":1}`, strings.TrimSuffix(body, "}")+`,"\u0075ser":"other"}`, strings.Replace(body, "unit", string([]byte{0xff}), 1))
	delivered := eventWireEvent()
	delivered.Seq = 1
	delivered.Received = delivered.Time
	cases = append(cases, eventWireJSON(t, delivered))
	calls := 0
	h := events.EmitHandler(eventWireSink(func(context.Context, events.Event) error { calls++; return nil }))
	for i, invalid := range cases {
		w := eventWireRequest(h, "POST", "application/json", invalid)
		if w.Code != 400 {
			t.Fatalf("case %d: %d for %s", i, w.Code, invalid)
		}
		eventWireStatus(t, w, 400)
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}

// R-1MTX-6OVB R-H3X0-E4XI R-H7KP-JG5L
func TestEventEmitCanonicalSize(t *testing.T) {
	e := eventWireEvent()
	e.Attrs = events.Attrs{"text": strings.Repeat("<>&", 4000)}
	canonical := eventWireJSON(t, e)
	raw := strings.NewReplacer(`\u003c`, "<", `\u003e`, ">", `\u0026`, "&").Replace(canonical)
	if len(raw) > events.MaxEventBytes || len(canonical) <= events.MaxEventBytes {
		t.Fatal("bad size fixture")
	}
	calls := 0
	h := events.EmitHandler(eventWireSink(func(context.Context, events.Event) error { calls++; return nil }))
	eventWireStatus(t, eventWireRequest(h, "POST", "application/json", raw), 413)
	invalid := strings.TrimSuffix(raw, "}") + `,"extra":1}`
	eventWireStatus(t, eventWireRequest(h, "POST", "application/json", invalid), 400)
	if calls != 0 {
		t.Fatal(calls)
	}
	e = eventWireEvent()
	e.Service += strings.Repeat("a", events.MaxEventBytes-len(eventWireJSON(t, e)))
	boundary := eventWireJSON(t, e)
	if len(boundary) != events.MaxEventBytes {
		t.Fatal(len(boundary))
	}
	eventWireStatus(t, eventWireRequest(h, "POST", "application/json", boundary), 204)
	if calls != 1 {
		t.Fatal(calls)
	}
}

// R-1O1T-KGM0 R-1P9P-Y8CP R-H7KP-JG5L
func TestEventEmitSinkContextAndStatuses(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "marker"))
	cancel()
	body := strings.Replace(eventWireJSON(t, eventWireEvent()), `"attrs":{}`, `"attrs":{"s":"\ud800","i":-9223372036854775808,"u":18446744073709551615,"f":1.25,"z":-0,"yes":true}`, 1)
	expected := eventWireEvent()
	expected.Attrs = events.Attrs{"s": "\ufffd", "i": int64(math.MinInt64), "u": uint64(math.MaxUint64), "f": 1.25, "z": math.Copysign(0, -1), "yes": true}
	for _, test := range []struct {
		err  error
		code int
	}{{nil, 204}, {fmt.Errorf("policy: %w", events.ErrRejected), 422}, {errors.New("store unavailable"), 500}} {
		calls := 0
		h := events.EmitHandler(eventWireSink(func(gotCtx context.Context, e events.Event) error {
			calls++
			if gotCtx.Value(key{}) != "marker" || !errors.Is(gotCtx.Err(), context.Canceled) {
				t.Error("context values or cancellation lost")
			}
			if !reflect.DeepEqual(e, expected) || !math.Signbit(e.Attrs["z"].(float64)) {
				t.Errorf("decoded event: %#v", e)
			}
			return test.err
		}))
		r := httptest.NewRequest("POST", events.EmitPath, strings.NewReader(body)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		eventWireStatus(t, w, test.code)
		if calls != 1 {
			t.Fatal(calls)
		}
	}
}

// R-HA0I-AZMZ R-HB8E-ORDO R-H7KP-JG5L
func TestEventEmitCanonicalRoundTripsConcurrent(t *testing.T) {
	type text string
	type number int16
	input := []events.Event{eventWireEvent(), eventWireEvent(), eventWireEvent()}
	input[0].Attrs = nil
	input[1].Service = "日本語<>&"
	input[1].RequestID = "\nrequest"
	input[1].User = "é"
	input[1].Cause = "evt_ffffffffffffffff"
	input[1].Depth = 7
	input[1].Attrs = events.Attrs{"text": text("<>&日本語"), "integer": number(-7), "unsigned": uint64(math.MaxUint64), "zero": math.Copysign(0, -1), "yes": true, "fraction": float32(0.5)}
	input[2].Time = input[2].Time.In(time.FixedZone("offset", 3600))
	input[2].Attrs = events.Attrs{"small": float64(1e-9)}
	type bodyKey struct{}
	var calls atomic.Int64
	h := events.EmitHandler(eventWireSink(func(ctx context.Context, e events.Event) error {
		expected, _ := ctx.Value(bodyKey{}).(string)
		got, err := e.MarshalJSON()
		if err != nil || string(got) != expected {
			t.Errorf("roundtrip: %s %v, want %s", got, err, expected)
		}
		calls.Add(1)
		return nil
	}))
	var group sync.WaitGroup
	for i := 0; i < 48; i++ {
		body := eventWireJSON(t, input[i%len(input)])
		group.Go(func() {
			ctx := context.WithValue(context.Background(), bodyKey{}, body)
			r := httptest.NewRequest("POST", events.EmitPath, strings.NewReader(body)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			h.ServeHTTP(out, r)
			eventWireStatus(t, out, 204)
		})
	}
	group.Wait()
	if calls.Load() != 48 {
		t.Fatal(calls.Load())
	}
}

// R-GPA7-SW16 R-GRQ0-KFIK
func TestEventSocketDiscoveryAndRequest(t *testing.T) {
	e := eventWireEvent()
	e.Attrs = events.Attrs{"text": "<>&"}
	body := eventWireJSON(t, e)
	var first, second atomic.Int64
	check := func(count *atomic.Int64) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			got, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			if r.Method != "POST" || r.Host != "events" || r.URL.Path != "/emit" || r.URL.RawQuery != "" || r.Proto != "HTTP/1.1" || !reflect.DeepEqual(r.Header.Values("Content-Type"), []string{"application/json"}) || string(got) != body {
				t.Errorf("request: %s %s %s %v %s", r.Method, r.Host, r.URL, r.Header, got)
			}
			w.WriteHeader(204)
		})
	}
	socketA := eventWireSocket(t, check(&first))
	socketB := eventWireSocket(t, check(&second))
	path := eventWireDiscover(t, socketA)
	sink := events.NewSocketSink()
	if err := sink.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	eventWireServices(t, path, socketB, socketA)
	if err := sink.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	alternate := filepath.Join(eventWireDir(t), "alternate.json")
	eventWireServices(t, alternate, socketA)
	t.Setenv(services.Variable, alternate)
	if err := sink.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if first.Load() != 2 || second.Load() != 1 {
		t.Fatalf("requests: %d %d", first.Load(), second.Load())
	}
}

// R-GSXW-Y799
func TestEventSocketStatusClassesAndRedirect(t *testing.T) {
	var calls, status atomic.Int64
	socket := eventWireSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "http://events/redirected")
		w.WriteHeader(int(status.Load()))
	}))
	eventWireDiscover(t, socket)
	sink := events.NewSocketSink()
	codes := []int{200, 201, 202, 204, 206, 299, 300, 301, 302, 303, 307, 308, 399, 400, 401, 403, 404, 413, 422, 499, 500, 503, 599}
	for _, code := range codes {
		status.Store(int64(code))
		err := sink.Deliver(context.Background(), eventWireEvent())
		switch {
		case code >= 200 && code <= 299:
			if err != nil {
				t.Fatalf("%d: %v", code, err)
			}
		case code >= 400 && code <= 499:
			if !errors.Is(err, events.ErrRejected) {
				t.Fatalf("%d: %v", code, err)
			}
		default:
			if err == nil || errors.Is(err, events.ErrRejected) {
				t.Fatalf("%d: %v", code, err)
			}
		}
	}
	if calls.Load() != int64(len(codes)) {
		t.Fatal("redirect followed or requests repeated", calls.Load())
	}
}

// R-GU5T-BYZY
func TestEventSocketDiscoveryFailures(t *testing.T) {
	var calls atomic.Int64
	socket := eventWireSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	sink := events.NewSocketSink()
	dir := eventWireDir(t)
	paths := []string{"", filepath.Join(dir, "missing"), dir, filepath.Join(dir, "malformed"), filepath.Join(dir, "absent"), filepath.Join(dir, "unconnected")}
	if err := os.WriteFile(paths[3], []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	eventWireServices(t, paths[4])
	eventWireServices(t, paths[5], filepath.Join(dir, "no-socket"))
	for _, path := range paths {
		t.Setenv(services.Variable, path)
		err := sink.Deliver(context.Background(), eventWireEvent())
		if err == nil || errors.Is(err, events.ErrRejected) {
			t.Fatalf("path %q: %v", path, err)
		}
	}
	if err := os.Unsetenv(services.Variable); err != nil {
		t.Fatal(err)
	}
	if err := sink.Deliver(context.Background(), eventWireEvent()); err == nil || errors.Is(err, events.ErrRejected) {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
	eventWireDiscover(t, socket)
	if err := sink.Deliver(context.Background(), eventWireEvent()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

// R-GU5T-BYZY
func TestEventSocketUnreadableAnswer(t *testing.T) {
	socket := eventWireSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("no hijacker")
			return
		}
		conn, buffer, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := buffer.WriteString("invalid HTTP answer\r\n\r\n"); err != nil {
			t.Error(err)
		}
		if err := buffer.Flush(); err != nil {
			t.Error(err)
		}
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}))
	eventWireDiscover(t, socket)
	if err := events.NewSocketSink().Deliver(context.Background(), eventWireEvent()); err == nil || errors.Is(err, events.ErrRejected) {
		t.Fatal(err)
	}
}

// R-GVDP-PQQN
func TestEventSocketCancellation(t *testing.T) {
	arrived := make(chan struct{})
	release := make(chan struct{})
	socket := eventWireSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { close(arrived); <-release; w.WriteHeader(204) }))
	eventWireDiscover(t, socket)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- events.NewSocketSink().Deliver(ctx, eventWireEvent()) }()
	<-arrived
	cancel()
	err := <-done
	close(release)
	if !errors.Is(err, context.Canceled) || errors.Is(err, events.ErrRejected) {
		t.Fatal(err)
	}
	t.Setenv(services.Variable, "")
	if err := events.NewSocketSink().Deliver(ctx, eventWireEvent()); !errors.Is(err, context.Canceled) || errors.Is(err, events.ErrRejected) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	if err := events.NewSocketSink().Deliver(ctx, eventWireEvent()); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, events.ErrRejected) {
		t.Fatal(err)
	}
}

// R-GWLM-3IHC R-GXTI-HA81
func TestEventSocketRejectsInvalidEventsBeforeDiscovery(t *testing.T) {
	var calls atomic.Int64
	socket := eventWireSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	path := eventWireDiscover(t, socket)
	invalid := []events.Event{{}}
	for _, change := range []func(*events.Event){
		func(e *events.Event) { e.ID = "bad" }, func(e *events.Event) { e.Name = "invalid" }, func(e *events.Event) { e.Service = "" },
		func(e *events.Event) { e.Time = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
		func(e *events.Event) { e.Cause = "bad"; e.Depth = 1 }, func(e *events.Event) { e.Depth = 1 }, func(e *events.Event) { e.Cause = "evt_ffffffffffffffff" },
		func(e *events.Event) { e.Seq = 1; e.Received = e.Time }, func(e *events.Event) { e.Seq = -1 }, func(e *events.Event) { e.Received = e.Time },
		func(e *events.Event) { e.Attrs = events.Attrs{"x": math.NaN()} }, func(e *events.Event) { e.Attrs = events.Attrs{"x": []int{1}} }, func(e *events.Event) { e.Attrs = events.Attrs{"x": make(chan int)} },
		func(e *events.Event) { e.Attrs = events.Attrs{"x": func() {}} }, func(e *events.Event) { e.Attrs = events.Attrs{"x": (*int)(nil)} }, func(e *events.Event) { a := events.Attrs{}; a["cycle"] = a; e.Attrs = a },
	} {
		e := eventWireEvent()
		change(&e)
		invalid = append(invalid, e)
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	for _, env := range []string{path, "", filepath.Join(eventWireDir(t), "missing")} {
		t.Setenv(services.Variable, env)
		for _, ctx := range []context.Context{context.Background(), done} {
			for _, e := range invalid {
				if err := events.NewSocketSink().Deliver(ctx, e); !errors.Is(err, events.ErrRejected) {
					t.Fatalf("invalid event: %v", err)
				}
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
}

// R-GXTI-HA81
func TestEventSocketConcurrentDeliveries(t *testing.T) {
	var calls atomic.Int64
	socket := eventWireSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	eventWireDiscover(t, socket)
	sink := events.NewSocketSink()
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Go(func() {
			if err := sink.Deliver(context.Background(), eventWireEvent()); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if calls.Load() != 32 {
		t.Fatal(calls.Load())
	}
}

// R-GRQ0-KFIK R-GSXW-Y799
func TestEventSocketOversizeIsSentAndRejected(t *testing.T) {
	e := eventWireEvent()
	e.Attrs = events.Attrs{"text": strings.Repeat("x", events.MaxEventBytes)}
	expected := eventWireJSON(t, e)
	var calls atomic.Int64
	handler := events.EmitHandler(eventWireSink(func(context.Context, events.Event) error { t.Error("oversize reached sink"); return nil }))
	socket := eventWireSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if string(body) != expected {
			t.Error("socket sink changed/truncated body")
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler.ServeHTTP(w, r)
	}))
	eventWireDiscover(t, socket)
	if err := events.NewSocketSink().Deliver(context.Background(), e); !errors.Is(err, events.ErrRejected) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

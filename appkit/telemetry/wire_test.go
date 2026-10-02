package telemetry_test

import (
	"bufio"
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

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type wireSinkFunc func(context.Context, telemetry.Event) error

func (f wireSinkFunc) Deliver(ctx context.Context, e telemetry.Event) error { return f(ctx, e) }

func wireEvent() telemetry.Event {
	return telemetry.Event{Time: time.Date(2024, 2, 29, 12, 34, 56, 123456000, time.UTC), Service: "unit", Name: "thing.finished", RequestID: "req", User: "usr", Attrs: telemetry.Attrs{}}
}
func wireBody(t *testing.T) string {
	t.Helper()
	b, err := wireEvent().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func wireRequest(handler http.Handler, method, mediaType, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, telemetry.IngestPath, strings.NewReader(body))
	if mediaType != "" {
		r.Header.Set("Content-Type", mediaType)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func wireHandlerFactory(factory func(telemetry.Sink) http.Handler) func(telemetry.Sink) http.Handler {
	return factory
}

func wireSocketFactory(factory func() telemetry.Sink) func() telemetry.Sink {
	return factory
}

// R-WOLQ-LNZL R-WR1J-D7GZ R-X20M-T558
func TestWireExportsAndNilSink(t *testing.T) {
	handler := wireHandlerFactory(telemetry.IngestHandler)
	const service = telemetry.ServiceName
	const path = telemetry.IngestPath
	const sizeUnsigned uint64 = telemetry.MaxEventBytes
	const sizeFloat float64 = telemetry.MaxEventBytes
	if service != "telemetry" || path != "/ingest" || sizeUnsigned != 65536 || sizeFloat != 65536 {
		t.Fatal("wire constants")
	}
	defer func() {
		p := recover()
		if p == nil || !strings.Contains(fmt.Sprint(p), "sink") || !strings.Contains(fmt.Sprint(p), "nil") {
			t.Fatalf("panic: %v", p)
		}
	}()
	handler(nil)
}

// R-X38J-6WVX R-X4GF-KOMM R-X5OB-YGDB
func TestIngestValidationOrder(t *testing.T) {
	calls := 0
	h := telemetry.IngestHandler(wireSinkFunc(func(context.Context, telemetry.Event) error { calls++; return nil }))
	for _, method := range []string{"GET", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"} {
		w := wireRequest(h, method, "bad;", strings.Repeat("x", telemetry.MaxEventBytes+1))
		if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"POST"}) {
			t.Fatalf("%s: %d %v", method, w.Code, w.Header())
		}
	}
	for _, media := range []string{"", "text/plain", "application/json; broken", "application/jsonx"} {
		if w := wireRequest(h, "POST", media, strings.Repeat("x", telemetry.MaxEventBytes+1)); w.Code != 415 {
			t.Fatalf("%q: %d", media, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/ingest", strings.NewReader(wireBody(t)))
	r.Header["Content-Type"] = []string{"text/plain", "application/json"}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
	if w := wireRequest(h, "POST", "application/json; charset=utf-8", strings.Repeat("x", telemetry.MaxEventBytes+1)); w.Code != 413 {
		t.Fatal(w.Code)
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	// A valid event padded to the exact boundary is accepted.
	body := wireBody(t)
	body += strings.Repeat(" ", telemetry.MaxEventBytes-len(body))
	if w := wireRequest(h, "POST", "application/json", body); w.Code != 204 || calls != 1 {
		t.Fatalf("boundary: %d %d", w.Code, calls)
	}
}

// R-X6W8-C840 R-X9C1-3RLE
func TestIngestRejectsInvalidBodies(t *testing.T) {
	body := wireBody(t)
	cases := []string{"", "null", "[]", body + body, body + " x", body[:len(body)-1], strings.Replace(body, `"attrs":{}`, `"attrs":null`, 1), strings.Replace(body, `"attrs":{}`, `"attrs":[]`, 1)}
	for _, field := range []string{"time", "service", "event", "request_id", "user", "attrs"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, field)
		b, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, string(b))
		cases = append(cases, strings.TrimSuffix(body, "}")+`,"`+field+`":null}`)
	}
	for _, replacement := range []struct{ old, new string }{
		{`"service":"unit"`, `"service":""`}, {`"service":"unit"`, `"service":1`},
		{`"event":"thing.finished"`, `"event":"Bad.Name"`}, {`"request_id":"req"`, `"request_id":null`}, {`"user":"usr"`, `"user":true`},
		{`"time":"2024-02-29T12:34:56.123456Z"`, `"time":"2024-02-29T12:34:56.12345Z"`},
		{`"time":"2024-02-29T12:34:56.123456Z"`, `"time":"2024-02-29T12:34:56.123456+00:00"`},
		{`"time":"2024-02-29T12:34:56.123456Z"`, `"time":"2024-02-30T12:34:56.123456Z"`},
		{`"attrs":{}`, `"attrs":{"Bad":1}`}, {`"attrs":{}`, `"attrs":{"x":null}`}, {`"attrs":{}`, `"attrs":{"x":[]}`}, {`"attrs":{}`, `"attrs":{"x":{}}`},
		{`"attrs":{}`, `"attrs":{"x":1,"\u0078":2}`}, {`"attrs":{}`, `"attrs":{"x":1e999}`}, {`"attrs":{}`, `"attrs":{"x":01}`}, {`"attrs":{}`, `"attrs":{"x":NaN}`},
	} {
		cases = append(cases, strings.Replace(body, replacement.old, replacement.new, 1))
	}
	cases = append(cases, strings.TrimSuffix(body, "}")+`,"extra":1}`, strings.Replace(body, "unit", string([]byte{0xff}), 1), strings.TrimSuffix(body, "}")+`,"\u0075ser":"other"}`)
	calls := 0
	h := telemetry.IngestHandler(wireSinkFunc(func(context.Context, telemetry.Event) error { calls++; return nil }))
	for i, b := range cases {
		if w := wireRequest(h, "POST", "application/json", b); w.Code != 400 {
			t.Fatalf("case %d: %d body %q", i, w.Code, b)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}

// R-X844-PZUP R-DI1E-RO4X R-DKH7-J7MB
func TestIngestDecodingAndStoreResult(t *testing.T) {
	type contextKey struct{}
	marker := &struct{}{}
	var got telemetry.Event
	calls := 0
	h := telemetry.IngestHandler(wireSinkFunc(func(ctx context.Context, e telemetry.Event) error {
		if ctx.Value(contextKey{}) != marker {
			t.Error("request context lost")
		}
		got = e
		calls++
		return nil
	}))
	body := strings.Replace(wireBody(t), `"attrs":{}`, `"attrs":{"s":"\ud800","yes":true,"no":false,"i":-9223372036854775808,"u":18446744073709551615,"f":1.25,"z":-0,"large":18446744073709551616,"exponent":1e2}`, 1)
	r := httptest.NewRequest("POST", "/ingest", strings.NewReader(body)).WithContext(context.WithValue(context.Background(), contextKey{}, marker))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || w.Body.Len() != 0 || calls != 1 {
		t.Fatalf("%d %q %d", w.Code, w.Body.String(), calls)
	}
	expected := wireEvent()
	expected.Attrs = telemetry.Attrs{"s": "\ufffd", "yes": true, "no": false, "i": int64(math.MinInt64), "u": uint64(math.MaxUint64), "f": 1.25, "z": math.Copysign(0, -1), "large": float64(18446744073709551616.0), "exponent": float64(100)}
	if !reflect.DeepEqual(got, expected) || got.Time.Location() != time.UTC || !math.Signbit(got.Attrs["z"].(float64)) {
		t.Fatalf("got %#v", got)
	}
	empty := telemetry.IngestHandler(wireSinkFunc(func(_ context.Context, e telemetry.Event) error {
		if e.Attrs == nil {
			t.Error("nil attrs")
		}
		return errors.New("store failed")
	}))
	if w := wireRequest(empty, "POST", "application/json", wireBody(t)); w.Code != 500 {
		t.Fatal(w.Code)
	}
}

// R-X2T0-S7CX
func TestIngestMarshalRoundTrip(t *testing.T) {
	type namedInt int
	type namedFloat float64
	type namedString string
	events := []telemetry.Event{wireEvent(), {Time: time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), Service: "日本語<&", Name: "api_key.minted", RequestID: "req\n\"\\", User: "é😀\ufffd", Attrs: telemetry.Attrs{"i": namedInt(-12), "min": int64(math.MinInt64), "u": uint64(math.MaxUint64), "f": namedFloat(1e-8), "large": math.MaxFloat64, "small": math.SmallestNonzeroFloat64, "z": math.Copysign(0, -1), "s": namedString("\ufffd\n<&\u2028\u2029"), "b": true, "no": false}}, {Time: time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC), Service: "last", Name: "x.y"}}
	for _, e := range events {
		body, err := e.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		h := telemetry.IngestHandler(wireSinkFunc(func(_ context.Context, received telemetry.Event) error {
			calls++
			b, err := received.MarshalJSON()
			if err != nil || !bytes.Equal(b, body) {
				t.Errorf("roundtrip %q != %q: %v", b, body, err)
			}
			return nil
		}))
		wireRequest(h, "POST", "application/json", string(body))
		if calls == 0 {
			t.Fatal("roundtrip did not deliver the event")
		}
	}
}

// R-XE7M-MUK6
func TestIngestConcurrent(t *testing.T) {
	var count atomic.Int64
	h := telemetry.IngestHandler(wireSinkFunc(func(context.Context, telemetry.Event) error { count.Add(1); return nil }))
	body := wireBody(t)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if w := wireRequest(h, "POST", "application/json", body); w.Code != 204 {
				t.Error(w.Code)
			}
		})
	}
	wg.Wait()
	if count.Load() != 32 {
		t.Fatal(count.Load())
	}
}

func wireSocketServer(t *testing.T, h http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wire-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	})
	return path
}
func wireServicesFile(t *testing.T, path, socket string, enabled bool) {
	t.Helper()
	data := fmt.Sprintf(`{"services":[{"name":"telemetry","url":"","description":"","socket":%q,"enabled":%t,"mcp":false}]}`, socket, enabled)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

// R-WPTM-ZFQA R-WS9F-QZ7O R-WTHC-4QYD
func TestSocketSinkRequestAndRediscovery(t *testing.T) {
	e := wireEvent()
	body, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var firstCount, secondCount atomic.Int64
	handler := func(count *atomic.Int64) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			if r.Proto != "HTTP/1.1" || r.Method != "POST" || r.URL.Path != "/ingest" || r.URL.RawQuery != "" || !reflect.DeepEqual(r.Header.Values("Content-Type"), []string{"application/json"}) || !bytes.Equal(b, body) {
				t.Errorf("request: %#v body %q", r, b)
			}
			w.WriteHeader(204)
		})
	}
	first := wireSocketServer(t, handler(&firstCount))
	second := wireSocketServer(t, handler(&secondCount))
	file := filepath.Join(t.TempDir(), "services.json")
	t.Setenv(services.Variable, file)
	factory := wireSocketFactory(telemetry.NewSocketSink)
	sink := factory()
	wireServicesFile(t, file, first, false)
	if err := sink.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if firstCount.Load() != 1 || secondCount.Load() != 0 {
		t.Fatalf("initial destination: first %d second %d", firstCount.Load(), secondCount.Load())
	}
	wireServicesFile(t, file, second, true)
	if err := sink.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if firstCount.Load() != 1 || secondCount.Load() != 1 {
		t.Fatalf("rewritten file destination: first %d second %d", firstCount.Load(), secondCount.Load())
	}
	other := filepath.Join(t.TempDir(), "services.json")
	wireServicesFile(t, other, first, false)
	t.Setenv(services.Variable, other)
	if err := sink.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if firstCount.Load() != 2 || secondCount.Load() != 1 {
		t.Fatalf("changed environment destination: first %d second %d", firstCount.Load(), secondCount.Load())
	}
}

// R-WUP8-IIP2
func TestSocketSinkStatusesAndNoRedirect(t *testing.T) {
	var status atomic.Int64
	var calls atomic.Int64
	socket := wireSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "http://telemetry/other")
		w.WriteHeader(int(status.Load()))
	}))
	file := filepath.Join(t.TempDir(), "services.json")
	wireServicesFile(t, file, socket, true)
	t.Setenv(services.Variable, file)
	sink := telemetry.NewSocketSink()
	for _, code := range []int{204, 200, 201, 301, 302, 307, 308, 400, 401, 404, 413, 499, 500, 503} {
		status.Store(int64(code))
		before := calls.Load()
		err := sink.Deliver(context.Background(), wireEvent())
		if (err == nil) != (code == 204) || errors.Is(err, telemetry.ErrRejected) != (code >= 400 && code <= 499) || calls.Load() != before+1 {
			t.Fatalf("status %d: %v calls %d", code, err, calls.Load()-before)
		}
	}
}

// R-WGPU-WEIP
func TestSocketSinkFailuresWithoutRequests(t *testing.T) {
	var calls atomic.Int64
	socket := wireSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	file := filepath.Join(t.TempDir(), "services.json")
	sink := telemetry.NewSocketSink()
	for _, state := range []string{"unset", "empty", "missing", "directory", "invalid", "absent", "connection"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv(services.Variable, file)
			switch state {
			case "unset":
				if err := os.Unsetenv(services.Variable); err != nil {
					t.Fatal(err)
				}
			case "empty":
				t.Setenv(services.Variable, "")
			case "missing":
				t.Setenv(services.Variable, file+"missing")
			case "directory":
				t.Setenv(services.Variable, t.TempDir())
			case "invalid":
				if err := os.WriteFile(file, []byte("!"), 0600); err != nil {
					t.Fatal(err)
				}
			case "absent":
				if err := os.WriteFile(file, []byte(`{"services":[]}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "connection":
				wireServicesFile(t, file, socket+"missing", true)
			}
			if err := sink.Deliver(context.Background(), wireEvent()); err == nil || errors.Is(err, telemetry.ErrRejected) {
				t.Fatalf("%v", err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
}

// R-WJ5N-NY03
func TestSocketSinkMarshalFailurePrecedesDiscoveryAndContext(t *testing.T) {
	var calls atomic.Int64
	socket := wireSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	file := filepath.Join(t.TempDir(), "services.json")
	sink := telemetry.NewSocketSink()
	malformed := []telemetry.Event{{}, wireEvent(), wireEvent(), wireEvent()}
	malformed[1].Name = "bad"
	malformed[2].Attrs = telemetry.Attrs{"x": make(chan int)}
	malformed[3].Time = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, state := range []string{"unset", "empty", "missing", "invalid", "absent", "connection", "valid"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv(services.Variable, file)
			switch state {
			case "unset":
				if err := os.Unsetenv(services.Variable); err != nil {
					t.Fatal(err)
				}
			case "empty":
				t.Setenv(services.Variable, "")
			case "missing":
				t.Setenv(services.Variable, file+"missing")
			case "invalid", "absent":
				data := "!"
				if state == "absent" {
					data = `{"services":[]}`
				}
				if err := os.WriteFile(file, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			case "connection":
				wireServicesFile(t, file, socket+"missing", true)
			case "valid":
				wireServicesFile(t, file, socket, true)
			}
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			expired, cancelDeadline := context.WithDeadline(context.Background(), time.Time{})
			defer cancelDeadline()
			for _, ctx := range []context.Context{context.Background(), canceled, expired} {
				for _, e := range malformed {
					if _, err := e.MarshalJSON(); err == nil {
						t.Fatal("fixture unexpectedly marshals")
					}
					if err := sink.Deliver(ctx, e); err == nil || !errors.Is(err, telemetry.ErrRejected) {
						t.Fatalf("context %v: %v", ctx.Err(), err)
					}
				}
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
}

// R-WGPU-WEIP
func TestSocketSinkUnreadableAnswer(t *testing.T) {
	dir, err := os.MkdirTemp("", "wire-")
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
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = connection.Close() }()
		request, err := http.ReadRequest(bufio.NewReader(connection))
		if err == nil {
			_, err = io.Copy(io.Discard, request.Body)
			_ = request.Body.Close()
		}
		done <- err
	}()
	file := filepath.Join(t.TempDir(), "services.json")
	wireServicesFile(t, file, socket, true)
	t.Setenv(services.Variable, file)
	if err := telemetry.NewSocketSink().Deliver(context.Background(), wireEvent()); err == nil || errors.Is(err, telemetry.ErrRejected) {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// R-WHXR-A69E
func TestSocketSinkAlreadyDoneContext(t *testing.T) {
	socket := wireSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	file := filepath.Join(t.TempDir(), "services.json")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelDeadline := context.WithDeadline(context.Background(), time.Time{})
	defer cancelDeadline()
	for _, state := range []string{"unset", "empty", "missing", "directory", "invalid", "absent", "connection", "valid"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv(services.Variable, file)
			switch state {
			case "unset":
				if err := os.Unsetenv(services.Variable); err != nil {
					t.Fatal(err)
				}
			case "empty":
				t.Setenv(services.Variable, "")
			case "missing":
				t.Setenv(services.Variable, file+"missing")
			case "directory":
				t.Setenv(services.Variable, t.TempDir())
			case "invalid", "absent":
				data := "!"
				if state == "absent" {
					data = `{"services":[]}`
				}
				if err := os.WriteFile(file, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			case "connection":
				wireServicesFile(t, file, socket+"missing", true)
			case "valid":
				wireServicesFile(t, file, socket, true)
			}
			for _, ctx := range []context.Context{canceled, expired} {
				err := telemetry.NewSocketSink().Deliver(ctx, wireEvent())
				if err == nil || !errors.Is(err, ctx.Err()) || errors.Is(err, telemetry.ErrRejected) {
					t.Errorf("context %v: %v", ctx.Err(), err)
				}
			}
		})
	}
}

type wireCancelAfterObservationContext struct {
	context.Context
	cancel context.CancelFunc
}

func (ctx wireCancelAfterObservationContext) Err() error {
	err := ctx.Context.Err()
	ctx.cancel()
	return err
}

// R-WHXR-A69E
func TestSocketSinkCancellationAfterInitialObservation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "services.json")
	for _, state := range []string{"empty", "missing", "invalid", "absent"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv(services.Variable, file)
			switch state {
			case "empty":
				t.Setenv(services.Variable, "")
			case "missing":
				t.Setenv(services.Variable, file+"missing")
			case "invalid", "absent":
				data := "!"
				if state == "absent" {
					data = `{"services":[]}`
				}
				if err := os.WriteFile(file, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := wireCancelAfterObservationContext{Context: base, cancel: cancel}
			err := telemetry.NewSocketSink().Deliver(ctx, wireEvent())
			if err == nil || !errors.Is(err, base.Err()) || errors.Is(err, telemetry.ErrRejected) {
				t.Fatalf("context %v: %v", base.Err(), err)
			}
		})
	}
}

// R-WHXR-A69E
func TestSocketSinkCancellation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	socket := wireSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { close(entered); <-release; w.WriteHeader(204) }))
	file := filepath.Join(t.TempDir(), "services.json")
	wireServicesFile(t, file, socket, true)
	t.Setenv(services.Variable, file)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- telemetry.NewSocketSink().Deliver(ctx, wireEvent()) }()
	<-entered
	cancel()
	err := <-done
	close(release)
	if !errors.Is(err, context.Canceled) || errors.Is(err, telemetry.ErrRejected) {
		t.Fatal(err)
	}
}

// R-WZKU-1LNU
func TestSocketSinkConcurrent(t *testing.T) {
	var calls atomic.Int64
	socket := wireSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	file := filepath.Join(t.TempDir(), "services.json")
	wireServicesFile(t, file, socket, true)
	t.Setenv(services.Variable, file)
	sink := telemetry.NewSocketSink()
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if err := sink.Deliver(context.Background(), wireEvent()); err != nil {
				t.Error(err)
			}
			if err := sink.Deliver(context.Background(), telemetry.Event{}); !errors.Is(err, telemetry.ErrRejected) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 32 {
		t.Fatal(calls.Load())
	}
}

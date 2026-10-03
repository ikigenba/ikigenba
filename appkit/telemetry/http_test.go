package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
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

	"github.com/ikigenba/ikigenba/appkit/identity"
)

func httpWriter(t *testing.T, now func() time.Time, random io.Reader) (*Writer, *Capture) {
	t.Helper()
	capture := &Capture{}
	if now == nil {
		now = func() time.Time { return time.Unix(100, 0) }
	}
	if random == nil {
		random = bytes.NewReader(bytes.Repeat([]byte{0x5a}, 16))
	}
	var pauseMu sync.Mutex
	var pauses []time.Duration
	w := New(Config{Service: "http-test", Sink: capture, Now: now, Rand: random, Sleep: func(_ context.Context, d time.Duration) {
		pauseMu.Lock()
		defer pauseMu.Unlock()
		pauses = append(pauses, d)
	}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test complete") })
	return w, capture
}
func httpEvents(t *testing.T, w *Writer, c *Capture) []Event {
	t.Helper()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c.Events()
}

// R-34A4-ETW4 R-DU8E-LDJV R-DWO7-CX19 R-PSID-DEJO R-E0BW-I89C
// R-E2RP-9RQQ R-PTQ9-R6AD R-JJPG-PM2B R-3LCP-RM9U
// R-VD1N-OM2I R-PRAG-ZMSZ
func TestMiddlewareContextAndTrail(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprint(present), func(t *testing.T) {
			now := time.Unix(100, 0)
			clockReads := 0
			random := &httpCountingReader{Reader: bytes.NewReader(bytes.Repeat([]byte{0xab}, 16))}
			w, c := httpWriter(t, func() time.Time { clockReads++; return now }, random)
			ctx, cancel := context.WithCancel(context.WithValue(identity.NewContext(context.Background(), identity.Caller{UserID: "prior user", Email: "prior email", RequestID: "prior id"}), httpContextKey{}, "value"))
			defer cancel()
			deadline := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
			ctx, stop := context.WithDeadline(ctx, deadline)
			defer stop()
			request := httptest.NewRequest("PATCH", "http://service/a%20b?secret=data", strings.NewReader("payload")).WithContext(ctx)
			request.Header["X-User-Id"] = []string{"user", "ignored"}
			request.Header["X-User-Email"] = []string{"mail", "ignored"}
			request.Header.Set("Other", "keep")
			id := strings.Repeat("ab", 16)
			if present {
				id = "UNCHANGED id"
				request.Header["X-Request-Id"] = []string{id, "second"}
			} else {
				request.Header["X-Request-Id"] = []string{"", "second"}
			}
			calls := 0
			var handler http.Handler = http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
				calls++
				if clockReads != 2 {
					t.Fatalf("reads before handler=%d", clockReads)
				}
				if events := httpEvents(t, w, c); len(events) != 1 || events[0].Name != "request.started" {
					t.Fatal("start not emitted before handler")
				}
				caller, ok := identity.FromContext(r.Context())
				if !ok || caller != (identity.Caller{UserID: "user", Email: "mail", RequestID: id}) {
					t.Fatalf("caller=%+v, %v", caller, ok)
				}
				gotDeadline, ok := r.Context().Deadline()
				if !ok || gotDeadline != deadline || r.Context().Value(httpContextKey{}) != "value" {
					t.Fatal("context not preserved")
				}
				cancel()
				if r.Context().Err() != context.Canceled {
					t.Fatal("cancellation missing")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != "payload" || r.Method != request.Method || !reflect.DeepEqual(r.URL, request.URL) || r.Header.Get("Other") != "keep" {
					t.Fatal("request changed")
				}
				wantValues := []string{id}
				if present {
					wantValues = append(wantValues, "second")
				}
				if !reflect.DeepEqual(r.Header.Values("X-Request-Id"), wantValues) {
					t.Fatal(r.Header)
				}
				w.Emit(r.Context(), "domain.changed", Attrs{"value": "metadata"})
				now = now.Add(1234*time.Microsecond + 999*time.Nanosecond)
				out.Header().Set("Custom", "value")
				out.WriteHeader(202)
				_, _ = io.WriteString(out, "answer")
			})
			out := httptest.NewRecorder()
			Middleware(w, handler).ServeHTTP(out, request)
			if clockReads != 5 {
				t.Fatalf("clock reads=%d", clockReads)
			}
			if calls != 1 || out.Code != 202 || out.Body.String() != "answer" || out.Header().Get("Custom") != "value" {
				t.Fatal("response or calls changed")
			}
			wantReads := 1
			if present {
				wantReads = 0
			}
			if random.reads != wantReads || random.bytes != 16*wantReads {
				t.Fatalf("random reads=%d bytes=%d", random.reads, random.bytes)
			}
			events := httpEvents(t, w, c)
			if len(events) != 3 || events[0].Name != "request.started" || events[1].Name != "domain.changed" || events[2].Name != "request.finished" {
				t.Fatalf("events=%+v", events)
			}
			for _, e := range events {
				if e.RequestID != id || e.User != "user" {
					t.Fatal(e)
				}
			}
			if !reflect.DeepEqual(events[0].Attrs, Attrs{"method": "PATCH", "path": "/a b"}) || !reflect.DeepEqual(events[2].Attrs, Attrs{"status": int64(202), "duration_us": int64(1234), "request_bytes": int64(7), "response_bytes": int64(6)}) {
				t.Fatal(events)
			}
		})
	}
}

type httpContextKey struct{}
type httpCountingReader struct {
	io.Reader
	reads, bytes int
	limit        int
}

func (r *httpCountingReader) Read(b []byte) (int, error) {
	r.reads++
	if r.limit > 0 && len(b) > r.limit {
		b = b[:r.limit]
	}
	n, err := r.Reader.Read(b)
	r.bytes += n
	return n, err
}

// R-DWO7-CX19
func TestMiddlewareReadsFullRequestID(t *testing.T) {
	random := &httpCountingReader{Reader: bytes.NewReader(bytes.Repeat([]byte{0xcd}, 16)), limit: 3}
	w, _ := httpWriter(t, nil, random)
	Middleware(w, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-Id") != strings.Repeat("cd", 16) {
			t.Fatal(r.Header)
		}
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if random.bytes != 16 {
		t.Fatalf("random bytes=%d", random.bytes)
	}
}

// R-37XT-K547 R-3P0E-WXHX
func TestHTTPNilWriter(t *testing.T) {
	for name, call := range map[string]func(){"middleware": func() { Middleware(nil, http.NotFoundHandler()) }, "client": func() { SiblingClient(nil, "sibling", nil) }} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				value := recover()
				if !strings.Contains(fmt.Sprint(value), "telemetry writer is nil") {
					t.Fatalf("panic=%v", value)
				}
			}()
			call()
			t.Fatal("no panic")
		})
	}
}

// R-DRSL-TU2H R-E0BW-I89C
func TestMiddlewareRandomFailure(t *testing.T) {
	random := &httpCountingReader{Reader: bytes.NewReader([]byte{0xde, 0xad})}
	w, c := httpWriter(t, nil, random)
	ids := map[string]bool{}
	handler := Middleware(w, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" || ids[id] {
			t.Fatalf("bad fallback id %q", id)
		}
		ids[id] = true
		caller, ok := identity.FromContext(r.Context())
		if !ok || caller.UserID != "" || caller.Email != "" || caller.RequestID != id {
			t.Fatal(caller)
		}
	}))
	for range 2 {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}
	if random.bytes != 2 {
		t.Fatal("unexpected random bytes")
	}
	if len(httpEvents(t, w, c)) != 4 {
		t.Fatal("request not served")
	}
}

// R-3HP0-MB1R R-PUY6-4Y12 R-PTQ9-R6AD R-JJPG-PM2B R-PRAG-ZMSZ
func TestMiddlewareStatusesAndPanic(t *testing.T) {
	panicValue := &struct{ message string }{"original"}
	for _, tc := range []struct {
		name   string
		write  func(http.ResponseWriter)
		panics bool
		status int
	}{
		{"empty", func(http.ResponseWriter) {}, false, 200},
		{"body", func(w http.ResponseWriter) { _, _ = w.Write([]byte("ok")); w.WriteHeader(503) }, false, 200},
		{"headers", func(w http.ResponseWriter) { w.WriteHeader(201); w.WriteHeader(503) }, false, 201},
		{"informational", func(w http.ResponseWriter) { w.WriteHeader(103); w.WriteHeader(204) }, false, 204},
		{"informational only", func(w http.ResponseWriter) { w.WriteHeader(103) }, false, 200},
		{"panic empty", func(http.ResponseWriter) {}, true, 500},
		{"panic answered", func(w http.ResponseWriter) { w.WriteHeader(418) }, true, 418},
		{"panic body", func(w http.ResponseWriter) { _, _ = w.Write([]byte("ok")) }, true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			w, c := httpWriter(t, func() time.Time { return now }, nil)
			request := httptest.NewRequest("GET", "/", nil)
			request.Header.Set("X-Request-Id", "known")
			var got any
			func() {
				defer func() { got = recover() }()
				Middleware(w, http.HandlerFunc(func(out http.ResponseWriter, _ *http.Request) {
					tc.write(out)
					now = now.Add(-time.Second)
					if tc.panics {
						panic(panicValue)
					}
				})).ServeHTTP(httptest.NewRecorder(), request)
			}()
			if tc.panics && got != panicValue || !tc.panics && got != nil {
				t.Fatalf("panic changed: %v", got)
			}
			events := httpEvents(t, w, c)
			wantBytes := int64(0)
			if tc.name == "body" || tc.name == "panic body" {
				wantBytes = 2
			}
			if len(events) != 2 || !reflect.DeepEqual(events[1].Attrs, Attrs{"status": int64(tc.status), "duration_us": int64(0), "request_bytes": int64(0), "response_bytes": wantBytes}) {
				t.Fatal(events)
			}
		})
	}
}

type httpBodyRead struct {
	data string
	err  error
}

type httpScriptBody struct {
	reads    []httpBodyRead
	next     int
	closed   int
	closeErr error
}

func (b *httpScriptBody) Read(p []byte) (int, error) {
	result := b.reads[b.next]
	b.next++
	return copy(p, result.data), result.err
}

func (b *httpScriptBody) Close() error {
	b.closed++
	return b.closeErr
}

// R-PSID-DEJO R-PW62-IPRR
func TestMiddlewareRequestBodyReadsAndClose(t *testing.T) {
	readErr := errors.New("body read failed")
	closeErr := errors.New("body close failed")
	closeErrors := map[error]bool{closeErr: true}
	for _, tc := range []struct {
		name  string
		nil   bool
		reads []httpBodyRead
	}{
		{name: "nil", nil: true},
		{name: "unread"},
		{name: "partial", reads: []httpBodyRead{{data: "ab"}}},
		{name: "errors", reads: []httpBodyRead{{data: "ab"}, {data: "c", err: readErr}, {err: io.EOF}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, c := httpWriter(t, nil, nil)
			body := &httpScriptBody{reads: tc.reads, closeErr: closeErr}
			r := httptest.NewRequest("POST", "/", nil)
			r.ContentLength = 999999
			r.Header.Set("X-Request-Id", "known")
			if tc.nil {
				r.Body = nil
			} else {
				r.Body = body
			}
			var wantBytes int64
			calls := 0
			Middleware(w, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				calls++
				if (request.Body == nil) != tc.nil {
					t.Fatal("nil body changed")
				}
				for _, want := range tc.reads {
					// Reads by another caller still belong to this request.
					got := make(chan httpBodyRead, 1)
					go func() {
						var p [16]byte
						n, err := request.Body.Read(p[:])
						got <- httpBodyRead{data: string(p[:n]), err: err}
					}()
					if result := <-got; result != want {
						t.Fatalf("read=%+v, want %+v", result, want)
					}
					wantBytes += int64(len(want.data))
				}
				if !tc.nil && !closeErrors[request.Body.Close()] {
					t.Fatal("close error changed")
				}
			})).ServeHTTP(httptest.NewRecorder(), r)
			if calls != 1 || body.next != len(tc.reads) || !tc.nil && body.closed != 1 {
				t.Fatalf("calls=%d body=%+v", calls, body)
			}
			events := httpEvents(t, w, c)
			if len(events) != 2 || events[1].Attrs["request_bytes"] != wantBytes {
				t.Fatal(events)
			}
		})
	}
}

type httpByteResponse struct {
	header  http.Header
	body    bytes.Buffer
	writes  int
	flushes int
	err     error
}

func (r *httpByteResponse) Header() http.Header { return r.header }

func (*httpByteResponse) WriteHeader(int) {}

func (r *httpByteResponse) Write(p []byte) (int, error) {
	r.writes++
	if r.writes == 1 {
		_, _ = r.body.Write(p[:2])
		return 2, r.err
	}
	return r.body.Write(p)
}

func (r *httpByteResponse) ReadFrom(src io.Reader) (int64, error) {
	n, err := io.CopyN(&r.body, src, 3)
	if err != nil {
		return n, err
	}
	return n, r.err
}

func (r *httpByteResponse) Flush() { r.flushes++ }

// R-PXDY-WHIG R-PYLV-A995
func TestMiddlewareResponseByteCounts(t *testing.T) {
	writeErr := errors.New("response failed")
	w, c := httpWriter(t, nil, nil)
	out := &httpByteResponse{header: make(http.Header), err: writeErr}
	var wantBytes int64
	Middleware(w, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Large-Header", strings.Repeat("h", 1024))
		rw.WriteHeader(http.StatusCreated)
		// The underlying writer accepts only two bytes and returns an error
		// on its first Write. Count what the handler's calls return.
		n, _ := rw.Write([]byte("abcd"))
		wantBytes += int64(n)
		n, _ = rw.Write([]byte("xyz"))
		wantBytes += int64(n)
		// Copy may use Write or an optional ReadFrom; either count applies.
		copied, _ := io.Copy(rw, struct{ io.Reader }{strings.NewReader("stream")})
		wantBytes += copied
		flusher, ok := rw.(http.Flusher)
		if !ok {
			t.Fatal("direct flusher missing")
		}
		flusher.Flush()
		if out.flushes != 1 {
			t.Fatal("flush did not reach underlying writer")
		}
		if unwrapper, ok := rw.(interface{ Unwrap() http.ResponseWriter }); ok {
			_, _ = unwrapper.Unwrap().Write([]byte("bypass"))
		}
	})).ServeHTTP(out, httptest.NewRequest("GET", "/", nil))
	events := httpEvents(t, w, c)
	if len(events) != 2 || events[1].Attrs["response_bytes"] != wantBytes {
		t.Fatal(events, out.body.String())
	}
}

// R-PXDY-WHIG
func TestMiddlewareReadFromWithoutFlusher(t *testing.T) {
	w, c := httpWriter(t, nil, nil)
	out := &httpByteResponse{header: make(http.Header), err: errors.New("copy failed")}
	var wantBytes int64
	// Expose ReaderFrom while omitting the optional Flush method.
	writer := struct {
		http.ResponseWriter
		io.ReaderFrom
	}{out, out}
	Middleware(w, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		if reader, ok := rw.(io.ReaderFrom); ok {
			wantBytes, _ = reader.ReadFrom(strings.NewReader("stream"))
		} else {
			wantBytes, _ = io.Copy(rw, struct{ io.Reader }{strings.NewReader("stream")})
		}
	})).ServeHTTP(writer, httptest.NewRequest("GET", "/", nil))
	events := httpEvents(t, w, c)
	if len(events) != 2 || events[1].Attrs["response_bytes"] != wantBytes {
		t.Fatal(events)
	}
}

// R-PXDY-WHIG R-PYLV-A995
func TestMiddlewareDirectFlushAndCopyWrites(t *testing.T) {
	w, c := httpWriter(t, nil, nil)
	out := httptest.NewRecorder()
	var wantBytes int64
	Middleware(w, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.(http.Flusher).Flush()
		if !out.Flushed {
			t.Fatal("direct flush did not reach recorder")
		}
		wantBytes, _ = io.Copy(rw, struct{ io.Reader }{strings.NewReader("copied")})
	})).ServeHTTP(out, httptest.NewRequest("GET", "/", nil))
	events := httpEvents(t, w, c)
	if len(events) != 2 || events[1].Attrs["response_bytes"] != wantBytes {
		t.Fatal(events)
	}
}

type httpHijackResponse struct {
	httpRecordingResponse
	conn net.Conn
}

func (r *httpHijackResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return r.conn, bufio.NewReadWriter(bufio.NewReader(r.conn), bufio.NewWriter(r.conn)), nil
}

// R-PW62-IPRR R-PXDY-WHIG
func TestMiddlewareHijackedBytesBypassCounts(t *testing.T) {
	w, c := httpWriter(t, nil, nil)
	conn, peer := net.Pipe()
	peerDone := make(chan struct{})
	result := make(chan httpBodyRead, 1)
	t.Cleanup(func() {
		_ = conn.Close()
		_ = peer.Close()
		<-peerDone
	})
	go func() {
		defer close(peerDone)
		if _, err := peer.Write([]byte("socket-in")); err != nil {
			result <- httpBodyRead{err: err}
			return
		}
		var p [10]byte
		n, err := io.ReadFull(peer, p[:])
		result <- httpBodyRead{data: string(p[:n]), err: err}
	}()
	out := &httpHijackResponse{httpRecordingResponse: httpRecordingResponse{header: make(http.Header)}, conn: conn}
	Middleware(w, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var body [2]byte
		if n, err := r.Body.Read(body[:]); n != 2 || err != nil {
			t.Fatalf("body read=%d, %v", n, err)
		}
		_, _ = rw.Write([]byte("before"))
		hijacked, _, err := http.NewResponseController(rw).Hijack()
		if err != nil {
			t.Fatal(err)
		}
		var p [9]byte
		if _, err := io.ReadFull(hijacked, p[:]); err != nil || string(p[:]) != "socket-in" {
			t.Fatalf("connection read=%q, %v", p, err)
		}
		if n, err := hijacked.Write([]byte("socket-out")); n != 10 || err != nil {
			t.Fatalf("connection write=%d, %v", n, err)
		}
		if got := <-result; got.data != "socket-out" || got.err != nil {
			t.Fatal(got)
		}
	})).ServeHTTP(out, httptest.NewRequest("POST", "/", strings.NewReader("request")))
	events := httpEvents(t, w, c)
	if len(events) != 2 || events[1].Attrs["request_bytes"] != int64(2) || events[1].Attrs["response_bytes"] != int64(6) {
		t.Fatal(events)
	}
}

// R-PTQ9-R6AD R-PUY6-4Y12 R-PRAG-ZMSZ R-PW62-IPRR R-PXDY-WHIG
func TestMiddlewarePanicTrailAndByteCounts(t *testing.T) {
	now := time.Unix(100, 0)
	w, c := httpWriter(t, func() time.Time { return now }, nil)
	r := httptest.NewRequest("POST", "/", strings.NewReader("request"))
	r.Header.Set("X-Request-Id", "request-id")
	r.Header.Set("X-User-Id", "user-id")
	panicValue := &struct{}{}
	var got any
	func() {
		defer func() { got = recover() }()
		Middleware(w, http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
			var body [3]byte
			if n, err := request.Body.Read(body[:]); n != 3 || err != nil {
				t.Fatalf("read=%d, %v", n, err)
			}
			_, _ = rw.Write([]byte("part"))
			w.Emit(request.Context(), "domain.changed", nil)
			now = now.Add(25 * time.Microsecond)
			panic(panicValue)
		})).ServeHTTP(httptest.NewRecorder(), r)
	}()
	if got != panicValue {
		t.Fatalf("panic changed: %v", got)
	}
	events := httpEvents(t, w, c)
	if len(events) != 3 || events[0].Name != "request.started" || events[1].Name != "domain.changed" || events[2].Name != "request.finished" {
		t.Fatal(events)
	}
	for _, e := range events {
		if e.RequestID != "request-id" || e.User != "user-id" {
			t.Fatal(e)
		}
	}
	if !reflect.DeepEqual(events[2].Attrs, Attrs{"status": int64(200), "duration_us": int64(25), "request_bytes": int64(3), "response_bytes": int64(4)}) {
		t.Fatal(events[2])
	}
}

// R-3MKM-5E0J
func TestMiddlewareResponseController(t *testing.T) {
	w, _ := httpWriter(t, nil, nil)
	out := httptest.NewRecorder()
	Middleware(w, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(rw).Flush(); err != nil {
			t.Fatal(err)
		}
	})).ServeHTTP(out, httptest.NewRequest("GET", "/", nil))
	if !out.Flushed {
		t.Fatal("underlying writer not flushed")
	}
}

type httpRecordingResponse struct {
	header   http.Header
	statuses []int
	body     bytes.Buffer
}

func (r *httpRecordingResponse) Header() http.Header { return r.header }

func (r *httpRecordingResponse) WriteHeader(status int) {
	r.statuses = append(r.statuses, status)
}

func (r *httpRecordingResponse) Write(body []byte) (int, error) {
	return r.body.Write(body)
}

// R-3LCP-RM9U
func TestMiddlewareResponsePassThrough(t *testing.T) {
	w, _ := httpWriter(t, nil, nil)
	out := &httpRecordingResponse{header: make(http.Header)}
	Middleware(w, http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Add("Custom", "first")
		rw.Header().Add("Custom", "second")
		rw.WriteHeader(103)
		rw.WriteHeader(201)
		rw.WriteHeader(503)
		_, _ = rw.Write([]byte("body"))
	})).ServeHTTP(out, httptest.NewRequest("GET", "/", nil))
	if !reflect.DeepEqual(out.header, http.Header{"Custom": {"first", "second"}}) || !reflect.DeepEqual(out.statuses, []int{103, 201, 503}) || out.body.String() != "body" {
		t.Fatalf("response=%+v", out)
	}
}

type httpExchangeError struct{}

func (*httpExchangeError) Error() string { return "dial failed" }

type httpRoundTripFunc func(*http.Request) (*http.Response, error)

func (f httpRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// R-35I0-SLMT R-3Q8B-AP8M R-3RG7-OGZB R-3SO4-28Q0 R-3TW0-G0GP
// R-3V3W-TS7E R-3WBT-7JY3 R-3YRL-Z3FH R-JM59-H5JP R-VFHG-G5JW
func TestSiblingTransport(t *testing.T) {
	sentinel := &httpExchangeError{}
	for _, caller := range []*identity.Caller{nil, {UserID: "user", RequestID: "id"}, {UserID: "user", Email: "mail", RequestID: "id"}} {
		withCaller := caller != nil
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprint(caller, failed), func(t *testing.T) {
				now := time.Unix(100, 0)
				clockReads := 0
				w, c := httpWriter(t, func() time.Time { clockReads++; return now }, nil)
				ctx := context.WithValue(context.Background(), httpContextKey{}, "keep")
				if withCaller {
					ctx = identity.NewContext(ctx, *caller)
				}
				request := httptest.NewRequest("POST", "http://sibling/p%20ath?secret=value", strings.NewReader("body")).WithContext(ctx)
				request.Header = http.Header{"X-User-Id": {"old", "second"}, "X-User-Email": {"old"}, "X-Request-Id": {"old"}, "Other": {"keep"}}
				headers := request.Header.Clone()
				response := &http.Response{StatusCode: 207, Body: io.NopCloser(strings.NewReader("response"))}
				var wantErr error
				if failed {
					wantErr = sentinel
				}
				calls := 0
				client := SiblingClient(w, "target", httpRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if clockReads != 1 || len(httpEvents(t, w, c)) != 0 {
						t.Fatal("exchange clock or event ordering")
					}
					expected := headers.Clone()
					if withCaller {
						expected.Set("X-User-Id", "user")
						if caller.Email == "" {
							expected.Del("X-User-Email")
						} else {
							expected.Set("X-User-Email", caller.Email)
						}
						expected.Set("X-Request-Id", "id")
					}
					if !reflect.DeepEqual(r.Header, expected) || r.Context() != ctx || r.Body != request.Body || !reflect.DeepEqual(r.URL, request.URL) || r.Method != "POST" {
						t.Fatal("forwarding changed request", r)
					}
					now = now.Add(789 * time.Microsecond)
					return response, wantErr
				}))
				if client.Timeout != 0 || client.CheckRedirect != nil || client.Jar != nil {
					t.Fatal("client defaults changed")
				}
				got, err := client.Transport.RoundTrip(request)
				if clockReads != 3 {
					t.Fatalf("clock reads=%d", clockReads)
				}
				if got != response || !errors.Is(err, wantErr) || calls != 1 || !reflect.DeepEqual(request.Header, headers) {
					t.Fatal("base result or original modified")
				}
				if failed {
					sameError := map[error]bool{sentinel: true}
					if !sameError[err] {
						t.Fatal("transport error replaced or wrapped")
					}
				}
				events := httpEvents(t, w, c)
				status := int64(207)
				if failed {
					status = 0
				}
				if len(events) != 1 || events[0].Name != "sibling.called" || !reflect.DeepEqual(events[0].Attrs, Attrs{"target": "target", "method": "POST", "path": "/p ath", "status": status, "duration_us": int64(789)}) {
					t.Fatal(events)
				}
				wantUser, wantID := "", ""
				if withCaller {
					wantUser = "user"
					wantID = "id"
				}
				if events[0].User != wantUser || events[0].RequestID != wantID {
					t.Fatal(events[0])
				}
			})
		}
	}
}

// R-3Q8B-AP8M R-3V3W-TS7E R-JM59-H5JP
func TestSiblingRedirectsAndDefaultTransport(t *testing.T) {
	now := time.Unix(100, 0)
	w, c := httpWriter(t, func() time.Time { return now }, nil)
	server := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			http.Redirect(out, r, "/last?secret=hidden", http.StatusFound)
			return
		}
		out.WriteHeader(204)
	}))
	defer server.Close()
	// The default transport is replaced temporarily with the loopback server's transport.
	original := http.DefaultTransport
	http.DefaultTransport = httpRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		response, err := server.Client().Transport.RoundTrip(r)
		now = now.Add(-time.Microsecond)
		return response, err
	})
	defer func() { http.DefaultTransport = original }()
	response, err := SiblingClient(w, "redirect", nil).Get(server.URL + "/first")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	events := httpEvents(t, w, c)
	if len(events) != 2 || events[0].Attrs["path"] != "/first" || events[1].Attrs["path"] != "/last" || events[0].Attrs["status"] != int64(302) || events[1].Attrs["status"] != int64(204) || events[0].Attrs["duration_us"] != int64(0) || events[1].Attrs["duration_us"] != int64(0) {
		t.Fatal(events)
	}
}

// R-3NSI-J5R8 R-417E-QMWV
func TestHTTPConcurrentCorrelation(t *testing.T) {
	w, c := httpWriter(t, nil, bytes.NewReader(bytes.Repeat([]byte{1}, 32*16)))
	client := SiblingClient(w, "target", httpRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		caller, _ := identity.FromContext(r.Context())
		if r.Header.Get("X-Request-Id") != caller.RequestID || r.Header.Get("X-User-Id") != caller.UserID {
			t.Error("identity mixed")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}))
	handler := Middleware(w, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		out := httptest.NewRequest("GET", "http://sibling/", nil).WithContext(r.Context())
		response, err := client.Transport.RoundTrip(out)
		if err != nil {
			t.Error(err)
		}
		_ = response.Body.Close()
	}))
	var wg sync.WaitGroup
	for n := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", fmt.Sprintf("/r%d", n), nil)
			r.Header.Set("X-User-Id", fmt.Sprint(n))
			r.Header.Set("X-Request-Id", fmt.Sprintf("id%d", n))
			handler.ServeHTTP(httptest.NewRecorder(), r)
		}()
	}
	wg.Wait()
	events := httpEvents(t, w, c)
	if len(events) != 96 {
		t.Fatal(len(events))
	}
	seen := map[string][]string{}
	for _, e := range events {
		if e.RequestID != "id"+e.User {
			t.Fatal(e)
		}
		seen[e.RequestID] = append(seen[e.RequestID], e.Name)
	}
	for _, names := range seen {
		if !reflect.DeepEqual(names, []string{"request.started", "sibling.called", "request.finished"}) {
			t.Fatal(names)
		}
	}
}

// R-36PX-6DDI R-E57I-1B84 R-DO4W-OIUE
func TestSocketTransport(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	path := filepath.Join(t.TempDir(), "sock")
	transport := SocketTransport(path)
	defer transport.CloseIdleConnections()
	if transport == nil || transport.Proxy != nil {
		t.Fatal("transport or proxy")
	}
	request := httptest.NewRequest("GET", "http://arbitrary.invalid:9876/path", nil)
	_, err := transport.RoundTrip(request)
	var op *net.OpError
	if !errors.As(err, &op) || op.Op != "dial" {
		t.Fatalf("dial error=%v", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		out.Header().Set("Content-Type", "text/plain")
		out.Header().Set("Host-Seen", r.Host)
		out.Header().Set("Path-Seen", r.URL.Path)
		_, _ = io.WriteString(out, "reached")
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	defer func() { _ = server.Close(); <-done }()
	for _, host := range []string{"different.invalid:99", "second.invalid"} {
		request := httptest.NewRequest("GET", "http://"+host+"/path", nil)
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || string(body) != "reached" || response.Header.Get("Path-Seen") != "/path" || response.Header.Get("Host-Seen") != host {
			t.Fatal("socket not reached")
		}
	}
	// A relative path is resolved when DialContext connects, after construction.
	relative := SocketTransport("sock")
	defer relative.CloseIdleConnections()
	t.Chdir(filepath.Dir(path))
	response, err := relative.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

// R-DWO7-CX19 R-3NSI-J5R8
func TestMiddlewareConcurrentMintedIDs(t *testing.T) {
	var data []byte
	for n := range 32 {
		data = append(data, bytes.Repeat([]byte{byte(n)}, 16)...)
	}
	random := &httpCountingReader{Reader: bytes.NewReader(data)}
	w, c := httpWriter(t, nil, random)
	handler := Middleware(w, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		caller, ok := identity.FromContext(r.Context())
		if !ok || caller.RequestID != r.Header.Get("X-Request-Id") || caller.UserID != r.Header.Get("X-User-Id") {
			t.Error("context mismatch")
		}
	}))
	var wg sync.WaitGroup
	for n := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("X-User-Id", fmt.Sprint(n))
			handler.ServeHTTP(httptest.NewRecorder(), r)
		}()
	}
	wg.Wait()
	events := httpEvents(t, w, c)
	byUser := map[string]string{}
	ids := map[string]bool{}
	for _, e := range events {
		if e.Name == "request.started" {
			if len(e.RequestID) != 32 || ids[e.RequestID] {
				t.Fatal("minted id collision")
			}
			ids[e.RequestID] = true
			byUser[e.User] = e.RequestID
		} else if byUser[e.User] != e.RequestID {
			t.Fatal("request correlation mixed")
		}
	}
	if len(events) != 64 || len(ids) != 32 || random.reads != 32 || random.bytes != 512 {
		t.Fatalf("events=%d IDs=%d reads=%d bytes=%d", len(events), len(ids), random.reads, random.bytes)
	}
}

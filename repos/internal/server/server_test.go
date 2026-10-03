package server_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/repos/internal/server"
)

type acceptResult struct {
	conn net.Conn
	err  error
}

type listener struct {
	results chan acceptResult
	closed  chan struct{}
	waiting chan struct{}
	once    sync.Once
}

func newListener() *listener {
	return &listener{results: make(chan acceptResult), closed: make(chan struct{}), waiting: make(chan struct{}, 16)}
}

func (l *listener) Accept() (net.Conn, error) {
	l.waiting <- struct{}{}
	select {
	case result := <-l.results:
		return result.conn, result.err
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *listener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *listener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server interaction")
		var zero T
		return zero
	}
}

func assertPending[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case value := <-ch:
		t.Fatalf("unexpected early result: %v", value)
	default:
	}
}

func start(t *testing.T, h http.Handler, drain time.Duration, stop func(context.Context)) (*listener, context.CancelFunc, <-chan error) {
	t.Helper()
	ln := newListener()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func(serve func(context.Context, net.Listener, http.Handler, time.Duration, func(context.Context)) error) {
		done <- serve(ctx, ln, h, drain, stop)
	}(server.Serve)
	receive(t, ln.waiting)
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = ln.Close() })
	return ln, cancel, done
}

func connect(t *testing.T, ln *listener) net.Conn {
	t.Helper()
	return connectWrapped(t, ln, func(conn net.Conn) net.Conn { return conn })
}

func connectWrapped(t *testing.T, ln *listener, wrap func(net.Conn) net.Conn) net.Conn {
	t.Helper()
	client, accepted := net.Pipe()
	select {
	case ln.results <- acceptResult{conn: wrap(accepted)}:
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not accept connection")
	}
	receive(t, ln.waiting)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type heldWrite struct {
	net.Conn
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (c *heldWrite) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return c.Conn.Write(p)
}

type response struct {
	status int
	body   string
	header http.Header
	err    error
}

func request(conn net.Conn, path string) <-chan response {
	result := make(chan response, 1)
	go func() {
		_, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: test\r\n\r\n", path)
		if err != nil {
			result <- response{err: err}
			return
		}
		r, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			result <- response{err: err}
			return
		}
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		result <- response{status: r.StatusCode, body: string(body), header: r.Header, err: err}
	}()
	return result
}

// R-SGGC-312V R-P98K-SZ69
func TestServeHTTPOnGivenListener(t *testing.T) {
	ln, cancel, done := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, html.EscapeString("answer "+r.URL.Path))
	}), time.Second, nil)
	conn := connect(t, ln)
	for _, path := range []string{"/first", "/second"} {
		got := receive(t, request(conn, path))
		if got.err != nil || got.status != http.StatusCreated || got.body != "answer "+path || got.header.Get("X-Test") != path {
			t.Fatalf("response: %+v", got)
		}
		assertPending(t, done)
	}
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
}

// R-PAGH-6QWY
func TestCancellationClosesListenerAndEmptyConnections(t *testing.T) {
	ln, cancel, done := start(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }), time.Second, nil)
	idle := connect(t, ln)
	if got := receive(t, request(idle, "/")); got.err != nil || got.body != "ok" {
		t.Fatalf("response: %+v", got)
	}
	newConn := connect(t, ln)
	partial := connect(t, ln)
	written := make(chan error, 1)
	go func() { _, err := io.WriteString(partial, "GET / HTTP/1.1\r\n"); written <- err }()
	if err := receive(t, written); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	receive(t, ln.closed)
	for _, conn := range []net.Conn{idle, newConn, partial} {
		read := make(chan error, 1)
		go func() { var b [1]byte; _, err := conn.Read(b[:]); read <- err }()
		if err := receive(t, read); err == nil {
			t.Fatal("empty connection remained open")
		}
	}
}

// R-PBOD-KINN R-PE46-C251
func TestGracefulDrainDeliversResponseThenCallsStop(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	stopEntered := make(chan context.Context, 1)
	stopRelease := make(chan struct{})
	writeEntered := make(chan struct{})
	writeRelease := make(chan struct{})
	var calls atomic.Int32
	ln, cancel, done := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- r.Context()
		<-release
		_, _ = io.WriteString(w, strings.Repeat("complete", 256))
	}), 300*time.Millisecond, func(ctx context.Context) {
		calls.Add(1)
		stopEntered <- ctx
		<-stopRelease
	})
	conn := connectWrapped(t, ln, func(conn net.Conn) net.Conn {
		return &heldWrite{Conn: conn, entered: writeEntered, release: writeRelease}
	})
	result := request(conn, "/")
	requestCtx := receive(t, entered)
	assertPending(t, stopEntered)
	cancel()
	receive(t, ln.closed)
	assertPending(t, requestCtx.Done())
	assertPending(t, stopEntered)
	close(release)
	receive(t, writeEntered)
	assertPending(t, stopEntered)
	assertPending(t, done)
	close(writeRelease)
	got := receive(t, result)
	if got.err != nil || got.body != strings.Repeat("complete", 256) {
		t.Fatalf("response: %+v", got)
	}
	stopCtx := receive(t, stopEntered)
	assertPending(t, stopCtx.Done())
	assertPending(t, done)
	close(stopRelease)
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("stop called %d times", calls.Load())
	}
	assertPending(t, stopCtx.Done())
	receive(t, stopCtx.Done())
}

// R-PCW9-YAEC R-PE46-C251
func TestForcedDrainStopsThenCancelsAndClosesWithoutWaiting(t *testing.T) {
	entered := make(chan context.Context, 2)
	release := make(chan struct{})
	returned := make(chan struct{}, 2)
	stopEntered := make(chan context.Context, 1)
	stopRelease := make(chan struct{})
	var calls atomic.Int32
	ln, cancel, done := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "prefix")
		w.(http.Flusher).Flush()
		entered <- r.Context()
		<-release
		_, _ = io.WriteString(w, "suffix")
		returned <- struct{}{}
	}), 100*time.Millisecond, func(ctx context.Context) {
		calls.Add(1)
		stopEntered <- ctx
		<-stopRelease
	})
	results := []<-chan response{request(connect(t, ln), "/one"), request(connect(t, ln), "/two")}
	contexts := []context.Context{receive(t, entered), receive(t, entered)}
	cancel()
	stopCtx := receive(t, stopEntered)
	if stopCtx.Err() == nil {
		t.Fatal("stop context not done at cutoff")
	}
	for _, ctx := range contexts {
		assertPending(t, ctx.Done())
	}
	for _, result := range results {
		assertPending(t, result)
	}
	assertPending(t, done)
	close(stopRelease)
	var drainErr *server.DrainError
	if err := receive(t, done); !errors.As(err, &drainErr) || drainErr.Unfinished != 2 {
		t.Fatalf("drain error: %v", err)
	}
	for _, ctx := range contexts {
		receive(t, ctx.Done())
	}
	for _, result := range results {
		got := receive(t, result)
		if got.body != "prefix" || !errors.Is(got.err, io.ErrUnexpectedEOF) {
			t.Fatalf("cutoff response: %+v", got)
		}
	}
	assertPending(t, returned)
	if calls.Load() != 1 {
		t.Fatalf("stop called %d times", calls.Load())
	}
	close(release)
	receive(t, returned)
	receive(t, returned)
}

// R-SIW4-UKK9 R-PFC2-PTVQ
func TestDrainErrorText(t *testing.T) {
	for _, n := range []int{-2, 0, 1, 2, 27} {
		e := &server.DrainError{Unfinished: n}
		var err error = e
		want := "stopped with " + strconv.Itoa(n) + " requests unfinished"
		if n == 1 {
			want = "stopped with 1 request unfinished"
		}
		if err.Error() != want {
			t.Fatalf("%d: %q != %q", n, err.Error(), want)
		}
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary accept failure" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

// R-PGJZ-3LMF R-PE46-C251
func TestAcceptFailuresRetryTemporaryAndReturnPermanent(t *testing.T) {
	var calls atomic.Int32
	ln, _, done := start(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "retried") }), time.Second, func(context.Context) { calls.Add(1) })
	ln.results <- acceptResult{err: temporaryError{}}
	receive(t, ln.waiting)
	if got := receive(t, request(connect(t, ln), "/")); got.err != nil || got.body != "retried" {
		t.Fatalf("response: %+v", got)
	}
	failure := errors.New("accept failed permanently")
	ln.results <- acceptResult{err: failure}
	if err := receive(t, done); !errors.Is(err, failure) {
		t.Fatalf("failure: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("stop called %d times before cancellation", calls.Load())
	}
}

// R-PIZR-V53T
func TestServerDoesNotUseDefaultLogger(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(old) })
	ln, cancel, done := start(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("handler failure") }), time.Second, nil)
	ln.results <- acceptResult{err: temporaryError{}}
	receive(t, ln.waiting)
	if got := receive(t, request(connect(t, ln), "/")); got.err == nil {
		t.Fatal("panic response succeeded")
	}
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("default logger output: %q", output.String())
	}
}

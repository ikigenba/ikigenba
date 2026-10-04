package server_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/server"
)

func listener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}
func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}
func start(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) <-chan error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln, h, drain, stop) }()
	return done
}
func request(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err = c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(c, "GET /held?query=value HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	return c
}

// R-UUUN-OVDR R-UYIC-U6LU R-V0Y5-LQ38 R-V3DY-D9KM
func TestServeCompletesResponseBeforeStop(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	stopCalls := 0
	var stopContext context.Context
	done := start(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Proto != "HTTP/1.1" || r.URL.RequestURI() != "/held?query=value" {
			t.Error("request changed")
		}
		close(entered)
		<-release
		w.Header().Set("X-Test", "answer")
		_, _ = io.WriteString(w, "whole answer")
		close(returned)
	}), 150*time.Millisecond, func(ctx context.Context) {
		stopContext = ctx
		stopCalls++
		select {
		case <-returned:
		default:
			t.Error("stop preceded handler completion")
		}
		if ctx.Err() != nil {
			t.Error("stop context ended early")
		}
	})
	c := request(t, ln)
	await(t, entered)
	select {
	case err := <-done:
		t.Fatalf("returned before cancellation: %v", err)
	default:
	}
	cancel()
	close(release)
	response, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.Header.Get("X-Test") != "answer" || string(body) != "whole answer" {
		t.Fatalf("response: %#v %q", response, string(body))
	}
	if err = await(t, done); err != nil {
		t.Fatal(err)
	}
	if stopCalls != 1 {
		t.Fatalf("stop calls=%d", stopCalls)
	}
	deadline, ok := stopContext.Deadline()
	if !ok || !time.Now().Before(deadline) || stopContext.Err() != nil {
		t.Fatal("retained stop context ended before its deadline")
	}
	await(t, stopContext.Done())
	if time.Now().Before(deadline) || !errors.Is(stopContext.Err(), context.DeadlineExceeded) {
		t.Fatal("stop context did not end at its deadline")
	}
}

// R-UZQ9-7YCJ
func TestCancellationClosesListenerAndRequestlessConnection(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := make(chan struct{}, 1)
	wrapped := &observeListener{Listener: ln, accepted: accepted}
	done := start(ctx, wrapped, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") }), time.Second, nil)
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	await(t, accepted)
	cancel()
	if err = await(t, done); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err = c.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("requestless connection did not close with EOF: %v", err)
	}
	if _, err = ln.Accept(); err == nil {
		t.Fatal("listener stayed open")
	}
}

type observeListener struct {
	net.Listener
	accepted chan struct{}
}

func (l *observeListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e == nil {
		l.accepted <- struct{}{}
	}
	return c, e
}

// R-UW2K-2N4G R-LR2V-WLUM R-V3DY-D9KM
func TestDeadlineStopsBeforeCancelAndDoesNotWaitForHandlers(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan context.Context, 2)
	release := make(chan struct{})
	defer close(release)
	stopCalled := false
	var requestContexts []context.Context
	done := start(ctx, ln, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { entered <- r.Context(); <-release }), 60*time.Millisecond, func(stopCtx context.Context) {
		stopCalled = true
		for _, requestCtx := range requestContexts {
			if requestCtx.Err() != nil {
				t.Error("request canceled before stop returned")
			}
		}
		if stopCtx.Err() == nil {
			t.Error("deadline context still live")
		}
	})
	c1 := request(t, ln)
	c2 := request(t, ln)
	r1 := await(t, entered)
	r2 := await(t, entered)
	requestContexts = []context.Context{r1, r2}
	cancel()
	err := await(t, done)
	var drainErr *server.DrainError
	if !errors.As(err, &drainErr) || drainErr.Unfinished != 2 {
		t.Fatalf("drain result=%v", err)
	}
	if !stopCalled || r1.Err() == nil || r2.Err() == nil {
		t.Fatal("stop or request cancellation missing")
	}
	for _, c := range []net.Conn{c1, c2} {
		var b [1]byte
		if _, err = c.Read(b[:]); err == nil {
			t.Fatal("cutoff connection stayed open")
		}
	}
}

// R-LR2V-WLUM
func TestCutoffWriteSeesEndedContext(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writing := make(chan struct{})
	failed := make(chan error, 1)
	done := start(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(writing)
		chunk := make([]byte, 64*1024)
		for {
			if _, err := w.Write(chunk); err != nil {
				failed <- r.Context().Err()
				return
			}
		}
	}), 60*time.Millisecond, nil)
	c := request(t, ln)
	_ = c
	await(t, writing)
	cancel()
	var de *server.DrainError
	if err := await(t, done); !errors.As(err, &de) || de.Unfinished != 1 {
		t.Fatalf("drain=%v", err)
	}
	if err := await(t, failed); err == nil {
		t.Fatal("write failed before context ended")
	}
}

// R-V4LU-R1BB
func TestDrainErrorText(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{{1, "stopped with 1 request unfinished"}, {0, "stopped with 0 requests unfinished"}, {2, "stopped with 2 requests unfinished"}, {-3, "stopped with -3 requests unfinished"}} {
		e := &server.DrainError{Unfinished: tc.n}
		if e.Error() != tc.want {
			t.Fatalf("%d: %q", tc.n, e.Error())
		}
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary accept failure" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

type failingListener struct {
	net.Listener
	mu    sync.Mutex
	first bool
	fatal error
}

func (l *failingListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.first {
		l.first = true
		l.mu.Unlock()
		return nil, temporaryError{}
	}
	l.mu.Unlock()
	if l.fatal != nil {
		return nil, l.fatal
	}
	return l.Listener.Accept()
}

// R-V5TR-4T20 R-V3DY-D9KM
func TestAcceptFailureReturnsErrorWithoutStop(t *testing.T) {
	ln := listener(t)
	fatal := errors.New("permanent failure")
	stopped := false
	err := server.Serve(context.Background(), &failingListener{Listener: ln, fatal: fatal}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, func(context.Context) { stopped = true })
	if !errors.Is(err, fatal) || stopped {
		t.Fatalf("error=%v stopped=%v", err, stopped)
	}
}

// R-V71N-IKSP R-V5TR-4T20
func TestTemporaryAcceptAndPanicDoNotLog(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	panicked := make(chan struct{})
	done := start(ctx, &failingListener{Listener: ln}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(panicked); panic("handler failure") }), time.Second, nil)
	c := request(t, ln)
	await(t, panicked)
	var b [1]byte
	if _, err := c.Read(b[:]); err == nil {
		t.Fatal("panic connection stayed open")
	}
	cancel()
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "" {
		t.Fatalf("default logger: %q", output.String())
	}
}

func TestGoexitReleasesHandler(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan struct{})
	done := start(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { defer close(ended); runtime.Goexit() }), time.Second, nil)
	c := request(t, ln)
	await(t, ended)
	var b [1]byte
	_, _ = c.Read(b[:])
	cancel()
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
}

// R-UZQ9-7YCJ
func TestRequestlessConnectionClosesWhileHandlerStillDrains(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := make(chan struct{}, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	done := start(ctx, &observeListener{Listener: ln, accepted: accepted}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "finished")
	}), 2*time.Second, nil)
	held := request(t, ln)
	await(t, entered)
	await(t, accepted)
	empty, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = empty.Close() }()
	await(t, accepted)
	cancel()
	_ = empty.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err = empty.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("requestless connection did not close during active drain: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("held handler did not keep drain open: %v", err)
	default:
	}
	releaseOnce.Do(func() { close(release) })
	response, err := http.ReadResponse(bufio.NewReader(held), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = await(t, done); err != nil {
		t.Fatal(err)
	}
}

package server_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/home/internal/server"
)

func receive[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	dir, err := os.MkdirTemp("", "home-server-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	ln, err := net.Listen("unix", filepath.Join(dir, "http.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func connect(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()
	c, err := net.DialTimeout(ln.Addr().Network(), ln.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return c
}

func send(t *testing.T, c net.Conn, path string) {
	t.Helper()
	if _, err := fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: example.test\r\n\r\n", path); err != nil {
		t.Fatal(err)
	}
}

func response(t *testing.T, c net.Conn) string {
	t.Helper()
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	if r.Proto != "HTTP/1.1" || r.StatusCode != http.StatusCreated || r.Header.Get("X-Test") != "response" {
		t.Fatalf("unexpected response: %v", r)
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type observedListener struct {
	net.Listener
	accepted chan net.Conn
	closed   chan struct{}
	once     sync.Once
}

func (ln *observedListener) Accept() (net.Conn, error) {
	c, err := ln.Listener.Accept()
	if err == nil {
		ln.accepted <- c
	}
	return c, err
}

func (ln *observedListener) Close() error {
	err := ln.Listener.Close()
	ln.once.Do(func() { close(ln.closed) })
	return err
}

// R-6V1B-BZVA R-6MI0-NLOF R-6NPX-1DF4 R-6Q5P-SWWI R-6XH4-3JCO
func TestServeCompletesResponsesAndStopsEarly(t *testing.T) {
	ln := &observedListener{Listener: listen(t), accepted: make(chan net.Conn, 2), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan string, 2)
	release := make(chan struct{})
	stopEntered := make(chan context.Context, 1)
	stopRelease := make(chan struct{})
	done := make(chan error, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- r.URL.Path
		<-release
		if r.Context().Err() != nil {
			t.Error("request cancelled during successful drain")
		}
		w.Header().Set("X-Test", "response")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "complete"+strings.Repeat("x", 32000))
	})
	go func() {
		done <- server.Serve(ctx, ln, h, time.Minute, func(c context.Context) {
			stopEntered <- c
			<-stopRelease
		})
	}()
	first, second := connect(t, ln), connect(t, ln)
	send(t, first, "/first")
	send(t, second, "/second")
	paths := map[string]bool{receive(t, entered): true, receive(t, entered): true}
	if !paths["/first"] || !paths["/second"] {
		t.Fatalf("handler paths: %v", paths)
	}
	select {
	case err := <-done:
		t.Fatalf("returned before cancellation: %v", err)
	case <-stopEntered:
		t.Fatal("stop called before cancellation")
	default:
	}
	cancel()
	receive(t, ln.closed)
	select {
	case <-stopEntered:
		t.Fatal("stop called before active handlers finished")
	default:
	}
	close(release)
	if got := response(t, first); got != "complete"+strings.Repeat("x", 32000) {
		t.Fatal("first response truncated")
	}
	if got := response(t, second); got != "complete"+strings.Repeat("x", 32000) {
		t.Fatal("second response truncated")
	}
	c := receive(t, stopEntered)
	if c.Err() != nil {
		t.Fatal("stop context ended before deadline")
	}
	select {
	case err := <-done:
		t.Fatalf("returned while stop was running: %v", err)
	default:
	}
	close(stopRelease)
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	if c.Err() != nil {
		t.Fatal("Serve cancelled stop context before its deadline")
	}
	select {
	case <-stopEntered:
		t.Fatal("stop called more than once")
	default:
	}
}

// R-4IW8-XTPD R-6OXT-F55T R-6XH4-3JCO
func TestDeadlineStopsBeforeCancellingUnfinishedRequests(t *testing.T) {
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan context.Context, 2)
	finished := make(chan struct{}, 2)
	release := make(chan struct{})
	releaseHandlers := sync.OnceFunc(func() { close(release) })
	defer releaseHandlers()
	stopEntered := make(chan context.Context, 1)
	stopRelease := make(chan struct{})
	done := make(chan error, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- r.Context()
		<-release
		_, _ = io.WriteString(w, "late response")
		finished <- struct{}{}
	})
	go func() {
		done <- server.Serve(ctx, ln, h, 100*time.Millisecond, func(c context.Context) {
			stopEntered <- c
			<-stopRelease
		})
	}()
	first, second := connect(t, ln), connect(t, ln)
	send(t, first, "/first")
	send(t, second, "/second")
	r1, r2 := receive(t, entered), receive(t, entered)
	cancelledAt := time.Now()
	cancel()
	c := receive(t, stopEntered)
	if time.Since(cancelledAt) < 100*time.Millisecond {
		t.Fatal("drain ended before its window elapsed")
	}
	if !errors.Is(c.Err(), context.DeadlineExceeded) {
		t.Fatalf("stop context: %v", c.Err())
	}
	if r1.Err() != nil || r2.Err() != nil {
		t.Fatal("requests cancelled before stop returned")
	}
	close(stopRelease)
	err := receive(t, done)
	var drainError *server.DrainError
	if !errors.As(err, &drainError) || drainError.Unfinished != 2 {
		t.Fatalf("drain result: %v", err)
	}
	for _, r := range []context.Context{r1, r2} {
		receive(t, r.Done())
	}
	for _, conn := range []net.Conn{first, second} {
		b, err := io.ReadAll(conn)
		if err != nil || len(b) != 0 {
			t.Fatalf("cut off response: %q, %v", b, err)
		}
	}
	select {
	case <-finished:
		t.Fatal("handler returned before Serve")
	default:
	}
	select {
	case <-stopEntered:
		t.Fatal("stop called more than once")
	default:
	}
	releaseHandlers()
	receive(t, finished)
	receive(t, finished)
}

// R-6NPX-1DF4 R-6Q5P-SWWI
func TestCancellationClosesIdleAndRequestlessConnections(t *testing.T) {
	ln := &observedListener{Listener: listen(t), accepted: make(chan net.Conn, 2), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Test", "response")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "complete")
		}), time.Minute, nil)
	}()
	idle, empty := connect(t, ln), connect(t, ln)
	receive(t, ln.accepted)
	receive(t, ln.accepted)
	send(t, idle, "/")
	if response(t, idle) != "complete" {
		t.Fatal("response did not complete")
	}
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	for _, c := range []net.Conn{idle, empty} {
		if b, err := io.ReadAll(c); len(b) != 0 || (err != nil && !errors.Is(err, net.ErrClosed)) {
			t.Fatalf("connection was not closed: %q, %v", b, err)
		}
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("listener still accepts")
	}
}

// R-4K45-BLG2
func TestDrainErrorText(t *testing.T) {
	for n, want := range map[int]string{
		1:  "stopped with 1 request unfinished",
		0:  "stopped with 0 requests unfinished",
		2:  "stopped with 2 requests unfinished",
		-3: "stopped with -3 requests unfinished",
	} {
		if got := (&server.DrainError{Unfinished: n}).Error(); got != want {
			t.Fatalf("count %d: got %q, want %q", n, got, want)
		}
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "retry accept" }
func (temporaryError) Temporary() bool { return true }
func (temporaryError) Timeout() bool   { return false }

type retryListener struct {
	net.Listener
	first bool
}

func (ln *retryListener) Accept() (net.Conn, error) {
	if !ln.first {
		ln.first = true
		return nil, temporaryError{}
	}
	return ln.Listener.Accept()
}

// R-6RDM-6ON7 R-5G6G-2CIH
func TestTemporaryAcceptAndHandlerPanicAreSilent(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	ln := &retryListener{Listener: listen(t)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			entered <- struct{}{}
			panic("test panic")
		}), time.Minute, nil)
	}()
	c := connect(t, ln)
	send(t, c, "/")
	receive(t, entered)
	_, _ = io.Copy(io.Discard, c)
	cancel()
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("default logger received %q", output.String())
	}
}

type failedListener struct {
	err error
}

func (ln *failedListener) Accept() (net.Conn, error) { return nil, ln.err }
func (ln *failedListener) Addr() net.Addr            { return &net.TCPAddr{} }
func (ln *failedListener) Close() error              { return nil }

// R-6RDM-6ON7 R-6XH4-3JCO
func TestAcceptFailureReturnsErrorWithoutStop(t *testing.T) {
	want := errors.New("failed accept")
	ln := &failedListener{err: want}
	called := false
	err := server.Serve(context.Background(), ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("unexpected request")
	}), time.Second, func(context.Context) { called = true })
	if err == nil || called {
		t.Fatalf("result %v, stop called %v", err, called)
	}
}

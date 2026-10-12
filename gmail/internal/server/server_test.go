package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/gmail/internal/server"
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

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

type response struct {
	status int
	body   string
	header http.Header
	err    error
}

type observedListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (l *observedListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return err
}

func request(ln net.Listener, path string) <-chan response {
	result := make(chan response, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		defer client.CloseIdleConnections()
		r, err := client.Get("http://" + ln.Addr().String() + path)
		if err != nil {
			result <- response{err: err}
			return
		}
		defer func() { _ = r.Body.Close() }()
		b, err := io.ReadAll(r.Body)
		result <- response{status: r.StatusCode, body: string(b), header: r.Header.Clone(), err: err}
	}()
	return result
}

// R-X9VU-NV6T R-XB3R-1MXI
func TestServeResponseAndGracefulDrain(t *testing.T) {
	ln := &observedListener{Listener: listener(t), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	stopCtx := make(chan context.Context, 1)
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/held" {
				entered <- struct{}{}
				<-release
			}
			w.Header().Set("X-Fixture", "owned")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, "fixture-body")
		}), time.Second, func(c context.Context) { calls.Add(1); stopCtx <- c })
	}()
	got := receive(t, request(ln, "/first"))
	if got.err != nil || got.status != http.StatusAccepted || got.body != "fixture-body" || got.header.Get("X-Fixture") != "owned" {
		t.Fatalf("response = %+v", got)
	}
	select {
	case err := <-done:
		t.Fatalf("returned before cancellation: %v", err)
	default:
	}
	held := request(ln, "/held")
	receive(t, entered)
	cancel()
	c := receive(t, stopCtx)
	if _, ok := c.Deadline(); !ok || c.Err() != nil {
		t.Fatalf("stop context: %v", c.Err())
	}
	select {
	case err := <-done:
		t.Fatalf("returned with handler active: %v", err)
	default:
	}
	// Shutdown has closed the listener while the original handler stays blocked.
	receive(t, ln.closed)
	if got := receive(t, request(ln, "/during")); got.err == nil {
		t.Fatal("accepted a new request during blocked drain")
	}
	select {
	case err := <-done:
		t.Fatalf("returned during blocked drain: %v", err)
	default:
	}
	release <- struct{}{}
	got = receive(t, held)
	if got.err != nil || got.body != "fixture-body" || got.status != http.StatusAccepted || got.header.Get("X-Fixture") != "owned" {
		t.Fatalf("drained response = %+v", got)
	}
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("stop calls = %d", calls.Load())
	}
	if got := receive(t, request(ln, "/after")); got.err == nil {
		t.Fatal("accepted request after drain")
	}
}

// R-XB3R-1MXI
func TestDrainDeadlineCountsRequests(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			ln := listener(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{}, count)
			release := make(chan struct{})
			defer close(release)
			finished := make(chan struct{}, count)
			var calls atomic.Int32
			stopDone := make(chan struct{})
			stopContext := make(chan context.Context, 1)
			done := make(chan error, 1)
			go func() {
				done <- server.Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { entered <- struct{}{}; <-release; finished <- struct{}{} }), 30*time.Millisecond, func(c context.Context) { calls.Add(1); stopContext <- c; <-c.Done(); close(stopDone) })
			}()
			requests := make([]<-chan response, count)
			for i := range count {
				requests[i] = request(ln, "/held")
				receive(t, entered)
			}
			began := time.Now()
			cancel()
			c := receive(t, stopContext)
			deadline, ok := c.Deadline()
			if !ok || deadline.Before(began.Add(25*time.Millisecond)) || deadline.After(began.Add(530*time.Millisecond)) {
				t.Fatalf("stop deadline offset = %s, has deadline %v", deadline.Sub(began), ok)
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(time.Until(began.Add(time.Second))):
				t.Fatal("30ms drain did not finish within tolerance")
			}
			if elapsed := time.Since(began); elapsed < 25*time.Millisecond {
				t.Fatalf("drain ended before its deadline: %s", elapsed)
			}
			var drainErr *server.DrainError
			if !errors.As(err, &drainErr) || drainErr.Unfinished != count {
				t.Fatalf("error = %v, want count %d", err, count)
			}
			receive(t, stopDone)
			if !errors.Is(c.Err(), context.DeadlineExceeded) {
				t.Fatalf("stop context ended without its deadline: %v", c.Err())
			}
			if calls.Load() != 1 {
				t.Fatalf("stop calls = %d", calls.Load())
			}
			if got := receive(t, request(ln, "/after")); got.err == nil {
				t.Fatal("accepted after deadline")
			}
			for i := range count {
				release <- struct{}{}
				receive(t, finished)
				receive(t, requests[i])
			}
		})
	}
}

// R-XB3R-1MXI
func TestServingFailureReturns(t *testing.T) {
	ln := listener(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(context.Background(), ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, nil)
	}()
	if err := receive(t, done); err == nil {
		t.Fatal("closed listener did not fail")
	}
}

// R-XCBN-FEO7
func TestDrainError(t *testing.T) {
	for _, count := range []int{0, 1, 2, 17} {
		want := "stopped with " + strconv.Itoa(count) + " requests unfinished"
		if count == 1 {
			want = "stopped with 1 request unfinished"
		}
		var err error = &server.DrainError{Unfinished: count}
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
	}
}

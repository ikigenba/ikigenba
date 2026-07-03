package appkit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// discardLogger is a no-op logger for the lifecycle tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// newTestServer stands up a real loopback server on an ephemeral port so
// runServerAndWorkers exercises the actual server.Run + Shutdown path. It returns
// the server; the OS picks the port (":0") so tests never collide.
func newServeTestServer(t *testing.T) *http.Server {
	t.Helper()
	return &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: http.NewServeMux(),
	}
}

// TestWorkers_RunAndCancelOnShutdown asserts a worker actually runs and that a
// shutdown (ctx cancel) propagates to the worker, which unwinds cleanly.
func TestWorkers_RunAndCancelOnShutdown(t *testing.T) {
	var started, sawCancel atomic.Bool
	worker := func(ctx context.Context) error {
		started.Store(true)
		<-ctx.Done() // transport-fault-free worker: blocks until the serve ctx is cancelled
		sawCancel.Store(true)
		return nil // clean shutdown
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv := newServeTestServer(t)

	done := make(chan error, 1)
	go func() {
		done <- runServerAndWorkers(ctx, cancel, srv, []func(context.Context) error{worker}, discardLogger())
	}()

	// Give the worker + server a moment to start, then trigger a clean shutdown.
	waitFor(t, func() bool { return started.Load() }, "worker did not start")
	cancel() // simulate a SIGTERM cancelling the serve context

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("clean shutdown returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServerAndWorkers did not return after shutdown")
	}
	if !sawCancel.Load() {
		t.Fatal("worker was never cancelled by the shutdown")
	}
}

// TestWorkers_ErrorBringsServerDown asserts a worker returning (a structural
// fault) cancels the serve context, bringing the HTTP server down with it, and
// that the worker's error becomes the serve return value — the
// consumer-fault-cancels-server coupling (decision 11).
func TestWorkers_ErrorBringsServerDown(t *testing.T) {
	wantErr := errors.New("structural consumer fault: feed_offset missing")
	worker := func(ctx context.Context) error {
		return wantErr // returns immediately — a structural fault, NOT a retried transport fault
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := newServeTestServer(t)

	done := make(chan error, 1)
	go func() {
		done <- runServerAndWorkers(ctx, cancel, srv, []func(context.Context) error{worker}, discardLogger())
	}()

	select {
	case err := <-done:
		// The server must have been brought down by the worker's cancel(), and the
		// worker's error must surface as the serve result.
		if !errors.Is(err, wantErr) {
			t.Fatalf("serve error = %v, want it to be the worker's structural fault", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a worker fault did not bring the server down (no return)")
	}
}

// TestWorkers_TransportFaultDoesNotKillServer asserts a worker that keeps
// retrying internally (never returns) does NOT take the server down: the serve
// loop stays up until a real shutdown signal. This is the crm-unreachable case
// notify must survive.
func TestWorkers_TransportFaultDoesNotKillServer(t *testing.T) {
	worker := func(ctx context.Context) error {
		<-ctx.Done() // model an engine that retries forever and only stops on shutdown
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv := newServeTestServer(t)

	done := make(chan error, 1)
	go func() {
		done <- runServerAndWorkers(ctx, cancel, srv, []func(context.Context) error{worker}, discardLogger())
	}()

	// The serve loop must still be running after a grace period (the worker never
	// returned, so nothing cancelled ctx).
	select {
	case err := <-done:
		t.Fatalf("serve returned early (%v) — a non-returning worker should NOT take the server down", err)
	case <-time.After(250 * time.Millisecond):
		// good: still up
	}
	// A real shutdown now unwinds everything cleanly.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("clean shutdown after transport-only worker returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after shutdown")
	}
}

// TestWorkers_NoneCollapsesToServer asserts the zero-worker case is exactly
// server.Run (producers / DB-only services pass no Workers).
func TestWorkers_NoneCollapsesToServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := newServeTestServer(t)

	done := make(chan error, 1)
	go func() { done <- runServerAndWorkers(ctx, cancel, srv, nil, discardLogger()) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("no-worker shutdown returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no-worker serve did not return after shutdown")
	}
}

// waitFor polls cond until it is true or the deadline elapses.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

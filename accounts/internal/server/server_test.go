package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/server"
)

type observedListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (ln *observedListener) Close() error {
	err := ln.Listener.Close()
	ln.once.Do(func() { close(ln.closed) })
	return err
}

func listen(t *testing.T) *observedListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return &observedListener{Listener: ln, closed: make(chan struct{})}
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

// R-QGN9-KO6D R-QJ32-C7NR
func TestDrainError(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{{0, "stopped with 0 requests unfinished"}, {1, "stopped with 1 request unfinished"}, {2, "stopped with 2 requests unfinished"}, {19, "stopped with 19 requests unfinished"}} {
		err := &server.DrainError{tc.n}
		if err.Unfinished != tc.n || err.Error() != tc.want {
			t.Fatalf("DrainError(%d) = %q, want %q", tc.n, err.Error(), tc.want)
		}
	}
}

// R-QFFD-6WFO R-QHV5-YFX2
func TestServeResponseAndGracefulDrain(t *testing.T) {
	serve := server.Serve
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	want := "chosen response payload"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.Header().Set("X-Test-Response", r.Header.Get("X-Test-Request"))
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, want)
	})
	stops := make(chan error, 2)
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, ln, handler, time.Second, func(stopCtx context.Context) {
			if _, ok := stopCtx.Deadline(); !ok {
				stops <- errors.New("stop context has no deadline")
				return
			}
			stops <- stopCtx.Err()
		})
	}()
	responses := make(chan *http.Response, 1)
	requestErr := make(chan error, 1)
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	go func() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String()+"/chosen", nil)
		if err != nil {
			requestErr <- err
			return
		}
		req.Header.Set("X-Test-Request", "caller-value")
		resp, err := client.Do(req)
		if err != nil {
			requestErr <- err
			return
		}
		responses <- resp
	}()
	await(t, entered)
	select {
	case err := <-done:
		t.Fatalf("Serve returned before cancellation: %v", err)
	default:
	}
	cancel()
	await(t, ln.closed)
	select {
	case err := <-done:
		t.Fatalf("Serve did not wait for active handler: %v", err)
	case <-stops:
		t.Fatal("stop ran before the handler finished")
	default:
	}
	close(release)
	var response *http.Response
	select {
	case response = <-responses:
	case err := <-requestErr:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("response did not finish")
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != want || response.StatusCode != http.StatusAccepted || response.Header.Get("X-Test-Response") != "caller-value" {
		t.Fatalf("handler response changed: %d %v %q, %v", response.StatusCode, response.Header, body, err)
	}
	if err := await(t, stops); err != nil {
		t.Fatalf("stop context was already done: %v", err)
	}
	if err := await(t, done); err != nil {
		t.Fatalf("clean drain returned %v", err)
	}
	select {
	case <-stops:
		t.Fatal("stop called more than once")
	default:
	}
}

// R-QHV5-YFX2
func TestServeDrainCountsHandlersAndCancelsTheirContexts(t *testing.T) {
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan context.Context, 2)
	finished := make(chan struct{}, 2)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		entered <- r.Context()
		if r.URL.Path == "/context-aware" {
			<-r.Context().Done()
		} else {
			<-release
		}
		finished <- struct{}{}
	})
	stops := make(chan error, 2)
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, handler, 100*time.Millisecond, func(stopCtx context.Context) { stops <- stopCtx.Err() })
	}()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	requests := make(chan struct{}, 2)
	for _, path := range []string{"/context-aware", "/blocked"} {
		go func() {
			response, err := client.Get("http://" + ln.Addr().String() + path)
			if err == nil {
				_ = response.Body.Close()
			}
			requests <- struct{}{}
		}()
	}
	first, second := await(t, entered), await(t, entered)
	cancel()
	err := await(t, done)
	var drainErr *server.DrainError
	if !errors.As(err, &drainErr) || drainErr.Unfinished != 2 {
		t.Fatalf("drain error = %v, want 2 unfinished", err)
	}
	if first.Err() == nil || second.Err() == nil {
		t.Fatal("unfinished request context not canceled")
	}
	if !errors.Is(await(t, stops), context.DeadlineExceeded) {
		t.Fatal("stop context did not reach drain deadline")
	}
	select {
	case <-stops:
		t.Fatal("stop called more than once")
	default:
	}
	close(release)
	for range 2 {
		await(t, finished)
		await(t, requests)
	}
}

type failedListener struct{ err error }

func (ln failedListener) Accept() (net.Conn, error) { return nil, ln.err }
func (failedListener) Close() error                 { return nil }
func (failedListener) Addr() net.Addr               { return &net.TCPAddr{} }

// R-MY4W-C8QN
func TestServeAcceptFailure(t *testing.T) {
	want := errors.New("listener gone")
	err := server.Serve(context.Background(), failedListener{want}, http.NotFoundHandler(), time.Second, func(context.Context) {})
	var drainErr *server.DrainError
	if err == nil || errors.As(err, &drainErr) || !strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("Accept failure = %v", err)
	}
}

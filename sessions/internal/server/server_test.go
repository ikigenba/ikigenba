package server_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sessions/internal/server"
)

// R-UFVL-2V07 R-UKR6-LXYZ
func TestDrainError(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 2, 17} {
		e := &server.DrainError{n}
		var err error = e
		want := "stopped with " + strconv.Itoa(n) + " requests unfinished"
		if n == 1 {
			want = "stopped with 1 request unfinished"
		}
		if err.Error() != want || e.Unfinished != n {
			t.Fatalf("DrainError(%d) = %q", n, err.Error())
		}
	}
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

func listener(t *testing.T) *observedListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := &observedListener{Listener: ln, closed: make(chan struct{})}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func request(t *testing.T, ln net.Listener, path string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.SetDeadline(time.Now().Add(4 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: test.invalid\r\nConnection: close\r\n\r\n", path); err != nil {
		t.Fatal(err)
	}
	return c
}

func wait[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(4 * time.Second):
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

func response(t *testing.T, c net.Conn, body string) {
	t.Helper()
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusAccepted || string(b) != body || r.Header.Get("X-Served") != "fixture" {
		t.Fatalf("response = %d %q %v", r.StatusCode, b, r.Header)
	}
}

// R-UENO-P39I R-UIBD-UEHL
func TestServeResponsesAndGracefulDrain(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan context.Context, 2)
	release := make(chan struct{})
	defer close(release)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := "answer-response"
		if r.URL.Path == "/held" {
			started <- r.Context()
			<-release
			body = "held-response"
		}
		w.Header().Set("X-Served", "fixture")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, body)
	})
	var stops atomic.Int32
	stopping := make(chan context.Context, 1)
	finishStop := make(chan struct{})
	defer close(finishStop)
	result := make(chan error, 1)
	serve := server.Serve
	go func() {
		result <- serve(ctx, ln, h, 2*time.Second, func(c context.Context) {
			stops.Add(1)
			stopping <- c
			<-finishStop
		})
	}()
	response(t, request(t, ln, "/answer"), "answer-response")
	c1 := request(t, ln, "/held")
	r1 := wait(t, started)
	c2 := request(t, ln, "/held")
	r2 := wait(t, started)
	select {
	case err := <-result:
		t.Fatalf("returned before cancellation: %v", err)
	default:
	}
	cancel()
	wait(t, ln.closed)
	for _, c := range []context.Context{r1, r2} {
		if c.Err() != nil {
			t.Fatal("request cancelled before the grace period ended")
		}
	}
	select {
	case <-stopping:
		t.Fatal("stop called before handlers finished")
	case err := <-result:
		t.Fatalf("returned with handlers in progress: %v", err)
	default:
	}
	if c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		_ = c.Close()
		t.Fatal("listener still accepts after cancellation")
	}
	release <- struct{}{}
	release <- struct{}{}
	response(t, c1, "held-response")
	response(t, c2, "held-response")
	stopctx := wait(t, stopping)
	if _, ok := stopctx.Deadline(); !ok || stopctx.Err() != nil {
		t.Fatal("graceful stop did not receive the remaining drain context")
	}
	finishStop <- struct{}{}
	if err := wait(t, result); err != nil || stops.Load() != 1 {
		t.Fatalf("Serve = %v, stops = %d", err, stops.Load())
	}
}

// R-UIBD-UEHL
func TestServeDeadlineCountsAndCancelsHandlers(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan context.Context, 3)
	release := make(chan struct{})
	defer close(release)
	finishOne := make(chan struct{})
	defer close(finishOne)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.Context()
		if r.URL.Path == "/finishes" {
			<-finishOne
			w.Header().Set("X-Served", "fixture")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, "finished")
			return
		}
		<-release
	})
	var stops atomic.Int32
	stopContexts := make(chan context.Context, 1)
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, h, 300*time.Millisecond, func(c context.Context) {
			stops.Add(1)
			stopContexts <- c
		})
	}()
	request(t, ln, "/one")
	c1 := wait(t, started)
	request(t, ln, "/two")
	c2 := wait(t, started)
	finishes := request(t, ln, "/finishes")
	wait(t, started)
	cancel()
	wait(t, ln.closed)
	finishOne <- struct{}{}
	response(t, finishes, "finished")
	err := wait(t, result)
	var drained *server.DrainError
	if !errors.As(err, &drained) || drained.Unfinished != 2 {
		t.Fatalf("Serve = %v, want two unfinished handlers", err)
	}
	for _, c := range []context.Context{c1, c2, wait(t, stopContexts)} {
		if !errors.Is(c.Err(), context.DeadlineExceeded) && !errors.Is(c.Err(), context.Canceled) {
			t.Fatalf("context at deadline = %v", c.Err())
		}
	}
	if stops.Load() != 1 {
		t.Fatalf("stop called %d times", stops.Load())
	}
}

// R-UIBD-UEHL
func TestStopContextExpiresWithinDrain(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopStarted := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), 100*time.Millisecond, func(c context.Context) {
			close(stopStarted)
			<-c.Done()
		})
	}()
	cancel()
	wait(t, stopStarted)
	if err := wait(t, result); err != nil {
		t.Fatal(err)
	}
}

type failedListener struct{ err error }

func (l failedListener) Accept() (net.Conn, error) { return nil, l.err }
func (failedListener) Close() error                { return nil }
func (failedListener) Addr() net.Addr              { return &net.TCPAddr{} }

// R-UJJA-868A
func TestServeListenerFailure(t *testing.T) {
	ctx := context.Background()
	err := server.Serve(ctx, failedListener{errors.New("listener gone")}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("failed listener dispatched a request")
	}), time.Second, func(context.Context) {})
	var drained *server.DrainError
	if err == nil || errors.As(err, &drained) || !strings.Contains(err.Error(), "listener gone") || ctx.Err() != nil {
		t.Fatalf("Serve error = %v", err)
	}
}

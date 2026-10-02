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
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/telemetry/internal/server"
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

func connection(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err = c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return c
}

func request(t *testing.T, c net.Conn) {
	t.Helper()
	if _, err := io.WriteString(c, "GET /probe?x=1 HTTP/1.1\r\nHost: example\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
}

func result(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return")
		return nil
	}
}

func start(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(context.Context)) <-chan error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln, h, drain, stop) }()
	return done
}

func requireTerminated(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
		t.Errorf("connection did not terminate: %v", err)
	}
}

type closeOrderListener struct {
	net.Listener
	stopReturned atomic.Bool
	closed       atomic.Int32
	early        atomic.Bool
}

func (ln *closeOrderListener) Accept() (net.Conn, error) {
	c, err := ln.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &closeOrderConn{Conn: c, listener: ln}, nil
}

type closeOrderConn struct {
	net.Conn
	listener *closeOrderListener
	once     sync.Once
}

func (c *closeOrderConn) Close() error {
	c.once.Do(func() {
		if !c.listener.stopReturned.Load() {
			c.listener.early.Store(true)
		}
		c.listener.closed.Add(1)
	})
	return c.Conn.Close()
}

// R-UD9W-OZNC R-OZKD-A5FT
func TestServeHTTP(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := start(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.RequestURI() != "/probe?x=1" || r.Proto != "HTTP/1.1" {
			t.Errorf("request = %s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
		}
		w.Header().Set("X-Test", "delivered")
		w.WriteHeader(202)
		_, _ = io.WriteString(w, "handler response")
	}), time.Second, nil)
	c := connection(t, ln)
	request(t, c)
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 202 || resp.Header.Get("X-Test") != "delivered" || string(body) != "handler response" {
		t.Fatalf("response = %v %q", resp, body)
	}
	select {
	case err := <-done:
		t.Fatalf("returned before cancellation: %v", err)
	default:
	}
	cancel()
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
}

// R-P206-1OX7 R-P4FY-T8EL
func TestGracefulDrain(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	stopEntered, stopRelease := make(chan struct{}), make(chan struct{})
	var calls int
	var passedContext context.Context
	done := start(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "complete response")
	}), 500*time.Millisecond, func(stopCtx context.Context) {
		passedContext = stopCtx
		calls++
		if stopCtx.Err() != nil {
			t.Error("stop context canceled before deadline")
		}
		close(stopEntered)
		<-stopRelease
	})
	c := connection(t, ln)
	request(t, c)
	<-entered
	select {
	case <-stopEntered:
		t.Fatal("stop before cancellation")
	default:
	}
	cancel()
	select {
	case <-stopEntered:
		t.Fatal("stop before handler completed")
	default:
	}
	close(release)
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != "complete response" {
		t.Fatalf("response %q, %v", body, err)
	}
	<-stopEntered
	select {
	case <-done:
		t.Fatal("returned before stop returned")
	default:
	}
	close(stopRelease)
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("stop calls = %d", calls)
	}
	if passedContext.Err() != nil {
		t.Fatal("stop context canceled before drain elapsed")
	}
	select {
	case <-passedContext.Done():
	case <-time.After(time.Second):
		t.Fatal("stop context did not reach its deadline")
	}
}

// R-UEHT-2RE1 R-P382-FGNW R-P4FY-T8EL
func TestDrainDeadline(t *testing.T) {
	for _, n := range []int{1, 3} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			ln := &closeOrderListener{Listener: listener(t)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{}, n)
			release := make(chan struct{})
			defer close(release)
			stopEntered, stopRelease := make(chan struct{}), make(chan struct{})
			calls := 0
			done := start(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				entered <- struct{}{}
				<-release
				_, _ = io.WriteString(w, "too late")
			}), 30*time.Millisecond, func(stopCtx context.Context) {
				defer ln.stopReturned.Store(true)
				calls++
				if stopCtx.Err() == nil {
					t.Error("deadline context is not done")
				}
				close(stopEntered)
				<-stopRelease
			})
			clients := make([]net.Conn, n)
			for i := range clients {
				clients[i] = connection(t, ln)
				request(t, clients[i])
				<-entered
			}
			closed := make(chan error, n)
			for _, c := range clients {
				go func() { var b [1]byte; _, err := c.Read(b[:]); closed <- err }()
			}
			cancel()
			<-stopEntered
			close(stopRelease)
			err := result(t, done)
			var drainErr *server.DrainError
			if !errors.As(err, &drainErr) || drainErr.Unfinished != n {
				t.Fatalf("drain result = %v", err)
			}
			for range clients {
				requireTerminated(t, <-closed)
			}
			if ln.early.Load() {
				t.Error("connection Close called before stop returned")
			}
			if count := ln.closed.Load(); int(count) != n {
				t.Errorf("closed %d connections, want %d", count, n)
			}
			if calls != 1 {
				t.Errorf("stop calls = %d", calls)
			}
		})
	}
}

// R-P5NV-705A
func TestDrainErrorText(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 2, 19} {
		want := "stopped with " + strconv.Itoa(n) + " requests unfinished"
		if n == 1 {
			want = "stopped with 1 request unfinished"
		}
		e := &server.DrainError{Unfinished: n}
		if e.Error() != want {
			t.Errorf("%d: %q", n, e.Error())
		}
	}
}

type observedListener struct {
	net.Listener
	accepted chan struct{}
	once     sync.Once
}

func (ln *observedListener) Accept() (net.Conn, error) {
	c, err := ln.Listener.Accept()
	if err == nil {
		ln.once.Do(func() { close(ln.accepted) })
	}
	return c, err
}

// R-P0S9-NX6I
func TestCloseEmptyAndIdleConnections(t *testing.T) {
	for _, idle := range []bool{false, true} {
		t.Run(strconv.FormatBool(idle), func(t *testing.T) {
			ln := &observedListener{Listener: listener(t), accepted: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := start(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), 2*time.Second, nil)
			c := connection(t, ln)
			<-ln.accepted
			if idle {
				request(t, c)
				resp, err := http.ReadResponse(bufio.NewReader(c), nil)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
			}
			cancel()
			if err := result(t, done); err != nil {
				t.Fatal(err)
			}
			var b [1]byte
			_, err := c.Read(b[:])
			requireTerminated(t, err)
			next, err := net.Dial("tcp", ln.Addr().String())
			if err == nil {
				_ = next.Close()
				t.Fatal("listener still accepting")
			}
		})
	}
}

type acceptError struct{ temporary bool }

func (e acceptError) Error() string   { return "injected accept failure" }
func (e acceptError) Timeout() bool   { return false }
func (e acceptError) Temporary() bool { return e.temporary }

type failingListener struct {
	net.Listener
	temporary bool
	once      sync.Once
}

func (ln *failingListener) Accept() (net.Conn, error) {
	first := false
	ln.once.Do(func() { first = true })
	if first {
		return nil, acceptError{temporary: ln.temporary}
	}
	return ln.Listener.Accept()
}

// R-P6VR-KRVZ R-P4FY-T8EL
func TestAcceptFailure(t *testing.T) {
	ln := &failingListener{Listener: listener(t)}
	calls := 0
	err := server.Serve(context.Background(), ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, func(context.Context) { calls++ })
	if err == nil {
		t.Fatal("failure returned nil")
	}
	if calls != 0 {
		t.Fatal("stop called on serve failure")
	}
}

// R-P83N-YJMO
func TestHTTPDiagnosticsDiscarded(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	ln := &failingListener{Listener: listener(t), temporary: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := start(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("injected handler panic") }), time.Second, nil)
	c := connection(t, ln)
	request(t, c)
	var b [1]byte
	if _, err := c.Read(b[:]); err == nil {
		t.Fatal("panic request delivered a response")
	}
	cancel()
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatalf("default logger: %q", logs.String())
	}
}

package server_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/cron/internal/server"
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
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

type closingListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (l *closingListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return err
}

// R-I2JR-OC3E R-I7FD-7F26 R-I8N9-L6SV R-I9V5-YYJK R-ICAY-QI0Y
func TestServeGraceful(t *testing.T) {
	ln := &closingListener{Listener: listener(t), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int32
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer close(finished)
			close(entered)
			<-release
			if r.Context().Err() != nil {
				t.Error("request canceled during graceful drain")
			}
			if r.Method != http.MethodGet || r.Proto != "HTTP/1.1" {
				t.Errorf("request %s %s", r.Method, r.Proto)
			}
			w.Header().Set("X-Test", "response")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, "complete response")
		}), time.Second, func(stopCtx context.Context) {
			select {
			case <-finished:
			default:
				t.Error("stop called before handler ended")
			}
			if stopCtx.Err() != nil {
				t.Error("early stop context canceled")
			}
			calls.Add(1)
		})
	}()
	response := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			response <- err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusAccepted || resp.Header.Get("X-Test") != "response" || resp.Proto != "HTTP/1.1" {
			t.Errorf("response status %d headers %v proto %s", resp.StatusCode, resp.Header, resp.Proto)
		}
		body, _ := io.ReadAll(resp.Body)
		response <- string(body)
	}()
	await(t, entered)
	select {
	case err := <-result:
		t.Fatalf("returned while serving: %v", err)
	default:
	}
	cancel()
	await(t, ln.closed)
	select {
	case <-result:
		t.Fatal("returned during active drain")
	default:
	}
	close(release)
	if body := await(t, response); body != "complete response" {
		t.Fatalf("response %q", body)
	}
	if err := await(t, result); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("stop calls", calls.Load())
	}
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener still accepts")
	}
}

// R-I3RO-23U3 R-IB32-CQA9 R-ICAY-QI0Y
func TestServeDrainCutoff(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	defer close(release)
	stopped := make(chan struct{})
	stopRelease := make(chan struct{})
	var requestCtx context.Context
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			entered <- r.Context()
			<-release
			_, _ = io.WriteString(w, "late")
		}), 100*time.Millisecond, func(stopCtx context.Context) {
			if stopCtx.Err() == nil {
				t.Error("deadline context not done")
			}
			if requestCtx.Err() != nil {
				t.Error("request canceled before stop returned")
			}
			close(stopped)
			<-stopRelease
		})
	}()
	response := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		response <- err
	}()
	requestCtx = await(t, entered)
	cancel()
	await(t, stopped)
	select {
	case <-result:
		t.Fatal("Serve returned while stop blocked")
	default:
	}
	select {
	case <-response:
		t.Fatal("connection closed while stop blocked")
	default:
	}
	close(stopRelease)
	err := await(t, result)
	var drain *server.DrainError
	if !errors.As(err, &drain) || drain.Unfinished != 1 {
		t.Fatalf("drain error %v", err)
	}
	select {
	case <-requestCtx.Done():
	default:
		t.Fatal("request context remains live")
	}
	if err := await(t, response); err == nil {
		t.Fatal("cut-off response completed")
	}
}

// R-IDIV-49RN
func TestDrainErrorText(t *testing.T) {
	for _, tc := range []struct {
		n    int
		text string
	}{{1, "stopped with 1 request unfinished"}, {0, "stopped with 0 requests unfinished"}, {3, "stopped with 3 requests unfinished"}} {
		if got := (&server.DrainError{Unfinished: tc.n}).Error(); got != tc.text {
			t.Fatalf("text %q", got)
		}
	}
}

type failedListener struct{ err error }

func (l failedListener) Accept() (net.Conn, error) { return nil, l.err }
func (failedListener) Close() error                { return nil }
func (failedListener) Addr() net.Addr              { return &net.TCPAddr{} }

// R-IEQR-I1IC R-ICAY-QI0Y
func TestServeFailure(t *testing.T) {
	failure := errors.New("accept failure")
	called := false
	err := server.Serve(context.Background(), failedListener{failure}, http.NotFoundHandler(), time.Second, func(context.Context) { called = true })
	if err == nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("stop called on failure")
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "retry accept" }
func (temporaryError) Temporary() bool { return true }
func (temporaryError) Timeout() bool   { return false }

type retryListener struct {
	net.Listener
	count atomic.Int32
}

func (l *retryListener) Accept() (net.Conn, error) {
	if l.count.Add(1) == 1 {
		return nil, temporaryError{}
	}
	return l.Listener.Accept()
}

// R-IFYN-VT91 R-IEQR-I1IC
func TestServeRetriesAndSilencesPanic(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	ln := &retryListener{Listener: listener(t)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); panic("handler failed") }), time.Second, nil)
	}()
	response := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if resp != nil {
			_ = resp.Body.Close()
		}
		response <- err
	}()
	await(t, entered)
	_ = await(t, response)
	cancel()
	if err := await(t, result); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "" {
		t.Fatalf("logged %q", output.String())
	}
	if ln.count.Load() < 2 {
		t.Fatal("temporary error not retried")
	}
}

type readingConnection struct {
	net.Conn
	reading chan struct{}
	once    sync.Once
}

func (c *readingConnection) Read(p []byte) (int, error) {
	c.once.Do(func() { close(c.reading) })
	return c.Conn.Read(p)
}

type readingListener struct {
	net.Listener
	reading chan struct{}
}

func (l readingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &readingConnection{Conn: conn, reading: l.reading}, nil
}

// R-I8N9-L6SV R-I9V5-YYJK
func TestIdleConnectionsClosed(t *testing.T) {
	ln := readingListener{Listener: listener(t), reading: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- server.Serve(ctx, ln, http.NotFoundHandler(), time.Minute, nil) }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	await(t, ln.reading)
	cancel()
	if err := await(t, result); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	_, err = conn.Read(b[:])
	if err == nil {
		t.Fatal("idle connection remains open")
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		t.Fatal("idle connection timed out")
	}
}

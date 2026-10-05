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
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/server"
)

func listener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}
func await(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(4 * time.Second):
		t.Fatal("Serve did not return")
		return nil
	}
}
func readResponse(t *testing.T, c net.Conn) *http.Response {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(4 * time.Second))
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func request(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err = io.WriteString(c, "GET / HTTP/1.1\r\nHost: backend\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	return c
}

// R-9Y71-W3NG R-9ZEY-9VE5 R-AAE1-PT2E
func TestExports(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 2, 25} {
		suffix := "requests"
		if n == 1 {
			suffix = "request"
		}
		e := &server.DrainError{Unfinished: n}
		if e.Error() != fmt.Sprintf("stopped with %d %s unfinished", n, suffix) {
			t.Fatal(e.Error())
		}
	}
}

// R-A1UR-1EVJ R-A32N-F6M8 R-A6QC-KHUB R-A965-C1BP
func TestCompleteResponseAndIdleDrain(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			w.Header().Set("X-Test", "complete")
			_, _ = io.WriteString(w, "complete answer")
		}), 3*time.Second, func(c context.Context) {
			calls.Add(1)
			if c.Err() != nil {
				t.Error("early stop context done")
			}
		})
	}()
	idle, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idle.Close() }()
	c := request(t, ln)
	<-entered
	select {
	case err := <-done:
		t.Fatalf("returned before cancellation: %v", err)
	default:
	}
	cancel()
	close(release)
	response := readResponse(t, c)
	b, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || response.Proto != "HTTP/1.1" || string(b) != "complete answer" || response.Header.Get("X-Test") != "complete" {
		t.Fatalf("response: %v %q %v", response, b, err)
	}
	if err = await(t, done); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("stop count", calls.Load())
	}
	_ = idle.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = idle.Read(make([]byte, 1)); !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("idle connection was not closed: %v", err)
	}
	if c, err = net.Dial("tcp", ln.Addr().String()); err == nil {
		_ = c.Close()
		t.Fatal("listener remains open")
	}
}

// R-A7Y8-Y9L0 R-A965-C1BP
func TestDeadlineCancelsAfterStop(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan context.Context, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { entered <- r.Context(); <-release }), 40*time.Millisecond, func(c context.Context) {
			calls.Add(1)
			if c.Err() == nil {
				t.Error("deadline context live")
			}
			for i := 0; i < 2; i++ {
				q := <-entered
				if q.Err() != nil {
					t.Error("request cancelled before stop returned")
				}
				entered <- q
			}
		})
	}()
	c1 := request(t, ln)
	c2 := request(t, ln)
	q1 := <-entered
	q2 := <-entered
	entered <- q1
	entered <- q2
	cancel()
	err := await(t, done)
	var drain *server.DrainError
	if !errors.As(err, &drain) || drain.Unfinished != 2 {
		t.Fatalf("drain: %v", err)
	}
	if q1.Err() == nil || q2.Err() == nil {
		t.Fatal("request contexts remain live")
	}
	if calls.Load() != 1 {
		t.Fatal("stop calls", calls.Load())
	}
	for _, c := range []net.Conn{c1, c2} {
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		if _, err = c.Read(make([]byte, 1)); err == nil {
			t.Fatal("connection remains open")
		}
	}
	close(release)
}

// R-A5IG-6Q3M
func TestGoexitEndsRequest(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	exited := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			defer close(exited)
			close(entered)
			<-r.Context().Done()
			runtime.Goexit()
		}), 3*time.Second, nil)
	}()
	c := request(t, ln)
	<-entered
	cancel()
	_ = c.Close()
	<-exited
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("completed Goexit request waited for drain")
	}
}

type temporary struct{}

func (temporary) Error() string   { return "temporary accept" }
func (temporary) Timeout() bool   { return false }
func (temporary) Temporary() bool { return true }

type retryListener struct {
	net.Listener
	first atomic.Bool
}

func (l *retryListener) Accept() (net.Conn, error) {
	if !l.first.Swap(true) {
		return nil, temporary{}
	}
	return l.Listener.Accept()
}

type failedListener struct{ net.Listener }

func (l failedListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }

// R-ABLY-3KT3 R-ACTU-HCJS R-A965-C1BP
func TestAcceptFailureRetryAndSilentPanic(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	ln := listener(t)
	var stops atomic.Int32
	err := server.Serve(context.Background(), failedListener{ln}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, func(context.Context) { stops.Add(1) })
	if err == nil || stops.Load() != 0 {
		t.Fatalf("failure: %v stop %d", err, stops.Load())
	}
	ln = listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	panicked := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, &retryListener{Listener: ln}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(panicked); panic("handler panic") }), time.Second, nil)
	}()
	c := request(t, ln)
	<-panicked
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = c.Read(make([]byte, 1))
	cancel()
	if err = await(t, done); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatal(logs.String())
	}
}

type pipeListener struct {
	conn      net.Conn
	accepted  atomic.Bool
	closed    chan struct{}
	closeOnce sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	if !l.accepted.Swap(true) {
		return l.conn, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *pipeListener) Close() error   { l.closeOnce.Do(func() { close(l.closed) }); return nil }
func (l *pipeListener) Addr() net.Addr { return l.conn.LocalAddr() }

// R-A7Y8-Y9L0
func TestBlockedWriteSeesCancelledContext(t *testing.T) {
	accepted, client := net.Pipe()
	defer func() { _ = client.Close() }()
	ln := &pipeListener{conn: accepted, closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writing := make(chan struct{})
	observed := make(chan bool, 1)
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(writing)
			_, err := w.Write(bytes.Repeat([]byte("x"), 8192))
			observed <- err != nil && r.Context().Err() != nil
		}), 40*time.Millisecond, nil)
	}()
	if _, err := io.WriteString(client, "GET / HTTP/1.1\r\nHost: backend\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	<-writing
	cancel()
	err := await(t, done)
	var drain *server.DrainError
	if !errors.As(err, &drain) || drain.Unfinished != 1 {
		t.Fatal(err)
	}
	select {
	case okay := <-observed:
		if !okay {
			t.Fatal("write did not see cancelled context")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Write did not end")
	}
}

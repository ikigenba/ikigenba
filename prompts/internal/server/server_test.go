package server_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/prompts/internal/server"
)

func wait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(4 * time.Second):
		t.Fatal("operation did not complete")
		var zero T
		return zero
	}
}
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}
func client(t *testing.T) *http.Client {
	t.Helper()
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 4 * time.Second}
}

// R-FDQ1-VESI R-FEXY-96J7 R-FOP5-BCGR
func TestPublicContract(t *testing.T) {

	for _, n := range []int{-1, 0, 1, 2, 99} {
		e := &server.DrainError{Unfinished: n}
		want := "stopped with " + strconv.Itoa(n) + " requests unfinished"
		if n == 1 {
			want = "stopped with 1 request unfinished"
		}
		if e.Error() != want {
			t.Fatalf("%q != %q", e.Error(), want)
		}
	}
}

// R-FHDR-0Q0L R-FILN-EHRA R-FL1G-618O R-FNH8-XKQ2
func TestDrainFinishesResponse(t *testing.T) {
	delivered := make(chan struct{})
	ln := &replyObservedListener{Listener: listen(t), delivered: delivered}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	stops := 0
	handlerReturned := make(chan struct{})
	started := time.Now()
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer close(handlerReturned)
			close(entered)
			<-release
			if r.Context().Err() != nil {
				t.Error("request was cancelled before completing")
			}
			w.Header().Set("X-Probe", "supplied")
			_, _ = io.WriteString(w, "complete reply")
		}), 3*time.Second, func(c context.Context) {
			stops++
			select {
			case <-delivered:
			default:
				t.Error("stop called before complete response was written to connection")
			}
			if ctx.Err() == nil {
				t.Error("stop called while serving context was live")
			}
			select {
			case <-handlerReturned:
			default:
				t.Error("stop called before handler completed")
			}
			if c.Err() != nil {
				t.Error("stop context ended before deadline")
			}
		})
	}()
	response := make(chan string, 1)
	go func() {
		r, e := client(t).Get("http://" + ln.Addr().String() + "/probe?q=value")
		if e != nil {
			response <- e.Error()
			return
		}
		defer func() { _ = r.Body.Close() }()
		b, _ := io.ReadAll(r.Body)
		response <- r.Header.Get("X-Probe") + ":" + string(b)
	}()
	wait(t, entered)
	select {
	case e := <-done:
		t.Fatalf("serve returned early: %v", e)
	default:
	}
	cancel()
	close(release)
	if got := wait(t, response); got != "supplied:complete reply" {
		t.Fatal(got)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("finished requests did not cause prompt return before the three-second drain")
	}
	if time.Since(started) >= 3*time.Second {
		t.Fatal("waited for drain deadline")
	}
	if stops != 1 {
		t.Fatalf("stop calls %d", stops)
	}
	if c, e := net.DialTimeout("tcp", ln.Addr().String(), time.Second); e == nil {
		_ = c.Close()
		t.Fatal("listener still accepts")
	}
}

// R-FM9C-JSZD R-FNH8-XKQ2
func TestCutoffStopsBeforeRequestCancellation(t *testing.T) {
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	ended := make(chan struct{})
	releaseHandler := make(chan struct{})
	handlerReturned := make(chan struct{})
	defer close(releaseHandler)
	stopCalled := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			defer close(handlerReturned)
			close(entered)
			<-r.Context().Done()
			select {
			case <-stopCalled:
			default:
				t.Error("request cancelled before stop")
			}
			close(ended)
			<-releaseHandler
		}), 40*time.Millisecond, func(c context.Context) {
			if c.Err() == nil {
				t.Error("deadline context is still live")
			}
			close(stopCalled)
		})
	}()
	go func() {
		r, e := client(t).Get("http://" + ln.Addr().String() + "/")
		if e == nil {
			_ = r.Body.Close()
		}
	}()
	wait(t, entered)
	cancel()
	e := wait(t, result)
	var drain *server.DrainError
	if !errors.As(e, &drain) || drain.Unfinished != 1 {
		t.Fatalf("cutoff %v", e)
	}
	wait(t, ended)
	select {
	case <-handlerReturned:
		t.Fatal("cut-off handler returned before Serve")
	default:
	}
}

// R-FJTJ-S9HZ
func TestGoexitDoesNotRemainInProgress(t *testing.T) {
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	quit := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { close(entered); <-quit; runtime.Goexit() }), 2*time.Second, nil)
	}()
	go func() {
		r, e := client(t).Get("http://" + ln.Addr().String() + "/")
		if e == nil {
			_ = r.Body.Close()
		}
	}()
	wait(t, entered)
	cancel()
	close(quit)
	if e := wait(t, done); e != nil {
		t.Fatal(e)
	}
}

type failListener struct {
	net.Listener
	mu      sync.Mutex
	failure error
}

func (l *failListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.failure != nil {
		e := l.failure
		l.failure = nil
		l.mu.Unlock()
		return nil, e
	}
	l.mu.Unlock()
	return l.Listener.Accept()
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary probe" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

// R-FR4Y-2VY5
func TestAcceptFailure(t *testing.T) {
	failure := errors.New("accept probe")
	ln := &failListener{Listener: listen(t), failure: failure}
	calls := 0
	e := server.Serve(context.Background(), ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, func(context.Context) { calls++ })
	if !errors.Is(e, failure) || calls != 0 {
		t.Fatalf("error %v stop calls %d", e, calls)
	}
}

// R-FR4Y-2VY5 R-FSCU-GNOU
func TestRetryAndPanicAreSilent(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	ln := &failListener{Listener: listen(t), failure: temporaryError{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	panicked := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(panicked); panic("probe panic") }), time.Second, nil)
	}()
	r, e := client(t).Get("http://" + ln.Addr().String() + "/")
	if e == nil {
		_ = r.Body.Close()
		t.Fatal("panic unexpectedly answered")
	}
	wait(t, panicked)
	cancel()
	if e = wait(t, done); e != nil {
		t.Fatal(e)
	}
	if output.Len() != 0 {
		t.Fatalf("default logger output %q", output.String())
	}
}

type readObservedListener struct {
	net.Listener
	reads chan string
}
type readObservedConn struct {
	net.Conn
	once  sync.Once
	reads chan string
}

func (c *readObservedConn) Read(b []byte) (int, error) {
	c.once.Do(func() { c.reads <- c.RemoteAddr().String() })
	return c.Conn.Read(b)
}
func (l *readObservedListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &readObservedConn{Conn: c, reads: l.reads}, nil
}

// R-FILN-EHRA
func TestDrainClosesIdleConnectionWhileRequestContinues(t *testing.T) {
	ln := &readObservedListener{Listener: listen(t), reads: make(chan string, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			_, _ = io.WriteString(w, "finished")
		}), 3*time.Second, nil)
	}()
	response := make(chan error, 1)
	go func() {
		r, e := client(t).Get("http://" + ln.Addr().String() + "/")
		if e == nil {
			_, e = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}
		response <- e
	}()
	wait(t, entered)
	wait(t, ln.reads)
	idle, e := net.Dial("tcp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = idle.Close() }()
	if peer := wait(t, ln.reads); peer != idle.LocalAddr().String() {
		t.Fatal("idle connection was not accepted", peer)
	}
	cancel()
	if e = idle.SetReadDeadline(time.Now().Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	var b [1]byte
	n, e := idle.Read(b[:])
	if n != 0 || !errors.Is(e, io.EOF) {
		t.Fatalf("idle connection not closed during drain: n=%d error=%v", n, e)
	}
	select {
	case <-done:
		t.Fatal("Serve returned while request remains held")
	default:
	}
	unlock()
	if e = wait(t, response); e != nil {
		t.Fatal(e)
	}
	if e = wait(t, done); e != nil {
		t.Fatal(e)
	}
}

// R-FM9C-JSZD
func TestCutoffCancelsBeforeBackpressuredWriteFails(t *testing.T) {
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writing := make(chan struct{})
	writeFailed := make(chan bool, 1)
	result := make(chan error, 1)
	stopCalled := make(chan struct{})
	go func() {
		result <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(writing)
			chunk := bytes.Repeat([]byte("x"), 65536)
			for {
				if _, e := w.Write(chunk); e != nil {
					writeFailed <- r.Context().Err() != nil
					return
				}
			}
		}), 100*time.Millisecond, func(context.Context) { close(stopCalled) })
	}()
	conn, e := net.Dial("tcp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = conn.Close() }()
	if _, e = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: fixture\r\n\r\n"); e != nil {
		t.Fatal(e)
	}
	wait(t, writing)
	cancel()
	e = wait(t, result)
	var drain *server.DrainError
	if !errors.As(e, &drain) || drain.Unfinished != 1 {
		t.Fatal("cutoff", e)
	}
	if !wait(t, writeFailed) {
		t.Fatal("blocked Write failed before request context was cancelled")
	}
	select {
	case <-stopCalled:
	default:
		t.Fatal("missing stop")
	}
}

type replyObservedListener struct {
	net.Listener
	delivered chan struct{}
}
type replyObservedConn struct {
	net.Conn
	once      sync.Once
	delivered chan struct{}
}

func (l *replyObservedListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &replyObservedConn{Conn: c, delivered: l.delivered}, nil
}
func (c *replyObservedConn) Write(b []byte) (int, error) {
	n, e := c.Conn.Write(b)
	if e == nil && bytes.Contains(b[:n], []byte("complete reply")) {
		c.once.Do(func() { close(c.delivered) })
	}
	return n, e
}

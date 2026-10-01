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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/mcp/internal/server"
)

const testLimit = 3 * time.Second

type trackedListener struct {
	net.Listener
	accepted chan struct{}
	closed   chan struct{}
	once     sync.Once
}

func (l *trackedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted <- struct{}{}
	}
	return c, err
}

func (l *trackedListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return l.Listener.Close()
}

func listener(t *testing.T) *trackedListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return &trackedListener{Listener: ln, accepted: make(chan struct{}, 32), closed: make(chan struct{})}
}

func wait(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(testLimit):
		t.Fatal("event did not arrive")
	}
}

func result(t *testing.T, c <-chan error) error {
	t.Helper()
	select {
	case err := <-c:
		return err
	case <-time.After(testLimit):
		t.Fatal("Serve did not return")
		return nil
	}
}

func launch(t *testing.T, ln net.Listener, h http.Handler, drain time.Duration) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln, h, drain) }()
	return cancel, done
}

func client(t *testing.T) *http.Client {
	t.Helper()
	transport := &http.Transport{DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: testLimit}
}

func get(t *testing.T, c *http.Client, ln net.Listener, path string) *http.Response {
	t.Helper()
	r, err := c.Get("http://" + ln.Addr().String() + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Body.Close() })
	return r
}

// R-VETV-HBKQ
func TestServeExport(t *testing.T) {
	ln := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	useServe := func(serve func(context.Context, net.Listener, http.Handler, time.Duration) error) {
		_ = serve(ctx, ln, http.NotFoundHandler(), time.Second)
	}
	useServe(server.Serve)
}

// R-VG1R-V3BF
func TestDrainErrorExport(_ *testing.T) {
	e := &server.DrainError{Unfinished: 3}
	var err error = e
	_ = strconv.Itoa(e.Unfinished)
	_ = err.Error()
}

// R-VVWG-U3YG
func TestServeHTTPAndBlock(t *testing.T) {
	ln := listener(t)
	cancel, done := launch(t, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Answer", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "answer")
	}), time.Second)
	c := client(t)
	for _, path := range []string{"/one", "/two"} {
		r := get(t, c, ln, path)
		body, err := io.ReadAll(r.Body)
		if err != nil || r.Proto != "HTTP/1.1" || r.StatusCode != http.StatusCreated || r.Header.Get("X-Answer") != path || string(body) != "answer" {
			t.Fatalf("response: %v, %q, %v", r, body, err)
		}
	}
	select {
	case err := <-done:
		t.Fatalf("Serve returned before cancellation: %v", err)
	default:
	}
	cancel()
	_ = result(t, done)
}

// R-VX4D-7VP5
func TestStopClosesIdleAndListener(t *testing.T) {
	ln := listener(t)
	cancel, done := launch(t, ln, http.NotFoundHandler(), time.Second)
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	wait(t, ln.accepted)
	cancel()
	wait(t, ln.closed)
	if err := c.SetReadDeadline(time.Now().Add(testLimit)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := c.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("idle connection not closed: %v", err)
	}
	if next, err := net.DialTimeout("tcp", ln.Addr().String(), testLimit); err == nil {
		_ = next.Close()
		t.Fatal("listener still accepts")
	}
	_ = result(t, done)
}

// R-VYC9-LNFU
func TestDrainCompletesResponsesPromptly(t *testing.T) {
	ln := listener(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	cancel, done := launch(t, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "first")
		_ = http.NewResponseController(w).Flush()
		entered <- struct{}{}
		<-release
		_, _ = io.WriteString(w, strings.Repeat("last", 10000))
	}), 2*time.Second)
	c := client(t)
	responses := []*http.Response{get(t, c, ln, "/a"), get(t, c, ln, "/b")}
	wait(t, entered)
	wait(t, entered)
	cancel()
	wait(t, ln.closed)
	select {
	case err := <-done:
		t.Fatalf("returned with handlers blocked: %v", err)
	default:
	}
	start := time.Now()
	once.Do(func() { close(release) })
	for _, r := range responses {
		b, err := io.ReadAll(r.Body)
		if err != nil || string(b) != "first"+strings.Repeat("last", 10000) {
			t.Fatalf("incomplete response: length=%d error=%v", len(b), err)
		}
	}
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= time.Second {
		t.Fatal("waited for drain instead of completed responses")
	}
}

// R-VZK5-ZF6J
func TestDrainDeadlineCountsAndCutsOff(t *testing.T) {
	for _, n := range []int{1, 2} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			ln := listener(t)
			entered := make(chan struct{}, n)
			release := make(chan struct{})
			defer close(release)
			finished := make(chan struct{}, n)
			cancel, done := launch(t, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "first")
				_ = http.NewResponseController(w).Flush()
				entered <- struct{}{}
				<-release
				_, _ = io.WriteString(w, "last")
				finished <- struct{}{}
			}), 100*time.Millisecond)
			c := client(t)
			responses := make([]*http.Response, n)
			for i := range responses {
				responses[i] = get(t, c, ln, "/held")
				wait(t, entered)
			}
			start := time.Now()
			cancel()
			err := result(t, done)
			var drain *server.DrainError
			if !errors.As(err, &drain) || drain.Unfinished != n {
				t.Fatalf("drain error: %v", err)
			}
			if elapsed := time.Since(start); elapsed < 100*time.Millisecond || elapsed >= time.Second {
				t.Fatalf("deadline elapsed: %v", elapsed)
			}
			select {
			case <-finished:
				t.Fatal("handler finished before drain returned")
			default:
			}
			for _, r := range responses {
				body, err := io.ReadAll(r.Body)
				if err == nil || string(body) != "first" {
					t.Fatalf("response not cut off: %q, %v", body, err)
				}
			}
		})
	}
}

func hijackedResponse(t *testing.T, ln net.Listener) *http.Response {
	t.Helper()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.SetDeadline(time.Now().Add(testLimit)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(c, "GET /held HTTP/1.1\r\nHost: example\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Body.Close() })
	return r
}

func hijackingHandler(t *testing.T, entered chan<- struct{}, release <-chan struct{}) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 9\r\n\r\nfirst")
		_ = rw.Flush()
		entered <- struct{}{}
		<-release
		_, _ = rw.WriteString("last")
		_ = rw.Flush()
	})
}

// R-VYC9-LNFU
func TestDrainCompletesHijackedResponse(t *testing.T) {
	ln := listener(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	cancel, done := launch(t, ln, hijackingHandler(t, entered, release), 2*time.Second)
	r := hijackedResponse(t, ln)
	wait(t, entered)
	cancel()
	wait(t, ln.closed)
	select {
	case err := <-done:
		t.Fatalf("returned while hijacked handler blocked: %v", err)
	default:
	}
	start := time.Now()
	once.Do(func() { close(release) })
	body, err := io.ReadAll(r.Body)
	if err != nil || string(body) != "firstlast" {
		t.Fatalf("incomplete hijacked response: %q, %v", body, err)
	}
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= time.Second {
		t.Fatal("waited for drain instead of completed response")
	}
}

// R-VZK5-ZF6J
func TestDrainCutsOffHijackedResponses(t *testing.T) {
	ln := listener(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	cancel, done := launch(t, ln, hijackingHandler(t, entered, release), 100*time.Millisecond)
	responses := []*http.Response{hijackedResponse(t, ln), hijackedResponse(t, ln)}
	wait(t, entered)
	wait(t, entered)
	start := time.Now()
	cancel()
	err := result(t, done)
	var drain *server.DrainError
	if !errors.As(err, &drain) || drain.Unfinished != 2 {
		t.Fatalf("hijacked drain error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond || elapsed >= time.Second {
		t.Fatalf("hijacked deadline elapsed: %v", elapsed)
	}
	for _, r := range responses {
		body, err := io.ReadAll(r.Body)
		if !errors.Is(err, io.ErrUnexpectedEOF) || string(body) != "first" {
			t.Fatalf("hijacked response not cut off: %q, %v", body, err)
		}
	}
}

// R-W0S2-D6X8
func TestDrainErrorText(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 2, 100} {
		want := "stopped with " + strconv.Itoa(n) + " requests unfinished"
		if n == 1 {
			want = "stopped with 1 request unfinished"
		}
		if got := (&server.DrainError{Unfinished: n}).Error(); got != want {
			t.Fatalf("%d: %q, want %q", n, got, want)
		}
	}
}

type failedListener struct {
	net.Listener
	err error
}

func (l failedListener) Accept() (net.Conn, error) { return nil, l.err }

// R-W1ZY-QYNX
func TestAcceptFailureIsError(t *testing.T) {
	ln := listener(t)
	failure := errors.New("accept failed")
	ctx := context.Background()
	if err := server.Serve(ctx, failedListener{Listener: ln, err: failure}, http.NotFoundHandler(), time.Second); err == nil {
		t.Fatal("accept failure returned nil while context was live")
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary accept failure" }
func (temporaryError) Temporary() bool { return true }
func (temporaryError) Timeout() bool   { return false }

type temporaryListener struct {
	net.Listener
	first bool
}

func (l *temporaryListener) Accept() (net.Conn, error) {
	if !l.first {
		l.first = true
		return nil, temporaryError{}
	}
	return l.Listener.Accept()
}

// R-W37V-4QEM
func TestServeNeverLogsAcceptErrorOrPanic(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	ln := listener(t)
	panicked := make(chan struct{})
	cancel, done := launch(t, &temporaryListener{Listener: ln}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(panicked)
		panic("handler failed")
	}), time.Second)
	response, err := client(t).Get("http://" + ln.Addr().String() + "/panic")
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("panicking handler unexpectedly completed response")
	}
	wait(t, panicked)
	cancel()
	_ = result(t, done)
	if output.Len() != 0 {
		t.Fatalf("default logger received %q", output.String())
	}
}

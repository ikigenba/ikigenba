package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// R-LLO3-0V1R
func TestServeSignature(t *testing.T) {
	t.Parallel()

	want := reflect.TypeOf((func(context.Context, net.Listener, http.Handler, time.Duration) error)(nil))
	if got := reflect.TypeOf(Serve); got != want {
		t.Errorf("Serve type = %v, want %v", got, want)
	}
}

// R-QLRL-RC1B R-LSZH-BHHX R-LU7D-P98M
func TestServeAnswersHTTPAndDrainsAcceptedRequests(t *testing.T) {
	listener := listenLoopback(t)
	tracked := &trackingListener{
		Listener: listener,
		accepted: make(chan struct{}, 3),
		closed:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	requestStarted := make(chan struct{})
	finishResponse := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		_, _ = io.WriteString(w, "first-")
		w.(http.Flusher).Flush()
		<-finishResponse
		_, _ = io.WriteString(w, "last")
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- Serve(ctx, tracked, handler, 2*time.Second) }()

	requestConn := dialListener(t, listener)
	if _, err := io.WriteString(requestConn, "GET /page HTTP/1.1\r\nHost: dummy\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	reader := bufio.NewReader(requestConn)
	responseResult := make(chan *http.Response, 1)
	responseErrors := make(chan error, 1)
	go func() {
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			responseErrors <- err
			return
		}
		responseResult <- response
	}()
	<-requestStarted
	<-tracked.accepted

	select {
	case err := <-serveResult:
		t.Fatalf("Serve returned before cancellation: %v", err)
	default:
	}

	idleConn := dialListener(t, listener)
	<-tracked.accepted
	partialConn := dialListener(t, listener)
	if _, err := io.WriteString(partialConn, "GET /page HTTP/1.1\r\nHost:"); err != nil {
		t.Fatalf("write partial request: %v", err)
	}
	<-tracked.accepted
	cancel()
	<-tracked.closed
	if connection, err := net.Dial("tcp", listener.Addr().String()); err == nil {
		_ = connection.Close()
		t.Error("listener accepted a new connection after cancellation")
	}
	select {
	case err := <-serveResult:
		t.Fatalf("Serve returned before accepted response completed: %v", err)
	default:
	}

	close(finishResponse)
	var response *http.Response
	select {
	case err := <-responseErrors:
		t.Fatalf("read response: %v", err)
	case response = <-responseResult:
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	_ = response.Body.Close()
	_ = requestConn.Close()
	if response.Proto != "HTTP/1.1" || string(body) != "first-last" {
		t.Errorf("response = %s %q, want HTTP/1.1 %q", response.Proto, body, "first-last")
	}
	select {
	case err = <-serveResult:
		if err != nil {
			t.Errorf("Serve after cancellation = %v, want nil", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Serve waited for the drain deadline after the response completed")
	}
	if err = idleConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set idle connection deadline: %v", err)
	}
	buffer := make([]byte, 1)
	var netErr net.Error
	if _, err = idleConn.Read(buffer); err == nil {
		t.Error("idle connection remained open after Serve returned")
	} else if errors.As(err, &netErr) && netErr.Timeout() {
		t.Error("Serve returned before closing the idle connection")
	}
	_ = idleConn.Close()
	if err = partialConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set partial connection deadline: %v", err)
	}
	if _, err = partialConn.Read(buffer); err == nil {
		t.Error("partial request connection remained open after Serve returned")
	} else if errors.As(err, &netErr) && netErr.Timeout() {
		t.Error("Serve returned before closing the partial request connection")
	}
	_ = partialConn.Close()
}

// R-LMVZ-EMSG R-LURX-UT17
func TestDrainErrorShapeAndText(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(DrainError{})
	if typ.Kind() != reflect.Struct || typ.NumField() != 1 {
		t.Fatalf("DrainError type = %v, want struct with one field", typ)
	}
	field := typ.Field(0)
	if field.Name != "Unfinished" || field.Type.Kind() != reflect.Int || field.PkgPath != "" {
		t.Errorf("DrainError field = %+v, want exported Unfinished int", field)
	}
	for _, tc := range []struct {
		count int
		want  string
	}{
		{0, "stopped with 0 requests unfinished"},
		{1, "stopped with 1 request unfinished"},
		{2, "stopped with 2 requests unfinished"},
		{-1, "stopped with -1 requests unfinished"},
	} {
		t.Run(strconv.Itoa(tc.count), func(t *testing.T) {
			var err error = &DrainError{Unfinished: tc.count}
			if got := err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// R-LVFA-30ZB
func TestServeClosesUnfinishedRequestsAtDrainDeadline(t *testing.T) {
	listener := listenLoopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "first-")
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-release
		_, _ = io.WriteString(w, "last")
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- Serve(ctx, listener, handler, 20*time.Millisecond) }()

	connections := make([]net.Conn, 2)
	for i := range connections {
		connections[i] = dialListener(t, listener)
		defer func(conn net.Conn) { _ = conn.Close() }(connections[i])
		if _, err := io.WriteString(connections[i], "GET /page HTTP/1.1\r\nHost: dummy\r\nConnection: close\r\n\r\n"); err != nil {
			t.Fatalf("write request %d: %v", i, err)
		}
	}
	for range connections {
		<-started
	}
	cancel()
	select {
	case err := <-serveResult:
		var drainErr *DrainError
		if !errors.As(err, &drainErr) || drainErr.Unfinished != 2 {
			t.Fatalf("Serve error = %v, want 2 unfinished requests", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve waited for blocked handlers after drain elapsed")
	}
	for i, connection := range connections {
		if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("set response deadline %d: %v", i, err)
		}
		body, err := io.ReadAll(connection)
		if err != nil {
			t.Fatalf("read response %d: %v", i, err)
		}
		if !strings.Contains(string(body), "first-") || strings.Contains(string(body), "last") {
			t.Errorf("response %d = %q, want prefix only", i, body)
		}
	}
}

// R-LU7D-P98M R-LVFA-30ZB
func TestServeDrainsHijackedHandler(t *testing.T) {
	listener := listenLoopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.WriteString(buffered, "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nfirst-")
		_ = buffered.Flush()
		close(started)
		<-release
		_, _ = io.WriteString(buffered, "last")
		_ = buffered.Flush()
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- Serve(ctx, listener, handler, 20*time.Millisecond) }()
	connection := dialListener(t, listener)
	defer func() { _ = connection.Close() }()
	if _, err := io.WriteString(connection, "GET /page HTTP/1.1\r\nHost: dummy\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	<-started
	cancel()
	select {
	case err := <-serveResult:
		var drainErr *DrainError
		if !errors.As(err, &drainErr) || drainErr.Unfinished != 1 {
			t.Fatalf("Serve error = %v, want one unfinished hijacked handler", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve waited for hijacked handler after drain elapsed")
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set response deadline: %v", err)
	}
	response, err := io.ReadAll(connection)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !strings.Contains(string(response), "first-") || strings.Contains(string(response), "last") {
		t.Errorf("response = %q, want prefix only", response)
	}
}

// R-LU7D-P98M
func TestServeDeliversFinishedResponseAfterDrainCutoff(t *testing.T) {
	listener := listenLoopback(t)
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	wrapped := &oneConnListener{
		Listener: listener,
		wrap: func(conn net.Conn) net.Conn {
			return &blockingWriteConn{Conn: conn, started: writeStarted, release: releaseWrite}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	finish := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-finish
		_, _ = io.WriteString(w, "complete response")
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- Serve(ctx, wrapped, handler, 20*time.Millisecond) }()
	connection := dialListener(t, listener)
	defer func() { _ = connection.Close() }()
	if _, err := io.WriteString(connection, "GET /page HTTP/1.1\r\nHost: dummy\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	<-started
	cancel()
	close(finish)
	<-writeStarted
	select {
	case err := <-serveResult:
		t.Fatalf("Serve returned before finished response was delivered: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	close(releaseWrite)
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set response deadline: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	_ = response.Body.Close()
	if string(body) != "complete response" {
		t.Errorf("response body = %q, want complete response", body)
	}
	select {
	case err := <-serveResult:
		if err != nil {
			t.Errorf("Serve after complete response = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after complete response")
	}
}

// R-LVFA-30ZB
func TestServeRejectsWriteAfterDrainCutoff(t *testing.T) {
	listener := listenLoopback(t)
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	wrapped := &oneConnListener{
		Listener: listener,
		wrap: func(conn net.Conn) net.Conn {
			return &blockingCloseConn{Conn: conn, started: closeStarted, release: releaseClose}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	releaseHandler := make(chan struct{})
	writeResult := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			writeResult <- err
			return
		}
		_, _ = io.WriteString(buffered, "HTTP/1.1 200 OK\r\n\r\nfirst-")
		_ = buffered.Flush()
		close(started)
		<-releaseHandler
		_, _ = io.WriteString(buffered, "last")
		writeResult <- buffered.Flush()
		_ = conn.Close()
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- Serve(ctx, wrapped, handler, 20*time.Millisecond) }()
	connection := dialListener(t, listener)
	defer func() { _ = connection.Close() }()
	if _, err := io.WriteString(connection, "GET /page HTTP/1.1\r\nHost: dummy\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	<-started
	cancel()
	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		t.Fatal("drain cutoff did not start closing the active connection")
	}
	close(releaseHandler)
	if err := <-writeResult; !errors.Is(err, net.ErrClosed) {
		t.Errorf("write after cutoff = %v, want closed connection", err)
	}
	close(releaseClose)
	select {
	case err := <-serveResult:
		var drainErr *DrainError
		if !errors.As(err, &drainErr) || drainErr.Unfinished != 1 {
			t.Fatalf("Serve error = %v, want one unfinished request at cutoff", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve waited after drain connection closed")
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set response deadline: %v", err)
	}
	response, err := io.ReadAll(connection)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !strings.Contains(string(response), "first-") || strings.Contains(string(response), "last") {
		t.Errorf("response = %q, want prefix only", response)
	}
}

// R-LVFA-30ZB
func TestServeReturnsWhileHandlerWriteIsBlocked(t *testing.T) {
	listener := listenLoopback(t)
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	defer close(releaseWrite)
	wrapped := &oneConnListener{
		Listener: listener,
		wrap: func(conn net.Conn) net.Conn {
			return &blockingWriteConn{Conn: conn, started: writeStarted, release: releaseWrite}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "response")
		w.(http.Flusher).Flush()
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- Serve(ctx, wrapped, handler, 20*time.Millisecond) }()
	connection := dialListener(t, listener)
	defer func() { _ = connection.Close() }()
	if _, err := io.WriteString(connection, "GET /page HTTP/1.1\r\nHost: dummy\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	<-writeStarted
	cancel()
	select {
	case err := <-serveResult:
		var drainErr *DrainError
		if !errors.As(err, &drainErr) || drainErr.Unfinished != 1 {
			t.Fatalf("Serve error = %v, want one unfinished blocked writer", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve waited for a handler blocked in a connection write")
	}
}

// R-LU7D-P98M R-LVFA-30ZB
func TestServePreservesFinishedResponseWhenAnotherRequestOverruns(t *testing.T) {
	listener := listenLoopback(t)
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	accepted := 0
	wrapped := &oneConnListener{
		Listener: listener,
		wrap: func(conn net.Conn) net.Conn {
			accepted++
			if accepted == 2 {
				return &blockingWriteConn{Conn: conn, started: writeStarted, release: releaseWrite}
			}
			return conn
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	defer close(releaseSlow)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			_, _ = io.WriteString(w, "first-")
			w.(http.Flusher).Flush()
			close(slowStarted)
			<-releaseSlow
			_, _ = io.WriteString(w, "last")
			return
		}
		_, _ = io.WriteString(w, "complete response")
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- Serve(ctx, wrapped, handler, 20*time.Millisecond) }()
	slowConn := dialListener(t, listener)
	defer func() { _ = slowConn.Close() }()
	if _, err := io.WriteString(slowConn, "GET /slow HTTP/1.1\r\nHost: dummy\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write slow request: %v", err)
	}
	<-slowStarted
	fastConn := dialListener(t, listener)
	defer func() { _ = fastConn.Close() }()
	if _, err := io.WriteString(fastConn, "GET /fast HTTP/1.1\r\nHost: dummy\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write fast request: %v", err)
	}
	<-writeStarted
	cancel()
	select {
	case err := <-serveResult:
		t.Fatalf("Serve returned before finished response was delivered: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	close(releaseWrite)
	if err := fastConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set fast response deadline: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(fastConn), nil)
	if err != nil {
		t.Fatalf("read fast response: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read fast body: %v", err)
	}
	_ = response.Body.Close()
	if string(body) != "complete response" {
		t.Errorf("fast body = %q, want complete response", body)
	}
	select {
	case err := <-serveResult:
		var drainErr *DrainError
		if !errors.As(err, &drainErr) || drainErr.Unfinished != 1 {
			t.Fatalf("Serve error = %v, want one unfinished slow request", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after finished response was delivered")
	}
}

// R-LVFA-30ZB
func TestServeStopsWriteThatPassedGateBeforeCutoff(t *testing.T) {
	listener := listenLoopback(t)
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	writeResult := make(chan error, 1)
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	wrapped := &oneConnListener{
		Listener: listener,
		wrap: func(conn net.Conn) net.Conn {
			paused := &pausingWriteConn{
				Conn: conn, started: writeStarted, release: releaseWrite, result: writeResult,
			}
			return &blockingCloseConn{Conn: paused, started: closeStarted, release: releaseClose}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "response that must be cut off")
		w.(http.Flusher).Flush()
	})
	serveResult := make(chan error, 1)
	go func() { serveResult <- Serve(ctx, wrapped, handler, 20*time.Millisecond) }()
	connection := dialListener(t, listener)
	defer func() { _ = connection.Close() }()
	if _, err := io.WriteString(connection, "GET /page HTTP/1.1\r\nHost: dummy\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	<-writeStarted
	cancel()
	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		t.Fatal("drain cutoff did not start closing connection")
	}
	close(releaseWrite)
	if err := <-writeResult; err == nil {
		t.Error("write that began before cutoff succeeded after its write deadline")
	}
	close(releaseClose)
	select {
	case err := <-serveResult:
		var drainErr *DrainError
		if !errors.As(err, &drainErr) || drainErr.Unfinished != 1 {
			t.Fatalf("Serve error = %v, want one unfinished request", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve waited after cutting off blocked write")
	}
}

// R-QO7E-IVIP
func TestServeReturnsServingFailureWhileContextIsLive(t *testing.T) {
	want := errors.New("accept failed")
	err := Serve(context.Background(), &errorListener{err: want}, http.NotFoundHandler(), time.Second)
	if !errors.Is(err, want) {
		t.Errorf("Serve error = %v, want %v", err, want)
	}
}

// R-IBR4-T2PU
func TestServeDiscardsHTTPServerDiagnostics(t *testing.T) {
	listener := listenLoopback(t)
	transient := &temporaryFirstListener{Listener: listener}
	ctx, cancel := context.WithCancel(context.Background())
	var defaultLog bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&defaultLog)
	t.Cleanup(func() { log.SetOutput(originalOutput) })
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- Serve(ctx, transient, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("handler failure")
		}), time.Second)
	}()

	connection := dialListener(t, listener)
	if _, err := io.WriteString(connection, "GET / HTTP/1.1\r\nHost: dummy\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set connection deadline: %v", err)
	}
	_, _ = io.Copy(io.Discard, connection)
	_ = connection.Close()
	cancel()
	if err := <-serveResult; err != nil {
		t.Fatalf("Serve after cancellation = %v, want nil", err)
	}
	if defaultLog.Len() != 0 {
		t.Errorf("default log = %q, want empty", defaultLog.String())
	}
}

func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func dialListener(t *testing.T, listener net.Listener) net.Conn {
	t.Helper()
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	return connection
}

type trackingListener struct {
	net.Listener
	accepted  chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

type oneConnListener struct {
	net.Listener
	wrap func(net.Conn) net.Conn
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.wrap(conn), nil
}

type blockingWriteConn struct {
	net.Conn
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type pausingWriteConn struct {
	net.Conn
	started chan struct{}
	release chan struct{}
	result  chan error
	once    sync.Once
}

func (c *pausingWriteConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	n, err := c.Conn.Write(p)
	c.result <- err
	return n, err
}

func (c *blockingWriteConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return c.Conn.Write(p)
}

type blockingCloseConn struct {
	net.Conn
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingCloseConn) Close() error {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return c.Conn.Close()
}

func (l *trackingListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err == nil {
		l.accepted <- struct{}{}
	}
	return connection, err
}

func (l *trackingListener) Close() error {
	err := l.Listener.Close()
	l.closeOnce.Do(func() { close(l.closed) })
	return err
}

type errorListener struct {
	err error
}

func (l *errorListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *errorListener) Close() error              { return nil }
func (l *errorListener) Addr() net.Addr            { return testAddress("127.0.0.1:1") }

type temporaryFirstListener struct {
	net.Listener
	first bool
}

func (l *temporaryFirstListener) Accept() (net.Conn, error) {
	if !l.first {
		l.first = true
		return nil, temporaryError{}
	}
	return l.Listener.Accept()
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary accept failure" }
func (temporaryError) Temporary() bool { return true }
func (temporaryError) Timeout() bool   { return false }

type testAddress string

func (a testAddress) Network() string { return "tcp" }
func (a testAddress) String() string  { return string(a) }

var _ net.Error = temporaryError{}

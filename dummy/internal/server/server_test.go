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
	"testing"
	"time"
)

// R-B16E-1SRK
func TestServeSignature(t *testing.T) {
	t.Parallel()

	want := reflect.TypeOf((func(context.Context, net.Listener, http.Handler) error)(nil))
	if got := reflect.TypeOf(Serve); got != want {
		t.Errorf("Serve type = %v, want %v", got, want)
	}
}

// R-QLRL-RC1B R-QMZI-53S0
func TestServeAnswersHTTPAndDrainsAcceptedRequests(t *testing.T) {
	listener := listenLoopback(t)
	tracked := &trackingListener{Listener: listener, closed: make(chan struct{})}
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
	go func() { serveResult <- Serve(ctx, tracked, handler) }()

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

	select {
	case err := <-serveResult:
		t.Fatalf("Serve returned before cancellation: %v", err)
	default:
	}

	idleConn := dialListener(t, listener)
	cancel()
	<-tracked.closed
	if connection, err := net.Dial("tcp", listener.Addr().String()); err == nil {
		_ = connection.Close()
		t.Error("listener accepted a new connection after cancellation")
	}
	if err := idleConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set idle connection deadline: %v", err)
	}
	buffer := make([]byte, 1)
	if _, err := idleConn.Read(buffer); err == nil {
		t.Error("idle connection remained open after cancellation")
	}
	_ = idleConn.Close()
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
	if err = <-serveResult; err != nil {
		t.Errorf("Serve after cancellation = %v, want nil", err)
	}
}

// R-QO7E-IVIP
func TestServeReturnsServingFailureWhileContextIsLive(t *testing.T) {
	want := errors.New("accept failed")
	err := Serve(context.Background(), &errorListener{err: want}, http.NotFoundHandler())
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
		}))
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
	closed chan struct{}
}

func (l *trackingListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return l.Listener.Close()
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

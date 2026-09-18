package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// R-YDK2-FEXT
func TestHandlerSignature(t *testing.T) {
	t.Parallel()

	want := reflect.TypeOf((func() http.Handler)(nil))
	if got := reflect.TypeOf(Handler); got != want {
		t.Errorf("Handler type = %v, want %v", got, want)
	}
}

// R-YERY-T6OI R-YFZV-6YF7 R-YPR2-94CR
func TestPageConstants(t *testing.T) {
	t.Parallel()

	if IndexText != "Hello from Dummy!" {
		t.Errorf("IndexText = %q, want %q", IndexText, "Hello from Dummy!")
	}
	wantBodies := map[string]string{
		"NotFoundBody":         NotFoundBody,
		"MethodNotAllowedBody": MethodNotAllowedBody,
	}
	for name, body := range wantBodies {
		if strings.Count(body, "\n") != 1 || !strings.HasSuffix(body, "\n") {
			t.Errorf("%s = %q, want exactly one line", name, body)
		}
	}
	if NotFoundBody != "not found\n" {
		t.Errorf("NotFoundBody = %q, want %q", NotFoundBody, "not found\n")
	}
	if MethodNotAllowedBody != "method not allowed\n" {
		t.Errorf("MethodNotAllowedBody = %q, want %q", MethodNotAllowedBody, "method not allowed\n")
	}
}

// R-YH7R-KQ5W R-TVL2-8JGJ
func TestHandlerReturnsIndex(t *testing.T) {
	t.Parallel()

	response := requestHandler(http.MethodGet, "http://dummy/?ignored=yes")
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/html; charset=utf-8")
	}
	if got := visibleBodyText(t, response.Body.String()); got != IndexText {
		t.Errorf("visible text = %q, want %q", got, IndexText)
	}
}

// R-YJNK-C9NA
func TestHandlerReturnsHeadersOnlyForHead(t *testing.T) {
	t.Parallel()

	response := requestHandler(http.MethodHead, "http://dummy/")
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/html; charset=utf-8")
	}
	if response.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", response.Body.String())
	}
}

// R-YKVG-Q1DZ
func TestHandlerReturnsNotFoundBeforeCheckingMethod(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead} {
		response := requestHandler(method, "http://dummy/nope?ignored=yes")
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want %d", method, response.Code, http.StatusNotFound)
		}
		if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("%s Content-Type = %q, want %q", method, got, "text/plain; charset=utf-8")
		}
		wantBody := NotFoundBody
		if method == http.MethodHead {
			wantBody = ""
		}
		if got := response.Body.String(); got != wantBody {
			t.Errorf("%s body = %q, want %q", method, got, wantBody)
		}
	}
}

// R-YM3D-3T4O
func TestHandlerRejectsUnsupportedIndexMethod(t *testing.T) {
	t.Parallel()

	response := requestHandler(http.MethodPost, "http://dummy/")
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
	}
	if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/plain; charset=utf-8")
	}
	if got := response.Body.String(); got != MethodNotAllowedBody {
		t.Errorf("body = %q, want %q", got, MethodNotAllowedBody)
	}
}

// R-TT59-GZZ5
func TestHandlerDependsOnlyOnMethodAndPath(t *testing.T) {
	t.Parallel()

	firstRequest := httptest.NewRequest(http.MethodGet, "http://first.example/?one=1", nil)
	firstRequest.Header.Set("X-Request", "first")
	secondRequest := httptest.NewRequest(http.MethodGet, "http://second.example/?two=2", strings.NewReader("ignored"))
	secondRequest.Header.Set("X-Request", "second")

	first := httptest.NewRecorder()
	second := httptest.NewRecorder()
	handler := Handler()
	handler.ServeHTTP(first, firstRequest)
	handler.ServeHTTP(second, secondRequest)

	if first.Code != second.Code || !reflect.DeepEqual(first.Header(), second.Header()) || first.Body.String() != second.Body.String() {
		t.Errorf("responses differ: first=(%d, %v, %q), second=(%d, %v, %q)",
			first.Code, first.Header(), first.Body.String(), second.Code, second.Header(), second.Body.String())
	}
}

// R-N5Q6-KDJ6
func TestServerPackageDeclaresNoPackageVariables(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	files := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(files, entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), parseErr)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if ok && general.Tok == token.VAR {
				t.Errorf("%s declares a package-level var at %s", entry.Name(), files.Position(general.Pos()))
			}
		}
	}
}

func requestHandler(method, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	return response
}

func visibleBodyText(t *testing.T, document string) string {
	t.Helper()

	doctype := regexp.MustCompile(`(?i)^\s*<!doctype html>`)
	bodyStart := regexp.MustCompile(`(?i)<body(?:\s[^>]*)?>`)
	bodyEnd := regexp.MustCompile(`(?i)</body>`)
	if !doctype.MatchString(document) {
		t.Errorf("document does not begin with an HTML doctype: %q", document)
	}
	starts := bodyStart.FindAllStringIndex(document, -1)
	ends := bodyEnd.FindAllStringIndex(document, -1)
	if len(starts) != 1 || len(ends) != 1 || starts[0][1] > ends[0][0] {
		t.Fatalf("body tags = %d start, %d end, want one ordered pair", len(starts), len(ends))
	}
	withoutTags := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(document[starts[0][1]:ends[0][0]], "")
	return strings.Join(strings.Fields(withoutTags), " ")
}

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
	tracked := &trackingListener{
		Listener: listener,
		accepted: make(chan struct{}, 2),
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
	<-tracked.accepted

	select {
	case err := <-serveResult:
		t.Fatalf("Serve returned before cancellation: %v", err)
	default:
	}

	idleConn := dialListener(t, listener)
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
	if err = <-serveResult; err != nil {
		t.Errorf("Serve after cancellation = %v, want nil", err)
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
	accepted chan struct{}
	closed   chan struct{}
}

func (l *trackingListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err == nil {
		l.accepted <- struct{}{}
	}
	return connection, err
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

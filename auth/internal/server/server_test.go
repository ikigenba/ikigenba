package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/server/assets"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func TestConfigAndNew(t *testing.T) {
	// R-KTXG-XSI4: Config is exactly the process dependencies handlers need.
	configType := reflect.TypeFor[Config]()
	wantFields := []struct {
		name string
		typ  reflect.Type
	}{
		{name: "Store", typ: reflect.TypeFor[*store.Store]()},
		{name: "Google", typ: reflect.TypeFor[*google.Client]()},
		{name: "Now", typ: reflect.TypeFor[func() time.Time]()},
		{name: "Rand", typ: reflect.TypeFor[io.Reader]()},
		{name: "Stderr", typ: reflect.TypeFor[io.Writer]()},
		{name: "WorkspaceDomain", typ: reflect.TypeFor[string]()},
	}
	if configType.NumField() != len(wantFields) {
		t.Fatalf("Config has %d fields, want %d", configType.NumField(), len(wantFields))
	}
	for i, want := range wantFields {
		field := configType.Field(i)
		if field.Name != want.name || field.Type != want.typ {
			t.Fatalf("Config field %d = %s %s, want %s %s", i, field.Name, field.Type, want.name, want.typ)
		}
	}

	// R-KWD9-PBZI: New is func(Config) *Server and the result's type is the exported Server.
	fn := reflect.TypeOf(New)
	wantFn := reflect.TypeOf(func(Config) *Server { return nil })
	if fn != wantFn {
		t.Fatalf("New has type %s, want %s", fn, wantFn)
	}
	serverType := reflect.TypeFor[*Server]().Elem()
	if serverType.Name() != "Server" || serverType.PkgPath() != "github.com/ikigenba/ikigenba/auth/internal/server" {
		t.Fatalf("Server type = %s pkg=%s", serverType.Name(), serverType.PkgPath())
	}

	constructor := pinNew(New)
	now := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	gc := google.NewClient("client", "secret", "space.example", "http://127.0.0.1:1")
	var stderr bytes.Buffer
	cfg := Config{
		Store:           nil,
		Google:          gc,
		Now:             now,
		Rand:            bytes.NewReader(nil),
		Stderr:          &stderr,
		WorkspaceDomain: "space.example",
	}
	s := pinServer(constructor(cfg))
	if s == nil || s.httpServer == nil || s.httpServer.Handler == nil {
		t.Fatal("New did not initialize the HTTP server and router")
	}
	if s.now() != now() || s.gc != gc || s.rand != cfg.Rand || s.stderr != cfg.Stderr || s.cfg.WorkspaceDomain != "space.example" {
		t.Fatal("New did not retain its injected handler dependencies")
	}
}

func pinNew(newFn func(Config) *Server) func(Config) *Server { return newFn }

func pinServer(s *Server) *Server { return s }

func TestServeSignatureAndHandler(t *testing.T) {
	// R-M22I-KPZ1: Serve exposes the listener, handler, context and drain seam.
	want := reflect.TypeOf(func(context.Context, net.Listener, http.Handler, time.Duration) error { return nil })
	if got := reflect.TypeOf(Serve); got != want {
		t.Fatalf("Serve type = %s, want %s", got, want)
	}
	// R-M4IB-C9GF: *Server itself handles requests through its router.
	var handler http.Handler = New(Config{Now: fixedNow})
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "Sign in with Google") {
		t.Fatalf("ServeHTTP returned %d %q", r.Code, r.Body.String())
	}
}

func TestServeRequestsAndStop(t *testing.T) {
	// R-MALT-945W: Serve answers HTTP/1.1 on the supplied listener and stays up.
	// R-MBTP-MVWL: cancellation closes the listener and idle connections.
	ln := &observedListener{Listener: loopbackListener(t), accepted: make(chan struct{}, 2)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "served") })
	go func() { done <- Serve(ctx, ln, h, time.Second) }()
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	<-ln.accepted
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.Proto != "HTTP/1.1" || string(body) != "served" {
		t.Fatalf("response proto=%s body=%q err=%v", resp.Proto, body, err)
	}
	select {
	case err := <-done:
		t.Fatalf("Serve returned before cancellation: %v", err)
	default:
	}
	idle, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idle.Close() }()
	// The socket has reached Serve's Accept, so closing it tests shutdown
	// of a connection carrying no request rather than a queued TCP dial.
	<-ln.accepted
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v", err)
	}
	if conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", ln.Addr().String()); err == nil {
		_ = conn.Close()
		t.Fatal("listener still accepts after cancellation")
	}
	_ = idle.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if n, err := idle.Read(one[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("idle connection read = %d, %v, want EOF", n, err)
	}
}

func TestServeDrainsAndOverruns(t *testing.T) {
	// R-MD1M-0NNA: a running handler completes and Serve returns promptly.
	ln := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			_, _ = io.WriteString(w, "complete")
		}), 2*time.Second)
	}()
	response := make(chan string, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String(), nil)
		if err != nil {
			response <- err.Error()
			return
		}
		r, err := client.Do(req)
		if err != nil {
			response <- err.Error()
			return
		}
		defer func() { _ = r.Body.Close() }()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			response <- err.Error()
			return
		}
		response <- string(body)
	}()
	<-started
	cancel()
	close(release)
	if got := <-response; got != "complete" {
		t.Fatalf("drained response = %q", got)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("drained Serve returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve waited toward the 2s drain deadline after response delivery")
	}

	// R-ME9I-EFDZ: the deadline counts active handlers, closes their sockets,
	// and returns without waiting for those handlers to finish.
	ln = loopbackListener(t)
	ctx, cancel = context.WithCancel(context.Background())
	entered := make(chan struct{}, 2)
	unblock := make(chan struct{})
	defer close(unblock)
	done = make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			entered <- struct{}{}
			<-unblock
			_, _ = io.WriteString(w, "too late")
		}), 10*time.Millisecond)
	}()
	conns := make([]net.Conn, 2)
	for i := range conns {
		var err error
		conns[i], err = (&net.Dialer{}).DialContext(context.Background(), "tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func(conn net.Conn) { _ = conn.Close() }(conns[i])
		if _, err := io.WriteString(conns[i], "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
	}
	<-entered
	<-entered
	cancel()
	var drainErr *DrainError
	if err := <-done; !errors.As(err, &drainErr) || drainErr.Unfinished != 2 {
		t.Fatalf("overrun error = %v, want two unfinished", err)
	}
	for _, conn := range conns {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		var b [1]byte
		if n, err := conn.Read(b[:]); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("overrun connection read = %d, %v, want EOF", n, err)
		}
	}
}

func TestDrainErrorText(t *testing.T) {
	// R-M3AE-YHPQ: DrainError exposes the exact unfinished count.
	// R-MFHE-S74O: singular and every other count use the specified text.
	for _, tc := range []struct {
		n    int
		want string
	}{
		{1, "stopped with 1 request unfinished"},
		{0, "stopped with 0 requests unfinished"},
		{3, "stopped with 3 requests unfinished"},
	} {
		if got := (&DrainError{Unfinished: tc.n}).Error(); got != tc.want {
			t.Fatalf("count %d: %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestServeFailureAndSilence(t *testing.T) {
	// R-MGPB-5YVD: a listener failure before cancellation is an error.
	ln := loopbackListener(t)
	_ = ln.Close()
	if err := Serve(context.Background(), ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second); err == nil {
		t.Fatal("Serve returned nil for a failed listener")
	}

	// R-MJ53-XICR: net/http's temporary Accept and panic diagnostics stay silent.
	acceptFailed := make(chan struct{})
	ln = &temporaryErrorListener{Listener: loopbackListener(t), failedSignal: acceptFailed}
	logger := log.Default()
	old := logger.Writer()
	var output bytes.Buffer
	logger.SetOutput(&output)
	defer logger.SetOutput(old)
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		t.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWrite, stderrWrite
	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		_ = stderrRead.Close()
		_ = stderrWrite.Close()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	panicked := make(chan struct{})
	go func() {
		done <- Serve(ctx, ln, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(panicked)
			panic("expected panic")
		}), time.Second)
	}()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	select {
	case <-acceptFailed:
	case <-time.After(time.Second):
		t.Fatal("temporary Accept error was not exercised")
	}
	select {
	case <-panicked:
	case <-time.After(time.Second):
		t.Fatal("panic handler was not exercised")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("default logger received %q", output.String())
	}
	os.Stdout, os.Stderr = oldStdout, oldStderr
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	stdoutBytes, stdoutErr := io.ReadAll(stdoutRead)
	stderrBytes, stderrErr := io.ReadAll(stderrRead)
	if stdoutErr != nil || stderrErr != nil || len(stdoutBytes) != 0 || len(stderrBytes) != 0 {
		t.Fatalf("process streams stdout=%q stderr=%q read errors=%v, %v", stdoutBytes, stderrBytes, stdoutErr, stderrErr)
	}
}

func TestRouterRegistersContractRoutesAndEmbeddedAssets(t *testing.T) {
	s := New(Config{Now: fixedNow})
	for _, target := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/"},
		{method: http.MethodPost, path: "/login/google"},
		{method: http.MethodPost, path: "/login/google/callback"},
		{method: http.MethodGet, path: "/logout"},
		{method: http.MethodPost, path: "/check"},
		{method: http.MethodPost, path: "/me"},
		{method: http.MethodGet, path: "/tokens"},
		{method: http.MethodGet, path: "/tokens/token-id/enable"},
	} {
		t.Run(target.method+" "+target.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			s.httpServer.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), target.method, target.path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
		})
	}
	missing := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(missing, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/not-a-route", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing route status = %d, want %d", missing.Code, http.StatusNotFound)
	}

	// Serving the files from this package directory would still succeed if the
	// handler read them off disk. A temp working directory has none of them.
	t.Chdir(t.TempDir())

	for _, asset := range []string{"/assets/index.html", "/assets/app.js", "/assets/style.css"} {
		t.Run(asset, func(t *testing.T) {
			response := httptest.NewRecorder()
			s.httpServer.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, asset, nil))
			if response.Code != http.StatusOK || response.Body.Len() == 0 {
				t.Fatalf("asset response = status %d, %d bytes", response.Code, response.Body.Len())
			}
		})
	}

	// HTML, JavaScript, and CSS bytes are the embedded filesystem's, still
	// served when the process working directory has no asset files.
	embedded := map[string]string{}
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		body, err := assets.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		embedded[name] = string(body)
	}
	wantType := map[string]string{
		"index.html": "text/html; charset=utf-8",
		"app.js":     "text/javascript; charset=utf-8",
		"style.css":  "text/css; charset=utf-8",
	}
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		response := httptest.NewRecorder()
		s.httpServer.Handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/assets/"+name, nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != wantType[name] || response.Body.String() != embedded[name] {
			t.Fatalf("served %s status=%d type=%q (%d bytes), want 200 %s and the embedded bytes", name, response.Code, response.Header().Get("Content-Type"), response.Body.Len(), wantType[name])
		}
		if _, err := os.ReadFile(filepath.Clean(filepath.Join("assets", name))); err == nil {
			t.Fatalf("asset %s is readable from the working directory; the serve above would not prove the embed", name)
		}
	}
}

// R-IVLV-52P1: each D05, D06, and D07 method and path is this server's route.
func TestContractRoutesServed(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"), bytes.NewReader(bytes.Repeat([]byte{4}, 64)))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer.URL,
			"authorization_endpoint":                issuer.URL + "/authorize",
			"token_endpoint":                        issuer.URL + "/token",
			"jwks_uri":                              issuer.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(issuer.Close)

	s := New(Config{
		Store:           st,
		Google:          google.NewClient("client-id", "client-secret", "example.test", issuer.URL),
		Now:             fixedNow,
		Rand:            bytes.NewReader(bytes.Repeat([]byte{5}, 64)),
		Stderr:          &bytes.Buffer{},
		WorkspaceDomain: "example.test",
	})

	root := serveRoute(s, http.MethodGet, "/", nil)
	if root.Code != http.StatusOK || root.Header().Get("Content-Type") != "text/html; charset=utf-8" || root.Body.String() != `<!doctype html><html><body><a href="/login/google">Sign in with Google</a></body></html>` {
		t.Fatalf("GET / = %d %q %q", root.Code, root.Header().Get("Content-Type"), root.Body.String())
	}

	login := serveRoute(s, http.MethodGet, "/login/google", nil)
	location := login.Header().Get("Location")
	if login.Code != http.StatusFound || !bytes.Contains([]byte(location), []byte(issuer.URL+"/authorize")) {
		t.Fatalf("GET /login/google = %d location %q", login.Code, location)
	}

	callback := serveRoute(s, http.MethodGet, "/login/google/callback", nil)
	if callback.Code != http.StatusBadRequest || callback.Header().Get("Content-Type") != "text/plain; charset=utf-8" || callback.Body.String() != "invalid login state\n" {
		t.Fatalf("GET /login/google/callback = %d %q %q", callback.Code, callback.Header().Get("Content-Type"), callback.Body.String())
	}

	logout := serveRoute(s, http.MethodPost, "/logout", map[string]string{"Origin": "https://evil.example"})
	if logout.Code != http.StatusForbidden || logout.Body.String() != "forbidden\n" || logout.Header().Get("Set-Cookie") != "" {
		t.Fatalf("POST /logout = %d body %q cookie %q", logout.Code, logout.Body.String(), logout.Header().Get("Set-Cookie"))
	}

	check := serveRoute(s, http.MethodGet, "/check", nil)
	if check.Code != http.StatusUnauthorized || check.Header().Get("Content-Type") != "text/plain; charset=utf-8" || check.Body.String() != "sign in required\n" || check.Header().Get("X-User-Id") != "" || check.Header().Get("X-User-Email") != "" {
		t.Fatalf("GET /check = %d %q id=%q email=%q body %q", check.Code, check.Header().Get("Content-Type"), check.Header().Get("X-User-Id"), check.Header().Get("X-User-Email"), check.Body.String())
	}

	me := serveRoute(s, http.MethodGet, "/me", nil)
	if me.Code != http.StatusUnauthorized || me.Header().Get("Content-Type") != "text/plain; charset=utf-8" || me.Body.String() != "sign in required\n" || bytes.Contains(me.Body.Bytes(), []byte("{")) {
		t.Fatalf("GET /me = %d %q body %q", me.Code, me.Header().Get("Content-Type"), me.Body.String())
	}

	for _, target := range []string{"/tokens", "/tokens/token-id/enable", "/tokens/token-id/disable", "/tokens/token-id/delete"} {
		response := serveRoute(s, http.MethodPost, target, map[string]string{"Origin": "https://evil.example"})
		if response.Code != http.StatusForbidden || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Body.String() != "forbidden\n" {
			t.Fatalf("POST %s = %d %q %q", target, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
	}
}

type diagnosticWrites struct{ calls []string }

func (w *diagnosticWrites) Write(p []byte) (int, error) {
	w.calls = append(w.calls, string(p))
	return len(p), nil
}

func TestCrossRouteFailureDiagnostics(t *testing.T) {
	// R-CCQE-EHNR: failed store operations on D05, D06, and D07 routes
	// produce the same plain 500 without identity headers.
	// R-XV9R-80CN: each 500's diagnostic carries the request id (or "-")
	// and the exact underlying error in one Write call.
	// R-XWHN-LS3C: requests outside the 5xx range leave Stderr untouched.
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"), bytes.NewReader(bytes.Repeat([]byte{1}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, method, target, cookie, origin, id, reason string
	}{
		{"root", http.MethodGet, "/", "session", "", "root-id", "lookup session identity: sql: database is closed"},
		{"login start", http.MethodGet, "/login/google", "", "", "", "insert login state: sql: database is closed"},
		{"callback", http.MethodGet, "/login/google/callback?state=recorded", "", "", "callback-id", "consume login state: sql: database is closed"},
		{"denied callback", http.MethodGet, "/login/google/callback?error=access_denied&state=recorded", "", "", "denied-id", "consume login state: sql: database is closed"},
		{"logout", http.MethodPost, "/logout", "session", "http://localhost:3001", "logout-id", "delete session: sql: database is closed"},
		{"check", http.MethodGet, "/check", "session", "", "check-id", "begin session touch: sql: database is closed"},
		{"me", http.MethodGet, "/me", "session", "", "me-id", "lookup session identity: sql: database is closed"},
		{"token create", http.MethodPost, "/tokens", "session", "http://localhost:3001", "create-id", "lookup session identity: sql: database is closed"},
		{"token action", http.MethodPost, "/tokens/a/enable", "session", "http://localhost:3001", "action-id", "lookup session identity: sql: database is closed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes diagnosticWrites
			s := New(Config{Store: st, Now: fixedNow, Rand: bytes.NewReader(bytes.Repeat([]byte{2}, 128)), Stderr: &writes})
			r := httptest.NewRequestWithContext(context.Background(), tc.method, tc.target, nil)
			r.Host = "localhost:3001"
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: tc.cookie, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.id != "" {
				r.Header.Set("X-Request-Id", tc.id)
			}
			response := httptest.NewRecorder()
			response.Header().Set(HeaderUserID, "stale-user")
			response.Header().Set(HeaderUserEmail, "stale@example.test")
			s.ServeHTTP(response, r)
			if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Body.String() != "internal server error\n" {
				t.Fatalf("response = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
			if response.Header().Get(HeaderUserID) != "" || response.Header().Get(HeaderUserEmail) != "" {
				t.Fatalf("identity headers leaked: %v", response.Header())
			}
			id := tc.id
			if id == "" {
				id = "-"
			}
			want := "auth: request " + id + ": " + tc.reason + "\n"
			if len(writes.calls) != 1 || writes.calls[0] != want {
				t.Fatalf("diagnostic writes = %q, want one %q", writes.calls, want)
			}
		})
	}

	var writes diagnosticWrites
	s := New(Config{Store: st, Now: fixedNow, Stderr: &writes})
	for _, tc := range []struct{ method, target, origin string }{
		{http.MethodGet, "/", ""},
		{http.MethodGet, "/check", ""},
		{http.MethodPost, "/logout", "https://wrong.example"},
		{http.MethodGet, "/missing", ""},
	} {
		r := httptest.NewRequestWithContext(context.Background(), tc.method, tc.target, nil)
		r.Host = "localhost:3001"
		r.Header.Set("Origin", tc.origin)
		response := httptest.NewRecorder()
		s.ServeHTTP(response, r)
		if response.Code >= 500 || len(writes.calls) != 0 {
			t.Fatalf("%s %s: status %d, writes %q", tc.method, tc.target, response.Code, writes.calls)
		}
	}
}

func serveRoute(s *Server, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(response, request)
	return response
}

func loopbackListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	return listener
}

type temporaryErrorListener struct {
	net.Listener
	failed       bool
	failedSignal chan struct{}
}

func (l *temporaryErrorListener) Accept() (net.Conn, error) {
	if !l.failed {
		l.failed = true
		close(l.failedSignal)
		return nil, temporaryAcceptError{}
	}
	return l.Listener.Accept()
}

type temporaryAcceptError struct{}

func (temporaryAcceptError) Error() string   { return "temporary accept failure" }
func (temporaryAcceptError) Temporary() bool { return true }
func (temporaryAcceptError) Timeout() bool   { return false }

type observedListener struct {
	net.Listener
	accepted chan struct{}
}

func (l *observedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.accepted <- struct{}{}
	}
	return conn, err
}

func fixedNow() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

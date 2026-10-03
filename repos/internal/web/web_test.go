package web

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

// R-QWZN-JQIA
func TestIdentityBeforeEveryRoute(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	before := f.gitCalls.Load()
	for _, path := range []string{"/", "/about", "/_appkit/theme.css", "/_appkit/nope", "/mcp", "/notes.git/info/refs?service=git-upload-pack", "/nope"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, values := range [][]string{nil, {""}, {"", "later-user"}} {
				r := httptest.NewRequest(method, path, strings.NewReader(`{"name":"intruder"}`))
				r.Header["X-User-Id"] = values
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-Request-Id", "missing-user")
				got, baseline := httptest.NewRecorder(), httptest.NewRecorder()
				h.ServeHTTP(got, r.Clone(r.Context()))
				identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("identity baseline passed request") })).ServeHTTP(baseline, r.Clone(r.Context()))
				if got.Code != 500 || got.Code != baseline.Code || !reflect.DeepEqual(got.Header(), baseline.Header()) || got.Body.String() != baseline.Body.String() {
					t.Fatalf("%s %s: %d %v %q", method, path, got.Code, got.Header(), got.Body.String())
				}
			}
		}
	}
	if f.bannerCalls.Load() != 0 || f.gitCalls.Load() != before {
		t.Fatal("missing identity reached page or git")
	}
	for _, e := range f.events(t) {
		if e.Name != "request.started" && e.Name != "request.finished" {
			t.Fatalf("missing identity called a tool: %+v", e)
		}
	}
	all, err := f.cfg.Store.All(t.Context())
	if err != nil || len(all) != 0 {
		t.Fatalf("identity failure changed catalog: %v %v", all, err)
	}
}

// R-QUJU-S70W R-QVRR-5YRL R-QY7J-XI8Z R-QZFG-B9ZO
func TestExactRoutesAndNotFound(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	before := f.gitCalls.Load()
	paths := []string{"/nope", "/assets/theme.css", "/mcp/", "/mcp/tool", "/about/", "/notes", "/notes/info/refs", "/_appkit", "/index.html", "//", "/x/../", "/x/notes.git/info/refs", "/notes.gitx"}
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := httptest.NewRequest(method, "http://misleading.test"+path+"?route=/mcp", strings.NewReader("ignored"))
			r.Header.Set("X-User-Id", "user")
			r.Header.Set("X-Original-URI", "/")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := "not found\n"
			if method == "HEAD" {
				want = ""
			}
			if w.Code != 404 || w.Body.String() != want || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Header().Get("Location") != "" {
				t.Fatalf("%s %s: %d %v %q", method, path, w.Code, w.Header(), w.Body.String())
			}
		}
	}
	if f.gitCalls.Load() != before {
		t.Fatal("repos404 started git")
	}
	for _, path := range []string{"/%61bout", "/%6dcp"} {
		w := webRequest(h, "GET", path, "user", "decoded", nil)
		want := 200
		if path == "/%6dcp" {
			want = 405
		}
		if w.Code != want || w.Header().Get("Location") != "" {
			t.Fatalf("decoded route %s: %d %v", path, w.Code, w.Header())
		}
	}
}

// R-R0NC-P1QD
func TestGitRoutesDelegate(t *testing.T) {
	f := newWebFixture(t)
	f.repo(t, "user", "notes")
	h := Handler(f.cfg)
	direct := smarthttp.Handler(smarthttp.Config{Store: f.cfg.Store, Git: f.cfg.Git, Limits: f.cfg.Limits, Telemetry: f.cfg.Telemetry})
	for _, path := range []string{"/notes.git", "/notes.git/", "/notes.git/HEAD", "/notes.git/info/refs?service=git-upload-pack", "/ghost.git/info/refs?service=git-upload-pack", "/Notes.git/HEAD", "/.git/x", "/mcp.git"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			r := httptest.NewRequest(method, path, nil)
			r.Header.Set("X-User-Id", "user")
			r.Header.Set("X-User-Email", "person@example.test")
			r.Header.Set("X-Request-Id", "delegated")
			got, want := httptest.NewRecorder(), httptest.NewRecorder()
			h.ServeHTTP(got, r)
			ctx := identity.NewContext(r.Context(), identity.Caller{UserID: "user", Email: "person@example.test", RequestID: "delegated"})
			direct.ServeHTTP(want, r.WithContext(ctx))
			if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
				t.Fatalf("git delegation %s %s: got %d %v %q want %d %v %q", method, path, got.Code, got.Header(), got.Body.String(), want.Code, want.Header(), want.Body.String())
			}
		}
	}
}

// R-R1V9-2TH2 R-8217-R9CL R-R335-GL7R
func TestMCPRegistrationAndCloneContext(t *testing.T) {
	f := newWebFixture(t)
	path := filepath.Join(f.dir, "services.json")
	f.cfg.ServicesPath = path
	webServices(t, path, `{"services":[{"name":"repos","enabled":true,"mcp":true,"url":"https://first.example","description":"Fixture repositories","socket":""}]}`)
	assertWebListedServices(t, path, services.List{{Name: "repos", Enabled: true, MCP: true, URL: "https://first.example", Description: "Fixture repositories"}})
	srv := httptest.NewServer(Handler(f.cfg))
	t.Cleanup(srv.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	caller := identity.Caller{UserID: "owner", Email: "person@example.test", RequestID: "tool-request"}
	listed, err := client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	directServer := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "fixture-version", Telemetry: f.cfg.Telemetry, Instructions: func(context.Context) string { return "" }})
	tools.Register(directServer, tools.Config{Store: f.cfg.Store, Limits: f.cfg.Limits, Telemetry: f.cfg.Telemetry})
	directHTTP := httptest.NewServer(identity.Require(directServer))
	t.Cleanup(directHTTP.Close)
	directClient := mcp.NewClient(mcp.ClientConfig{Endpoint: directHTTP.URL, HTTPClient: directHTTP.Client()})
	want, err := directClient.ListTools(t.Context(), caller)
	if err != nil || !reflect.DeepEqual(listed, want) {
		t.Fatalf("registered tools differ: %v %v %v", listed, want, err)
	}
	for _, method := range []string{"GET", "HEAD", "DELETE", "PUT"} {
		r := httptest.NewRequest(method, "/mcp?ignored=yes", nil)
		r.Header.Set("X-User-Id", "owner")
		r.Header.Set("X-Request-Id", "mcp-method")
		got, baseline := httptest.NewRecorder(), httptest.NewRecorder()
		// Reuse the handler already built; creating another would register twice.
		srv.Config.Handler.ServeHTTP(got, r)
		ctx := identity.NewContext(r.Context(), identity.Caller{UserID: "owner", RequestID: "mcp-method"})
		ctx = clone.NewContext(ctx, clone.Base(r, path))
		f.cfg.MCP.ServeHTTP(baseline, r.WithContext(ctx))
		if got.Code != 405 || got.Code != baseline.Code || !reflect.DeepEqual(got.Header(), baseline.Header()) || got.Body.String() != baseline.Body.String() {
			t.Fatalf("MCP %s: %d %v %q", method, got.Code, got.Header(), got.Body.String())
		}
	}
	created := webTool(t, client, caller, "create", `{"name":"notes"}`)
	if created["clone_url"] != "https://first.example/notes.git" {
		t.Fatalf("create clone context: %v", created)
	}
	webServices(t, path, `{"services":[{"name":"repos","enabled":true,"mcp":true,"url":"https://second.example/","description":"Fixture repositories","socket":""}]}`)
	assertWebListedServices(t, path, services.List{{Name: "repos", Enabled: true, MCP: true, URL: "https://second.example/", Description: "Fixture repositories"}})
	shown := webTool(t, client, caller, "show", `{"repo":"notes"}`)
	if shown["clone_url"] != "https://second.example/notes.git" {
		t.Fatalf("refreshed show clone context: %v", shown)
	}
	if shown["id"] != created["id"] {
		t.Fatalf("show selected a different repository: created %v shown %v", created, shown)
	}
	other := identity.Caller{UserID: "other", RequestID: "other-request"}
	result, err := client.CallTool(t.Context(), other, "show", json.RawMessage(`{"repo":"notes"}`))
	if err != nil || !result.IsError() {
		t.Fatalf("caller identity was not forwarded: %v %v", result, err)
	}
}

func assertWebListedServices(t *testing.T, path string, want services.List) {
	t.Helper()
	got, err := services.Read(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("services fixture was not accepted exactly: got %+v want %+v error %v", got, want, err)
	}
}

func webTool(t *testing.T, client *mcp.Client, caller identity.Caller, name, args string) map[string]any {
	t.Helper()
	result, err := client.CallTool(t.Context(), caller, name, json.RawMessage(args))
	if err != nil || result.IsError() {
		b, _ := result.MarshalJSON()
		t.Fatalf("tool %s: %s %v", name, b, err)
	}
	b, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Structured map[string]any `json:"structuredContent"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Structured
}

// R-ED46-YDO9
func TestNoSiblingConnections(t *testing.T) {
	f := newWebFixture(t)
	dir, err := os.MkdirTemp("", "repos-web-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f.cfg.ServicesPath = filepath.Join(f.dir, "services.json")
	data, err := json.Marshal(map[string]any{"services": []map[string]any{
		{"name": "repos", "enabled": true, "mcp": true, "url": "https://repos.example", "description": "Fixture repositories", "socket": socket},
		{"name": "dummy", "enabled": true, "mcp": false, "url": "https://dummy.example", "description": "Fixture sibling", "socket": socket},
	}})
	if err != nil {
		t.Fatal(err)
	}
	webServices(t, f.cfg.ServicesPath, string(data))
	assertWebListedServices(t, f.cfg.ServicesPath, services.List{
		{Name: "repos", Enabled: true, MCP: true, URL: "https://repos.example", Description: "Fixture repositories", Socket: socket},
		{Name: "dummy", Enabled: true, MCP: false, URL: "https://dummy.example", Description: "Fixture sibling", Socket: socket},
	})
	srv := httptest.NewServer(Handler(f.cfg))
	t.Cleanup(srv.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	caller := identity.Caller{UserID: "owner", RequestID: "operation"}
	webTool(t, client, caller, "create", `{"name":"notes"}`)
	webTool(t, client, caller, "list", `{}`)
	webTool(t, client, caller, "show", `{"repo":"notes"}`)
	webTool(t, client, caller, "status", `{}`)
	webTool(t, client, caller, "rename", `{"repo":"notes","name":"journal"}`)
	work := filepath.Join(f.dir, "work")
	f.git(t, f.dir, "-c", "http.extraHeader=X-User-Id: owner", "clone", srv.URL+"/journal.git", work)
	f.git(t, work, "commit", "--allow-empty", "-m", "fixture")
	f.git(t, work, "-c", "http.extraHeader=X-User-Id: owner", "push", "origin", "HEAD:main")
	f.git(t, work, "-c", "http.extraHeader=X-User-Id: owner", "fetch", "origin")
	webTool(t, client, caller, "delete", `{"repo":"journal"}`)
	webRequest(srv.Config.Handler, "POST", "/nope", "owner", "not-found", nil)
	raw, err := ln.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var acceptErr error
	if err := raw.Control(func(fd uintptr) {
		accepted, _, e := syscall.Accept4(int(fd), syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
		acceptErr = e
		if e == nil {
			_ = syscall.Close(accepted)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if acceptErr != syscall.EAGAIN && acceptErr != syscall.EWOULDBLOCK {
		t.Fatalf("listed sibling received connection: %v", acceptErr)
	}
}

// R-R4B1-UCYG R-8394-513A R-84H0-ISTZ R-R6QU-LWFU R-RCUC-IR5B
func TestRequestTraceAcrossRoutes(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	for i, tc := range []struct {
		method, path, user string
		status             int
	}{
		{"GET", "/", "user", 200}, {"GET", "/about", "user", 200},
		{"HEAD", "/", "user", 200}, {"HEAD", "/about", "user", 200},
		{"GET", "/_appkit/theme.css", "user", 200}, {"HEAD", "/_appkit/theme.css", "user", 200},
		{"POST", "/about", "user", 405}, {"GET", "/nope", "user", 404},
		{"GET", "/ghost.git/info/refs", "user", 404}, {"GET", "/", "", 500},
		{"GET", "/mcp", "user", 405},
	} {
		id := fmt.Sprintf("trace-%d", i)
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("X-Request-Id", id)
		if tc.user != "" {
			r.Header.Set("X-User-Id", tc.user)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(&webFirstWrite{ResponseWriter: w, check: func() {
			found := webEventsFor(f.events(t), id)
			if len(found) == 0 || found[0].Name != "request.started" {
				t.Errorf("response started before request.started: %v", found)
			}
		}}, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		events := webEventsFor(f.events(t), id)
		assertWebTrace(t, events, tc.user, tc.method, r.URL.Path, w.Code, 0, int64(w.Body.Len()))
		if len(events) != 2 {
			t.Fatalf("simple route emitted domain event: %v", events)
		}
		if tc.path == "/nope" && w.Body.Len() != 10 {
			t.Fatal("404 byte count fixture wrong")
		}
		if tc.path == "/ghost.git/info/refs" && w.Body.Len() != 21 {
			t.Fatal("unknown-repository byte count fixture wrong")
		}
	}
	repo := f.repo(t, "user", "notes")
	f.cfg.Limits.Drain()
	w := webRequest(h, "GET", "/notes.git/info/refs?service=git-upload-pack", "user", "drained", nil)
	if w.Code != 503 {
		t.Fatalf("draining: %d %q for %s", w.Code, w.Body.String(), repo.Name)
	}
	events := webEventsFor(f.events(t), "drained")
	assertWebTrace(t, events, "user", "GET", "/notes.git/info/refs", 503, 0, int64(w.Body.Len()))
	if len(events) != 3 || events[1].Name != "operation.rejected" {
		t.Fatalf("draining trace: %v", events)
	}
	if got := f.stderr.String(); got != "" {
		t.Fatalf("successful delivery wrote stderr: %s", got)
	}
}

type webFirstWrite struct {
	http.ResponseWriter
	once  sync.Once
	check func()
}

func (w *webFirstWrite) WriteHeader(status int) {
	w.once.Do(w.check)
	w.ResponseWriter.WriteHeader(status)
}

func (w *webFirstWrite) Write(b []byte) (int, error) {
	w.once.Do(w.check)
	return w.ResponseWriter.Write(b)
}

func (w *webFirstWrite) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func webEventsFor(events []telemetry.Event, id string) []telemetry.Event {
	var found []telemetry.Event
	for _, e := range events {
		if e.RequestID == id {
			found = append(found, e)
		}
	}
	return found
}

func assertWebTrace(t *testing.T, events []telemetry.Event, user, method, path string, status int, requestBytes, responseBytes int64) {
	t.Helper()
	if len(events) < 2 {
		t.Fatalf("incomplete trace: %v", events)
	}
	first, last := events[0], events[len(events)-1]
	if first.Name != "request.started" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"method": method, "path": path}) {
		t.Fatalf("started: %+v", first)
	}
	want := telemetry.Attrs{"status": int64(status), "duration_us": int64(0), "request_bytes": requestBytes, "response_bytes": responseBytes}
	if last.Name != "request.finished" || !reflect.DeepEqual(last.Attrs, want) {
		t.Fatalf("finished: %+v want %+v", last, want)
	}
	for _, e := range events {
		if e.RequestID != first.RequestID || e.User != user {
			t.Fatalf("caller changed: %+v", e)
		}
	}
	assertWebRequestEventCounts(t, events)
}

func assertWebRequestEventCounts(t *testing.T, events []telemetry.Event) {
	t.Helper()
	var started, finished int
	for _, e := range events {
		switch e.Name {
		case "request.started":
			started++
		case "request.finished":
			finished++
		}
	}
	if started != 1 || finished != 1 {
		t.Fatalf("request event counts: started %d finished %d: %+v", started, finished, events)
	}
}

// R-R5IY-84P5
func TestRequestIDFromHeaderOrWriterRandomness(t *testing.T) {
	f := newWebFixture(t)
	random := bytes.Repeat([]byte{0xa7}, 16)
	f.setWriter(t, f.capture, bytes.NewReader(append(append([]byte(nil), random...), random...)), func() time.Time { return time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC) })
	h := Handler(f.cfg)
	for _, values := range [][]string{nil, {"", "ignored"}, {"explicit", "later"}} {
		r := httptest.NewRequest("GET", "/nope", nil)
		r.Header.Set("X-User-Id", "user")
		r.Header["X-Request-Id"] = values
		before := len(f.events(t))
		h.ServeHTTP(httptest.NewRecorder(), r)
		events := f.events(t)[before:]
		want := hex.EncodeToString(random)
		if len(values) > 0 && values[0] != "" {
			want = values[0]
		}
		if len(events) != 2 {
			t.Fatalf("trace: %v", events)
		}
		for _, e := range events {
			if e.RequestID != want || e.User != "user" {
				t.Fatalf("id %+v want %s", e, want)
			}
		}
	}
}

// R-R96N-DFX8
func TestGitPostCountsLengthAndChunkedBodies(t *testing.T) {
	f := newWebFixture(t)
	work := filepath.Join(f.dir, "work")
	f.git(t, f.dir, "init", "--initial-branch=main", work)
	f.git(t, work, "commit", "--allow-empty", "-m", "fixture")
	sha := strings.TrimSpace(string(f.git(t, work, "rev-parse", "HEAD")))
	pack := f.git(t, work, "pack-objects", "--all", "--stdout")
	command := strings.Repeat("0", 40) + " " + sha + " refs/heads/main\x00report-status\n"
	body := append([]byte(fmt.Sprintf("%04x%s0000", len(command)+4, command)), pack...)
	srv := httptest.NewServer(Handler(f.cfg))
	t.Cleanup(srv.Close)
	for _, chunked := range []bool{false, true} {
		name := "length"
		if chunked {
			name = "chunked"
		}
		repo := f.repo(t, "owner", name)
		r, err := http.NewRequestWithContext(t.Context(), "POST", srv.URL+"/"+name+".git/git-receive-pack", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("X-User-Id", "owner")
		r.Header.Set("X-Request-Id", name)
		r.Header.Set("Content-Type", "application/x-git-receive-pack-request")
		if chunked {
			r.ContentLength = -1
			r.TransferEncoding = []string{"chunked"}
		}
		response, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		answer, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != 200 || !bytes.Contains(answer, []byte("ok refs/heads/main")) {
			t.Fatalf("git post: %d %q %v", response.StatusCode, answer, err)
		}
		gotSHA := strings.TrimSpace(string(f.git(t, f.dir, "--git-dir="+f.cfg.Store.Dir(repo.ID), "rev-parse", "refs/heads/main")))
		if gotSHA != sha {
			t.Fatalf("push did not complete: %s", gotSHA)
		}
		assertWebTrace(t, webEventsFor(f.events(t), name), "owner", "POST", "/"+name+".git/git-receive-pack", 200, int64(len(body)), int64(len(answer)))
	}
}

// R-RAEJ-R7NX R-R6QU-LWFU R-R4B1-UCYG
func TestDomainAndToolTraceNames(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	type observedRequest struct {
		id, user, method, path string
		status                 int
		requestBytes           int64
		responseBytes          int64
		trace                  []telemetry.Event
	}
	var mu sync.Mutex
	var requestMu sync.Mutex
	var observed []observedRequest
	gitRequest := 0
	begun := 0
	completed := make(chan struct{}, 128)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Each interval belongs to this known request, independently of event IDs.
		requestMu.Lock()
		defer requestMu.Unlock()
		before := len(f.events(t))
		mu.Lock()
		begun++
		mu.Unlock()
		id := r.Header.Get("X-Request-Id")
		if id == "git-request" {
			mu.Lock()
			gitRequest++
			id = fmt.Sprintf("git-request-%d", gitRequest)
			mu.Unlock()
			r.Header.Set("X-Request-Id", id)
		}
		body := &webTraceBody{ReadCloser: r.Body}
		r.Body = body
		response := &webTraceResponse{ResponseWriter: w}
		h.ServeHTTP(&webFirstWrite{ResponseWriter: response, check: func() {
			events := webEventsFor(f.events(t), id)
			if len(events) == 0 || events[0].Name != "request.started" || events[0].User != r.Header.Get("X-User-Id") || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": r.Method, "path": r.URL.Path}) {
				t.Errorf("incorrect trace before first response write: %+v", events)
			}
		}}, r)
		trace := f.events(t)[before:]
		mu.Lock()
		observed = append(observed, observedRequest{id: id, user: r.Header.Get("X-User-Id"), method: r.Method, path: r.URL.Path, status: response.status, requestBytes: body.bytes, responseBytes: response.bytes, trace: trace})
		mu.Unlock()
		completed <- struct{}{}
	}))
	t.Cleanup(srv.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	caller := identity.Caller{UserID: "owner", Email: "person@example.test", RequestID: "create-call"}
	webTool(t, client, caller, "create", `{"name":"notes"}`)
	createEvents := webEventsFor(f.events(t), caller.RequestID)
	if len(createEvents) != 4 || createEvents[0].Name != "request.started" || createEvents[1].Name != "repo.created" || createEvents[2].Name != "tool.called" || createEvents[3].Name != "request.finished" {
		t.Fatalf("create event ordering: %v", createEvents)
	}
	for _, tc := range []struct{ name, args string }{{"list", `{}`}, {"show", `{"repo":"notes"}`}, {"status", `{}`}, {"rename", `{"repo":"notes","name":"journal"}`}} {
		caller.RequestID = tc.name + "-call"
		webTool(t, client, caller, tc.name, tc.args)
	}
	work := filepath.Join(f.dir, "work")
	f.git(t, f.dir, "-c", "http.extraHeader=X-User-Id: owner", "-c", "http.extraHeader=X-Request-Id: git-request", "clone", srv.URL+"/journal.git", work)
	f.git(t, work, "commit", "--allow-empty", "-m", "fixture")
	f.git(t, work, "-c", "http.extraHeader=X-User-Id: owner", "-c", "http.extraHeader=X-Request-Id: git-request", "push", "origin", "HEAD:main")
	f.git(t, work, "-c", "http.extraHeader=X-User-Id: owner", "-c", "http.extraHeader=X-Request-Id: git-request", "fetch", "origin")
	caller.RequestID = "delete-call"
	webTool(t, client, caller, "delete", `{"repo":"journal"}`)
	for i, path := range []string{"/", "/about", "/_appkit/theme.css", "/nope", "/ghost.git/info/refs"} {
		webRequest(srv.Config.Handler, "GET", path, "owner", fmt.Sprintf("simple-route-%d", i), nil)
	}
	allowed := map[string]bool{"request.started": true, "request.finished": true, "tool.called": true, "repo.created": true, "repo.renamed": true, "repo.deleted": true, "repo.pushed": true, "repo.fetched": true, "operation.waited": true, "operation.rejected": true, "operation.timed_out": true, "maintenance.finished": true, "repo.unavailable": true}
	seen := map[string]bool{}
	mu.Lock()
	count := begun
	mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for range count {
		select {
		case <-completed:
		case <-ctx.Done():
			t.Fatal("request observers did not complete")
		}
	}
	events := f.events(t)
	mu.Lock()
	requests := append([]observedRequest(nil), observed...)
	mu.Unlock()
	expected := make(map[string]observedRequest, len(requests))
	for _, r := range requests {
		if r.id == "" || r.user != "owner" {
			t.Fatalf("invalid request fixture: %+v", r)
		}
		if _, exists := expected[r.id]; exists {
			t.Fatalf("duplicate request fixture id: %s", r.id)
		}
		expected[r.id] = r
		for _, e := range r.trace {
			if e.RequestID != r.id || e.User != r.user {
				t.Fatalf("request %s event carries another caller: %+v", r.id, e)
			}
		}
		assertWebTrace(t, r.trace, r.user, r.method, r.path, r.status, r.requestBytes, r.responseBytes)
	}
	for _, e := range events {
		r, exists := expected[e.RequestID]
		if !allowed[e.Name] || !exists || e.User != r.user || e.RequestID != r.id {
			t.Fatalf("unexpected trace: %+v", e)
		}
		seen[e.Name] = true
	}
	for _, name := range []string{"request.started", "request.finished", "tool.called", "repo.created", "repo.renamed", "repo.deleted", "repo.pushed", "repo.fetched"} {
		if !seen[name] {
			t.Fatalf("flow never exercised %s", name)
		}
	}
}

type webTraceBody struct {
	io.ReadCloser
	bytes int64
}

func (b *webTraceBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytes += int64(n)
	return n, err
}

type webTraceResponse struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *webTraceResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *webTraceResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *webTraceResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type webSinkFunc func(context.Context, telemetry.Event) error

func (s webSinkFunc) Deliver(ctx context.Context, e telemetry.Event) error { return s(ctx, e) }

// R-RBMG-4ZEM
func TestResponsesDoNotWaitForTelemetry(t *testing.T) {
	f := newWebFixture(t)
	f.repo(t, "owner", "notes")
	baseline := Handler(f.cfg)
	paths := []string{"/", "/about", "/nope", "/notes.git/info/refs?service=git-upload-pack", "/ghost.git/info/refs", "/_appkit/theme.css", "/mcp"}
	statuses := []int{200, 200, 404, 200, 404, 200, 405}
	want := make([]*httptest.ResponseRecorder, len(paths))
	for i, path := range paths {
		want[i] = webRequest(baseline, "GET", path, "owner", "same-id", nil)
		if want[i].Code != statuses[i] {
			t.Fatalf("baseline %s: got %d want %d", path, want[i].Code, statuses[i])
		}
	}
	for _, blocking := range []bool{false, true} {
		started := make(chan context.Context, 1)
		release, deliveryEnded := make(chan struct{}), make(chan struct{})
		var entered sync.Once
		var released sync.Once
		releaseSink := func() { released.Do(func() { close(release) }) }
		sink := webSinkFunc(func(ctx context.Context, _ telemetry.Event) error {
			first := false
			entered.Do(func() {
				first = true
				started <- ctx
			})
			if first {
				defer close(deliveryEnded)
			}
			if !blocking {
				return fmt.Errorf("delivery failed")
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		f.setWriter(t, sink, &webRandom{}, func() time.Time { return time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC) })
		t.Cleanup(releaseSink)
		h := Handler(f.cfg)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		var deliveryCtx context.Context
		var whileBlocked <-chan struct{}
		if blocking {
			whileBlocked = deliveryEnded
		}
		for i, path := range paths {
			answered := make(chan *httptest.ResponseRecorder, 1)
			go func() { answered <- webRequest(h, "GET", path, "owner", "same-id", nil) }()
			if i == 0 {
				select {
				case deliveryCtx = <-started:
				case <-ctx.Done():
					releaseSink()
					cancel()
					t.Fatal("sink never entered")
				}
			}
			select {
			case got := <-answered:
				if blocking {
					select {
					case <-deliveryEnded:
						releaseSink()
						cancel()
						t.Fatal("answer arrived only after blocked delivery ended")
					default:
					}
					if deliveryCtx.Err() != nil {
						releaseSink()
						cancel()
						t.Fatal("delivery timed out before the answer arrived")
					}
				}
				if got.Code != want[i].Code || !reflect.DeepEqual(got.Header(), want[i].Header()) || got.Body.String() != want[i].Body.String() {
					releaseSink()
					cancel()
					t.Fatalf("sink changed %s: %d %v %q", path, got.Code, got.Header(), got.Body.String())
				}
			case <-whileBlocked:
				releaseSink()
				cancel()
				t.Fatal("blocked delivery ended before the response")
			case <-ctx.Done():
				releaseSink()
				cancel()
				t.Fatal("response waited for event delivery")
			}
		}
		releaseSink()
		cancel()
	}
}

// R-RE28-WIW0 R-RFA5-AAMP
func TestNoCookiesAndOptionalEmail(t *testing.T) {
	f := newWebFixture(t)
	f.repo(t, "owner", "notes")
	h := Handler(f.cfg)
	for _, path := range []string{"/", "/about", "/_appkit/theme.css", "/_appkit/nope", "/mcp", "/mcp/", "/nope", "/notes.git/info/refs?service=git-upload-pack"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			expectedStatus := 404
			switch path {
			case "/", "/about", "/_appkit/theme.css":
				expectedStatus = 405
				if method == "GET" || method == "HEAD" {
					expectedStatus = 200
				}
			case "/mcp":
				expectedStatus = 405
				if method == "POST" {
					expectedStatus = 200
				}
			case "/notes.git/info/refs?service=git-upload-pack":
				if method == "GET" {
					expectedStatus = 200
				}
			}
			var answers []*httptest.ResponseRecorder
			for _, email := range []http.Header{{}, {"X-User-Email": {""}}} {
				body := `{}`
				if path == "/mcp" && method == "POST" {
					body = fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{}}}}`, mcp.ProtocolVersion)
				}
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				r.Header = email.Clone()
				r.Header.Set("X-User-Id", "owner")
				r.Header.Set("X-Request-Id", "optional-email")
				r.Header.Set("Content-Type", "application/json")
				if path == "/mcp" && method == "POST" {
					r.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
					r.Header.Set("Mcp-Method", "server/discover")
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != expectedStatus {
					t.Fatalf("optional email %v %s %s: got %d want %d", email, method, path, w.Code, expectedStatus)
				}
				if !strings.HasPrefix(path, "/notes.git/") && len(w.Header().Values("Set-Cookie")) != 0 {
					t.Fatalf("%s %s sets cookies", method, path)
				}
				answers = append(answers, w)
			}
			if answers[0].Code != answers[1].Code || !reflect.DeepEqual(answers[0].Header(), answers[1].Header()) {
				t.Fatalf("email precondition %s %s: %d %d", method, path, answers[0].Code, answers[1].Code)
			}
		}
	}
	for _, path := range []string{"/", "/about", "/_appkit/theme.css", "/mcp", "/nope"} {
		w := webRequest(h, "GET", path, "", "no-identity", nil)
		if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("identity refusal sets cookies for %s", path)
		}
	}
}

// R-RGI1-O2DE
func TestConcurrentRequestsKeepTheirOwnCallers(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	type answered struct {
		id, user, path string
		w              *httptest.ResponseRecorder
	}
	const count = 24
	ready := make(chan struct{})
	answers := make(chan answered, count)
	for i := range count {
		go func() {
			<-ready
			id, user := fmt.Sprintf("request-%d", i), fmt.Sprintf("user-%d", i)
			paths := []string{"/", "/about", "/nope", "/_appkit/OFL.txt"}
			path := paths[i%len(paths)]
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("X-User-Id", user)
			r.Header.Set("X-User-Email", user+"@example.test")
			r.Header.Set("X-Request-Id", id)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			answers <- answered{id, user, path, w}
		}()
	}
	close(ready)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var results []answered
	for range count {
		select {
		case result := <-answers:
			results = append(results, result)
		case <-ctx.Done():
			t.Fatal("concurrent responses did not finish")
		}
	}
	events := f.events(t)
	if len(events) != count*2 {
		t.Fatalf("concurrent events: %d", len(events))
	}
	for _, answer := range results {
		status := 200
		if answer.path == "/nope" {
			status = 404
		}
		if answer.w.Code != status {
			t.Fatalf("response %s: %d", answer.id, answer.w.Code)
		}
		assertWebTrace(t, webEventsFor(events, answer.id), answer.user, "GET", answer.path, status, 0, int64(answer.w.Body.Len()))
	}
}

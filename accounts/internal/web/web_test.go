package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/daemonapi"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/web"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func signature[T any](value T) T { return value }

type fixture struct {
	cfg     web.Config
	capture *telemetry.Capture
	d       *db.DB
	path    string
	now     func() time.Time
	wakes   atomic.Int32
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2026, 11, 17, 14, 3, 7, 0, time.UTC) }
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.db")
	d, err := db.Open(t.Context(), db.Config{Path: path, Migrations: accounts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: now, Rand: strings.NewReader("mock-id1mock-id2mock-id3mock-id4")})
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: capture, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 1<<16)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	banner := func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Release: "chosen-release", Commit: "chosen-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true}
	}
	s, err := settings.Read(func(name string) (string, bool) { return "1", name == "IKIGENBA_SANDBOX" })
	if err != nil {
		t.Fatal(err)
	}
	hosts := settings.SandboxHosts
	c, err := memory.New(memory.Config{Path: filepath.Join(dir, "sandbox-cloud.json"), Hosts: hosts, Now: now, Rand: strings.NewReader(strings.Repeat("chosen-m", 8))})
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "accountsd")
	if err := os.WriteFile(binary, []byte("chosen daemon bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	v := hostauth.NewVerifier(hostauth.VerifierConfig{Region: hosts.Region, Role: hosts.InstanceProfile, Binding: func() (string, error) { return "chosen-binding", nil }, STS: c, Now: now})
	ds, err := daemonapi.New(t.Context(), daemonapi.Config{Store: st, Verifier: v, Binary: binary, Telemetry: w, Configured: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ds.Stop)
	f := &fixture{capture: capture, d: d, path: path, now: now}
	wake := func() { f.wakes.Add(1) }
	f.cfg = web.Config{banner, mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w}), "", st, c, hosts, true, wake, ds, w, s}
	return f
}

func makeRequest(method, target, user string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "accounts.example.test:443"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-User-Email", "chosen@example.test")
	r.Header.Set("X-Request-Id", "chosen-request")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	return r
}

func request(h http.Handler, method, target, user string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, makeRequest(method, target, user, strings.NewReader("chosen-body")))
	return w
}

func same(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("answers differ: %d %v %q / %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-GC6V-KP5O R-QMQR-HIVU R-GEMO-C8N2 R-QSU9-EDLB R-GIAD-HJV5 R-GJI9-VBLU R-QVA2-5X2P
func TestRouteDispatch(t *testing.T) {
	f := setup(t)
	st := f.cfg.Store
	caller := identity.Caller{UserID: "owner"}
	host, op, err := st.CreateHost(t.Context(), store.HostSpec{Type: "chosen-type", RootGB: 27, DataGB: 69}, caller)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), op.ID, store.Update{InstanceID: "chosen-instance"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PlaceAccount(t.Context(), "example-co", host.Name, caller); err != nil {
		t.Fatal(err)
	}
	// The independently built page handler uses the writer clock, not the store clock.
	f.cfg.Telemetry.Shutdown(t.Context(), "replace test clock")
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: capture, Now: func() time.Time { return f.now().Add(store.SilentAfter + time.Second) }, Stderr: io.Discard})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	f.cfg.Telemetry = w
	f.cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w})
	constructor := signature[func(web.Config) http.Handler](web.Handler)
	h := constructor(f.cfg)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := identity.Optional(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: st, MCP: f.cfg.MCP, Configured: f.cfg.Configured, Domain: f.cfg.Hosts.Domain, Now: w.Now}))
	var expectedStarts, expectedFinishes []telemetry.Attrs
	var expectedUsers []string
	for _, user := range []string{"", "viewer"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, tc := range []struct {
				target  string
				handler http.Handler
			}{
				{"/", ph}, {"/tools?ignored=one", ph}, {"/about", ph}, {"/hosts/" + host.Name, ph}, {"/nope", ph}, {"/mcp/", ph}, {"/events", ph}, {"/declarations", ph}, {"/_appkit", ph}, {"/accountsdx", ph},
				{"/_appkit/theme.css", page.Static()}, {"/_appkit/not-present", page.Static()},
				{protocol.BinaryPath, f.cfg.Daemon}, {protocol.StatePath, f.cfg.Daemon}, {"/accountsd//malformed", f.cfg.Daemon}, {"/%61ccountsd?ignored=one", f.cfg.Daemon},
			} {
				got := request(h, method, tc.target, user)
				same(t, got, request(tc.handler, method, tc.target, user))
				if len(got.Header().Values("Set-Cookie")) != 0 {
					t.Fatal("cookie")
				}
				expectedStarts = append(expectedStarts, telemetry.Attrs{"method": method, "path": httptest.NewRequest(method, tc.target, nil).URL.Path})
				expectedFinishes = append(expectedFinishes, telemetry.Attrs{"status": int64(got.Code)})
				expectedUsers = append(expectedUsers, user)
			}
		}
	}
	if err := w.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != 2*len(expectedStarts) {
		t.Fatalf("request event count = %d, want %d", len(events), 2*len(expectedStarts))
	}
	for i, want := range expectedStarts {
		start, finish := events[2*i], events[2*i+1]
		if start.Name != "request.started" || finish.Name != "request.finished" || start.RequestID != "chosen-request" || finish.RequestID != start.RequestID || start.User != expectedUsers[i] || finish.User != start.User || !reflect.DeepEqual(start.Attrs, want) || finish.Attrs["status"] != expectedFinishes[i]["status"] {
			t.Fatal("request telemetry wiring", start, finish)
		}
	}
}

// R-GFUK-Q0DR R-QVA2-5X2P
func TestGuestMCPRefusesBeforeToolDispatch(t *testing.T) {
	f := setup(t)
	f.cfg.Cloud = struct{ cloud.Cloud }{}
	h := web.Handler(f.cfg)
	never := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("guest reached backend") })
	refusal := identity.Require(never)
	methods := []string{"GET", "HEAD", "POST", "DELETE", "PUT"}
	for _, method := range methods {
		target := "/%6dcp?ignored=one"
		result := func(h http.Handler) *httptest.ResponseRecorder {
			r := makeRequest(method, target, "", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_host","arguments":{}}}`))
			r.Header["X-User-Id"] = []string{"", "unused"}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			return w
		}
		got := result(h)
		same(t, got, result(refusal))
		body := identity.MissingBody
		if method == "HEAD" {
			body = ""
		}
		if got.Code != 500 || got.Body.String() != body || !reflect.DeepEqual(got.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || len(got.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("guest MCP response = %d %v %q", got.Code, got.Header(), got.Body.String())
		}
	}
	if f.wakes.Load() != 0 {
		t.Fatal("guest MCP woke engine")
	}
	assertEmptyStore(t, f.cfg.Store)
	if err := f.cfg.Telemetry.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	events := f.capture.Events()
	if len(events) != 2*len(methods) {
		t.Fatalf("events: %v", events)
	}
	for i := range methods {
		if events[2*i].Name != "request.started" || events[2*i+1].Name != "request.finished" {
			t.Fatal("guest MCP emitted tool events")
		}
	}
}

func assertEmptyStore(t *testing.T, st *store.Store) {
	t.Helper()
	hosts, he := st.ListHosts(t.Context(), 0, 1)
	accounts, ae := st.ListAccounts(t.Context(), "", 1)
	users, ue := st.ListUsers(t.Context(), "", "", "", 1)
	ops, oe := st.Runnable(t.Context(), time.Date(2026, 11, 17, 14, 3, 7, 0, time.UTC), "", 1)
	if he != nil || ae != nil || ue != nil || oe != nil || len(hosts)+len(accounts)+len(users)+len(ops) != 0 {
		t.Fatal("unexpected store records", hosts, accounts, users, ops, he, ae, ue, oe)
	}
}

// R-GUNC-P0SM R-QP6K-92D8 R-QVA2-5X2P
func TestMCPRegistrationAndIdentity(t *testing.T) {
	f := setup(t)
	h := web.Handler(f.cfg)
	catalog := []string{
		tools.ToolHosts, tools.ToolHost, tools.ToolCreateHost, tools.ToolStopHost, tools.ToolStartHost, tools.ToolResizeHost,
		tools.ToolRetryHost, tools.ToolDeleteHost, tools.ToolAccounts, tools.ToolAccount, tools.ToolCreateAccount, tools.ToolRemoveAccount,
		tools.ToolUsers, tools.ToolAddUser, tools.ToolAddKey, tools.ToolRemoveKey, tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser,
		tools.ToolUnbanIP, tools.ToolAllowIP, tools.ToolDisallowIP,
	}
	registered := f.cfg.MCP.Tools()
	if len(registered) != len(catalog) {
		t.Fatal("registered count", len(registered))
	}
	for i, tool := range registered {
		if tool.Name != catalog[i] {
			t.Fatal("registered tools", registered)
		}
	}
	observed := make(chan identity.Caller, 1)
	mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct{}]{Name: "observe_caller", Description: "Chosen test description.", Effect: mcp.Read, Handler: func(_ context.Context, c identity.Caller, _ struct{}) (struct{}, error) {
		observed <- c
		return struct{}{}, nil
	}})
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	caller := identity.Caller{UserID: "owner", Email: "chosen@example.test", RequestID: "chosen-mcp-id"}
	change := call[tools.HostChange](t, client, caller, tools.ToolCreateHost, `{}`)
	if change.Host != "host01" || change.State != store.HostProvisioning || f.wakes.Load() == 0 {
		t.Fatal("create_host did not use configured tools", change)
	}
	detail := call[tools.HostRead](t, client, caller, tools.ToolHost, `{"host":"host01"}`)
	if detail.Created != "2026-11-17T14:03:07Z" || detail.CreatedBy != caller.UserID {
		t.Fatal("host did not use store or caller", detail)
	}
	account := call[tools.Account](t, client, caller, tools.ToolCreateAccount, `{"account":"acme","host":"host01"}`)
	if account.DNSName != "acme."+f.cfg.Hosts.Domain {
		t.Fatal("create_account did not use hosts settings", account)
	}
	for _, first := range []string{"supplied", ""} {
		r := makeRequest("POST", "/mcp", "", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"observe_caller","arguments":{}}}`))
		r.Header["X-User-Id"] = []string{"test-viewer", "unused-id"}
		r.Header["X-User-Email"] = []string{"first@example.test", "unused-email@example.test"}
		r.Header["X-Request-Id"] = []string{first, "unused-request"}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("MCP answer", w.Code, w.Body.String())
		}
		var got identity.Caller
		select {
		case got = <-observed:
		default:
			t.Fatal("test tool was not called")
		}
		id := first
		if first == "" {
			id = strings.Repeat("5a", 16)
		}
		if got != (identity.Caller{UserID: "test-viewer", Email: "first@example.test", RequestID: id}) {
			t.Fatalf("tool caller = %+v", got)
		}
	}
}

func call[T any](t *testing.T, c *mcp.Client, caller identity.Caller, name, arguments string) T {
	t.Helper()
	result, err := c.CallTool(t.Context(), caller, name, json.RawMessage(arguments))
	if err != nil || result.IsError() {
		t.Fatalf("%s: %v %v", name, result, err)
	}
	raw, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data T `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

// R-GKQ6-93CJ
func TestOtherRoutesKeepRecordsAndDoNotUseCloud(t *testing.T) {
	f := setup(t)
	// An unexpected cloud call invokes a nil embedded method and fails the test.
	f.cfg.Cloud = struct{ cloud.Cloud }{}
	h := web.Handler(f.cfg)
	for _, user := range []string{"", "viewer"} {
		for _, method := range []string{"GET", "POST", "DELETE"} {
			for _, target := range []string{"/", "/tools", "/about", "/hosts/host01", "/nope", "/mcp/", "/_appkit/theme.css"} {
				request(h, method, target, user)
			}
		}
	}
	if f.wakes.Load() != 0 {
		t.Fatal("page or static route woke engine")
	}
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), db.Config{Path: f.path, Migrations: accounts.Migrations(), Now: f.now})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	assertEmptyStore(t, store.New(d, store.Config{Now: f.now}))
}

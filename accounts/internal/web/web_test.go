package web_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/daemonapi"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/join"
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
	advance func(time.Duration)
	wakes   atomic.Int32
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	var elapsed atomic.Int64
	now := func() time.Time {
		return time.Date(2026, 11, 17, 14, 3, 7, 0, time.UTC).Add(time.Duration(elapsed.Load()))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.db")
	d, err := db.Open(t.Context(), db.Config{Path: path, Migrations: accounts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: now, Rand: rand.NewChaCha8([32]byte{1})})
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: capture, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 1<<16)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	banner := func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Release: "chosen-release", Commit: "chosen-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true}
	}
	s, err := settings.Read(func(name string) (string, bool) { return "", name == "IKIGENBA_SANDBOX" })
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
	f := &fixture{capture: capture, d: d, path: path, now: now, advance: func(d time.Duration) { elapsed.Add(int64(d)) }}
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

// R-GC6V-KP5O R-QMQR-HIVU R-GEMO-C8N2 R-QSU9-EDLB R-GIAD-HJV5 R-FUH5-FVTH R-QVA2-5X2P
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
				{"/join/", ph}, {"/join/x", ph}, {"/join/x/y?token=value", ph}, {"/joinx", ph}, {"/JOIN", ph},
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

// R-JNG8-4KJ3 R-QP6K-92D8 R-QVA2-5X2P
func TestMCPRegistrationAndIdentity(t *testing.T) {
	f := setup(t)
	h := web.Handler(f.cfg)
	catalog := []string{
		tools.ToolHosts, tools.ToolHost, tools.ToolCreateHost, tools.ToolStopHost, tools.ToolStartHost, tools.ToolResizeHost,
		tools.ToolRetryHost, tools.ToolDeleteHost, tools.ToolAccounts, tools.ToolAccount, tools.ToolCreateAccount, tools.ToolRemoveAccount,
		tools.ToolUsers, tools.ToolAddUser, tools.ToolAddKey, tools.ToolRemoveKey, tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser,
		tools.ToolUnbanIP, tools.ToolAllowIP, tools.ToolDisallowIP,
		tools.ToolSetAdmin, tools.ToolCreateInvite, tools.ToolListInvites, tools.ToolRevokeInvite,
		tools.ToolKeptVolumes, tools.ToolDeleteKeptVolume,
	}
	registered := f.cfg.MCP.Tools()
	names := make([]string, 0, len(registered))
	for _, tool := range registered {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	slices.Sort(catalog)
	if !slices.Equal(names, catalog) {
		t.Fatal("registered tools", names)
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
	invite := call[tools.InviteMade](t, client, caller, tools.ToolCreateInvite, `{"account":"acme"}`)
	if !strings.HasPrefix(invite.Link, "http://"+settings.SandboxBinding+"/join?token=") {
		t.Fatalf("fallback invitation link = %q", invite.Link)
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

// R-ISYQ-KAZQ
func TestOtherRoutesKeepRecordsAndDoNotUseCloud(t *testing.T) {
	f := setup(t)
	st := f.cfg.Store
	caller := identity.Caller{UserID: "viewer"}
	host, _, err := st.CreateHost(t.Context(), store.HostSpec{Type: "sample", RootGB: 20, DataGB: 50}, caller)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PlaceAccount(t.Context(), "acme", host.Name, caller); err != nil {
		t.Fatal(err)
	}
	// An unexpected cloud call invokes a nil embedded method and fails the test.
	f.cfg.Cloud = struct{ cloud.Cloud }{}
	h := web.Handler(f.cfg)
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	invite := call[tools.InviteMade](t, client, caller, tools.ToolCreateInvite, `{"account":"acme"}`)
	link, err := url.Parse(invite.Link)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"", "viewer"} {
		for _, method := range []string{"GET", "POST", "DELETE"} {
			for _, target := range []string{"/", "/tools", "/about", "/hosts/host01", "/nope", "/mcp/", "/_appkit/theme.css", "/join/", "/join/" + link.Query().Get(join.TokenParam)} {
				request(h, method, target, user)
			}
		}
		for _, method := range []string{"GET", "DELETE"} {
			request(h, method, link.RequestURI(), user)
		}
	}
	token := link.Query().Get(join.TokenParam)
	if found, err := st.InviteByToken(t.Context(), token); err != nil || found.ID != invite.Invite {
		t.Fatalf("invite after reads = %+v, %v", found, err)
	}
	if got := postForm(h, link.RequestURI(), "", "guest-enrollment", "daffy"); got.Code != http.StatusOK {
		t.Fatalf("first enrollment status = %d, want %d", got.Code, http.StatusOK)
	}
	if got := postForm(h, link.RequestURI(), "viewer", "repeated-enrollment", "daffy"); got.Code != http.StatusNotFound {
		t.Fatalf("repeated enrollment status = %d, want %d", got.Code, http.StatusNotFound)
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
	reopened := store.New(d, store.Config{Now: f.now})
	list, err := reopened.ListUsers(t.Context(), "", "", "", 10)
	if err != nil || len(list) != 1 || list[0].Name != "daffy" || list[0].Account != "acme" {
		t.Fatalf("users after reopening = %+v, %v", list, err)
	}
	hostList, he := reopened.ListHosts(t.Context(), 0, 10)
	accountsList, ae := reopened.ListAccounts(t.Context(), "", 10)
	invites, ie := reopened.Invites(t.Context(), "acme")
	if he != nil || ae != nil || ie != nil || len(hostList) != 1 || hostList[0].Name != host.Name || len(accountsList) != 1 || accountsList[0].Slug != "acme" || len(invites) != 0 {
		t.Fatalf("records after reopening: hosts=%+v accounts=%+v invites=%+v errors=%v %v %v", hostList, accountsList, invites, he, ae, ie)
	}
	if report, err := reopened.Report(t.Context(), host.Name); err != nil || report != nil {
		t.Fatalf("report after reopening = %+v, %v", report, err)
	}
}

// R-JNG8-4KJ3 R-HX9C-M9PO
func TestEnrollmentDispatch(t *testing.T) {
	f := setup(t)
	f.cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
	h := web.Handler(f.cfg)
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	caller := identity.Caller{UserID: "owner"}
	call[tools.HostChange](t, client, caller, tools.ToolCreateHost, `{}`)
	call[tools.Account](t, client, caller, tools.ToolCreateAccount, `{"account":"acme","host":"host01"}`)
	invitePath := func(base string) string {
		t.Helper()
		body := fmt.Sprintf(`{"services":[{"name":"accounts","url":%q,"description":"fixture","socket":"","enabled":true,"mcp":true}]}`, base)
		if err := os.WriteFile(f.cfg.ServicesPath, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		invite := call[tools.InviteMade](t, client, caller, tools.ToolCreateInvite, `{"account":"acme"}`)
		if !strings.HasPrefix(invite.Link, strings.TrimRight(base, "/")+"/join?token=") {
			t.Fatalf("invite link = %q for base %q", invite.Link, base)
		}
		link, err := url.Parse(invite.Link)
		if err != nil {
			t.Fatal(err)
		}
		return link.RequestURI()
	}
	first := invitePath("https://accounts.one.test/")
	second := invitePath("https://accounts.two.test")
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []string{"", "viewer"} {
		want := httptest.NewRecorder()
		set.Write(want, makeRequest(http.MethodGet, first, viewer, nil), http.StatusOK, "join", pages.JoinData{
			Outcome: pages.JoinForm, Account: "acme", DNSName: "acme.hosts.test",
		})
		got := request(h, http.MethodGet, first, viewer)
		if got.Code != want.Code || got.Body.String() != want.Body.String() {
			t.Fatalf("form for %q answered %d, want %d with the join template", viewer, got.Code, want.Code)
		}
	}
	direct := join.Handler(join.Config{Pages: set, Store: f.cfg.Store, Configured: true, Domain: f.cfg.Hosts.Domain,
		Limiter: join.NewLimiter(f.now), Telemetry: f.cfg.Telemetry})
	for _, target := range []string{"/join", strings.Replace(first, "/join", "/%6aoin", 1), first + "&user=ignore", first + "&token="} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete} {
			same(t, request(h, method, target, "viewer"), request(direct, method, target, "viewer"))
		}
	}
	f.d.SetFailing(true)
	unavailable := postForm(h, first, "viewer", "unavailable-enrollment", "daffy")
	f.d.SetFailing(false)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("enrollment with unreachable store status = %d", unavailable.Code)
	}
	post := postForm(h, first, "", "", "daffy")
	if post.Code != http.StatusOK {
		t.Fatalf("enrollment status = %d", post.Code)
	}
	list := call[tools.UserList](t, client, caller, tools.ToolUsers, `{"account":"acme"}`)
	if len(list.Users) != 1 || list.Users[0].User != "daffy" {
		t.Fatalf("users after enrollment = %+v", list)
	}
	// Restore the tokens taken by the first two submissions.
	f.advance(2 * join.Every)
	for i := range join.Burst + 1 {
		got := postForm(h, second, "", "rate-limit-request", "")
		if (i == join.Burst) != (got.Code == http.StatusTooManyRequests) {
			t.Fatalf("attempt %d = %d", i, got.Code)
		}
	}
	if got := request(h, http.MethodGet, second, ""); got.Code != http.StatusOK {
		t.Fatalf("GET after rate limit = %d", got.Code)
	}
	f.advance(join.Every)
	if got := postForm(h, second, "", "refilled-request", "bugs"); got.Code != http.StatusOK {
		t.Fatalf("enrollment after refill status = %d", got.Code)
	}
	if err := f.cfg.Telemetry.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	var enrollmentID string
	used := make(map[string]string)
	for _, event := range f.capture.Events() {
		if event.Name == "request.started" && event.Attrs["method"] == http.MethodPost && event.Attrs["path"] == "/join" {
			enrollmentID = event.RequestID
		}
		if event.Name == "invite.used" {
			name, _ := event.Attrs["user"].(string)
			if used[name] != "" || enrollmentID == "" || event.RequestID != enrollmentID || event.RequestID == "unavailable-enrollment" || event.User != "" {
				t.Fatalf("enrollment event = %+v, request id %q", event, enrollmentID)
			}
			used[name] = event.RequestID
		}
	}
	if len(used) != 2 || used["daffy"] == "" || used["daffy"] == "refilled-request" || used["bugs"] != "refilled-request" {
		t.Fatalf("enrollment request ids = %v", used)
	}
}

func postForm(h http.Handler, path, caller, id, user string) *httptest.ResponseRecorder {
	keyBlob := append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
	keyBlob = append(keyBlob, 0, 0, 0, 32)
	keyBlob = append(keyBlob, bytes.Repeat([]byte{22}, 32)...)
	form := url.Values{"user": {user}, "key": {"ssh-ed25519 " + base64.StdEncoding.EncodeToString(keyBlob)}}
	r := makeRequest(http.MethodPost, path, caller, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Real-IP", "192.0.2.7")
	r.Header.Set("X-Request-Id", id)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

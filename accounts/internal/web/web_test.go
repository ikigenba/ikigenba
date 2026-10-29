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
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
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
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2026, 10, 29, 14, 3, 7, 0, time.UTC) }
	path := filepath.Join(t.TempDir(), "accounts.db")
	d, err := db.Open(t.Context(), db.Config{Path: path, Migrations: accounts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: capture, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 1000000)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test") })
	banner := func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Release: "chosen-release", Commit: "chosen-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true}
	}
	s, err := settings.Read(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	cfg := web.Config{banner, mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w}), "", store.New(d, store.Config{Now: now}), host.NewMemory(), w, s}
	return &fixture{cfg: cfg, capture: capture, d: d, path: path, now: now}
}
func request(h http.Handler, method, target, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader("chosen-body"))
	r.Host = "accounts.example.test:443"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-User-Email", "chosen@example.test")
	r.Header.Set("X-Request-Id", "chosen-request")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func same(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("answers differ: %d %v %q / %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-QKAY-PZEG R-QMQR-HIVU R-QNYN-VAMJ R-QQEG-MU3X R-QSU9-EDLB R-EIGQ-GZX9 R-QVA2-5X2P
func TestRouteDispatch(t *testing.T) {
	f := setup(t)
	constructor := signature[func(web.Config) http.Handler](web.Handler)
	h := constructor(f.cfg)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := identity.Optional(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Host: f.cfg.Host, MCP: f.cfg.MCP}))
	for _, user := range []string{"", "viewer"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, target := range []string{"/", "/tools?ignored=one", "/about", "/nope", "/mcp/", "/events", "/declarations", "/_appkit", "/_appkit/theme.css", "/_appkit/not-present"} {
				got := request(h, method, target, user)
				want := ph
				if strings.HasPrefix(target, "/_appkit/") {
					want = page.Static()
				}
				same(t, got, request(want, method, target, user))
				if len(got.Header().Values("Set-Cookie")) != 0 {
					t.Fatal("cookie")
				}
			}
		}
	}
	for _, method := range []string{"GET", "HEAD", "POST", "DELETE", "PUT"} {
		got := request(h, method, "/mcp?ignored=one", "")
		want := identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("guest reached backend") }))
		same(t, got, request(want, method, "/mcp?ignored=one", ""))
		if len(got.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("MCP cookie")
		}
	}
	if err := f.cfg.Telemetry.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	events := f.capture.Events()
	wantRequests := 2*4*10 + 5
	if len(events) != 2*wantRequests {
		t.Fatalf("request telemetry count %d want %d", len(events), 2*wantRequests)
	}
	for i := 0; i < len(events); i += 2 {
		start, finish := events[i], events[i+1]
		if start.Name != "request.started" || finish.Name != "request.finished" || start.RequestID != "chosen-request" || finish.RequestID != start.RequestID {
			t.Fatal("request telemetry wiring", start, finish)
		}
	}
	groups, err := f.cfg.Host.GroupList(t.Context())
	if err != nil || len(groups) != 0 {
		t.Fatal("host changed", groups, err)
	}
	rec, err := f.cfg.Store.Groups(t.Context(), []string{"business"})
	if err != nil || len(rec) != 0 {
		t.Fatal("store changed", rec, err)
	}
}

// R-QRMD-0LUM R-QP6K-92D8 R-QVA2-5X2P
func TestMCPRegistrationAndIdentity(t *testing.T) {
	f := setup(t)
	h := web.Handler(f.cfg)
	catalog := []string{tools.ToolGroups, tools.ToolGroup, tools.ToolCreateGroup, tools.ToolRemoveGroup, tools.ToolUsers, tools.ToolAddUser, tools.ToolAddKey, tools.ToolRemoveKey, tools.ToolAddMember, tools.ToolRemoveMember, tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser}
	registered := f.cfg.MCP.Tools()
	if len(registered) != len(catalog) {
		t.Fatal("registered count", len(registered))
	}
	for i, tool := range registered {
		if tool.Name != catalog[i] {
			t.Fatal("registration order", registered)
		}
	}
	var observed identity.Caller
	mcp.AddTool(f.cfg.MCP, mcp.Tool[struct{}, struct{}]{Name: "observe_caller", Description: "Chosen description.", Effect: mcp.Read, Handler: func(_ context.Context, c identity.Caller, _ struct{}) (struct{}, error) {
		observed = c
		return struct{}{}, nil
	}})
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	infos, err := client.ListTools(t.Context(), identity.Caller{UserID: "reader"})
	if err != nil || len(infos) != len(catalog)+1 {
		t.Fatal("MCP tools", infos, err)
	}
	for i, info := range infos[:len(catalog)] {
		if info.Name != catalog[i] {
			t.Fatal("tool order", info)
		}
	}
	for i, caller := range []identity.Caller{{UserID: "owner", Email: "first@example.test", RequestID: "first-request"}, {UserID: "other", Email: "second@example.test", RequestID: "second-request"}} {
		group := []string{"acme", "beta"}[i]
		args, err := json.Marshal(map[string]string{"group": group})
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.CallTool(t.Context(), caller, tools.ToolCreateGroup, args)
		if err != nil || result.IsError() {
			t.Fatal("create_group", err, result)
		}
		rec, err := f.cfg.Store.Groups(t.Context(), []string{group})
		if err != nil || rec[group].CreatedUserID != caller.UserID || !rec[group].Created.Equal(f.now()) {
			t.Fatal("tool did not use config store or caller", rec, err)
		}
	}
	groups, err := f.cfg.Host.GroupList(t.Context())
	if err != nil || len(groups) != 2 || groups[0].Slug != "acme" || groups[1].Slug != "beta" {
		t.Fatal("tool did not use config host", groups, err)
	}
	result, err := client.CallTool(t.Context(), identity.Caller{UserID: "reader"}, tools.ToolGroups, json.RawMessage(`{}`))
	if err != nil || result.IsError() {
		t.Fatal(err, result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Data tools.GroupList `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data.Groups) != 2 || listed.Data.Groups[0].Created != f.now().Format(time.RFC3339) {
		t.Fatal("groups did not read configured store", string(raw))
	}
	if err := f.cfg.Telemetry.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	changes := []telemetry.Event{}
	for _, event := range f.capture.Events() {
		if event.Name == trail.EventChangeFinished {
			changes = append(changes, event)
		}
	}
	if len(changes) != 2 || changes[0].User != "owner" || changes[0].RequestID != "first-request" || changes[1].User != "other" || changes[1].RequestID != "second-request" {
		t.Fatal("tool did not use config writer/caller", changes)
	}
	caller := identity.Caller{UserID: "chosen-user", Email: "chosen@example.test", RequestID: "chosen-observation"}
	if _, err := client.CallTool(t.Context(), caller, "observe_caller", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if observed != caller {
		t.Fatal("caller", observed, caller)
	}
	caller.RequestID = ""
	if _, err := client.CallTool(t.Context(), caller, "observe_caller", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if observed.RequestID == "" || observed.UserID != caller.UserID || observed.Email != caller.Email {
		t.Fatal("missing request ID was not minted", observed)
	}
}

// R-QWHY-JOTE
func TestOtherRoutesRunOnlyListsAndKeepRecords(t *testing.T) {
	f := setup(t)
	dir := t.TempDir()
	stub := filepath.Join(dir, "host")
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$1"
shift
case "$*" in
 'group list') printf '%s\n' '{"groups":[{"slug":"business","members":0}]}' ;;
 'user list') printf '%s\n' '{"users":[]}' ;;
 *) exit 1 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	f.cfg.Host = host.NewCommand([]string{"/bin/sh", stub, log})
	h := web.Handler(f.cfg)
	if _, _, err := f.cfg.Store.AddGroup(t.Context(), "business", "creator"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.cfg.Store.AddUser(t.Context(), "person", "creator"); err != nil {
		t.Fatal(err)
	}
	beforeGroups, err := f.cfg.Store.Groups(t.Context(), []string{"business", "other"})
	if err != nil {
		t.Fatal(err)
	}
	beforeUsers, err := f.cfg.Store.Users(t.Context(), []string{"person", "other"})
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"", "viewer"} {
		for _, method := range []string{"GET", "POST", "DELETE"} {
			for _, target := range []string{"/", "/tools", "/about", "/nope", "/mcp/", "/_appkit/theme.css"} {
				request(h, method, target, user)
			}
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	raw, err := root.ReadFile("calls")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatal("unexpected host calls", string(raw))
	}
	if lines[0] != log+" group list" || lines[1] != log+" user list" {
		t.Fatal("non-list host calls", string(raw))
	}
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(t.Context(), db.Config{Path: f.path, Migrations: accounts.Migrations(), Now: f.now})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	st := store.New(reopened, store.Config{Now: f.now})
	afterGroups, err := st.Groups(t.Context(), []string{"business", "other"})
	if err != nil {
		t.Fatal(err)
	}
	afterUsers, err := st.Users(t.Context(), []string{"person", "other"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeGroups, afterGroups) || !reflect.DeepEqual(beforeUsers, afterUsers) {
		t.Fatal("non-MCP route changed record")
	}
}

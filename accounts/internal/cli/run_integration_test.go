package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/join"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/server"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
)

const runVersion = "integration-display"

var fixedTime = time.Date(2026, 11, 17, 14, 3, 7, 0, time.UTC)

type integrationRandom struct {
	n         uint64
	active    atomic.Bool
	collision atomic.Bool
}

func (r *integrationRandom) Read(p []byte) (int, error) {
	if r.active.Swap(true) {
		r.collision.Store(true)
	}
	defer r.active.Store(false)
	r.n++
	for i := range p {
		p[i] = 0xa0 + byte((r.n>>uint((i%8)*6))&0x3f)
	}
	return len(p), nil
}

type integrationConstant byte

func (r integrationConstant) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

func integrationSocketDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("", "accounts-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(d); e != nil {
			t.Error(e)
		}
	})
	return d
}
func integrationServer(t *testing.T, h http.Handler) string {
	t.Helper()
	socket := filepath.Join(integrationSocketDir(t), "s")
	ln, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close(); <-done })
	return socket
}
func integrationServices(t *testing.T, path string, entries map[string]string, auth string) {
	t.Helper()
	rows := []map[string]any{}
	names := []string{}
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		u := "https://" + n + ".test"
		if n == "auth" && auth != "" {
			u = auth
		}
		rows = append(rows, map[string]any{"name": n, "url": u, "socket": entries[n], "description": "fixture", "enabled": true, "mcp": true})
	}
	b, e := json.Marshal(map[string]any{"services": rows})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}

type integrationOptions struct {
	now                                 func() time.Time
	socketDir                           string
	acceptNotify, noNotify, unsetNotify bool
	dir, path                           string
	random                              io.Reader
	sink                                telemetry.Sink
	nilSinks, nilUnset                  bool
	noSandbox                           bool
	unsetServices                       bool
	emptyDir                            bool
	env                                 map[string]string
	mcp                                 func(*telemetry.Writer) *mcp.Server
	banner                              func(page.User) page.Banner
	cloud                               cloud.Opener
	executable                          func() (string, error)
	after                               func(time.Duration) <-chan time.Time
	sleep                               func(context.Context, time.Duration)
	email                               string
}
type integrationRun struct {
	t           *testing.T
	dir         string
	client      *mcp.Client
	http        *http.Client
	base        string
	email       string
	cancel      context.CancelCauseFunc
	done        chan int
	capture     *telemetry.Capture
	out, err    safeBuffer
	keys, unset []string
	sent        []string
	inherit     int
	mu          sync.Mutex
	stopped     bool
	notice      *net.UnixConn
	writer      *telemetry.Writer
	servers     []*mcp.Server
	readyTools  [][]string
	makers      atomic.Int64
}

func startIntegration(t *testing.T, o integrationOptions) *integrationRun {
	t.Helper()
	t.Setenv(services.Variable, o.path)
	r := &integrationRun{t: t, dir: o.dir, capture: &telemetry.Capture{}, done: make(chan int, 1)}
	if r.dir == "" {
		r.dir = t.TempDir()
	}
	sd := o.socketDir
	if sd == "" {
		sd = integrationSocketDir(t)
	}
	notice, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(sd, "n"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	r.notice = notice
	t.Cleanup(func() { _ = notice.Close() })
	ln, e := net.Listen("unix", filepath.Join(sd, "app"))
	if e != nil {
		t.Fatal(e)
	}
	r.base = "http://accounts.test"
	r.email = o.email
	if r.email == "" {
		r.email = "mgreenly+fake01@gmail.com"
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(sd, "app"))
	}}
	r.http = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 5 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	r.client = mcp.NewClient(mcp.ClientConfig{Endpoint: r.base + "/mcp", HTTPClient: r.http})
	env := map[string]string{"IKIGENBA_SANDBOX": "", "LISTEN_PID": "73", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notice.LocalAddr().String(), services.Variable: o.path}
	if o.noNotify {
		env["NOTIFY_SOCKET"] = ""
	}
	if o.unsetNotify {
		delete(env, "NOTIFY_SOCKET")
	}
	for k, v := range o.env {
		env[k] = v
	}
	if o.noSandbox {
		delete(env, "IKIGENBA_SANDBOX")
	}
	if o.unsetServices {
		delete(env, services.Variable)
	}
	p := cli.Process{nil, nil, nil, 73, &r.out, &r.err, runVersion, nil, func() time.Time { return fixedTime }, func(ctx context.Context, _ time.Duration) {
		select {
		case <-ctx.Done():
		default:
		}
	}, o.random, r.dir, o.sink, page.New(pages.ServiceName, version.Identity{}).Banner, o.mcp, o.after, o.executable, o.cloud}
	if p.Executable == nil {
		name := filepath.Join(t.TempDir(), "accounts")
		p.Executable = func() (string, error) { return name, nil }
	}
	if p.After == nil {
		p.After = func(time.Duration) <-chan time.Time { return nil }
	}
	if o.sleep != nil {
		p.Sleep = o.sleep
	}
	if o.banner != nil {
		p.Banner = o.banner
	}
	if o.now != nil {
		p.Now = o.now
	}
	if o.emptyDir {
		p.Dir = ""
	}
	if p.Rand == nil {
		p.Rand = &integrationRandom{}
	}
	if !o.nilSinks {
		if p.Sink == nil {
			p.Sink = r.capture
		}
	}
	if p.MCP == nil {
		p.MCP = func(w *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: p.Version, Telemetry: w})
		}
	}
	maker := p.MCP
	p.MCP = func(w *telemetry.Writer) *mcp.Server {
		srv := maker(w)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.writer != nil && r.writer != w {
			t.Error("the MCP servers did not receive the same writer")
		}
		r.writer = w
		r.servers = append(r.servers, srv)
		r.makers.Add(1)
		return srv
	}
	p.LookupEnv = func(k string) (string, bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.keys = append(r.keys, k)
		if k == "NOTIFY_SOCKET" && r.readyTools == nil {
			// Snapshot registered tools before returning the notification
			// socket. Observing them after READY alone can miss late setup.
			r.readyTools = make([][]string, 0, len(r.servers))
			for _, server := range r.servers {
				var names []string
				for _, tool := range server.Tools() {
					names = append(names, tool.Name)
				}
				r.readyTools = append(r.readyTools, names)
			}
		}
		v, ok := env[k]
		return v, ok
	}
	if !o.nilUnset {
		p.Unsetenv = func(k string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.unset = append(r.unset, k)
			delete(env, k)
			return nil
		}
	}
	readyAccept := make(chan error, 1)
	if o.acceptNotify || o.noNotify || o.unsetNotify {
		ln = &integrationAcceptListener{Listener: ln, check: func() {
			datagrams, err := integrationDatagrams(notice)
			want := 1
			if o.noNotify || o.unsetNotify {
				want = 0
			}
			if err == nil && (len(datagrams) != want || (want == 1 && datagrams[0] != "READY=1")) {
				err = fmt.Errorf("notification before Accept: %q want %d READY datagram", datagrams, want)
			}
			readyAccept <- err
		}}
	}
	p.Inherit = func(fd uintptr) (net.Listener, error) {
		r.inherit++
		if fd != 3 {
			t.Errorf("descriptor %d", fd)
		}
		return ln, nil
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	r.cancel = cancel
	if o.acceptNotify || o.noNotify || o.unsetNotify {
		probe, err := net.Dial("unix", filepath.Join(sd, "app"))
		if err != nil {
			t.Fatal(err)
		}
		_ = probe.Close()
	}
	go func() { r.done <- cli.Run(ctx, p) }()
	t.Cleanup(func() {
		if !r.stopped {
			r.stop()
		}
	})
	if o.acceptNotify || o.noNotify || o.unsetNotify {
		select {
		case err := <-readyAccept:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("server never accepted")
		}
		return r
	}
	if e = notice.SetReadDeadline(time.Now().Add(5 * time.Second)); e != nil {
		t.Fatal(e)
	}
	buf := make([]byte, 100)
	n, _, e := notice.ReadFromUnix(buf)
	if e != nil {
		select {
		case code := <-r.done:
			r.stopped = true
			t.Fatalf("run before ready: %d stderr=%s", code, r.err.String())
		default:
			t.Fatal(e)
		}
	}
	if string(buf[:n]) != "READY=1" {
		t.Fatalf("notification %q", buf[:n])
	}
	return r
}
func (r *integrationRun) stop() int {
	r.t.Helper()
	if r.stopped {
		r.t.Fatal("run stopped twice")
	}
	r.cancel(errors.New("integration stop"))
	select {
	case code := <-r.done:
		r.stopped = true
		r.assertKeys()
		extra, err := integrationDatagrams(r.notice)
		if err != nil || len(extra) != 0 {
			r.t.Errorf("extra readiness datagrams %q: %v", extra, err)
		}
		return code
	case <-time.After(6 * time.Second):
		r.t.Fatal("Run did not stop")
		return -1
	}
}
func (r *integrationRun) assertKeys() {
	r.t.Helper()
	allowed := map[string]bool{"DRAIN_SECONDS": true, "AWS_REGION": true, "IKIGENBA_HOSTS_DOMAIN": true, "IKIGENBA_HOSTS_ZONE_ID": true, "IKIGENBA_HOSTS_LAUNCH_TEMPLATE": true, "IKIGENBA_HOSTS_INSTANCE_PROFILE": true, "IKIGENBA_SANDBOX": true, services.Variable: true, "LISTEN_PID": true, "LISTEN_FDS": true, "NOTIFY_SOCKET": true}
	for _, k := range r.keys {
		if !allowed[k] {
			r.t.Errorf("unexpected environment read %q", k)
		}
	}
}
func (r *integrationRun) callAs(name string, args any, caller identity.Caller) (mcp.Result, error) {
	if caller.RequestID != "" {
		r.mu.Lock()
		r.sent = append(r.sent, caller.RequestID)
		r.mu.Unlock()
	}
	b, e := json.Marshal(args)
	if e != nil {
		return mcp.Result{}, e
	}
	return r.client.CallTool(context.Background(), caller, name, b)
}

func (r *integrationRun) catalogsBeforeReady() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readyTools
}
func (r *integrationRun) call(name string, args any, into any) {
	r.t.Helper()
	q := fmt.Sprintf("call-%d", integrationCalls.Add(1))
	result, e := r.callAs(name, args, identity.Caller{UserID: "owner", Email: r.email, RequestID: q})
	if e != nil {
		r.t.Fatalf("%s: %v", name, e)
	}
	if result.IsError() {
		r.t.Fatalf("%s refused: %+v", name, result)
	}
	if into != nil {
		decodeToolResponse(r.t, result, into)
	}
}

func decodeToolResponse(t *testing.T, result mcp.Result, into any) {
	t.Helper()
	b, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Content json.RawMessage `json:"structuredContent"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(env.Content, into); err != nil {
		t.Fatalf("tool output %s: %v", b, err)
	}
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *safeBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func (b *safeBuffer) Len() int       { b.mu.Lock(); defer b.mu.Unlock(); return b.b.Len() }

var integrationCalls atomic.Uint64

func (r *integrationRun) refuse(name string, args any, word string) {
	r.t.Helper()
	result, e := r.callAs(name, args, identity.Caller{UserID: "owner", Email: r.email, RequestID: fmt.Sprintf("refuse-%d", integrationCalls.Add(1))})
	if e != nil {
		r.t.Fatal(e)
	}
	b, _ := result.MarshalJSON()
	if !result.IsError() || !strings.Contains(string(b), word) {
		r.t.Fatalf("%s expected %s refusal: %s", name, word, b)
	}
}
func (r *integrationRun) request(method, path, user, id string) (int, http.Header, string) {
	r.t.Helper()
	return r.requestBody(method, path, user, id, "", nil)
}

func (r *integrationRun) requestBody(method, path, user, id, media string, body io.Reader) (int, http.Header, string) {
	r.t.Helper()
	if id != "" {
		r.mu.Lock()
		r.sent = append(r.sent, id)
		r.mu.Unlock()
	}
	req, e := http.NewRequestWithContext(context.Background(), method, r.base+path, body)
	if e != nil {
		r.t.Fatal(e)
	}
	req.Host = "accounts.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	if media != "" {
		req.Header.Set("Content-Type", media)
	}
	if user != "" {
		req.Header.Set("X-User-Id", user)
		req.Header.Set("X-User-Email", r.email)
	}
	if id != "" {
		req.Header.Set("X-Request-Id", id)
	}
	res, e := r.http.Do(req)
	if e != nil {
		r.t.Fatal(e)
	}
	reply, e := io.ReadAll(res.Body)
	if e != nil {
		r.t.Error(e)
	}
	if e = res.Body.Close(); e != nil {
		r.t.Error(e)
	}
	return res.StatusCode, res.Header, string(reply)
}

func (r *integrationRun) enroll(path, caller, id, user, key string) (int, string) {
	r.t.Helper()
	form := url.Values{"user": {user}, "key": {key}}
	status, _, response := r.requestBody(http.MethodPost, path, caller, id, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	return status, response
}

func invitePath(t *testing.T, invite tools.InviteMade) string {
	t.Helper()
	link, err := url.Parse(invite.Link)
	if err != nil || !strings.HasPrefix(link.Path, "/join/") {
		t.Fatalf("bad invitation link %q: %v", invite.Link, err)
	}
	return link.Path
}

// R-14LR-87RE R-15TN-LZI3 R-125Y-GOA0 R-EH3H-OOMA
func TestInviteAndEnrollmentTrail(t *testing.T) {
	run := startIntegration(t, integrationOptions{})
	run.call(tools.ToolCreateHost, struct{}{}, nil)
	run.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "acme", Host: "host01"}, nil)
	owner := identity.Caller{UserID: "owner", Email: run.email}
	centralCalls := make(map[string]string)
	type expectedEvent struct {
		name, user string
		attrs      telemetry.Attrs
	}
	expected := map[string]expectedEvent{}
	central := func(id, tool string, args any, reject bool, into any) {
		t.Helper()
		owner.RequestID = id
		centralCalls[id] = tool
		result, err := run.callAs(tool, args, owner)
		if err != nil || result.IsError() != reject {
			t.Fatalf("%s: result=%+v err=%v", tool, result, err)
		}
		if into != nil {
			decodeToolResponse(t, result, into)
		}
	}
	create := func(id string, admin bool) tools.InviteMade {
		t.Helper()
		var made tools.InviteMade
		args := map[string]any{"account": "acme"}
		if admin {
			args["admin"] = true
		}
		central(id, tools.ToolCreateInvite, args, false, &made)
		expected[id] = expectedEvent{trail.EventInviteCreated, owner.UserID,
			telemetry.Attrs{"invite": made.Invite, "account": made.Account, "host": "host01", "admin": made.Admin}}
		return made
	}
	admin := create("make-admin", true)
	ordinary := create("make-ordinary", false)
	revoke := create("make-revoked", false)
	var revoked tools.Invite
	central("revoke", tools.ToolRevokeInvite, tools.InviteArgs{Invite: revoke.Invite}, false, &revoked)
	expected["revoke"] = expectedEvent{trail.EventInviteRevoked, owner.UserID,
		telemetry.Attrs{"invite": revoked.Invite, "account": revoked.Account, "host": "host01"}}
	central("revoke-missing", tools.ToolRevokeInvite, tools.InviteArgs{Invite: revoke.Invite}, true, nil)
	central("list-invites", tools.ToolListInvites, tools.AccountArgs{Account: "acme"}, false, nil)

	type reply struct {
		method string
		user   string
		status int
	}
	joinReplies := map[string]reply{}
	posted := 0
	request := func(method, path, user, id string, expectedStatus int) {
		t.Helper()
		actual, _, _ := run.request(method, path, user, id)
		if actual != expectedStatus {
			t.Fatalf("%s %s = %d, want %d", method, id, actual, expectedStatus)
		}
		joinReplies[id] = reply{method, user, actual}
	}
	enroll := func(invite tools.InviteMade, id, caller, username, key string, wantStatus int) {
		t.Helper()
		posted++
		status, body := run.enroll(invitePath(t, invite), caller, id, username, key)
		if status != wantStatus {
			t.Fatalf("enroll %s = %d, want %d", id, status, wantStatus)
		}
		joinReplies[id] = reply{http.MethodPost, caller, status}
		if status != http.StatusOK {
			return
		}
		set, err := pages.Load()
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		set.Write(rec, httptest.NewRequest(http.MethodPost, "/join/", nil), http.StatusOK, "join", pages.JoinData{
			Outcome: pages.JoinDone, User: username, Account: invite.Account, DNSName: "acme.hosts.test", Admin: invite.Admin,
		})
		if body != rec.Body.String() {
			t.Fatalf("enrollment %s did not render the completed form", id)
		}
		expected[id] = expectedEvent{trail.EventInviteUsed, "",
			telemetry.Attrs{"invite": invite.Invite, "account": invite.Account, "host": "host01", "user": username, "admin": invite.Admin}}
	}
	path := invitePath(t, admin)
	request(http.MethodGet, path, "", "get-form", http.StatusOK)
	request(http.MethodHead, path, owner.UserID, "head-form", http.StatusOK)
	request(http.MethodDelete, path, owner.UserID, "bad-method", http.StatusMethodNotAllowed)
	request(http.MethodGet, invitePath(t, revoke), "", "get-revoked", http.StatusNotFound)
	enroll(admin, "use-admin", owner.UserID, "daffy", validKey(), http.StatusOK)
	enroll(ordinary, "taken-name", owner.UserID, "daffy", validKey(), http.StatusUnprocessableEntity)
	enroll(ordinary, "bad-key", "", "bugs", "invalid", http.StatusUnprocessableEntity)
	enroll(ordinary, "use-ordinary", "", "bugs", validKey(), http.StatusOK)
	enroll(admin, "used-link", "", "elmer", validKey(), http.StatusNotFound)
	var fullInvite tools.InviteMade
	for i := range store.MaxInvites {
		made := create(fmt.Sprintf("make-until-cap-%d", i), false)
		if i == 0 {
			fullInvite = made
			path = invitePath(t, made)
		}
	}
	owner.RequestID = "invite-at-cap"
	centralCalls[owner.RequestID] = tools.ToolCreateInvite
	result, err := run.callAs(tools.ToolCreateInvite, tools.AccountArgs{Account: "acme"}, owner)
	raw, encodeErr := result.MarshalJSON()
	if err != nil || encodeErr != nil || !result.IsError() || !strings.Contains(string(raw), tools.TooManyInvites) {
		t.Fatalf("invite at cap: %s, %v, %v", raw, err, encodeErr)
	}
	submit := func(id, body string, expectedStatus int) {
		t.Helper()
		posted++
		status, _, _ := run.requestBody(http.MethodPost, path, "", id, "application/x-www-form-urlencoded", strings.NewReader(body))
		if status != expectedStatus {
			t.Fatalf("%s answered %d, want %d", id, status, expectedStatus)
		}
		joinReplies[id] = reply{http.MethodPost, "", status}
	}
	submit("body-too-large", strings.Repeat("X", join.MaxBodyBytes+1), http.StatusRequestEntityTooLarge)
	fillEnrollmentHost(t, run)
	enroll(fullInvite, "full-host", "", "elmer", validKey(), http.StatusConflict)
	for posted < join.Burst {
		submit(fmt.Sprintf("invalid-form-%d", posted), "", http.StatusUnprocessableEntity)
	}
	submit("limited", "", http.StatusTooManyRequests)
	for _, args := range []struct {
		id, user string
		admin    bool
		reject   bool
	}{
		{"demote", "daffy", false, false},
		{"demote-again", "daffy", false, false},
		{"promote", "bugs", true, false},
		{"set-missing", "elmer", true, true},
	} {
		central(args.id, tools.ToolSetAdmin, tools.SetAdminArgs{Account: "acme", User: args.user, Admin: args.admin}, args.reject, nil)
		if args.id == "demote" || args.id == "promote" {
			expected[args.id] = expectedEvent{trail.EventAdminChanged, owner.UserID,
				telemetry.Attrs{"account": "acme", "host": "host01", "user": args.user, "admin": args.admin}}
		}
	}
	if run.stop() != cli.ExitSuccess {
		t.Fatal(run.err.String())
	}
	eventsByRequest := make(map[string][]telemetry.Event)
	allowed := eventNames()
	for _, event := range run.capture.Events() {
		if !slices.Contains(allowed, event.Name) {
			t.Fatal("unknown event", event.Name)
		}
		eventsByRequest[event.RequestID] = append(eventsByRequest[event.RequestID], event)
		switch event.Name {
		case trail.EventInviteCreated, trail.EventInviteRevoked, trail.EventInviteUsed, trail.EventAdminChanged:
			want, ok := expected[event.RequestID]
			if !ok || event.Name != want.name || event.User != want.user || !reflect.DeepEqual(event.Attrs, want.attrs) {
				t.Fatalf("unexpected event %+v", event)
			}
			delete(expected, event.RequestID)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing events: %v", expected)
	}
	for id, tool := range centralCalls {
		var sequence []string
		for _, event := range eventsByRequest[id] {
			if event.Name == trail.EventChangeFinished || event.Name == "tool.called" {
				if event.User != owner.UserID || event.Attrs["tool"] != tool {
					t.Fatalf("%s: unexpected tool event %+v", id, event)
				}
				sequence = append(sequence, event.Name)
			}
		}
		want := []string{trail.EventChangeFinished, "tool.called"}
		if tool == tools.ToolListInvites {
			want = []string{"tool.called"}
		}
		if !slices.Equal(sequence, want) {
			t.Fatalf("%s: event sequence = %v, want %v", id, sequence, want)
		}
	}
	for id, reply := range joinReplies {
		events := eventsByRequest[id]
		if len(events) < 2 {
			t.Fatalf("%s: missing request pair: %+v", id, events)
		}
		start, finish := events[0], events[len(events)-1]
		if start.Name != "request.started" || !reflect.DeepEqual(start.Attrs, telemetry.Attrs{"method": reply.method, "path": "/join/"}) ||
			start.User != reply.user || finish.Name != "request.finished" || finish.User != reply.user || finish.Attrs["status"] != int64(reply.status) {
			t.Fatalf("%s: invalid request pair: %+v", id, events)
		}
		used := reply.method == http.MethodPost && reply.status == http.StatusOK
		if (!used && len(events) != 2) || (used && (len(events) != 3 || events[1].Name != trail.EventInviteUsed)) {
			t.Fatalf("%s: invalid request events: %+v", id, events)
		}
	}
}

func fillEnrollmentHost(t *testing.T, run *integrationRun) {
	t.Helper()
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(run.dir, "state", "accounts.db"), Migrations: accounts.Migrations(), Now: func() time.Time { return fixedTime }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	st := store.New(d, store.Config{Now: func() time.Time { return fixedTime }})
	parsed, err := valid.ParseKey(validKey())
	if err != nil {
		t.Fatal(err)
	}
	key := store.Key{Type: parsed.Type, Body: parsed.Body, Line: parsed.Line()}
	for i := 2; i < valid.MaxUsersPerHost; i++ {
		if _, err := st.AddUser(t.Context(), "acme", fmt.Sprintf("testuser%d", i), key, identity.Caller{UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
}

type integrationAcceptListener struct {
	net.Listener
	once  sync.Once
	check func()
}

func (l *integrationAcceptListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.once.Do(l.check)
	}
	return c, err
}
func integrationDatagrams(c *net.UnixConn) ([]string, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return nil, err
	}
	var result []string
	var readErr error
	err = raw.Control(func(fd uintptr) {
		for {
			b := make([]byte, 100)
			n, _, e := syscall.Recvfrom(int(fd), b, syscall.MSG_DONTWAIT)
			if e == syscall.EAGAIN || e == syscall.EWOULDBLOCK {
				return
			}
			if e != nil {
				readErr = e
				return
			}
			result = append(result, string(b[:n]))
		}
	})
	if err != nil {
		return nil, err
	}
	return result, readErr
}

// R-ESG4-8UGU R-Y9CP-KR8R R-YAKL-YIZG R-YBSI-CAQ5 R-QYXR-B8AS R-R1DK-2RS6 R-3GDR-38WI R-R519-8309 R-UVI5-53XK R-H30N-ZNGY
func TestRunPersistenceAndLifecycle(t *testing.T) {
	dir := t.TempDir()
	r := startIntegration(t, integrationOptions{dir: dir, acceptNotify: true})
	checkState(t, dir)
	catalogs := r.catalogsBeforeReady()
	if len(catalogs) != 3 || !slices.Contains(catalogs[0], tools.ToolHosts) {
		t.Fatalf("MCP catalogs before readiness notification: %v", catalogs)
	}
	if r.makers.Load() != 3 {
		t.Fatal("server factory calls", r.makers.Load())
	}
	mcp.AddTool(r.servers[0], mcp.Tool[struct{}, struct{}]{Name: "central_probe", Effect: mcp.Read, Description: "Identify the first server.", Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) {
		return struct{}{}, nil
	}})
	r.call("central_probe", struct{}{}, nil)
	result, err := r.callAs("hosts", struct{}{}, identity.Caller{UserID: "owner", RequestID: "first-read"})
	if err != nil || result.IsError() {
		t.Fatal(result, err)
	}
	assertServerInfo(t, result, runVersion)
	r.writer.Emit(t.Context(), "change.finished", telemetry.Attrs{"tool": "probe"})
	r.call("create_host", struct{}{}, nil)
	checkState(t, dir)
	if r.stop() != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	if r.makers.Load() != 3 {
		t.Fatalf("server factory calls after shutdown = %d", r.makers.Load())
	}
	checkState(t, dir)
	if r.inherit != 1 || !reflect.DeepEqual(r.unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatal(r.inherit, r.unset)
	}
	assertLifecycle(t, r.capture.Events())
	found := false
	for _, event := range r.capture.Events() {
		if event.Name == "change.finished" && event.Attrs["tool"] == "probe" {
			found = true
		}
	}
	if !found {
		t.Fatal("factory writer did not deliver")
	}
	r.writer.Emit(t.Context(), "change.finished", telemetry.Attrs{})
	if r.err.Len() != 0 || r.out.Len() != 0 {
		t.Fatal(r.out.String(), r.err.String())
	}
	for _, next := range []string{dir, t.TempDir()} {
		n := startIntegration(t, integrationOptions{dir: next})
		if next == dir {
			var host tools.HostRead
			n.call("host", map[string]string{"host": "host01"}, &host)
			if host.Host != "host01" || host.Created != fixedTime.Format(time.RFC3339) {
				t.Fatal(host)
			}
		} else {
			n.refuse("host", map[string]string{"host": "host01"}, tools.NoHost)
		}
		if n.stop() != cli.ExitSuccess {
			t.Fatal(n.err.String())
		}
	}
}
func assertServerInfo(t *testing.T, result mcp.Result, v string) {
	t.Helper()
	b, e := result.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var answer struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if e = json.Unmarshal(b, &answer); e != nil {
		t.Fatal(e)
	}
	var got map[string]string
	if e = json.Unmarshal(answer.Meta["io.modelcontextprotocol/serverInfo"], &got); e != nil || !reflect.DeepEqual(got, map[string]string{"name": pages.ServiceName, "version": v}) {
		t.Fatal(string(b), e)
	}
}
func assertLifecycle(t *testing.T, events []telemetry.Event) {
	t.Helper()
	if len(events) < 2 {
		t.Fatal(events)
	}
	a, b := events[0], events[len(events)-1]
	if a.Name != "service.started" || a.User != "" || a.RequestID != "" || !reflect.DeepEqual(a.Attrs, telemetry.Attrs{"version": runVersion}) {
		t.Fatal(a)
	}
	if b.Name != "service.stopping" || b.User != "" || b.RequestID != "" || !reflect.DeepEqual(b.Attrs, telemetry.Attrs{"reason": "integration stop"}) {
		t.Fatal(b)
	}
	n := 0
	for _, e := range events {
		if e.Name == "service.stopping" {
			n++
		}
	}
	if n != 1 {
		t.Fatal("stop count", n)
	}
}
func checkState(t *testing.T, dir string) {
	t.Helper()
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 1 || entries[0].Name() != "state" || !entries[0].IsDir() {
		t.Fatal(entries, e)
	}
	files, e := os.ReadDir(filepath.Join(dir, "state"))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range files {
		if !strings.HasPrefix(f.Name(), "accounts.db") && f.Name() != "sandbox-cloud.json" {
			t.Fatal(f.Name())
		}
		if f.Name() == "accounts.db" {
			info, err := f.Info()
			if err != nil || !info.Mode().IsRegular() {
				t.Fatal(info, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("database missing")
	}
}

func validKey() string {
	blob := make([]byte, 51)
	binary.BigEndian.PutUint32(blob[:4], 11)
	copy(blob[4:15], "ssh-ed25519")
	binary.BigEndian.PutUint32(blob[15:19], 32)
	for i := 19; i < len(blob); i++ {
		blob[i] = byte(i)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " fixture"
}

// R-Y6WW-T7RD R-YE8B-3U7J
func TestRunMemoryAndClock(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var clock atomic.Int64
	clock.Store(fixedTime.Unix())
	r := startIntegration(t, integrationOptions{emptyDir: true, nilUnset: true, now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }})
	r.call("create_host", struct{}{}, nil)
	createdAccount := time.Date(2026, 11, 17, 14, 5, 0, 0, time.UTC)
	clock.Store(createdAccount.Unix())
	r.call("create_account", map[string]string{"account": "acme", "host": "host01"}, nil)
	var host tools.HostRead
	r.call("host", map[string]string{"host": "host01"}, &host)
	var account tools.AccountRead
	r.call("account", map[string]string{"account": "acme"}, &account)
	if host.Created != fixedTime.Format(time.RFC3339) || account.Created != createdAccount.Format(time.RFC3339) {
		t.Fatal(host, account)
	}
	code, _, body := r.request("GET", "/", "owner", "landing")
	if code != 200 || !strings.Contains(body, host.Host) || !strings.Contains(body, account.Account) {
		t.Fatal(code, body)
	}
	if r.stop() != cli.ExitSuccess || len(r.unset) != 0 {
		t.Fatal(r.err.String(), r.unset)
	}
	checkState(t, dir)
}

// R-CEVK-HOJU R-CG3G-VGAJ R-PE8F-6SDX R-EJJA-G83O
func TestRunRandomAndTime(t *testing.T) {
	var ids []string
	for i, clock := range []time.Time{time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC), time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC), fixedTime} {
		value := integrationConstant(0x5a)
		if i == 2 {
			value = 0
		}
		r := startIntegration(t, integrationOptions{random: value, now: func() time.Time { return clock }})
		// The first request omits its id so the middleware must mint it.
		result, e := r.callAs("create_host", struct{}{}, identity.Caller{UserID: "owner"})
		if e != nil || result.IsError() {
			t.Fatal(result, e)
		}
		if r.stop() != cli.ExitSuccess {
			t.Fatal(r.err.String())
		}
		id := ""
		change := ""
		for _, event := range r.capture.Events() {
			if !event.Time.Equal(clock) {
				t.Fatal(event)
			}
			if event.Name == "request.started" && event.RequestID != "" && event.RequestID != "call-" {
				if event.Attrs["path"] == "/mcp" && !strings.HasPrefix(event.RequestID, "call-") {
					id = event.RequestID
				}
			}
			if event.Name == "change.finished" && event.Attrs["host"] == "host01" {
				change = event.RequestID
			}
		}
		if id == "" || id != change {
			t.Fatal(id, change)
		}
		ids = append(ids, id)
	}
	if ids[0] != ids[1] || ids[0] == ids[2] {
		t.Fatal(ids)
	}
	random := &integrationRandom{}
	r := startIntegration(t, integrationOptions{random: random})
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, e := r.callAs("create_host", struct{}{}, identity.Caller{UserID: "owner"})
			if e != nil || result.IsError() {
				t.Error(result, e)
			}
		}()
	}
	wg.Wait()
	r.stop()
	if random.collision.Load() {
		t.Fatal("concurrent random access")
	}
}

// R-KPF2-K9TT
func TestRunWithNoUsableServices(t *testing.T) {
	for _, fixture := range []string{"unset", "empty", "missing", "invalid", "no_entries"} {
		t.Run(fixture, func(t *testing.T) {
			options := integrationOptions{unsetServices: fixture == "unset"}
			if fixture != "unset" && fixture != "empty" {
				options.path = filepath.Join(t.TempDir(), "services")
			}
			if fixture == "invalid" {
				if err := os.WriteFile(options.path, []byte("not json"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if fixture == "no_entries" {
				integrationServices(t, options.path, nil, "")
			}
			assertServes := func(t *testing.T, options integrationOptions) {
				t.Helper()
				r := startIntegration(t, options)
				r.call("hosts", struct{}{}, nil)
				if code, _, _ := r.request("GET", "/", "owner", "landing"); code != http.StatusOK {
					t.Fatal(code)
				}
				r.stop()
			}
			t.Run("sandbox", func(t *testing.T) { assertServes(t, options) })
			t.Run("AWS", func(t *testing.T) {
				options.env = awsEnvironment()
				options.noSandbox = true
				base := testMemoryCloud(t)
				options.cloud = func(context.Context, string) (cloud.Cloud, error) { return base, nil }
				assertServes(t, options)
			})
		})
	}
}

// R-R7H1-ZMHN
func TestRunServices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"auth": ""}, "https://auth.one.test")
	r := startIntegration(t, integrationOptions{path: path})
	integrationServices(t, path, map[string]string{"auth": ""}, "https://auth.two.test")
	code, h, _ := r.request("GET", "/", "", "guest")
	if code != 302 || !strings.HasPrefix(h.Get("Location"), "https://auth.two.test/") {
		t.Fatal(code, h)
	}
	r.stop()
}

type catalogCall struct {
	name string
	args map[string]any
}

func changeCalls(key string) []catalogCall {
	host := map[string]any{"host": "host01"}
	account := map[string]any{"account": "acme"}
	user := map[string]any{"account": "acme", "user": "alice"}
	userKey := map[string]any{"account": "acme", "user": "alice", "key": key}
	return []catalogCall{
		{"create_host", map[string]any{}}, {"create_account", map[string]any{"host": "host01", "account": "acme"}},
		{"add_user", userKey}, {"add_key", userKey}, {"remove_key", userKey}, {"lock_user", user}, {"unlock_user", user}, {"delete_user", user},
		{"remove_account", account}, {"stop_host", host}, {"start_host", host}, {"resize_host", map[string]any{"host": "host01", "data_gb": 60}},
		{"retry_host", host}, {"delete_host", host},
	}
}
func readCalls() []catalogCall {
	return []catalogCall{
		{"hosts", map[string]any{}}, {"host", map[string]any{"host": "host01"}}, {"accounts", map[string]any{}},
		{"account", map[string]any{"account": "acme"}}, {"users", map[string]any{}},
	}
}

func addressCalls() []catalogCall {
	return []catalogCall{
		{tools.ToolUnbanIP, map[string]any{"host": "host01", "ip": "192.0.2.77"}},
		{tools.ToolUnbanIP, map[string]any{"ip": "192.0.2.78"}},
		{tools.ToolAllowIP, map[string]any{"host": "host01", "ip": "192.0.2.0/24"}},
		{tools.ToolDisallowIP, map[string]any{"host": "host01", "ip": "192.0.2.0/24"}},
	}
}

// R-125Y-GOA0 R-EH3H-OOMA R-YOUH-C3ML
func TestRunTrail(t *testing.T) {
	r := startIntegration(t, integrationOptions{})
	type requestRecord struct {
		method, path, user, tool string
		status                   int
		change                   bool
	}
	requests := map[string]requestRecord{}
	caller := identity.Caller{UserID: "actor", Email: "actor@fixture.test"}
	changing := append(changeCalls(validKey()), addressCalls()...)
	for i, c := range append(changing, readCalls()...) {
		caller.RequestID = fmt.Sprintf("tool-%d", i)
		if _, err := r.callAs(c.name, c.args, caller); err != nil {
			t.Fatal(c.name, err)
		}
		requests[caller.RequestID] = requestRecord{"POST", "/mcp", caller.UserID, c.name, 200, i < len(changing)}
	}
	caller.RequestID = "invalid-args"
	if result, err := r.callAs("add_user", map[string]any{"account": "acme", "user": "alice"}, caller); err != nil || !result.IsError() {
		t.Fatal(err)
	}
	requests[caller.RequestID] = requestRecord{"POST", "/mcp", caller.UserID, "", 200, false}
	for _, user := range []string{"", "actor"} {
		for _, path := range []string{"/", "/hosts/host01", "/tools?x=1", "/about", "/accountsd", "/nope", "/join", "/joinx", "/join/x/y"} {
			for _, method := range []string{"GET", "POST", "DELETE", "HEAD"} {
				id := fmt.Sprintf("page-%d", len(requests))
				code, _, _ := r.request(method, path, user, id)
				trailPath, _, _ := strings.Cut(path, "?")
				if strings.HasPrefix(trailPath, "/join/") {
					trailPath = "/join/"
				}
				requests[id] = requestRecord{method, trailPath, user, "", code, false}
			}
		}
	}
	if r.stop() != cli.ExitSuccess || r.err.Len() != 0 || r.out.Len() != 0 {
		t.Fatal(r.err.String(), r.out.String())
	}
	byRequest := map[string][]telemetry.Event{}
	allowed := eventNames()
	for _, event := range r.capture.Events() {
		if !slices.Contains(allowed, event.Name) {
			t.Fatal(event)
		}
		if event.Name == "change.finished" && !requests[event.RequestID].change {
			t.Fatalf("unexpected change event: %+v", event)
		}
		byRequest[event.RequestID] = append(byRequest[event.RequestID], event)
	}
	for id, request := range requests {
		events := byRequest[id]
		if len(events) < 2 {
			t.Fatal(id, events)
		}
		first, last := events[0], events[len(events)-1]
		if first.Name != "request.started" || last.Name != "request.finished" ||
			!reflect.DeepEqual(first.Attrs, telemetry.Attrs{"method": request.method, "path": request.path}) ||
			fmt.Sprint(last.Attrs["status"]) != fmt.Sprint(request.status) {
			t.Fatal(id, events)
		}
		changeAt, toolAt := -1, -1
		for i, event := range events {
			if event.User != request.user || event.RequestID != id {
				t.Fatal(id, event)
			}
			switch event.Name {
			case "request.started":
				if i != 0 {
					t.Fatal(id, event)
				}
			case "request.finished":
				if i != len(events)-1 {
					t.Fatal(id, event)
				}
			case "tool.called":
				if toolAt == -1 {
					toolAt = i
				}
			case "change.finished":
				if changeAt != -1 || !request.change || event.Attrs["tool"] != request.tool {
					t.Fatal(id, events)
				}
				changeAt = i
			case "operation.started", "operation.finished", "host.changed", "account.changed":
				if !request.change {
					t.Fatal(id, event)
				}
			}
		}
		if request.change && (changeAt < 0 || toolAt <= changeAt) {
			t.Fatal(id, events)
		}
	}
}

func eventNames() []string {
	return strings.Fields("service.started service.stopping request.started request.finished tool.called change.finished operation.started operation.finished host.changed account.changed host.silent host.synced host.apply_failed resource.unexplained invite.created invite.revoked invite.used admin.changed")
}

// R-7SSU-QP74
func TestRunNoSiblingConnections(t *testing.T) {
	var accepted atomic.Int64
	ln, e := net.Listen("unix", filepath.Join(integrationSocketDir(t), "trap"))
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close(); <-done })
	barrier := func() {
		t.Helper()
		c, err := net.Dial("unix", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		if err = c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var buf [1]byte
		_, err = c.Read(buf[:])
		if !errors.Is(err, io.EOF) {
			t.Fatal("trap barrier", err)
		}
	}
	barrier()
	if accepted.Load() != 1 {
		t.Fatal("trap positive control", accepted.Load())
	}
	entries := map[string]string{}
	for _, name := range []string{"auth", "telemetry", "mcp", "repos", "sites", "scripts", "events", "cron", "home", "webhooks", "prompts", "artifacts", "connections", "gdrive", "gmail", "scrapes", "candidates", "campaigns", "ledger", "sessions", "dummy"} {
		entries[name] = ln.Addr().String()
	}
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, entries, "")
	bin := t.TempDir()
	daemon := "chosen daemon for sibling check"
	if err := os.WriteFile(filepath.Join(bin, "accountsd"), []byte(daemon), 0600); err != nil {
		t.Fatal(err)
	}
	r := startIntegration(t, integrationOptions{path: path, executable: func() (string, error) {
		return filepath.Join(bin, "accounts"), nil
	}})
	r.call(tools.ToolCreateHost, struct{}{}, nil)
	r.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "acme", Host: "host01"}, nil)
	var invite tools.InviteMade
	r.call(tools.ToolCreateInvite, tools.AccountArgs{Account: "acme"}, &invite)
	pathToJoin := invitePath(t, invite)
	if status, _, _ := r.request(http.MethodGet, pathToJoin, "", "sibling-enroll-get"); status != http.StatusOK {
		t.Fatalf("join page status = %d", status)
	}
	if status, _ := r.enroll(pathToJoin, "", "sibling-enroll-post", "daffy", validKey()); status != http.StatusOK {
		t.Fatalf("enrollment status = %d", status)
	}
	r.call(tools.ToolSetAdmin, tools.SetAdminArgs{Account: "acme", User: "daffy", Admin: true}, nil)
	r.call(tools.ToolListInvites, tools.AccountArgs{Account: "acme"}, nil)
	r.call(tools.ToolCreateInvite, tools.AccountArgs{Account: "acme"}, &invite)
	r.call(tools.ToolRevokeInvite, tools.InviteArgs{Invite: invite.Invite}, nil)
	calls := append(changeCalls(validKey()), readCalls()...)
	calls = append(calls, addressCalls()...)
	requests := make(map[string]string, len(calls))
	for i, call := range calls {
		id := fmt.Sprintf("sibling-check-%d", i)
		requests[id] = call.name
		_, err := r.callAs(call.name, call.args, identity.Caller{UserID: "viewer", RequestID: id})
		if err != nil {
			t.Fatal(call.name, err)
		}
	}
	for _, path := range []string{"/", "/tools", "/about", "/hosts/host01"} {
		if status, _, _ := r.request(http.MethodGet, path, "owner", "page"+path); status != http.StatusOK {
			t.Fatalf("GET %s = %d", path, status)
		}
	}
	if status, _, body := r.request(http.MethodGet, "/accountsd", "", "download"); status != http.StatusOK || body != daemon {
		t.Fatalf("daemon download = %d, %q", status, body)
	}
	if code := r.stop(); code != cli.ExitSuccess {
		t.Fatal(code, r.err.String())
	}
	events := r.capture.Events()
	assertLifecycle(t, events)
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.Name] = true
		if event.Name == "tool.called" {
			delete(requests, event.RequestID)
		}
	}
	if len(requests) != 0 {
		t.Fatalf("tool calls missing from the capture: %v", requests)
	}
	for _, name := range []string{"request.started", "request.finished", "tool.called", "change.finished"} {
		if !seen[name] {
			t.Fatal("capture positive control missing", name)
		}
	}
	barrier()
	if accepted.Load() != 2 {
		t.Fatal("sibling reached", accepted.Load())
	}
}

// R-R1DK-2RS6
func TestRunNoNotification(t *testing.T) {
	for _, unset := range []bool{false, true} {
		r := startIntegration(t, integrationOptions{noNotify: !unset, unsetNotify: unset})
		r.call("hosts", struct{}{}, nil)
		if r.stop() != cli.ExitSuccess {
			t.Fatal(r.err.String())
		}
	}
}

// R-UWQ1-IVO9
func TestRunDrainDeadline(t *testing.T) {
	entered, release, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r := startIntegration(t, integrationOptions{env: map[string]string{"DRAIN_SECONDS": "1"}, mcp: func(w *telemetry.Writer) *mcp.Server {
		s := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: runVersion, Telemetry: w})
		mcp.AddTool(s, mcp.Tool[struct{}, struct{}]{Name: "wait", Description: "Wait for the fixture.", Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, _ struct{}) (struct{}, error) {
			close(entered)
			go func() { <-ctx.Done(); close(canceled) }()
			<-release
			return struct{}{}, nil
		}})
		return s
	}})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = r.callAs("wait", struct{}{}, identity.Caller{UserID: "owner", RequestID: "waiting"})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("request not entered")
	}
	start := time.Now()
	code := r.stop()
	if code != cli.ExitServerFailed || time.Since(start) > 3*time.Second {
		t.Error("drain", code, time.Since(start))
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Error("request context not canceled")
	}
	line := "accounts: " + (&server.DrainError{Unfinished: 1}).Error() + "\n"
	before := r.err.String()
	if !strings.HasSuffix(before, line) {
		t.Error(before)
	}
	delivered := len(r.capture.Events())
	r.writer.Emit(context.Background(), "change.finished", telemetry.Attrs{})
	close(release)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not finish")
	}
	r.writer.Emit(context.Background(), "change.finished", telemetry.Attrs{})
	if r.err.String() != before || len(r.capture.Events()) != delivered || r.out.Len() != 0 {
		t.Fatal("output or sink changed after return", r.err.String())
	}
}

type blockedSink struct {
	calls   atomic.Int64
	entered chan struct{}
	release chan struct{}
}

func (s *blockedSink) Deliver(context.Context, telemetry.Event) error {
	if s.calls.Add(1) == 1 {
		close(s.entered)
	}
	<-s.release
	return nil
}

// R-UWQ1-IVO9
func TestRunBlockedSinkStopsAtDeadline(t *testing.T) {
	sink := &blockedSink{entered: make(chan struct{}), release: make(chan struct{})}
	r := startIntegration(t, integrationOptions{sink: sink, env: map[string]string{"DRAIN_SECONDS": "1"}})
	select {
	case <-sink.entered:
	case <-time.After(3 * time.Second):
		close(sink.release)
		t.Fatal("sink not entered")
	}
	r.call("create_host", struct{}{}, nil)
	before := time.Now()
	code := r.stop()
	if code != cli.ExitSuccess || time.Since(before) > 3*time.Second {
		t.Error(code, time.Since(before))
	}
	calls := sink.calls.Load()
	output := r.err.String()
	r.writer.Emit(context.Background(), "change.finished", telemetry.Attrs{})
	close(sink.release)
	if sink.calls.Load() != calls || r.err.String() != output {
		t.Fatal("late delivery or output")
	}
}

type failedListener struct{}

func (failedListener) Accept() (net.Conn, error) { return nil, errors.New("listener gone\nnow") }
func (failedListener) Close() error              { return nil }
func (failedListener) Addr() net.Addr            { return &net.UnixAddr{Name: "fixture", Net: "unix"} }

// R-K28Z-AMQM
func TestRunFatalAccept(t *testing.T) {
	t.Setenv(services.Variable, "")
	for _, tc := range []struct {
		name        string
		ahead       bool
		unavailable bool
	}{
		{name: "listener failure"},
		{name: "listener and cloud failure", unavailable: true},
		{name: "ahead database and listener failure", ahead: true},
		{name: "ahead database and listener and cloud failure", ahead: true, unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) { assertFatalAccept(t, tc.ahead, tc.unavailable) })
	}
}

func assertFatalAccept(t *testing.T, ahead, unavailable bool) {
	t.Helper()
	dir := t.TempDir()
	var prefix string
	if ahead {
		prefix = aheadDatabase(t, dir)
	}
	env := map[string]string{"LISTEN_PID": "79", "LISTEN_FDS": "1"}
	var open cloud.Opener
	if unavailable {
		for k, v := range awsEnvironment() {
			env[k] = v
		}
		open = func(context.Context, string) (cloud.Cloud, error) {
			return nil, errors.New("no credentials\nretry")
		}
		prefix += "accounts: " + cli.NoCloud + ": no credentials retry\n"
	}
	var out safeBuffer
	var errout diagnosticWrites
	var writer *telemetry.Writer
	p := cli.Process{Args: nil, Dir: dir, Pid: 79, LookupEnv: func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}, Stdout: &out, Stderr: &errout, Version: runVersion, Inherit: func(uintptr) (net.Listener, error) { return failedListener{}, nil }, Now: func() time.Time { return fixedTime }, Sleep: func(context.Context, time.Duration) {}, Rand: integrationConstant(0), Sink: &telemetry.Capture{}, Banner: page.New(pages.ServiceName, version.Identity{}).Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		writer = w
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w})
	}, After: func(time.Duration) <-chan time.Time { return nil }, Cloud: open, Executable: func() (string, error) { return filepath.Join(dir, "accounts"), nil }}
	if code := cli.Run(context.Background(), p); code != cli.ExitServerFailed {
		t.Fatal(code)
	}
	before := errout.snapshot()
	if out.Len() != 0 || len(before) == 0 {
		t.Fatal(out.String(), before)
	}
	last := before[len(before)-1]
	if strings.Join(before[:len(before)-1], "") != prefix || !strings.HasPrefix(last, "accounts: ") ||
		!strings.Contains(last, "listener gone now") || !strings.HasSuffix(last, "\n") || strings.Count(last, "\n") != 1 {
		t.Fatalf("warning writes followed by a single listener diagnostic: %q", before)
	}
	writer.Emit(context.Background(), "change.finished", telemetry.Attrs{})
	if !slices.Equal(errout.snapshot(), before) || out.Len() != 0 {
		t.Fatal("late output")
	}
}

type diagnosticWrites struct {
	mu     sync.Mutex
	writes []string
}

func (d *diagnosticWrites) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, string(p))
	return len(p), nil
}

func (d *diagnosticWrites) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.writes)
}

func aheadDatabase(t *testing.T, dir string) string {
	t.Helper()
	var warning bytes.Buffer
	cfg := db.Config{Path: filepath.Join(dir, "state", "accounts.db"), Migrations: accounts.Migrations(),
		Now: func() time.Time { return fixedTime }, Service: pages.ServiceName, Stderr: &warning}
	d, err := db.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = d.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "INSERT INTO schema_migrations SELECT MAX(version) + 1, ? FROM schema_migrations", fixedTime.Format(time.RFC3339))
		return err
	})
	if err = errors.Join(err, d.Close()); err != nil {
		t.Fatal(err)
	}
	d, err = db.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	if warning.Len() == 0 {
		t.Fatal("ahead database emitted no warning")
	}
	return warning.String()
}

// R-Y6WW-T7RD
func TestRunDefaultSocketSink(t *testing.T) {
	var mu sync.Mutex
	var events []string
	socket := integrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event struct {
			Event string `json:"event"`
		}
		if e := json.NewDecoder(r.Body).Decode(&event); e != nil {
			t.Error(e)
		}
		mu.Lock()
		events = append(events, event.Event)
		mu.Unlock()
		w.WriteHeader(204)
	}))
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"telemetry": socket}, "")
	r := startIntegration(t, integrationOptions{path: path, nilSinks: true, nilUnset: true})
	r.call("create_host", struct{}{}, nil)
	if r.stop() != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 || events[0] != "service.started" || events[len(events)-1] != "service.stopping" {
		t.Fatal(events)
	}
}

func TestRunInjectedBanner(t *testing.T) {
	const chosen = "test-owned-banner"
	banner := func(u page.User) page.Banner {
		return page.Banner{Service: chosen, Release: chosen, Commit: chosen, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}
	r := startIntegration(t, integrationOptions{banner: banner})
	code, _, body := r.request("GET", "/about", "owner", "about-injection")
	req := httptest.NewRequest("GET", r.base+"/about", nil)
	req.Host = "accounts.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	b := banner(page.User{Email: "mgreenly+fake01@gmail.com", ProfileURL: pages.Auth(req, "") + "/", LogoutURL: pages.Auth(req, "") + "/logout"})
	b.Trail = []page.Level{pages.AboutLevel}
	want := httptest.NewRecorder()
	set.Write(want, req, 200, "about", pages.AboutData{Banner: b, Description: pages.Description, DefaultType: valid.DefaultType, DefaultRootGB: valid.DefaultRootGB, DefaultDataGB: valid.DefaultDataGB})
	if code != 200 || body != want.Body.String() {
		t.Fatal("injected banner did not reach rendered page")
	}
	if r.stop() != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
}

package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/server"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
)

const runVersion = "integration-display"

var fixedTime = time.Date(2026, 10, 29, 14, 3, 7, 0, time.UTC)

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
	emptyDir                            bool
	env                                 map[string]string
	mcp                                 func(*telemetry.Writer) *mcp.Server
	banner                              func(page.User) page.Banner
}
type integrationRun struct {
	t           *testing.T
	dir         string
	client      *mcp.Client
	http        *http.Client
	base        string
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
	p := cli.Process{nil, nil, nil, 73, &r.out, &r.err, runVersion, nil, func() time.Time { return fixedTime }, func(ctx context.Context, _ time.Duration) {
		select {
		case <-ctx.Done():
		default:
		}
	}, o.random, r.dir, o.sink, page.New(pages.ServiceName, version.Identity{}).Banner, o.mcp}
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
	p.MCP = func(w *telemetry.Writer) *mcp.Server { r.writer = w; r.makers.Add(1); return maker(w) }
	p.LookupEnv = func(k string) (string, bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.keys = append(r.keys, k)
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
	allowed := map[string]bool{"DRAIN_SECONDS": true, "ACCOUNTS_HOST_COMMAND": true, "IKIGENBA_SANDBOX": true, services.Variable: true, "LISTEN_PID": true, "LISTEN_FDS": true, "NOTIFY_SOCKET": true}
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
func (r *integrationRun) call(name string, args any, into any) {
	r.t.Helper()
	q := fmt.Sprintf("call-%d", integrationCalls.Add(1))
	result, e := r.callAs(name, args, identity.Caller{UserID: "owner", Email: "mgreenly+fake01@gmail.com", RequestID: q})
	if e != nil {
		r.t.Fatalf("%s: %v", name, e)
	}
	b, e := result.MarshalJSON()
	if e != nil {
		r.t.Fatal(e)
	}
	if result.IsError() {
		r.t.Fatalf("%s refused: %s", name, b)
	}
	if into != nil {
		var env struct {
			Content json.RawMessage `json:"structuredContent"`
		}
		if e = json.Unmarshal(b, &env); e != nil {
			r.t.Fatal(e)
		}
		if e = json.Unmarshal(env.Content, into); e != nil {
			r.t.Fatalf("%s output %s: %v", name, b, e)
		}
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
	result, e := r.callAs(name, args, identity.Caller{UserID: "owner", Email: "mgreenly+fake01@gmail.com", RequestID: fmt.Sprintf("refuse-%d", integrationCalls.Add(1))})
	if e != nil {
		r.t.Fatal(e)
	}
	b, _ := result.MarshalJSON()
	if !result.IsError() || !strings.Contains(string(b), word) {
		r.t.Fatalf("%s expected %s refusal: %s", name, word, b)
	}
}
func (r *integrationRun) request(method, path, user, id string, headers map[string]string) (int, http.Header, string) {
	r.t.Helper()
	if id != "" {
		r.mu.Lock()
		r.sent = append(r.sent, id)
		r.mu.Unlock()
	}
	req, e := http.NewRequestWithContext(context.Background(), method, r.base+path, nil)
	if e != nil {
		r.t.Fatal(e)
	}
	req.Host = "accounts.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	if user != "" {
		req.Header.Set("X-User-Id", user)
		req.Header.Set("X-User-Email", "mgreenly+fake01@gmail.com")
	}
	if id != "" {
		req.Header.Set("X-Request-Id", id)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, e := r.http.Do(req)
	if e != nil {
		r.t.Fatal(e)
	}
	body, e := io.ReadAll(res.Body)
	if e != nil {
		r.t.Error(e)
	}
	if e = res.Body.Close(); e != nil {
		r.t.Error(e)
	}
	return res.StatusCode, res.Header, string(body)
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

// R-C9ZY-YLL2 R-CIJ9-MZRX R-CJR6-0RIM R-CKZ2-EJ9B R-CM6Y-SB00 R-QYXR-B8AS R-R1DK-2RS6 R-R3TC-UB9K R-R519-8309 R-UVI5-53XK R-EJOM-URNY
func TestRunPersistenceAndLifecycle(t *testing.T) {
	dir := t.TempDir()
	command, record := integrationStub(t, "", "", "")
	r := startIntegration(t, integrationOptions{dir: dir, acceptNotify: true, env: map[string]string{"ACCOUNTS_HOST_COMMAND": command}})
	checkState(t, dir)
	if r.makers.Load() != 1 {
		t.Fatal("server factory calls", r.makers.Load())
	}
	result, err := r.callAs("groups", struct{}{}, identity.Caller{UserID: "owner", RequestID: "first-read"})
	if err != nil || result.IsError() {
		t.Fatal(result, err)
	}
	assertServerInfo(t, result, runVersion)
	r.writer.Emit(context.Background(), "change.finished", telemetry.Attrs{"tool": "probe"})
	r.call("create_group", map[string]any{"group": "acme"}, nil)
	checkState(t, dir)
	if r.stop() != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	checkState(t, dir)
	if r.inherit != 1 || !reflect.DeepEqual(r.unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatal(r.inherit, r.unset)
	}
	assertLifecycle(t, r.capture.Events(), runVersion)
	found := false
	for _, event := range r.capture.Events() {
		if event.Name == "change.finished" && event.Attrs["tool"] == "probe" {
			found = true
		}
	}
	if !found {
		t.Fatal("factory writer did not deliver")
	}
	before := r.err.String()
	r.writer.Emit(context.Background(), "change.finished", telemetry.Attrs{})
	if r.err.String() != before || r.out.Len() != 0 || before != "" {
		t.Fatal(r.out.String(), r.err.String())
	}
	b, err := os.ReadFile(filepath.Clean(record))
	if err != nil || !strings.Contains(string(b), "group|add|acme|") {
		t.Fatal(string(b), err)
	}
	for _, next := range []string{dir, t.TempDir()} {
		n := startIntegration(t, integrationOptions{dir: next, env: map[string]string{"ACCOUNTS_HOST_COMMAND": command}})
		var groups tools.GroupList
		n.call("groups", struct{}{}, &groups)
		want := ""
		if next == dir {
			want = fixedTime.Format(time.RFC3339)
		}
		if len(groups.Groups) != 1 || groups.Groups[0].Created != want {
			t.Fatal(groups)
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
func assertLifecycle(t *testing.T, events []telemetry.Event, v string) {
	t.Helper()
	if len(events) < 2 {
		t.Fatal(events)
	}
	a, b := events[0], events[len(events)-1]
	if a.Name != "service.started" || a.User != "" || a.RequestID != "" || !reflect.DeepEqual(a.Attrs, telemetry.Attrs{"version": v}) {
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
		if !strings.HasPrefix(f.Name(), "accounts.db") {
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

// integrationStub writes a command, three protocol documents, and a control
// file. Only accounts starts this stub; all shell operations are builtins.
func integrationStub(t *testing.T, leading, users, diagnostic string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	command := filepath.Join(dir, "command")
	record := filepath.Join(dir, "record")
	if users == "" {
		users = `{"users":[]}`
	}
	for name, body := range map[string]string{"groups": `{"groups":[{"slug":"acme","members":0}]}`, "users": users, "mode": "0", "diagnostic": diagnostic} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\nrecord='" + record + "'\nprintf '%s|' \"$@\" >> \"$record\"\nprintf '\\n' >> \"$record\"\ninput=\nwhile IFS= read -r line; do input=\"$input$line\"; done\nprintf '%s\\n' \"$input\" >> '" + filepath.Join(dir, "input") + "'\n"
	if leading != "" {
		script += "shift\n"
	}
	script += "case \"$1 $2\" in\n'group list') IFS= read -r document < '" + filepath.Join(dir, "groups") + "'; printf '%s\\n' \"$document\"; exit 0;;\n'group members') printf '{\"group\":\"%s\",\"members\":[]}\\n' \"$3\"; exit 0;;\n'user list') IFS= read -r document < '" + filepath.Join(dir, "users") + "'; printf '%s\\n' \"$document\"; exit 0;;\nesac\nmode=0\nIFS= read -r mode < '" + filepath.Join(dir, "mode") + "'\nIFS= read -r diagnostic < '" + filepath.Join(dir, "diagnostic") + "'\nprintf '%s\\n' \"$diagnostic\" >&2\nexit \"$mode\"\n"
	if err := writeExecutable(command, []byte(script)); err != nil {
		t.Fatal(err)
	}
	return command, record
}
func setStubMode(t *testing.T, command, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(command), "mode"), []byte(mode+"\n"), 0600); err != nil {
		t.Fatal(err)
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

// R-CDNO-3WT5 R-CPUN-XM83 R-PGO7-YBVB
func TestRunMemoryAndClock(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var clock atomic.Int64
	clock.Store(fixedTime.Unix())
	for _, blank := range []string{"", " \t\n"} {
		r := startIntegration(t, integrationOptions{emptyDir: true, nilUnset: true, now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }, env: map[string]string{"ACCOUNTS_HOST_COMMAND": blank, "IKIGENBA_SANDBOX": ""}})
		var groups tools.GroupList
		r.call("groups", struct{}{}, &groups)
		if len(groups.Groups) != 0 {
			t.Fatal(groups)
		}
		r.refuse("create_group", map[string]any{"group": "Acme"}, host.RefuseInvalidName)
		r.call("create_group", map[string]any{"group": "acme"}, nil)
		r.call("add_user", map[string]any{"user": "alice", "group": "acme", "key": validKey()}, nil)
		code, _, body := r.request("GET", "/", "owner", "landing", nil)
		if code != 200 || !strings.Contains(body, "acme") || !strings.Contains(body, "alice") || !strings.Contains(body, fixedTime.Format(time.RFC3339)) {
			t.Fatal(code, body)
		}
		clock.Store(time.Date(2026, 10, 29, 14, 5, 0, 0, time.UTC).Unix())
		r.call("add_user", map[string]any{"user": "bob", "group": "acme", "key": validKey()}, nil)
		var users tools.UserList
		r.call("users", struct{}{}, &users)
		if len(users.Users) != 2 || users.Users[0].Created != fixedTime.Format(time.RFC3339) || users.Users[1].Created != time.Unix(clock.Load(), 0).UTC().Format(time.RFC3339) {
			t.Fatal(users)
		}
		r.call("groups", struct{}{}, &groups)
		if len(groups.Groups) != 1 || groups.Groups[0].Created != fixedTime.Format(time.RFC3339) {
			t.Fatal(groups)
		}
		if r.stop() != cli.ExitSuccess {
			t.Fatal(r.err.String())
		}
		clock.Store(fixedTime.Unix())
	}
}

// R-PFGB-KK4M
func TestRunCommandOverride(t *testing.T) {
	for _, sandbox := range []string{"absent", "", "space"} {
		command, record := integrationStub(t, "-x", "", "")
		r := startIntegration(t, integrationOptions{noSandbox: sandbox == "absent", env: map[string]string{"ACCOUNTS_HOST_COMMAND": "\t" + command + "   -x\t", "IKIGENBA_SANDBOX": sandbox}})
		r.call("create_group", map[string]any{"group": "acme"}, nil)
		r.request("GET", "/", "owner", "page", nil)
		if r.stop() != cli.ExitSuccess {
			t.Fatal(r.err.String())
		}
		b, e := os.ReadFile(filepath.Clean(record))
		if e != nil {
			t.Fatal(e)
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		for _, line := range lines {
			if !strings.HasPrefix(line, "-x|") {
				t.Fatal(line)
			}
		}
		if !strings.Contains(string(b), "-x|group|add|acme|\n") {
			t.Fatal(string(b))
		}
	}
}

// R-CEVK-HOJU R-CG3G-VGAJ R-PE8F-6SDX R-A7C7-TQ4C
func TestRunRandomAndTime(t *testing.T) {
	var ids []string
	for i, clock := range []time.Time{time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC), time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC), fixedTime} {
		value := integrationConstant(0x5a)
		if i == 2 {
			value = 0
		}
		r := startIntegration(t, integrationOptions{random: value, now: func() time.Time { return clock }})
		// The first request omits its id so the middleware must mint it.
		result, e := r.callAs("create_group", map[string]any{"group": "beta"}, identity.Caller{UserID: "owner"})
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
			if event.Name == "change.finished" && event.Attrs["group"] == "beta" {
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
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, e := r.callAs("create_group", map[string]any{"group": fmt.Sprintf("team%d", i)}, identity.Caller{UserID: "owner"})
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

// R-7XHX-UQ7T R-R7H1-ZMHN
func TestRunServices(t *testing.T) {
	for _, mode := range []string{"unset", "empty", "missing", "invalid", "no_entries"} {
		path := ""
		if mode != "unset" && mode != "empty" {
			path = filepath.Join(t.TempDir(), "services")
		}
		if mode == "invalid" {
			if e := os.WriteFile(path, []byte("not json"), 0600); e != nil {
				t.Fatal(e)
			}
		}
		if mode == "no_entries" {
			integrationServices(t, path, map[string]string{}, "")
		}
		command, _ := integrationStub(t, "", "", "")
		if e := os.WriteFile(filepath.Join(filepath.Dir(command), "groups"), []byte(`{"groups":[]}`), 0600); e != nil {
			t.Fatal(e)
		}
		r := startIntegration(t, integrationOptions{path: path, env: map[string]string{"ACCOUNTS_HOST_COMMAND": command}})
		r.call("groups", struct{}{}, nil)
		code, _, _ := r.request("GET", "/", "owner", "landing", nil)
		if code != 200 {
			t.Fatal(code)
		}
		r.stop()
	}
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"auth": ""}, "https://auth.one.test")
	r := startIntegration(t, integrationOptions{path: path})
	integrationServices(t, path, map[string]string{"auth": ""}, "https://auth.two.test")
	code, h, _ := r.request("GET", "/", "", "guest", nil)
	if code != 302 || !strings.HasPrefix(h.Get("Location"), "https://auth.two.test/") {
		t.Fatal(code, h)
	}
	r.stop()
}

func changingCalls(key string) []struct {
	name string
	args map[string]any
} {
	return []struct {
		name string
		args map[string]any
	}{
		{"create_group", map[string]any{"group": "acme"}}, {"remove_group", map[string]any{"group": "acme"}},
		{"add_user", map[string]any{"user": "alice", "group": "acme", "key": key}}, {"add_key", map[string]any{"user": "alice", "key": key}}, {"remove_key", map[string]any{"user": "alice", "key": key}},
		{"add_member", map[string]any{"user": "alice", "group": "acme"}}, {"remove_member", map[string]any{"user": "alice", "group": "acme"}}, {"lock_user", map[string]any{"user": "alice"}}, {"unlock_user", map[string]any{"user": "alice"}}, {"delete_user", map[string]any{"user": "alice"}},
	}
}

// R-A00T-J3O6 R-5ESL-LDPF R-AG0K-V54M R-AH8H-8WVB R-A4WF-26MY R-AIGD-MOM0 R-AJOA-0GCP
func TestRunTrailAndSecrecy(t *testing.T) {
	markers := []string{"GHIJKLMNGHIJKLMNGHIJKLMN", "OPQRSTUVOPQRSTUVOPQRSTUV", "WXYZ1234WXYZ1234WXYZ1234"}
	encoded := strings.Split(validKey(), " ")[1]
	encoded = encoded[:28] + markers[0] + encoded[52:]
	key := "ssh-ed25519 " + encoded + " " + markers[2]
	blob, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil || len(blob) != 51 || !strings.Contains(base64.StdEncoding.EncodeToString(blob), markers[0]) {
		t.Fatal("key fixture", e)
	}
	userDoc, bErr := json.Marshal(map[string]any{"users": []any{map[string]any{"name": "alice", "groups": []string{"acme"}, "keys": []string{key}, "state": host.StateActive, "last_sign_in": nil}}})
	if bErr != nil {
		t.Fatal(bErr)
	}
	command, _ := integrationStub(t, "", string(userDoc), host.DiagnosticPrefix+strings.Join(markers, " "))
	r := startIntegration(t, integrationOptions{env: map[string]string{"ACCOUNTS_HOST_COMMAND": command}})
	type expected struct {
		tool, group, user, result, method, path string
		status                                  int
	}
	calls := map[string]expected{}
	requests := map[string]expected{}
	reads := []struct {
		name string
		args map[string]any
	}{{"groups", map[string]any{}}, {"group", map[string]any{"group": "acme"}}, {"users", map[string]any{}}}
	for _, mode := range []struct {
		status, result string
		failure        bool
	}{{"0", trail.ResultOK, false}, {"2", trail.ResultRefused, true}, {"4", trail.ResultRefused, true}, {"1", trail.ResultFailed, true}, {"3", trail.ResultFailed, true}} {
		setStubMode(t, command, mode.status)
		for _, call := range changingCalls(key) {
			id := fmt.Sprintf("change-%s-%s", mode.status, call.name)
			result, err := r.callAs(call.name, call.args, identity.Caller{UserID: "actor", Email: markers[1] + "@test.invalid", RequestID: id})
			if err != nil || result.IsError() != mode.failure {
				t.Fatal(call.name, mode, result, err)
			}
			want := expected{tool: call.name, result: mode.result, status: 200, method: "POST", path: "/mcp"}
			want.group, _ = call.args["group"].(string)
			want.user, _ = call.args["user"].(string)
			calls[id] = want
			requests[id] = want
		}
		for _, call := range reads {
			id := "read-" + mode.status + call.name
			result, err := r.callAs(call.name, call.args, identity.Caller{UserID: "actor", Email: markers[1] + "@test.invalid", RequestID: id})
			if err != nil || result.IsError() {
				t.Fatal(result, err)
			}
			requests[id] = expected{status: 200, method: "POST", path: "/mcp"}
		}
	}
	invalidID := "missing-key"
	result, err := r.callAs("add_user", map[string]any{"user": "alice", "group": "acme"}, identity.Caller{UserID: "actor", Email: markers[1] + "@test.invalid", RequestID: invalidID})
	if err != nil || !result.IsError() {
		t.Fatal(result, err)
	}
	requests[invalidID] = expected{status: 200, method: "POST", path: "/mcp"}
	_, err = r.callAs("not_a_tool", map[string]any{}, identity.Caller{UserID: "actor", Email: markers[1] + "@test.invalid", RequestID: "unknown-tool"})
	if err == nil {
		t.Fatal("unknown tool should fail")
	}
	for _, user := range []string{"", "actor"} {
		for _, path := range []string{"/", "/tools?x=1", "/about", "/nope"} {
			for _, method := range []string{"GET", "POST", "DELETE", "HEAD"} {
				id := fmt.Sprintf("page-%d", len(requests))
				code, _, _ := r.request(method, path, user, id, map[string]string{"X-User-Email": markers[1] + "@test.invalid"})
				urlPath, _, _ := strings.Cut(path, "?")
				requests[id] = expected{user: user, status: code, method: method, path: urlPath}
			}
		}
	}
	if r.stop() != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	if r.out.Len() != 0 || r.err.Len() != 0 {
		t.Fatal(r.out.String(), r.err.String())
	}
	events := r.capture.Events()
	allowed := map[string]bool{"service.started": true, "service.stopping": true, "request.started": true, "request.finished": true, "tool.called": true, "change.finished": true}
	byID := map[string][]telemetry.Event{}
	for _, event := range events {
		if !allowed[event.Name] {
			t.Fatal(event)
		}
		if !event.Time.Equal(fixedTime) {
			t.Fatal(event)
		}
		bytes, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		for _, marker := range markers {
			if strings.Contains(strings.ToLower(string(bytes)), strings.ToLower(marker)) {
				t.Fatal("marker leaked", event.Name)
			}
		}
		if event.RequestID != "" {
			byID[event.RequestID] = append(byID[event.RequestID], event)
		}
	}
	for _, event := range byID["unknown-tool"] {
		if event.Name == "change.finished" {
			t.Fatal("transport error recorded a change", event)
		}
	}
	for id, want := range requests {
		got := byID[id]
		if len(got) < 2 || got[0].Name != "request.started" || got[len(got)-1].Name != "request.finished" {
			t.Fatal(id, got)
		}
		user := "actor"
		if strings.HasPrefix(id, "page-") {
			user = want.user
		}
		if !reflect.DeepEqual(got[0].Attrs, telemetry.Attrs{"method": want.method, "path": want.path}) {
			t.Fatal(got[0])
		}
		starts, finishes, changes, toolAt, changeAt := 0, 0, 0, -1, -1
		for i, event := range got {
			if event.User != user || event.RequestID != id {
				t.Fatal(event)
			}
			switch event.Name {
			case "request.started":
				starts++
			case "request.finished":
				finishes++
				if fmt.Sprint(event.Attrs["status"]) != fmt.Sprint(want.status) {
					t.Fatal(event, want)
				}
			case "tool.called":
				toolAt = i
			case "change.finished":
				changes++
				changeAt = i
				if !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"tool": want.tool, "group": want.group, "user": want.user, "result": want.result}) {
					t.Fatal(event, want)
				}
			}
		}
		if starts != 1 || finishes != 1 {
			t.Fatal(id, starts, finishes)
		}
		_, changing := calls[id]
		if changing {
			if changes != 1 || changeAt >= toolAt {
				t.Fatal(id, got)
			}
		} else if changes != 0 {
			t.Fatal(id, got)
		}
	}
	missing := startIntegration(t, integrationOptions{env: map[string]string{"ACCOUNTS_HOST_COMMAND": filepath.Join(t.TempDir(), "absent")}})
	missing.refuse("create_group", map[string]any{"group": "acme"}, tools.Failed)
	missing.stop()
	seen := false
	for _, event := range missing.capture.Events() {
		if event.Name == "change.finished" {
			seen = true
			if event.Attrs["result"] != trail.ResultFailed {
				t.Fatal(event)
			}
		}
	}
	if !seen {
		t.Fatal("failure event missing")
	}
}

// R-CNEV-62QP
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
	for _, name := range []string{"auth", "telemetry", "mcp", "repos", "sites", "scripts", "events", "cron", "home", "webhooks", "prompts", "artifacts", "connections", "gdrive", "gmail", "scrapes", "candidates", "campaigns", "ledger", "dummy"} {
		entries[name] = ln.Addr().String()
	}
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, entries, "")
	command, _ := integrationStub(t, "", "", "")
	r := startIntegration(t, integrationOptions{path: path, env: map[string]string{"ACCOUNTS_HOST_COMMAND": command}})
	for _, call := range changingCalls(validKey()) {
		r.call(call.name, call.args, nil)
	}
	r.call("groups", struct{}{}, nil)
	r.call("group", map[string]any{"group": "acme"}, nil)
	r.call("users", struct{}{}, nil)
	for _, path := range []string{"/", "/tools", "/about"} {
		code, _, _ := r.request("GET", path, "owner", "page"+path, nil)
		if code != 200 {
			t.Fatal(path, code)
		}
	}
	if code := r.stop(); code != cli.ExitSuccess {
		t.Fatal(code, r.err.String())
	}
	events := r.capture.Events()
	assertLifecycle(t, events, runVersion)
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.Name] = true
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
		r.call("groups", struct{}{}, nil)
		if r.stop() != cli.ExitSuccess {
			t.Fatal(r.err.String())
		}
	}
}

// R-UWQ1-IVO9 R-UXXX-WNEY R-EJOM-URNY
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
	r.call("create_group", map[string]any{"group": "acme"}, nil)
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

// R-AESO-HDDX
func TestRunFatalAccept(t *testing.T) {
	t.Setenv(services.Variable, "")
	var out, errout writes
	var writer *telemetry.Writer
	p := cli.Process{Args: nil, Dir: t.TempDir(), Pid: 79, LookupEnv: func(k string) (string, bool) {
		v, ok := map[string]string{"LISTEN_PID": "79", "LISTEN_FDS": "1", "IKIGENBA_SANDBOX": ""}[k]
		return v, ok
	}, Stdout: &out, Stderr: &errout, Version: runVersion, Inherit: func(uintptr) (net.Listener, error) { return failedListener{}, nil }, Now: func() time.Time { return fixedTime }, Sleep: func(context.Context, time.Duration) {}, Rand: integrationConstant(0), Sink: &telemetry.Capture{}, Banner: page.New(pages.ServiceName, version.Identity{}).Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		writer = w
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w})
	}}
	if code := cli.Run(context.Background(), p); code != cli.ExitServerFailed {
		t.Fatal(code)
	}
	before := errout.String()
	if out.Len() != 0 || errout.n != 1 || !strings.HasPrefix(before, "accounts: ") || !strings.Contains(before, "listener gone now") || strings.Count(before, "\n") != 1 {
		t.Fatal(out.String(), before, errout.n)
	}
	writer.Emit(context.Background(), "change.finished", telemetry.Attrs{})
	if errout.String() != before {
		t.Fatal("late output")
	}
}

// R-CDNO-3WT5
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
	r.call("create_group", map[string]any{"group": "acme"}, nil)
	if r.stop() != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 || events[0] != "service.started" || events[len(events)-1] != "service.stopping" {
		t.Fatal(events)
	}
}

// R-UXXX-WNEY
func TestRunDrainCancelsHostCommand(t *testing.T) {
	dir := t.TempDir()
	enteredPath := filepath.Join(dir, "entered")
	releasePath := filepath.Join(dir, "release")
	for _, path := range []string{enteredPath, releasePath} {
		if e := syscall.Mkfifo(path, 0600); e != nil {
			t.Fatal(e)
		}
	}
	entered, e := os.OpenFile(filepath.Clean(enteredPath), os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = entered.Close() }()
	release, e := os.OpenFile(filepath.Clean(releasePath), os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = release.Close() }()
	command := filepath.Join(dir, "host")
	script := "#!/bin/sh\nprintf x > '" + enteredPath + "'\nIFS= read -r line < '" + releasePath + "'\n"
	if e = writeExecutable(command, []byte(script)); e != nil {
		t.Fatal(e)
	}
	r := startIntegration(t, integrationOptions{env: map[string]string{"DRAIN_SECONDS": "1", "ACCOUNTS_HOST_COMMAND": command}})
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		_, _ = r.callAs("create_group", map[string]any{"group": "acme"}, identity.Caller{UserID: "owner", RequestID: "blocked-host"})
	}()
	received := make(chan error, 1)
	go func() { var b [1]byte; _, err := entered.Read(b[:]); received <- err }()
	select {
	case err := <-received:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("host not entered")
	}
	start := time.Now()
	if code := r.stop(); code != cli.ExitServerFailed || time.Since(start) > 3*time.Second {
		t.Error(code, time.Since(start))
	}
	want := "accounts: " + (&server.DrainError{Unfinished: 1}).Error() + "\n"
	before := r.err.String()
	if !strings.HasSuffix(before, want) {
		t.Error(before)
	}
	// The request must end without releasing the stub's blocking read.
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Error("host command survived request cancellation")
	}
	_, _ = release.Write([]byte("released\n"))
	r.writer.Emit(context.Background(), "change.finished", telemetry.Attrs{})
	if r.err.String() != before || r.out.Len() != 0 {
		t.Fatal("late output", r.err.String())
	}
}

func writeExecutable(path string, data []byte) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	if err = root.WriteFile(name, data, 0600); err != nil {
		return err
	}
	return root.Chmod(name, 0700)
}

// R-C9ZY-YLL2
func TestRunInjectedBanner(t *testing.T) {
	const chosen = "test-owned-banner"
	banner := func(u page.User) page.Banner {
		return page.Banner{Service: chosen, Release: chosen, Commit: chosen, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}
	r := startIntegration(t, integrationOptions{banner: banner})
	code, _, body := r.request("GET", "/about", "owner", "about-injection", nil)
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
	set.Write(want, req, 200, "about", pages.AboutData{Banner: b, Description: pages.Description})
	if code != 200 || body != want.Body.String() {
		t.Fatal("injected banner did not reach rendered page")
	}
	if r.stop() != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
}

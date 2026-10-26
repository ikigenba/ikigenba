package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/ledger/internal/cli"
	"github.com/ikigenba/ikigenba/ledger/internal/pages"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
)

var integrationTime = time.Date(2026, 10, 5, 9, 0, 0, 123456789, time.FixedZone("test", 3600))

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

type integrationRequest struct{ Method, Path, ID, User, Email string }
type integrationFake struct {
	mu       sync.Mutex
	requests []integrationRequest
	socket   string
}

func integrationSocketDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("", "ledger-")
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
func (f *integrationFake) snapshot() []integrationRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]integrationRequest(nil), f.requests...)
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
	bus                                 events.Sink
	nilSinks, nilUnset                  bool
	emptyDir                            bool
	env                                 map[string]string
	mcp                                 func(*telemetry.Writer) *mcp.Server
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
	bus         *events.Capture
	out, err    bytes.Buffer
	keys, unset []string
	sent        []string
	inherit     int
	mu          sync.Mutex
	stopped     bool
	notice      *net.UnixConn
}

func startIntegration(t *testing.T, o integrationOptions) *integrationRun {
	t.Helper()
	t.Setenv(services.Variable, o.path)
	r := &integrationRun{t: t, dir: o.dir, capture: &telemetry.Capture{}, bus: &events.Capture{}, done: make(chan int, 1)}
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
	r.base = "http://ledger.test"
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(sd, "app"))
	}}
	r.http = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 5 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	r.client = mcp.NewClient(mcp.ClientConfig{Endpoint: r.base + "/mcp", HTTPClient: r.http})
	env := map[string]string{"LISTEN_PID": "73", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notice.LocalAddr().String(), services.Variable: o.path}
	if o.noNotify {
		env["NOTIFY_SOCKET"] = ""
	}
	if o.unsetNotify {
		delete(env, "NOTIFY_SOCKET")
	}
	for k, v := range o.env {
		env[k] = v
	}
	p := cli.Process{nil, nil, nil, 73, &r.out, &r.err, "integration-display", nil, func() time.Time { return integrationTime }, func(ctx context.Context, _ time.Duration) {
		select {
		case <-ctx.Done():
		default:
		}
	}, o.random, r.dir, o.sink, o.bus, page.New(pages.ServiceName, version.Identity{}).Banner, o.mcp}
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
		if p.EventSink == nil {
			p.EventSink = r.bus
		}
	}
	if p.MCP == nil {
		p.MCP = func(w *telemetry.Writer) *mcp.Server {
			return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: p.Version, Telemetry: w})
		}
	}
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
	allowed := map[string]bool{"DRAIN_SECONDS": true, services.Variable: true, "LISTEN_PID": true, "LISTEN_FDS": true, "NOTIFY_SOCKET": true}
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
func (r *integrationRun) request(method, path, user, id string, headers map[string]string) (int, http.Header) {
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
	req.Host = "ledger.test"
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
	_, e = io.Copy(io.Discard, res.Body)
	if e != nil {
		r.t.Error(e)
	}
	if e = res.Body.Close(); e != nil {
		r.t.Error(e)
	}
	return res.StatusCode, res.Header
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

// R-68NF-39FL R-69VB-H16A R-6EQX-0452 R-6H6P-RNMG R-6IEM-5FD5 R-6M2B-AQL8 R-8NGD-0SO0 R-8PW5-SC5E R-8R42-63W3 R-8X7K-2YLK R-90V9-89TN
func TestRunPersistence(t *testing.T) {
	dir := t.TempDir()
	r := startIntegration(t, integrationOptions{dir: dir, acceptNotify: true})
	info, e := os.Stat(filepath.Join(dir, "state", "ledger.db"))
	if e != nil || !info.Mode().IsRegular() {
		t.Fatal(info, e)
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 1 || entries[0].Name() != "state" {
		t.Fatal(entries, e)
	}
	for _, n := range []string{"Assets:Bank", "Equity:Opening"} {
		r.call("open_account", map[string]any{"name": n, "date": "2026-10-05"}, nil)
	}
	r.call("post", map[string]any{"date": "2026-10-05", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "1.00"}, map[string]any{"account": "Equity:Opening", "amount": "-1.00"}}}, nil)
	checkTree := func() {
		entries, e := os.ReadDir(dir)
		if e != nil || len(entries) != 1 || entries[0].Name() != "state" {
			t.Fatal(entries, e)
		}
		files, e := os.ReadDir(filepath.Join(dir, "state"))
		if e != nil {
			t.Fatal(e)
		}
		for _, f := range files {
			if !strings.HasPrefix(f.Name(), "ledger.db") {
				t.Fatal(f.Name())
			}
		}
	}
	checkTree()
	if r.stop() != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	checkTree()
	if r.inherit != 1 || !reflect.DeepEqual(r.unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) || r.out.Len() != 0 || r.err.Len() != 0 {
		t.Fatal(r.inherit, r.unset, r.out.String(), r.err.String())
	}
	ev := r.capture.Events()
	if len(ev) < 2 || ev[0].Name != "service.started" || ev[0].RequestID != "" || ev[0].User != "" || fmt.Sprint(ev[0].Attrs["version"]) != "integration-display" || len(ev[0].Attrs) != 1 {
		t.Fatal(ev)
	}
	last := ev[len(ev)-1]
	if last.Name != "service.stopping" || fmt.Sprint(last.Attrs["reason"]) != "integration stop" || len(last.Attrs) != 1 || last.User != "" || last.RequestID != "" {
		t.Fatal(last)
	}
	if len(r.bus.Events()) != 1 || r.bus.Events()[0].Name != trail.EventTransactionPosted {
		t.Fatal(r.bus.Events())
	}
	r = startIntegration(t, integrationOptions{dir: dir})
	var got struct{ Accounts []struct{ Name string } }
	r.call("accounts", struct{}{}, &got)
	if len(got.Accounts) != 2 {
		t.Fatal(got)
	}
	r.stop()
	r = startIntegration(t, integrationOptions{})
	r.call("accounts", struct{}{}, &got)
	if len(got.Accounts) != 0 {
		t.Fatal(got)
	}
	r.stop()
}

// R-8SBY-JVMS R-B7NL-L9F4
func TestRunUnavailableArtifacts(t *testing.T) {
	for _, mode := range []string{"unset", "missing", "bad", "empty", "down"} {
		t.Run(mode, func(t *testing.T) {
			path := ""
			if mode != "unset" {
				path = filepath.Join(t.TempDir(), "services")
				if mode == "bad" {
					if e := os.WriteFile(path, []byte("broken"), 0600); e != nil {
						t.Fatal(e)
					}
				}
				if mode == "empty" || mode == "down" {
					entries := map[string]string{}
					if mode == "down" {
						entries["artifacts"] = filepath.Join(integrationSocketDir(t), "absent")
					}
					integrationServices(t, path, entries, "")
				}
			}
			r := startIntegration(t, integrationOptions{path: path})
			r.call("book", struct{}{}, nil)
			r.call("open_account", map[string]any{"name": "Assets:Bank", "date": "2026-10-05"}, nil)
			r.refuse("add_document", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "path": "receipts/office.pdf"}, "unavailable")
			var account struct{ Documents []any }
			r.call("account", map[string]any{"name": "Assets:Bank", "date": "2026-10-05"}, &account)
			if len(account.Documents) != 0 {
				t.Fatal(account)
			}
			r.stop()
			if mode != "down" {
				for _, e := range r.capture.Events() {
					if e.Name == "sibling.called" {
						t.Fatal(e)
					}
				}
			}
		})
	}
}

func newIntegrationFake(t *testing.T, _ bool) *integrationFake {
	t.Helper()
	f := &integrationFake{}
	f.socket = integrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, integrationRequest{r.Method, r.URL.Path, r.Header.Get("X-Request-Id"), r.Header.Get("X-User-Id"), r.Header.Get("X-User-Email")})
		f.mu.Unlock()
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "3")
			w.Header().Set("ETag", `"hash"`)
			w.Header().Set("Last-Modified", "Mon, 05 Oct 2026 09:00:00 GMT")
			w.WriteHeader(200)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var root struct {
			ID json.RawMessage `json:"id"`
		}
		if e := json.NewDecoder(r.Body).Decode(&root); e != nil {
			t.Error(e)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": root.ID, "result": map[string]any{"content": []any{}, "structuredContent": map[string]any{"id": "artifact-one", "path": "receipts/office.pdf", "content_type": "application/pdf", "bytes": 3, "sha256": "hash", "owner": "owner", "url": "https://artifacts.test/files/receipt", "created": "2026-10-05T09:00:00Z", "modified": "2026-10-05T09:00:00Z"}}})
	}))
	return f
}

// R-6JMI-J73U R-6KUE-WYUJ R-B8VH-Z15T R-8VZN-P6UV
func TestRunDeterministicAndPinned(t *testing.T) {
	f, other := newIntegrationFake(t, true), newIntegrationFake(t, true)
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"artifacts": f.socket, "auth": ""}, "https://auth.one.test")
	var rng integrationMutable
	rng.value.Store(0x5a)
	r := startIntegration(t, integrationOptions{path: path, random: &rng})
	r.call("open_account", map[string]any{"name": "Assets:Bank", "date": "2026-10-05"}, nil)
	r.call("open_account", map[string]any{"name": "Equity:Opening", "date": "2026-10-05"}, nil)
	var txn struct{ Transaction struct{ ID string } }
	r.call("post", map[string]any{"date": "2026-10-05", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "1.00"}, map[string]any{"account": "Equity:Opening", "amount": "-1.00"}}}, &txn)
	if txn.Transaction.ID != "txn_5a5a5a5a5a5a5a5a" {
		t.Fatal(txn)
	}
	for _, tc := range []struct {
		name, prefix string
		args         map[string]any
	}{{"assert_balance", "bal", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "amount": "1.00"}}, {"add_note", "note", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "text": "chosen note"}}, {"add_document", "doc", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "path": "receipts/office.pdf"}}} {
		var out map[string]json.RawMessage
		r.call(tc.name, tc.args, &out)
		var record struct{ ID string }
		if e := json.Unmarshal(func() json.RawMessage { b, _ := json.Marshal(out); return b }(), &record); e != nil || record.ID != tc.prefix+"_5a5a5a5a5a5a5a5a" {
			t.Fatal(out, e)
		}
	}
	before := len(f.snapshot())
	rng.value.Store(0x5b)
	integrationServices(t, path, map[string]string{"artifacts": other.socket, "auth": ""}, "https://auth.two.test")
	r.call("add_document", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "path": "receipts/office.pdf"}, nil)
	code, h := r.request("GET", "/", "", "", nil)
	if code != 302 || !strings.HasPrefix(h.Get("Location"), "https://auth.two.test/") {
		t.Fatal(code, h)
	}
	r.stop()
	if len(other.snapshot()) != 0 || len(f.snapshot()) != before+2 {
		t.Fatal(f.snapshot(), other.snapshot())
	}
	for _, q := range f.snapshot() {
		if q.User != "owner" || q.Email != "mgreenly+fake01@gmail.com" {
			t.Fatal(q)
		}
	}
}

// R-8ZNC-UI2Y R-8YFG-GQC9
func TestRunIntegrationDrainDeadline(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			entered, release := make(chan struct{}, count), make(chan struct{})
			requestFinished := make(chan struct{}, count)
			var writer *telemetry.Writer
			capture := &telemetry.Capture{}
			r := startIntegration(t, integrationOptions{env: map[string]string{"DRAIN_SECONDS": "1"}, sink: capture, mcp: func(w *telemetry.Writer) *mcp.Server {
				writer = w
				s := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "drain", Telemetry: w})
				mcp.AddTool(s, mcp.Tool[struct{}, struct{}]{Name: "wait", Description: "Wait for the fixture.", Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, _ struct{}) (struct{}, error) {
					entered <- struct{}{}
					go func() { <-ctx.Done(); requestFinished <- struct{}{} }()
					<-release
					return struct{}{}, nil
				}})
				return s
			}})
			var wg sync.WaitGroup
			for i := range count {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _ = r.callAs("wait", struct{}{}, identity.Caller{UserID: "owner", Email: "mgreenly+fake01@gmail.com", RequestID: fmt.Sprintf("blocked-%d", i)})
				}()
			}
			for range count {
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					close(release)
					t.Fatal("handler not entered")
				}
			}
			if code := r.stop(); code != cli.ExitServerFailed {
				t.Errorf("drain exit %d", code)
			}
			drainLine := fmt.Sprintf("ledger: stopped with %d requests unfinished\n", count)
			if count == 1 {
				drainLine = "ledger: stopped with 1 request unfinished\n"
			}
			if !strings.HasSuffix(r.err.String(), drainLine) {
				t.Errorf("last drain diagnostic %q", r.err.String())
			}
			before := len(capture.Events())
			writer.Emit(context.Background(), "request.started", telemetry.Attrs{})
			close(release)
			for range count {
				select {
				case <-requestFinished:
				case <-time.After(3 * time.Second):
					t.Fatal("blocked handler did not finish")
				}
			}
			wg.Wait()
			if len(capture.Events()) != before {
				t.Error("sink delivery began after drain deadline")
			}
			if !strings.Contains(r.err.String(), "undelivered event") {
				t.Error("late event not diagnosed")
			}
			if r.out.Len() != 0 {
				t.Error("unexpected stdout")
			}
		})
	}
}

// R-6B37-USWZ R-6CB4-8KNO
func TestRunClockAndDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var clock atomic.Int64
	clock.Store(integrationTime.Unix())
	r := startIntegration(t, integrationOptions{emptyDir: true, nilUnset: true, now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }})
	r.call("open_account", map[string]any{"name": "Assets:Bank", "date": "2026-10-05"}, nil)
	var out struct{ Accounts []struct{ Opened string } }
	r.call("accounts", struct{}{}, &out)
	if len(out.Accounts) != 1 || out.Accounts[0].Opened != integrationTime.UTC().Format(time.RFC3339) {
		t.Fatal(out)
	}
	var report struct{ Date string }
	r.call("balance_sheet", struct{}{}, &report)
	if report.Date != integrationTime.UTC().Format("2006-01-02") {
		t.Fatal(report)
	}
	r.call("open_account", map[string]any{"name": "Equity:Opening", "date": "2026-10-05"}, nil)
	clock.Add(1800)
	var posted struct{ Transaction struct{ ID, Posted string } }
	r.call("post", map[string]any{"date": "2026-10-05", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "1.00"}, map[string]any{"account": "Equity:Opening", "amount": "-1.00"}}}, &posted)
	var transaction struct{ Posted string }
	r.call("transaction", map[string]any{"transaction": posted.Transaction.ID}, &transaction)
	if transaction.Posted != time.Unix(clock.Load(), 0).UTC().Format(time.RFC3339) {
		t.Fatal(transaction)
	}
	clock.Add(86400)
	r.call("balance_sheet", struct{}{}, &report)
	if report.Date != "2026-10-06" {
		t.Fatal(report)
	}
	r.stop()
}

// R-6DJ0-MCED
func TestRunConcurrentRandom(t *testing.T) {
	rnd := &integrationRandom{}
	r := startIntegration(t, integrationOptions{random: rnd})
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, e := r.callAs("open_account", map[string]any{"name": fmt.Sprintf("Assets:A%d", i), "date": "2026-10-05"}, identity.Caller{UserID: "owner"})
			if e != nil || result.IsError() {
				t.Error(result, e)
			}
		}()
	}
	wg.Wait()
	r.stop()
	if rnd.collision.Load() {
		t.Fatal("concurrent random Read")
	}
}

// R-6NA7-OIBX
func TestRunOnlyArtifacts(t *testing.T) {
	var touched atomic.Int64
	socket := filepath.Join(integrationSocketDir(t), "trap")
	ln, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = ln.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			touched.Add(1)
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close(); <-done })
	trap := socket
	f := newIntegrationFake(t, true)
	entries := map[string]string{"artifacts": f.socket}
	for _, name := range []string{"auth", "telemetry", "mcp", "repos", "sites", "scripts", "events", "cron", "home", "webhooks", "prompts", "connections", "gdrive", "gmail", "scrapes", "candidates", "campaigns", "dummy"} {
		entries[name] = trap
	}
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, entries, "")
	r := startIntegration(t, integrationOptions{path: path})
	call := func(name string, args any) map[string]any {
		before := len(f.snapshot())
		var out map[string]any
		r.call(name, args, &out)
		if name != "add_document" && len(f.snapshot()) != before {
			t.Fatalf("%s reached artifacts", name)
		}
		return out
	}
	call("book", struct{}{})
	call("set_book", map[string]any{"fiscal_year_start": "02-01"})
	for _, n := range []string{"Assets:Bank", "Equity:Opening"} {
		call("open_account", map[string]any{"name": n, "date": "2026-10-05"})
	}
	call("accounts", struct{}{})
	call("account", map[string]any{"name": "Assets:Bank"})
	posted := call("post", map[string]any{"date": "2026-10-05", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "1.00"}, map[string]any{"account": "Equity:Opening", "amount": "-1.00"}}})
	id := posted["transaction"].(map[string]any)["id"].(string)
	call("transaction", map[string]any{"transaction": id})
	call("annotate", map[string]any{"transaction": id, "flag": "cleared"})
	call("history", map[string]any{"transaction": id})
	call("reverse", map[string]any{"transaction": id})
	assertion := call("assert_balance", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "amount": "0.00"})
	call("check", struct{}{})
	call("retract_assertion", map[string]any{"assertion": assertion["id"]})
	note := call("add_note", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "text": "chosen"})
	call("retract_note", map[string]any{"note": note["id"]})
	doc := call("add_document", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "path": "receipts/office.pdf"})
	call("retract_document", map[string]any{"document": doc["id"]})
	call("journal", struct{}{})
	call("register", map[string]any{"account": "Assets:Bank"})
	call("balances", struct{}{})
	call("balance_sheet", struct{}{})
	call("income_statement", map[string]any{"from": "2026-01-01", "to": "2026-10-05"})
	call("close_account", map[string]any{"name": "Assets:Bank", "date": "2026-10-06"})
	for _, p := range []string{"/", "/tools", "/about"} {
		code, _ := r.request("GET", p, "owner", "page", nil)
		if code != 200 {
			t.Fatal(code)
		}
	}
	r.stop()
	if touched.Load() != 0 {
		t.Fatal("other sibling reached", touched.Load())
	}
	if len(f.snapshot()) != 2 {
		t.Fatal(f.snapshot())
	}
}

type integrationMutable struct{ value atomic.Int64 }

func (r *integrationMutable) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r.value.Load() & 255)
	}
	return len(p), nil
}

type integrationBlockingBus struct {
	calls   atomic.Int32
	entered chan struct{}
}

func (s *integrationBlockingBus) Deliver(ctx context.Context, _ events.Event) error {
	if s.calls.Add(1) == 1 {
		close(s.entered)
	}
	<-ctx.Done()
	return ctx.Err()
}

// R-8YFG-GQC9
func TestRunBusDrainDeadline(t *testing.T) {
	sink := &integrationBlockingBus{entered: make(chan struct{})}
	r := startIntegration(t, integrationOptions{bus: sink, env: map[string]string{"DRAIN_SECONDS": "1"}})
	for _, n := range []string{"Assets:Bank", "Equity:Opening"} {
		r.call("open_account", map[string]any{"name": n, "date": "2026-10-05"}, nil)
	}
	r.call("post", map[string]any{"date": "2026-10-05", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "1.00"}, map[string]any{"account": "Equity:Opening", "amount": "-1.00"}}}, nil)
	r.call("post", map[string]any{"date": "2026-10-05", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "2.00"}, map[string]any{"account": "Equity:Opening", "amount": "-2.00"}}}, nil)
	select {
	case <-sink.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("event sink not entered")
	}
	if r.stop() != 0 {
		t.Fatal(r.err.String())
	}
	if sink.calls.Load() != 1 || !strings.Contains(r.err.String(), "lost event") {
		t.Fatal(sink.calls.Load(), r.err.String())
	}
}

// R-6B37-USWZ
func TestRunSocketSinks(t *testing.T) {
	var mu sync.Mutex
	var names []string
	busCount := 0
	telemetrySocket := integrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e struct {
			Event string `json:"event"`
		}
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Error(err)
		}
		mu.Lock()
		names = append(names, e.Event)
		mu.Unlock()
		w.WriteHeader(204)
	}))
	busSocket := integrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e events.Event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Error(err)
		}
		mu.Lock()
		busCount++
		mu.Unlock()
		w.WriteHeader(204)
	}))
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"telemetry": telemetrySocket, "events": busSocket}, "")
	r := startIntegration(t, integrationOptions{path: path, nilSinks: true, nilUnset: true})
	for _, n := range []string{"Assets:Bank", "Equity:Opening"} {
		r.call("open_account", map[string]any{"name": n, "date": "2026-10-05"}, nil)
	}
	r.call("post", map[string]any{"date": "2026-10-05", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "1.00"}, map[string]any{"account": "Equity:Opening", "amount": "-1.00"}}}, nil)
	if r.stop() != 0 {
		t.Fatal(r.err.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if busCount != 1 || len(names) < 2 || names[0] != "service.started" || names[len(names)-1] != "service.stopping" {
		t.Fatal(busCount, names)
	}
}

// R-8PW5-SC5E
func TestRunNoNotification(t *testing.T) {
	for _, unset := range []bool{false, true} {
		r := startIntegration(t, integrationOptions{noNotify: !unset, unsetNotify: unset})
		r.call("book", struct{}{}, nil)
		if r.stop() != 0 {
			t.Fatal(r.err.String())
		}
	}
}

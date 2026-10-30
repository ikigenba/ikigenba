package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/campaigns"
	"github.com/ikigenba/ikigenba/campaigns/internal/cli"
	"github.com/ikigenba/ikigenba/campaigns/internal/links"
	"github.com/ikigenba/ikigenba/campaigns/internal/pages"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/tools"
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
	failureText  string
	mu           sync.Mutex
	requests     []integrationRequest
	fail         bool
	block        <-chan struct{}
	entered      chan struct{}
	business     map[string]any
	prospectList []string
	socket       string
}

func integrationSocketDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("", "campaigns-")
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
func newIntegrationFake(t *testing.T, artifact bool) *integrationFake {
	t.Helper()
	f := &integrationFake{business: map[string]any{"name": "Fixture business", "names": []string{"Fixture alias"}, "street": "100 Test Street", "city": "Test City", "state": "MN", "zip": "55001", "county": "Test County", "phone": "8005550100", "email": "mgreenly+fake00@gmail.com", "people": []map[string]string{{"name": "Fixture Person", "role": "owner"}}}, prospectList: []string{"cand_0000000000000001", "cand_0000000000000002", "cand_0000000000000003"}}
	f.socket = integrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, integrationRequest{r.Method, r.URL.Path, r.Header.Get("X-Request-Id"), r.Header.Get("X-User-Id"), r.Header.Get("X-User-Email")})
		fail := f.fail
		failureText := f.failureText
		block := f.block
		entered := f.entered
		f.mu.Unlock()
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		if block != nil {
			<-block
		}
		if fail {
			w.WriteHeader(500)
			_, _ = fmt.Fprint(w, failureText)
			return
		}
		if artifact && r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "3")
			w.Header().Set("ETag", `"hash"`)
			w.Header().Set("Last-Modified", "Mon, 05 Oct 2026 09:00:00 GMT")
			w.WriteHeader(200)
			return
		}
		var root struct {
			ID     json.RawMessage `json:"id"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if e := json.NewDecoder(r.Body).Decode(&root); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		var content any
		if artifact {
			content = map[string]any{"id": "artifact-one", "path": root.Params.Arguments["path"], "content_type": "text/plain", "sha256": "hash", "owner": "owner", "url": "https://artifacts.test/files/material", "bytes": 3, "created": "2026-10-05T09:00:00Z", "modified": "2026-10-05T09:00:00Z"}
		} else {
			business := func(id string) map[string]any {
				b := map[string]any{"id": id, "status": "active"}
				for k, v := range f.business {
					b[k] = v
				}
				return b
			}
			switch root.Params.Name {
			case "prospect_list":
				members := []any{}
				for _, id := range f.prospectList {
					members = append(members, map[string]any{"candidate": business(id)})
				}
				content = map[string]any{"members": members}
			case "candidate":
				content = map[string]any{"candidate": business(fmt.Sprint(root.Params.Arguments["candidate"]))}
			default:
				t.Errorf("unexpected candidates tool %q", root.Params.Name)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": root.ID, "result": map[string]any{"content": []any{}, "structuredContent": content}})
	}))
	return f
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
	socketDir                           string
	acceptNotify, noNotify, unsetNotify bool
	dir, path                           string
	random                              io.Reader
	sweepAfter                          func(time.Duration) <-chan time.Time
	now                                 func() time.Time
	sink                                telemetry.Sink
	bus                                 events.Sink
	nilSinks, nilUnset                  bool
	emptyDir                            bool
	env                                 map[string]string
	mcp                                 func(*telemetry.Writer) *mcp.Server
}
type integrationBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *integrationBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *integrationBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func (b *integrationBuffer) Len() int       { return len(b.String()) }
func (b *integrationBuffer) Bytes() []byte  { return []byte(b.String()) }

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
	out, err    integrationBuffer
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
	r.base = "http://campaigns.test"
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
	p := cli.Process{Pid: 73, Dir: r.dir, Version: "integration-display", Stdout: &r.out, Stderr: &r.err, Now: func() time.Time { return integrationTime }, Rand: o.random, Sink: o.sink, EventSink: o.bus, Banner: page.New(pages.ServiceName, version.Identity{}).Banner, MCP: o.mcp, Sleep: func(ctx context.Context, _ time.Duration) {
		select {
		case <-ctx.Done():
		default:
		}
	}}
	if o.now != nil {
		p.Now = o.now
	}
	p.SweepAfter = o.sweepAfter
	if p.SweepAfter == nil {
		p.SweepAfter = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
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
	allowed := map[string]bool{"DRAIN_SECONDS": true, "CAMPAIGNS_DEFAULT_DESTINATION": true, "CAMPAIGNS_LINK_PREFIX": true, services.Variable: true, "LISTEN_PID": true, "LISTEN_FDS": true, "NOTIFY_SOCKET": true}
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
	req.Host = "campaigns.test"
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
func (r *integrationRun) create(name string) tools.Campaign {
	r.t.Helper()
	var c tools.Campaign
	r.call("create_campaign", map[string]any{"name": name, "description": name, "channels": []string{"mail"}, "destination": "https://destination.test/"}, &c)
	return c
}
func integrationTree(t *testing.T, d string) {
	t.Helper()
	entries, e := os.ReadDir(d)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 1 || entries[0].Name() != "state" || !entries[0].IsDir() {
		t.Fatalf("working directory entries: %v", entries)
	}
	entries, e = os.ReadDir(filepath.Join(d, "state"))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, x := range entries {
		if !strings.HasPrefix(x.Name(), "campaigns.db") || !x.Type().IsRegular() {
			t.Errorf("unexpected state entry %s", x.Name())
		}
		found = found || x.Name() == "campaigns.db"
	}
	if !found {
		t.Fatal("database missing")
	}
}

// R-AFLX-GRUQ R-AI1Q-8BC4 R-AJ9M-M32T R-AKHI-ZUTI R-QC0W-ZYIO R-QEGP-RI02 R-QFOM-59QR R-QO7W-TNXM R-ZGSL-30QA
func TestRunIntegrationPersistence(t *testing.T) {
	d := t.TempDir()
	r := startIntegration(t, integrationOptions{dir: d, acceptNotify: true})
	integrationTree(t, d)
	c := r.create("Persisted")
	if c.Created != integrationTime.UTC().Format(time.RFC3339) {
		t.Fatalf("created time %s", c.Created)
	}
	integrationTree(t, d)
	var read tools.CampaignRead
	r.call("campaign", map[string]any{"campaign": c.ID}, &read)
	if read.Campaign.Created != c.Created {
		t.Fatal("clock not passed to store")
	}
	if code := r.stop(); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	integrationTree(t, d)
	if r.inherit != 1 || !reflect.DeepEqual(r.unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatalf("inherit/unset %d %v", r.inherit, r.unset)
	}
	if r.out.Len() != 0 || r.err.Len() != 0 {
		t.Fatalf("streams %s %s", r.out.String(), r.err.String())
	}
	ev := r.capture.Events()
	if len(ev) < 2 || ev[0].Name != "service.started" || ev[0].RequestID != "" || ev[0].User != "" || !reflect.DeepEqual(ev[0].Attrs, telemetry.Attrs{"version": "integration-display"}) {
		t.Fatalf("first event %+v", ev)
	}
	stopping := 0
	for _, event := range ev {
		if event.Name == "service.stopping" {
			stopping++
		}
	}
	if stopping != 1 {
		t.Errorf("service.stopping count %d", stopping)
	}
	last := ev[len(ev)-1]
	if last.Name != "service.stopping" || last.RequestID != "" || last.User != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "integration stop"}) {
		t.Fatalf("last event %+v", last)
	}
	for _, tc := range []struct {
		dir  string
		want int
	}{{d, 1}, {t.TempDir(), 0}} {
		next := startIntegration(t, integrationOptions{dir: tc.dir, nilUnset: true})
		var list tools.CampaignList
		next.call("campaigns", map[string]any{}, &list)
		if len(list.Campaigns) != tc.want {
			t.Errorf("persisted list %+v", list)
		}
		if tc.want == 1 && list.Campaigns[0].ID != c.ID {
			t.Error("wrong campaign restored")
		}
		if code := next.stop(); code != 0 {
			t.Fatal(code)
		}
	}
}

// R-AFLX-GRUQ
func TestRunIntegrationEmptyDirAndSocketSinks(t *testing.T) {
	d := t.TempDir()
	t.Chdir(d)
	var mu sync.Mutex
	var names []string
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
	busCount := 0
	busSocket := integrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var event events.Event
		if err := json.NewDecoder(req.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		mu.Lock()
		busCount++
		mu.Unlock()
		w.WriteHeader(204)
	}))
	fake := newIntegrationFake(t, false)
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"telemetry": telemetrySocket, "events": busSocket, "candidates": fake.socket}, "")
	r := startIntegration(t, integrationOptions{dir: ".", path: path, nilSinks: true, nilUnset: true, emptyDir: true})
	c := r.create("Default sinks")
	r.call("add_member", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000001"}, nil)
	if code := r.stop(); code != 0 {
		t.Fatal(code)
	}
	integrationTree(t, d)
	mu.Lock()
	defer mu.Unlock()
	if busCount != 1 {
		t.Errorf("default event socket delivered %d", busCount)
	}
	if len(names) < 2 || names[0] != "service.started" || names[len(names)-1] != "service.stopping" {
		t.Fatalf("socket sink events %v", names)
	}
}

// R-QGWI-J1HG R-QI4E-WT85
func TestRunIntegrationMissingSiblings(t *testing.T) {
	for _, kind := range []string{"unset", "missing", "invalid", "empty", "down"} {
		t.Run(kind, func(t *testing.T) {
			path := ""
			if kind != "unset" {
				path = filepath.Join(t.TempDir(), "services")
				switch kind {
				case "invalid":
					if e := os.WriteFile(path, []byte("invalid"), 0600); e != nil {
						t.Fatal(e)
					}
				case "empty":
					integrationServices(t, path, map[string]string{}, "")
				case "down":
					integrationServices(t, path, map[string]string{"candidates": filepath.Join(integrationSocketDir(t), "absent")}, "")
				}
			}
			r := startIntegration(t, integrationOptions{path: path})
			r.call("campaigns", map[string]any{}, nil)
			c := r.create("No sibling")
			r.refuse("add_member", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000001"}, "unavailable")
			r.refuse("add_material", map[string]any{"campaign": c.ID, "path": "material", "label": "Fixture"}, "unavailable")
			if code := r.stop(); code != 0 {
				t.Fatal(code)
			}
			if kind != "down" {
				for _, e := range r.capture.Events() {
					if e.Name == "sibling.called" {
						t.Errorf("absent sibling called: %+v", e)
					}
				}
			}
			if r.out.Len() != 0 || r.err.Len() != 0 {
				t.Errorf("streams %q %q", r.out.String(), r.err.String())
			}
		})
	}
}

// R-QJCB-AKYU R-QKK7-OCPJ R-CZLD-15G0
func TestRunIntegrationPinnedClientsAndFreshAuth(t *testing.T) {
	c1, c2 := newIntegrationFake(t, false), newIntegrationFake(t, false)
	a1, a2 := newIntegrationFake(t, true), newIntegrationFake(t, true)
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"candidates": c1.socket, "artifacts": a1.socket, "auth": ""}, "https://auth.one.test")
	r := startIntegration(t, integrationOptions{path: path})
	c := r.create("Pinned")
	r.call("add_member", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000001"}, nil)
	r.call("add_material", map[string]any{"campaign": c.ID, "path": "first", "label": "Fixture"}, nil)
	integrationServices(t, path, map[string]string{"candidates": c2.socket, "artifacts": a2.socket, "auth": ""}, "https://auth.two.test")
	r.call("add_member", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000002"}, nil)
	r.call("add_material", map[string]any{"campaign": c.ID, "path": "second", "label": "Fixture"}, nil)
	status, h := r.request("GET", "/", "", "auth-refresh", nil)
	if status != 302 || !strings.HasPrefix(h.Get("Location"), "https://auth.two.test/?return=") {
		t.Fatalf("fresh auth %d %s", status, h.Get("Location"))
	}
	if code := r.stop(); code != 0 {
		t.Fatal(code)
	}
	if len(c1.snapshot()) != 2 || len(a1.snapshot()) != 4 || len(c2.snapshot()) != 0 || len(a2.snapshot()) != 0 {
		t.Fatalf("client pinning %d %d %d %d", len(c1.snapshot()), len(a1.snapshot()), len(c2.snapshot()), len(a2.snapshot()))
	}
	for _, f := range []*integrationFake{c1, a1} {
		for _, q := range f.snapshot() {
			if q.User != "owner" || q.Email != "mgreenly+fake01@gmail.com" {
				t.Errorf("identity %+v", q)
			}
		}
	}
}

// R-ALPF-DMK7 R-5R00-E98R R-AMXB-REAW
func TestRunIntegrationDeterminism(t *testing.T) {
	f := newIntegrationFake(t, false)
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"candidates": f.socket}, "")
	r := startIntegration(t, integrationOptions{path: path, random: integrationConstant(0x5a)})
	c := r.create("Deterministic")
	if c.ID != "camp_5a5a5a5a5a5a5a5a" {
		t.Fatal(c.ID)
	}
	r.call("add_member", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000001"}, nil)
	r.request("GET", "/about", "owner", "", nil)
	if code := r.stop(); code != 0 {
		t.Fatal(code)
	}
	es := r.capture.Events()
	if es[0].Name != "service.started" || es[len(es)-1].Name != "service.stopping" {
		t.Fatal("lifecycle capture")
	}
	started, finished := 0, 0
	for _, e := range es {
		if !e.Time.Equal(integrationTime.UTC().Truncate(time.Microsecond)) || e.Time.Location() != time.UTC {
			t.Errorf("time %+v", e)
		}
		if e.Name == "request.started" && e.Attrs["path"] == "/about" {
			started++
			if e.RequestID != strings.Repeat("5a", 16) {
				t.Error(e.RequestID)
			}
		}
		if e.Name == "request.finished" && e.RequestID == strings.Repeat("5a", 16) {
			finished++
		}
	}
	if started != 1 || finished != 1 {
		t.Fatalf("minted request pair: started %d finished %d", started, finished)
	}
	bus := r.bus.Events()
	if len(bus) != 1 || bus[0].Name != "campaign.member.added" || bus[0].ID != "evt_5a5a5a5a5a5a5a5a" || !bus[0].Time.Equal(integrationTime.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("bus %+v", bus)
	}
}

type integrationHeaderTransport struct{ base http.RoundTripper }

func (h integrationHeaderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Event-Cause", "evt_8c3f1a6e2d9b4075")
	r.Header.Set("X-Event-Depth", "1")
	return h.base.RoundTrip(r)
}
func integrationMarker(n int) string { return fmt.Sprintf("G%07dH%07dJ%07d", n, n, n) }
func integrationToken(t *testing.T, link string) string {
	t.Helper()
	u, e := url.Parse(link)
	if e != nil {
		t.Fatal(e)
	}
	token := u.Query().Get("t")
	if token == "" {
		t.Fatalf("link lacks token %s", link)
	}
	return token
}

// R-5BD2-EN8H R-POTZ-2NHP R-PQ1V-GF8E R-2PRN-RP1S R-4B2X-QWWF R-GMS2-C1J8 R-D6X2-J44K R-D84Y-WVV9 R-D9CV-ANLY R-4CAU-4ON4 R-4DIQ-IGDT R-4EQM-W84I R-Q4PI-PC2I
func TestRunIntegrationAllToolsPrivacyAndTrail(t *testing.T) {
	candidatesFake, artifactsFake := newIntegrationFake(t, false), newIntegrationFake(t, true)
	markers := []string{}
	marker := func() string { m := integrationMarker(len(markers) + 1); markers = append(markers, m); return m }
	for _, key := range []string{"name", "street", "city", "state", "zip", "county", "email"} {
		candidatesFake.business[key] = marker()
	}
	candidatesFake.business["names"] = []string{marker(), marker()}
	candidatesFake.business["people"] = []map[string]string{{"name": marker(), "role": marker()}}
	phone := "8765432109"
	candidatesFake.business["phone"] = phone
	name, desc, destination, label := marker(), marker(), "https://"+marker()+".example/", marker()
	visitHeaders := map[string]string{"User-Agent": marker(), "Referer": marker(), "X-Real-IP": marker()}
	queryMarker := marker()
	candidatesFake.failureText = strings.Join(append(append([]string{}, markers...), phone), " ")
	var otherConnections atomic.Int32

	// Count at Accept, rather than HTTP, so an unexpected connection cannot hide behind an unfinished request.
	otherListener, e := net.Listen("unix", filepath.Join(integrationSocketDir(t), "others"))
	if e != nil {
		t.Fatal(e)
	}
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		for {
			c, err := otherListener.Accept()
			if err != nil {
				return
			}
			otherConnections.Add(1)
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = otherListener.Close(); <-otherDone })
	entries := map[string]string{"candidates": candidatesFake.socket, "artifacts": artifactsFake.socket}
	for _, n := range []string{"auth", "telemetry", "mcp", "repos", "sites", "scripts", "events", "cron", "home", "webhooks", "prompts", "connections", "gdrive", "gmail", "scrapes", "dummy"} {
		entries[n] = otherListener.Addr().String()
	}
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, entries, "")
	r := startIntegration(t, integrationOptions{path: path})
	r.http.Transport = integrationHeaderTransport{r.http.Transport}
	var campaign tools.Campaign
	r.call("create_campaign", map[string]any{"name": name, "description": desc, "channels": []string{"mail", "email"}, "destination": destination}, &campaign)
	id := campaign.ID
	r.call("add_members", map[string]any{"campaign": id, "candidates": []string{"cand_0000000000000004"}}, nil)
	r.call("add_prospect_list", map[string]any{"campaign": id, "prospect_list": "pl_0000000000000001"}, nil)
	r.call("add_member", map[string]any{"campaign": id, "candidate": "cand_0000000000000004"}, nil)
	var merge tools.MergeList
	r.call("merge_list", map[string]any{"campaign": id, "channel": "mail", "all": true}, &merge)
	if len(merge.Rows) != 4 {
		t.Fatalf("merge rows %d", len(merge.Rows))
	}
	rows := map[string]tools.MergeRow{}
	tokens := []string{}
	for _, row := range merge.Rows {
		rows[row.Candidate] = row
		tokens = append(tokens, integrationToken(t, row.Link))
	}
	r.call("deliver", map[string]any{"piece": rows["cand_0000000000000001"].Piece}, nil)
	r.call("drop", map[string]any{"piece": rows["cand_0000000000000002"].Piece}, nil)
	r.call("reject", map[string]any{"campaign": id, "candidate": "cand_0000000000000003", "why": "not appropriate"}, nil)
	var delivered tools.Delivered
	r.call("deliver_channel", map[string]any{"campaign": id, "channel": "email"}, &delivered)
	if len(delivered.Pieces) != 3 {
		t.Fatalf("channel deliveries %d", len(delivered.Pieces))
	}
	r.call("edit_campaign", map[string]any{"campaign": id, "name": name, "description": desc, "destination": destination}, nil)
	r.call("add_material", map[string]any{"campaign": id, "path": "material", "label": label}, nil)
	r.call("remove_material", map[string]any{"campaign": id, "path": "material"}, nil)
	r.call("campaigns", map[string]any{}, nil)
	r.call("campaign", map[string]any{"campaign": id}, nil)
	r.call("engagement", map[string]any{"piece": rows["cand_0000000000000001"].Piece}, nil)
	var doomed tools.Campaign
	r.call("create_campaign", map[string]any{"name": name, "description": desc, "channels": []string{"mail"}, "destination": destination}, &doomed)
	r.call("add_member", map[string]any{"campaign": doomed.ID, "candidate": "cand_0000000000000005"}, nil)
	var doomedMerge tools.MergeList
	r.call("merge_list", map[string]any{"campaign": doomed.ID, "channel": "mail", "all": true}, &doomedMerge)
	deletedToken := integrationToken(t, doomedMerge.Rows[0].Link)
	tokens = append(tokens, deletedToken)
	r.call("delete_campaign", map[string]any{"campaign": doomed.ID}, nil)
	// Read the email links too: secrecy covers every token minted, including those not visited.
	var emailMerge tools.MergeList
	r.call("merge_list", map[string]any{"campaign": id, "channel": "email", "all": true}, &emailMerge)
	for _, row := range emailMerge.Rows {
		tokens = append(tokens, integrationToken(t, row.Link))
	}
	for _, tc := range []struct {
		name string
		args any
	}{
		{"campaigns", map[string]any{"limit": 0}}, {"campaign", map[string]any{"campaign": "bad"}}, {"create_campaign", map[string]any{"name": name, "description": desc, "channels": []string{}, "destination": destination}}, {"edit_campaign", map[string]any{"campaign": "bad", "name": name, "description": desc, "destination": destination}}, {"delete_campaign", map[string]any{"campaign": "bad"}}, {"add_prospect_list", map[string]any{"campaign": "bad", "prospect_list": "pl_0000000000000001"}}, {"add_member", map[string]any{"campaign": "bad", "candidate": "cand_0000000000000001"}}, {"add_members", map[string]any{"campaign": "bad", "candidates": []string{"cand_0000000000000001"}}}, {"reject", map[string]any{"campaign": "bad", "candidate": "cand_0000000000000001", "why": "no"}}, {"drop", map[string]any{"piece": "bad"}}, {"deliver", map[string]any{"piece": "bad"}}, {"deliver_channel", map[string]any{"campaign": "bad", "channel": "mail"}}, {"add_material", map[string]any{"campaign": "bad", "path": "material", "label": label}}, {"remove_material", map[string]any{"campaign": "bad", "path": "material"}}, {"merge_list", map[string]any{"campaign": "bad", "channel": "mail"}}, {"engagement", map[string]any{}},
	} {
		r.refuse(tc.name, tc.args, "")
	}
	candidatesFake.mu.Lock()
	candidatesFake.fail = true
	candidatesFake.mu.Unlock()
	r.refuse("add_member", map[string]any{"campaign": id, "candidate": "cand_0000000000000006"}, "unavailable")
	r.refuse("add_prospect_list", map[string]any{"campaign": id, "prospect_list": "pl_0000000000000002"}, "unavailable")
	candidatesFake.mu.Lock()
	candidatesFake.fail = false
	candidatesFake.mu.Unlock()
	type expectedVisit struct{ campaign, piece string }
	visits := map[string]expectedVisit{}
	requests := map[string]integrationRequest{}
	for i, candidate := range []string{"cand_0000000000000001", "cand_0000000000000002", "cand_0000000000000003", "cand_0000000000000004"} {
		row := rows[candidate]
		token := integrationToken(t, row.Link)
		for j, method := range []string{"GET", "HEAD"} {
			q := fmt.Sprintf("visit-%d-%d", i, j)
			user := ""
			if j == 1 {
				user = "owner"
			}
			p := "/t/" + token
			if i == 0 && j == 1 {
				p = "/t/" + strings.ToUpper(token)
			}
			status, _ := r.request(method, p+"?r="+queryMarker, user, q, visitHeaders)
			if status != 302 {
				t.Fatal(status)
			}
			visits[q] = expectedVisit{id, row.Piece}
			requests[q] = integrationRequest{Method: method, Path: p, ID: q, User: user}
		}
	}
	// A repeated open is a separate visit.
	token := integrationToken(t, rows["cand_0000000000000001"].Link)
	status, _ := r.request("GET", "/t/"+token+"?r="+queryMarker, "", "visit-repeat", visitHeaders)
	if status != 302 {
		t.Fatal(status)
	}
	visits["visit-repeat"] = expectedVisit{id, rows["cand_0000000000000001"].Piece}
	requests["visit-repeat"] = integrationRequest{Method: "GET", Path: "/t/" + token, ID: "visit-repeat"}
	for i, tc := range []struct{ method, path string }{{"GET", "/t/unknown"}, {"HEAD", "/t/unknown"}, {"GET", "/t/" + deletedToken}, {"HEAD", "/t/" + deletedToken}, {"POST", "/t/" + token}, {"GET", "/t/" + token + "/x"}, {"GET", "/t/"}} {
		q := fmt.Sprintf("no-visit-%d", i)
		r.request(tc.method, tc.path+"?r="+queryMarker, "", q, visitHeaders)
		requests[q] = integrationRequest{Method: tc.method, Path: tc.path, ID: q}
	}
	for i, p := range []string{"/", "/campaigns/" + id, "/tools", "/about", "/missing"} {
		q := fmt.Sprintf("page-%d", i)
		status, _ := r.request("GET", p, "owner", q, nil)
		want := 200
		if i == 4 {
			want = 404
		}
		if status != want {
			t.Errorf("page %s: %d", p, status)
		}
		requests[q] = integrationRequest{Method: "GET", Path: p, ID: q, User: "owner"}
	}
	r.request("POST", "/mcp", "", "guest-mcp", nil)
	requests["guest-mcp"] = integrationRequest{Method: "POST", Path: "/mcp", ID: "guest-mcp"}
	r.request("GET", "/declarations", "", "declarations", nil)
	requests["declarations"] = integrationRequest{Method: "GET", Path: "/declarations", ID: "declarations"}
	if code := r.stop(); code != 0 {
		t.Fatal(code)
	}
	if otherConnections.Load() != 0 {
		t.Fatalf("unexpected sibling connections %d", otherConnections.Load())
	}
	if len(candidatesFake.snapshot()) == 0 || len(artifactsFake.snapshot()) != 2 {
		t.Fatal("sibling fakes not exercised")
	}
	if r.out.Len() != 0 || r.err.Len() != 0 {
		t.Fatalf("streams %s %s", r.out.String(), r.err.String())
	}
	trail := r.capture.Events()
	bus := r.bus.Events()
	byRequest := map[string][]telemetry.Event{}
	closed := map[string]bool{}
	for _, n := range []string{"service.started", "service.stopping", "request.started", "request.finished", "tool.called", "event.lost", "sibling.called", "campaign.member.added", "campaign.member.rejected", "campaign.piece.delivered", "campaign.member.opened", "campaign.visited"} {
		closed[n] = true
	}
	visitCounts := map[string]int{}
	for _, event := range trail {
		if !closed[event.Name] {
			t.Errorf("unrecognized event %+v", event)
		}
		if event.RequestID != "" {
			byRequest[event.RequestID] = append(byRequest[event.RequestID], event)
		}
		if event.Name == "sibling.called" && event.Attrs["target"] != "candidates" && event.Attrs["target"] != "artifacts" {
			t.Error("other sibling", event)
		}
		if event.Name == "campaign.visited" {
			visitCounts[event.RequestID]++
			want, ok := visits[event.RequestID]
			if !ok || event.User != "" || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"campaign": want.campaign, "piece": want.piece}) {
				t.Errorf("visited %+v", event)
			}
		}
	}
	for q := range visits {
		if visitCounts[q] != 1 {
			t.Errorf("visit %s count %d", q, visitCounts[q])
		}
	}
	for _, q := range r.sent {
		starts, finishes := 0, 0
		for _, event := range byRequest[q] {
			if event.Name == "request.started" {
				starts++
			}
			if event.Name == "request.finished" {
				finishes++
			}
		}
		if starts != 1 || finishes != 1 {
			t.Errorf("sent request %s pair: started %d finished %d", q, starts, finishes)
		}
	}
	for q, es := range byRequest {
		first, last := es[0], es[len(es)-1]
		if first.Name != "request.started" || last.Name != "request.finished" {
			t.Errorf("request order %s: %+v", q, es)
			continue
		}
		starts, finishes := 0, 0
		for _, event := range es {
			if event.Name == "request.started" {
				starts++
			}
			if event.Name == "request.finished" {
				finishes++
			}
			if event.Name != "campaign.visited" && event.Name != "campaign.member.opened" && event.User != first.User {
				t.Errorf("request user %+v", event)
			}
		}
		if starts != 1 || finishes != 1 {
			t.Errorf("request pair %s %d %d", q, starts, finishes)
		}
		if expected, ok := requests[q]; ok {
			if first.User != expected.User || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"method": expected.Method, "path": expected.Path}) {
				t.Errorf("request metadata %+v", first)
			}
			status := int64(302)
			if strings.HasPrefix(q, "page-") {
				status = 200
				if q == "page-4" {
					status = 404
				}
			}
			if q == "guest-mcp" {
				status = 500
			}
			if q == "declarations" {
				status = 200
			}

			if last.Attrs["status"] != status {
				t.Errorf("request status %s %+v want %d", q, last, status)
			}
		} else if !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"method": "POST", "path": "/mcp"}) || last.Attrs["status"] != int64(200) {
			t.Errorf("tool request pair %+v %+v", first, last)
		}
	}
	changeKeys := map[string][]string{"campaign.member.added": {"campaign", "candidate", "prospect_list"}, "campaign.member.rejected": {"campaign", "candidate", "reason"}, "campaign.piece.delivered": {"campaign", "candidate", "piece", "channel", "how"}}
	changeKeys["campaign.member.opened"] = []string{"campaign", "candidate", "at"}
	changes := []telemetry.Event{}
	for _, event := range trail {
		if _, ok := changeKeys[event.Name]; ok {
			changes = append(changes, event)
		}
	}
	if len(changes) != len(bus) {
		t.Fatalf("trail/bus counts %d %d", len(changes), len(bus))
	}
	matched := map[int]bool{}
	channelCount := 0
	for _, change := range changes {
		keys := changeKeys[change.Name]
		if len(change.Attrs) != len(keys) {
			t.Errorf("change attributes %+v", change)
		}
		for _, key := range keys {
			if _, ok := change.Attrs[key].(string); !ok {
				t.Errorf("attribute %s %+v", key, change)
			}
		}
		found := false
		for j, event := range bus {
			if !matched[j] && event.Name == change.Name && event.RequestID == change.RequestID && event.User == change.User && reflect.DeepEqual(map[string]any(event.Attrs), map[string]any(change.Attrs)) {
				matched[j] = true
				found = true
				if change.Name != "campaign.member.opened" && (event.Cause != "evt_8c3f1a6e2d9b4075" || event.Depth != 2) {
					t.Errorf("cause/depth %+v", event)
				}
				break
			}
		}
		if !found {
			t.Errorf("no matching bus %+v", change)
		}
		if change.Name == "campaign.member.opened" {
			continue
		}
		seenChange, seenCall := false, false
		for _, event := range byRequest[change.RequestID] {
			if reflect.DeepEqual(event, change) {
				seenChange = true
			}
			if event.Name == "tool.called" {
				seenCall = true
				if !seenChange {
					t.Error("tool.called before change")
				}
				if event.Attrs["tool"] == "deliver_channel" {
					channelCount++
				}
			}
		}
		if !seenCall {
			t.Errorf("missing tool.called %s", change.RequestID)
		}
	}
	if channelCount != 3 {
		t.Errorf("bulk delivery changes %d", channelCount)
	}
	for target, fake := range map[string]*integrationFake{"candidates": candidatesFake, "artifacts": artifactsFake} {
		actual := map[integrationRequest]int{}
		for _, event := range trail {
			if event.Name == "sibling.called" && event.Attrs["target"] == target {
				key := integrationRequest{Method: fmt.Sprint(event.Attrs["method"]), Path: fmt.Sprint(event.Attrs["path"]), ID: event.RequestID, User: event.User}
				actual[key]++
			}
		}
		want := map[integrationRequest]int{}
		for _, q := range fake.snapshot() {
			q.Email = ""
			want[q]++
		}
		if !reflect.DeepEqual(actual, want) {
			t.Errorf("%s sibling trail=%v requests=%v", target, actual, want)
		}
	}
	secrets := append(append([]string{}, markers...), phone)
	for _, event := range trail {
		b, e := json.Marshal(event)
		if e != nil {
			t.Fatal(e)
		}
		integrationNoSecrets(t, b, secrets)
		if event.Name == "request.started" {
			copyEvent := event
			copyEvent.Attrs = telemetry.Attrs{}
			for k, v := range event.Attrs {
				if k != "path" {
					copyEvent.Attrs[k] = v
				}
			}
			b, e = json.Marshal(copyEvent)
			if e != nil {
				t.Fatal(e)
			}
		}
		integrationNoSecrets(t, b, tokens)
	}
	for _, event := range bus {
		b, e := json.Marshal(event)
		if e != nil {
			t.Fatal(e)
		}
		integrationNoSecrets(t, b, append(secrets, tokens...))
	}
	integrationNoSecrets(t, r.out.Bytes(), append(secrets, tokens...))
	integrationNoSecrets(t, r.err.Bytes(), append(secrets, tokens...))
}
func integrationNoSecrets(t *testing.T, b []byte, secrets []string) {
	t.Helper()
	s := strings.ToLower(string(b))
	for _, secret := range secrets {
		if strings.Contains(s, strings.ToLower(secret)) {
			t.Errorf("secret %q in %s", secret, b)
		}
	}
}

// R-AGTT-UJLF
func TestRunIntegrationConcurrentRandom(t *testing.T) {
	f := newIntegrationFake(t, false)
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"candidates": f.socket}, "")
	random := &integrationRandom{}
	r := startIntegration(t, integrationOptions{path: path, random: random})
	c := r.create("Concurrency")
	r.call("add_member", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000001"}, nil)
	var merge tools.MergeList
	r.call("merge_list", map[string]any{"campaign": c.ID, "channel": "mail"}, &merge)
	token := integrationToken(t, merge.Rows[0].Link)
	var wg sync.WaitGroup
	errs := make(chan error, 90)
	gate := make(chan struct{})
	for i := range 90 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			if i%3 == 0 {
				req, err := http.NewRequestWithContext(context.Background(), "GET", r.base+"/t/"+token, nil)
				if err != nil {
					errs <- err
					return
				}
				res, err := r.http.Do(req)
				if err == nil {
					_, _ = io.Copy(io.Discard, res.Body)
					err = res.Body.Close()
				}
				if err != nil {
					errs <- err
				}
				return
			}
			name := "campaigns"
			args := map[string]any{}
			if i%3 == 1 {
				name = "create_campaign"
				args = map[string]any{"name": "concurrent", "channels": []string{"mail"}, "destination": "https://destination.test/"}
			}
			result, err := r.callAs(name, args, identity.Caller{UserID: "owner", Email: "mgreenly+fake01@gmail.com"})
			if err != nil {
				errs <- err
			} else if result.IsError() {
				errs <- errors.New("concurrent tool refused")
			}
		}()
	}
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if code := r.stop(); code != 0 {
		t.Fatal(code)
	}
	if random.collision.Load() {
		t.Fatal("Rand.Read called concurrently")
	}
}

type integrationBusSink struct {
	capture   events.Capture
	reject    bool
	delivered chan events.Event
}

func (s *integrationBusSink) Deliver(ctx context.Context, e events.Event) error {
	_ = s.capture.Deliver(ctx, e)
	if s.delivered != nil {
		s.delivered <- e
	}
	if s.reject {
		return fmt.Errorf("fixture rejection: %w", events.ErrRejected)
	}
	return nil
}

// R-XSDJ-PDX1 R-XYH1-M8MI R-PW5D-D9XV
func TestRunIntegrationRejectedBusKeepsTrail(t *testing.T) {
	f := newIntegrationFake(t, false)
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"candidates": f.socket}, "")
	var changes [2][]telemetry.Event
	sharedSocketDir := integrationSocketDir(t)
	for i := range 2 {
		sink := &integrationBusSink{reject: i == 1, delivered: make(chan events.Event, 10)}
		r := startIntegration(t, integrationOptions{path: path, bus: sink, socketDir: sharedSocketDir})
		call := func(q, name string, args any, into any) {
			t.Helper()
			result, err := r.callAs(name, args, identity.Caller{UserID: "owner", Email: "mgreenly+fake01@gmail.com", RequestID: q})
			if err != nil || result.IsError() {
				t.Fatalf("%s: %v %+v", name, err, result)
			}
			if into != nil {
				b, _ := result.MarshalJSON()
				var env struct {
					Content json.RawMessage `json:"structuredContent"`
				}
				if err = json.Unmarshal(b, &env); err != nil {
					t.Fatal(err)
				}
				if err = json.Unmarshal(env.Content, into); err != nil {
					t.Fatal(err)
				}
			}
		}
		var c tools.Campaign
		call("create", "create_campaign", map[string]any{"name": "Bus", "channels": []string{"mail", "email"}, "destination": "https://destination.test/"}, &c)
		var added tools.AddedMember
		call("add", "add_member", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000001"}, &added)
		call("deliver", "deliver", map[string]any{"piece": added.Member.Pieces[0].ID}, nil)
		call("reject", "reject", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000001", "why": "not appropriate"}, nil)
		var merge tools.MergeList
		call("merge", "merge_list", map[string]any{"campaign": c.ID, "channel": "mail", "all": true}, &merge)
		r.request("GET", "/t/"+integrationToken(t, merge.Rows[0].Link), "owner", "open", nil)
		if code := r.stop(); code != 0 {
			t.Fatal(code)
		}
		if err := r.notice.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(sharedSocketDir, "n")); err != nil {
			t.Fatal(err)
		}
		trail := r.capture.Events()
		bus := sink.capture.Events()
		if len(bus) != 4 {
			t.Fatalf("bus calls %d", len(bus))
		}
		lost := map[string][]telemetry.Event{}
		for _, e := range trail {
			switch e.Name {
			case "campaign.member.added", "campaign.member.rejected", "campaign.piece.delivered", "campaign.member.opened":
				changes[i] = append(changes[i], e)
			case "event.lost":
				lost[fmt.Sprint(e.Attrs["event"])] = append(lost[fmt.Sprint(e.Attrs["event"])], e)
			}
		}
		if len(changes[i]) != 4 {
			t.Fatalf("changes %d", len(changes[i]))
		}
		if i == 0 && len(lost) != 0 {
			t.Error("successful bus lost event")
		}
		if i == 1 {
			if len(lost) != len(bus) {
				t.Errorf("lost events %v", lost)
			}
			for _, event := range bus {
				es := lost[event.ID]
				if len(es) != 1 {
					t.Errorf("bus %s lost count %d", event.ID, len(es))
					continue
				}
				e := es[0]
				if e.Attrs["cause"] != event.Cause || e.RequestID != event.RequestID || e.User != event.User {
					t.Errorf("lost mismatch %+v %+v", e, event)
				}
			}
		}
	}
	if !reflect.DeepEqual(changes[0], changes[1]) {
		t.Errorf("bus delivery changed trail\nsuccess=%+v\nreject=%+v", changes[0], changes[1])
	}
}

// R-QPFT-7FOB R-QQNP-L7F0
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
			drainLine := fmt.Sprintf("campaigns: stopped with %d requests unfinished\n", count)
			if count == 1 {
				drainLine = "campaigns: stopped with 1 request unfinished\n"
			}
			if !strings.HasSuffix(r.err.String(), drainLine) {
				t.Errorf("last drain diagnostic %q", r.err.String())
			}
			before := len(capture.Events())
			writer.Emit(context.Background(), "campaign.visited", telemetry.Attrs{"campaign": "campaign", "piece": "piece"})
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

// R-QEGP-RI02
func TestRunIntegrationNoNotification(t *testing.T) {
	for _, unset := range []bool{false, true} {
		t.Run(fmt.Sprint(unset), func(t *testing.T) {
			r := startIntegration(t, integrationOptions{noNotify: !unset, unsetNotify: unset})
			r.call("campaigns", map[string]any{}, nil)
			if code := r.stop(); code != 0 {
				t.Fatal(code)
			}
		})
	}
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

// R-QPFT-7FOB
func TestRunIntegrationBusDrainDeadline(t *testing.T) {
	f := newIntegrationFake(t, false)
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"candidates": f.socket}, "")
	sink := &integrationBlockingBus{entered: make(chan struct{})}
	r := startIntegration(t, integrationOptions{path: path, bus: sink, env: map[string]string{"DRAIN_SECONDS": "1"}})
	c := r.create("Queued events")
	r.call("add_prospect_list", map[string]any{"campaign": c.ID, "prospect_list": "pl_0000000000000001"}, nil)
	select {
	case <-sink.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("event sink not entered")
	}
	if code := r.stop(); code != 0 {
		t.Fatal(code)
	}
	if sink.calls.Load() != 1 {
		t.Errorf("new bus delivery after deadline: %d calls", sink.calls.Load())
	}
	if !strings.Contains(r.err.String(), "lost event") {
		t.Errorf("bus drain diagnostics %q", r.err.String())
	}
}

func seedIntegrationVisits(t *testing.T, count int, extra bool) (string, map[string]telemetry.Attrs) {
	t.Helper()
	dir := t.TempDir()
	now := integrationTime
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "campaigns.db"), Migrations: campaigns.Migrations(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	st := store.New(d, store.Config{Now: func() time.Time { return now }, Rand: &integrationRandom{}})
	c, err := st.CreateCampaign(context.Background(), store.Campaign{Name: "seed", CreatedUserID: "owner", Channels: []string{store.ChannelMail}, Destination: "https://destination.test/"})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]telemetry.Attrs{}
	add := func(c store.Campaign, id string, visited bool) {
		t.Helper()
		m, _, err := st.AddMember(context.Background(), store.MemberInput{Campaign: c.ID, Candidate: id, UserID: "owner", Channels: c.Channels})
		if err != nil {
			t.Fatal(err)
		}
		if !visited {
			return
		}
		now = integrationTime
		if _, err = st.Visit(context.Background(), m.Pieces[0].Piece.Token, store.VisitInput{}); err != nil {
			t.Fatal(err)
		}
		expected[id] = telemetry.Attrs{"campaign": c.ID, "candidate": id, "at": now.UTC().Format(time.RFC3339)}
		now = now.Add(time.Hour)
		if _, err = st.Visit(context.Background(), m.Pieces[0].Piece.Token, store.VisitInput{}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range count {
		add(c, fmt.Sprintf("cand_%016x", i+1), true)
	}
	add(c, "cand_fffffffffffffffd", false)
	if extra {
		if _, _, err = st.RejectMember(context.Background(), store.Rejection{Campaign: c.ID, Candidate: "cand_0000000000000001", Why: "fixture", UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
		doomed, err := st.CreateCampaign(context.Background(), store.Campaign{Name: "deleted", CreatedUserID: "owner", Channels: c.Channels, Destination: c.Destination})
		if err != nil {
			t.Fatal(err)
		}
		add(doomed, "cand_fffffffffffffffe", true)
		if _, _, err = st.DeleteCampaign(context.Background(), doomed.ID); err != nil {
			t.Fatal(err)
		}
		delete(expected, "cand_fffffffffffffffe")
		add(c, "cand_ffffffffffffffff", true)
		delete(expected, "cand_ffffffffffffffff")
	}
	if err = d.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE members SET opened_emitted = '' WHERE candidate <> ?", "cand_ffffffffffffffff")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return dir, expected
}

type integrationTimer struct{ calls chan chan time.Time }

func newIntegrationTimer() *integrationTimer {
	return &integrationTimer{calls: make(chan chan time.Time, 10)}
}
func (a *integrationTimer) after(d time.Duration) <-chan time.Time {
	if d != time.Hour {
		panic("wrong sweep interval")
	}
	c := make(chan time.Time, 1)
	a.calls <- c
	return c
}
func (a *integrationTimer) next(t *testing.T) chan time.Time {
	t.Helper()
	select {
	case c := <-a.calls:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("sweep timer not called")
		return nil
	}
}
func integrationOpened(t *testing.T, trail []telemetry.Event, bus []events.Event, expected map[string]telemetry.Attrs) {
	t.Helper()
	seen := map[string]int{}
	for _, e := range trail {
		if e.Name != "campaign.member.opened" {
			continue
		}
		id := fmt.Sprint(e.Attrs["candidate"])
		seen[id]++
		if !reflect.DeepEqual(e.Attrs, expected[id]) || e.RequestID != "" || e.User != "" {
			t.Errorf("opened trail %+v", e)
		}
	}
	if len(seen) != len(expected) {
		t.Fatalf("opened members %d want %d", len(seen), len(expected))
	}
	for id := range expected {
		if seen[id] != 1 {
			t.Errorf("member %s count %d", id, seen[id])
		}
	}
	seen = map[string]int{}
	for _, e := range bus {
		if e.Name != "campaign.member.opened" {
			continue
		}
		id := fmt.Sprint(e.Attrs["candidate"])
		seen[id]++
		if !reflect.DeepEqual(map[string]any(e.Attrs), map[string]any(expected[id])) || e.RequestID != "" || e.User != "" {
			t.Errorf("opened bus %+v", e)
		}
	}
	if len(seen) != len(expected) {
		t.Fatalf("bus members %d want %d", len(seen), len(expected))
	}
	for id := range expected {
		if seen[id] != 1 {
			t.Errorf("bus member %s count %d", id, seen[id])
		}
	}
}

// R-ZI0H-GSGZ R-QD1K-0939 R-QFHC-RSKN R-PUXG-ZI76
func TestRunIntegrationHourlySweep(t *testing.T) {
	for _, extra := range []bool{false, true} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			count := 2*links.SweepBatchSize + 1
			if extra {
				count = 2
			}
			dir, expected := seedIntegrationVisits(t, count, extra)
			timer := newIntegrationTimer()
			r := startIntegration(t, integrationOptions{dir: dir, sweepAfter: timer.after})
			first := timer.next(t)
			first <- integrationTime
			second := timer.next(t)
			if r.err.String() != "campaigns: opened events sent\n" {
				t.Fatalf("completion %q", r.err.String())
			}
			integrationOpened(t, r.capture.Events(), r.bus.Events(), expected)
			before := r.err.String()
			second <- integrationTime
			timer.next(t)
			if r.err.String() != before {
				t.Errorf("empty sweep wrote %q", r.err.String())
			}
			if code := r.stop(); code != cli.ExitSuccess {
				t.Fatal(code)
			}
			integrationOpened(t, r.capture.Events(), r.bus.Events(), expected)
		})
	}
}

// R-QE9G-E0TY R-QGP9-5KBC
func TestRunIntegrationSweepWaitCancelled(t *testing.T) {
	dir, _ := seedIntegrationVisits(t, 2*links.SweepBatchSize+1, false)
	timer := newIntegrationTimer()
	r := startIntegration(t, integrationOptions{dir: dir, sweepAfter: timer.after})
	timer.next(t)
	if code := r.stop(); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	if r.err.Len() != 0 || len(r.bus.Events()) != 0 {
		t.Fatalf("sweep ran before firing %q %+v", r.err.String(), r.bus.Events())
	}
	select {
	case <-timer.calls:
		t.Fatal("timer called after stop")
	default:
	}
}

type integrationCutoffSink struct {
	capture events.Capture
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *integrationCutoffSink) Deliver(ctx context.Context, e events.Event) error {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.capture.Deliver(ctx, e)
}

// R-ZKGA-8BYD R-QGP9-5KBC
func TestRunIntegrationStopsRunningSweep(t *testing.T) {
	dir, _ := seedIntegrationVisits(t, 2*links.SweepBatchSize+1, false)
	sink := &integrationCutoffSink{entered: make(chan struct{}), release: make(chan struct{})}
	timer := newIntegrationTimer()
	r := startIntegration(t, integrationOptions{dir: dir, sweepAfter: timer.after, bus: sink})
	timer.next(t) <- integrationTime
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep never delivered")
	}
	r.cancel(errors.New("cutoff"))
	close(sink.release)
	if code := r.stop(); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	if r.err.Len() != 0 {
		t.Fatalf("cutoff wrote %q", r.err.String())
	}
	select {
	case <-timer.calls:
		t.Fatal("timer called after cutoff")
	default:
	}
	expected := map[string]telemetry.Attrs{}
	stopped := false
	for _, e := range r.capture.Events() {
		if e.Name == "service.stopping" {
			stopped = true
		}
		if e.Name == "campaign.member.opened" {
			if stopped {
				t.Error("opened after stopping")
			}
			expected[fmt.Sprint(e.Attrs["candidate"])] = e.Attrs
		}
	}
	if len(expected) == 0 || len(expected) > links.SweepBatchSize {
		t.Fatalf("cutoff page size %d", len(expected))
	}
	integrationOpened(t, r.capture.Events(), sink.capture.Events(), expected)
}

// R-PR9R-U6Z3 R-PTPK-LQGH
func TestRunIntegrationLiveFirstOpen(t *testing.T) {
	f := newIntegrationFake(t, false)
	path := filepath.Join(t.TempDir(), "services")
	integrationServices(t, path, map[string]string{"candidates": f.socket}, "")
	r := startIntegration(t, integrationOptions{path: path})
	var c tools.Campaign
	r.call("create_campaign", map[string]any{"name": "live first", "channels": []string{"mail", "email"}, "destination": "https://destination.test/"}, &c)
	r.call("add_member", map[string]any{"campaign": c.ID, "candidate": "cand_0000000000000001"}, nil)
	var mail, email tools.MergeList
	r.call("merge_list", map[string]any{"campaign": c.ID, "channel": "mail"}, &mail)
	r.call("merge_list", map[string]any{"campaign": c.ID, "channel": "email"}, &email)
	mailToken, emailToken := integrationToken(t, mail.Rows[0].Link), integrationToken(t, email.Rows[0].Link)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			status, _ := r.request("GET", "/t/"+strings.ToUpper(mailToken)+"?r=private-query", "owner", fmt.Sprintf("concurrent-open-%d", i), nil)
			if status != 302 {
				t.Errorf("visit status %d", status)
			}
		}()
	}
	close(gate)
	wg.Wait()
	r.request("HEAD", "/t/"+emailToken, "owner", "later-head", nil)
	r.request("GET", "/t/unknown", "owner", "unknown-open", nil)
	r.request("POST", "/t/"+mailToken, "owner", "post-open", nil)
	if code := r.stop(); code != 0 {
		t.Fatal(code)
	}
	integrationAssertLiveOpened(t, r, c.ID, "cand_0000000000000001", integrationTime, []string{"concurrent-open-0", "concurrent-open-1"})
}

// R-PTPK-LQGH
func TestRunIntegrationEarlierVisitFirstOpen(t *testing.T) {
	dir, expected := seedIntegrationVisits(t, 1, false)
	var campaign string
	for _, attrs := range expected {
		campaign = fmt.Sprint(attrs["campaign"])
	}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "campaigns.db"), Migrations: campaigns.Migrations(), Now: func() time.Time { return integrationTime }})
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(d, store.Config{Now: func() time.Time { return integrationTime }, Rand: &integrationRandom{}})
	m, err := st.GetMember(context.Background(), campaign, "cand_0000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	token := m.Pieces[0].Piece.Token
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	r := startIntegration(t, integrationOptions{dir: dir, now: func() time.Time { return integrationTime.Add(3 * time.Hour) }})
	r.request("GET", "/t/"+token, "owner", "earlier-open", nil)
	r.request("GET", "/t/"+token, "owner", "already-open", nil)
	if code := r.stop(); code != 0 {
		t.Fatal(code)
	}
	integrationAssertLiveOpened(t, r, campaign, "cand_0000000000000001", integrationTime, []string{"earlier-open"})
}

func integrationAssertLiveOpened(t *testing.T, r *integrationRun, campaign, candidate string, first time.Time, requests []string) {
	t.Helper()
	want := telemetry.Attrs{"campaign": campaign, "candidate": candidate, "at": first.UTC().Truncate(time.Second).Format(time.RFC3339)}
	var opened []telemetry.Event
	for _, e := range r.capture.Events() {
		if e.Name == "campaign.member.opened" {
			opened = append(opened, e)
		}
	}
	if len(opened) != 1 {
		t.Fatalf("first-open trail count %d", len(opened))
	}
	e := opened[0]
	allowed := false
	for _, q := range requests {
		if e.RequestID == q {
			allowed = true
		}
	}
	if !allowed || e.User != "" || !reflect.DeepEqual(e.Attrs, want) {
		t.Fatalf("first-open trail %+v", e)
	}
	visited, finished := false, false
	for _, event := range r.capture.Events() {
		if event.RequestID != e.RequestID {
			continue
		}
		switch event.Name {
		case "campaign.visited":
			visited = true
		case "campaign.member.opened":
			if !visited || finished {
				t.Error("opened event order")
			}
		case "request.finished":
			finished = true
		}
	}
	if !visited || !finished {
		t.Error("first-open request missing visit/finish")
	}
	bus := []events.Event{}
	for _, event := range r.bus.Events() {
		if event.Name == "campaign.member.opened" {
			bus = append(bus, event)
		}
	}
	if len(bus) != 1 || bus[0].RequestID != e.RequestID || bus[0].User != "" || !reflect.DeepEqual(map[string]any(bus[0].Attrs), map[string]any(want)) {
		t.Fatalf("first-open bus %+v", bus)
	}
}

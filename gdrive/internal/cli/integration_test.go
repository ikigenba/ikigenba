package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/cli"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
)

type integrationRoundTripper func(*http.Request) (*http.Response, error)

func (f integrationRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type integrationRandom struct{}

func (integrationRandom) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 0x5a
	}
	return len(b), nil
}

type integrationLog struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
}

func (l *integrationLog) add(r *http.Request, b string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, r.Clone(context.Background()))
	l.bodies = append(l.bodies, b)
}

type integrationRun struct {
	p        cli.Process
	cancel   context.CancelCauseFunc
	done     chan int
	client   *mcp.Client
	address  string
	trace    *telemetry.Capture
	bus      *events.Capture
	out, err bytes.Buffer
	after    chan chan time.Time
	dir      string
	env      map[string]string
	notify   *net.UnixConn
}

func integrationSocketDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("", "gd-it-")
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
func integrationUnix(t *testing.T, d, name string, h http.Handler) string {
	t.Helper()
	p := filepath.Join(d, name)
	l, e := net.Listen("unix", p)
	if e != nil {
		t.Fatal(e)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close(); <-done })
	return p
}
func integrationServices(t *testing.T, d string, entries ...map[string]any) string {
	t.Helper()
	b, e := json.Marshal(map[string]any{"services": entries})
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(d, "services.json")
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func integrationEntry(name, socket, url string) map[string]any {
	return map[string]any{"name": name, "socket": socket, "url": url, "description": "fake", "enabled": true, "mcp": false}
}
func integrationNew(t *testing.T, path string, transport http.RoundTripper) *integrationRun {
	t.Helper()
	t.Setenv(services.Variable, path)
	d := integrationSocketDir(t)
	n, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(d, "ready"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = n.Close() })
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	r := &integrationRun{dir: t.TempDir(), done: make(chan int, 1), trace: &telemetry.Capture{}, bus: &events.Capture{}, after: make(chan chan time.Time, 8), address: "http://" + ln.Addr().String(), env: map[string]string{"LISTEN_PID": "314", "LISTEN_FDS": "1", "IKIGENBA_SERVICES": path, "NOTIFY_SOCKET": n.LocalAddr().String()}, notify: n}
	r.p = cli.Process{LookupEnv: func(k string) (string, bool) { v, ok := r.env[k]; return v, ok }, Unsetenv: func(string) error { return nil }, Pid: 314, Stdout: &r.out, Stderr: &r.err, Version: "integration-version", Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("descriptor %d", fd)
		}
		return ln, nil
	}, Now: func() time.Time { return time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC) }, Sleep: func(ctx context.Context, _ time.Duration) { <-ctx.Done() }, After: func(d time.Duration) <-chan time.Time {
		if d != cli.SweepInterval {
			t.Errorf("interval %v", d)
		}
		c := make(chan time.Time, 1)
		r.after <- c
		return c
	}, Rand: integrationRandom{}, Dir: r.dir, Sink: r.trace, EventSink: r.bus, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: "integration", Version: "integration-version", Telemetry: w})
	}, HTTP: &http.Client{Transport: transport}}
	r.client = mcp.NewClient(mcp.ClientConfig{Endpoint: r.address + "/mcp", HTTPClient: &http.Client{Timeout: 5 * time.Second}})
	t.Cleanup(func() {
		if r.cancel != nil {
			r.stop(t)
		}
	})
	return r
}
func (r *integrationRun) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	r.cancel = cancel
	go func() { r.done <- cli.Run(ctx, r.p) }()
	_ = r.notify.SetReadDeadline(time.Now().Add(5 * time.Second))
	b := make([]byte, 32)
	n, e := r.notify.Read(b)
	if e != nil {
		t.Fatalf("ready: %v", e)
	}
	if string(b[:n]) != "READY=1" {
		t.Fatalf("ready %q", b[:n])
	}
}
func (r *integrationRun) stop(t *testing.T) int {
	t.Helper()
	r.cancel(fmt.Errorf("test-stop"))
	select {
	case code := <-r.done:
		r.cancel = nil
		return code
	case <-time.After(7 * time.Second):
		t.Fatal("Run did not stop")
		return -1
	}
}

var integrationCaller = identity.Caller{UserID: "chosen-user", Email: "chosen@example.invalid", RequestID: "chosen-request"}

func (r *integrationRun) call(t *testing.T, name, args string) mcp.Result {
	t.Helper()
	v, e := r.client.CallTool(context.Background(), integrationCaller, name, json.RawMessage(args))
	if e != nil {
		t.Fatalf("%s: %v", name, e)
	}
	return v
}
func integrationJSON(t *testing.T, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func (r *integrationRun) waitTransfer(t *testing.T, id, status string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		v, e := r.client.CallTool(ctx, integrationCaller, "transfer", json.RawMessage(fmt.Sprintf(`{"transfer":%q}`, id)))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(integrationJSON(t, v), `"status":"`+status+`"`) {
			return
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
}
func integrationStore(t *testing.T, dir string) (*store.Store, func()) {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "gdrive.db"), Migrations: gdrive.Migrations(), Now: func() time.Time { return time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC) }})
	if e != nil {
		t.Fatal(e)
	}
	return store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC) }, Rand: integrationRandom{}}), func() { _ = d.Close() }
}
func integrationResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

// R-R49O-M31A
func TestRunPullExpiryUsesInjectedClock(t *testing.T) {
	d := integrationSocketDir(t)
	cs := integrationUnix(t, d, "connections", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fake-token", "expires": "2040-01-01T00:00:00Z", "account": "account", "scopes": []string{}})
	}))
	path := integrationServices(t, d, integrationEntry("connections", cs, ""))
	r := integrationNew(t, path, forbiddenTransport{t})
	r.p.Now = func() time.Time { return time.Date(2035, 6, 7, 8, 9, 10, 0, time.UTC) }
	r.start(t)
	result := r.call(t, "pull", `{"file":"source","path":"destination","expires":"2035-06-07T08:09:10Z"}`)
	if !result.IsError() || !strings.Contains(integrationJSON(t, result), "expires: ") {
		t.Fatalf("expiry did not compare the injected clock: %+v", result)
	}
	if code := r.stop(t); code != cli.ExitSuccess {
		t.Fatal(code, r.err.String())
	}
}

// R-QQNQ-76OR R-QRVM-KYFG R-R49O-M31A R-RBE0-PAAK R-RCLX-3219 R-RDTT-GTRY
// R-JFJJ-MKAG R-JGRG-0C15 R-JHZC-E3RU R-JJ78-RVIJ R-KYF0-U8NP R-KTJF-B5OX R-KVZ8-2P6B
// R-0797-D6VO R-08H3-QYMD R-L5QF-4V3V R-L3AM-DBMH R-L4II-R3D6 R-5J13-EJ5I
// R-JKF5-5N98 R-JLN1-JEZX
func TestIntegrationPullIdentityAndSecrecy(t *testing.T) {
	d := integrationSocketDir(t)
	token := strings.Repeat("Zg7Q", 10)
	body := strings.Repeat("Yh8Rx2Lm9Pw3Ks6T", 4096)
	cl, al, dl := &integrationLog{}, &integrationLog{}, &integrationLog{}
	cs := integrationUnix(t, d, "connections", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cl.add(r, "")
		if r.URL.Path != "/token/google" {
			t.Error(r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires": "2030-01-01T00:00:00Z", "account": "account", "scopes": []string{"scope"}})
	}))
	as := integrationUnix(t, d, "artifacts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		al.add(r, string(b))
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "artifact", "path": "destination", "content_type": "application/octet-stream", "sha256": "hash", "owner": integrationCaller.UserID, "url": "https://artifacts.invalid/file", "bytes": len(b), "created": "2026-01-20T12:00:00Z", "modified": "2026-01-20T12:00:00Z"})
	}))
	p := integrationServices(t, d, integrationEntry("connections", cs, "https://connections.invalid"), integrationEntry("artifacts", as, "https://artifacts.invalid"))
	r := integrationNew(t, p, integrationRoundTripper(func(q *http.Request) (*http.Response, error) {
		dl.add(q, "")
		b := `{"drives":[]}`
		if strings.HasSuffix(q.URL.Path, "/files") {
			b = `{"files":[]}`
		}
		if strings.Contains(q.URL.Path, "/files/") {
			b = fmt.Sprintf(`{"id":"source","name":"document","mimeType":"application/octet-stream","size":%q}`, fmt.Sprint(len(body)))
			if q.URL.Query().Get("alt") == "media" {
				b = body
			}
		}
		return integrationResponse(q, b), nil
	}))
	allowedKeys := map[string]bool{}
	for _, k := range []string{"DRAIN_SECONDS", "TRANSFER_MAX_BYTES", "TRANSFER_MAX_ACTIVE", "TRANSFER_MAX_QUEUED", "TRANSFER_KEEP_DAYS", "TRANSFER_KEEP_COUNT", "IKIGENBA_SERVICES", "LISTEN_PID", "LISTEN_FDS", "NOTIFY_SOCKET"} {
		allowedKeys[k] = true
	}
	after := r.p.After
	stopping := make(chan struct{})
	r.p.After = func(d time.Duration) <-chan time.Time {
		select {
		case <-stopping:
			t.Error("After called after cancellation")
		default:
		}
		return after(d)
	}
	lookup := r.p.LookupEnv
	r.p.LookupEnv = func(k string) (string, bool) {
		if !allowedKeys[k] {
			t.Errorf("unexpected lookup %s", k)
		}
		return lookup(k)
	}
	r.start(t)
	entries, e := os.ReadDir(filepath.Join(r.dir, "state"))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range entries {
		if !strings.HasPrefix(v.Name(), "gdrive.db") {
			t.Error(v.Name())
		}
		if v.Name() == "gdrive.db" {
			i, e := v.Info()
			if e != nil || !i.Mode().IsRegular() {
				t.Fatal("database not regular")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("database missing")
	}
	outputs := []string{}
	for _, v := range []struct{ name, args string }{{"drives", "{}"}, {"list", "{}"}, {"search", `{"query":"query"}`}, {"show", `{"file":"source"}`}} {
		result := r.call(t, v.name, v.args)
		outputs = append(outputs, integrationJSON(t, result))
		if result.IsError() {
			t.Fatalf("%s: %s", v.name, outputs[len(outputs)-1])
		}
	}
	v := r.call(t, "pull", `{"file":"source","path":"destination"}`)
	outputs = append(outputs, integrationJSON(t, v))
	id := "xfr_5a5a5a5a5a5a5a5a"
	if v.IsError() || !strings.Contains(outputs[len(outputs)-1], id) {
		t.Fatalf("pull: %s", outputs[len(outputs)-1])
	}
	r.waitTransfer(t, id, "done")
	outputs = append(outputs, integrationJSON(t, r.call(t, "transfers", "{}")), integrationJSON(t, r.call(t, "transfer", fmt.Sprintf(`{"transfer":%q}`, id))), integrationJSON(t, r.call(t, "push", `{"path":"missing"}`)))
	for _, path := range []string{"/", "/" + id, "/about", "/tools", "/absent"} {
		q, e := http.NewRequest("GET", r.address+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		q.Header.Set("X-User-Id", integrationCaller.UserID)
		a, e := http.DefaultClient.Do(q)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(a.Body)
		_ = a.Body.Close()
		if e != nil {
			t.Fatal(e)
		}
		outputs = append(outputs, string(b))
	}
	cancelRun := r.cancel
	r.cancel = func(cause error) { cancelRun(cause); close(stopping) }
	if code := r.stop(t); code != cli.ExitSuccess {
		t.Fatal(code, r.err.String())
	}
	st, closeDB := integrationStore(t, r.dir)
	defer closeDB()
	saved, e := st.Get(context.Background(), integrationCaller.UserID, id)
	if e != nil || saved.Status != store.StatusDone || saved.Bytes != int64(len(body)) || saved.Started != r.p.Now() || saved.Finished != r.p.Now() {
		t.Fatalf("saved: %+v %v", saved, e)
	}
	puts := 0
	for i, q := range al.requests {
		if q.Method == "PUT" {
			puts++
			if q.URL.Path != "/files/destination" || al.bodies[i] != body {
				t.Error("PUT body/path")
			}
		}
	}
	if puts != 1 {
		t.Fatal("PUT count", puts)
	}
	for _, l := range []*integrationLog{cl, al} {
		for i, q := range l.requests {
			for k, want := range map[string]string{"X-User-Id": integrationCaller.UserID, "X-User-Email": integrationCaller.Email, "X-Request-Id": integrationCaller.RequestID} {
				if !reflect.DeepEqual(q.Header.Values(k), []string{want}) {
					t.Errorf("identity %s %v", k, q.Header.Values(k))
				}
			}
			if strings.Contains(q.URL.String()+integrationJSON(t, q.Header)+l.bodies[i], token) {
				t.Error("token leaked to sibling")
			}
		}
	}
	if len(cl.requests) == 0 || len(dl.requests) == 0 {
		t.Fatal("missing calls")
	}
	for _, q := range dl.requests {
		if q.Header.Get("Authorization") != "Bearer "+token {
			t.Error("Drive auth")
		}
	}
	trace, bus := r.trace.Events(), r.bus.Events()
	integrationCheckTransferEvents(t, r, id, integrationCaller, true)
	stopCount := 0
	for _, event := range trace {
		if event.Name == "service.stopping" {
			stopCount++
		}
	}
	if stopCount != 1 {
		t.Errorf("stopping count %d", stopCount)
	}
	for _, te := range trace {
		if strings.HasPrefix(te.Name, "transfer.") {
			matches := 0
			for _, be := range bus {
				if be.Name == te.Name && be.RequestID == te.RequestID && be.User == te.User && reflect.DeepEqual(telemetry.Attrs(be.Attrs), te.Attrs) {
					matches++
				}
			}
			if matches != 1 {
				t.Errorf("orphan telemetry event %s: %d bus matches", te.Name, matches)
			}
		}
	}
	if trace[0].Name != "service.started" || trace[len(trace)-1].Name != "service.stopping" || len(bus) != 2 || bus[0].Name != "transfer.started" || bus[1].Name != "transfer.finished" {
		t.Fatalf("lifecycle: %v %v", trace, bus)
	}
	if trace[0].RequestID != "" || trace[0].User != "" || !reflect.DeepEqual(trace[0].Attrs, telemetry.Attrs{"version": r.p.Version}) {
		t.Error("start attributes")
	}
	last := trace[len(trace)-1]
	if last.RequestID != "" || last.User != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "test-stop"}) {
		t.Error("stop attributes")
	}
	names := map[string]bool{}
	for _, n := range []string{"service.started", "service.stopping", "request.started", "request.finished", "tool.called", "sibling.called", "drive.called", "event.lost", "transfer.started", "transfer.finished"} {
		names[n] = true
	}
	targets := map[string]bool{}
	for _, ev := range trace {
		if !names[ev.Name] {
			t.Error("event name", ev.Name)
		}
		if ev.Name == "sibling.called" {
			targets[fmt.Sprint(ev.Attrs["target"])] = true
			if ev.User != integrationCaller.UserID {
				t.Error("sibling user")
			}
		}
	}
	if !targets["connections"] || !targets["artifacts"] {
		t.Error("missing sibling events")
	}
	for _, ev := range bus {
		if ev.RequestID != integrationCaller.RequestID || ev.User != integrationCaller.UserID {
			t.Error("event identity")
		}
		n := 0
		for _, te := range trace {
			if te.Name == ev.Name && te.RequestID == ev.RequestID && te.User == ev.User && reflect.DeepEqual(te.Attrs, telemetry.Attrs(ev.Attrs)) {
				n++
			}
		}
		if n != 1 {
			t.Error("event pairing", n)
		}
	}
	outputs = append(outputs, integrationJSON(t, trace), integrationJSON(t, bus), r.out.String(), r.err.String())
	for _, s := range outputs {
		if strings.Contains(s, token) {
			t.Error("token leaked")
		}
		for i := 0; i+16 <= len("Yh8Rx2Lm9Pw3Ks6T")*2; i++ {
			if strings.Contains(s, ("Yh8Rx2Lm9Pw3Ks6T" + "Yh8Rx2Lm9Pw3Ks6T")[i:i+16]) {
				t.Error("file bytes leaked")
				break
			}
		}
	}
	if r.out.Len() != 0 || r.err.Len() != 0 {
		t.Fatalf("streams %q %q", r.out.String(), r.err.String())
	}
}

// R-599W-CD7Y R-5AHS-Q4YN R-5CXL-HOG1
func TestIntegrationMissingServices(t *testing.T) {
	for _, mode := range []string{"no-artifacts", "no-connections", "unset", "missing-file", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			d := integrationSocketDir(t)
			var entries []map[string]any
			if mode == "no-artifacts" {
				socket := integrationUnix(t, d, "connections", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fake-token", "expires": "2030-01-01T00:00:00Z", "account": "account", "scopes": []string{}})
				}))
				entries = append(entries, integrationEntry("connections", socket, ""))
			}
			path := integrationServices(t, d, entries...)
			switch mode {
			case "unset":
				path = ""
			case "missing-file":
				path = filepath.Join(d, "absent")
			case "malformed":
				if e := os.WriteFile(path, []byte("invalid"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			requests := &integrationLog{}
			r := integrationNew(t, path, integrationRoundTripper(func(q *http.Request) (*http.Response, error) {
				requests.add(q, "")
				return integrationResponse(q, `{"id":"source","mimeType":"application/octet-stream"}`), nil
			}))
			r.start(t)
			calls := []struct{ name, args string }{{"drives", "{}"}, {"pull", `{"file":"source","path":"destination"}`}}
			if mode == "no-artifacts" {
				calls = []struct{ name, args string }{{"pull", `{"file":"source","path":"destination"}`}, {"push", `{"path":"source"}`}}
			}
			for _, c := range calls {
				v := r.call(t, c.name, c.args)
				if !v.IsError() || !strings.Contains(integrationJSON(t, v), "unavailable") {
					t.Fatalf("%s: %s", c.name, integrationJSON(t, v))
				}
			}
			if r.stop(t) != cli.ExitSuccess {
				t.Fatal(r.err.String())
			}
			if len(requests.requests) != 0 {
				t.Error("Drive called")
			}
			for _, ev := range r.trace.Events() {
				if ev.Name == "sibling.called" && (mode != "no-artifacts" || ev.Attrs["target"] != "connections") {
					t.Error("unexpected sibling", ev)
				}
			}
			st, closeDB := integrationStore(t, r.dir)
			defer closeDB()
			v, e := st.List(context.Background(), integrationCaller.UserID)
			if e != nil || len(v) != 0 {
				t.Fatalf("transfers %+v %v", v, e)
			}
		})
	}
}

// R-0RZH-VAHH
func TestIntegrationConnectAddress(t *testing.T) {
	for _, base := range []string{"https://configured.invalid", "https://configured.invalid/", ""} {
		t.Run(base, func(t *testing.T) {
			d := integrationSocketDir(t)
			socket := integrationUnix(t, d, "connections", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(404)
				_, _ = io.WriteString(w, `{"error":"not_connected"}`)
			}))
			path := integrationServices(t, d, integrationEntry("connections", socket, base))
			r := integrationNew(t, path, integrationRoundTripper(func(*http.Request) (*http.Response, error) {
				t.Error("Drive called")
				return nil, fmt.Errorf("unexpected Drive")
			}))
			r.start(t)
			r.client = mcp.NewClient(mcp.ClientConfig{Endpoint: r.address + "/mcp", HTTPClient: &http.Client{Transport: integrationRoundTripper(func(q *http.Request) (*http.Response, error) {
				q.Host = "untrusted.invalid"
				q.Header.Set("X-Forwarded-Proto", "http")
				return http.DefaultTransport.RoundTrip(q)
			})}})
			v := r.call(t, "pull", `{"file":"source","path":"destination"}`)
			text := integrationJSON(t, v)
			want := strings.TrimSuffix(base, "/") + "/google/connect"
			if !v.IsError() || !strings.Contains(text, "not_connected") || !strings.Contains(text, want) {
				t.Fatalf("result %s expected %s", text, want)
			}
			if r.stop(t) != cli.ExitSuccess {
				t.Fatal(r.err.String())
			}
		})
	}
}

type integrationGuardRandom struct {
	busy    bool
	mu      sync.Mutex
	overlap bool
}

func (g *integrationGuardRandom) Read(b []byte) (int, error) {
	g.mu.Lock()
	if g.busy {
		g.overlap = true
	}
	g.busy = true
	g.mu.Unlock()
	runtime.Gosched()
	for i := range b {
		b[i] = 0x5a
	}
	g.mu.Lock()
	g.busy = false
	g.mu.Unlock()
	return len(b), nil
}

// R-QUBF-CHWU
func TestIntegrationConcurrentRandom(t *testing.T) {
	r := integrationNew(t, "", integrationRoundTripper(func(q *http.Request) (*http.Response, error) { return integrationResponse(q, `{}`), nil }))
	random := &integrationGuardRandom{}
	r.p.Rand = random
	r.start(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			q, e := http.NewRequest("GET", r.address+"/about", nil)
			if e != nil {
				t.Error(e)
				return
			}
			q.Header.Set("X-User-Id", "user")
			v, e := http.DefaultClient.Do(q)
			if e != nil {
				t.Error(e)
				return
			}
			_, _ = io.Copy(io.Discard, v.Body)
			_ = v.Body.Close()
		}()
	}
	close(start)
	wg.Wait()
	if r.stop(t) != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	if random.overlap {
		t.Fatal("concurrent random Read")
	}
}

// R-L3AM-DBMH R-L4II-R3D6 R-5J13-EJ5I
func TestIntegrationPushBodiesAndProviderFailureRemainSecret(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d := integrationSocketDir(t)
			token := strings.Repeat("Zg7Q", 10)
			body := strings.Repeat("Yh8Rx2Lm9Pw3Ks6T", 4096)
			siblings := &integrationLog{}
			uploaded := ""
			cs := integrationUnix(t, d, "connections", http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				siblings.add(q, "")
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires": "2030-01-01T00:00:00Z", "account": "account", "scopes": []string{}})
			}))
			as := integrationUnix(t, d, "artifacts", http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				siblings.add(q, "")
				w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("ETag", `"chosen-hash"`)
				w.Header().Set("Last-Modified", "Tue, 20 Jan 2026 12:00:00 GMT")
				if q.Method == "GET" {
					_, _ = io.WriteString(w, body)
				}
			}))
			path := integrationServices(t, d, integrationEntry("connections", cs, "https://connections.invalid"), integrationEntry("artifacts", as, "https://artifacts.invalid"))
			r := integrationNew(t, path, integrationRoundTripper(func(q *http.Request) (*http.Response, error) {
				switch {
				case q.URL.Host == "session.invalid":
					b, e := io.ReadAll(q.Body)
					if e != nil {
						return nil, e
					}
					uploaded = string(b)
					v := integrationResponse(q, `{"id":"created-file","name":"document","mimeType":"application/octet-stream","size":"65536"}`)
					if fail {
						v.StatusCode = 503
						v.Body = io.NopCloser(strings.NewReader(token + body))
					}
					return v, nil
				case strings.Contains(q.URL.Path, "/upload/"):
					v := integrationResponse(q, "")
					v.Header.Set("Location", "https://session.invalid/upload")
					return v, nil
				default:
					return integrationResponse(q, `{"id":"root-id","name":"root","mimeType":"application/vnd.google-apps.folder"}`), nil
				}
			}))
			r.start(t)
			v := r.call(t, "push", `{"path":"source-artifact","name":"document"}`)
			if v.IsError() {
				t.Fatal(integrationJSON(t, v))
			}
			id := integrationID(t, v)
			want := store.StatusDone
			if fail {
				want = store.StatusFailed
			}
			r.waitTransfer(t, id, want)
			outputs := []string{integrationJSON(t, v), integrationJSON(t, r.call(t, "transfer", fmt.Sprintf(`{"transfer":%q}`, id))), integrationJSON(t, r.call(t, "transfers", "{}"))}
			q, e := http.NewRequest("GET", r.address+"/"+id, nil)
			if e != nil {
				t.Fatal(e)
			}
			q.Header.Set("X-User-Id", integrationCaller.UserID)
			resp, e := http.DefaultClient.Do(q)
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			outputs = append(outputs, string(b))
			if r.stop(t) != cli.ExitSuccess {
				t.Fatal(r.err.String())
			}
			if uploaded != body {
				t.Fatal("push did not stream artifact body")
			}
			outputs = append(outputs, integrationJSON(t, r.trace.Events()), integrationJSON(t, r.bus.Events()), r.out.String(), r.err.String())
			for _, s := range outputs {
				if strings.Contains(s, token) {
					t.Error("token leaked")
				}
				for i := 0; i < 16; i++ {
					if strings.Contains(s, body[i:i+16]) {
						t.Error("body leaked")
						break
					}
				}
			}
			for _, q := range siblings.requests {
				if strings.Contains(q.URL.String()+integrationJSON(t, q.Header), token) {
					t.Error("token leaked to sibling")
				}
			}
		})
	}
}

func integrationCheckTransferEvents(t *testing.T, r *integrationRun, id string, caller identity.Caller, started bool) {
	t.Helper()
	for _, stream := range []string{"trail", "bus"} {
		names := []string{}
		if stream == "trail" {
			for _, v := range r.trace.Events() {
				if v.Attrs["transfer"] == id {
					if v.RequestID != caller.RequestID || v.User != caller.UserID {
						t.Errorf("trail transfer identity %+v", v)
					}
					names = append(names, v.Name)
				}
			}
		} else {
			for _, v := range r.bus.Events() {
				if v.Attrs["transfer"] == id {
					if v.RequestID != caller.RequestID || v.User != caller.UserID {
						t.Errorf("bus transfer identity %+v", v)
					}
					names = append(names, v.Name)
				}
			}
		}
		want := []string{"transfer.finished"}
		if started {
			want = []string{"transfer.started", "transfer.finished"}
		}
		if !reflect.DeepEqual(names, want) {
			t.Errorf("%s transfer ordering %v want %v", stream, names, want)
		}
	}
}

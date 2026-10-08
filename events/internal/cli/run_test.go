package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/cli"
)

type repeatByte byte

func (b repeatByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}

type runFixture struct {
	p             cli.Process
	out, errw     bytes.Buffer
	capture       telemetry.Capture
	client        *http.Client
	cancel        context.CancelCauseFunc
	done          chan int
	reads, unsets []string
	mu            sync.Mutex
	timers        map[string][]time.Duration
	dir           string
	fixed         time.Time
}

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "events-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}
func startRun(t *testing.T, dir string, env map[string]string, customize ...func(*runFixture)) *runFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	sockets := shortDir(t)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(sockets, "events"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(sockets, "ready"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notify.Close() })
	f := &runFixture{dir: dir, fixed: time.Date(2026, 10, 6, 1, 2, 3, 456789123, time.FixedZone("test", 3600)), timers: map[string][]time.Duration{}, done: make(chan int, 1)}
	env["LISTEN_PID"] = "0012"
	env["LISTEN_FDS"] = "01"
	env["NOTIFY_SOCKET"] = notify.LocalAddr().String()
	timer := func(name string) func(time.Duration) <-chan time.Time {
		return func(d time.Duration) <-chan time.Time {
			f.mu.Lock()
			f.timers[name] = append(f.timers[name], d)
			f.mu.Unlock()
			return make(chan time.Time)
		}
	}
	f.p = cli.Process{Version: "seam-display", Dir: dir, Pid: 12, Stdout: &f.out, Stderr: &f.errw, Sink: &f.capture, Now: func() time.Time { return f.fixed }, Rand: repeatByte(0x5a), LookupEnv: func(k string) (string, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reads = append(f.reads, k)
		v, ok := env[k]
		return v, ok
	}, Unsetenv: func(k string) error { f.mu.Lock(); defer f.mu.Unlock(); f.unsets = append(f.unsets, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("descriptor %d", fd)
		}
		return listener, nil
	}, SweepAfter: timer("sweep"), RefreshAfter: timer("refresh"), AskAfter: timer("ask"), TimeoutAfter: timer("timeout"), BackoffAfter: timer("backoff")}
	ctx, cancel := context.WithCancelCause(context.Background())
	f.cancel = cancel
	f.client = &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", listener.Addr().String())
	}}}
	for _, configure := range customize {
		configure(f)
	}
	go func() { f.done <- cli.Run(ctx, f.p) }()
	if err := notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _, err := notify.ReadFromUnix(buf)
	if err != nil {
		cancel(context.Canceled)
		t.Fatal("ready", err)
	}
	if string(buf[:n]) != "READY=1" {
		t.Fatal(string(buf[:n]))
	}
	return f
}
func (f *runFixture) stop(t *testing.T) {
	t.Helper()
	f.cancel(context.Canceled)
	select {
	case code := <-f.done:
		if code != 0 {
			t.Fatal(code, f.errw.String())
		}
	case <-time.After(7 * time.Second):
		t.Fatal("run did not stop")
	}
	f.client.CloseIdleConnections()
}
func (f *runFixture) request(t *testing.T, method, path, body, id string) *http.Response {
	t.Helper()
	r, err := http.NewRequest(method, "http://events.test"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-User-Id", "user")
	r.Header.Set("X-User-Email", "private-email")
	r.Header.Set("X-Request-Id", id)
	r.Header.Set("X-Forwarded-Proto", "https")
	if path == "/emit" || path == "/mcp" {
		r.Header.Set("Content-Type", "application/json")
		if strings.Contains(body, `"server/discover"`) {
			r.Header.Set("MCP-Protocol-Version", mcp.ProtocolVersion)
			r.Header.Set("Mcp-Method", "server/discover")
		}
	}
	resp, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
func body(t *testing.T, r *http.Response) string {
	t.Helper()
	defer func() { _ = r.Body.Close() }()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func (f *runFixture) mcp() *mcp.Client {
	return mcp.NewClient(mcp.ClientConfig{Endpoint: "http://events.test/mcp", HTTPClient: f.client})
}
func call(t *testing.T, f *runFixture, name string, args any, id string) mcp.Result {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.mcp().CallTool(context.Background(), identity.Caller{UserID: "user", RequestID: id}, name, data)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// R-9LB6-UL7G R-ZX69-H5FK R-09D9-AUUI R-BTC4-YV4Y R-BY7Q-HY3Q R-BZFM-VPUF
// R-IH24-NGU5 R-96IT-4P0M R-97QP-IGRB R-C6R1-6CAL R-9XI6-OAME R-ZTTZ-Z2G4
// R-ZW9S-QLXI R-CBMM-PF9D R-ZXHP-4DO7 R-CAEQ-BNIO R-9YQ3-22D3 R-9ZXZ-FU3S
// R-CK5X-DTG8 R-A15V-TLUH R-A2DS-7DL6 R-E0E3-6NDU
func TestRunWiring(t *testing.T) {
	for _, servicePath := range []string{"", "missing", "broken", "valid"} {
		t.Run(servicePath, func(t *testing.T) {
			dir := t.TempDir()
			path := ""
			if servicePath != "" {
				path = filepath.Join(t.TempDir(), "services.json")
			}
			if servicePath == "broken" {
				if err := os.WriteFile(path, []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if servicePath == "valid" {
				writeServices(t, path, "first instruction", []map[string]any{{"name": "launcher-sibling", "url": "https://launcher-sibling.test", "socket": "unused", "description": "Launcher fixture", "mcp": false, "enabled": false, "icon": "<svg></svg>"}})
			}
			f := startRun(t, dir, map[string]string{"IKIGENBA_SERVICES": path, "DRAIN_SECONDS": "1"})
			resp := f.request(t, "GET", "/about", "", "")
			about := body(t, resp)
			match := regexp.MustCompile(`<dd id="about-version">(.*?)</dd>`).FindStringSubmatch(about)
			if resp.StatusCode != 200 || len(match) != 2 || match[1] != f.p.Version {
				t.Fatal("about version", about)
			}
			resp = f.request(t, "GET", "/", "", "landing")
			if resp.StatusCode != 200 {
				t.Fatal(body(t, resp))
			}
			landing := body(t, resp)
			if servicePath == "valid" && !strings.Contains(landing, `title="launcher-sibling is unavailable"><svg></svg>launcher-sibling</a>`) {
				t.Fatal(landing)
			}
			tools, err := f.mcp().ListTools(context.Background(), identity.Caller{UserID: "user", RequestID: "list"})
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, tool := range tools {
				names = append(names, tool.Name)
			}
			if strings.Join(names, ",") != "catalog,search,subscribers,skip,resume" {
				t.Fatal(names)
			}
			discover := func() string {
				r := f.request(t, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`, "discover")
				return body(t, r)
			}
			first := discover()
			checkServerInfo(t, first, f.p.Version)
			if servicePath == "valid" {
				if !strings.Contains(first, "first instruction") {
					t.Fatal(first)
				}
				writeServices(t, path, "second instruction", nil)
				if !strings.Contains(discover(), "second instruction") {
					t.Fatal("instructions stale")
				}
			} else if strings.Contains(first, `"instructions"`) {
				t.Fatal(first)
			}
			f.stop(t)
			if f.out.Len() != 0 || f.errw.Len() != 0 {
				t.Fatal(f.out.String(), f.errw.String())
			}
			entries, err := os.ReadDir(filepath.Join(dir, "state"))
			if err != nil || len(entries) != 1 || entries[0].Name() != "events.db" {
				t.Fatal(entries, err)
			}
			reference := filepath.Join(t.TempDir(), "events.db")
			d, err := db.Open(context.Background(), db.Config{Path: reference, Migrations: events.Migrations(), Now: func() time.Time { return f.fixed }})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			var got, want bytes.Buffer
			_ = db.Status(context.Background(), db.Config{Path: filepath.Join(dir, "state", "events.db"), Migrations: events.Migrations()}, &got)
			_ = db.Status(context.Background(), db.Config{Path: reference, Migrations: events.Migrations()}, &want)
			if got.String() != want.String() {
				t.Fatal(got.String(), want.String())
			}
			count := 0
			allowed := map[string]bool{}
			for _, k := range []string{"DRAIN_SECONDS", "EVENTS_DEPTH_MAX", "EVENTS_DELIVERY_TIMEOUT_SECONDS", "EVENTS_DELIVERY_ATTEMPTS", "EVENTS_INFLIGHT_MAX", "EVENTS_RETENTION_DAYS", "EVENTS_DECLARATIONS_SECONDS", "IKIGENBA_SERVICES", "LISTEN_PID", "LISTEN_FDS", "NOTIFY_SOCKET"} {
				allowed[k] = true
			}
			for _, k := range f.reads {
				if !allowed[k] {
					t.Fatal(k)
				}
				if k == "IKIGENBA_SERVICES" {
					count++
				}
			}
			if count != 1 {
				t.Fatal(count)
			}
			if strings.Join(f.unsets, ",") != "LISTEN_PID,LISTEN_FDS,LISTEN_FDNAMES" {
				t.Fatal(f.unsets)
			}
			if len(f.timers["sweep"]) != 1 || f.timers["sweep"][0] != time.Hour || len(f.timers["refresh"]) != 1 || f.timers["refresh"][0] != time.Minute {
				t.Fatal(f.timers)
			}
			foundRandom := false
			for _, e := range f.capture.Events() {
				if e.Service != "events" || !e.Time.Equal(f.fixed.UTC().Truncate(time.Microsecond)) {
					t.Fatal(e)
				}
				if e.Name == "service.started" && e.Attrs["version"] != f.p.Version {
					t.Fatal(e)
				}
				if e.Name == "request.started" && e.RequestID == strings.Repeat("5a", 16) {
					foundRandom = true
				}
			}
			if !foundRandom {
				t.Fatal("injected request randomness unused")
			}
		})
	}
}
func writeServices(t *testing.T, path, description string, extra []map[string]any) {
	t.Helper()
	entries := []map[string]any{{"name": "events", "description": description, "url": "https://events.test", "socket": "unused", "enabled": true, "mcp": true, "icon": "<svg></svg>"}}
	entries = append(entries, extra...)
	data, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func sibling(t *testing.T, accepted bool) (string, <-chan appEvents.Event) {
	t.Helper()
	dir := shortDir(t)
	socket := filepath.Join(dir, "sibling")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan appEvents.Event, 16)
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/declarations":
			_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":["private_attr"]}],"accepts":["repo.pushed"]}`)
		case "/events":
			var e struct {
				ID       string    `json:"id"`
				Seq      int64     `json:"seq"`
				Received time.Time `json:"received"`
			}
			if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
				t.Error(err)
			}
			received <- appEvents.Event{ID: e.ID, Seq: e.Seq, Received: e.Received}
			if accepted {
				w.WriteHeader(204)
			} else {
				w.WriteHeader(500)
				_, _ = io.WriteString(w, "subscriber broke")
			}
		default:
			w.WriteHeader(404)
		}
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socket, received
}
func emitted(f *runFixture, n int, depth int) appEvents.Event {
	e := appEvents.Event{ID: "evt_" + strconv.FormatInt(int64(n)+0x1000000000000000, 16), Time: f.fixed, Service: "repos", Name: "repo.pushed", Attrs: appEvents.Attrs{"private_attr": "private-bus-value"}, Depth: depth}
	if depth > 0 {
		e.Cause = "evt_2000000000000000"
	}
	return e
}
func emit(t *testing.T, f *runFixture, e appEvents.Event, want int) {
	t.Helper()
	data, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	resp := f.request(t, "POST", "/emit", string(data), "emit-request")
	if resp.StatusCode != want {
		t.Fatalf("emit %d %s", resp.StatusCode, body(t, resp))
	}
	_ = body(t, resp)
}

// R-9CMB-1JQ3 R-9F23-T37H R-9GA0-6UY6 R-HG04-PK6H R-HH81-3BX6 R-HIFX-H3NV
// R-HJNT-UVEK R-F8NN-M0DB R-F9VJ-ZS40 R-HKVQ-8N59
func TestRunEventTrail(t *testing.T) {
	socket, received := sibling(t, false)
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{{"name": "repos", "description": "repos", "url": "https://repos.test", "socket": socket, "enabled": true, "mcp": false}})
	f := startRun(t, t.TempDir(), map[string]string{"IKIGENBA_SERVICES": path, "EVENTS_DEPTH_MAX": "1", "EVENTS_DELIVERY_ATTEMPTS": "1"})
	emit(t, f, emitted(f, 1, 2), 422)
	e := emitted(f, 2, 0)
	emit(t, f, e, 204)
	select {
	case delivered := <-received:
		if delivered.ID != e.ID || delivered.Seq != 1 || !delivered.Received.Equal(f.fixed.UTC().Truncate(time.Microsecond)) {
			t.Fatal(delivered)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no delivery")
	}
	// A failing delivery's completion is observed through the public subscribers tool.
	deadline := time.Now().Add(3 * time.Second)
	for {
		result := call(t, f, "subscribers", map[string]any{}, "subscribers")
		data, _ := json.Marshal(result)
		if bytes.Contains(data, []byte("paused")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber not paused", string(data))
		}
	}
	_ = call(t, f, "skip", map[string]any{"service": "repos"}, "skip-call")
	_ = call(t, f, "search", map[string]any{"attrs": map[string]any{"private_attr": "private-tool-argument"}}, "search-call")
	_ = call(t, f, "resume", map[string]any{"service": "repos"}, "resume-call")
	_ = call(t, f, "skip", map[string]any{}, "invalid-call")
	_, _ = f.mcp().ListTools(context.Background(), identity.Caller{UserID: "user", RequestID: "list-call"})
	r := f.request(t, "GET", "/?private-query", "", "page-call")
	_ = body(t, r)
	f.stop(t)
	allowed := map[string]bool{}
	for _, name := range []string{"service.started", "service.stopping", "request.started", "request.finished", "tool.called", "sibling.called", "event.accepted", "event.delivered", "event.skipped", "subscriber.paused"} {
		allowed[name] = true
	}
	byRequest := map[string][]string{}
	acceptedIndex := -1
	seenStarted := false
	seenAsk := false
	for i, event := range f.capture.Events() {
		if !allowed[event.Name] {
			t.Fatal(event.Name)
		}
		data, _ := event.MarshalJSON()
		for _, private := range []string{"private_attr", "private-bus-value", "private-email", "private-query", "private-tool-argument"} {
			if bytes.Contains(data, []byte(private)) {
				t.Fatal("leaked", private, string(data))
			}
		}
		byRequest[event.RequestID] = append(byRequest[event.RequestID], event.Name)
		if event.Name == "sibling.called" {
			if event.Attrs["target"] != "repos" {
				t.Fatal(event)
			}
			if event.Attrs["path"] == "/declarations" {
				seenAsk = true
				if seenStarted {
					continue
				}
			} else if event.Attrs["path"] != "/events" {
				t.Fatal(event)
			}
		}
		if event.Name == "service.started" {
			if !seenAsk {
				t.Fatal("start before asks")
			}
			seenStarted = true
		}
		if event.Name == "event.accepted" {
			acceptedIndex = i
		}
		if event.Name == "subscriber.paused" || event.Name == "event.skipped" {
			if acceptedIndex < 0 || i <= acceptedIndex {
				t.Fatal("outcome before accepted")
			}
		}
		if event.Name == "sibling.called" && event.Attrs["path"] == "/events" && (acceptedIndex < 0 || i <= acceptedIndex) {
			t.Fatal("delivery before accepted")
		}
		if event.Name == "tool.called" {
			if event.RequestID == "skip-call" && (event.Attrs["kind"] != "destructive" || event.Attrs["outcome"] != "ok") {
				t.Fatal(event)
			}
			if event.RequestID == "invalid-call" && (event.Attrs["outcome"] != "invalid_arguments" || event.Attrs["duration_us"] != int64(0)) {
				t.Fatal(event)
			}
		}
	}
	for id, want := range map[string]string{"skip-call": "request.started,event.skipped,tool.called,request.finished", "search-call": "request.started,tool.called,request.finished", "resume-call": "request.started,tool.called,request.finished", "list-call": "request.started,request.finished", "page-call": "request.started,request.finished"} {
		if strings.Join(byRequest[id], ",") != want {
			t.Fatal(id, byRequest[id])
		}
	}
	if f.out.Len() != 0 || f.errw.Len() != 0 {
		t.Fatal(f.out.String(), f.errw.String())
	}
}

// R-9YQ3-22D3 R-9ZXZ-FU3S R-9XI6-OAME R-A15V-TLUH R-A2DS-7DL6
func TestEmptyRunVersion(t *testing.T) {
	f := startRun(t, t.TempDir(), map[string]string{}, func(f *runFixture) { f.p.Version = "" })
	about := body(t, f.request(t, "GET", "/about", "", "empty-about"))
	if !strings.Contains(about, `<dd id="about-version"></dd>`) {
		t.Fatal(about)
	}
	r := f.request(t, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`, "empty-discover")
	checkServerInfo(t, body(t, r), "")
	f.stop(t)
	started := 0
	for _, record := range f.capture.Events() {
		if record.Name == "service.started" {
			started++
			if !reflect.DeepEqual(record.Attrs, telemetry.Attrs{"version": ""}) {
				t.Fatal(record)
			}
		}
	}
	if started != 1 || f.out.Len() != 0 || f.errw.Len() != 0 {
		t.Fatal(started, f.out.String(), f.errw.String())
	}
}
func checkServerInfo(t *testing.T, body, display string) {
	t.Helper()
	var response struct {
		Result struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	var info map[string]string
	if err := json.Unmarshal(response.Result.Meta["io.modelcontextprotocol/serverInfo"], &info); err != nil {
		t.Fatal(err, body)
	}
	if !reflect.DeepEqual(info, map[string]string{"name": "events", "version": display}) {
		t.Fatal(info)
	}
}

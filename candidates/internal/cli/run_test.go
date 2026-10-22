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
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/server"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

type runBytes struct {
	n       byte
	fixed   bool
	reading atomic.Bool
	overlap atomic.Bool
}

func (r *runBytes) Read(p []byte) (int, error) {
	if !r.reading.CompareAndSwap(false, true) {
		r.overlap.Store(true)
	}
	defer r.reading.Store(false)
	for i := range p {
		p[i] = r.n
		if !r.fixed {
			r.n++
			if r.n < 0xa0 {
				r.n = 0xa0
			}
		}
	}
	return len(p), nil
}

type runBuffer struct {
	mu     sync.Mutex
	b      bytes.Buffer
	writes int
}

func (b *runBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes++
	return b.b.Write(p)
}
func (b *runBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type runRequest struct {
	Path   string
	Header http.Header
}
type runFake struct {
	mu           sync.Mutex
	requests     []runRequest
	rows         []map[string]any
	recordStatus int
	rowStatus    int
	hold         <-chan struct{}
	arrived      chan struct{}
}

func (f *runFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/scrapers/ppp/current" {
		f.mu.Lock()
		f.requests = append(f.requests, runRequest{r.URL.Path, r.Header.Clone()})
		f.mu.Unlock()
		w.WriteHeader(404)
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, runRequest{r.URL.Path, r.Header.Clone()})
	rows := f.rows
	rs, rr, hold, arrived := f.recordStatus, f.rowStatus, f.hold, f.arrived
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/rows") {
		if arrived != nil {
			select {
			case arrived <- struct{}{}:
			default:
			}
		}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		if rr != 0 {
			w.WriteHeader(rr)
			return
		}
		for _, row := range rows {
			_ = json.NewEncoder(w).Encode(row)
		}
		return
	}
	if rs != 0 {
		w.WriteHeader(rs)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "run_fixture", "scraper": "fmcsa", "status": "done", "started": "2026-01-01T00:00:00Z", "finished": "2026-01-01T01:00:00Z", "rows": len(rows), "new": len(rows), "changed": 0, "unchanged": 0, "gone": 0})
}
func (f *runFake) Requests() []runRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runRequest(nil), f.requests...)
}
func runSocket(t *testing.T, h http.Handler) string {
	t.Helper()
	d, err := os.MkdirTemp("", "cand-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	socket := filepath.Join(d, "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close() })
	return socket
}
func runServices(t *testing.T, entries map[string]string) string {
	t.Helper()
	list := []map[string]any{}
	for name, socket := range entries {
		list = append(list, map[string]any{"name": name, "socket": socket, "url": "https://" + name + ".example.test", "description": "fixture", "enabled": true, "mcp": false})
	}
	b, err := json.Marshal(map[string]any{"services": list})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "services.json")
	if err = os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func runRow() map[string]any {
	return map[string]any{"scraper": "fmcsa", "run": "run_fixture", "source_id": "101", "scraped": "2026-01-01T00:00:00Z", "digest": "fixture-digest", "name": "Fixture Trucks", "names": []string{"Fixture Trucks"}, "address": map[string]any{"street": "Fixture Road", "city": "Fixture City", "state": "MN", "zip": "55072", "county": "115"}, "people": []any{}, "facts": map[string]any{"power_units": 3, "drivers": 4, "class": []string{"Authorized For Hire"}, "cargo": []string{"General Freight"}, "mcs150_date": "2025-01-01"}, "raw": map[string]any{"fixture": true}}
}

type runHarness struct {
	t               *testing.T
	p               cli.Process
	env             map[string]string
	mu              sync.Mutex
	lookups, unsets []string
	inherits        int
	ln              net.Listener
	notify          *net.UnixConn
	hc              *http.Client
	client          *mcp.Client
	ctx             context.Context
	cancel          context.CancelCauseFunc
	done            chan int
	telemetry       *telemetry.Capture
	bus             *events.Capture
	out, err        *runBuffer
	timers          chan chan time.Time
	afterCancelled  atomic.Int64
	stopped         bool
}

func newRunHarness(t *testing.T, dir, path string) *runHarness {
	t.Helper()
	t.Setenv(services.Variable, "")
	h := &runHarness{t: t, env: map[string]string{"LISTEN_PID": "777", "LISTEN_FDS": "1", services.Variable: path}, telemetry: &telemetry.Capture{}, bus: &events.Capture{}, out: &runBuffer{}, err: &runBuffer{}, timers: make(chan chan time.Time, 10), done: make(chan int, 1)}
	var err error
	h.ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	d, err := os.MkdirTemp("", "cand-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	addr := &net.UnixAddr{Name: filepath.Join(d, "n"), Net: "unixgram"}
	h.notify, err = net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.notify.Close() })
	h.env["NOTIFY_SOCKET"] = addr.Name
	h.ctx, h.cancel = context.WithCancelCause(context.Background())
	now := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.FixedZone("fixture", 3600))
	h.p = cli.Process{Pid: 777, Dir: dir, Version: "fixture-display", Stdout: h.out, Stderr: h.err, Now: func() time.Time { return now }, Rand: &runBytes{n: 0xa0}, Sink: h.telemetry, EventSink: h.bus, LookupEnv: func(k string) (string, bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.lookups = append(h.lookups, k)
		v, ok := h.env[k]
		return v, ok
	}, Unsetenv: func(k string) error { h.mu.Lock(); defer h.mu.Unlock(); h.unsets = append(h.unsets, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.inherits++
		if fd != 3 {
			return nil, errors.New("wrong fd")
		}
		return h.ln, nil
	}, After: func(d time.Duration) <-chan time.Time {
		if h.ctx.Err() != nil {
			h.afterCancelled.Add(1)
		}
		if d != cli.SweepInterval {
			t.Errorf("After interval = %v", d)
		}
		ch := make(chan time.Time, 1)
		h.timers <- ch
		return ch
	}, Sleep: func(ctx context.Context, _ time.Duration) { <-ctx.Done() }, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: "candidates", Version: "fixture-display", Telemetry: w})
	}}
	h.hc = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	h.client = mcp.NewClient(mcp.ClientConfig{Endpoint: "http://" + h.ln.Addr().String() + "/mcp", HTTPClient: h.hc})
	t.Cleanup(func() {
		if !h.stopped {
			h.stop()
		}
	})
	return h
}
func (h *runHarness) start() {
	h.t.Helper()
	go func() { h.done <- cli.Run(h.ctx, h.p) }()
	_ = h.notify.SetReadDeadline(time.Now().Add(5 * time.Second))
	b := make([]byte, 100)
	n, _, err := h.notify.ReadFromUnix(b)
	if err != nil {
		h.t.Fatalf("readiness: %v; stderr %s", err, h.err.String())
	}
	if string(b[:n]) != "READY=1" {
		h.t.Fatalf("notification %q", b[:n])
	}
}
func (h *runHarness) stop() int {
	h.t.Helper()
	h.hc.CloseIdleConnections()
	h.cancel(errors.New("fixture-stop"))
	select {
	case code := <-h.done:
		h.stopped = true
		return code
	case <-time.After(7 * time.Second):
		h.t.Fatal("Run did not stop")
		return -1
	}
}
func (h *runHarness) call(c identity.Caller, name string, args any) (mcp.Result, map[string]json.RawMessage) {
	h.t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		h.t.Fatal(err)
	}
	r, err := h.client.CallTool(context.Background(), c, name, b)
	if err != nil {
		h.t.Fatalf("%s: %v", name, err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		h.t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(raw, &envelope); err != nil {
		h.t.Fatal(err)
	}
	var value map[string]json.RawMessage
	if !r.IsError() {
		if err = json.Unmarshal(envelope["structuredContent"], &value); err != nil {
			h.t.Fatalf("%s result: %s: %v", name, raw, err)
		}
	}
	return r, value
}
func runText(v map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(v[key], &s)
	return s
}
func (h *runHarness) waitPass(c identity.Caller, id string) map[string]json.RawMessage {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		r, v := h.call(c, "pass", map[string]any{"pass": id})
		if r.IsError() {
			h.t.Fatalf("pass %s refused", id)
		}
		if runText(v, "status") != store.StatusRunning {
			return v
		}
		if ctx.Err() != nil {
			h.t.Fatal("pass did not finish")
		}
	}
}
func (h *runHarness) request(method, path, q string, body []byte) (int, []byte) {
	h.t.Helper()
	r, err := http.NewRequest(method, "http://"+h.ln.Addr().String()+path, bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	r.Header.Set("X-Request-Id", q)
	if path != "/events" && path != "/declarations" {
		r.Header.Set("X-User-Id", "fixture-user")
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.hc.Do(r)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp.StatusCode, b
}
func (h *runHarness) deliver(id, q, status string, depth int) {
	h.t.Helper()
	e := events.Event{ID: id, Time: h.p.Now(), Service: "scrapes", Name: "scrape.finished", Attrs: events.Attrs{"scraper": "fmcsa", "run": "run_fixture", "status": status}, Depth: depth, Seq: 1, Received: h.p.Now()}
	if depth > 0 {
		e.Cause = "evt_1111111111111111"
	}
	b, err := json.Marshal(e)
	if err != nil {
		h.t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		h.t.Fatal(err)
	}
	v["attempt"] = 1
	b, err = json.Marshal(v)
	if err != nil {
		h.t.Fatal(err)
	}
	s, answer := h.request("POST", "/events", q, b)
	if s != 200 {
		h.t.Fatalf("delivery %d: %s", s, answer)
	}
	want := events.OutcomeOK
	if status != "done" {
		want = events.OutcomeSkip
	}
	var out map[string]string
	if err = json.Unmarshal(answer, &out); err != nil || out["outcome"] != want {
		h.t.Fatalf("outcome %s", answer)
	}
}
func runStore(t *testing.T, dir string, now func() time.Time) (*db.DB, *store.Store) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "candidates.db"), Migrations: candidates.Migrations(), Now: now, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, store.New(d, store.Config{Now: now, Rand: &runBytes{n: 0xb0}})
}
func runOnlyState(t *testing.T, dir string) {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 || es[0].Name() != "state" || !es[0].IsDir() {
		t.Fatalf("Dir entries %v", es)
	}
	es, err = os.ReadDir(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if !strings.HasPrefix(e.Name(), "candidates.db") {
			t.Fatalf("unexpected state file %s", e.Name())
		}
	}
	s, err := os.Stat(filepath.Join(dir, "state", "candidates.db"))
	if err != nil || !s.Mode().IsRegular() {
		t.Fatalf("database: %v %v", s, err)
	}
}

// R-60KT-WY1K R-119A-9L3H R-13P3-14KV R-9NW2-FV7B R-A1AY-NCCY R-XHVR-JJLE
// R-3TTL-43TV R-42CV-SI0Q R-43KS-69RF R-QR9U-4LFL R-49OA-34GW R-4C42-UNYA
// R-8S6O-HTX9 R-R335-N3PU R-ZY3R-GAKJ R-R0NC-VK8G   R-ZQSD-5O4D
// R-08GD-3XUM R-06N2-4ORE R-APOY-AR6U R-76JM-ZE8M
func TestRunAssembledChain(t *testing.T) {
	if cli.SweepInterval != time.Hour {
		t.Fatal("sweep constant")
	}
	fake := &runFake{rows: []map[string]any{runRow()}}
	entries := map[string]string{"scrapes": runSocket(t, fake)}
	for _, name := range []string{"events", "telemetry"} {
		entries[name] = runSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	}
	var unrelated atomic.Int64
	for _, name := range []string{"auth", "artifacts", "connections", "cron", "gdrive", "prompts"} {
		entries[name] = runSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { unrelated.Add(1); w.WriteHeader(500) }))
	}
	dir := t.TempDir()
	h := newRunHarness(t, dir, runServices(t, entries))
	h.start()
	runOnlyState(t, dir)
	caller := identity.Caller{UserID: "arbitrary-owner", Email: "arbitrary@example.test", RequestID: "ingest-request"}
	r, v := h.call(caller, "ingest", map[string]any{})
	if r.IsError() {
		t.Fatal("ingest refused")
	}
	id := runText(v, "id")
	if runText(v, "user") != caller.UserID || runText(v, "email") != caller.Email {
		t.Fatalf("ingest identity %v", v)
	}
	finished := h.waitPass(caller, id)
	if runText(finished, "status") != store.StatusDone {
		t.Fatalf("ingest failed %v", finished)
	}
	runOnlyState(t, dir)
	_, list := h.call(caller, "candidates", map[string]any{})
	var cs []struct{ ID string }
	if err := json.Unmarshal(list["candidates"], &cs); err != nil || len(cs) != 1 {
		t.Fatalf("candidates %v %v", list, err)
	}
	cid := cs[0].ID
	_, decision := h.call(identity.Caller{UserID: "ruler", Email: "ruler@example.test", RequestID: "decision-request"}, "exclude", map[string]any{"why": "fixture ruling", "candidate": cid})
	var ruling map[string]json.RawMessage
	_ = json.Unmarshal(decision["ruling"], &ruling)
	if runText(ruling, "user") != "ruler" || runText(ruling, "email") != "ruler@example.test" {
		t.Fatalf("ruling identity %v", ruling)
	}
	for name, args := range map[string]any{"prospect_lists": map[string]any{}, "candidate": map[string]any{"candidate": cid}, "passes": map[string]any{}, "pass": map[string]any{"pass": id}} {
		rr, _ := h.call(caller, name, args)
		if rr.IsError() {
			t.Fatalf("%s refused", name)
		}
	}
	for _, path := range []string{"/", "/tools", "/about", "/candidates/" + cid, "/passes/" + id} {
		status, _ := h.request("GET", path, "page-request", nil)
		if status != 200 {
			t.Fatalf("page %s status %d", path, status)
		}
	}
	h.deliver("evt_2222222222222222", "skipped-request", "failed", 0)
	h.deliver("evt_3333333333333333", "delivery-request", "done", 2)
	_, pl := h.call(caller, "passes", map[string]any{})
	var ps []map[string]json.RawMessage
	if err := json.Unmarshal(pl["passes"], &ps); err != nil {
		t.Fatal(err)
	}
	deliveryID := ""
	for _, p := range ps {
		if runText(p, "trigger") == "run_fixture" {
			deliveryID = runText(p, "id")
		}
	}
	if deliveryID == "" {
		t.Fatal("delivery pass missing")
	}
	dp := h.waitPass(caller, deliveryID)
	if runText(dp, "status") != store.StatusDone {
		t.Fatalf("delivery pass %v", dp)
	}
	for _, key := range []string{"rows", "unchanged"} {
		if string(dp[key]) != "1" {
			t.Fatalf("%s = %s", key, dp[key])
		}
	}
	for _, key := range []string{"changed", "gone"} {
		if string(dp[key]) != "0" {
			t.Fatalf("%s = %s", key, dp[key])
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			_, err := h.client.CallTool(context.Background(), caller, "passes", json.RawMessage(`{}`))
			if err != nil {
				t.Errorf("concurrent passes: %v", err)
			}
		})
	}
	wg.Wait()
	if code := h.stop(); code != cli.ExitSuccess {
		t.Fatalf("exit %d stderr %s", code, h.err.String())
	}
	runOnlyState(t, dir)
	if h.p.Rand.(*runBytes).overlap.Load() {
		t.Fatal("concurrent Rand.Read")
	}
	if unrelated.Load() != 0 {
		t.Fatalf("unrelated sibling calls %d", unrelated.Load())
	}
	h.mu.Lock()
	unsets := append([]string(nil), h.unsets...)
	lookups := append([]string(nil), h.lookups...)
	inherits := h.inherits
	h.mu.Unlock()
	if inherits != 1 || !reflect.DeepEqual(unsets, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatalf("socket seam %d %v", inherits, unsets)
	}
	allowed := map[string]bool{"DRAIN_SECONDS": true, "CANDIDATES_KEEP_PASSES": true, "CANDIDATES_ROUTE_ORIGIN": true, "GOOGLE_MAPS_API_KEY": true, services.Variable: true, "LISTEN_PID": true, "LISTEN_FDS": true, "NOTIFY_SOCKET": true, "IKIGENBA_SANDBOX": true}
	for _, k := range lookups {
		if !allowed[k] {
			t.Fatalf("environment lookup %s", k)
		}
	}
	if h.out.String() != "" || h.err.String() != "" {
		t.Fatalf("streams stdout %q stderr %q", h.out.String(), h.err.String())
	}
	evs := h.telemetry.Events()
	if len(evs) < 2 || evs[0].Name != "service.started" || evs[len(evs)-1].Name != "service.stopping" {
		t.Fatalf("lifecycle events %v", evs)
	}
	if !reflect.DeepEqual(evs[0].Attrs, telemetry.Attrs{"version": h.p.Version}) || evs[0].RequestID != "" || evs[0].User != "" {
		t.Fatalf("start %v", evs[0])
	}
	stopping := 0
	for _, e := range evs {
		if e.Name == "service.stopping" {
			stopping++
		}
	}
	if stopping != 1 || h.afterCancelled.Load() != 0 {
		t.Fatalf("shutdown stopping=%d After after cancel=%d", stopping, h.afterCancelled.Load())
	}
	last := evs[len(evs)-1]
	if !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "fixture-stop"}) || last.RequestID != "" || last.User != "" {
		t.Fatalf("stop %v", last)
	}
	allowedEvents := map[string]bool{}
	for _, name := range []string{"service.started", "service.stopping", "request.started", "request.finished", "tool.called", "event.lost", "sibling.called", "pass.started", "pass.finished"} {
		allowedEvents[name] = true
	}
	calls := fake.Requests()
	sibling := 0
	skip := 0
	for _, e := range evs {
		if !allowedEvents[e.Name] {
			t.Fatalf("event name %s", e.Name)
		}
		if e.RequestID == "skipped-request" {
			skip++
			if e.Name != "request.started" && e.Name != "request.finished" {
				t.Fatalf("skip event %v", e)
			}
		}
		if e.Name == "sibling.called" {
			sibling++
			if e.Attrs["target"] != "scrapes" || e.Attrs["method"] != "GET" {
				t.Fatalf("sibling %v", e)
			}
			pair := 0
			for _, rq := range calls {
				if rq.Path == e.Attrs["path"] && rq.Header.Get("X-Request-Id") == e.RequestID && rq.Header.Get("X-User-Id") == e.User {
					pair++
				}
			}
			if pair == 0 {
				t.Fatalf("sibling unmatched %v", e)
			}
		}
	}
	if sibling != len(calls) || skip != 2 {
		t.Fatalf("sibling count %d/%d skip %d", sibling, len(calls), skip)
	}
	for _, q := range []string{"skipped-request", "delivery-request"} {
		start, finish := 0, 0
		for _, e := range evs {
			if e.RequestID != q {
				continue
			}
			switch e.Name {
			case "request.started":
				start++
				if e.User != "" {
					t.Fatalf("delivery request start user %v", e)
				}
			case "pass.started", "pass.finished":
				if q != "delivery-request" || (e.Attrs["pass"] != deliveryID && e.Attrs["origin"] != store.OriginPass) {
					t.Fatalf("delivery pass event %v", e)
				}
			case "sibling.called":
				if q != "delivery-request" || e.User != "candidates" {
					t.Fatalf("delivery sibling event %v", e)
				}
			case "request.finished":
				finish++
				if e.User != "" || e.Attrs["status"] != int64(http.StatusOK) {
					t.Fatalf("delivery request finish %v", e)
				}
			default:
				t.Fatalf("unexpected delivery event %v", e)
			}
		}
		if start != 1 || finish != 1 {
			t.Fatalf("delivery %s request pair %d/%d", q, start, finish)
		}
	}
	for _, q := range []string{caller.RequestID, "delivery-request"} {
		fakeCount, trailCount := 0, 0
		for _, rq := range calls {
			if rq.Header.Get("X-Request-Id") == q {
				fakeCount++
			}
		}
		for _, e := range evs {
			if e.Name == "sibling.called" && e.RequestID == q {
				trailCount++
			}
		}
		if fakeCount != trailCount {
			t.Fatalf("sibling count %s fake=%d trail=%d", q, fakeCount, trailCount)
		}
	}
	currentReads := 0
	for _, rq := range calls {
		if rq.Path == "/scrapers/fmcsa/current" {
			currentReads++
		}
	}
	if currentReads != 1 {
		t.Fatalf("current fmcsa reads=%d", currentReads)
	}
	for _, rq := range calls {
		switch rq.Header.Get("X-Request-Id") {
		case "delivery-request":
			if rq.Header.Get("X-User-Id") != "candidates" || rq.Header.Get("X-User-Email") != "" {
				t.Fatalf("service identity %v", rq)
			}
		case caller.RequestID:
			if rq.Header.Get("X-User-Id") != caller.UserID || rq.Header.Get("X-User-Email") != caller.Email {
				t.Fatalf("caller identity %v", rq)
			}
		default:
			t.Fatalf("unexpected sibling request %v", rq)
		}
		if strings.HasPrefix(rq.Path, "/scrapers/") && rq.Path != "/scrapers/fmcsa/current" && rq.Path != "/scrapers/ppp/current" {
			t.Fatalf("registry requested %s", rq.Path)
		}
	}
	bus := []events.Event{}
	followIDs := map[string]bool{}
	for _, e := range h.bus.Events() {
		if e.Attrs["origin"] == store.OriginPass {
			followIDs[e.Attrs["pass"].(string)] = true
			continue
		}
		bus = append(bus, e)
	}
	for _, e := range bus {
		if e.Name == "pass.finished" {
			record := finished
			if e.RequestID == "delivery-request" {
				record = dp
			}
			for _, key := range []string{"kind", "origin", "trigger", "status", "reason", "rows", "new", "changed", "unchanged", "gone", "created", "linked", "judged", "unranged", "derived"} {
				raw, err := json.Marshal(e.Attrs[key])
				if err != nil {
					t.Fatal(err)
				}
				want := record[key]
				if len(want) == 0 {
					want = json.RawMessage(`""`)
				}
				if !bytes.Equal(raw, want) {
					t.Fatalf("finished event %s=%s record=%s", key, raw, want)
				}
			}
		}
	}
	if len(bus) != 4 {
		t.Fatalf("bus events %d", len(bus))
	}
	for _, te := range evs {
		if followIDs[fmt.Sprint(te.Attrs["pass"])] || (te.Name != "pass.started" && te.Name != "pass.finished") {
			continue
		}
		matches := 0
		for _, e := range bus {
			if te.Name == e.Name && te.RequestID == e.RequestID && te.User == e.User && reflect.DeepEqual(map[string]any(te.Attrs), map[string]any(e.Attrs)) {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("unmatched trail pass event %v matches=%d", te, matches)
		}
	}
	for i, e := range bus {
		if e.Name != []string{"pass.started", "pass.finished", "pass.started", "pass.finished"}[i] {
			t.Fatalf("bus order %v", bus)
		}
		wantID, wantTrigger, wantRequest, wantUser := id, store.TriggerIngest, caller.RequestID, caller.UserID
		if i >= 2 {
			wantID, wantTrigger, wantRequest, wantUser = deliveryID, "run_fixture", "delivery-request", ""
		}
		if e.Attrs["pass"] != wantID || e.Attrs["trigger"] != wantTrigger || e.RequestID != wantRequest || e.User != wantUser {
			t.Fatalf("pass event identity %v", e)
		}
		matches := 0
		for _, te := range evs {
			if te.Name == e.Name && te.RequestID == e.RequestID && te.User == e.User && reflect.DeepEqual(map[string]any(te.Attrs), map[string]any(e.Attrs)) {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("bus/trail pair %v matches %d", e, matches)
		}
		if i >= 2 && (e.Cause != "evt_3333333333333333" || e.Depth != 3 || e.RequestID != "delivery-request" || e.User != "") {
			t.Fatalf("delivery event context %v", e)
		}
	}
	d, st := runStore(t, dir, h.p.Now)
	p, err := st.GetPass(context.Background(), id)
	if err != nil || p.ID != id || p.UserID != caller.UserID || p.UserEmail != caller.Email || p.RequestID != caller.RequestID {
		t.Fatalf("persisted ingest %v %v", p, err)
	}
	p, err = st.GetPass(context.Background(), deliveryID)
	if err != nil || p.Delivery != "evt_3333333333333333" || p.UserEmail != "" {
		t.Fatalf("persisted delivery %v %v", p, err)
	}
	ls, err := st.Links(context.Background(), cid)
	if err != nil || len(ls) != 1 {
		t.Fatalf("links %v %v", ls, err)
	}
	// Compare the stored reading with the published source through a separately decoded envelope.
	row := runRow()
	sc := runDecodedRow(row)
	if !reflect.DeepEqual(ls[0].Reading, fmcsa.New().Read(sc)) {
		t.Fatalf("source reading %v", ls[0].Reading)
	}
	rulings, err := st.Rulings(context.Background(), cid)
	if err != nil || len(rulings) != 1 || rulings[0].UserID != "ruler" || rulings[0].UserEmail != "ruler@example.test" {
		t.Fatalf("persisted ruling %v %v", rulings, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

func runDecodedRow(row map[string]any) scrapes.Row {
	address := row["address"].(map[string]any)
	facts := map[string]json.RawMessage{}
	for k, v := range row["facts"].(map[string]any) {
		b, _ := json.Marshal(v)
		facts[k] = b
	}
	raw, _ := json.Marshal(row["raw"])
	scraped, _ := time.Parse(time.RFC3339, row["scraped"].(string))
	return scrapes.Row{Scraper: row["scraper"].(string), Run: row["run"].(string), SourceID: row["source_id"].(string), Scraped: scraped, Digest: row["digest"].(string), Name: row["name"].(string), Names: row["names"].([]string), Address: scrapes.Address{Street: address["street"].(string), City: address["city"].(string), State: address["state"].(string), Zip: address["zip"].(string), County: address["county"].(string)}, People: []scrapes.Person{}, Facts: facts, Raw: raw}
}

// R-Y9L9-RE9E R-XXE9-XOUG R-8B43-51JJ
func TestRunInjectedClockAndBytes(t *testing.T) {
	fake := &runFake{}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, fake)}))
	h.p.Rand = &runBytes{n: 0x5a, fixed: true}
	h.start()
	c := identity.Caller{UserID: "fixture-user"}
	r, v := h.call(c, "ingest", map[string]any{})
	if r.IsError() || runText(v, "id") != "pass_5a5a5a5a5a5a5a5a" {
		t.Fatalf("injected id %v", v)
	}
	h.waitPass(c, runText(v, "id"))
	h.stop()
	evs := h.telemetry.Events()
	bus := h.bus.Events()
	if len(evs) < 2 || evs[0].Name != "service.started" || evs[len(evs)-1].Name != "service.stopping" || len(bus) != 2 || bus[0].Name != "pass.started" || bus[1].Name != "pass.finished" {
		t.Fatalf("captured lifecycle %v %v", evs, bus)
	}
	want := h.p.Now().UTC().Truncate(time.Microsecond)
	for _, e := range h.telemetry.Events() {
		if !e.Time.Equal(want) {
			t.Fatalf("event time %v", e)
		}
		if (e.Name == "request.started" || e.Name == "request.finished") && e.RequestID != strings.Repeat("5a", 16) {
			t.Fatalf("request id %v", e)
		}
	}
	for _, e := range h.bus.Events() {
		if !e.Time.Equal(want) || e.ID != "evt_5a5a5a5a5a5a5a5a" {
			t.Fatalf("bus seam %v", e)
		}
	}
}

// R-7Q2A-VGJ0
func TestRunFreshDeliveryCounts(t *testing.T) {
	row := runRow()
	second := runRow()
	second["source_id"] = "102"
	second["name"] = "Different Hauler"
	second["names"] = []string{"Different Hauler"}
	f := &runFake{rows: []map[string]any{row, second}}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.start()
	h.deliver("evt_4444444444444444", "fresh-request", "done", 0)
	c := identity.Caller{UserID: "reader"}
	_, l := h.call(c, "passes", map[string]any{})
	var ps []map[string]json.RawMessage
	_ = json.Unmarshal(l["passes"], &ps)
	if len(ps) != 1 {
		t.Fatalf("passes %v", l)
	}
	id := runText(ps[0], "id")
	p := h.waitPass(c, id)
	for _, k := range []string{"rows", "new"} {
		if string(p[k]) != "2" {
			t.Fatalf("%s = %s", k, p[k])
		}
	}
	for _, k := range []string{"changed", "unchanged", "gone"} {
		if string(p[k]) != "0" {
			t.Fatalf("%s = %s", k, p[k])
		}
	}
	var created, linked int64
	_ = json.Unmarshal(p["created"], &created)
	_ = json.Unmarshal(p["linked"], &linked)
	if created+linked != 2 || created > 2 {
		t.Fatalf("counts %v", p)
	}
	h.stop()
	_, st := runStore(t, h.p.Dir, h.p.Now)
	saved, err := st.GetPass(context.Background(), id)
	if err != nil || saved.Delivery != "evt_4444444444444444" || saved.UserEmail != "" {
		t.Fatalf("saved delivery %v %v", saved, err)
	}
}

// R-7OUE-HOSB R-ZJGY-V1O7 R-ZKOV-8TEW
func TestRunSettlesLoadsAndSweepsBeforeAndAfterReady(t *testing.T) {
	dir := t.TempDir()
	h := newRunHarness(t, dir, "")
	_, st := runStore(t, dir, h.p.Now)
	ctx := context.Background()
	if err := st.ReplaceZipDrive(ctx, map[string]int64{"00000": 1}); err != nil {
		t.Fatal(err)
	}
	var oldDone, oldFailed string
	doneIDs, failedIDs := []string{}, []string{}
	for i := 0; i < 3; i++ {
		p, err := st.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "seed"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = st.FinishPass(ctx, p.ID, store.Counts{}); err != nil {
			t.Fatal(err)
		}
		doneIDs = append(doneIDs, p.ID)
		if i == 0 {
			oldDone = p.ID
		}
	}
	for i := 0; i < 2; i++ {
		p, err := st.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "seed"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = st.FailPass(ctx, p.ID, store.ReasonScrapes); err != nil {
			t.Fatal(err)
		}
		failedIDs = append(failedIDs, p.ID)
		if i == 0 {
			oldFailed = p.ID
		}
	}
	running, err := st.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "run_seed", UserID: "seed", UserEmail: "seed@example.test", RequestID: "seed-request", Delivery: "evt_8888888888888888"})
	if err != nil {
		t.Fatal(err)
	}
	h.env["CANDIDATES_KEEP_PASSES"] = "1"
	h.start()
	for z, want := range candidates.ZipDrive() {
		got, err := st.ZipMinutes(ctx, z)
		if err != nil || got != want {
			t.Fatalf("zip %s %d %v", z, got, err)
		}
	}
	if _, err = st.ZipMinutes(ctx, "00000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale zip %v", err)
	}
	p, err := st.GetPass(ctx, running.ID)
	expected := running
	expected.Status = store.StatusFailed
	expected.Reason = store.ReasonInterrupted
	expected.Finished = h.p.Now().UTC().Truncate(time.Second)
	if err != nil || !reflect.DeepEqual(p, expected) {
		t.Fatalf("recovered %v %v", p, err)
	}
	c := identity.Caller{UserID: "reader"}
	for _, id := range []string{oldDone, oldFailed} {
		r, _ := h.call(c, "pass", map[string]any{"pass": id})
		runAssertNotFound(t, r)
	}
	for i, id := range doneIDs {
		r, _ := h.call(c, "pass", map[string]any{"pass": id})
		if i < 2 {
			runAssertNotFound(t, r)
		} else if r.IsError() {
			t.Fatal("newest done pass refused")
		}
	}
	for _, id := range failedIDs {
		r, _ := h.call(c, "pass", map[string]any{"pass": id})
		runAssertNotFound(t, r)
	}
	// A fresh failed pass makes the recovered failed pass old; driving After must prune it.
	newer, err := st.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.FailPass(ctx, newer.ID, store.ReasonScrapes); err != nil {
		t.Fatal(err)
	}
	timer := runTimer(t, h)
	timer <- h.p.Now()
	select {
	case <-h.timers:
	case <-time.After(5 * time.Second):
		t.Fatal("sweeper did not rearm")
	}
	r, _ := h.call(c, "pass", map[string]any{"pass": running.ID})
	if !r.IsError() {
		t.Fatal("sweep kept old failed pass")
	}
	r, _ = h.call(c, "pass", map[string]any{"pass": newer.ID})
	if r.IsError() {
		t.Fatal("sweep removed newest failed pass")
	}
	h.stop()
	evs := h.telemetry.Events()
	if evs[0].Name != "service.started" {
		t.Fatal("settling preceded start")
	}
	seenRequest := false
	for _, e := range evs {
		if e.Name == "request.started" {
			seenRequest = true
		}
		if e.Name == "pass.finished" && e.Attrs["pass"] == running.ID && seenRequest {
			t.Fatal("settling event after request")
		}
	}
}

// R-44SO-K1I4
func TestRunMissingServicesDoesNotRefuseStart(t *testing.T) {
	for _, kind := range []string{"unset", "absent", "invalid", "empty"} {
		t.Run(kind, func(t *testing.T) {
			path := ""
			if kind != "unset" {
				path = filepath.Join(t.TempDir(), "services.json")
				if kind != "absent" {
					b := []byte("invalid")
					if kind == "empty" {
						b = []byte(`{"services":[]}`)
					}
					if err := os.WriteFile(path, b, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			h := newRunHarness(t, t.TempDir(), path)
			if kind == "unset" {
				delete(h.env, services.Variable)
			}
			h.start()
			r, _ := h.call(identity.Caller{UserID: "reader"}, "passes", map[string]any{})
			if r.IsError() {
				t.Fatal("passes refused")
			}
			h.stop()
		})
	}
}

// R-ADHY-H1RW
func TestRunKeepsScrapesSocketSelectedAtStart(t *testing.T) {
	first, second := &runFake{}, &runFake{}
	path := filepath.Join(t.TempDir(), "services.json")
	write := func(socket string) {
		t.Helper()
		b, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "scrapes", "socket": socket, "url": "https://scrapes.example.test", "description": "fixture", "enabled": true, "mcp": false}}})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(runSocket(t, first))
	h := newRunHarness(t, t.TempDir(), path)
	h.start()
	c := identity.Caller{UserID: "reader"}
	_, v := h.call(c, "ingest", map[string]any{})
	h.waitPass(c, runText(v, "id"))
	write(runSocket(t, second))
	_, v = h.call(c, "ingest", map[string]any{})
	h.waitPass(c, runText(v, "id"))
	h.stop()
	if len(first.Requests()) != 6 || len(second.Requests()) != 0 {
		t.Fatalf("socket reads first %d second %d", len(first.Requests()), len(second.Requests()))
	}
}

// R-2M6B-6WBI
func TestRunInterruptsHeldScrapesRead(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	arrived := make(chan struct{}, 1)
	f := &runFake{hold: hold, arrived: arrived}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.env["DRAIN_SECONDS"] = "1"
	h.start()
	c := identity.Caller{UserID: "reader"}
	_, v := h.call(c, "ingest", map[string]any{})
	id := runText(v, "id")
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("rows read not begun")
	}
	started := time.Now()
	if code := h.stop(); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	runAssertSilent(t, h)
	if time.Since(started) > 2*time.Second {
		t.Fatal("stop exceeded drain plus second")
	}
	_, st := runStore(t, h.p.Dir, h.p.Now)
	p, err := st.GetPass(context.Background(), id)
	if err != nil || p.Status != store.StatusFailed || p.Reason != store.ReasonInterrupted {
		t.Fatalf("interrupted pass %v %v", p, err)
	}
}

// R-981D-GUKA
func TestRunUnreachableScrapesIngestAndDelivery(t *testing.T) {
	d, err := os.MkdirTemp("", "cand-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": filepath.Join(d, "absent")}))
	h.start()
	c := identity.Caller{UserID: "reader"}
	r, v := h.call(c, "ingest", map[string]any{})
	if r.IsError() || runText(v, "status") != store.StatusRunning {
		t.Fatalf("ingest %v", v)
	}
	p := h.waitPass(c, runText(v, "id"))
	if runText(p, "status") != store.StatusFailed || runText(p, "reason") != store.ReasonScrapes {
		t.Fatalf("failed ingest %v", p)
	}
	h.deliver("evt_5555555555555555", "delivery", "done", 0)
	_, l := h.call(c, "passes", map[string]any{})
	var ps []map[string]json.RawMessage
	_ = json.Unmarshal(l["passes"], &ps)
	found := false
	for _, p := range ps {
		if runText(p, "trigger") == "run_fixture" {
			found = true
			if runText(p, "status") != store.StatusFailed || runText(p, "reason") != store.ReasonScrapes {
				t.Fatalf("failed delivery %v", p)
			}
		}
	}
	if !found {
		t.Fatal("delivery pass missing")
	}
	h.stop()
	runAssertSilent(t, h)
}

type runRefusalListener struct {
	net.Listener
	accepts atomic.Int64
}

func (ln *runRefusalListener) Accept() (net.Conn, error) {
	ln.accepts.Add(1)
	return nil, errors.New("unexpected connection acceptance")
}

// R-3V1H-HVKK
func TestRunOpenRefusalSendsNoReadyAndAcceptsNoConnection(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	if err := os.WriteFile(filepath.Join(h.p.Dir, "state"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	_, openErr := db.Open(h.ctx, db.Config{Path: filepath.Join(h.p.Dir, "state", "candidates.db"), Migrations: candidates.Migrations(), Now: h.p.Now})
	if openErr == nil {
		t.Fatal("fixture database opened")
	}
	ln := &runRefusalListener{Listener: h.ln}
	h.p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
	code := cli.Run(h.ctx, h.p)
	h.stopped = true
	want := "candidates: cannot open database state/candidates.db: " + strings.ReplaceAll(openErr.Error(), "\n", " ") + "\n"
	if code != cli.ExitServerFailed || h.out.String() != "" || h.err.String() != want || h.err.writes != 1 || ln.accepts.Load() != 0 || len(h.telemetry.Events()) != 0 || len(h.bus.Events()) != 0 {
		t.Fatalf("refusal code=%d out=%q err=%q writes=%d accepts=%d", code, h.out.String(), h.err.String(), h.err.writes, ln.accepts.Load())
	}
	_ = h.notify.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, _, err := h.notify.ReadFromUnix(make([]byte, 100)); err == nil {
		t.Fatal("refused start notified")
	}
}

type runFailTelemetry struct{}

func (runFailTelemetry) Deliver(context.Context, telemetry.Event) error {
	return errors.New("fixture refusal")
}

type runFailBus struct{}

func (runFailBus) Deliver(context.Context, events.Event) error { return errors.New("fixture refusal") }

// R-QUJU-YPIZ
func TestRunSinkFailuresDoNotChangePass(t *testing.T) {
	var answers []map[string]json.RawMessage
	var initial []map[string]json.RawMessage
	for _, failing := range []bool{false, true} {
		f := &runFake{}
		h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
		if failing {
			h.p.Sink = runFailTelemetry{}
			h.p.EventSink = runFailBus{}
		}
		h.start()
		c := identity.Caller{UserID: "reader"}
		r, v := h.call(c, "ingest", map[string]any{})
		if r.IsError() {
			t.Fatal("sink failure refused ingest")
		}
		passID := runText(v, "id")
		delete(v, "id")
		delete(v, "started")
		delete(v, "finished")
		initial = append(initial, v)
		p := h.waitPass(c, passID)
		delete(p, "id")
		delete(p, "started")
		delete(p, "finished")
		answers = append(answers, p)
		h.stop()
	}
	if !reflect.DeepEqual(initial[0], initial[1]) {
		t.Fatalf("sinks changed initial ingest %v %v", initial[0], initial[1])
	}
	if !reflect.DeepEqual(answers[0], answers[1]) {
		t.Fatalf("sinks changed pass %v %v", answers[0], answers[1])
	}
}

// R-12H6-NCU6
func TestRunDefaultsUseWorkingDirectoryAndSocketSinks(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var mu sync.Mutex
	trailEvents := []telemetry.Event{}
	busEvents := []events.Event{}
	ts := runSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e telemetry.Event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Errorf("telemetry decode: %v", err)
		}
		mu.Lock()
		trailEvents = append(trailEvents, e)
		mu.Unlock()
		w.WriteHeader(204)
	}))
	es := runSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e events.Event
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Errorf("bus decode: %v", err)
		}
		mu.Lock()
		busEvents = append(busEvents, e)
		mu.Unlock()
		w.WriteHeader(204)
	}))
	f := &runFake{}
	path := runServices(t, map[string]string{"telemetry": ts, "events": es, "scrapes": runSocket(t, f)})
	h := newRunHarness(t, "", path)
	t.Setenv(services.Variable, path)
	h.p.Unsetenv = nil
	h.p.Sink = nil
	h.p.EventSink = nil
	h.start()
	c := identity.Caller{UserID: "reader"}
	_, v := h.call(c, "ingest", map[string]any{})
	h.waitPass(c, runText(v, "id"))
	h.stop()
	_, st := runStore(t, dir, h.p.Now)
	p, err := st.GetPass(context.Background(), runText(v, "id"))
	if err != nil || !p.Started.Equal(h.p.Now().UTC().Truncate(time.Second)) || !p.Finished.Equal(h.p.Now().UTC().Truncate(time.Second)) {
		t.Fatalf("working-directory clock %v %v", p, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(trailEvents) == 0 || len(busEvents) != 2 {
		t.Fatalf("default sinks telemetry %d bus %d", len(trailEvents), len(busEvents))
	}
	for _, e := range trailEvents {
		if !e.Time.Equal(h.p.Now().UTC().Truncate(time.Microsecond)) {
			t.Fatalf("default sink time %v", e)
		}
	}
}

// R-4AW6-GW7L
func TestRunDrainRefusesUnfinishedRequest(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	arrived := make(chan struct{}, 1)
	h := newRunHarness(t, t.TempDir(), "")
	h.env["DRAIN_SECONDS"] = "1"
	h.p.MCP = func(w *telemetry.Writer) *mcp.Server {
		srv := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: w})
		mcp.AddTool(srv, mcp.Tool[struct{}, struct{}]{Name: "fixture_hold", Description: tools.DescribePasses, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) {
			arrived <- struct{}{}
			<-hold
			return struct{}{}, nil
		}})
		return srv
	}
	h.start()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		_, _ = h.client.CallTool(context.Background(), identity.Caller{UserID: "reader"}, "fixture_hold", json.RawMessage(`{}`))
	}()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not begin")
	}
	if code := h.stop(); code != cli.ExitServerFailed {
		t.Fatalf("unfinished request exit %d stderr %q", code, h.err.String())
	}
	want := "candidates: " + (&server.DrainError{Unfinished: 1}).Error() + "\n"
	if !strings.HasSuffix(h.err.String(), want) {
		t.Fatalf("drain line %q", h.err.String())
	}
	select {
	case <-requestDone:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not close")
	}
}

// R-13P3-14KV
func TestRunConcurrentRequestsWhilePassWorksUseSerializedRandom(t *testing.T) {
	hold := make(chan struct{})
	arrived := make(chan struct{}, 1)
	f := &runFake{hold: hold, arrived: arrived}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.start()
	c := identity.Caller{UserID: "reader"}
	_, v := h.call(c, "ingest", map[string]any{})
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("rows not begun")
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			_, err := h.client.CallTool(context.Background(), c, "passes", json.RawMessage(`{}`))
			if err != nil {
				t.Errorf("request %v", err)
			}
		})
	}
	wg.Wait()
	close(hold)
	h.waitPass(c, runText(v, "id"))
	h.stop()
	if h.p.Rand.(*runBytes).overlap.Load() {
		t.Fatal("random reads overlapped")
	}
}

type runBlockedTelemetry struct {
	calls atomic.Int64
	began chan struct{}
}

func (s *runBlockedTelemetry) Deliver(ctx context.Context, _ telemetry.Event) error {
	s.calls.Add(1)
	select {
	case s.began <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

type runBlockedBus struct {
	calls atomic.Int64
	began chan struct{}
}

func (s *runBlockedBus) Deliver(ctx context.Context, _ events.Event) error {
	s.calls.Add(1)
	select {
	case s.began <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

// R-49OA-34GW
func TestRunSinkDeadlinePreventsLaterDeliveryCalls(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	f := &runFake{hold: hold}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	ts := &runBlockedTelemetry{began: make(chan struct{}, 1)}
	es := &runBlockedBus{began: make(chan struct{}, 1)}
	h.p.Sink = ts
	h.p.EventSink = es
	h.env["DRAIN_SECONDS"] = "1"
	h.start()
	_, v := h.call(identity.Caller{UserID: "reader"}, "ingest", map[string]any{})
	if runText(v, "id") == "" {
		t.Fatal("pass missing")
	}
	for _, began := range []chan struct{}{ts.began, es.began} {
		select {
		case <-began:
		case <-time.After(5 * time.Second):
			t.Fatal("sink call not begun")
		}
	}
	h.stop()
	if ts.calls.Load() != 1 || es.calls.Load() != 1 {
		t.Fatalf("sink began later calls %d %d", ts.calls.Load(), es.calls.Load())
	}
	if h.err.String() == "" {
		t.Fatal("lost events produced no diagnostic")
	}
}

// R-42CV-SI0Q
func TestRunNotificationIsSingleAndOptional(t *testing.T) {
	for _, mode := range []string{"set", "unset", "empty"} {
		t.Run(mode, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir(), "")
			switch mode {
			case "set":
				h.start()
			case "unset":
				delete(h.env, "NOTIFY_SOCKET")
				go func() { h.done <- cli.Run(h.ctx, h.p) }()
			case "empty":
				h.env["NOTIFY_SOCKET"] = ""
				go func() { h.done <- cli.Run(h.ctx, h.p) }()
			}
			r, _ := h.call(identity.Caller{UserID: "reader"}, "passes", map[string]any{})
			if r.IsError() {
				t.Fatal("passes refused")
			}
			_ = h.notify.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
			if _, _, err := h.notify.ReadFromUnix(make([]byte, 100)); err == nil {
				t.Fatal("unexpected notification datagram")
			}
			h.stop()
		})
	}
}

// R-R6QU-SEXX
func TestRunBusyRefusalsEmitNoPassEvents(t *testing.T) {
	hold := make(chan struct{})
	f := &runFake{hold: hold, arrived: make(chan struct{}, 1)}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.start()
	c := identity.Caller{UserID: "reader", RequestID: "first"}
	_, v := h.call(c, "ingest", map[string]any{})
	select {
	case <-f.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("rows not begun")
	}
	r, _ := h.call(identity.Caller{UserID: "reader", RequestID: "refused"}, "ingest", map[string]any{})
	if !r.IsError() {
		t.Fatal("busy ingest accepted")
	}
	e := events.Event{ID: "evt_7777777777777777", Time: h.p.Now(), Service: "scrapes", Name: "scrape.finished", Attrs: events.Attrs{"scraper": "fmcsa", "run": "run_fixture", "status": "running"}, Seq: 1, Received: h.p.Now()}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	_ = json.Unmarshal(b, &env)
	env["attempt"] = 1
	b, _ = json.Marshal(env)
	status, body := h.request("POST", "/events", "skipped-delivery", b)
	var out map[string]string
	_ = json.Unmarshal(body, &out)
	if status != 200 || out["outcome"] != events.OutcomeSkip {
		t.Fatalf("skipped delivery %d %s", status, body)
	}
	r, _ = h.call(identity.Caller{UserID: "reader", RequestID: "refused-enrich"}, "enrich", tools.EnrichArgs{Kind: "unknown-kind"})
	if !r.IsError() {
		t.Fatal("unknown enrich accepted")
	}
	h.call(c, "passes", tools.PassesArgs{})
	h.request("GET", "/", "page-request", nil)
	close(hold)
	h.waitPass(c, runText(v, "id"))
	h.stop()
	runAssertSilent(t, h)
	for _, e := range h.telemetry.Events() {
		if (e.Name == "pass.started" || e.Name == "pass.finished") && (e.RequestID == "refused" || e.RequestID == "skipped-delivery" || e.RequestID == "refused-enrich" || e.RequestID == "page-request") {
			t.Fatalf("refusal emitted %v", e)
		}
	}
	if len(h.bus.Events()) != 2 {
		t.Fatalf("refusals emitted bus events %v", h.bus.Events())
	}
}

func runTimer(t *testing.T, h *runHarness) chan time.Time {
	t.Helper()
	select {
	case ch := <-h.timers:
		return ch
	case <-time.After(5 * time.Second):
		t.Fatal("sweeper timer not received")
		return nil
	}
}
func runAssertNotFound(t *testing.T, r mcp.Result) {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Content           []struct{ Type, Text string }
		StructuredContent json.RawMessage
	}
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if !r.IsError() || len(v.Content) != 1 || v.Content[0].Type != "text" || v.Content[0].Text != "not_found: "+tools.RefuseNotFound || len(v.StructuredContent) != 0 {
		t.Fatalf("not_found refusal %s", b)
	}
}

// R-8AIP-87V7
func TestRunSweepPrunesDoneAndPreservesRunning(t *testing.T) {
	f := &runFake{}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	baseNow := h.p.Now()
	var offset atomic.Int64
	h.p.Now = func() time.Time { return baseNow.Add(time.Duration(offset.Load()) * time.Second) }
	_, st := runStore(t, h.p.Dir, h.p.Now)
	ctx := context.Background()
	seed, err := st.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.FinishPass(ctx, seed.ID, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	h.env["CANDIDATES_KEEP_PASSES"] = "1"
	h.start()
	c := identity.Caller{UserID: "reader"}
	r, _ := h.call(c, "pass", map[string]any{"pass": seed.ID})
	if r.IsError() {
		t.Fatal("seed not kept before ingest")
	}
	offset.Store(1)
	_, v := h.call(c, "ingest", map[string]any{})
	doneID := runText(v, "id")
	h.waitPass(c, doneID)
	hold := make(chan struct{})
	f.mu.Lock()
	f.hold = hold
	f.arrived = make(chan struct{}, 1)
	arrived := f.arrived
	f.mu.Unlock()
	offset.Store(2)
	_, v = h.call(c, "ingest", map[string]any{})
	runningID := runText(v, "id")
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("running rows read not begun")
	}
	timer := runTimer(t, h)
	timer <- h.p.Now()
	timer = runTimer(t, h)
	r, _ = h.call(c, "pass", map[string]any{"pass": seed.ID})
	runAssertNotFound(t, r)
	r, _ = h.call(c, "pass", map[string]any{"pass": doneID})
	if r.IsError() {
		t.Fatal("sweep removed newest done")
	}
	r, v = h.call(c, "pass", map[string]any{"pass": runningID})
	if r.IsError() || runText(v, "status") != store.StatusRunning {
		t.Fatal("sweep removed running pass")
	}
	close(hold)
	h.waitPass(c, runningID)
	timer <- h.p.Now()
	runTimer(t, h)
	r, _ = h.call(c, "pass", map[string]any{"pass": doneID})
	runAssertNotFound(t, r)
	r, _ = h.call(c, "pass", map[string]any{"pass": runningID})
	if r.IsError() {
		t.Fatal("sweep removed newly done pass")
	}
	h.stop()
}

type runNotifyListener struct {
	net.Listener
	notify   *net.UnixConn
	once     sync.Once
	observed chan error
}

func (ln *runNotifyListener) Accept() (net.Conn, error) {
	c, err := ln.Listener.Accept()
	if err != nil {
		return c, err
	}
	ln.once.Do(func() {
		raw, e := ln.notify.SyscallConn()
		if e != nil {
			ln.observed <- e
			return
		}
		b := make([]byte, 100)
		n := 0
		var readErr error
		e = raw.Control(func(fd uintptr) { n, _, readErr = syscall.Recvfrom(int(fd), b, syscall.MSG_DONTWAIT) })
		if e == nil {
			e = readErr
		}
		if e == nil && string(b[:n]) != "READY=1" {
			e = errors.New("accepted before readiness datagram")
		}
		ln.observed <- e
	})
	return c, nil
}

// R-42CV-SI0Q
func TestRunSendsReadyBeforeActualAccept(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	ln := &runNotifyListener{Listener: h.ln, notify: h.notify, observed: make(chan error, 1)}
	h.p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
	pending, err := net.DialTimeout("tcp", h.ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pending.Close() }()
	go func() { h.done <- cli.Run(h.ctx, h.p) }()
	select {
	case err := <-ln.observed:
		if err != nil {
			t.Fatalf("actual Accept preceded READY: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connection was not accepted")
	}
	_ = pending.Close()
	r, _ := h.call(identity.Caller{UserID: "reader"}, "passes", map[string]any{})
	if r.IsError() {
		t.Fatal("passes refused")
	}
	h.stop()
}

func runAssertSilent(t *testing.T, h *runHarness) {
	t.Helper()
	if h.out.String() != "" || h.err.String() != "" {
		t.Fatalf("streams stdout=%q stderr=%q", h.out.String(), h.err.String())
	}
}

// R-YKXK-J639 R-XQ2V-N2EA
func TestAllToolsAndPagesLeaveUnrelatedSiblingsAlone(t *testing.T) {
	rows := make([]map[string]any, 3)
	for i := range rows {
		rows[i] = runRow()
		rows[i]["source_id"] = fmt.Sprintf("%d", 501+i)
		rows[i]["name"] = fmt.Sprintf("Independent carrier %d", i)
		rows[i]["names"] = []string{fmt.Sprintf("Independent carrier %d", i)}
		facts := rows[i]["facts"].(map[string]any)
		facts["phone"] = fmt.Sprintf("612555%04d", i)
		facts["email"] = fmt.Sprintf("owner@independent%d.example", i)
	}
	fake := &runFake{rows: rows}
	entries := map[string]string{"scrapes": runSocket(t, fake)}
	for _, name := range []string{"events", "telemetry"} {
		entries[name] = runSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	}
	var unrelated atomic.Int64
	for _, name := range []string{"auth", "artifacts", "campaigns", "connections", "cron", "gdrive", "prompts"} {
		entries[name] = runSocket(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { unrelated.Add(1); w.WriteHeader(500) }))
	}
	h := newRunHarness(t, t.TempDir(), runServices(t, entries))
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("<html><title>fixture</title><p>fixture page</p></html>"))}, nil
	})}
	websiteTimers(t, h, nil)
	h.start()
	caller := identity.Caller{UserID: "owner", RequestID: "all-tools"}
	call := func(name string, args any) map[string]json.RawMessage {
		t.Helper()
		r, v := h.call(caller, name, args)
		if r.IsError() {
			t.Fatalf("%s refused: %v", name, r)
		}
		return v
	}
	p := call("ingest", map[string]any{})
	passID := runText(p, "id")
	if runText(h.waitPass(caller, passID), "status") != store.StatusDone {
		t.Fatal("ingest failed")
	}
	call("prospect_lists", map[string]any{})
	list := call("candidates", map[string]any{})
	var active []map[string]json.RawMessage
	if err := json.Unmarshal(list["candidates"], &active); err != nil || len(active) < 3 {
		t.Fatal("three candidates missing", err)
	}
	id, other := runText(active[0], "id"), runText(active[1], "id")
	call("passes", map[string]any{})
	call("pass", map[string]any{"pass": passID})
	judge := call("judge", map[string]any{})
	if p := h.waitPass(caller, runText(judge, "id")); runText(p, "status") != store.StatusDone {
		t.Fatalf("judge failed: %v", p)
	}
	enrich := call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck})
	if p := h.waitPass(caller, runText(enrich, "id")); runText(p, "status") != store.StatusDone {
		t.Fatalf("enrich failed: %v", p)
	}
	call("candidate", map[string]any{"candidate": id})
	var email string
	_ = json.Unmarshal(active[0]["email"], &email)
	domain := strings.SplitN(email, "@", 2)
	if len(domain) != 2 {
		t.Fatal("candidate email missing", active[0])
	}
	call("add_page", map[string]any{"candidate": id, "url": "https://" + domain[1] + "/"})
	ruler := identity.Caller{UserID: "chosen-ruler", Email: "chosen-ruler@example.test", RequestID: "ruler-request"}
	result, decision := h.call(ruler, "decide", map[string]any{"kind": store.KindNotSame, "candidate": id, "other": other, "why": "independent records"})
	var ruling map[string]json.RawMessage
	if err := json.Unmarshal(decision["ruling"], &ruling); err != nil || result.IsError() || runText(ruling, "user") != ruler.UserID || runText(ruling, "email") != ruler.Email {
		t.Fatal("decide ruling identity mismatch", err)
	}
	call("exclude", map[string]any{"candidate": id, "why": "owner excludes"})
	call("include", map[string]any{"candidate": id, "why": "owner includes"})
	co := call("create_prospect_list", map[string]any{"name": "first prospect list"})
	prospectListID := runText(co, "id")
	call("prospect_list", map[string]any{"prospect_list": prospectListID})
	call("edit_prospect_list", map[string]any{"prospect_list": prospectListID, "name": "renamed prospect list"})
	call("reject", map[string]any{"prospect_list": prospectListID, "candidate": id, "why": "owner rejects"})
	call("unreject", map[string]any{"prospect_list": prospectListID, "candidate": id, "why": "owner restores"})
	h.campaignDelivery("campaign.member.added", "campaign_fixture", id, "", "", h.p.Now(), "campaign-tool-chain")
	for _, path := range []string{"/", "/tools", "/about", "/prospect-lists/" + prospectListID, "/candidates/" + id, "/passes/" + passID} {
		status, _ := h.request("GET", path, "all-pages", nil)
		if status != 200 {
			t.Fatalf("%s status %d", path, status)
		}
	}
	call("delete_prospect_list", map[string]any{"prospect_list": prospectListID})
	h.deliver("evt_4444444444444444", "all-delivery", "done", 2)
	ps := call("passes", map[string]any{})
	var records []map[string]json.RawMessage
	_ = json.Unmarshal(ps["passes"], &records)
	found := false
	for _, p := range records {
		if runText(p, "trigger") == "run_fixture" {
			found = true
			if runText(h.waitPass(caller, runText(p, "id")), "status") != store.StatusDone {
				t.Fatal("delivery failed")
			}
		}
	}
	if !found {
		t.Fatal("delivery pass missing")
	}
	if code := h.stop(); code != cli.ExitSuccess {
		t.Fatal("run failed", code)
	}
	_, persisted := runStore(t, h.p.Dir, h.p.Now)
	rulings, err := persisted.Rulings(context.Background(), id)
	foundRuling := false
	for _, r := range rulings {
		if r.Kind == store.KindNotSame && r.UserID == ruler.UserID && r.UserEmail == ruler.Email {
			foundRuling = true
		}
	}
	if err != nil || !foundRuling {
		t.Fatal("persisted decide identity mismatch", err)
	}
	if unrelated.Load() != 0 {
		t.Fatalf("unrelated requests %d", unrelated.Load())
	}
}

// R-6NGO-W6UC
func TestRunUnlistedLegacyProspectList(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	makeMCP := h.p.MCP
	var writers []*telemetry.Writer
	h.p.MCP = func(w *telemetry.Writer) *mcp.Server {
		writers = append(writers, w)
		return makeMCP(w)
	}

	d, st := runStore(t, h.p.Dir, h.p.Now)
	pl, err := st.CreateProspectList(t.Context(), store.ProspectList{Name: "legacy fixture", Size: 10, CreatedUserID: "legacy-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()

	caller := identity.Caller{UserID: "legacy-owner"}
	list, err := h.client.ListTools(t.Context(), caller)
	if err != nil || len(list) != len(tools.Catalog()) {
		t.Fatal(list, err)
	}
	for i, entry := range tools.Catalog() {
		if list[i].Name != entry.Name {
			t.Fatal(list)
		}
	}
	normal, err := h.client.CallTool(t.Context(), caller, "prospect_list", json.RawMessage(`{"prospect_list":"`+pl.ID+`","peek":true}`))
	if err != nil || normal.IsError() {
		t.Fatal(normal, err)
	}
	legacy, err := h.client.CallTool(t.Context(), caller, "cohort", json.RawMessage(`{"cohort":"coh_`+strings.TrimPrefix(pl.ID, "pl_")+`"}`))
	if err != nil || legacy.IsError() || !reflect.DeepEqual(normal, legacy) {
		t.Fatal(normal, legacy, err)
	}
	h.stop()
	if len(writers) != 2 || writers[0] == nil || writers[0] != writers[1] {
		t.Fatalf("MCP writer construction: %v", writers)
	}
}

package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	prompts "github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/cli"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/settings"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
)

type serveBuffer struct {
	mu     sync.Mutex
	b      bytes.Buffer
	writes [][]byte
}

func (b *serveBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes = append(b.writes, append([]byte(nil), p...))
	return b.b.Write(p)
}
func (b *serveBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func (b *serveBuffer) Calls() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]byte(nil), b.writes...)
}
func serveWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(6 * time.Second):
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}
func serveRandom() io.Reader {
	b := make([]byte, 65536)
	for i := range b {
		b[i] = byte(i / 8)
	}
	return bytes.NewReader(b)
}

var serveTime = time.Date(2031, 4, 5, 6, 7, 8, 123456789, time.UTC)

// R-FTKQ-UFFJ R-FUSN-8768 R-FW0J-LYWX R-FX8F-ZQNM R-FYGC-DIEB R-FZO8-RA50 R-G3BX-WLD3 R-8TWS-7UB5 R-HPV4-9KYF
func TestServeRefusalsTouchNothing(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		inherit bool
		want    string
		code    int
	}{
		{"setting", map[string]string{"DRAIN_SECONDS": "abc"}, false, "", cli.ExitUsage},
		{"toolsetting", map[string]string{"RUN_MAX_TOOL_CALLS": "0"}, false, "", cli.ExitUsage},
		{"absent", nil, false, "prompts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"wrongpid", map[string]string{"LISTEN_PID": "09", "LISTEN_FDS": "1"}, false, "prompts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"zero", map[string]string{"LISTEN_PID": "9", "LISTEN_FDS": "0"}, false, "prompts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"signed", map[string]string{"LISTEN_PID": "9", "LISTEN_FDS": "+1"}, false, "prompts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"several", map[string]string{"LISTEN_PID": "9", "LISTEN_FDS": "002"}, false, "prompts: 002 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"inherit", map[string]string{"LISTEN_PID": "9", "LISTEN_FDS": "01"}, true, "prompts: listener probe\n", cli.ExitServerFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cg := t.TempDir()
			if e := os.WriteFile(filepath.Join(cg, "cgroup.procs"), []byte("9"), 0600); e != nil {
				t.Fatal(e)
			}
			before := readTree(t, cg)
			var stdout bytes.Buffer
			stderr := &serveBuffer{}
			lookups := []string{}
			unset := []string{}
			inherits := 0
			p := cli.Process{Pid: 9, Dir: root, Cgroup: cg, Version: "refusal-version", Stdout: &stdout, Stderr: stderr, Now: func() time.Time { return serveTime }, Rand: tripReader{t}, Sink: tripSink{t}, LookupEnv: func(k string) (string, bool) { lookups = append(lookups, k); v, ok := tc.env[k]; return v, ok }, Unsetenv: func(k string) error { unset = append(unset, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) {
				inherits++
				if !tc.inherit || fd != 3 {
					t.Fatal("unexpected inherit")
				}
				return nil, errors.New("listener probe")
			}, Banner: func(page.User) page.Banner { t.Fatal("unexpected banner"); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { t.Fatal("unexpected MCP"); return nil }}
			want := tc.want
			if want == "" {
				_, e := settings.Read(func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
				want = "prompts: " + e.Error() + "\n"
			}
			if code := cli.Run(context.Background(), p); code != tc.code {
				t.Fatalf("exit %d", code)
			}
			if stderr.String() != want || stdout.Len() != 0 || len(stderr.Calls()) != 1 {
				t.Fatalf("output %q, calls %d", stderr.String(), len(stderr.Calls()))
			}
			if !reflect.DeepEqual(before, readTree(t, cg)) || len(readTree(t, root)) != 1 {
				t.Fatal("refusal changed state")
			}
			if tc.inherit {
				if inherits != 1 || !reflect.DeepEqual(unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
					t.Fatal("socket operations", inherits, unset)
				}
			} else if inherits != 0 || len(unset) != 0 {
				t.Fatal("unexpected socket operations")
			}
			for _, k := range lookups {
				if k == "PATH" || k == "NOTIFY_SOCKET" || k == services.Variable || strings.HasSuffix(k, "API_KEY") {
					t.Fatal("early lookup", k)
				}
			}
			if tc.name != "setting" && tc.name != "toolsetting" {
				if len(lookups) < 14 || lookups[12] != "LISTEN_PID" || lookups[13] != "LISTEN_FDS" {
					t.Fatal("settings were not first", lookups)
				}
			}
		})
	}
}

type serveFixture struct {
	p        cli.Process
	env      map[string]string
	ln       net.Listener
	notify   *net.UnixConn
	out, err *serveBuffer
	capture  *telemetry.Capture
	cancel   context.CancelCauseFunc
	result   chan int
	client   *mcp.Client
	http     *http.Client
	writer   *telemetry.Writer
	unset    []string
	lookups  []string
	mcps     int
}

func newServeFixture(t *testing.T) *serveFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	short, e := os.MkdirTemp("", "prompts-serve-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	ln, e := net.Listen("unix", filepath.Join(short, "app.sock"))
	if e != nil {
		t.Fatal(e)
	}
	n, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify.sock"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = n.Close(); _ = ln.Close() })
	f := &serveFixture{ln: ln, notify: n, env: map[string]string{"LISTEN_PID": "73", "LISTEN_FDS": "1", "NOTIFY_SOCKET": n.LocalAddr().String(), "PATH": "/chosen/path"}, out: &serveBuffer{}, err: &serveBuffer{}, capture: &telemetry.Capture{}, result: make(chan int, 1)}
	root := t.TempDir()
	cg := t.TempDir()
	if e = os.WriteFile(filepath.Join(cg, "cgroup.procs"), []byte("73\n"), 0600); e != nil {
		t.Fatal(e)
	}
	kit := page.New(pages.ServiceName, "fixture-code")
	f.p = cli.Process{Pid: 73, Dir: root, Cgroup: cg, Version: "fixture-code", Stdout: f.out, Stderr: f.err, Now: func() time.Time { return serveTime }, Rand: serveRandom(), Sink: f.capture, ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Sleep: func(context.Context, time.Duration) {}, Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("wrong descriptor %d", fd)
		}
		return ln, nil
	}, LookupEnv: func(k string) (string, bool) { f.lookups = append(f.lookups, k); v, ok := f.env[k]; return v, ok }, Unsetenv: func(k string) error { f.unset = append(f.unset, k); return nil }, Banner: kit.Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		f.mcps++
		f.writer = w
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "fixture-code", Telemetry: w})
	}}
	transport := &http.Transport{DialContext: func(c context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(c, "unix", ln.Addr().String())
	}}
	t.Cleanup(transport.CloseIdleConnections)
	f.http = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: "http://backend/mcp", HTTPClient: f.http, Name: "fixture-client", Version: "fixture"})
	return f
}
func (f *serveFixture) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	f.cancel = cancel
	t.Cleanup(func() { cancel(errors.New("test cleanup")) })
	go func() { f.result <- cli.Run(ctx, f.p) }()
	_ = f.notify.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 128)
	n, _, e := f.notify.ReadFromUnix(buf)
	if e != nil {
		select {
		case code := <-f.result:
			t.Fatalf("start exited %d: %s", code, f.err.String())
		default:
			t.Fatal(e)
		}
	}
	if string(buf[:n]) != "READY=1" {
		t.Fatalf("notification %q", buf[:n])
	}
}
func (f *serveFixture) stop(t *testing.T) int {
	t.Helper()
	f.cancel(errors.New("fixture-stop"))
	return serveWait(t, f.result)
}
func (f *serveFixture) call(t *testing.T, name string, args any) mcp.Result {
	t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := f.client.CallTool(ctx, identity.Caller{UserID: "fixture-user", Email: "fixture@example.test", RequestID: "fixture-request"}, name, b)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func decodeServe[T any](t *testing.T, r mcp.Result) T {
	t.Helper()
	if r.IsError() {
		t.Fatalf("tool refusal %#v", r)
	}
	var v T
	if e := json.Unmarshal(serveContent(t, r), &v); e != nil {
		t.Fatal(e)
	}
	return v
}

// R-G0W5-51VP R-G241-ITME R-GHYQ-HU9F R-GJ6M-VM04 R-GKEJ-9DQT R-GD34-YRAN R-GU5Q-BJOD R-GXTF-GUWG R-GZ1B-UMN5 R-87YL-BYYN R-HJRM-CQ8Y R-8BMA-HA6Q R-8AEE-3IG1 R-8Q93-2J32 R-8RGZ-GATR R-8K5L-5ODL
func TestServeFreshCatalogLifecycle(t *testing.T) {
	f := newServeFixture(t)
	f.env["IKIGENBA_SERVICES"] = filepath.Join(t.TempDir(), "absent-services")
	f.env["RUNS_MEMORY_MAX_BYTES"] = "1000000"
	f.env["RUNS_CPU_PERCENT"] = "250"
	f.start(t)
	r := f.call(t, "list", map[string]any{})
	if string(serveContent(t, r)) != "{\"prompts\":[]}" {
		t.Fatalf("fresh list %s", serveContent(t, r))
	}
	entries, e := os.ReadDir(filepath.Join(f.p.Dir, "state", "runs"))
	if e != nil || len(entries) != 0 {
		t.Fatalf("runs entries %v %v", entries, e)
	}
	for path, want := range map[string]string{"main/cgroup.procs": "73", "cgroup.subtree_control": "+cpu +memory +pids", "runs/memory.max": "1000000", "runs/cpu.max": "250000 100000", "runs/cgroup.subtree_control": "+memory +pids"} {
		root, e := os.OpenRoot(f.p.Cgroup)
		if e != nil {
			t.Fatal(e)
		}
		b, e := root.ReadFile(path)
		_ = root.Close()
		if e != nil || string(b) != want {
			t.Fatalf("cgroup %s %q %v", path, b, e)
		}
	}
	var actual, expected bytes.Buffer
	if e = db.Status(context.Background(), db.Config{Path: filepath.Join(f.p.Dir, "state", "prompts.db"), Migrations: prompts.Migrations()}, &actual); e != nil {
		t.Fatal(e)
	}
	referencePath := filepath.Join(t.TempDir(), "catalog.db")
	d, e := db.Open(context.Background(), db.Config{Path: referencePath, Migrations: prompts.Migrations(), Now: f.p.Now})
	if e != nil {
		t.Fatal(e)
	}
	_ = d.Close()
	if e = db.Status(context.Background(), db.Config{Path: referencePath, Migrations: prompts.Migrations()}, &expected); e != nil {
		t.Fatal(e)
	}
	if actual.String() != expected.String() {
		t.Fatalf("migrations %q != %q", actual.String(), expected.String())
	}
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	if f.out.String() != "" || f.err.String() != "" {
		t.Fatalf("unexpected streams %q %q", f.out.String(), f.err.String())
	}
	if f.mcps != 1 || !reflect.DeepEqual(f.unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatal("composition", f.mcps, f.unset)
	}
	es := f.capture.Events()
	if len(es) != 5 || es[0].Name != "service.started" || es[len(es)-1].Name != "service.stopping" {
		t.Fatalf("events %#v", es)
	}
	if es[0].RequestID != "" || es[0].User != "" || !reflect.DeepEqual(es[0].Attrs, telemetry.Attrs{"version": f.p.Version}) {
		t.Fatal("start", es[0])
	}
	if !reflect.DeepEqual(es[len(es)-1].Attrs, telemetry.Attrs{"reason": "fixture-stop"}) {
		t.Fatal("stop", es[len(es)-1])
	}
	for _, event := range es {
		if event.Service != pages.ServiceName || !event.Time.Equal(serveTime.Truncate(time.Microsecond)) {
			t.Fatal("event identity", event)
		}
	}
}

// R-G6ZN-1WL6 R-GBV8-KZJY R-8TWS-7UB5 R-HPV4-9KYF
func TestServeStorageRefusals(t *testing.T) {
	for _, kind := range []string{"state", "database", "runs"} {
		t.Run(kind, func(t *testing.T) {
			f := newServeFixture(t)
			before := readTree(t, f.p.Cgroup)
			path := filepath.Join(f.p.Dir, "state")
			if kind != "state" {
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
				if kind == "database" {
					path = filepath.Join(path, "prompts.db")
				} else {
					path = filepath.Join(path, "runs")
				}
			}
			if e := os.WriteFile(path, []byte("storage-fixture"), 0600); e != nil {
				t.Fatal(e)
			}
			code := cli.Run(context.Background(), f.p)
			if code != cli.ExitServerFailed {
				t.Fatal(code)
			}
			root, e := os.OpenRoot(filepath.Dir(path))
			if e != nil {
				t.Fatal(e)
			}
			b, e := root.ReadFile(filepath.Base(path))
			_ = root.Close()
			if e != nil || string(b) != "storage-fixture" {
				t.Fatal("fixture changed", e)
			}
			prefix := "prompts: cannot open database state/prompts.db: "
			if kind == "runs" {
				prefix = "prompts: cannot create directory state/runs: "
			}
			if !strings.HasPrefix(f.err.String(), prefix) || len(f.err.Calls()) != 1 || f.out.String() != "" {
				t.Fatal("diagnostic", f.err.String())
			}
			if !reflect.DeepEqual(before, readTree(t, f.p.Cgroup)) || len(f.capture.Events()) != 0 || f.mcps != 0 {
				t.Fatal("late startup work")
			}
		})
	}
}

// R-8HPS-E4W7 R-8SOV-U2KG R-8IXO-RWMW
func TestServeUnavailableCgroup(t *testing.T) {
	for _, kind := range []string{"absent", "shared", "write"} {
		t.Run(kind, func(t *testing.T) {
			f := newServeFixture(t)
			switch kind {
			case "absent":
				f.p.Cgroup = ""
			case "shared":
				if e := os.WriteFile(filepath.Join(f.p.Cgroup, "cgroup.procs"), []byte("73 74"), 0600); e != nil {
					t.Fatal(e)
				}
			case "write":
				if e := os.Mkdir(filepath.Join(f.p.Cgroup, "cgroup.subtree_control"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			before := map[string]string{}
			if f.p.Cgroup != "" {
				before = readTree(t, f.p.Cgroup)
			}
			f.start(t)
			f.call(t, "list", map[string]any{})
			model := agentkit.Catalog()[0].Model
			f.call(t, "create", map[string]any{"name": "probe", "model": model, "prompt": "supplied prompt"})
			r := f.call(t, "run", map[string]any{"name": "probe"})
			if !r.IsError() || serveContentCount(t, r) != 1 {
				t.Fatalf("refusal %#v", r)
			}
			reason := strings.TrimSuffix(strings.TrimPrefix(f.err.String(), "prompts: runs are unavailable: "), "\n")
			if reason == "" || serveText(t, r) != fmt.Sprintf(runs.NoRuns, reason) {
				t.Fatalf("reason %q result %#v", reason, r)
			}
			if kind == "absent" && reason != "no control group was found" {
				t.Fatal(reason)
			}
			list := f.call(t, "runs", map[string]any{"name": "probe"})
			if string(serveContent(t, list)) != "{\"runs\":[]}" {
				t.Fatal(string(serveContent(t, list)))
			}
			after := map[string]string{}
			if f.p.Cgroup != "" {
				after = readTree(t, f.p.Cgroup)
			}
			if kind == "shared" && !reflect.DeepEqual(before, after) {
				t.Fatal("shared group changed")
			}
			if kind == "write" {
				for path := range after {
					if strings.HasPrefix(path, "runs/") {
						t.Fatal("continued after failed write")
					}
				}
			}
			if code := f.stop(t); code != cli.ExitSuccess {
				t.Fatal(code)
			}
		})
	}
}

// R-GWLJ-335R R-8XKH-D5J8
func TestServeReadinessFailureAndCancelledStart(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelled), func(t *testing.T) {
			f := newServeFixture(t)
			f.env["NOTIFY_SOCKET"] = filepath.Join(t.TempDir(), "missing.sock")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			code := cli.Run(ctx, f.p)
			if cancelled {
				if code != cli.ExitSuccess || f.err.String() != "" {
					t.Fatalf("cancelled start %d %q", code, f.err.String())
				}
			} else {
				if code != cli.ExitServerFailed || !strings.Contains(f.err.String(), f.env["NOTIFY_SOCKET"]) || len(f.err.Calls()) != 1 {
					t.Fatalf("readiness failure %d %q", code, f.err.String())
				}
			}
			if len(f.capture.Events()) != 0 {
				t.Fatal("recorded readiness on failed start")
			}
		})
	}
}

func serveCatalog(t *testing.T, dir string) (*db.DB, *store.Store) {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "prompts.db"), Migrations: prompts.Migrations(), Now: func() time.Time { return serveTime }})
	if e != nil {
		t.Fatal(e)
	}
	return d, store.New(d, store.Config{Now: func() time.Time { return serveTime }, Rand: serveRandom()})
}
func seedServeRuns(t *testing.T, f *serveFixture) (store.Prompt, []store.Run) {
	t.Helper()
	d, st := serveCatalog(t, f.p.Dir)
	defer func() { _ = d.Close() }()
	p, e := st.Create(context.Background(), store.Draft{Owner: "fixture-user", OwnerEmail: "fixture@example.test", Name: "recovered", Model: agentkit.Catalog()[0].Model, Prompt: "seeded prompt"})
	if e != nil {
		t.Fatal(e)
	}
	records := []store.Run{}
	for i, status := range []string{store.StatusExited, store.StatusRunning, store.StatusQueued} {
		id := fmt.Sprintf("prr_%016x", i+1)
		r := store.Run{ID: id, Prompt: p.ID, Model: p.Model, User: p.Owner, RequestID: fmt.Sprintf("seed-request-%d", i), Trigger: store.TriggerManual, Status: status, Started: serveTime.Add(time.Duration(i-3) * 24 * time.Hour)}
		if status == store.StatusExited {
			r.Status = store.StatusRunning
		}
		r, e = st.AddRun(context.Background(), r)
		if e != nil {
			t.Fatal(e)
		}
		if status == store.StatusExited {
			r, e = st.FinishRun(context.Background(), r.ID, store.Ending{Status: store.StatusExited, Finished: r.Started.Add(time.Second)})
			if e != nil {
				t.Fatal(e)
			}
		}
		records = append(records, r)
		folder := filepath.Join(f.p.Dir, "state", "runs", p.ID, r.ID)
		if e = os.MkdirAll(filepath.Join(folder, "work"), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(folder, "input.json"), []byte("{\"seed\":true}"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return p, records
}

// R-GLMF-N5HI R-GMUC-0X87 R-GO28-EOYW R-GKEJ-9DQT R-HJRM-CQ8Y
func TestServeRecoveryAndPruning(t *testing.T) {
	f := newServeFixture(t)
	p, records := seedServeRuns(t, f)
	f.env["RUN_KEEP_DAYS"] = "1"
	f.env["RUN_KEEP_COUNT"] = "2"
	before := readTree(t, filepath.Join(f.p.Dir, "state", "runs", p.ID))
	f.start(t)
	for _, r := range records[1:] {
		result := decodeServe[map[string]any](t, f.call(t, "result", map[string]any{"run": r.ID}))
		want := store.StatusKilled
		if r.Status == store.StatusQueued {
			want = store.StatusFailed
			if result["reason"] != store.ReasonQueueAbandoned {
				t.Fatal(result)
			}
		}
		if result["status"] != want || result["exit_code"] != nil {
			t.Fatal(result)
		}
	}
	if r := f.call(t, "result", map[string]any{"run": records[0].ID}); !r.IsError() {
		t.Fatal("old record not pruned")
	}
	after := readTree(t, filepath.Join(f.p.Dir, "state", "runs", p.ID))
	for path, value := range before {
		if path == records[0].ID || strings.HasPrefix(path, records[0].ID+"/") {
			if _, ok := after[path]; ok {
				t.Fatal("old folder not pruned")
			}
		} else if after[path] != value {
			t.Fatal("recovered folder changed", path)
		}
	}
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	if len(es) < 4 || es[0].Name != "run.finished" || es[1].Name != "run.finished" || es[2].Name != "service.started" {
		t.Fatalf("recovery order %#v", es)
	}
	if es[0].Attrs["status"] != store.StatusKilled || es[1].Attrs["status"] != store.StatusFailed {
		t.Fatal(es[:2])
	}
}

// R-8WCK-ZDSJ R-8XKH-D5J8 R-900A-4P0M
func TestServeCancelledDuringRecovery(t *testing.T) {
	f := newServeFixture(t)
	_, rs := seedServeRuns(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.p.Now = func() time.Time {
		if _, e := os.Stat(filepath.Join(f.p.Dir, "state", "runs")); e == nil {
			cancel()
		}
		return serveTime
	}
	if code := cli.Run(ctx, f.p); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	if f.mcps != 0 || f.err.String() != "" {
		t.Fatalf("late start %d %q", f.mcps, f.err.String())
	}
	d, st := serveCatalog(t, f.p.Dir)
	defer func() { _ = d.Close() }()
	for _, seed := range rs[1:] {
		r, e := st.RunByID(context.Background(), seed.ID)
		if e != nil {
			t.Fatal(e)
		}
		if r.Status == store.StatusRunning || r.Status == store.StatusQueued {
			t.Fatal("not settled", r)
		}
	}
	es := f.capture.Events()
	if len(es) != 2 || es[0].Name != "run.finished" || es[1].Name != "run.finished" {
		t.Fatal(es)
	}
}

// R-8V4O-LM1U
func TestServeRefusesFailedRecovery(t *testing.T) {
	f := newServeFixture(t)
	_, _ = seedServeRuns(t, f)
	dropped := false
	f.p.Now = func() time.Time {
		if !dropped {
			if _, e := os.Stat(filepath.Join(f.p.Dir, "state", "runs")); e == nil {
				dropped = true
				d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(f.p.Dir, "state", "prompts.db"), Migrations: prompts.Migrations(), Now: func() time.Time { return serveTime }})
				if e != nil {
					t.Fatal(e)
				}
				e = d.Write(context.Background(), func(tx *sql.Tx) error { _, e := tx.Exec("DROP TABLE runs"); return e })
				_ = d.Close()
				if e != nil {
					t.Fatal(e)
				}
			}
		}
		return serveTime
	}
	code := cli.Run(context.Background(), f.p)
	if code != cli.ExitServerFailed || f.err.String() != "prompts: "+store.Unreachable+"\n" || f.mcps != 0 || len(f.capture.Events()) != 0 {
		t.Fatalf("failed recovery %d %q events %#v", code, f.err.String(), f.capture.Events())
	}
}

// R-G87J-FOBV R-8GHW-0D5I
func TestServeNewerCatalogWarnsAndServes(t *testing.T) {
	f := newServeFixture(t)
	p, rs := seedServeRuns(t, f)
	f.env["RUN_KEEP_DAYS"] = "1"
	f.env["RUN_KEEP_COUNT"] = "2"
	d, st := serveCatalog(t, f.p.Dir)
	_, e := st.Create(context.Background(), store.Draft{Owner: "another-user", Name: "other-owner", Model: p.Model, Prompt: "supplied"})
	if e != nil {
		t.Fatal(e)
	}
	e = d.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES ('9999','2000-01-01T00:00:00Z')")
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	_ = d.Close()
	var expected, before, after bytes.Buffer
	cfg := db.Config{Path: filepath.Join(f.p.Dir, "state", "prompts.db"), Migrations: prompts.Migrations(), Now: f.p.Now, Service: pages.ServiceName, Stderr: &expected}
	d, e = db.Open(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	_ = d.Close()
	if e = db.Status(context.Background(), cfg, &before); e != nil {
		t.Fatal(e)
	}
	f.start(t)
	listed := decodeServe[tools.PromptList](t, f.call(t, "list", map[string]any{}))
	if len(listed.Prompts) != 1 || listed.Prompts[0].Name != p.Name || listed.Prompts[0].ID != p.ID {
		t.Fatalf("newer catalog prompt list %#v", listed)
	}
	for _, seed := range rs[1:] {
		result := decodeServe[tools.RunResult](t, f.call(t, "result", map[string]any{"run": seed.ID}))
		want := store.StatusKilled
		if seed.Status == store.StatusQueued {
			want = store.StatusFailed
			if result.Reason == nil || *result.Reason != store.ReasonQueueAbandoned {
				t.Fatal(result)
			}
		}
		if result.Status != want || result.ExitCode != nil {
			t.Fatal("unsettled newer run", result)
		}
	}
	if result := f.call(t, "result", map[string]any{"run": rs[0].ID}); !result.IsError() {
		t.Fatal("expired newer run retained")
	}
	if _, e = os.Stat(filepath.Join(f.p.Dir, "state", "runs", p.ID, rs[0].ID)); !os.IsNotExist(e) {
		t.Fatal("expired newer folder retained", e)
	}
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	if expected.Len() == 0 || f.err.String() != expected.String() || len(f.err.Calls()) != 1 {
		t.Fatalf("warning %q != %q", f.err.String(), expected.String())
	}
	if e = db.Status(context.Background(), cfg, &after); e != nil {
		t.Fatal(e)
	}
	if before.String() != after.String() {
		t.Fatalf("migration status changed: before %q after %q", before.String(), after.String())
	}
	es := f.capture.Events()
	if len(es) < 4 || es[0].Name != "run.finished" || es[1].Name != "run.finished" || es[2].Name != "service.started" || es[3].Name != "request.started" {
		t.Fatalf("newer catalog pre-request event order %#v", es)
	}
	for i, seed := range rs[1:] {
		if es[i].Attrs["prompt_run"] != seed.ID {
			t.Fatal("pre-request recovery event", es[i])
		}
	}
}

type failServeSink struct{}

func (failServeSink) Deliver(context.Context, telemetry.Event) error { return telemetry.ErrRejected }

// R-HSAX-14FT R-HTIT-EW6I
func TestServeUndeliveredEvents(t *testing.T) {
	f := newServeFixture(t)
	f.p.Sink = failServeSink{}
	f.start(t)
	f.call(t, "list", map[string]any{})
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	calls := f.err.Calls()
	if len(calls) != 5 {
		t.Fatalf("event diagnostics %d %s", len(calls), f.err.String())
	}
	names := []string{}
	for _, line := range calls {
		prefix := "prompts: undelivered event: "
		if !bytes.HasPrefix(line, []byte(prefix)) || line[len(line)-1] != '\n' {
			t.Fatalf("line %q", line)
		}
		var envelope struct {
			Name      string `json:"event"`
			RequestID string `json:"request_id"`
			User      string `json:"user"`
		}
		if e := json.Unmarshal(line[len(prefix):], &envelope); e != nil {
			t.Fatal(e)
		}
		event := telemetry.Event{Name: envelope.Name, RequestID: envelope.RequestID, User: envelope.User}
		names = append(names, event.Name)
		if event.Name != "service.started" && event.Name != "service.stopping" && (event.RequestID != "fixture-request" || event.User != "fixture-user") {
			t.Fatal(event)
		}
	}
	if names[0] != "service.started" || names[len(names)-1] != "service.stopping" {
		t.Fatal(names)
	}
}

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == agent.Command {
		os.Exit(cli.Run(context.Background(), cli.Process{Args: []string{agent.Command}, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, LookupEnv: os.LookupEnv, Now: func() time.Time { return serveTime }}))
	}
	os.Exit(m.Run())
}
func serveChatModel(t *testing.T) string {
	t.Helper()
	for _, entry := range agentkit.Catalog() {
		off, e := agent.Offering(entry.Model)
		if e == nil && off.Host == agentkit.HostAnthropic {
			return entry.Model
		}
	}
	t.Fatal("catalog has no default Anthropic offering")
	return ""
}
func serveAnswer(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []string{
		`{"type":"message_start","message":{"id":"fixture-message","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"drain answer"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	} {
		_, _ = io.WriteString(w, "data: "+event+"\n\n")
	}
}

type observedServeSink struct {
	capture telemetry.Capture
	events  chan telemetry.Event
}

func (s *observedServeSink) Deliver(c context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(c, e)
	s.events <- e
	return nil
}
func awaitServeEvent(t *testing.T, s *observedServeSink, name, id string) telemetry.Event {
	t.Helper()
	for {
		e := serveWait(t, s.events)
		if e.Name == name && (id == "" || e.Attrs["prompt_run"] == id) {
			return e
		}
	}
}
func serveProvider(t *testing.T, f *serveFixture) (<-chan struct{}, func()) {
	t.Helper()
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var once sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			serveAnswer(w)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(provider.Close)
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	f.p.BaseURL = provider.URL
	model := serveChatModel(t)
	off, e := agent.Offering(model)
	if e != nil {
		t.Fatal(e)
	}
	f.env[agent.KeyVariable(off.Host)] = "provider-fixture-key"
	return entered, unlock
}
func createServePrompt(t *testing.T, f *serveFixture) tools.Prompt {
	t.Helper()
	return decodeServe[tools.Prompt](t, f.call(t, "create", map[string]any{"name": "drain-probe", "model": serveChatModel(t), "prompt": "supplied prompt"}))
}
func waitListenerClosed(t *testing.T, f *serveFixture) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		c, e := net.DialTimeout("unix", f.ln.Addr().String(), 50*time.Millisecond)
		if e != nil {
			return
		}
		_ = c.Close()
	}
	t.Fatal("listener did not close")
}

// R-8LDH-JG4A R-8MLD-X7UZ R-8IXO-RWMW R-8NTA-AZLO R-8AEE-3IG1 R-8BMA-HA6Q
func TestServeDrainLetsChildFinishAndAbandonsQueue(t *testing.T) {
	f := newServeFixture(t)
	f.env["DRAIN_SECONDS"] = "30"
	f.env["RUN_MAX_ACTIVE"] = "1"
	f.env["RUN_MAX_QUEUED"] = "1"
	f.env["RUN_MEMORY_MAX_BYTES"] = "1000000"
	f.env["RUN_PIDS_MAX"] = "7"
	entered, release := serveProvider(t, f)
	sink := &observedServeSink{events: make(chan telemetry.Event, 128)}
	f.p.Sink = sink
	f.start(t)
	p := createServePrompt(t, f)
	first := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
	serveWait(t, entered)
	second := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
	if first.Status != store.StatusRunning || second.Status != store.StatusQueued {
		t.Fatalf("run admission %#v %#v", first, second)
	}
	third := f.call(t, "run", map[string]any{"name": p.Name})
	if !third.IsError() || serveText(t, third) != fmt.Sprintf(runs.QueueFull, int64(1)) {
		t.Fatal(third)
	}
	for file, want := range map[string]string{"memory.max": "1000000", "pids.max": "7"} {
		root, e := os.OpenRoot(filepath.Join(f.p.Cgroup, "runs", first.ID))
		if e != nil {
			t.Fatal(e)
		}
		b, e := root.ReadFile(file)
		_ = root.Close()
		if e != nil || strings.TrimSpace(string(b)) != want {
			t.Fatalf("run cgroup %s %q %v", file, b, e)
		}
	}
	f.cancel(errors.New("fixture-stop"))
	event := awaitServeEvent(t, sink, "run.finished", second.ID)
	if event.Attrs["status"] != store.StatusFailed || event.Attrs["reason"] != store.ReasonQueueAbandoned {
		t.Fatal(event)
	}
	for _, file := range []string{runs.StdoutFile, runs.StderrFile, runs.TranscriptFile} {
		if _, e := os.Stat(filepath.Join(f.p.Dir, "state", "runs", p.ID, second.ID, file)); !os.IsNotExist(e) {
			t.Fatalf("queued file %s: %v", file, e)
		}
	}
	select {
	case <-f.result:
		t.Fatal("returned while child still runs")
	default:
	}
	release()
	if code := serveWait(t, f.result); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	d, st := serveCatalog(t, f.p.Dir)
	defer func() { _ = d.Close() }()
	r, e := st.RunByID(context.Background(), first.ID)
	if e != nil || r.Status != store.StatusExited || r.ExitCode != 0 {
		t.Fatalf("drained child %#v %v", r, e)
	}
	active, e := st.Running(context.Background())
	if e != nil || len(active) != 0 {
		t.Fatal(active, e)
	}
	if f.err.String() != "" {
		t.Fatal(f.err.String())
	}
	es := sink.capture.Events()
	if es[len(es)-1].Name != "service.stopping" {
		t.Fatal(es)
	}
}

// R-86QO-Y77Y R-8NTA-AZLO R-8E23-8TO4 R-8BMA-HA6Q
func TestServeDrainKillsChildAtDeadline(t *testing.T) {
	f := newServeFixture(t)
	f.env["DRAIN_SECONDS"] = "1"
	entered, _ := serveProvider(t, f)
	f.start(t)
	p := createServePrompt(t, f)
	run := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
	serveWait(t, entered)
	pidBytes, e := os.ReadFile(filepath.Join(f.p.Cgroup, "runs", run.ID, "cgroup.procs"))
	if e != nil {
		t.Fatal(e)
	}
	pid, e := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	elapsed := time.Since(start)
	if elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatalf("drain elapsed %v", elapsed)
	}
	d, st := serveCatalog(t, f.p.Dir)
	defer func() { _ = d.Close() }()
	r, e := st.RunByID(context.Background(), run.ID)
	if e != nil || r.Status != store.StatusKilled {
		t.Fatal(r, e)
	}
	if _, e = os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); !os.IsNotExist(e) {
		t.Fatalf("child %d remains: %v", pid, e)
	}
	for _, event := range f.capture.Events() {
		if event.Name == "service.stopping" || (event.Name == "run.finished" && event.Attrs["prompt_run"] == run.ID) {
			t.Fatal("late event reached sink", event)
		}
	}
	diag := f.err.String()
	killIndex := strings.Index(diag, "\"status\":\"killed\"")
	stopIndex := strings.Index(diag, "\"event\":\"service.stopping\"")
	if killIndex < 0 || stopIndex < killIndex {
		t.Fatalf("event order %s", diag)
	}
}

func serveContent(t *testing.T, r mcp.Result) json.RawMessage {
	t.Helper()
	b, e := r.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]json.RawMessage
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v["structuredContent"]
}
func serveTexts(t *testing.T, r mcp.Result) []struct{ Text string } {
	t.Helper()
	b, e := r.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Content []struct{ Text string } }
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v.Content
}
func serveText(t *testing.T, r mcp.Result) string {
	t.Helper()
	v := serveTexts(t, r)
	if len(v) != 1 {
		t.Fatalf("content %#v", v)
	}
	return v[0].Text
}
func serveContentCount(t *testing.T, r mcp.Result) int { return len(serveTexts(t, r)) }

func partialServeRequest(t *testing.T, f *serveFixture, path, body, requestID string) net.Conn {
	t.Helper()
	c, e := net.Dial("unix", f.ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Close() })
	if e = c.SetDeadline(time.Now().Add(5 * time.Second)); e != nil {
		t.Fatal(e)
	}
	_, e = fmt.Fprintf(c, "POST %s HTTP/1.1\r\nHost: backend\r\nX-User-Id: fixture-user\r\nX-Request-Id: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", path, requestID, len(body))
	if e != nil {
		t.Fatal(e)
	}
	return c
}

// R-896H-PQPC R-8P16-ORCD R-H6CQ-593B
func TestServeCutsOffUnfinishedRequest(t *testing.T) {
	f := newServeFixture(t)
	f.env["DRAIN_SECONDS"] = "1"
	sink := &observedServeSink{events: make(chan telemetry.Event, 64)}
	f.p.Sink = sink
	f.start(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	c := partialServeRequest(t, f, "/mcp", body, "partial-request")
	awaitServeEvent(t, sink, "request.started", "")
	ended := make(chan string, 1)
	go func() {
		_, e := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: "POST"})
		if e == nil {
			ended <- "response unexpectedly completed"
			return
		}
		ended <- f.err.String()
	}()
	started := time.Now()
	if code := f.stop(t); code != cli.ExitServerFailed {
		t.Fatal(code)
	}
	if d := time.Since(started); d < time.Second || d >= 2*time.Second {
		t.Fatal("drain duration", d)
	}
	diag := serveWait(t, ended)
	if !strings.Contains(diag, "\"event\":\"service.stopping\"") {
		t.Fatalf("connection closed before stop diagnostic: %s", diag)
	}
	calls := f.err.Calls()
	if len(calls) == 0 || string(calls[len(calls)-1]) != "prompts: stopped with 1 request unfinished\n" {
		t.Fatalf("last line %s", f.err.String())
	}
	for _, event := range sink.capture.Events() {
		if event.Name == "service.stopping" {
			t.Fatal("late stop delivered")
		}
	}
}

type serveAcceptFailure struct {
	net.Listener
	failure error
	once    sync.Once
}

func (l *serveAcceptFailure) Accept() (net.Conn, error) {
	var e error
	l.once.Do(func() { e = l.failure })
	if e != nil {
		return nil, e
	}
	return l.Listener.Accept()
}

type serveTemporaryError struct{}

func (serveTemporaryError) Error() string   { return "temporary accept fixture" }
func (serveTemporaryError) Timeout() bool   { return false }
func (serveTemporaryError) Temporary() bool { return true }

// R-HG3X-7F0V
func TestServeAcceptFailureDiagnostic(t *testing.T) {
	f := newServeFixture(t)
	failure := errors.New("accept fixture")
	f.p.Inherit = func(uintptr) (net.Listener, error) { return &serveAcceptFailure{Listener: f.ln, failure: failure}, nil }
	if code := cli.Run(context.Background(), f.p); code != cli.ExitServerFailed {
		t.Fatal(code)
	}
	calls := f.err.Calls()
	if len(calls) == 0 || string(calls[len(calls)-1]) != "prompts: accept fixture\n" || f.out.String() != "" {
		t.Fatalf("failure stream %q", f.err.String())
	}
}

// R-HUQP-SNX7
func TestServeRetryAndBannerPanicSilence(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	f := newServeFixture(t)
	f.p.Inherit = func(uintptr) (net.Listener, error) {
		return &serveAcceptFailure{Listener: f.ln, failure: serveTemporaryError{}}, nil
	}
	called := make(chan struct{})
	f.p.Banner = func(page.User) page.Banner { close(called); panic("banner fixture") }
	f.start(t)
	request, e := http.NewRequest("GET", "http://backend/", nil)
	if e != nil {
		t.Fatal(e)
	}
	request.Header.Set("X-User-Id", "fixture-user")
	r, e := f.http.Do(request)
	if e == nil {
		_ = r.Body.Close()
	}
	serveWait(t, called)
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	if output.Len() != 0 {
		t.Fatal("default logger", output.String())
	}
}

type serveBlockingRandom struct {
	source           io.Reader
	mu               sync.Mutex
	block            bool
	entered, release chan struct{}
	once             sync.Once
}

func (r *serveBlockingRandom) Read(b []byte) (int, error) {
	r.mu.Lock()
	block := r.block
	r.mu.Unlock()
	if block {
		r.once.Do(func() { close(r.entered) })
		<-r.release
	}
	return r.source.Read(b)
}
func (r *serveBlockingRandom) hold() { r.mu.Lock(); r.block = true; r.mu.Unlock() }

// R-832Z-SVZV
func TestServeCutsOffRunBeforeItsRecord(t *testing.T) {
	f := newServeFixture(t)
	f.env["DRAIN_SECONDS"] = "1"
	random := &serveBlockingRandom{source: serveRandom(), entered: make(chan struct{}), release: make(chan struct{})}
	f.p.Rand = random
	f.start(t)
	p := createServePrompt(t, f)
	random.hold()
	callDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, e := f.client.CallTool(ctx, identity.Caller{UserID: "fixture-user", RequestID: "blocked-run-request"}, "run", json.RawMessage(`{"name":"`+p.Name+`"}`))
		callDone <- e
	}()
	serveWait(t, random.entered)
	f.cancel(errors.New("fixture-stop"))
	<-time.After(1100 * time.Millisecond)
	released := time.Now()
	close(random.release)
	select {
	case code := <-f.result:
		if code != cli.ExitServerFailed {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return within one second after the pending random read was released")
	}
	if time.Since(released) >= time.Second {
		t.Fatal("Run exceeded return bound after pending random read")
	}
	calls := f.err.Calls()
	if len(calls) == 0 || string(calls[len(calls)-1]) != "prompts: stopped with 1 request unfinished\n" {
		t.Fatalf("blocked run did not count as one cut-off request with final diagnostic: %s", f.err.String())
	}
	_ = serveWait(t, callDone)
	d, st := serveCatalog(t, f.p.Dir)
	defer func() { _ = d.Close() }()
	rs, e := st.Runs(context.Background(), p.ID)
	if e != nil || len(rs) != 0 {
		t.Fatal("recorded blocked run", rs, e)
	}
	entries, e := os.ReadDir(filepath.Join(f.p.Dir, "state", "runs", p.ID))
	if e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("blocked run folders", entries)
	}
	for _, event := range f.capture.Events() {
		if event.RequestID == "blocked-run-request" && (event.Name == "tool.called" || strings.HasPrefix(event.Name, "run.") || strings.HasPrefix(event.Name, "prompt.")) {
			t.Fatal("blocked run event", event)
		}
	}
	if strings.Contains(f.err.String(), "\"request_id\":\"blocked-run-request\"") && (strings.Contains(f.err.String(), "\"event\":\"tool.called\"") || strings.Contains(f.err.String(), "\"event\":\"run.started\"")) {
		t.Fatal(f.err.String())
	}
}

// R-H3WX-DPLX R-H54T-RHCM
func TestServeRefusesLateRunAndDelivery(t *testing.T) {
	for _, path := range []string{"/mcp", "/events"} {
		t.Run(path, func(t *testing.T) {
			f := newServeFixture(t)
			entered, release := serveProvider(t, f)
			sink := &observedServeSink{events: make(chan telemetry.Event, 128)}
			f.p.Sink = sink
			f.start(t)
			p := createServePrompt(t, f)
			f.call(t, "subscribe", map[string]any{"name": p.Name, "event": "repo.pushed"})
			first := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
			serveWait(t, entered)
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run","arguments":{"name":"` + p.Name + `"}}}`
			if path == "/events" {
				event := events.Event{ID: "evt_0011223344556677", Time: serveTime, Service: "repos", Name: "repo.pushed", RequestID: "event-source", User: "fixture-user", Attrs: events.Attrs{}, Seq: 1, Received: serveTime}
				b, e := json.Marshal(event)
				if e != nil {
					t.Fatal(e)
				}
				var envelope map[string]json.RawMessage
				if e = json.Unmarshal(b, &envelope); e != nil {
					t.Fatal(e)
				}
				envelope["attempt"] = json.RawMessage("1")
				b, e = json.Marshal(envelope)
				if e != nil {
					t.Fatal(e)
				}
				body = string(b)
			}
			c := partialServeRequest(t, f, path, body, "late-request")
			for {
				event := serveWait(t, sink.events)
				if event.Name == "request.started" && event.RequestID == "late-request" {
					break
				}
			}
			f.cancel(errors.New("fixture-stop"))
			waitListenerClosed(t, f)
			if _, e := io.WriteString(c, body); e != nil {
				t.Fatal(e)
			}
			response, e := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: "POST"})
			if e != nil {
				t.Fatal(e)
			}
			answer, e := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			var decoded map[string]json.RawMessage
			if e = json.Unmarshal(answer, &decoded); e != nil {
				t.Fatal(e)
			}
			if path == "/events" {
				var outcome, reason string
				_ = json.Unmarshal(decoded["outcome"], &outcome)
				_ = json.Unmarshal(decoded["error"], &reason)
				if response.StatusCode != 500 || outcome != "error" || reason != runs.Stopping {
					t.Fatal(response.StatusCode, string(answer))
				}
			} else {
				var result mcp.Result
				if e = json.Unmarshal(decoded["result"], &result); e != nil {
					t.Fatal(e)
				}
				if !result.IsError() || serveText(t, result) != runs.Stopping {
					t.Fatal(string(answer))
				}
			}
			release()
			if code := serveWait(t, f.result); code != cli.ExitSuccess {
				t.Fatal(code)
			}
			d, st := serveCatalog(t, f.p.Dir)
			defer func() { _ = d.Close() }()
			rs, e := st.Runs(context.Background(), p.ID)
			if e != nil || len(rs) != 1 || rs[0].ID != first.ID {
				t.Fatal("late admission", rs, e)
			}
		})
	}
}

type serveReadProbe struct {
	active  atomic.Int32
	overlap atomic.Bool
	next    uint64
}

func (r *serveReadProbe) Read(b []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	defer r.active.Add(-1)
	for i := range b {
		b[i] = byte((r.next >> uint((i%8)*8)) & 0xff)
	}
	r.next++
	return len(b), nil
}

type serveWriteProbe struct {
	active   atomic.Int32
	overlap  atomic.Bool
	returned atomic.Bool
	late     atomic.Bool
	buffer   serveBuffer
}

func (w *serveWriteProbe) Write(b []byte) (int, error) {
	if w.active.Add(1) != 1 {
		w.overlap.Store(true)
	}
	defer w.active.Add(-1)
	if w.returned.Load() {
		w.late.Store(true)
	}
	return w.buffer.Write(b)
}

// R-HTIT-EW6I
func TestServeConcurrentSeamsAreSerialized(t *testing.T) {
	f := newServeFixture(t)
	reader := &serveReadProbe{}
	writer := &serveWriteProbe{}
	f.p.Rand = reader
	f.p.Stderr = writer
	f.p.Sink = failServeSink{}
	f.start(t)
	var requests sync.WaitGroup
	for i := 0; i < 20; i++ {
		requests.Go(func() {
			b, _ := json.Marshal(map[string]any{"name": fmt.Sprintf("concurrent-%d", i), "model": agentkit.Catalog()[0].Model, "prompt": "supplied"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r, e := f.client.CallTool(ctx, identity.Caller{UserID: "fixture-user"}, "create", b)
			if e != nil || r.IsError() {
				t.Errorf("concurrent create %v %#v", e, r)
			}
		})
	}
	requests.Wait()
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	writer.returned.Store(true)
	if reader.overlap.Load() || writer.overlap.Load() || writer.late.Load() {
		t.Fatal("process seam calls overlapped or wrote after return")
	}
	if len(writer.buffer.Calls()) == 0 {
		t.Fatal("diagnostic probe unused")
	}
}

// R-8Q93-2J32
func TestServeHandledFailuresKeepStreamsQuiet(t *testing.T) {
	f := newServeFixture(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "provider fixture refusal")
	}))
	t.Cleanup(provider.Close)
	f.p.BaseURL = provider.URL
	sink := &observedServeSink{events: make(chan telemetry.Event, 128)}
	f.p.Sink = sink
	f.start(t)
	for _, tc := range []struct{ method, path, user string }{{"GET", "/nope", "fixture-user"}, {"POST", "/mcp", ""}} {
		r, e := http.NewRequest(tc.method, "http://backend"+tc.path, nil)
		if e != nil {
			t.Fatal(e)
		}
		if tc.user != "" {
			r.Header.Set("X-User-Id", tc.user)
		}
		response, e := f.http.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	if r := f.call(t, "show", map[string]any{"name": "absent"}); !r.IsError() {
		t.Fatal("missing prompt accepted")
	}
	p := createServePrompt(t, f)
	run := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
	event := awaitServeEvent(t, sink, "run.finished", run.ID)
	if event.Attrs["status"] != store.StatusExited || event.Attrs["exit_code"] == int64(0) {
		t.Fatal(event)
	}
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	if f.err.String() != "" || f.out.String() != "" {
		t.Fatalf("handled failures wrote %q %q", f.out.String(), f.err.String())
	}
}

// R-8RGZ-GATR
func TestServeNoRequestsRecordsOnlyServiceEvents(t *testing.T) {
	f := newServeFixture(t)
	d, st := serveCatalog(t, f.p.Dir)
	p, e := st.Create(context.Background(), store.Draft{Owner: "fixture-user", Name: "absent-folder", Model: agentkit.Catalog()[0].Model, Prompt: "supplied"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := st.AddRun(context.Background(), store.Run{ID: "prr_0011223344556677", Prompt: p.ID, Model: p.Model, User: p.Owner, Trigger: store.TriggerManual, Status: store.StatusRunning, Started: serveTime.Add(-time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	_, e = st.FinishRun(context.Background(), r.ID, store.Ending{Status: store.StatusExited, Finished: serveTime})
	if e != nil {
		t.Fatal(e)
	}
	_ = d.Close()
	f.start(t)
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	es := f.capture.Events()
	if len(es) != 2 || es[0].Name != "service.started" || es[1].Name != "service.stopping" {
		t.Fatal(es)
	}
}

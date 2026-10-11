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
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	artifacts "github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/cli"
	"github.com/ikigenba/ikigenba/artifacts/internal/pages"
	"github.com/ikigenba/ikigenba/artifacts/internal/server"
	"github.com/ikigenba/ikigenba/artifacts/internal/settings"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/sweeper"
)

// runHarness carries every process input; readiness is the systemd datagram.
type runHarness struct {
	t               *testing.T
	p               cli.Process
	now             atomic.Int64
	env             map[string]string
	keys            []string
	unset           []string
	inherited       int
	ln              net.Listener
	notify          *net.UnixConn
	stdout, stderr  writeLog
	trail           telemetry.Capture
	bus             events.Capture
	waits           chan timerWait
	cancel          context.CancelCauseFunc
	result          chan int
	client          *http.Client
	stopped         bool
	beforeReadyRead chan struct{}
}
type timerWait struct {
	duration time.Duration
	fire     chan time.Time
}
type writeLog struct {
	sync.Mutex
	data  bytes.Buffer
	calls int
}

func (w *writeLog) Write(p []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	w.calls++
	return w.data.Write(p)
}
func (w *writeLog) String() string { w.Lock(); defer w.Unlock(); return w.data.String() }
func (w *writeLog) Writes() int    { w.Lock(); defer w.Unlock(); return w.calls }

type repeatingByte byte

func (b repeatingByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
func shortDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("", "art-")
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
func newRun(t *testing.T, dir string) *runHarness {
	t.Helper()
	t.Setenv(services.Variable, "")
	socketDir := shortDir(t)
	ln, e := net.Listen("unix", filepath.Join(socketDir, "app"))
	if e != nil {
		t.Fatal(e)
	}
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(socketDir, "ready"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	r := &runHarness{t: t, env: map[string]string{"LISTEN_PID": "321", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String()}, ln: ln, notify: notify, waits: make(chan timerWait, 32), result: make(chan int, 1)}
	r.now.Store(time.Date(2031, 4, 5, 6, 7, 8, 0, time.UTC).Unix())
	r.p = cli.Process{Dir: dir, Pid: 321, Stdout: &r.stdout, Stderr: &r.stderr, Version: "test-run-display", LookupEnv: func(k string) (string, bool) { r.keys = append(r.keys, k); v, ok := r.env[k]; return v, ok }, Unsetenv: func(k string) error { r.unset = append(r.unset, k); delete(r.env, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) {
		r.inherited++
		if fd != 3 {
			return nil, fmt.Errorf("unexpected descriptor %d", fd)
		}
		return ln, nil
	}, Now: func() time.Time { return time.Unix(r.now.Load(), 0).UTC() }, Sleep: func(ctx context.Context, _ time.Duration) { <-ctx.Done() }, After: func(d time.Duration) <-chan time.Time {
		w := timerWait{d, make(chan time.Time, 1)}
		r.waits <- w
		return w.fire
	}, Rand: repeatingByte(0x5a), Sink: &r.trail, EventSink: &r.bus, Banner: func(u page.User) page.Banner { return page.Banner{Service: pages.ServiceName, Email: u.Email} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "test-run-display", Telemetry: w})
	}}
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", ln.Addr().String())
	}}
	r.client = &http.Client{Transport: tr, Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() {
		if r.cancel != nil && !r.stopped {
			r.stop()
		}
		tr.CloseIdleConnections()
		_ = ln.Close()
		_ = notify.Close()
	})
	return r
}
func (r *runHarness) start() {
	r.t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	r.cancel = cancel
	go func() { r.result <- cli.Run(ctx, r.p) }()
	if r.beforeReadyRead != nil {
		select {
		case <-r.beforeReadyRead:
		case <-time.After(5 * time.Second):
			r.t.Fatal("Accept not reached")
		}
	}
	_ = r.notify.SetReadDeadline(time.Now().Add(5 * time.Second))
	b := make([]byte, 128)
	n, _, e := r.notify.ReadFromUnix(b)
	if e != nil {
		r.t.Fatalf("readiness: %v stderr=%s", e, r.stderr.String())
	}
	if string(b[:n]) != "READY=1" {
		r.t.Fatalf("readiness %q", b[:n])
	}
}
func (r *runHarness) stop() int {
	r.t.Helper()
	r.cancel(errors.New("test-stop"))
	select {
	case code := <-r.result:
		r.stopped = true
		return code
	case <-time.After(6 * time.Second):
		r.t.Fatal("Run did not stop")
		return -1
	}
}
func (r *runHarness) request(method, path, user string, body io.Reader, headers map[string]string) (int, []byte) {
	r.t.Helper()
	req, e := http.NewRequest(method, "http://artifacts"+path, body)
	if e != nil {
		r.t.Fatal(e)
	}
	req.Header.Set("X-User-Id", user)
	req.Header.Set("X-User-Email", user+"@example.test")
	req.Header.Set("X-Forwarded-Proto", "https")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, e := r.client.Do(req)
	if e != nil {
		r.t.Fatal(e)
	}
	defer func() { _ = res.Body.Close() }()
	data, e := io.ReadAll(res.Body)
	if e != nil {
		r.t.Fatal(e)
	}
	return res.StatusCode, data
}
func (r *runHarness) call(name string, args any) mcp.Result {
	r.t.Helper()
	raw, e := json.Marshal(args)
	if e != nil {
		r.t.Fatal(e)
	}
	c := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://artifacts/mcp", HTTPClient: r.client})
	result, e := c.CallTool(context.Background(), identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "caller-request"}, name, raw)
	if e != nil {
		r.t.Fatal(e)
	}
	return result
}
func readWait(t *testing.T, ch <-chan timerWait) timerWait {
	t.Helper()
	select {
	case w := <-ch:
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("timer not requested")
		return timerWait{}
	}
}
func expectStatus(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("status %d want %d", got, want)
	}
}
func blobs(t *testing.T, dir string) []string {
	t.Helper()
	entries, e := os.ReadDir(filepath.Join(dir, "state", "blobs"))
	if e != nil {
		t.Fatal(e)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// R-U2U0-UJPN R-YSHG-S964 R-Z8C5-R9T5 R-UBDB-IXWI R-DGR0-TF4I
// R-ZBZU-WL18 R-DSY0-N4JG
func TestRunProcessAndCapturedLifecycle(t *testing.T) {
	dir := t.TempDir()
	r := newRun(t, dir)
	r.start()
	info, e := os.Stat(filepath.Join(dir, "state", "artifacts.db"))
	if e != nil || !info.Mode().IsRegular() {
		t.Fatalf("database: %v %v", info, e)
	}
	if names := blobs(t, dir); len(names) != 0 {
		t.Fatalf("initial blobs %v", names)
	}
	for _, want := range []int{201} {
		s, b := r.request("PUT", "/files/item.bin", "owner", strings.NewReader("payload"), nil)
		expectStatus(t, s, want)
		var meta map[string]any
		if e := json.Unmarshal(b, &meta); e != nil {
			t.Fatal(e)
		}
		if meta["id"] != "art_5a5a5a5a5a5a5a5a" {
			t.Fatalf("id %v", meta)
		}
	}
	status, _ := r.request("PUT", "/files/item.bin", "other", strings.NewReader("refused body"), nil)
	expectStatus(t, status, 403)
	if code := r.stop(); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	if r.inherited != 1 || !reflect.DeepEqual(r.unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatalf("inherit=%d unset=%v", r.inherited, r.unset)
	}
	allowed := map[string]bool{"DRAIN_SECONDS": true, "ARTIFACT_MAX_BYTES": true, "IKIGENBA_SERVICES": true, "LISTEN_PID": true, "LISTEN_FDS": true, "NOTIFY_SOCKET": true}
	counts := map[string]int{}
	for _, k := range r.keys {
		if !allowed[k] {
			t.Errorf("unexpected environment key %s", k)
		}
		counts[k]++
	}
	for _, k := range []string{"DRAIN_SECONDS", "ARTIFACT_MAX_BYTES", "IKIGENBA_SERVICES"} {
		if counts[k] != 1 {
			t.Errorf("%s looked up %d times", k, counts[k])
		}
	}
	ev := r.trail.Events()
	if len(ev) < 2 || ev[0].Name != "service.started" || ev[len(ev)-1].Name != "service.stopping" {
		t.Fatalf("lifecycle %v", ev)
	}
	first, last := ev[0], ev[len(ev)-1]
	if first.User != "" || first.RequestID != "" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": r.p.Version}) {
		t.Errorf("started %#v", first)
	}
	if last.User != "" || last.RequestID != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "test-stop"}) {
		t.Errorf("stopping %#v", last)
	}
	stopping := 0
	for _, e := range ev {
		if e.Name == "service.stopping" {
			stopping++
		}
		if !e.Time.Equal(r.p.Now()) {
			t.Errorf("event clock %v", e.Time)
		}
	}
	if stopping != 1 {
		t.Errorf("stopping count %d", stopping)
	}
	bus := r.bus.Events()
	if len(bus) != 1 || bus[0].Name != "artifact.created" {
		t.Fatalf("bus %v", bus)
	}
	if r.stdout.String() != "" || r.stderr.String() != "" {
		t.Fatalf("streams stdout=%q stderr=%q", r.stdout.String(), r.stderr.String())
	}
}

// R-Z749-DI2G: empty Dir and nil Unsetenv; injected Now.
func TestRunPersistenceAndWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	r := newRun(t, "")
	r.p.Unsetenv = nil
	r.start()
	s, _ := r.request("PUT", "/files/persist.bin", "owner", strings.NewReader("persistent bytes"), nil)
	expectStatus(t, s, 201)
	if r.stop() != cli.ExitSuccess {
		t.Fatal("first stop failed")
	}
	again := newRun(t, dir)
	again.start()
	s, b := again.request("GET", "/files/persist.bin", "owner", nil, nil)
	expectStatus(t, s, 200)
	if string(b) != "persistent bytes" {
		t.Fatalf("body %q", b)
	}
	if again.stop() != cli.ExitSuccess {
		t.Fatal("second stop failed")
	}
	other := newRun(t, t.TempDir())
	other.start()
	s, _ = other.request("GET", "/files/persist.bin", "owner", nil, nil)
	expectStatus(t, s, 404)
	if other.stop() != cli.ExitSuccess {
		t.Fatal("other stop failed")
	}
}

// R-Z5WC-ZQBR R-DFJ4-FNDT
func TestRunEarlyRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"setting", map[string]string{"ARTIFACT_MAX_BYTES": "0"}, "artifacts: " + (&settings.Error{Name: "ARTIFACT_MAX_BYTES", Value: "0", Unit: "bytes"}).Error() + "\n"},
		{"no socket", map[string]string{}, "artifacts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
		{"pid mismatch", map[string]string{"LISTEN_PID": "999", "LISTEN_FDS": "1"}, "artifacts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"},
		{"many sockets", map[string]string{"LISTEN_PID": "321", "LISTEN_FDS": "2"}, "artifacts: 2 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRun(t, t.TempDir())
			r.env = tc.env
			code := cli.Run(context.Background(), r.p)
			if code != cli.ExitUsage || r.inherited != 0 || r.stdout.String() != "" || r.stderr.String() != tc.want || r.stderr.Writes() != 1 {
				t.Fatalf("code %d inherited %d stdout=%q stderr=%q writes=%d", code, r.inherited, r.stdout.String(), r.stderr.String(), r.stderr.Writes())
			}
			entries, e := os.ReadDir(r.p.Dir)
			if e != nil || len(entries) != 0 {
				t.Fatalf("entries %v err=%v", entries, e)
			}
		})
	}
}

func seedRun(t *testing.T, dir string, now time.Time, fn func(*db.DB, *store.Store)) {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "artifacts.db"), Migrations: artifacts.Migrations(), Now: func() time.Time { return now }, Service: pages.ServiceName, Stderr: io.Discard})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = d.Close() }()
	path := filepath.Join(dir, "state", "blobs")
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	fn(d, store.New(d, path, store.Config{Now: func() time.Time { return now }, Rand: &exclusiveRandom{}}))
}

// R-DHYX-76V7 R-DJ6T-KYLW R-DKEP-YQCL R-DLMM-CI3A
func TestRunStartupFailures(t *testing.T) {
	for _, kind := range []string{"state", "blobs", "schema"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			r := newRun(t, dir)
			var prefix, want string
			switch kind {
			case "state":
				if e := os.WriteFile(filepath.Join(dir, "state"), []byte("untouched"), 0600); e != nil {
					t.Fatal(e)
				}
				prefix = "artifacts: cannot open database state/artifacts.db: "
				_, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "artifacts.db"), Migrations: artifacts.Migrations(), Now: r.p.Now, Service: pages.ServiceName, Stderr: io.Discard})
				if err == nil {
					t.Fatal("invalid state unexpectedly opened")
				}
				want = prefix + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"
			case "blobs":
				if e := os.Mkdir(filepath.Join(dir, "state"), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(dir, "state", "blobs"), []byte("untouched"), 0600); e != nil {
					t.Fatal(e)
				}
				prefix = "artifacts: cannot create directory state/blobs: "
			case "schema":
				seedRun(t, dir, r.p.Now(), func(d *db.DB, _ *store.Store) {
					if e := d.Write(context.Background(), func(tx *sql.Tx) error { _, e := tx.Exec("DROP TABLE artifacts"); return e }); e != nil {
						t.Fatal(e)
					}
				})
				want = "artifacts: " + store.Unreachable + "\n"
			}
			accepts := atomic.Int64{}
			r.p.Inherit = func(fd uintptr) (net.Listener, error) {
				if fd != 3 {
					t.Errorf("descriptor %d", fd)
				}
				return &countAccept{Listener: r.ln, count: &accepts}, nil
			}
			code := cli.Run(context.Background(), r.p)
			line := r.stderr.String()
			if code != cli.ExitServerFailed || r.stdout.String() != "" || r.stderr.Writes() != 1 || strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") || !strings.HasPrefix(line, "artifacts: ") {
				t.Fatalf("refusal code=%d line=%q writes=%d", code, line, r.stderr.Writes())
			}
			if want != "" && line != want {
				t.Errorf("line %q want %q", line, want)
			}
			if prefix != "" && !strings.HasPrefix(line, prefix) {
				t.Errorf("prefix %q", line)
			}
			if accepts.Load() != 0 {
				t.Errorf("accepted %d", accepts.Load())
			}
			if len(r.trail.Events()) != 0 || len(r.bus.Events()) != 0 {
				t.Fatal("events on failed startup")
			}
			_ = r.notify.SetReadDeadline(time.Now().Add(15 * time.Millisecond))
			buf := make([]byte, 32)
			if _, _, e := r.notify.ReadFromUnix(buf); e == nil {
				t.Fatal("ready on failure")
			}
			if kind != "schema" {
				path := filepath.Join(dir, "state")
				if kind == "blobs" {
					path = filepath.Join(path, "blobs")
				}
				b, e := os.ReadFile(filepath.Clean(path))
				if e != nil || string(b) != "untouched" {
					t.Fatalf("fixture changed %q %v", b, e)
				}
			}
		})
	}
}

type countAccept struct {
	net.Listener
	count *atomic.Int64
}

func (l *countAccept) Accept() (net.Conn, error) { l.count.Add(1); return l.Listener.Accept() }

// R-4MZ1-WQ94 R-DO2F-41KO R-4O6Y-AHZT R-DRQ4-9CSR
func TestRunReconcileAndDrivenSweeps(t *testing.T) {
	dir := t.TempDir()
	r := newRun(t, dir)
	now := r.p.Now()
	var gone, expired, keep store.Artifact
	seedRun(t, dir, now.Add(-time.Hour), func(_ *db.DB, st *store.Store) {
		put := func(path string, expiry time.Time) store.Artifact {
			a, _, e := st.Put(context.Background(), store.Draft{Path: path, OwnerID: "seed-owner", OwnerEmail: "seed@example.test", Expires: expiry, Max: 99}, strings.NewReader(path))
			if e != nil {
				t.Fatal(e)
			}
			return a
		}
		gone = put("missing.bin", time.Time{})
		expired = put("expired.bin", now.Add(-time.Minute))
		keep = put("keep.bin", now.Add(time.Minute))
		entries := blobs(t, dir)
		for _, name := range entries {
			path := filepath.Join(dir, "state", "blobs", name)
			b, e := os.ReadFile(filepath.Clean(path))
			if e != nil {
				t.Fatal(e)
			}
			if string(b) == gone.Path {
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
			}
		}
		if e := os.WriteFile(filepath.Join(dir, "state", "blobs", "stray"), []byte("stray fixture"), 0600); e != nil {
			t.Fatal(e)
		}
	})
	acceptedBeforeReady := atomic.Bool{}
	r.beforeReadyRead = make(chan struct{})
	r.p.Inherit = func(_ uintptr) (net.Listener, error) {
		return &readyListener{Listener: r.ln, before: &acceptedBeforeReady, notify: r.notify, observed: r.beforeReadyRead}, nil
	}
	r.start()
	if acceptedBeforeReady.Load() {
		t.Fatal("accepted before ready")
	}
	if len(blobs(t, dir)) != 1 {
		t.Fatal("reconcile/sweep did not clean blobs")
	}
	for _, path := range []string{gone.Path, expired.Path} {
		s, _ := r.request("GET", "/files/"+path, "owner", nil, nil)
		expectStatus(t, s, 404)
	}
	s, b := r.request("GET", "/files/"+keep.Path, "owner", nil, nil)
	expectStatus(t, s, 200)
	if string(b) != keep.Path {
		t.Fatalf("survivor %q", b)
	}
	seedRun(t, dir, r.p.Now(), func(_ *db.DB, st *store.Store) {
		got, err := st.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []store.Artifact{keep}) {
			t.Fatalf("surviving records changed: got %#v want %#v", got, keep)
		}
	})
	wait := readWait(t, r.waits)
	if wait.duration != sweeper.Interval {
		t.Fatalf("sweep wait %v", wait.duration)
	}
	r.now.Store(now.Add(time.Minute).Unix())
	// Advancing the clock alone must not run the hourly sweep.
	s, b = r.request("GET", "/files/"+keep.Path, "owner", nil, nil)
	expectStatus(t, s, 200)
	if string(b) != keep.Path {
		t.Fatalf("expired survivor before timer %q", b)
	}
	wait.fire <- r.p.Now()
	next := readWait(t, r.waits)
	if next.duration != sweeper.Interval {
		t.Fatal("next sweep wait")
	}
	s, _ = r.request("GET", "/files/"+keep.Path, "owner", nil, nil)
	expectStatus(t, s, 404)
	if r.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	ev := r.trail.Events()
	if ev[0].Name != "service.started" {
		t.Fatal("first event")
	}
	if ev[0].RequestID != "" || ev[0].User != "" || !reflect.DeepEqual(ev[0].Attrs, telemetry.Attrs{"version": r.p.Version}) {
		t.Fatalf("start %#v", ev[0])
	}
	if len(ev) < 3 || ev[1].Name != "artifact.orphaned" || ev[2].Name != "artifact.deleted" {
		t.Fatalf("startup order %v", ev)
	}
	artifactEvents := make([]telemetry.Event, 0, 3)
	for _, event := range ev {
		if strings.HasPrefix(event.Name, "artifact.") {
			artifactEvents = append(artifactEvents, event)
		}
	}
	if len(artifactEvents) != 3 {
		t.Fatalf("artifact telemetry count %d: %#v", len(artifactEvents), artifactEvents)
	}
	for i, a := range []store.Artifact{gone, expired, keep} {
		e := artifactEvents[i]
		name, stamp := "artifact.deleted", now
		if i == 0 {
			name = "artifact.orphaned"
		}
		if i == 2 {
			stamp = now.Add(time.Minute)
		}
		attrs := telemetry.Attrs{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}
		if e.Name != name || e.Service != pages.ServiceName || e.User != a.OwnerID || e.RequestID != "" || !e.Time.Equal(stamp) || !reflect.DeepEqual(e.Attrs, attrs) {
			t.Errorf("artifact telemetry %d: %#v", i, e)
		}
	}
	bus := r.bus.Events()
	if len(bus) != 2 {
		t.Fatalf("bus count %d: %#v", len(bus), bus)
	}
	for i, a := range []store.Artifact{expired, keep} {
		e := bus[i]
		stamp := now
		if i == 1 {
			stamp = now.Add(time.Minute)
		}
		attrs := events.Attrs{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}
		if e.Name != "artifact.deleted" || e.Service != pages.ServiceName || e.User != a.OwnerID || e.RequestID != "" || e.Cause != "" || e.Depth != 0 || !e.Time.Equal(stamp) || !reflect.DeepEqual(e.Attrs, attrs) {
			t.Errorf("sweep bus %d: %#v", i, e)
		}
	}
	_ = r.notify.SetReadDeadline(time.Now().Add(15 * time.Millisecond))
	buf := make([]byte, 32)
	if _, _, e := r.notify.ReadFromUnix(buf); e == nil {
		t.Fatal("second ready datagram")
	}
}

// Once Run calls Accept, its readiness send must have already completed.
type readyListener struct {
	net.Listener
	before   *atomic.Bool
	notify   *net.UnixConn
	observed chan struct{}
	once     sync.Once
}

func (l *readyListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		raw, e := l.notify.SyscallConn()
		if e != nil {
			l.before.Store(true)
		} else {
			e = raw.Control(func(fd uintptr) {
				b := make([]byte, 32)
				n, _, err := syscall.Recvfrom(int(fd), b, syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
				if err != nil || string(b[:n]) != "READY=1" {
					l.before.Store(true)
				}
			})
			if e != nil {
				l.before.Store(true)
			}
		}
		close(l.observed)
	})
	return l.Listener.Accept()
}

type exclusiveRandom struct {
	active     atomic.Bool
	concurrent atomic.Bool
	serial     byte
}

func (r *exclusiveRandom) Read(p []byte) (int, error) {
	if !r.active.CompareAndSwap(false, true) {
		r.concurrent.Store(true)
		return 0, errors.New("concurrent random read")
	}
	defer r.active.Store(false)
	runtime.Gosched()
	r.serial++
	for i := range p {
		p[i] = r.serial
	}
	return len(p), nil
}

// R-YUX9-JSNI
func TestRunSerializesRandomAcrossRequests(t *testing.T) {
	r := newRun(t, t.TempDir())
	random := &exclusiveRandom{}
	r.p.Rand = random
	r.start()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := range 12 {
		wg.Go(func() {
			req, e := http.NewRequest("PUT", fmt.Sprintf("http://artifacts/files/item%d", i), strings.NewReader("data"))
			if e != nil {
				errs <- e
				return
			}
			req.Header.Set("X-User-Id", "owner")
			res, e := r.client.Do(req)
			if e != nil {
				errs <- e
				return
			}
			_ = res.Body.Close()
			if res.StatusCode != 201 {
				errs <- fmt.Errorf("status %d", res.StatusCode)
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if r.stop() != cli.ExitSuccess {
		t.Error("stop failed")
	}
	if random.concurrent.Load() {
		t.Fatal("Rand used concurrently")
	}
}

func serveSibling(t *testing.T, h http.Handler) (string, string) {
	t.Helper()
	path := filepath.Join(shortDir(t), "sibling")
	ln, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		select {
		case e := <-done:
			if e != nil && !errors.Is(e, http.ErrServerClosed) {
				t.Error(e)
			}
		case <-time.After(5 * time.Second):
			t.Error("sibling did not stop")
		}
	})
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	doc := map[string]any{"services": []map[string]any{{"name": "scripts", "url": "https://scripts.space.test", "description": "test sibling", "socket": path, "enabled": true, "mcp": false}}}
	b, e := json.Marshal(doc)
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(servicesPath, b, 0600); e != nil {
		t.Fatal(e)
	}
	return servicesPath, "https://scripts.space.test/output"
}
func resultText(t *testing.T, r mcp.Result) string {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if e := json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if len(v.Content) != 1 {
		t.Fatalf("result %s", b)
	}
	return v.Content[0].Text
}

// R-DU5X-0WA5
func TestRunUnfinishedRequestDrain(t *testing.T) {
	r := newRun(t, t.TempDir())
	r.env["DRAIN_SECONDS"] = "1"
	started := make(chan struct{})
	r.p.Sink = &startBarrier{capture: &r.trail, started: started}
	r.start()
	conn, e := net.Dial("unix", r.ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = conn.Close() }()
	if _, e := io.WriteString(conn, "PUT /files/blocked HTTP/1.1\r\nHost: artifacts\r\nX-User-Id: owner\r\nContent-Length: 999\r\n\r\nx"); e != nil {
		t.Fatal(e)
	}
	// A second request on the same listener does not prove the first began; the
	// first request's start event is observed through a sink barrier instead.

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked request not started")
	}
	if code := r.stop(); code != cli.ExitServerFailed {
		t.Fatalf("exit %d", code)
	}
	want := "artifacts: " + (&server.DrainError{Unfinished: 1}).Error() + "\n"
	if !strings.HasSuffix(r.stderr.String(), want) {
		t.Fatalf("stderr %q", r.stderr.String())
	}
}

type startBarrier struct {
	capture *telemetry.Capture
	started chan struct{}
	once    sync.Once
}

func (s *startBarrier) Deliver(ctx context.Context, e telemetry.Event) error {
	if e.Name == "request.started" {
		s.once.Do(func() { close(s.started) })
	}
	return s.capture.Deliver(ctx, e)
}

// R-XO1A-6QM3: nil sinks deliver over their documented services sockets.
func TestRunDefaultSocketSinks(t *testing.T) {
	var mu sync.Mutex
	var trailEvents []telemetry.Event
	var busEvents []events.Event
	path, _ := serveSibling(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() { _ = req.Body.Close() }()
		data, e := io.ReadAll(req.Body)
		if e != nil {
			t.Error(e)
			w.WriteHeader(500)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch req.URL.Path {
		case "/ingest":
			var wire struct {
				Time      time.Time       `json:"time"`
				Service   string          `json:"service"`
				Name      string          `json:"event"`
				RequestID string          `json:"request_id"`
				User      string          `json:"user"`
				Attrs     telemetry.Attrs `json:"attrs"`
			}
			if e := json.Unmarshal(data, &wire); e != nil {
				t.Error(e)
			}
			trailEvents = append(trailEvents, telemetry.Event{Time: wire.Time, Service: wire.Service, Name: wire.Name, RequestID: wire.RequestID, User: wire.User, Attrs: wire.Attrs})
		case events.EmitPath:
			var event events.Event
			if e := json.Unmarshal(data, &event); e != nil {
				t.Error(e)
			}
			busEvents = append(busEvents, event)
		default:
			t.Errorf("sink path %s", req.URL.Path)
		}
		w.WriteHeader(204)
	}))
	entries, e := services.Read(path)
	if e != nil {
		t.Fatal(e)
	}
	list := make([]map[string]any, 0, 2)
	for _, name := range []string{"telemetry", "events"} {
		list = append(list, map[string]any{"name": name, "url": "http://" + name + ".test", "socket": entries[0].Socket, "description": "test sink", "enabled": true, "mcp": false})
	}
	data, e := json.Marshal(map[string]any{"services": list})
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	r := newRun(t, t.TempDir())
	t.Setenv(services.Variable, path)
	r.env[services.Variable] = path
	r.p.Sink = nil
	r.p.EventSink = nil
	r.start()
	s, _ := r.request("PUT", "/files/socket.bin", "owner", strings.NewReader("data"), nil)
	expectStatus(t, s, 201)
	if r.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(trailEvents) < 2 || trailEvents[0].Name != "service.started" || trailEvents[len(trailEvents)-1].Name != "service.stopping" {
		t.Fatalf("socket trail %v", trailEvents)
	}
	if len(busEvents) != 1 || busEvents[0].Name != "artifact.created" {
		t.Fatalf("socket events %v", busEvents)
	}
}

// R-ZARY-ITAJ
func TestRunConstantRandomReplacement(t *testing.T) {
	r := newRun(t, t.TempDir())
	r.start()
	for _, want := range []int{201, 200} {
		status, body := r.request("PUT", "/files/repeat.bin", "owner", strings.NewReader("body"), map[string]string{"X-Artifact-Expires": r.p.Now().Add(time.Minute).Format(time.RFC3339)})
		expectStatus(t, status, want)
		var meta map[string]any
		if err := json.Unmarshal(body, &meta); err != nil {
			t.Fatal(err)
		}
		if meta["id"] != "art_5a5a5a5a5a5a5a5a" {
			t.Fatalf("id %v", meta["id"])
		}
	}
	if r.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
}

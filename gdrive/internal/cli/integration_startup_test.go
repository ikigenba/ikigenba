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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/cli"
	"github.com/ikigenba/ikigenba/gdrive/internal/settings"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
)

type startupListener struct {
	net.Listener
	mu    sync.Mutex
	calls int
	first func()
}

func (l *startupListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	l.calls++
	first := l.calls == 1
	l.mu.Unlock()
	if first && l.first != nil {
		l.first()
	}
	return l.Listener.Accept()
}
func (l *startupListener) count() int { l.mu.Lock(); defer l.mu.Unlock(); return l.calls }

type startupClock struct {
	sync.Mutex
	at time.Time
}

func (c *startupClock) now() time.Time   { c.Lock(); defer c.Unlock(); return c.at }
func (c *startupClock) set(at time.Time) { c.Lock(); c.at = at; c.Unlock() }

type startupWait struct {
	d    time.Duration
	tick chan time.Time
}
type startupFixture struct {
	p         cli.Process
	ln        *startupListener
	notify    *net.UnixConn
	out, err  writes
	capture   telemetry.Capture
	clock     startupClock
	waits     chan startupWait
	env       map[string]string
	unset     []string
	inherited []uintptr
}

func newStartupFixture(t *testing.T) *startupFixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	short, err := os.MkdirTemp("", "gd-start-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(short); e != nil {
			t.Error(e)
		}
	})
	ln, err := net.Listen("unix", filepath.Join(short, "listen"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	n, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	f := &startupFixture{ln: &startupListener{Listener: ln}, notify: n, waits: make(chan startupWait, 10), env: map[string]string{"LISTEN_PID": "713", "LISTEN_FDS": "1", "NOTIFY_SOCKET": n.LocalAddr().String()}}
	f.clock.at = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	f.p = cli.Process{Dir: t.TempDir(), Pid: 713, Version: "startup-fixture", Stdout: &f.out, Stderr: &f.err, LookupEnv: func(k string) (string, bool) { v, ok := f.env[k]; return v, ok }, Unsetenv: func(k string) error { f.unset = append(f.unset, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) { f.inherited = append(f.inherited, fd); return f.ln, nil }, Now: f.clock.now, After: func(d time.Duration) <-chan time.Time {
		w := startupWait{d: d, tick: make(chan time.Time, 1)}
		f.waits <- w
		return w.tick
	}, Sleep: func(ctx context.Context, _ time.Duration) { <-ctx.Done() }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 65536)), Sink: &f.capture, EventSink: &events.Capture{}, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: "startup", Version: "fixture", Telemetry: w})
	}, HTTP: &http.Client{Transport: forbiddenTransport{t}}}
	return f
}
func startupDatagram(n *net.UnixConn) (string, error) {
	raw, e := n.SyscallConn()
	if e != nil {
		return "", e
	}
	var b [128]byte
	var size int
	var recv error
	e = raw.Control(func(fd uintptr) { size, _, recv = syscall.Recvfrom(int(fd), b[:], syscall.MSG_DONTWAIT) })
	if e != nil {
		return "", e
	}
	if recv != nil {
		return "", recv
	}
	return string(b[:size]), nil
}
func startupOpen(t *testing.T, dir string, now func() time.Time) (*db.DB, *store.Store) {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "gdrive.db"), Migrations: gdrive.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	return d, store.New(d, store.Config{Now: now, Rand: bytes.NewReader([]byte("abcdefghijklmnopqrstuvwxzy0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"))})
}
func startupAwait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("startup operation did not complete")
		var zero T
		return zero
	}
}
func (f *startupFixture) start(t *testing.T) (context.CancelFunc, <-chan int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	finished := make(chan struct{})
	go func() { defer close(finished); done <- cli.Run(ctx, f.p) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("Run did not finish during cleanup")
		}
	})
	return cancel, done
}
func (f *startupFixture) page(t *testing.T, id string) int {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", f.ln.Addr().String())
	}}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	r, _ := http.NewRequest(http.MethodGet, "http://local/"+id, nil)
	r.Header.Set("X-User-Id", "owner")
	res, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

// R-R49O-M31A
func TestRunNilDefaultsAndWorkingDirectory(t *testing.T) {
	f := newStartupFixture(t)
	dir := f.p.Dir
	t.Chdir(dir)
	now := f.clock.now()
	f.clock.set(now.Add(-time.Hour))
	d, st := startupOpen(t, dir, f.clock.now)
	tr, err := st.Add(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Path: "destination", UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	f.clock.set(now)
	short := integrationSocketDir(t)
	trace := &telemetry.Capture{}
	bus := &events.Capture{}
	ts := integrationUnix(t, short, "telemetry", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != telemetry.IngestPath {
			t.Error(r.URL.Path)
		}
		telemetry.IngestHandler(trace).ServeHTTP(w, r)
	}))
	bs := integrationUnix(t, short, "events", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event events.Event
		if r.Method != http.MethodPost || r.URL.Path != events.EmitPath {
			t.Error("unexpected bus request", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := bus.Deliver(r.Context(), event); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	path := integrationServices(t, short, integrationEntry("telemetry", ts, ""), integrationEntry("events", bs, ""))
	t.Setenv(services.Variable, path)
	f.env[services.Variable] = path
	f.env["LISTEN_FDNAMES"] = "fixture-listener"
	f.p.Unsetenv = nil
	f.p.Dir = ""
	f.p.Sink = nil
	f.p.EventSink = nil
	ready := startupReadyAtAccept(t, f)
	cancel, done := f.start(t)
	if err := startupAwait(t, ready); err != nil {
		t.Fatal(err)
	}
	if status := f.page(t, tr.ID); status != http.StatusOK {
		t.Fatal("transfer page status", status)
	}
	startupStop(t, f, cancel, done)
	for key, want := range map[string]string{"LISTEN_PID": "713", "LISTEN_FDS": "1", "LISTEN_FDNAMES": "fixture-listener"} {
		if f.env[key] != want {
			t.Errorf("nil Unsetenv changed %s", key)
		}
	}
	d, st = startupOpen(t, dir, f.clock.now)
	defer func() { _ = d.Close() }()
	got, err := st.Get(context.Background(), "owner", tr.ID)
	if err != nil || got.Finished != now || got.Started != tr.Started {
		t.Fatalf("working-directory record and clock: %+v %v", got, err)
	}
	times := trace.Events()
	if len(times) == 0 {
		t.Fatal("nil Sink delivered no telemetry")
	}
	for _, event := range times {
		if event.Time != now {
			t.Errorf("telemetry time %v want %v", event.Time, now)
		}
	}
	emitted := bus.Events()
	if len(emitted) != 1 || emitted[0].Time != now || emitted[0].Name != "transfer.finished" {
		t.Fatalf("nil EventSink and clock: %+v", emitted)
	}
}

// R-RA64-BIJV
func TestStartupInvalidSettingsBeforeAnyProcessWork(t *testing.T) {
	f := newStartupFixture(t)
	f.env["TRANSFER_MAX_ACTIVE"] = "0"
	var looked []string
	orig := f.p.LookupEnv
	f.p.LookupEnv = func(k string) (string, bool) { looked = append(looked, k); return orig(k) }
	_, want := settings.Read(orig)
	code := cli.Run(context.Background(), f.p)
	if code != cli.ExitUsage || f.out.Len() != 0 || f.err.n != 1 || f.err.String() != "gdrive: "+want.Error()+"\n" {
		t.Fatalf("code %d out %q err %q writes %d", code, f.out.String(), f.err.String(), f.err.n)
	}
	for _, k := range looked {
		if k == "LISTEN_PID" || k == "LISTEN_FDS" || k == "IKIGENBA_SERVICES" {
			t.Fatalf("premature lookup %s", k)
		}
	}
	if len(f.inherited) != 0 || len(f.unset) != 0 {
		t.Fatal("process hooks called")
	}
	entries, e := os.ReadDir(f.p.Dir)
	if e != nil || len(entries) != 0 {
		t.Fatalf("directory changed %v %v", entries, e)
	}
}

// R-AIJ5-SCHY
func TestStartupSocketUsageRefusal(t *testing.T) {
	for _, c := range []struct{ pid, fds, want string }{{"", "", "no socket was passed in"}, {"714", "1", "no socket was passed in"}, {"713", "0", "no socket was passed in"}, {"713", "0002", "0002 sockets were passed in, expected 1"}} {
		t.Run(c.pid+"_"+c.fds, func(t *testing.T) {
			f := newStartupFixture(t)
			f.env["LISTEN_PID"] = c.pid
			f.env["LISTEN_FDS"] = c.fds
			code := cli.Run(context.Background(), f.p)
			want := "gdrive: " + c.want + "\n\nrun it under systemd, with a listening socket passed in\n"
			if code != cli.ExitUsage || f.err.n != 1 || f.err.String() != want || len(f.inherited) != 0 {
				t.Fatalf("code %d err %q inherited %v", code, f.err.String(), f.inherited)
			}
			entries, e := os.ReadDir(f.p.Dir)
			if e != nil || len(entries) != 0 {
				t.Fatalf("directory changed %v %v", entries, e)
			}
		})
	}
}

// R-B851-TJ2J R-AM6U-XNQ1
func TestStartupDatabaseOpenRefusesBeforeReadiness(t *testing.T) {
	f := newStartupFixture(t)
	if e := os.WriteFile(filepath.Join(f.p.Dir, "state"), []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	_, e := db.Open(context.Background(), db.Config{Path: filepath.Join(f.p.Dir, "state", "gdrive.db"), Migrations: gdrive.Migrations(), Now: f.clock.now})
	if e == nil {
		t.Fatal("fixture did not provoke opening failure")
	}
	code := cli.Run(context.Background(), f.p)
	want := "gdrive: cannot open database state/gdrive.db: " + strings.ReplaceAll(e.Error(), "\n", " ") + "\n"
	if code != cli.ExitServerFailed || f.out.Len() != 0 || f.err.n != 1 || f.err.String() != want || f.ln.count() != 0 || len(f.capture.Events()) != 0 || len(f.p.EventSink.(*events.Capture).Events()) != 0 {
		t.Fatalf("refusal code=%d out=%q err=%q accepts=%d events=%v", code, f.out.String(), f.err.String(), f.ln.count(), f.capture.Events())
	}
	if msg, e := startupDatagram(f.notify); e == nil || !errors.Is(e, syscall.EAGAIN) {
		t.Fatalf("unexpected notification %q %v", msg, e)
	}
}

// R-B9CY-7AT8
func TestStartupRecoveryFailureDoesNotServe(t *testing.T) {
	f := newStartupFixture(t)
	d, _ := startupOpen(t, f.p.Dir, f.clock.now)
	if e := d.Write(context.Background(), func(tx *sql.Tx) error { _, e := tx.Exec("DROP TABLE transfers"); return e }); e != nil {
		t.Fatal(e)
	}
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	code := cli.Run(context.Background(), f.p)
	es := f.capture.Events()
	if code != cli.ExitServerFailed || f.out.Len() != 0 || f.err.n != 1 || f.err.String() != "gdrive: "+store.Unreachable+"\n" || f.ln.count() != 0 || len(es) != 1 || es[0].Name != "service.started" || len(f.p.EventSink.(*events.Capture).Events()) != 0 {
		t.Fatalf("code=%d err=%q accepts=%d events=%v", code, f.err.String(), f.ln.count(), es)
	}
	if msg, e := startupDatagram(f.notify); e == nil || !errors.Is(e, syscall.EAGAIN) {
		t.Fatalf("unexpected notification %q %v", msg, e)
	}
}

func startupSeed(t *testing.T, f *startupFixture, at time.Time, states ...string) []store.Transfer {
	t.Helper()
	d, st := startupOpen(t, f.p.Dir, func() time.Time { return at })
	defer func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	}()
	var out []store.Transfer
	for i, state := range states {
		draftState := state
		if state == store.StatusDone {
			draftState = store.StatusRunning
		}
		tr, e := st.Add(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: draftState, File: fmt.Sprintf("file-%d", i), Name: fmt.Sprintf("name-%d", i), Kind: "file", Path: fmt.Sprintf("path-%d", i), ContentType: "text/plain", Format: "chosen-format", UserID: "owner", UserEmail: "owner@example.invalid", RequestID: fmt.Sprintf("origin-%d", i)})
		if e != nil {
			t.Fatal(e)
		}
		if state == store.StatusRunning {
			tr, e = st.SetBytes(context.Background(), tr.ID, 42)
		}
		if state == store.StatusDone {
			tr, e = st.Finish(context.Background(), tr.ID, store.Ending{Status: store.StatusDone, Bytes: 25, URL: "https://artifact.invalid/fixture", Link: "https://drive.invalid/fixture", ArtifactID: "artifact-fixture"})
		}
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, tr)
	}
	return out
}
func startupReadyAtAccept(t *testing.T, f *startupFixture) <-chan error {
	t.Helper()
	ready := make(chan error, 1)
	f.ln.first = func() {
		msg, e := startupDatagram(f.notify)
		if e == nil && msg != "READY=1" {
			e = fmt.Errorf("notification %q", msg)
		}
		ready <- e
	}
	return ready
}
func startupStop(t *testing.T, f *startupFixture, cancel context.CancelFunc, done <-chan int) {
	t.Helper()
	cancel()
	if code := startupAwait(t, done); code != cli.ExitSuccess {
		t.Fatalf("stop code %d stderr %q", code, f.err.String())
	}
	if f.out.Len() != 0 || f.err.Len() != 0 {
		t.Fatalf("streams %q %q", f.out.String(), f.err.String())
	}
}

// startupReadinessGuard observes the notification socket during injected clock
// reads while durable state still requires settling. The socket is peeked, so
// the first Accept must independently consume and validate the same datagram.
func startupReadinessGuard(t *testing.T, f *startupFixture, pending func(*store.Store) bool) func() {
	t.Helper()
	d, st := startupOpen(t, f.p.Dir, f.clock.now)
	t.Cleanup(func() { _ = d.Close() })
	var mu sync.Mutex
	observations := 0
	var violation error
	f.p.Now = func() time.Time {
		if pending(st) {
			raw, err := f.notify.SyscallConn()
			var b [128]byte
			var n int
			var recv error
			if err == nil {
				err = raw.Control(func(fd uintptr) { n, _, recv = syscall.Recvfrom(int(fd), b[:], syscall.MSG_DONTWAIT|syscall.MSG_PEEK) })
			}
			mu.Lock()
			observations++
			switch {
			case err != nil:
				violation = err
			case recv == nil:
				violation = fmt.Errorf("notification %q before durable startup work completed", b[:n])
			case !errors.Is(recv, syscall.EAGAIN):
				violation = recv
			}
			mu.Unlock()
		}
		return f.clock.now()
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if observations == 0 {
			t.Fatal("no clock read observed while startup work remained")
		}
		if violation != nil {
			t.Fatal(violation)
		}
	}
}

// R-AJR2-648N R-AOMN-P77F
func TestStartupRecoversBeforeReadinessAndAccept(t *testing.T) {
	f := newStartupFixture(t)
	seed := startupSeed(t, f, f.clock.now().Add(-time.Hour), store.StatusRunning, store.StatusQueued)
	checkReadiness := startupReadinessGuard(t, f, func(st *store.Store) bool {
		for _, tr := range seed {
			got, e := st.Get(context.Background(), tr.UserID, tr.ID)
			if e != nil {
				return true
			}
			if got.Status == store.StatusRunning || got.Status == store.StatusQueued {
				return true
			}
		}
		return false
	})
	accepted := make(chan error, 1)
	f.ln.first = func() {
		msg, e := startupDatagram(f.notify)
		if e == nil && msg != "READY=1" {
			e = fmt.Errorf("notification %q", msg)
		}
		if e == nil {
			d, openErr := db.Open(context.Background(), db.Config{Path: filepath.Join(f.p.Dir, "state", "gdrive.db"), Migrations: gdrive.Migrations(), Now: f.clock.now})
			if openErr != nil {
				e = openErr
			} else {
				st := store.New(d, store.Config{Now: f.clock.now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x6b}, 64))})
				for _, before := range seed {
					got, getErr := st.Get(context.Background(), before.UserID, before.ID)
					want := before
					want.Status = store.StatusFailed
					want.Reason = store.ReasonInterrupted
					want.Finished = f.clock.now()
					if getErr != nil || !reflect.DeepEqual(got, want) {
						e = fmt.Errorf("recovery before accept: got %+v want %+v error %w", got, want, getErr)
						break
					}
				}
				_ = d.Close()
			}
		}
		accepted <- e
	}
	cancel, done := f.start(t)
	if e := startupAwait(t, accepted); e != nil {
		cancel()
		_ = startupAwait(t, done)
		t.Fatal(e)
	}
	if status := f.page(t, seed[0].ID); status != http.StatusOK {
		t.Fatalf("served page status %d", status)
	}
	startupStop(t, f, cancel, done)
	checkReadiness()
	if !reflect.DeepEqual(f.inherited, []uintptr{3}) || !reflect.DeepEqual(f.unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatalf("inherit %v unset %v", f.inherited, f.unset)
	}
	d, st := startupOpen(t, f.p.Dir, f.clock.now)
	defer func() { _ = d.Close() }()
	for _, before := range seed {
		got, e := st.Get(context.Background(), before.UserID, before.ID)
		want := before
		want.Status = store.StatusFailed
		want.Reason = store.ReasonInterrupted
		want.Finished = f.clock.now()
		if e != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("recovered %+v want %+v err %v", got, want, e)
		}
	}
	if msg, e := startupDatagram(f.notify); e == nil || !errors.Is(e, syscall.EAGAIN) {
		t.Fatalf("extra notification %q %v", msg, e)
	}
}

// R-AOMN-P77F
func TestStartupNoNotificationWhenUnsetOrEmpty(t *testing.T) {
	for _, unset := range []bool{false, true} {
		t.Run(fmt.Sprint(unset), func(t *testing.T) {
			f := newStartupFixture(t)
			if unset {
				delete(f.env, "NOTIFY_SOCKET")
			} else {
				f.env["NOTIFY_SOCKET"] = ""
			}
			accepted := make(chan struct{}, 1)
			f.ln.first = func() { accepted <- struct{}{} }
			cancel, done := f.start(t)
			startupAwait(t, accepted)
			if status := f.page(t, ""); status != http.StatusOK {
				t.Fatalf("status %d", status)
			}
			startupStop(t, f, cancel, done)
			if msg, e := startupDatagram(f.notify); e == nil || !errors.Is(e, syscall.EAGAIN) {
				t.Fatalf("unexpected notification %q %v", msg, e)
			}
		})
	}
}

// R-KX74-GGX0
func TestStartupPrunesBeforeReadiness(t *testing.T) {
	f := newStartupFixture(t)
	f.env["TRANSFER_KEEP_COUNT"] = "1"
	now := f.clock.now()
	d, st := startupOpen(t, f.p.Dir, f.clock.now)
	var records []store.Transfer
	for i, age := range []int{17, 16} {
		f.clock.set(now.Add(-time.Duration(age) * 24 * time.Hour))
		tr, e := st.Add(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Path: fmt.Sprint(i), UserID: "owner"})
		if e != nil {
			t.Fatal(e)
		}
		tr, e = st.Finish(context.Background(), tr.ID, store.Ending{Status: store.StatusDone})
		if e != nil {
			t.Fatal(e)
		}
		records = append(records, tr)
	}
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	f.clock.set(now)
	checkReadiness := startupReadinessGuard(t, f, func(st *store.Store) bool {
		_, e := st.Get(context.Background(), "owner", records[0].ID)
		return !errors.Is(e, store.ErrNotFound)
	})
	ready := startupReadyAtAccept(t, f)
	cancel, done := f.start(t)
	if e := startupAwait(t, ready); e != nil {
		cancel()
		_ = startupAwait(t, done)
		t.Fatal(e)
	}
	for i, want := range []int{http.StatusNotFound, http.StatusOK} {
		if got := f.page(t, records[i].ID); got != want {
			t.Fatalf("page %d status %d want %d", i, got, want)
		}
	}
	startupStop(t, f, cancel, done)
	checkReadiness()
	d, st = startupOpen(t, f.p.Dir, f.clock.now)
	defer func() { _ = d.Close() }()
	if _, e := st.Get(context.Background(), "owner", records[0].ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatalf("old record retained %v", e)
	}
	if got, e := st.Get(context.Background(), "owner", records[1].ID); e != nil || !reflect.DeepEqual(got, records[1]) {
		t.Fatalf("retained record changed %+v %v", got, e)
	}
}

// R-ASAC-UIFI R-R0EX-9CMB R-R49O-M31A
func TestStartupSweepUsesInjectedTimeAndRearms(t *testing.T) {
	f := newStartupFixture(t)
	f.env["TRANSFER_KEEP_COUNT"] = "1"
	now := f.clock.now()
	d, st := startupOpen(t, f.p.Dir, f.clock.now)
	var records []store.Transfer
	for i, age := range []int{14, 13} {
		f.clock.set(now.Add(-time.Duration(age) * 24 * time.Hour))
		tr, e := st.Add(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Path: fmt.Sprint(i), UserID: "owner"})
		if e != nil {
			t.Fatal(e)
		}
		tr, e = st.Finish(context.Background(), tr.ID, store.Ending{Status: store.StatusDone})
		if e != nil {
			t.Fatal(e)
		}
		records = append(records, tr)
	}
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	f.clock.set(now)
	ready := startupReadyAtAccept(t, f)
	cancel, done := f.start(t)
	if e := startupAwait(t, ready); e != nil {
		cancel()
		_ = startupAwait(t, done)
		t.Fatal(e)
	}
	for _, tr := range records {
		if got := f.page(t, tr.ID); got != http.StatusOK {
			t.Fatalf("initial page status %d", got)
		}
	}
	first := startupAwait(t, f.waits)
	if first.d != cli.SweepInterval {
		t.Fatalf("first timer %v", first.d)
	}
	f.clock.set(now.Add(2 * 24 * time.Hour))
	first.tick <- f.clock.now()
	second := startupAwait(t, f.waits)
	if second.d != cli.SweepInterval {
		t.Fatalf("second timer %v", second.d)
	}
	if got := f.page(t, records[0].ID); got != http.StatusNotFound {
		t.Fatalf("swept status %d", got)
	}
	if got := f.page(t, records[1].ID); got != http.StatusOK {
		t.Fatalf("kept status %d", got)
	}
	f.clock.set(now.Add(3 * 24 * time.Hour))
	second.tick <- f.clock.now()
	third := startupAwait(t, f.waits)
	if third.d != cli.SweepInterval {
		t.Fatalf("third timer %v", third.d)
	}
	if got := f.page(t, records[1].ID); got != http.StatusOK {
		t.Fatalf("newest transfer pruned %d", got)
	}
	startupStop(t, f, cancel, done)
	d, st = startupOpen(t, f.p.Dir, f.clock.now)
	defer func() { _ = d.Close() }()
	if _, e := st.Get(context.Background(), "owner", records[0].ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatalf("sweep not durable %v", e)
	}
	if got, e := st.Get(context.Background(), "owner", records[1].ID); e != nil || !reflect.DeepEqual(got, records[1]) {
		t.Fatalf("kept transfer changed %+v %v", got, e)
	}
}

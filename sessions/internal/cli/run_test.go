package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

// R-UY62-TF4M
func TestSocketActivationUsage(t *testing.T) {
	for _, tc := range []struct{ pid, fds, want string }{{"", "", "no socket was passed in"}, {"other", "1", "no socket was passed in"}, {"0241", "1", "no socket was passed in"}, {"241", "", "no socket was passed in"}, {"241", "0", "no socket was passed in"}, {"241", "00", "no socket was passed in"}, {"241", "-1", "no socket was passed in"}, {"241", "+1", "no socket was passed in"}, {"241", "one", "no socket was passed in"}, {"241", "2", "2 sockets were passed in, expected 1"}, {"241", "002", "002 sockets were passed in, expected 1"}, {"241", "99999999999999999999999999", "99999999999999999999999999 sockets were passed in, expected 1"}} {
		t.Run(tc.pid+"/"+tc.fds, func(t *testing.T) {
			p, out, errout := commandProcess(t, nil)
			p.LookupEnv = func(k string) (string, bool) {
				switch k {
				case "DRAIN_SECONDS":
					return "", false
				case "LISTEN_PID":
					return tc.pid, tc.pid != ""
				case "LISTEN_FDS":
					return tc.fds, tc.fds != ""
				}
				panic("unexpected key")
			}
			code := cli.Run(context.Background(), p)
			assertCode(t, code)
			want := "sessions: " + tc.want + "\n\nrun it under systemd, with a listening socket passed in\n"
			if code != cli.ExitUsage || out.calls != 0 || errout.calls != 1 || errout.body.String() != want {
				t.Fatalf("code %d output %q/%q", code, out.body.String(), errout.body.String())
			}
			assertEmpty(t, p.Dir)
		})
	}
}

// R-UZDZ-76VB R-CQQ4-W9Q7 R-28GP-FP8D R-2AWI-78PR R-VIKN-TDZO R-VGGK-JZ91 R-XJOJ-AMJO R-23L3-WM9L R-V49K-Q9U3 R-29OL-TGZ2
func TestRunLifecycleAndWiring(t *testing.T) {
	r := newRunRig(t)
	var removed []string
	inherit := 0
	r.p.Unsetenv = func(k string) error { removed = append(removed, k); return errors.New("ignored removal") }
	r.p.Inherit = func(fd uintptr) (net.Listener, error) {
		inherit++
		if fd != 3 {
			t.Errorf("descriptor %d", fd)
		}
		return r.ln, nil
	}
	r.start(t)
	assertRunTree(t, r.p.Dir)
	if r.mcpCalls != 1 || inherit != 1 || !reflect.DeepEqual(removed, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatalf("constructors %d inherit %d removed %v", r.mcpCalls, inherit, removed)
	}
	v := r.ok(t, tools.ToolListRules, tools.NoArgs{})
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var wire struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if e = json.Unmarshal(b, &wire); e != nil {
		t.Fatal(e)
	}
	var info map[string]string
	if e = json.Unmarshal(wire.Meta["io.modelcontextprotocol/serverInfo"], &info); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(info, map[string]string{"name": pages.ServiceName, "version": r.p.Version}) {
		t.Fatalf("server info %+v", info)
	}
	r.writer.Emit(context.Background(), "test.observed", telemetry.Attrs{"value": "from supplied writer"})
	pushRun(t, r, "held", "2026-10-27T09:00:00Z", 18204)
	assertRunTree(t, r.p.Dir)
	if code, _, _ := r.request(t, "/", true); code != 200 {
		t.Fatalf("landing %d", code)
	}
	r.request(t, "/", false)
	r.request(t, "/mcp", false)
	if !r.call(t, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 0}).IsError() {
		t.Fatal("expected refusal")
	}
	if code := r.stop(t); code != cli.ExitSuccess {
		t.Fatalf("stop %d", code)
	}
	assertRunTree(t, r.p.Dir)
	events := r.capture.Events()
	if len(events) < 5 {
		t.Fatalf("events %v", events)
	}
	first, last := events[0], events[len(events)-1]
	if first.Name != "service.started" || first.RequestID != "" || first.User != "" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": r.p.Version}) {
		t.Fatalf("first %+v", first)
	}
	if last.Name != "service.stopping" || last.RequestID != "" || last.User != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "test stop"}) {
		t.Fatalf("last %+v", last)
	}
	stops, observed := 0, false
	for _, event := range events {
		if !event.Time.Equal(runInstant) {
			t.Errorf("clock %s", event.Time)
		}
		if event.Name == "service.stopping" {
			stops++
		}
		if event.Name == "test.observed" {
			observed = true
		}
	}
	if stops != 1 || !observed {
		t.Fatalf("stops %d observed %v", stops, observed)
	}
	r.writer.Emit(context.Background(), "test.after", nil)
	if len(r.capture.Events()) != len(events) || r.out.String() != "" || r.errout.String() != "" {
		t.Fatalf("post-return events/output %q/%q", r.out.String(), r.errout.String())
	}
	assertNoNotification(t, r.notify)
}

// R-ZTLH-PP1J R-2DCA-YS75
func TestRunPageReadsStoreAndCurrentClock(t *testing.T) {
	r := newRunRig(t)
	var moved atomic.Bool
	r.p.Now = func() time.Time {
		if moved.Load() {
			return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
		}
		return runInstant
	}
	r.start(t)
	pushRun(t, r, "old", "2026-10-13T09:00:00Z", 0)
	_, before, _ := r.request(t, "/", true)
	if !strings.Contains(before, "2026-10-26") || strings.Contains(before, "2026-11-02") {
		t.Fatalf("before clock move %s", before)
	}
	pushRun(t, r, "current", "2026-10-27T09:00:00Z", 18204)
	_, body, _ := r.request(t, "/", true)
	if !strings.Contains(body, "18,204") {
		t.Fatalf("store did not reach page %s", body)
	}
	moved.Store(true)
	_, after, _ := r.request(t, "/", true)
	if !strings.Contains(after, "2026-11-02") {
		t.Fatalf("after clock move %s", after)
	}
	if code := r.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
}

// R-4KXD-SKNO
func TestRunPersistenceAndIsolation(t *testing.T) {
	r := newRunRig(t)
	dir := r.p.Dir
	r.start(t)
	pushRun(t, r, "persistent", "2026-10-27T09:00:00Z", 4)
	if r.stop(t) != cli.ExitSuccess {
		t.Fatal("first run failed")
	}
	for _, tc := range []struct {
		dir   string
		count int
	}{{dir, 1}, {t.TempDir(), 0}} {
		r := newRunRig(t)
		r.p.Dir = tc.dir
		r.start(t)
		got := runDecode[tools.SessionList](t, r.ok(t, tools.ToolListSessions, tools.ListSessionsArgs{}))
		if len(got.Sessions) != tc.count {
			t.Fatalf("sessions %+v", got)
		}
		if tc.count != 0 && (got.Sessions[0].ID != "persistent" || got.Sessions[0].Tool != "codex") {
			t.Fatalf("persisted %+v", got)
		}
		if r.stop(t) != cli.ExitSuccess {
			t.Fatal("later run failed")
		}
	}
}

// R-260W-O5QZ
func TestRunRequestIDsUseOnlyRand(t *testing.T) {
	ids := make([]string, 3)
	for i := range ids {
		r := newRunRig(t)
		r.p.Now = func() time.Time {
			if i == 1 {
				return time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC)
			}
			return time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
		}
		if i == 2 {
			r.p.Rand = repeatByte(0)
		}
		r.start(t)
		r.request(t, "/about", true)
		r.stop(t)
		for _, event := range r.capture.Events() {
			if event.Name == "request.started" {
				ids[i] = event.RequestID
				break
			}
		}
	}
	if ids[0] == "" || ids[0] != ids[1] || ids[0] == ids[2] {
		t.Fatalf("request IDs %v", ids)
	}
}

type watchedReader struct {
	active  atomic.Int32
	overlap atomic.Bool
	data    []byte
	pos     int
}

func (r *watchedReader) Read(p []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	defer r.active.Add(-1)
	for i := range p {
		p[i] = r.data[r.pos%len(r.data)]
		r.pos++
	}
	return len(p), nil
}

// R-24T0-AE0A
func TestRunConcurrentRandomness(t *testing.T) {
	r := newRunRig(t)
	rng := &watchedReader{data: []byte{1, 2, 3, 4}}
	r.p.Rand = rng
	r.start(t)
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() { r.request(t, "/about", true); r.ok(t, tools.ToolListRules, tools.NoArgs{}) })
	}
	wg.Wait()
	r.stop(t)
	if rng.overlap.Load() {
		t.Fatal("concurrent Rand.Read")
	}
}

// R-V0LV-KYM0
func TestRunDatabaseOpenFailure(t *testing.T) {
	for _, newline := range []bool{false, true} {
		r := newRunRig(t)
		observer := &acceptObserver{Listener: r.ln}
		r.p.Inherit = func(uintptr) (net.Listener, error) { return observer, nil }
		queued, e := net.Dial("tcp", r.ln.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = queued.Close() })
		if newline {
			r.p.Dir = filepath.Join(r.p.Dir, "line\nbreak")
			if e := os.Mkdir(r.p.Dir, 0700); e != nil {
				t.Fatal(e)
			}
		}
		if e := os.WriteFile(filepath.Join(r.p.Dir, "state"), []byte("file"), 0600); e != nil {
			t.Fatal(e)
		}
		cfg := db.Config{Path: filepath.Join(r.p.Dir, "state", "sessions.db"), Migrations: sessions.Migrations(), Now: r.p.Now, Service: pages.ServiceName, Stderr: io.Discard}
		_, expected := db.Open(context.Background(), cfg)
		if expected == nil {
			t.Fatal("fixture did not fail")
		}
		code := cli.Run(context.Background(), r.p)
		assertCode(t, code)
		want := "sessions: cannot open database state/sessions.db: " + strings.ReplaceAll(expected.Error(), "\n", " ") + "\n"
		if code != cli.ExitServerFailed || r.errout.String() != want || r.errout.calls != 1 || r.out.String() != "" || len(r.capture.Events()) != 0 || r.mcpCalls != 0 {
			t.Fatalf("code %d output %q/%q", code, r.out.String(), r.errout.String())
		}
		if n := observer.calls.Load(); n != 0 {
			t.Fatalf("database failure accepted %d connections", n)
		}
		assertNoNotification(t, r.notify)
	}
}

type acceptObserver struct {
	net.Listener
	calls atomic.Int32
}

func (l *acceptObserver) Accept() (net.Conn, error) { l.calls.Add(1); return l.Listener.Accept() }

// R-V956-9CSV
func TestRunMissingAndInvalidServicesStillServes(t *testing.T) {
	base := t.TempDir()
	invalid := filepath.Join(base, "invalid")
	if e := os.WriteFile(invalid, []byte("not json"), 0600); e != nil {
		t.Fatal(e)
	}
	for i, path := range []string{"", "", filepath.Join(base, "absent"), invalid, servicesFile(t, []map[string]any{})} {
		r := newRunRig(t)
		if i != 0 {
			r.env[services.Variable] = path
		}
		r.start(t)
		r.ok(t, tools.ToolListRules, tools.NoArgs{})
		if status, _, _ := r.request(t, "/", true); status != http.StatusOK {
			t.Fatal(status)
		}
		if r.stop(t) != cli.ExitSuccess {
			t.Fatal("stop failed")
		}
	}
}

// R-VF8O-67IC
func TestRunUsesRewrittenAuthEntry(t *testing.T) {
	r := newRunRig(t)
	path := servicesFile(t, []map[string]any{serviceEntry("auth", "https://auth.one.test", "")})
	r.env[services.Variable] = path
	r.start(t)
	_, _, h := r.request(t, "/", false)
	if !strings.HasPrefix(h.Get("Location"), "https://auth.one.test/") {
		t.Fatal(h)
	}
	writeServices(t, path, []map[string]any{serviceEntry("auth", "https://auth.two.test", "")})
	_, _, h = r.request(t, "/", false)
	if !strings.HasPrefix(h.Get("Location"), "https://auth.two.test/") {
		t.Fatal(h)
	}
	r.stop(t)
}

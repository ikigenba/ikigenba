package cli_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

func seedRun(t *testing.T, st *store.Store, scraper, status string) store.Run {
	t.Helper()
	ctx := context.Background()
	r, e := st.AddRun(ctx, store.Run{Scraper: scraper, UserID: "seed-owner", RequestID: "seed-request"})
	if e != nil {
		t.Fatal(e)
	}
	counts := store.Counts{}
	if status == store.StatusDone {
		build, err := st.Build(ctx, r.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range []source.Row{{SourceID: "seed-one", Raw: []byte(`{"dot_number":"seed-one"}`), Digest: "seed-digest", Scraped: fixtureNow()}, {SourceID: "seed-two", Raw: []byte(`{"dot_number":"seed-two"}`), Digest: "seed-digest", Scraped: fixtureNow()}} {
			if err := build.Add(ctx, []source.Row{row}); err != nil {
				t.Fatal(err)
			}
		}
		if err := build.Seal(ctx); err != nil {
			t.Fatal(err)
		}
		counts, err = build.Load(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	switch status {
	case store.StatusDone:
		r, e = st.FinishRun(ctx, r.ID, counts)
	case store.StatusFailed:
		r, e = st.FailRun(ctx, r.ID, store.ReasonSource)
	}
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func runRefused(t *testing.T, f *runFixture, id string) bool {
	t.Helper()
	return f.call(fixtureCaller, "run", map[string]any{"run": id}).IsError()
}

// R-RJPE-VAAJ R-FORG-FK6Z
func TestRunSettlesBeforeReady(t *testing.T) {
	dir := t.TempDir()
	st, d := openStore(t, dir)
	seed := seedRun(t, st, "fmcsa", store.StatusRunning)
	loadSeedRows(t, st, seed.ID)
	done := seedRun(t, st, "other", store.StatusDone)
	failed := seedRun(t, st, "other", store.StatusRunning)
	loadSeedRows(t, st, failed.ID)
	if _, err := st.FailRun(context.Background(), failed.ID, store.ReasonSource); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{seed.ID + ".part", "fixture-junk"} {
		if err := os.WriteFile(filepath.Join(dir, "runs", name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "runs", "fixture-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	f := startRun(t, runOptions{Dir: dir})
	assertRunFiles(t, dir, done.ID)
	v := resultJSON(t, f.call(fixtureCaller, "run", map[string]any{"run": seed.ID}))
	if v["status"] != store.StatusFailed || v["reason"] != store.ReasonInterrupted || v["finished"] != fixtureNow().Format(time.RFC3339) {
		t.Fatalf("settled %v", v)
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop")
	}
	st, _ = openStore(t, dir)
	r, e := st.GetRun(context.Background(), seed.ID)
	if e != nil || r.Status != store.StatusFailed || r.Reason != store.ReasonInterrupted || !r.Finished.Equal(fixtureNow()) || r.UserID != seed.UserID || r.RequestID != seed.RequestID || !r.Started.Equal(seed.Started) {
		t.Fatalf("settled %+v %v", r, e)
	}
	for _, id := range []string{"seed-one", "seed-two"} {
		if _, e = st.GetRow(context.Background(), seed.ID, id); !errors.Is(e, store.ErrNotFound) {
			t.Fatalf("row %s %v", id, e)
		}
	}
	evs := f.Telemetry.Events()
	if len(evs) < 3 || evs[0].Name != "service.started" || evs[1].Name != "scrape.finished" || evs[2].Name != "request.started" {
		t.Fatalf("events %v", evs)
	}
	if evs[0].RequestID != "" || evs[0].User != "" || len(evs[0].Attrs) != 1 || evs[0].Attrs["version"] != f.Process.Version {
		t.Fatalf("started %+v", evs[0])
	}
}

// R-RKXB-9218 R-3MYZ-6YRM
func TestRunPrunesBeforeReady(t *testing.T) {
	dir := t.TempDir()
	st, d := openStore(t, dir)
	done := []store.Run{}
	failed := []store.Run{}
	for range 3 {
		done = append(done, seedRun(t, st, "fmcsa", store.StatusDone))
	}
	for range 2 {
		failed = append(failed, seedRun(t, st, "fmcsa", store.StatusFailed))
	}
	other := seedRun(t, st, "other", store.StatusDone)
	_ = d.Close()
	f := startRun(t, runOptions{Dir: dir, Env: map[string]string{"SCRAPE_KEEP_RUNS": "1"}})
	assertRunFiles(t, dir, done[2].ID, other.ID)
	all := append(append(done, failed...), other)
	for _, r := range all {
		want := r.ID != done[2].ID && r.ID != failed[1].ID && r.ID != other.ID
		if got := runRefused(t, f, r.ID); got != want {
			t.Fatalf("run %s refused %v want %v", r.ID, got, want)
		}
	}
}

type sweepClock struct {
	channels chan chan time.Time
	t        *testing.T
}

func newSweep(t *testing.T) *sweepClock {
	return &sweepClock{channels: make(chan chan time.Time, 10), t: t}
}
func (s *sweepClock) after(d time.Duration) <-chan time.Time {
	if d != cli.SweepInterval {
		s.t.Errorf("interval %v", d)
	}
	c := make(chan time.Time, 1)
	s.channels <- c
	return c
}
func (s *sweepClock) next() chan time.Time {
	s.t.Helper()
	select {
	case c := <-s.channels:
		return c
	case <-time.After(5 * time.Second):
		s.t.Fatal("sweep was not scheduled")
		return nil
	}
}

// R-YE0M-T5ML R-2K09-UXU0 R-DNJ9-8BA3
func TestRunSweepsOnInjectedTimer(t *testing.T) {
	if cli.SweepInterval != time.Hour {
		t.Fatalf("interval %v", cli.SweepInterval)
	}
	timer := newSweep(t)
	f := startRun(t, runOptions{After: timer.after, Env: map[string]string{"SCRAPE_KEEP_RUNS": "1"}})
	tick := timer.next()
	first := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))["id"].(string)
	f.awaitRun(first)
	second := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))["id"].(string)
	f.awaitRun(second)
	if runRefused(t, f, first) {
		t.Fatal("pruned before tick")
	}
	tick <- fixtureNow()
	_ = timer.next()
	if !runRefused(t, f, first) || runRefused(t, f, second) {
		t.Fatal("sweep kept wrong runs")
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop")
	}
	select {
	case <-timer.channels:
		t.Fatal("timer scheduled after stop")
	default:
	}
}

// R-6UMH-DQUS R-3MYZ-6YRM
func TestRunPruneFailureIsSilentAndRetried(t *testing.T) {
	dir := t.TempDir()
	st, d := openStore(t, dir)
	first := seedRun(t, st, "fmcsa", store.StatusDone)
	second := seedRun(t, st, "fmcsa", store.StatusDone)
	ctx := context.Background()
	if e := d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `CREATE TRIGGER fixture_delete BEFORE DELETE ON runs BEGIN SELECT RAISE(ABORT,'fixture'); END`)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	timer := newSweep(t)
	f := startRun(t, runOptions{Dir: dir, After: timer.after, Env: map[string]string{"SCRAPE_KEEP_RUNS": "1"}})
	tick := timer.next()
	if runRefused(t, f, first.ID) || runRefused(t, f, second.ID) || f.Err.String() != "" {
		t.Fatal("prune refused startup or removed run")
	}
	assertRunFiles(t, dir, first.ID, second.ID)
	before := len(f.Events.Events())
	tick <- fixtureNow()
	tick = timer.next()
	assertRunFiles(t, dir, first.ID, second.ID)
	if runRefused(t, f, first.ID) || f.Err.String() != "" || len(f.Events.Events()) != before {
		t.Fatal("failed prune not silent")
	}
	if e := d.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, `DROP TRIGGER fixture_delete`); return e }); e != nil {
		t.Fatal(e)
	}
	tick <- fixtureNow()
	_ = timer.next()
	assertRunFiles(t, dir, second.ID)
	if !runRefused(t, f, first.ID) || runRefused(t, f, second.ID) {
		t.Fatal("prune not retried")
	}
	if f.stop() != cli.ExitSuccess || f.Err.String() != "" {
		t.Fatal("stop")
	}
}

// R-FNJK-1SGA
func TestRunRecoveryFailure(t *testing.T) {
	t.Setenv(services.Variable, "")
	dir := t.TempDir()
	st, d := openStore(t, dir)
	seedRun(t, st, "fmcsa", store.StatusRunning)
	ctx := context.Background()
	if e := d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `CREATE TRIGGER fixture_settle BEFORE INSERT ON runs BEGIN SELECT RAISE(ABORT,'fixture'); END`)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	_ = d.Close()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = ln.Close() }()
	notify := notificationSocket(t)
	accepted := make(chan struct{})
	tracked := &acceptSignal{Listener: ln, ready: accepted}
	out, errw := &outputBuffer{}, &outputBuffer{}
	capture := &telemetry.Capture{}
	bus := &events.Capture{}
	p := cli.Process{Dir: dir, Pid: 9, Stdout: out, Stderr: errw, Now: fixtureNow, Rand: &runRandom{}, Sink: capture, EventSink: bus, LookupEnv: func(k string) (string, bool) {
		switch k {
		case "NOTIFY_SOCKET":
			return notify.LocalAddr().String(), true
		case "LISTEN_PID":
			return "9", true
		case "LISTEN_FDS":
			return "1", true
		}
		return "", false
	}, Inherit: func(uintptr) (net.Listener, error) { return tracked, nil }, HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("source called")
		return nil, errors.New("unexpected")
	})}}
	if code := cli.Run(ctx, p); code != cli.ExitServerFailed {
		t.Fatalf("code %d", code)
	}
	evs := capture.Events()
	if len(evs) != 1 || evs[0].Name != "service.started" || len(bus.Events()) != 0 || errw.String() != "scrapes: "+store.Unreachable+"\n" || out.String() != "" || errw.writes != 1 {
		t.Fatalf("recovery events %v streams %q %q", evs, out.String(), errw.String())
	}
	if pendingNotify(t, notify) {
		t.Fatal("refusal sent readiness")
	}
	select {
	case <-accepted:
		t.Fatal("refusal accepted a connection")
	default:
	}
}

// R-YGGF-KP3Z
func TestRunSerializesRandomReader(t *testing.T) {
	rnd := &checkingRandom{}
	f := startRun(t, runOptions{Rand: rnd})
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() { r := f.request("GET", "/scrapers/fmcsa/current", "", true); _ = r.Body.Close() })
	}
	v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))
	f.awaitRun(v["id"].(string))
	wg.Wait()
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop")
	}
	if rnd.overlap.Load() {
		t.Fatal("random Read overlapped")
	}
}

type checkingRandom struct {
	active  atomic.Int32
	overlap atomic.Bool
	n       byte
}

func (r *checkingRandom) Read(p []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	defer r.active.Add(-1)
	runtime.Gosched()
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

// R-2L86-8PKP R-UDO9-9DC7
func TestRunDrainsBlockedSource(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	completed := make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	src := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(entered) })
		<-release
		resp := jsonResponse(r, []any{})
		resp.Body = &notifyingBody{ReadCloser: resp.Body, done: completed}
		return resp, nil
	}}
	trailSink, busSink := &timedTrail{}, &timedBus{}
	f := startRun(t, runOptions{Env: map[string]string{"DRAIN_SECONDS": "1"}, Source: src, Sink: trailSink, EventSink: busSink})
	v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))
	id := v["id"].(string)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("page not reached")
	}
	began := time.Now()
	if code := f.stop(); code != cli.ExitSuccess {
		t.Fatalf("code %d", code)
	}
	for _, begun := range append(trailSink.Starts(), busSink.Starts()...) {
		if begun.After(began.Add(time.Second)) {
			t.Fatalf("delivery began after deadline %v", begun.Sub(began))
		}
	}
	if time.Since(began) > 2*time.Second {
		t.Fatal("drain exceeded bound")
	}
	st, _ := openStore(t, f.Dir)
	r, e := st.GetRun(context.Background(), id)
	if e != nil || r.Status != store.StatusFailed || r.Reason != store.ReasonInterrupted {
		t.Fatalf("interrupted %+v %v", r, e)
	}
	rows, e := st.ListRowsAfter(context.Background(), id, "", "", 10)
	if e != nil || len(rows) != 0 {
		t.Fatalf("rows %v %v", rows, e)
	}
	beforeTrail, beforeBus := len(trailSink.Starts()), len(busSink.Starts())
	released.Do(func() { close(release) })
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("held source exchange did not finish")
	}
	awaitDiagnosticEvent(t, f.Err, source.Called)
	if len(trailSink.Starts()) != beforeTrail || len(busSink.Starts()) != beforeBus {
		t.Fatal("late source event reached sink")
	}
}

// R-DR6Y-DMI6 R-UDO9-9DC7
func TestRunReportsUnfinishedRequest(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	handlerEnded := make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	trailSink, busSink := &timedTrail{}, &timedBus{}
	f := startRun(t, runOptions{Sink: trailSink, EventSink: busSink, Env: map[string]string{"DRAIN_SECONDS": "1"}, Modify: func(p *cli.Process) {
		p.MCP = func(w *telemetry.Writer) *mcp.Server {
			srv := mcp.NewServer(mcp.ServerConfig{Name: "fixture", Version: "fixture", Telemetry: w})
			mcp.AddTool(srv, mcp.Tool[struct{}, struct{}]{Name: "fixture_block", Description: "Fixture blocked request.", Effect: mcp.Read, Handler: func(ctx context.Context, _ identity.Caller, _ struct{}) (struct{}, error) {
				defer close(handlerEnded)
				close(entered)
				<-release
				w.Emit(ctx, "fixture.late", telemetry.Attrs{})
				return struct{}{}, nil
			}})
			return srv
		}
	}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = f.Client.CallTool(ctx, fixtureCaller, "fixture_block", []byte(`{}`))
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request not reached")
	}
	if code := f.stop(); code != cli.ExitServerFailed {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasSuffix(f.Err.String(), "scrapes: stopped with 1 request unfinished\n") {
		t.Fatalf("diagnostic %q", f.Err.String())
	}
	beforeTrail, beforeBus := len(trailSink.Starts()), len(busSink.Starts())
	released.Do(func() { close(release) })
	select {
	case <-handlerEnded:
	case <-time.After(5 * time.Second):
		t.Fatal("held handler did not finish")
	}
	awaitDiagnosticEvent(t, f.Err, "fixture.late")
	awaitDiagnosticEvent(t, f.Err, "request.finished")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("client did not finish")
	}
	if len(trailSink.Starts()) != beforeTrail || len(busSink.Starts()) != beforeBus {
		t.Fatal("late handler event reached sink")
	}
	if f.Out.String() != "" {
		t.Fatalf("stdout %q", f.Out.String())
	}
	for _, line := range strings.Split(strings.TrimSuffix(f.Err.String(), "\n"), "\n") {
		if line != "scrapes: stopped with 1 request unfinished" && !strings.HasPrefix(line, "scrapes: undelivered event: ") && !strings.HasPrefix(line, "scrapes: lost event: ") {
			t.Fatalf("unexpected stderr line %q", line)
		}
	}
}

// R-UDO9-9DC7
func TestRunFlushesCancelledRunWithinDrain(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	src := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(entered) })
		<-r.Context().Done()
		return nil, r.Context().Err()
	}}
	f := startRun(t, runOptions{Env: map[string]string{"DRAIN_SECONDS": "1"}, Source: src})
	v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))
	id := v["id"].(string)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("page not reached")
	}
	if code := f.stop(); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	bus := f.Events.Events()
	if len(bus) != 2 || bus[0].Name != "scrape.started" || bus[1].Name != "scrape.finished" || bus[1].Attrs["run"] != id {
		t.Fatalf("bus not flushed %v", bus)
	}
	if f.Out.String() != "" || f.Err.String() != "" {
		t.Fatalf("healthy sinks wrote output %q %q", f.Out.String(), f.Err.String())
	}
}

type timedTrail struct {
	mu     sync.Mutex
	starts []time.Time
}

func (s *timedTrail) Deliver(_ context.Context, _ telemetry.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts = append(s.starts, time.Now())
	return nil
}
func (s *timedTrail) Starts() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time{}, s.starts...)
}

type timedBus struct {
	mu     sync.Mutex
	starts []time.Time
}

func (s *timedBus) Deliver(_ context.Context, _ events.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts = append(s.starts, time.Now())
	return nil
}
func (s *timedBus) Starts() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time{}, s.starts...)
}

// R-2K09-UXU0
func TestRunSweepKeepsRunningRun(t *testing.T) {
	var hold atomic.Bool
	entered := make(chan struct{})
	var once sync.Once
	src := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
		if hold.Load() {
			once.Do(func() { close(entered) })
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return jsonResponse(r, []any{map[string]string{"dot_number": r.URL.Query().Get("$where")}}), nil
	}}
	timer := newSweep(t)
	f := startRun(t, runOptions{Source: src, After: timer.after, Env: map[string]string{"SCRAPE_KEEP_RUNS": "1"}})
	tick := timer.next()
	first := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))["id"].(string)
	f.awaitRun(first)
	second := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))["id"].(string)
	f.awaitRun(second)
	hold.Store(true)
	third := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}))["id"].(string)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("page not reached")
	}
	tick <- fixtureNow()
	_ = timer.next()
	if !runRefused(t, f, first) || runRefused(t, f, second) {
		t.Fatal("wrong done runs kept")
	}
	if v := resultJSON(t, f.call(fixtureCaller, "run", map[string]any{"run": third})); v["status"] != store.StatusRunning {
		t.Fatalf("running run removed %v", v)
	}
}

type notifyingBody struct {
	io.ReadCloser
	done chan struct{}
	once sync.Once
}

func (b *notifyingBody) Close() error {
	e := b.ReadCloser.Close()
	b.once.Do(func() { close(b.done) })
	return e
}
func awaitDiagnosticEvent(t *testing.T, b *outputBuffer, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		for _, line := range strings.Split(b.String(), "\n") {
			prefix := "scrapes: undelivered event: "
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			var event map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &event) == nil && event["event"] == name {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("late event %s missing from diagnostics %q", name, b.String())
		default:
			runtime.Gosched()
		}
	}
}

func loadSeedRows(t *testing.T, st *store.Store, id string) {
	t.Helper()
	ctx := context.Background()
	c, err := st.Build(ctx, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Add(ctx, []source.Row{{SourceID: "seed-one", Raw: []byte(`{"id":"seed-one"}`), Digest: "seed-digest", Scraped: fixtureNow()}}); err != nil {
		t.Fatal(err)
	}
	if err = c.Seal(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Load(ctx, nil); err != nil {
		t.Fatal(err)
	}
}
func assertRunFiles(t *testing.T, dir string, ids ...string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "runs"))
	if err != nil || len(entries) != len(ids) {
		t.Fatalf("run files %v: %v, want %v", entries, err, ids)
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id+".db"] = true
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !want[entry.Name()] {
			t.Fatalf("unexpected run file %v", entry)
		}
	}
}

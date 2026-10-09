package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

var testInstant = time.Date(2025, 2, 3, 4, 5, 6, 987654321, time.UTC)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == agent.Command {
		os.Exit(agent.Run(context.Background(), agent.Process{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, LookupEnv: os.LookupEnv, Now: func() time.Time { return testInstant }}))
	}
	os.Exit(m.Run())
}

type testCounter struct {
	mu sync.Mutex
	n  byte
}

func (r *testCounter) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

type testSink struct {
	capture telemetry.Capture
	events  chan telemetry.Event
}

func (s *testSink) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(ctx, e)
	s.events <- e
	return nil
}

type coreFixture struct {
	pending []telemetry.Event
	c       *Core
	s       *store.Store
	d       *db.DB
	p       store.Prompt
	w       *telemetry.Writer
	sink    *testSink
	timers  chan chan time.Time
	cfg     Config
}

func newCoreFixture(t *testing.T) *coreFixture {
	t.Helper()
	dir := t.TempDir()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "catalog.db"), Migrations: prompts.Migrations(), Now: func() time.Time { return testInstant }})
	if e != nil {
		t.Fatal(e)
	}
	s := store.New(d, store.Config{Now: func() time.Time { return testInstant }, Rand: &testCounter{}})
	model := ""
	for _, entry := range agentkit.Catalog() {
		o, e := agent.Offering(entry.Model)
		if e == nil && o.Host == agentkit.HostAnthropic {
			model = entry.Model
			break
		}
	}
	if model == "" {
		t.Fatal("no Anthropic offering")
	}
	p, e := s.Create(context.Background(), store.Draft{Owner: "alice", OwnerEmail: "alice@example.test", Name: "one", Model: model, Prompt: "hello"})
	if e != nil {
		t.Fatal(e)
	}
	sink := &testSink{events: make(chan telemetry.Event, 512)}
	w := telemetry.New(telemetry.Config{Service: "prompts", Sink: sink, Stderr: io.Discard, Now: func() time.Time { return testInstant }, Rand: &testCounter{}})
	w.Ready()
	timers := make(chan chan time.Time, 64)
	cfg := Config{Store: s, Writer: w, Runs: filepath.Join(dir, "runs"), Path: "", Keys: map[agentkit.Host]string{agentkit.HostAnthropic: "xK7!yQ2%zP9@aF6&bH4?mV8+"}, PromptSeconds: 2, OutputMaxBytes: 100, MaxToolCalls: 1, KeepDays: 30, KeepCount: 100, RunMemoryMaxBytes: 1024, RunPidsMax: 10, MaxActive: 2, MaxQueued: 3, Now: func() time.Time { return testInstant }, ScriptAfter: func(_ time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); timers <- ch; return ch }, Rand: &testCounter{}}
	f := &coreFixture{s: s, d: d, p: p, w: w, sink: sink, timers: timers, cfg: cfg}
	f.c = New(cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f.c.Drain(ctx)
		w.Shutdown(context.Background(), "test finished")
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	return f
}
func (f *coreFixture) rebuild() { f.c = New(f.cfg) }
func (f *coreFixture) request() Request {
	return Request{Input: []byte(` {"x":1} `), Caller: identity.Caller{UserID: "alice", Email: "alice@example.test", RequestID: "request-one"}}
}
func (f *coreFixture) start(t *testing.T, p store.Prompt, req Request) store.Run {
	t.Helper()
	r, e := f.c.Run(context.Background(), p, req)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (f *coreFixture) event(t *testing.T, name, id string) telemetry.Event {
	t.Helper()
	for i, e := range f.pending {
		if e.Name == name && e.Attrs["prompt_run"] == id {
			f.pending = append(f.pending[:i], f.pending[i+1:]...)
			return e
		}
	}
	guard := time.NewTimer(10 * time.Second)
	defer guard.Stop()
	for {
		select {
		case e := <-f.sink.events:
			if e.Name == name && e.Attrs["prompt_run"] == id {
				return e
			}
			f.pending = append(f.pending, e)
		case <-guard.C:
			t.Fatalf("no %s for %s", name, id)
		}
	}
}
func testReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("channel deadline")
		var zero T
		return zero
	}
}
func testEqual(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v want %#v", a, b)
	}
}
func testRead(t *testing.T, p string) []byte {
	t.Helper()
	root, e := os.OpenRoot(filepath.Dir(p))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = root.Close() }()
	file, e := root.Open(filepath.Base(p))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = file.Close() }()
	b, e := io.ReadAll(file)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func provider(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)
	return server
}
func frame(w http.ResponseWriter, event string, v any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}
func answer(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	frame(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]int{"input_tokens": 3}}})
	frame(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
	frame(w, "message_delta", map[string]any{"type": "message_delta", "usage": map[string]int{"output_tokens": 2}, "delta": map[string]string{"stop_reason": "end_turn"}})
	frame(w, "message_stop", map[string]string{"type": "message_stop"})
}

// R-NGVH-QLL2 R-NJBA-I52G R-NKJ6-VWT5 R-NMYZ-NGAJ R-NO6W-1818 R-NPES-EZRX R-13V7-KI2I R-NQMO-SRIM
func TestCoreContract(t *testing.T) {
	f := newCoreFixture(t)
	testEqual(t, []string{InputFile, StdoutFile, StderrFile, TranscriptFile, WorkDir}, []string{"input.json", "stdout", "stderr", "transcript.jsonl", "work"})
	for i, a := range []error{ErrDraining, ErrStarting, ErrQueueFull, ErrNoCgroup, ErrCutOff, store.ErrNotFound, store.ErrDelivered, store.ErrEnded, context.Canceled, context.DeadlineExceeded} {
		if a == nil {
			t.Fatal(i)
		}
		for j, b := range []error{ErrDraining, ErrStarting, ErrQueueFull, ErrNoCgroup, ErrCutOff, store.ErrNotFound, store.ErrDelivered, store.ErrEnded, context.Canceled, context.DeadlineExceeded} {
			if i != j && errors.Is(a, b) {
				t.Fatal(i, j)
			}
		}
	}
	_ = []string{Stopping, Starting, NoEventID}
	for _, check := range []struct {
		format   string
		verb     byte
		value    any
		rendered string
	}{{NoRuns, 's', "fixture-reason", "fixture-reason"}, {QueueFull, 'd', int64(17), "17"}} {
		var expected strings.Builder
		var verbs []byte
		for i := 0; i < len(check.format); i++ {
			if check.format[i] != '%' {
				expected.WriteByte(check.format[i])
				continue
			}
			i++
			if i == len(check.format) {
				t.Fatal("incomplete copy format")
			}
			if check.format[i] == '%' {
				expected.WriteByte('%')
				continue
			}
			verbs = append(verbs, check.format[i])
			expected.WriteString(check.rendered)
		}
		testEqual(t, verbs, []byte{check.verb})
		testEqual(t, fmt.Sprintf(check.format, check.value), expected.String())
	}
	if e := f.c.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := f.c.Prune(context.Background()); e != nil {
		t.Fatal(e)
	}
}

// R-O0DV-UXG6 R-O2TO-MGXK R-OHGH-7PTW R-OJW9-Z9BA R-OTNH-1F8U R-OUVD-F6ZJ R-OW39-SYQ8 R-OYJ2-KI7M R-FDPH-KJZZ R-FEXD-YBQO R-PBXY-RZD9 R-QEYR-0N3H R-G5R6-DA1Y R-QHEJ-S6KV
func TestCoreAnswerAndBound(t *testing.T) {
	f := newCoreFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server := provider(t, func(w http.ResponseWriter, _ *http.Request) { close(entered); <-release; answer(w, "pong pong") })
	f.cfg.BaseURL = server.URL
	f.cfg.OutputMaxBytes = 4
	var mu sync.Mutex
	now := testInstant
	f.cfg.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	f.rebuild()
	ctx, cancel := context.WithCancel(context.Background())
	r, e := f.c.Run(ctx, f.p, f.request())
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	testReceive(t, entered)
	if r.Status != store.StatusRunning || !store.ValidRunID(r.ID) || r.Prompt != f.p.ID || r.Model != f.p.Model || r.User != "alice" || r.RequestID != "request-one" || r.Trigger != store.TriggerManual || r.Event != "" || !r.Started.Equal(testInstant.Truncate(time.Second)) {
		t.Fatal(r)
	}
	stored, e := f.s.RunByID(context.Background(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, r, stored)
	testEqual(t, testRead(t, filepath.Join(f.c.Folder(r), InputFile)), f.request().Input)
	entries, e := os.ReadDir(f.c.Folder(r))
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	testEqual(t, names, map[string]bool{InputFile: true, WorkDir: true, StdoutFile: true, StderrFile: true, TranscriptFile: true})
	started := f.event(t, "run.started", r.ID)
	testEqual(t, started.Attrs, StartedAttrs(r))
	testEqual(t, started.User, r.User)
	testEqual(t, started.RequestID, r.RequestID)
	mu.Lock()
	now = testInstant.Add(2*time.Second + 1500*time.Microsecond)
	mu.Unlock()
	close(release)
	finished := f.event(t, "run.finished", r.ID)
	ended, e := f.s.RunByID(context.Background(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if ended.Status != store.StatusExited || ended.ExitCode != agent.ExitAnswered || ended.StdoutBytes != 4 || !ended.StdoutTruncated || ended.StderrBytes != 0 || ended.StderrTruncated {
		t.Fatal(ended)
	}
	testEqual(t, string(testRead(t, filepath.Join(f.c.Folder(r), StdoutFile))), "pong")
	testEqual(t, string(testRead(t, filepath.Join(f.c.Folder(r), StderrFile))), "")
	testEqual(t, finished.Attrs, FinishedAttrs(ended, 2*time.Second+1500*time.Microsecond))
	testEqual(t, ended.Finished, testInstant.Add(2*time.Second).Truncate(time.Second))
	if finished.User != r.User || finished.RequestID != r.RequestID {
		t.Fatal(finished)
	}
}

// R-F2QE-4MBQ R-F566-W5T4 R-O59H-E0EY R-EYEO-8L0I R-12NB-6QBT R-OA52-X3DQ R-OBCZ-AV4F R-FTK6-JKN0 R-107I-F6UF R-0RO7-QSNK R-PO4Y-LOS7
func TestCoreQueueFIFO(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.MaxActive = 1
	f.cfg.MaxQueued = 3
	entered := make(chan struct{}, 8)
	release := make(chan struct{}, 8)
	server := provider(t, func(w http.ResponseWriter, _ *http.Request) { entered <- struct{}{}; <-release; answer(w, "done") })
	f.cfg.BaseURL = server.URL
	f.rebuild()
	a := f.start(t, f.p, f.request())
	testReceive(t, entered)
	if a.Status != store.StatusRunning {
		t.Fatal(a)
	}
	b := f.start(t, f.p, f.request())
	bad := f.request()
	bad.Input = []byte("{x")
	invalid := f.start(t, f.p, bad)
	cancelled := f.start(t, f.p, f.request())
	for _, r := range []store.Run{b, invalid, cancelled} {
		if r.Status != store.StatusQueued {
			t.Fatal(r)
		}
		entries, e := os.ReadDir(f.c.Folder(r))
		if e != nil {
			t.Fatal(e)
		}
		testEqual(t, []string{entries[0].Name(), entries[1].Name()}, []string{InputFile, WorkDir})
		stored, e := f.s.RunByID(context.Background(), r.ID)
		if e != nil {
			t.Fatal(e)
		}
		testEqual(t, stored, r)
	}
	r, e := f.c.Run(context.Background(), f.p, f.request())
	testEqual(t, r, store.Run{})
	if !errors.Is(e, ErrQueueFull) || e.Error() != fmt.Sprintf(QueueFull, 3) {
		t.Fatal(e)
	}
	ended, e := f.c.Cancel(context.Background(), cancelled.ID)
	if e != nil || ended.Status != store.StatusKilled || ended.ExitCode != 0 || ended.Truncated() || ended.Usage != (store.Usage{}) {
		t.Fatal(ended, e)
	}
	f.event(t, "run.finished", cancelled.ID)
	following := f.start(t, f.p, f.request())
	if following.Status != store.StatusQueued {
		t.Fatal(following)
	}
	if len(f.timers) != 1 {
		t.Fatal("a queued or failed run received a timer", len(f.timers))
	}
	release <- struct{}{}
	f.event(t, "run.finished", a.ID)
	testReceive(t, entered)
	f.event(t, "run.started", b.ID)
	if len(f.timers) != 2 {
		t.Fatal("started run did not receive exactly one timer", len(f.timers))
	}
	select {
	case <-entered:
		t.Fatal("next queued request started too early")
	default:
	}
	release <- struct{}{}
	f.event(t, "run.finished", b.ID)
	event := f.event(t, "run.finished", invalid.ID)
	stored, e := f.s.RunByID(context.Background(), invalid.ID)
	if e != nil || stored.Status != store.StatusFailed || stored.Reason != store.ReasonStartFailed || stored.Usage != (store.Usage{}) || stored.StdoutBytes != 0 || stored.StderrBytes != 0 {
		t.Fatal(stored, e)
	}
	testEqual(t, event.Attrs, FinishedAttrs(stored, 0))
	testReceive(t, entered)
	f.event(t, "run.started", following.ID)
	if len(f.timers) != 3 {
		t.Fatal("started or failed run received wrong timer count", len(f.timers))
	}
	release <- struct{}{}
	f.event(t, "run.finished", following.ID)
	entries, e := os.ReadDir(f.c.Folder(invalid))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 2 {
		t.Fatal(entries)
	}
	// An immediately uncomposable spec is still a recorded failed run.
	direct := f.start(t, f.p, bad)
	if len(f.timers) != 3 {
		t.Fatal("failed start received a timer")
	}
	if direct.Status != store.StatusFailed || direct.Reason != store.ReasonStartFailed {
		t.Fatal(direct)
	}
	f.event(t, "run.finished", direct.ID)
	if e = f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	events := f.sink.capture.Events()
	seen := map[string][]string{}
	for _, ev := range events {
		if id, ok := ev.Attrs["prompt_run"].(string); ok {
			seen[id] = append(seen[id], ev.Name)
		}
	}
	testEqual(t, seen[a.ID], []string{"run.started", "run.finished"})
	testEqual(t, seen[b.ID], []string{"run.started", "run.finished"})
	testEqual(t, seen[following.ID], []string{"run.started", "run.finished"})
	for _, id := range []string{invalid.ID, cancelled.ID, direct.ID} {
		testEqual(t, seen[id], []string{"run.finished"})
	}
}

// R-M97H-3GOR R-FJSZ-HEPG R-FUS2-XCDP R-PT0K-4RQZ R-P8A9-MO56
func TestCoreKillAndTimeout(t *testing.T) {
	for _, mode := range []string{"cancel", "timer", "race"} {
		t.Run(mode, func(t *testing.T) {
			f := newCoreFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			server := provider(t, func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
					answer(w, "done")
				case <-r.Context().Done():
				}
			})
			f.cfg.BaseURL = server.URL
			f.cfg.PromptSeconds = math.MaxInt64
			var duration time.Duration
			f.cfg.ScriptAfter = func(d time.Duration) <-chan time.Time {
				duration = d
				ch := make(chan time.Time, 1)
				f.timers <- ch
				return ch
			}
			f.cfg.KeepCount = 1
			f.rebuild()
			// An older ending is retained until the next ending's prune.
			old, e := f.s.AddRun(context.Background(), store.Run{ID: "prr_fefefefefefefefe", Prompt: f.p.ID, Model: f.p.Model, User: f.p.Owner, RequestID: "old", Trigger: store.TriggerManual, Status: store.StatusFailed, Reason: store.ReasonStartFailed, Started: testInstant.Add(-31 * 24 * time.Hour), Finished: testInstant.Add(-31 * 24 * time.Hour)})
			if e != nil {
				t.Fatal(e)
			}
			if e = os.MkdirAll(f.c.Folder(old), 0700); e != nil {
				t.Fatal(e)
			}
			r := f.start(t, f.p, f.request())
			testReceive(t, entered)
			timer := testReceive(t, f.timers)
			if duration != time.Duration(math.MaxInt64) {
				t.Fatal(duration)
			}
			var cancelErr error
			switch mode {
			case "timer":
				timer <- testInstant
			case "race":
				close(release)
				_, cancelErr = f.c.Cancel(context.Background(), r.ID)
				if cancelErr != nil && !errors.Is(cancelErr, store.ErrEnded) {
					t.Fatal(cancelErr)
				}
			default:
				ended, e := f.c.Cancel(context.Background(), r.ID)
				if e != nil || ended.Status != store.StatusKilled {
					t.Fatal(ended, e)
				}
			}
			ev := f.event(t, "run.finished", r.ID)
			stored, e := f.s.RunByID(context.Background(), r.ID)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "timer" && stored.Status != store.StatusTimedOut {
				t.Fatal(stored)
			}
			if mode == "race" {
				if cancelErr == nil && stored.Status != store.StatusKilled || cancelErr != nil && stored.Status != store.StatusExited {
					t.Fatal(stored, cancelErr)
				}
			}
			if stored.Status != store.StatusExited && stored.ExitCode != 0 {
				t.Fatal(stored)
			}
			testEqual(t, ev.Attrs, FinishedAttrs(stored, 0))
			if _, e = f.s.RunByID(context.Background(), old.ID); !errors.Is(e, store.ErrNotFound) || !f.c.Gone(old) {
				t.Fatal("ending did not prune", e)
			}
			if _, e = os.Stat(filepath.Join(f.c.Folder(r), TranscriptFile)); e != nil {
				t.Fatal(e)
			}
			if e = f.w.Flush(context.Background()); e != nil {
				t.Fatal(e)
			}
			count := 0
			for _, e := range f.sink.capture.Events() {
				if e.Name == "run.finished" && e.Attrs["prompt_run"] == r.ID {
					count++
				}
			}
			if count != 1 {
				t.Fatal(count)
			}
		})
	}
}

// R-F8TW-1H17
func TestQueuedCatalogStartFailure(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.MaxActive = 1
	f.cfg.Cgroup = filepath.Join(t.TempDir(), "groups")
	if e := os.Mkdir(f.cfg.Cgroup, 0700); e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{}, 4)
	release := make(chan struct{}, 4)
	server := provider(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			answer(w, "done")
		case <-r.Context().Done():
		}
	})
	f.cfg.BaseURL = server.URL
	starts := 0
	f.cfg.ScriptAfter = func(_ time.Duration) <-chan time.Time {
		starts++
		switch starts {
		case 2:
			f.d.SetFailing(true)
		case 3:
			f.d.SetFailing(false)
		}
		return make(chan time.Time)
	}
	f.rebuild()
	a := f.start(t, f.p, f.request())
	testReceive(t, entered)
	b := f.start(t, f.p, f.request())
	c := f.start(t, f.p, f.request())
	release <- struct{}{}
	f.event(t, "run.started", c.ID)
	if _, e := os.Lstat(filepath.Join(f.cfg.Cgroup, b.ID)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	if e := f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, ev := range f.sink.capture.Events() {
		if ev.Attrs["prompt_run"] == b.ID {
			t.Fatal(ev)
		}
	}
	_ = a
	if _, e := f.c.Cancel(context.Background(), c.ID); e != nil {
		t.Fatal(e)
	}
}

// R-FX7V-OVV3 R-PXW5-NUPR
func TestCoreDelete(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.MaxActive = 1
	entered := make(chan struct{}, 2)
	server := provider(t, func(_ http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() })
	f.cfg.BaseURL = server.URL
	f.rebuild()
	a := f.start(t, f.p, f.request())
	testReceive(t, entered)
	b := f.start(t, f.p, f.request())
	if e := f.c.Delete(context.Background(), f.p.ID); e != nil {
		t.Fatal(e)
	}
	for _, r := range []store.Run{a, b} {
		ev := f.event(t, "run.finished", r.ID)
		if ev.Attrs["status"] != store.StatusKilled {
			t.Fatal(ev)
		}
		if _, e := f.s.RunByID(context.Background(), r.ID); !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
	if _, e := os.Lstat(filepath.Join(f.cfg.Runs, f.p.ID)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	if e := f.c.Delete(context.Background(), f.p.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
}
func toolAnswer(w http.ResponseWriter, name string, input any) {
	w.Header().Set("Content-Type", "text/event-stream")
	frame(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]int{"input_tokens": 3}}})
	frame(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "call-one", "name": name, "input": map[string]any{}}})
	b, _ := json.Marshal(input)
	frame(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": string(b)}})
	frame(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	frame(w, "message_delta", map[string]any{"type": "message_delta", "usage": map[string]int{"output_tokens": 2}, "delta": map[string]string{"stop_reason": "tool_use"}})
	frame(w, "message_stop", map[string]string{"type": "message_stop"})
}
func readRecords(t *testing.T, path string) []agentkit.LogRecord {
	t.Helper()
	records := []agentkit.LogRecord{}
	for _, line := range bytes.Split(testRead(t, path), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var r agentkit.LogRecord
		if e := json.Unmarshal(line, &r); e != nil {
			t.Fatal(e)
		}
		records = append(records, r)
	}
	return records
}

// R-9FUZ-3PNN R-OL46-D11Z R-GY3K-6WJA R-OORV-ICA2 R-OPZR-W40R R-MQKN-6C2I R-OXB6-6QGX
func TestCoreEnvironmentSpecAndWork(t *testing.T) {
	f := newCoreFixture(t)
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Fatal(e)
	}
	f.cfg.Path = filepath.Dir(bash)
	f.cfg.MaxToolCalls = 3
	f.cfg.OutputMaxBytes = 4
	f.cfg.Cgroup = filepath.Join(t.TempDir(), "groups")
	if e = os.Mkdir(f.cfg.Cgroup, 0700); e != nil {
		t.Fatal(e)
	}
	socket := filepath.Join(t.TempDir(), "gateway.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	servicesJSON, _ := json.Marshal(map[string]any{"services": []any{map[string]any{"name": "mcp", "url": "http://mcp.test", "description": "test gateway", "socket": socket, "enabled": true, "mcp": true}}})
	if e = os.WriteFile(servicesPath, servicesJSON, 0600); e != nil {
		t.Fatal(e)
	}
	f.cfg.Services = servicesPath
	t.Setenv("IKIGENBA_SERVICES", "")
	gateway := mcp.NewServer(mcp.ServerConfig{Name: "gateway", Telemetry: f.w})
	gatewayCalls := make(chan identity.Caller, 1)
	gatewayCauses := make(chan events.Cause, 1)
	type gatewayIn struct {
		Value string `json:"value"`
	}
	type gatewayOut struct {
		Value string `json:"value"`
	}
	mcp.AddTool(gateway, mcp.Tool[gatewayIn, gatewayOut]{Name: "probe", Description: "Return the fixture value.", Effect: mcp.Read, Handler: func(ctx context.Context, caller identity.Caller, in gatewayIn) (gatewayOut, error) {
		gatewayCalls <- caller
		cause, _ := events.FromContext(ctx)
		gatewayCauses <- cause
		return gatewayOut(in), nil
	}})
	gatewayServer := &http.Server{ReadHeaderTimeout: time.Second, Handler: events.Middleware(identity.Require(gateway))}
	serveDone := make(chan error, 1)
	go func() { serveDone <- gatewayServer.Serve(listener) }()
	t.Cleanup(func() { _ = gatewayServer.Close(); <-serveDone })
	finalEntered, release := make(chan struct{}), make(chan struct{})
	round := 0
	providerServer := provider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != f.cfg.Keys[agentkit.HostAnthropic] {
			t.Errorf("key header %q", r.Header.Get("x-api-key"))
		}
		round++
		switch round {
		case 1:
			toolAnswer(w, "Write", map[string]any{"file_path": "nested/a/result.txt", "content": strings.Repeat("z", 64)})
		case 2:
			toolAnswer(w, "Bash", map[string]any{"command": `cat /proc/$$/environ > "$IKIGENBA_WORK_DIR/env"; printf '%s' "$PPID" > "$IKIGENBA_WORK_DIR/pid"; mkdir -p "$IKIGENBA_WORK_DIR/free/deep"; printf '%s' ok > "$IKIGENBA_WORK_DIR/free/deep/probe"; rm "$IKIGENBA_WORK_DIR/free/deep/probe"; rmdir "$IKIGENBA_WORK_DIR/free/deep"; :`})
		case 3:
			toolAnswer(w, "probe", map[string]any{"value": "gateway-value"})
		default:
			close(finalEntered)
			select {
			case <-release:
				answer(w, "ok")
			case <-r.Context().Done():
			}
		}
	})
	f.cfg.BaseURL = providerServer.URL
	f.rebuild()
	p := f.p
	p.Tools = []string{agent.GroupSuite, agent.GroupBash, agent.GroupFiles}
	p.System = "fixture-system"
	req := f.request()
	req.Cause = events.Cause{ID: "evt_0123456789abcdef", Depth: 2}
	r := f.start(t, p, req)
	testReceive(t, finalEntered)
	folder, e := filepath.Abs(f.c.Folder(r))
	if e != nil {
		t.Fatal(e)
	}
	env := map[string]string{}
	for _, item := range bytes.Split(testRead(t, filepath.Join(folder, WorkDir, "env")), []byte{0}) {
		if len(item) == 0 {
			continue
		}
		parts := strings.SplitN(string(item), "=", 2)
		if len(parts) != 2 {
			t.Fatal(string(item))
		}
		if _, ok := env[parts[0]]; ok {
			t.Fatal("duplicate env")
		}
		env[parts[0]] = parts[1]
	}
	testEqual(t, env, map[string]string{"PATH": f.cfg.Path, "HOME": folder, "LANG": "C.UTF-8", "IKIGENBA_RUN_ID": r.ID, "IKIGENBA_PROMPT": p.ID, "IKIGENBA_RUN_DIR": folder, "IKIGENBA_WORK_DIR": filepath.Join(folder, WorkDir), "IKIGENBA_INPUT": filepath.Join(folder, InputFile), "IKIGENBA_USER_ID": req.Caller.UserID, "IKIGENBA_REQUEST_ID": req.Caller.RequestID, "IKIGENBA_EVENT_ID": req.Cause.ID, "IKIGENBA_EVENT_DEPTH": "2", "IKIGENBA_SERVICES": servicesPath})
	group := filepath.Join(f.cfg.Cgroup, r.ID)
	testEqual(t, strings.TrimSpace(string(testRead(t, filepath.Join(group, "cgroup.procs")))), string(testRead(t, filepath.Join(folder, WorkDir, "pid"))))
	testEqual(t, strings.TrimSpace(string(testRead(t, filepath.Join(group, "memory.max")))), strconv.FormatInt(f.cfg.RunMemoryMaxBytes, 10))
	testEqual(t, strings.TrimSpace(string(testRead(t, filepath.Join(group, "pids.max")))), strconv.FormatInt(f.cfg.RunPidsMax, 10))
	testEqual(t, testReceive(t, gatewayCalls), req.Caller)
	testEqual(t, testReceive(t, gatewayCauses), req.Cause)
	testEqual(t, string(testRead(t, filepath.Join(folder, WorkDir, "nested/a/result.txt"))), strings.Repeat("z", 64))
	if _, e = os.Stat(filepath.Join(folder, WorkDir, "free/deep")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	close(release)
	f.event(t, "run.finished", r.ID)
	ended, e := f.s.RunByID(context.Background(), r.ID)
	if e != nil || ended.Status != store.StatusExited || ended.ExitCode != agent.ExitAnswered || ended.Truncated() {
		t.Fatal(ended, e)
	}
	if _, e = os.Lstat(group); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	transcriptPath := filepath.Join(folder, TranscriptFile)
	records := readRecords(t, transcriptPath)
	if len(testRead(t, transcriptPath)) <= 4 {
		t.Fatal("transcript bounded")
	}
	if records[0].Conversation.Limits.MaxToolCalls != 3 {
		t.Fatal(records[0])
	}
	names := []string{}
	for _, tool := range records[0].Conversation.Tools {
		names = append(names, tool.Name)
	}
	testEqual(t, names, []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash", "probe"})
	user, system := "", ""
	for _, rec := range records {
		if rec.Message != nil {
			for _, block := range rec.Message.Blocks {
				if text, ok := block.(agentkit.Text); ok {
					if rec.Message.Role == agentkit.RoleUser {
						user = text.Text
					}
					if rec.Message.Role == agentkit.RoleSystem {
						system = text.Text
					}
				}
			}
		}
	}
	expectedUser, e := agent.UserMessage(p.Prompt, req.Input)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, user, expectedUser)
	testEqual(t, system, p.System)
	key := f.cfg.Keys[agentkit.HostAnthropic]
	checkSecret := func(b []byte) {
		for i := 0; i+8 <= len(key); i++ {
			if bytes.Contains(b, []byte(key[i:i+8])) {
				t.Fatalf("key fragment in run output")
			}
		}
	}
	if e = filepath.WalkDir(folder, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			checkSecret(testRead(t, path))
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, ev := range f.sink.capture.Events() {
		b, _ := json.Marshal(ev)
		checkSecret(b)
	}
	// The output schema is handed over too, rather than inferred by the core.
	p.Tools = nil
	p.Schema = json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)
	schemaServer := provider(t, func(w http.ResponseWriter, _ *http.Request) { answer(w, `{"value":"ok"}`) })
	f.c.cfg.BaseURL = schemaServer.URL
	srun := f.start(t, p, f.request())
	f.event(t, "run.finished", srun.ID)
	sr := readRecords(t, filepath.Join(f.c.Folder(srun), TranscriptFile))
	testEqual(t, sr[0].Conversation.Output.Schema, p.Schema)
}

func checkRunCredentials(t *testing.T, f *coreFixture, r store.Run) {
	t.Helper()
	off, err := agent.Offering(r.Model)
	if err != nil {
		t.Fatal(err)
	}
	runKey := f.cfg.Keys[off.Host]
	check := func(b []byte) {
		for _, key := range f.cfg.Keys {
			if key != "" && bytes.Contains(b, []byte(key)) {
				t.Error("provider credential in run output")
			}
		}
		for i := 0; i+8 <= len(runKey); i++ {
			if bytes.Contains(b, []byte(runKey[i:i+8])) {
				t.Error("provider credential fragment in run output")
			}
		}
	}
	if err := filepath.WalkDir(f.c.Folder(r), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			check(testRead(t, path))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, ev := range f.sink.capture.Events() {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		check(b)
	}
}

// R-MQKN-6C2I
func TestCoreCredentialsAfterProviderFailure(t *testing.T) {
	f := newCoreFixture(t)
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Path = filepath.Dir(bash)
	f.cfg.OutputMaxBytes = 4096
	// Keep every host's configured credential distinct, including unused ones.
	f.cfg.Keys[agentkit.HostGemini] = "gT3!nB6%rV2@jC8&dW5?lS1+"
	f.cfg.Keys[agentkit.HostOpenAI] = "oM4!uD7%sL2@cT9&hR5?eN1+"
	f.cfg.Keys[agentkit.HostXAI] = "aJ6!fY3%wG8@kU2&nE9?qD4+"
	f.cfg.Keys[agentkit.HostOpenRouter] = "rV5!mH8%zK1@pF4&uB7?dA2+"
	requests := 0
	server := provider(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("x-api-key") != f.cfg.Keys[agentkit.HostAnthropic] {
			t.Error("provider did not receive credential")
		}
		if requests == 1 {
			toolAnswer(w, "Bash", map[string]any{"command": `cat /proc/$$/environ > "$IKIGENBA_WORK_DIR/env"; :`})
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"denied"}}`))
		}
	})
	f.cfg.BaseURL = server.URL
	f.rebuild()
	p := f.p
	p.Tools = []string{agent.GroupFiles, agent.GroupBash}
	r := f.start(t, p, f.request())
	f.event(t, "run.finished", r.ID)
	ended, err := f.s.RunByID(context.Background(), r.ID)
	if err != nil || ended.Status != store.StatusExited || ended.ExitCode != agent.ExitFailed || requests != 2 {
		t.Fatal(ended, err, requests)
	}
	if len(testRead(t, filepath.Join(f.c.Folder(r), WorkDir, "env"))) == 0 {
		t.Fatal("no environment probe")
	}
	checkRunCredentials(t, f, r)
}

// R-MQKN-6C2I
func TestCoreCredentialTransportFailure(t *testing.T) {
	models := map[agentkit.Host]string{}
	for _, entry := range agentkit.Catalog() {
		off, err := agent.Offering(entry.Model)
		if err == nil && models[off.Host] == "" {
			models[off.Host] = entry.Model
		}
	}
	if models[agentkit.HostGemini] == "" {
		t.Fatal("no Gemini offering")
	}
	for host, model := range models {
		t.Run(string(host), func(t *testing.T) {
			f := newCoreFixture(t)
			f.cfg.Keys = map[agentkit.Host]string{host: "aZ9!fixture-secret.Mixed_456"}
			f.cfg.OutputMaxBytes = 4096
			requested := make(chan struct{}, 8)
			server := provider(t, func(w http.ResponseWriter, r *http.Request) {
				found := false
				for _, values := range r.Header {
					for _, value := range values {
						found = found || strings.Contains(value, f.cfg.Keys[host])
					}
				}
				if !found {
					t.Error("provider did not receive credential")
				}
				requested <- struct{}{}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			})
			f.cfg.BaseURL = server.URL
			f.rebuild()
			p := f.p
			p.Model = model
			r := f.start(t, p, f.request())
			f.event(t, "run.finished", r.ID)
			testReceive(t, requested)
			ended, err := f.s.RunByID(context.Background(), r.ID)
			if err != nil || ended.Status != store.StatusExited || ended.ExitCode != agent.ExitFailed {
				t.Fatal(ended, err)
			}
			if len(testRead(t, filepath.Join(f.c.Folder(r), StderrFile))) == 0 {
				t.Fatal("missing transport diagnostic")
			}
			checkRunCredentials(t, f, r)
		})
	}
}

// R-PMX2-7X1I
func TestCoreConcurrentFolderIsolation(t *testing.T) {
	f := newCoreFixture(t)
	firstHeld, release := make(chan struct{}), make(chan struct{})
	var firstCalls int
	server := provider(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		b, _ := json.Marshal(body)
		if bytes.Contains(b, []byte("first-run")) {
			firstCalls++
			if firstCalls == 1 {
				toolAnswer(w, "Write", map[string]any{"file_path": "result.txt", "content": "first-file"})
			} else {
				close(firstHeld)
				select {
				case <-release:
					answer(w, "one")
				case <-r.Context().Done():
				}
			}
		} else {
			answer(w, "two")
		}
	})
	f.cfg.BaseURL = server.URL
	f.rebuild()
	p := f.p
	p.Tools = []string{agent.GroupFiles}
	req := f.request()
	req.Input = []byte(`{"who":"first-run"}`)
	a := f.start(t, p, req)
	testReceive(t, firstHeld)
	paths := []string{InputFile, StdoutFile, StderrFile, filepath.Join(WorkDir, "result.txt")}
	before := map[string][]byte{}
	for _, path := range paths {
		before[path] = testRead(t, filepath.Join(f.c.Folder(a), path))
	}
	req.Input = []byte(`{"who":"second-run"}`)
	b := f.start(t, p, req)
	if b.Status != store.StatusRunning {
		t.Fatal(b)
	}
	for _, path := range paths {
		testEqual(t, testRead(t, filepath.Join(f.c.Folder(a), path)), before[path])
	}
	f.event(t, "run.finished", b.ID)
	for _, path := range paths {
		testEqual(t, testRead(t, filepath.Join(f.c.Folder(a), path)), before[path])
	}
	close(release)
	f.event(t, "run.finished", a.ID)
}

// R-FDPH-KJZZ R-OTNH-1F8U R-OUVD-F6ZJ
func TestCoreExitStatuses(t *testing.T) {
	for _, mode := range []string{"failed", "limit", "no-output", "unusable", "signal", "exact"} {
		t.Run(mode, func(t *testing.T) {
			f := newCoreFixture(t)
			p := f.p
			round := 0
			server := provider(t, func(w http.ResponseWriter, _ *http.Request) {
				round++
				switch mode {
				case "failed":
					w.WriteHeader(http.StatusUnauthorized)
				case "limit":
					toolAnswer(w, "Glob", map[string]any{"pattern": "*"})
				case "no-output":
					answer(w, "invalid")
				case "signal":
					toolAnswer(w, "Bash", map[string]any{"command": `kill -TERM "$PPID"`})
				default:
					answer(w, "pong")
				}
			})
			f.cfg.BaseURL = server.URL
			f.cfg.OutputMaxBytes = 4
			switch mode {
			case "limit":
				p.Tools = []string{agent.GroupFiles}
			case "no-output":
				p.Schema = json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)
			case "unusable":
				p.Model = "missing-model"
			case "signal":
				bash, e := exec.LookPath("bash")
				if e != nil {
					t.Fatal(e)
				}
				f.cfg.Path = filepath.Dir(bash)
				p.Tools = []string{agent.GroupBash}
			}
			f.rebuild()
			r := f.start(t, p, f.request())
			f.event(t, "run.finished", r.ID)
			r, e := f.s.RunByID(context.Background(), r.ID)
			if e != nil {
				t.Fatal(e)
			}
			code := map[string]int{"failed": agent.ExitFailed, "limit": agent.ExitLimit, "no-output": agent.ExitNoOutput, "unusable": agent.ExitUnusable, "signal": 143, "exact": agent.ExitAnswered}[mode]
			if r.Status != store.StatusExited || r.ExitCode != code {
				t.Fatal(r)
			}
			if mode == "exact" {
				if r.StdoutBytes != 4 || r.StdoutTruncated || r.StderrTruncated {
					t.Fatal(r)
				}
				testEqual(t, string(testRead(t, filepath.Join(f.c.Folder(r), StdoutFile))), "pong")
			} else if mode != "signal" {
				if r.StdoutBytes != 0 || r.StderrBytes != 4 || !r.StderrTruncated || r.StdoutTruncated {
					t.Fatal(r)
				}
			}
		})
	}
}

// R-G5R6-DA1Y
func TestCoreFailedFinishHasNoEvent(t *testing.T) {
	f := newCoreFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server := provider(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
			answer(w, "done")
		case <-r.Context().Done():
		}
	})
	f.cfg.BaseURL = server.URL
	f.rebuild()
	r := f.start(t, f.p, f.request())
	testReceive(t, entered)
	f.d.SetFailing(true)
	close(release)
	f.c.Drain(context.Background())
	f.d.SetFailing(false)
	r, e := f.s.RunByID(context.Background(), r.ID)
	if e != nil || r.Status != store.StatusRunning {
		t.Fatal(r, e)
	}
	if e = f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, ev := range f.sink.capture.Events() {
		if ev.Name == "run.finished" && ev.Attrs["prompt_run"] == r.ID {
			t.Fatal(ev)
		}
	}
	if e = f.c.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.event(t, "run.finished", r.ID)
}

// R-QHEJ-S6KV
func TestCoreMethodsKeepOwnStreamsQuiet(t *testing.T) {
	f := newCoreFixture(t)
	out, e := os.Create(filepath.Join(t.TempDir(), "out"))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = out.Close() }()
	errFile, e := os.Create(filepath.Join(t.TempDir(), "err"))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = errFile.Close() }()
	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errFile
	defer func() { os.Stdout, os.Stderr = originalOut, originalErr }()
	server := provider(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	f.cfg.BaseURL = server.URL
	f.rebuild()
	r := f.start(t, f.p, f.request())
	f.event(t, "run.finished", r.ID)
	if e = f.c.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = f.c.Prune(context.Background()); e != nil {
		t.Fatal(e)
	}
	_, _ = f.c.Cancel(context.Background(), r.ID)
	if e = f.c.Delete(context.Background(), f.p.ID); e != nil {
		t.Fatal(e)
	}
	f.c.Drain(context.Background())
	for _, file := range []*os.File{out, errFile} {
		info, e := file.Stat()
		if e != nil || info.Size() != 0 {
			t.Fatal(info, e)
		}
	}
}

// R-FUS2-XCDP
func TestCancelReturnsEndingWhenPruneRemovesIt(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.KeepDays = 1
	f.cfg.KeepCount = 1
	var mu sync.Mutex
	now := testInstant
	f.cfg.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	entered := make(chan struct{})
	server := provider(t, func(_ http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() })
	f.cfg.BaseURL = server.URL
	f.rebuild()
	run := f.start(t, f.p, f.request())
	testReceive(t, entered)
	newer, e := f.s.AddRun(context.Background(), store.Run{ID: "prr_fefefefefefefefe", Prompt: f.p.ID, Model: f.p.Model, User: f.p.Owner, RequestID: "newer", Trigger: store.TriggerManual, Status: store.StatusFailed, Reason: store.ReasonStartFailed, Started: testInstant.Add(time.Hour), Finished: testInstant.Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(f.c.Folder(newer), 0700); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	now = testInstant.Add(48 * time.Hour)
	mu.Unlock()
	ended, e := f.c.Cancel(context.Background(), run.ID)
	if e != nil || ended.ID != run.ID || ended.Status != store.StatusKilled || ended.ExitCode != 0 {
		t.Fatal(ended, e)
	}
	testEqual(t, ended.Finished, testInstant.Add(48*time.Hour).Truncate(time.Second))
	finished := f.event(t, "run.finished", run.ID)
	testEqual(t, finished.Attrs, FinishedAttrs(ended, 48*time.Hour))
	if _, e = f.s.RunByID(context.Background(), run.ID); !errors.Is(e, store.ErrNotFound) || !f.c.Gone(run) {
		t.Fatal("cancelled run was not pruned", e)
	}
}

// R-FEXD-YBQO
func TestCoreBackwardEndingClock(t *testing.T) {
	f := newCoreFixture(t)
	var mu sync.Mutex
	now := testInstant
	f.cfg.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	entered, release := make(chan struct{}), make(chan struct{})
	server := provider(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
			answer(w, "done")
		case <-r.Context().Done():
		}
	})
	f.cfg.BaseURL = server.URL
	f.rebuild()
	r := f.start(t, f.p, f.request())
	testReceive(t, entered)
	mu.Lock()
	now = testInstant.Add(-time.Hour)
	mu.Unlock()
	close(release)
	ev := f.event(t, "run.finished", r.ID)
	ended, e := f.s.RunByID(context.Background(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, ended.Finished, r.Started)
	testEqual(t, ev.Attrs, FinishedAttrs(ended, 0))
}

// R-PO4Y-LOS7 R-OA52-X3DQ
func TestFailedEndingsPruneBeforeEvent(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprint(queued), func(t *testing.T) {
			f := newCoreFixture(t)
			f.cfg.KeepDays = 1
			f.cfg.KeepCount = 1
			f.cfg.MaxActive = 1
			calls := 0
			f.cfg.Now = func() time.Time {
				calls++
				if queued && calls >= 5 {
					return testInstant.Add(48 * time.Hour)
				}
				return testInstant
			}
			entered, release := make(chan struct{}), make(chan struct{})
			server := provider(t, func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
					answer(w, "done")
				case <-r.Context().Done():
				}
			})
			f.cfg.BaseURL = server.URL
			f.rebuild()
			oldTime := testInstant.Add(-48 * time.Hour)
			if queued {
				oldTime = testInstant.Add(-12 * time.Hour)
			}
			old, e := f.s.AddRun(context.Background(), store.Run{ID: "prr_fefefefefefefefe", Prompt: f.p.ID, Model: f.p.Model, User: f.p.Owner, RequestID: "old", Trigger: store.TriggerManual, Status: store.StatusFailed, Reason: store.ReasonStartFailed, Started: oldTime, Finished: oldTime})
			if e != nil {
				t.Fatal(e)
			}
			if e = os.MkdirAll(f.c.Folder(old), 0700); e != nil {
				t.Fatal(e)
			}
			if queued {
				a := f.start(t, f.p, f.request())
				testReceive(t, entered)
				_ = a
			}
			req := f.request()
			req.Input = []byte("{x")
			r := f.start(t, f.p, req)
			if queued {
				if r.Status != store.StatusQueued {
					t.Fatal(r)
				}
				close(release)
			} else if r.Status != store.StatusFailed {
				t.Fatal(r)
			}
			f.event(t, "run.finished", r.ID)
			if _, e = f.s.RunByID(context.Background(), old.ID); !errors.Is(e, store.ErrNotFound) || !f.c.Gone(old) {
				t.Fatal("failed ending's prune did not finish", e)
			}
			if e = f.w.Flush(context.Background()); e != nil {
				t.Fatal(e)
			}
			seen := 0
			for _, ev := range f.sink.capture.Events() {
				if ev.Attrs["prompt_run"] == r.ID {
					if ev.Name != "run.finished" {
						t.Fatal(ev)
					}
					seen++
				}
			}
			if seen != 1 {
				t.Fatal(seen)
			}
		})
	}
}

// R-GY3K-6WJA R-OORV-ICA2 R-OPZR-W40R R-EYEO-8L0I R-M97H-3GOR
func TestQueuedSnapshotManualRelativeRun(t *testing.T) {
	f := newCoreFixture(t)
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Fatal(e)
	}
	f.cfg.Path = filepath.Dir(bash)
	cwd, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	f.cfg.Runs, e = filepath.Rel(cwd, f.cfg.Runs)
	if e != nil {
		t.Fatal(e)
	}
	f.cfg.MaxActive = 1
	f.cfg.Keys = nil
	f.cfg.MaxToolCalls = math.MaxInt64
	timerDurations := make(chan time.Duration, 3)
	f.cfg.ScriptAfter = func(d time.Duration) <-chan time.Time {
		timerDurations <- d
		ch := make(chan time.Time)
		f.timers <- ch
		return ch
	}
	var clockMu sync.Mutex
	now := testInstant
	f.cfg.Now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	entered, release := make(chan struct{}), make(chan struct{})
	round := 0
	server := provider(t, func(w http.ResponseWriter, r *http.Request) {
		b, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Error(readErr)
		}
		if r.Header.Get("x-api-key") != "" {
			t.Error("missing key was replaced")
		}
		if bytes.Contains(b, []byte("queue-blocker")) {
			close(entered)
			select {
			case <-release:
				answer(w, "done")
			case <-r.Context().Done():
			}
			return
		}
		round++
		switch round {
		case 1:
			toolAnswer(w, "Write", map[string]any{"file_path": "probe", "content": "snapshot-file"})
		case 2:
			toolAnswer(w, "Bash", map[string]any{"command": `cat /proc/$$/environ > "$IKIGENBA_WORK_DIR/env"; :`})
		default:
			answer(w, `{"value":"ok"}`)
		}
	})
	f.cfg.BaseURL = server.URL
	f.rebuild()
	blockReq := f.request()
	blockReq.Input = []byte(`{"who":"queue-blocker"}`)
	a := f.start(t, f.p, blockReq)
	testReceive(t, entered)
	p := f.p
	p.Prompt = "snapshot-prompt"
	p.System = "snapshot-system"
	p.Tools = []string{agent.GroupFiles, agent.GroupBash}
	p.Schema = json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)
	schema := bytes.Clone(p.Schema)
	req := f.request()
	req.Input = []byte(` {"who":"snapshot-input"} `)
	input := bytes.Clone(req.Input)
	queued := f.start(t, p, req)
	if queued.Status != store.StatusQueued || len(f.timers) != 1 {
		t.Fatal(queued, len(f.timers))
	}
	model, text, system := "missing-model", "changed-prompt", "changed-system"
	tools := []string{}
	changedSchema := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	if _, _, e = f.s.Update(context.Background(), p.ID, store.Change{Model: &model, Prompt: &text, System: &system, Tools: &tools, Schema: &changedSchema}); e != nil {
		t.Fatal(e)
	}
	p.Tools[0] = "invalid-group"
	for i := range p.Schema {
		p.Schema[i] = ' '
	}
	for i := range req.Input {
		req.Input[i] = ' '
	}
	clockMu.Lock()
	now = testInstant.Add(2 * time.Second)
	clockMu.Unlock()
	close(release)
	f.event(t, "run.finished", a.ID)
	started := f.event(t, "run.started", queued.ID)
	testEqual(t, started.Attrs, StartedAttrs(queued))
	ev := f.event(t, "run.finished", queued.ID)
	ended, e := f.s.RunByID(context.Background(), queued.ID)
	if e != nil || ended.Status != store.StatusExited || ended.ExitCode != agent.ExitAnswered {
		t.Fatal(ended, e)
	}
	testEqual(t, ended.Started, testInstant.Truncate(time.Second))
	testEqual(t, ended.Finished, testInstant.Add(2*time.Second).Truncate(time.Second))
	testEqual(t, ev.Attrs, FinishedAttrs(ended, 2*time.Second))
	if len(f.timers) != 2 {
		t.Fatal("wrong timer count", len(f.timers))
	}
	for range 2 {
		testEqual(t, testReceive(t, timerDurations), 2*time.Second)
	}
	folder, e := filepath.Abs(f.c.Folder(queued))
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, testRead(t, filepath.Join(folder, InputFile)), input)
	testEqual(t, string(testRead(t, filepath.Join(folder, WorkDir, "probe"))), "snapshot-file")
	env := map[string]string{}
	for _, item := range bytes.Split(testRead(t, filepath.Join(folder, WorkDir, "env")), []byte{0}) {
		if len(item) == 0 {
			continue
		}
		parts := strings.SplitN(string(item), "=", 2)
		if len(parts) != 2 {
			t.Fatal(string(item))
		}
		if _, duplicate := env[parts[0]]; duplicate {
			t.Fatal("duplicate environment variable")
		}
		env[parts[0]] = parts[1]
	}
	testEqual(t, env, map[string]string{"PATH": f.cfg.Path, "HOME": folder, "LANG": "C.UTF-8", "IKIGENBA_RUN_ID": queued.ID, "IKIGENBA_PROMPT": p.ID, "IKIGENBA_RUN_DIR": folder, "IKIGENBA_WORK_DIR": filepath.Join(folder, WorkDir), "IKIGENBA_INPUT": filepath.Join(folder, InputFile), "IKIGENBA_USER_ID": req.Caller.UserID, "IKIGENBA_REQUEST_ID": req.Caller.RequestID, "IKIGENBA_EVENT_ID": "", "IKIGENBA_EVENT_DEPTH": "0", "IKIGENBA_SERVICES": f.cfg.Services})
	records := readRecords(t, filepath.Join(folder, TranscriptFile))
	testEqual(t, records[0].Conversation.Limits.MaxToolCalls, math.MaxInt)
	testEqual(t, records[0].Conversation.Output.Schema, json.RawMessage(schema))
	names := []string{}
	for _, tool := range records[0].Conversation.Tools {
		names = append(names, tool.Name)
	}
	testEqual(t, names, []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash"})
	user, sys := "", ""
	for _, record := range records {
		if record.Message == nil {
			continue
		}
		for _, block := range record.Message.Blocks {
			if text, ok := block.(agentkit.Text); ok {
				switch record.Message.Role {
				case agentkit.RoleUser:
					user = text.Text
				case agentkit.RoleSystem:
					sys = text.Text
				}
			}
		}
	}
	expectedUser, e := agent.UserMessage("snapshot-prompt", input)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, user, expectedUser)
	testEqual(t, sys, "snapshot-system")
	if e = f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	finishIndex, startIndex := -1, -1
	for i, event := range f.sink.capture.Events() {
		if event.Name == "run.finished" && event.Attrs["prompt_run"] == a.ID {
			finishIndex = i
		}
		if event.Name == "run.started" && event.Attrs["prompt_run"] == queued.ID {
			startIndex = i
		}
	}
	if finishIndex < 0 || startIndex <= finishIndex {
		t.Fatal("queue start did not follow preceding finish", finishIndex, startIndex)
	}
}

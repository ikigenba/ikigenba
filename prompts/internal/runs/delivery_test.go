package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

func deliveryFixture(t *testing.T) (*coreFixture, context.Context, events.Delivery) {
	t.Helper()
	f := newCoreFixture(t)
	if _, e := f.s.Subscribe(context.Background(), f.p.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	ctx := identity.NewContext(context.Background(), identity.Caller{RequestID: "delivery-request"})
	d := events.Delivery{Event: events.Event{ID: "evt_0123456789abcdef", Time: testInstant, Service: "repos", Name: "repo.pushed", RequestID: "producer-request", User: "producer", Attrs: events.Attrs{"repo": "fixture"}, Cause: "evt_abcdef0123456789", Depth: 2, Seq: 3, Received: testInstant}, Attempt: 1}
	return f, ctx, d
}
func deliveryRuns(t *testing.T, f *coreFixture, p store.Prompt) []store.Run {
	t.Helper()
	rs, e := f.s.Runs(context.Background(), p.ID)
	if e != nil {
		t.Fatal(e)
	}
	return rs
}
func deliveryNoWork(t *testing.T, f *coreFixture) {
	t.Helper()
	testEqual(t, len(deliveryRuns(t, f, f.p)), 0)
	es, e := os.ReadDir(f.cfg.Runs)
	if e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	testEqual(t, len(es), 0)
	testEqual(t, len(f.timers), 0)
	for _, e := range f.sink.capture.Events() {
		if strings.HasPrefix(e.Name, "run.") {
			t.Fatalf("unexpected run event: %#v", e)
		}
	}
}
func deliveryHeldProvider(t *testing.T, f *coreFixture) <-chan struct{} {
	t.Helper()
	release := make(chan struct{})
	entered := make(chan struct{}, 16)
	s := provider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		entered <- struct{}{}
		select {
		case <-release:
			answer(w, "answer")
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	f.cfg.BaseURL = s.URL
	f.rebuild()
	return entered
}

// R-IO3F-IFR8 R-IPBB-W7HX R-IQJ8-9Z8M R-ISZ1-1IQ0
func TestDeliveryEarlyRefusals(t *testing.T) {
	for _, mode := range []string{"drain", "empty", "catalog", "irrelevant"} {
		t.Run(mode, func(t *testing.T) {
			f, ctx, d := deliveryFixture(t)
			want := events.Skip()
			switch mode {
			case "drain":
				f.c.Drain(context.Background())
				f.d.SetFailing(true)
				d.Event.ID = ""
				want = events.Fail(Stopping)
			case "empty":
				f.d.SetFailing(true)
				d.Event.ID = ""
				want = events.Fail(NoEventID)
			case "catalog":
				f.d.SetFailing(true)
				want = events.Fail(store.Unreachable)
			case "irrelevant":
				d.Event.Name = "repo.created"
			}
			testEqual(t, f.c.Deliver(ctx, d), want)
			f.d.SetFailing(false)
			deliveryNoWork(t, f)
			if mode == "catalog" {
				deliveryHeldProvider(t, f)
				testEqual(t, f.c.Deliver(ctx, d), events.OK())
				testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
			}
		})
	}
}

// R-IU6X-FAGP
func TestDeliveryUnavailable(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	f.cfg.Unavailable = "fixture control group"
	f.rebuild()
	testEqual(t, f.c.Deliver(ctx, d), events.Fail(fmt.Sprintf(NoRuns, f.cfg.Unavailable)))
	deliveryNoWork(t, f)
	other := d
	other.Event.Name = "repo.created"
	testEqual(t, f.c.Deliver(ctx, other), events.Skip())
	f.cfg.Unavailable = ""
	deliveryHeldProvider(t, f)
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	f.c.cfg.Unavailable = "fixture control group"
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
}

// R-ILNM-QW9U R-IMVJ-4O0J R-IWMQ-6TY3 R-4IGI-1S62 R-IXUM-KLOS
func TestDeliveryRecordsCanonicalInputBeforeAnswer(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	model := ""
	for _, entry := range agentkit.Catalog() {
		o, e := agent.Offering(entry.Model)
		if e == nil && o.Host == agentkit.HostAnthropic && entry.Model != f.p.Model {
			model = entry.Model
			break
		}
	}
	if model == "" {
		t.Fatal("no second Anthropic model")
	}
	p, e := f.s.Create(ctx, store.Draft{Owner: "bob", Name: "two", Model: model, Prompt: "second"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Subscribe(ctx, p.ID, "repo.*"); e != nil {
		t.Fatal(e)
	}
	requests := make(chan map[string]any, 2)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			t.Error(e)
		}
		requests <- v
		select {
		case <-release:
			answer(w, "answer")
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(srv.CloseClientConnections)
	t.Cleanup(func() { close(release) })
	f.cfg.BaseURL = srv.URL
	f.rebuild()
	var handler events.Handler = f.c.Deliver
	handlers := events.Handlers{"*": handler}
	testEqual(t, handlers["*"](ctx, d), events.OK())
	b, e := d.Event.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var canonical map[string]any
	if e = json.Unmarshal(b, &canonical); e != nil {
		t.Fatal(e)
	}
	testEqual(t, len(canonical), 11)
	expected := map[string]bool{}
	for _, p := range []store.Prompt{f.p, p} {
		rs := deliveryRuns(t, f, p)
		testEqual(t, len(rs), 1)
		r := rs[0]
		testEqual(t, r.Prompt, p.ID)
		testEqual(t, r.Model, p.Model)
		testEqual(t, r.Trigger, store.TriggerEvent)
		testEqual(t, r.Event, d.Event.ID)
		testEqual(t, r.User, p.Owner)
		testEqual(t, r.RequestID, "delivery-request")
		testEqual(t, r.Started, testInstant.UTC().Truncate(time.Second))
		testEqual(t, r.Status, store.StatusRunning)
		testEqual(t, testRead(t, filepath.Join(f.c.Folder(r), InputFile)), b)
		v, e := agent.UserMessage(p.Prompt, b)
		if e != nil {
			t.Fatal(e)
		}
		expected[v] = true
	}
	for i := 0; i < 2; i++ {
		v := testReceive(t, requests)
		messages := v["messages"].([]any)
		first := messages[0].(map[string]any)
		content := first["content"].([]any)
		text := content[0].(map[string]any)["text"].(string)
		if !expected[text] {
			t.Fatalf("unexpected user message: %q", text)
		}
		delete(expected, text)
	}
	testEqual(t, len(expected), 0)
}

// R-IWMQ-6TY3 R-IXUM-KLOS R-CIR5-JXMC
func TestDeliveryRecordedStartFailure(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	f.cfg.Cgroup = filepath.Join(t.TempDir(), "absent")
	f.rebuild()
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	rs := deliveryRuns(t, f, f.p)
	testEqual(t, len(rs), 1)
	testEqual(t, rs[0].Status, store.StatusFailed)
	testEqual(t, rs[0].Reason, store.ReasonStartFailed)
	testEqual(t, rs[0].Event, d.Event.ID)
	b, e := d.Event.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, testRead(t, filepath.Join(f.c.Folder(rs[0]), InputFile)), b)
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
}

// R-IVET-T27E R-J1IB-PWWV
func TestDeliverySharesQueueAndCapacity(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	f.cfg.MaxActive = 1
	f.cfg.MaxQueued = 1
	entered := deliveryHeldProvider(t, f)
	first := f.start(t, f.p, f.request())
	testReceive(t, entered)
	queued := f.start(t, f.p, f.request())
	testEqual(t, queued.Status, store.StatusQueued)
	testEqual(t, f.c.Deliver(ctx, d), events.Fail(fmt.Sprintf(QueueFull, f.cfg.MaxQueued)))
	testEqual(t, len(deliveryRuns(t, f, f.p)), 2)
	if _, e := f.c.Cancel(ctx, queued.ID); e != nil {
		t.Fatal(e)
	}
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	var eventRun store.Run
	for _, r := range deliveryRuns(t, f, f.p) {
		if r.Event == d.Event.ID {
			eventRun = r
		}
	}
	testEqual(t, eventRun.Status, store.StatusQueued)
	if _, e := f.c.Cancel(ctx, first.ID); e != nil {
		t.Fatal(e)
	}
	testReceive(t, entered)
	started := f.event(t, "run.started", eventRun.ID)
	testEqual(t, started.User, f.p.Owner)
	testEqual(t, started.RequestID, "delivery-request")
	testEqual(t, started.Attrs["trigger"], store.TriggerEvent)
	testReceive(t, f.timers)
	timer := testReceive(t, f.timers)
	timer <- testInstant
	finished := f.event(t, "run.finished", eventRun.ID)
	testEqual(t, finished.RequestID, "delivery-request")
	testEqual(t, finished.User, f.p.Owner)
	r, e := f.s.RunByID(ctx, eventRun.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, r.Status, store.StatusTimedOut)
}

// R-J2Q8-3ONK
func TestDeliveryRedeliveryAfterEndPruneAndRestart(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	entered := deliveryHeldProvider(t, f)
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testReceive(t, entered)
	r := deliveryRuns(t, f, f.p)[0]
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
	testEqual(t, len(f.timers), 1)
	if _, e := f.c.Cancel(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
	if e := removeTree(f.c.Folder(r)); e != nil {
		t.Fatal(e)
	}
	if e := f.s.DeleteRun(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	f.rebuild()
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, f.p)), 0)
	if _, e := os.Stat(f.c.Folder(r)); !os.IsNotExist(e) {
		t.Fatalf("folder recreated: %v", e)
	}
	testEqual(t, len(f.timers), 1)
}

type deliveryBlockRand struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	counter testCounter
}

func (b *deliveryBlockRand) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered); <-b.release })
	return b.counter.Read(p)
}
func deliveryBlock(t *testing.T, f *coreFixture) *deliveryBlockRand {
	t.Helper()
	b := &deliveryBlockRand{entered: make(chan struct{}), release: make(chan struct{})}
	f.cfg.Rand = b
	f.rebuild()
	return b
}

// R-CJZ1-XPD1 R-4JOE-FJWR
func TestDeliveryConcurrentAttempts(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	deliveryHeldProvider(t, f)
	b := deliveryBlock(t, f)
	done := make(chan events.Outcome, 1)
	go func() { done <- f.c.Deliver(ctx, d) }()
	testReceive(t, b.entered)
	testEqual(t, f.c.Deliver(ctx, d), events.Fail(Starting))
	testEqual(t, len(deliveryRuns(t, f, f.p)), 0)
	close(b.release)
	testEqual(t, testReceive(t, done), events.OK())
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
	testEqual(t, len(f.timers), 1)
}

// R-JB9I-S2UF
func TestDeliveryIgnoresCancellation(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(fmt.Sprint(before), func(t *testing.T) {
			f, ctx, d := deliveryFixture(t)
			deliveryHeldProvider(t, f)
			b := deliveryBlock(t, f)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if before {
				cancel()
			}
			done := make(chan events.Outcome, 1)
			go func() { done <- f.c.Deliver(ctx, d) }()
			testReceive(t, b.entered)
			cancel()
			close(b.release)
			testEqual(t, testReceive(t, done), events.OK())
			testEqual(t, deliveryRuns(t, f, f.p)[0].Status, store.StatusRunning)
		})
	}
}

// R-4JOE-FJWR
func TestDeliveryCatalogFailureRetainsEarlierRuns(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	deliveryHeldProvider(t, f)
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	first := deliveryRuns(t, f, f.p)[0]
	p, e := f.s.Create(ctx, store.Draft{Owner: "bob", Name: "two", Model: f.p.Model, Prompt: "two"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Subscribe(ctx, p.ID, d.Event.Name); e != nil {
		t.Fatal(e)
	}
	b := &deliveryBlockRand{entered: make(chan struct{}), release: make(chan struct{})}
	f.c.cfg.Rand = b
	done := make(chan events.Outcome, 1)
	go func() { done <- f.c.Deliver(ctx, d) }()
	testReceive(t, b.entered)
	f.d.SetFailing(true)
	close(b.release)
	testEqual(t, testReceive(t, done), events.Fail(store.Unreachable))
	f.d.SetFailing(false)
	testEqual(t, deliveryRuns(t, f, f.p)[0], first)
	testEqual(t, len(deliveryRuns(t, f, p)), 0)
	es, e := os.ReadDir(filepath.Join(f.cfg.Runs, p.ID))
	if e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	testEqual(t, len(es), 0)
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, p)), 1)
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
}

// R-4JOE-FJWR
func TestDeliveryDrainCutsMaking(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	b := deliveryBlock(t, f)
	done := make(chan events.Outcome, 1)
	go func() { done <- f.c.Deliver(ctx, d) }()
	testReceive(t, b.entered)
	drainCtx, cancel := context.WithCancel(ctx)
	cancel()
	drained := make(chan struct{})
	go func() { f.c.Drain(drainCtx); close(drained) }()
	for {
		f.c.mu.Lock()
		stopping := f.c.draining
		changed := f.c.changed
		f.c.mu.Unlock()
		if stopping {
			break
		}
		testReceive(t, changed)
	}
	close(b.release)
	testEqual(t, testReceive(t, done), events.Fail(Stopping))
	testReceive(t, drained)
	deliveryNoWork(t, f)
}

// R-JDPB-JMBT
func TestDeliveryMatchesWholeWordsOnce(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	if _, e := f.s.Unsubscribe(ctx, f.p.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	for _, pattern := range []string{"cron.*.fired", "cron.hourly.fired"} {
		if _, e := f.s.Subscribe(ctx, f.p.ID, pattern); e != nil {
			t.Fatal(e)
		}
	}
	other, e := f.s.Create(ctx, store.Draft{Owner: "bob", Name: "two", Model: f.p.Model, Prompt: "two"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Subscribe(ctx, other.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	deliveryHeldProvider(t, f)
	d.Event.Name = "cron.hourly.fired"
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
	testEqual(t, len(deliveryRuns(t, f, other)), 0)
	b, e := d.Event.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	r := deliveryRuns(t, f, f.p)[0]
	testEqual(t, testRead(t, filepath.Join(f.c.Folder(r), InputFile)), b)
	for _, name := range []string{"cron.fired", "cron.a.b.fired"} {
		d.Event.Name = name
		testEqual(t, f.c.Deliver(ctx, d), events.Skip())
	}
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
}

// R-IZ2I-YDFH
func TestDeliveryEnvironment(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Fatal(e)
	}
	f.cfg.Path = filepath.Dir(bash)
	tools := []string{agent.GroupBash}
	p, _, e := f.s.Update(ctx, f.p.ID, store.Change{Tools: &tools})
	if e != nil {
		t.Fatal(e)
	}
	f.p = p
	f.cfg.BaseURL = provider(t, func(w http.ResponseWriter, _ *http.Request) {
		toolAnswer(w, "Bash", map[string]any{"command": "printf '%s|%s|%s|%s' \"$IKIGENBA_USER_ID\" \"$IKIGENBA_REQUEST_ID\" \"$IKIGENBA_EVENT_ID\" \"$IKIGENBA_EVENT_DEPTH\""})
	}).URL
	f.rebuild()
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	r := deliveryRuns(t, f, f.p)[0]
	f.event(t, "run.finished", r.ID)
	transcript := testRead(t, filepath.Join(f.c.Folder(r), TranscriptFile))
	if !bytes.Contains(transcript, []byte(f.p.Owner+"|delivery-request|"+d.Event.ID+"|2")) {
		t.Fatalf("environment result missing: %s", transcript)
	}
}

// R-J0AF-C566
func TestDeliverySuiteIdentityAndCause(t *testing.T) {
	for _, email := range []string{"alice@example.test", ""} {
		t.Run(fmt.Sprint(email != ""), func(t *testing.T) {
			f, ctx, d := deliveryFixture(t)
			if e := f.s.Delete(ctx, f.p.ID); e != nil {
				t.Fatal(e)
			}
			p, e := f.s.Create(ctx, store.Draft{Owner: f.p.Owner, OwnerEmail: email, Name: f.p.Name, Model: f.p.Model, Prompt: f.p.Prompt, Tools: []string{agent.GroupSuite}})
			if e != nil {
				t.Fatal(e)
			}
			f.p = p
			if _, e = f.s.Subscribe(ctx, p.ID, d.Event.Name); e != nil {
				t.Fatal(e)
			}
			dir, e := os.MkdirTemp("", "delivery-gateway-")
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				if e := os.RemoveAll(dir); e != nil {
					t.Error(e)
				}
			})
			socket := filepath.Join(dir, "mcp.sock")
			listener, e := net.Listen("unix", socket)
			if e != nil {
				t.Fatal(e)
			}
			headers := make(chan http.Header, 16)
			invoked := make(chan struct{}, 1)
			t.Setenv("IKIGENBA_SERVICES", "")
			gateway := mcp.NewServer(mcp.ServerConfig{Name: "fixture", Telemetry: f.w})
			type fixtureIn struct {
				Value string `json:"value"`
			}
			type fixtureOut struct {
				Value string `json:"value"`
			}
			mcp.AddTool(gateway, mcp.Tool[fixtureIn, fixtureOut]{Name: "echo", Description: "Return fixture value.", Effect: mcp.Read, Handler: func(_ context.Context, _ identity.Caller, _ fixtureIn) (fixtureOut, error) {
				invoked <- struct{}{}
				return fixtureOut{Value: "ok"}, nil
			}})

			srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				headers <- r.Header.Clone()
				events.Middleware(identity.Require(gateway)).ServeHTTP(w, r)
			}), ReadHeaderTimeout: time.Second}
			go func() { _ = srv.Serve(listener) }()
			t.Cleanup(func() { _ = srv.Close() })
			f.cfg.Services = filepath.Join(dir, "services.json")
			if e = os.WriteFile(f.cfg.Services, deliveryJSON(t, map[string]any{"services": []any{map[string]any{"name": "mcp", "url": "http://mcp", "description": "Fixture gateway.", "socket": socket, "enabled": true, "mcp": true}}}), 0600); e != nil {
				t.Fatal(e)
			}
			f.cfg.BaseURL = provider(t, func(w http.ResponseWriter, _ *http.Request) { toolAnswer(w, "echo", map[string]any{}) }).URL
			f.rebuild()
			testEqual(t, f.c.Deliver(ctx, d), events.OK())
			r := deliveryRuns(t, f, f.p)[0]
			f.event(t, "run.finished", r.ID)
			testReceive(t, invoked)
			if len(headers) < 2 {
				t.Fatalf("gateway requests=%d stderr=%s", len(headers), testRead(t, filepath.Join(f.c.Folder(r), StderrFile)))
			}
			for i := 0; i < 2; i++ {
				h := testReceive(t, headers)
				testEqual(t, h.Get("X-User-Id"), f.p.Owner)
				testEqual(t, h.Get("X-User-Email"), email)
				testEqual(t, h.Get("X-Request-Id"), "delivery-request")
				testEqual(t, h.Get("X-Event-Cause"), d.Event.ID)
				testEqual(t, h.Get("X-Event-Depth"), "2")
			}
		})
	}
}
func deliveryJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

// R-J1IB-PWWV R-J2Q8-3ONK
func TestDeliveryLifecycleBoundsUsageAndPruning(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	f.cfg.OutputMaxBytes = 4
	f.cfg.BaseURL = provider(t, func(w http.ResponseWriter, _ *http.Request) { answer(w, "answer longer than limit") }).URL
	f.rebuild()
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	r := deliveryRuns(t, f, f.p)[0]
	started := f.event(t, "run.started", r.ID)
	testEqual(t, started.Attrs, StartedAttrs(r))
	testEqual(t, started.RequestID, "delivery-request")
	testEqual(t, started.User, f.p.Owner)
	finished := f.event(t, "run.finished", r.ID)
	ended, e := f.s.RunByID(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, ended.Status, store.StatusExited)
	testEqual(t, ended.ExitCode, agent.ExitAnswered)
	testEqual(t, ended.StdoutBytes, int64(4))
	testEqual(t, ended.StdoutTruncated, true)
	testEqual(t, string(testRead(t, filepath.Join(f.c.Folder(r), StdoutFile))), "answ")
	testEqual(t, ended.Usage.Calls, int64(1))
	testEqual(t, ended.Usage.InputTokens, int64(3))
	testEqual(t, ended.Usage.OutputTokens, int64(2))
	if ended.Usage.CostNanos <= 0 {
		t.Fatal("missing usage cost")
	}
	testEqual(t, finished.RequestID, "delivery-request")
	testEqual(t, finished.User, f.p.Owner)
	testEqual(t, finished.Attrs, FinishedAttrs(ended, 0))
	f.c.cfg.KeepCount = 1
	second := d
	second.Event.ID = "evt_1123456789abcdef"
	testEqual(t, f.c.Deliver(ctx, second), events.OK())
	var next store.Run
	for _, candidate := range deliveryRuns(t, f, f.p) {
		if candidate.Event == second.Event.ID {
			next = candidate
		}
	}
	f.event(t, "run.finished", next.ID)
	f.c.mu.Lock()
	f.c.cfg.Now = func() time.Time { return testInstant.Add(31 * 24 * time.Hour) }
	f.c.mu.Unlock()
	if e = f.c.Prune(ctx); e != nil {
		t.Fatal(e)
	}
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)
	if _, e = os.Lstat(f.c.Folder(r)); !os.IsNotExist(e) {
		t.Fatalf("folder retained: %v", e)
	}
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testEqual(t, len(deliveryRuns(t, f, f.p)), 1)

}

// R-J1IB-PWWV
func TestDeliveryRunIsWaitedForAndKilledByDrain(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	entered := deliveryHeldProvider(t, f)
	testEqual(t, f.c.Deliver(ctx, d), events.OK())
	testReceive(t, entered)
	r := deliveryRuns(t, f, f.p)[0]
	drainCtx, cancel := context.WithCancel(context.Background())
	drained := make(chan struct{})
	go func() { f.c.Drain(drainCtx); close(drained) }()
	for {
		f.c.mu.Lock()
		stopping := f.c.draining
		changed := f.c.changed
		f.c.mu.Unlock()
		if stopping {
			break
		}
		testReceive(t, changed)
	}
	select {
	case <-drained:
		t.Fatal("drain returned with event child running")
	default:
	}
	cancel()
	testReceive(t, drained)
	ended, e := f.s.RunByID(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, ended.Status, store.StatusKilled)
}

// R-IVET-T27E
func TestDeliveryCapacityRefusesAllSubscribersTogether(t *testing.T) {
	f, ctx, d := deliveryFixture(t)
	f.cfg.MaxActive = 1
	f.cfg.MaxQueued = 0
	deliveryHeldProvider(t, f)
	p, e := f.s.Create(ctx, store.Draft{Owner: "bob", Name: "two", Model: f.p.Model, Prompt: "second"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Subscribe(ctx, p.ID, d.Event.Name); e != nil {
		t.Fatal(e)
	}
	testEqual(t, f.c.Deliver(ctx, d), events.Fail(fmt.Sprintf(QueueFull, f.cfg.MaxQueued)))
	deliveryNoWork(t, f)
	testEqual(t, len(deliveryRuns(t, f, p)), 0)
}

// R-IMVJ-4O0J
func TestDeliveryWithoutCallerHasEmptyRequestID(t *testing.T) {
	f, _, d := deliveryFixture(t)
	deliveryHeldProvider(t, f)
	testEqual(t, f.c.Deliver(context.Background(), d), events.OK())
	r := deliveryRuns(t, f, f.p)[0]
	testEqual(t, r.RequestID, "")
	testEqual(t, r.User, f.p.Owner)
}

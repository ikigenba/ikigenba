package runs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func deliveredEvent() events.Event {
	return events.Event{ID: "evt_0123456789abcdef", Time: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Service: "repos", Name: "repo.pushed", RequestID: "producer-request", User: "producer", Attrs: events.Attrs{"repo": "different-repo"}, Cause: "evt_fedcba9876543210", Depth: 2, Seq: 1, Received: time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC)}
}
func subscribe(t *testing.T, h *harness, sc store.Script) {
	t.Helper()
	_, err := h.st.Subscribe(context.Background(), sc.ID, "repo.pushed")
	must(t, err)
}
func (h *harness) sourceAfter(after func(time.Duration) <-chan time.Time) {
	h.t.Helper()
	g, e := git.Find(h.cfg.Path, func() []string { return h.env })
	must(h.t, e)
	h.limit = limits.New(settings.Defaults(), limits.Clock{After: after})
	h.cfg.Source = source.New(source.Config{Repos: filepath.Dir(h.repo), Git: g, Limits: h.limit})
	h.core = runs.New(h.cfg)
}
func runList(t *testing.T, h *harness, sc store.Script) []store.Run {
	t.Helper()
	rs, err := h.st.Runs(context.Background(), sc.ID)
	must(t, err)
	return rs
}
func noNewWork(t *testing.T, h *harness) {
	t.Helper()
	if rs := runList(t, h, h.sc); len(rs) != 0 {
		t.Fatal(rs)
	}
	ds, e := os.ReadDir(h.cfg.Runs)
	if e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if es := h.sink.capture.Events(); len(es) != 0 {
		t.Fatal(es)
	}
}

// R-2T51-2K1E
func TestDeliveryUnavailableRefusesOnlyUndeliveredSubscribers(t *testing.T) {
	h := queuedHarness(t)
	subscribe(t, h, h.sc)
	h.cfg.Unavailable = "delegation missing"
	var calls atomic.Int32
	h.sourceAfter(func(time.Duration) <-chan time.Time { calls.Add(1); return make(chan time.Time) })
	d := events.Delivery{Event: deliveredEvent(), Attempt: 1}
	if got := h.core.Deliver(context.Background(), d); got != events.Fail("runs are unavailable: delegation missing") {
		t.Fatal(got)
	}
	noNewWork(t, h)
	if calls.Load() != 0 || len(h.timers) != 0 {
		t.Fatal("refusal started git or a process")
	}
	irrelevant := d
	irrelevant.Event.Name = "repo.created"
	if got := h.core.Deliver(context.Background(), irrelevant); got != events.Skip() {
		t.Fatal(got)
	}
	h.cfg.Unavailable = ""
	h.core = runs.New(h.cfg)
	if got := h.core.Deliver(context.Background(), d); got != events.OK() {
		t.Fatal(got)
	}
	rs := runList(t, h, h.sc)
	if len(rs) != 1 || rs[0].Status != store.StatusRunning || rs[0].Event != d.Event.ID {
		t.Fatal(rs)
	}
	h.release(rs[0])
	h.finished(rs[0].ID)
	beforeCalls := calls.Load()
	beforeFolders := runFolderSnapshot(t, h.cfg.Runs)
	beforeEvents := h.sink.capture.Events()
	h.cfg.Unavailable = "delegation missing"
	h.core = runs.New(h.cfg)
	if got := h.core.Deliver(context.Background(), d); got != events.OK() {
		t.Fatal(got)
	}
	if calls.Load() != beforeCalls || len(runList(t, h, h.sc)) != 1 || !reflect.DeepEqual(beforeFolders, runFolderSnapshot(t, h.cfg.Runs)) || !reflect.DeepEqual(beforeEvents, h.sink.capture.Events()) {
		t.Fatal("already delivered event created work")
	}
}

// R-2UCX-GBS3
func TestDeliveryFullQueueRefusesBeforeAnyPreparation(t *testing.T) {
	h := queuedHarness(t)
	h.cfg.MaxQueued = 1
	var calls atomic.Int32
	h.sourceAfter(func(time.Duration) <-chan time.Time { calls.Add(1); return make(chan time.Time) })
	subscribe(t, h, h.sc)
	a := h.run(nil)
	b := h.run(nil)
	assertQueued(t, h, b)
	// Establish the running script's complete initial output and delivered start event.
	until(t, func() bool {
		path := filepath.Join(h.core.Folder(a), runs.StdoutFile)
		if !fileExists(path) || !strings.HasSuffix(string(read(t, path)), "\n") {
			return false
		}
		for _, ev := range h.sink.capture.Events() {
			if ev.Name == "run.started" && ev.Attrs["run"] == a.ID {
				return true
			}
		}
		return false
	})
	beforeCalls := calls.Load()
	beforeFolders := runFolderSnapshot(t, h.cfg.Runs)
	beforeEvents := h.sink.capture.Events()
	d := events.Delivery{Event: deliveredEvent(), Attempt: 1}
	if got := h.core.Deliver(context.Background(), d); got != events.Fail("the run queue is full (1 queued); try again later") {
		t.Fatal(got)
	}
	if calls.Load() != beforeCalls || len(h.timers) != 1 || !reflect.DeepEqual(beforeFolders, runFolderSnapshot(t, h.cfg.Runs)) || !reflect.DeepEqual(beforeEvents, h.sink.capture.Events()) || len(runList(t, h, h.sc)) != 2 || h.record(a.ID) != a || h.record(b.ID) != b {
		t.Fatal("full delivery changed existing work")
	}
	_, e := h.core.Cancel(context.Background(), b.ID)
	must(t, e)
	h.finished(b.ID)
	if got := h.core.Deliver(context.Background(), d); got != events.OK() {
		t.Fatal(got)
	}
	var queued store.Run
	for _, r := range runList(t, h, h.sc) {
		if r.Event == d.Event.ID {
			queued = r
		}
	}
	assertQueued(t, h, queued)
	h.release(a)
	h.finished(a.ID)
	until(t, func() bool { return h.record(queued.ID).Status == store.StatusRunning })
	h.release(queued)
	h.finished(queued.ID)
}

// R-SBN1-0EEO R-T4WM-6W7C R-2LTM-RXL8 R-SHQI-X945 R-SIYF-B0UU R-2O9F-JH2M
func TestDeliveryCanonicalRunsAndTrail(t *testing.T) {
	script := `import os,json
with open(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'probe.json'),'w') as f:
 json.dump({'input':open(os.environ['IKIGENBA_INPUT']).read(),'env':open('/proc/self/environ','rb').read().decode().split('\u0000')[:-1]},f)
` + waitScript
	h := fixture(t, script)
	other, e := h.st.Create(context.Background(), store.Draft{Owner: "other", Name: "other-job", Repo: h.sc.Repo, Ref: "main"})
	must(t, e)
	failed, e := h.st.Create(context.Background(), store.Draft{Owner: "third", Name: "failed-job", Repo: h.sc.Repo, Ref: "missing"})
	must(t, e)
	for _, sc := range []store.Script{h.sc, other} {
		subscribe(t, h, sc)
	}
	_, e = h.st.Subscribe(context.Background(), failed.ID, "repo.*")
	must(t, e)
	ev := deliveredEvent()
	b, e := ev.MarshalJSON()
	must(t, e)
	var fields map[string]json.RawMessage
	must(t, json.Unmarshal(b, &fields))
	fields["attempt"] = json.RawMessage("7")
	body, e := json.Marshal(fields)
	must(t, e)
	req := httptest.NewRequest("POST", "/events", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "delivery-request")
	handler := events.Handlers{"*": h.core.Deliver}
	rec := httptest.NewRecorder()
	telemetry.Middleware(h.cfg.Writer, events.DeliveryHandler(handler)).ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != `{"outcome":"ok"}` {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	for _, sc := range []store.Script{h.sc, other, failed} {
		rs := runList(t, h, sc)
		if len(rs) != 1 {
			t.Fatal(rs)
		}
		r := rs[0]
		if r.Script != sc.ID || r.Event != ev.ID || r.Trigger != store.TriggerEvent || r.User != sc.Owner || r.RequestID != "delivery-request" || r.Ref != sc.Ref || r.Started != h.readNow().UTC().Truncate(time.Second) || !store.ValidRunID(r.ID) {
			t.Fatal(r)
		}
		if string(read(t, filepath.Join(h.core.Folder(r), runs.InputFile))) != string(b) {
			t.Fatal("noncanonical input")
		}
		if sc.ID == failed.ID {
			if r.Status != store.StatusFailed || r.Reason != store.ReasonCommitMissing || r.SHA != "" {
				t.Fatal(r)
			}
			continue
		}
		if r.Status != store.StatusRunning || r.SHA != h.sha {
			t.Fatal(r)
		}
		probe := filepath.Join(h.core.Folder(r), runs.OutDir, "probe.json")
		until(t, func() bool {
			if !fileExists(probe) {
				return false
			}
			return json.Valid(read(t, probe))
		})
		var got struct {
			Input string
			Env   []string
		}
		must(t, json.Unmarshal(read(t, probe), &got))
		if got.Input != string(b) {
			t.Fatal(got.Input)
		}
		env := map[string]string{}
		for _, v := range got.Env {
			k, v, _ := strings.Cut(v, "=")
			if _, ok := env[k]; ok {
				t.Fatal("duplicate environment")
			}
			env[k] = v
		}
		folder := h.core.Folder(r)
		want := map[string]string{"PATH": h.cfg.Path, "HOME": folder, "LANG": "C.UTF-8", "IKIGENBA_RUN_ID": r.ID, "IKIGENBA_SCRIPT": sc.ID, "IKIGENBA_SHA": r.SHA, "IKIGENBA_RUN_DIR": folder, "IKIGENBA_OUT_DIR": filepath.Join(folder, runs.OutDir), "IKIGENBA_INPUT": filepath.Join(folder, runs.InputFile), "IKIGENBA_USER_ID": sc.Owner, "IKIGENBA_REQUEST_ID": "delivery-request", "IKIGENBA_EVENT_ID": ev.ID, "IKIGENBA_EVENT_DEPTH": "2", "IKIGENBA_SERVICES": h.cfg.Services}
		if !reflect.DeepEqual(env, want) {
			t.Fatal(env)
		}
	}
	until(t, func() bool {
		n := 0
		for _, e := range h.sink.capture.Events() {
			if e.RequestID == "delivery-request" && strings.HasPrefix(e.Name, "run.") {
				n++
			}
		}
		return n == 3
	})
	es := h.sink.capture.Events()
	started, finished := 0, 0
	startsByRun, finishesByRun := map[string]int{}, map[string]int{}
	inside := false
	for _, e := range es {
		if e.RequestID != "delivery-request" {
			continue
		}
		if e.Name == "request.started" {
			inside = true
		}
		if e.Name == "request.finished" {
			inside = false
		}
		if e.Name == "tool.called" || strings.HasPrefix(e.Name, "script.") {
			t.Fatal(e)
		}
		if strings.HasPrefix(e.Name, "run.") {
			if !inside {
				t.Fatal("run outside request window", e)
			}
			r := h.record(e.Attrs["run"].(string))
			if e.User != r.User {
				t.Fatal(e)
			}
			if e.Name == "run.started" {
				started++
				startsByRun[r.ID]++
				if !reflect.DeepEqual(e.Attrs, runs.StartedAttrs(r)) {
					t.Fatal(e)
				}
			} else {
				finished++
				finishesByRun[r.ID]++
				if e.Attrs["status"] != store.StatusFailed || e.Attrs["reason"] != store.ReasonCommitMissing {
					t.Fatal(e)
				}
			}
		}
	}
	if started != 2 || finished != 1 {
		t.Fatal(started, finished)
	}
	for _, sc := range []store.Script{h.sc, other, failed} {
		r := runList(t, h, sc)[0]
		wantStarts, wantFinishes := 1, 0
		if sc.ID == failed.ID {
			wantStarts, wantFinishes = 0, 1
		}
		if startsByRun[r.ID] != wantStarts || finishesByRun[r.ID] != wantFinishes {
			t.Fatalf("run %s: starts %d, finishes %d; want %d/%d", r.ID, startsByRun[r.ID], finishesByRun[r.ID], wantStarts, wantFinishes)
		}
	}
	for _, sc := range []store.Script{h.sc, other} {
		r := runList(t, h, sc)[0]
		ended, e := h.core.Cancel(context.Background(), r.ID)
		must(t, e)
		if ended.Event != ev.ID {
			t.Fatal(ended)
		}
		h.finished(r.ID)
	}
	for _, e := range h.sink.capture.Events() {
		if e.Name == "run.finished" {
			r := h.record(e.Attrs["run"].(string))
			if e.RequestID != "delivery-request" || e.User != r.User {
				t.Fatal(e)
			}
		}
	}
}

// R-SCUX-E65D R-1CFG-L6G7 R-ZD3Q-J39X R-ZEBM-WV0M R-ZGRF-OEI0
func TestDeliveryEarlyRefusals(t *testing.T) {
	for _, mode := range []string{"drain", "empty", "failing", "irrelevant", "run-failing"} {
		t.Run(mode, func(t *testing.T) {
			h := fixture(t, waitScript)
			subscribe(t, h, h.sc)
			var calls atomic.Int32
			h.sourceAfter(func(time.Duration) <-chan time.Time { calls.Add(1); return make(chan time.Time) })
			ev := deliveredEvent()
			want := events.Fail(store.Unreachable)
			switch mode {
			case "drain":
				h.core.Drain(context.Background())
				h.db.SetFailing(true)
				ev.ID = ""
				want = events.Fail("scripts is stopping; try again later")
			case "empty":
				ev.ID = ""
				h.db.SetFailing(true)
				want = events.Fail("event has no id")
			case "failing", "run-failing":
				h.db.SetFailing(true)
			case "irrelevant":
				ev.Name = "repo.created"
				want = events.Skip()
			}
			if mode == "run-failing" {
				r, e := h.core.Run(context.Background(), h.sc, runs.Request{Cause: events.Cause{ID: ev.ID}})
				if r != (store.Run{}) || e == nil || errors.Is(e, runs.ErrStarting) || errors.Is(e, runs.ErrDraining) || errors.Is(e, store.ErrDelivered) || errors.Is(e, limits.ErrHalted) {
					t.Fatal(r, e)
				}
			} else if got := h.core.Deliver(context.Background(), events.Delivery{Event: ev, Attempt: 1}); got != want {
				t.Fatal(got, want)
			}
			h.db.SetFailing(false)
			if calls.Load() != 0 {
				t.Fatal("git started")
			}
			noNewWork(t, h)
			if mode == "failing" {
				if got := h.core.Deliver(context.Background(), events.Delivery{Event: ev, Attempt: 2}); got != events.OK() {
					t.Fatal(got)
				}
				if len(runList(t, h, h.sc)) != 1 {
					t.Fatal("retry missing run")
				}
			}
		})
	}
}

// R-1HQG-GIZM
func TestStartingSentinel(t *testing.T) {
	if runs.ErrStarting == nil {
		t.Fatal("nil sentinel")
	}
	for _, e := range []error{runs.ErrDraining, store.ErrNotFound, store.ErrDelivered, store.ErrEnded, limits.ErrHalted, context.Canceled} {
		if errors.Is(fmt.Errorf("wrapped: %w", runs.ErrStarting), e) || errors.Is(fmt.Errorf("wrapped: %w", e), runs.ErrStarting) {
			t.Fatal(e)
		}
	}
}

// R-SP1X-7VKB R-2QP8-B0K0
func TestDeliveryIgnoresCancellationAndStartsSideBySide(t *testing.T) {
	for _, mode := range []string{"cancelled", "cancel-during", "parallel"} {
		t.Run(mode, func(t *testing.T) {
			h := fixture(t, waitScript)
			subscribe(t, h, h.sc)
			if mode == "parallel" {
				sc, e := h.st.Create(context.Background(), store.Draft{Owner: "other", Name: "parallel", Repo: h.sc.Repo, Ref: "main"})
				must(t, e)
				subscribe(t, h, sc)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			second := make(chan struct{})
			h.sourceAfter(func(time.Duration) <-chan time.Time {
				n := calls.Add(1)
				if mode == "cancel-during" {
					cancel()
				}
				if mode == "parallel" {
					switch n {
					case 1:
						select {
						case <-second:
						case <-time.After(10 * time.Second):
							t.Error("second subscriber did not begin git")
						}
					case 2:
						close(second)
					}
				}
				return make(chan time.Time)
			})
			if mode == "cancelled" {
				cancel()
			}
			ctx = identity.NewContext(ctx, identity.Caller{RequestID: "uncancelled-delivery"})
			if got := h.core.Deliver(ctx, events.Delivery{Event: deliveredEvent(), Attempt: 1}); got != events.OK() {
				t.Fatal(got)
			}
			subs, e := h.st.Subscribers(context.Background(), "repo.pushed")
			must(t, e)
			for _, sc := range subs {
				rs := runList(t, h, sc)
				if len(rs) != 1 || rs[0].Status != store.StatusRunning || rs[0].RequestID != "uncancelled-delivery" {
					t.Fatal(rs)
				}
			}
		})
	}
}

// R-SAF4-MMNZ R-SLE8-2KC8 R-BIOT-T3QU R-2RX4-OSAP R-2PHB-X8TB
func TestEventAdmissionAndDuplicateMemory(t *testing.T) {
	h := fixture(t, waitScript)
	subscribe(t, h, h.sc)
	held, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls atomic.Int32
	h.sourceAfter(func(time.Duration) <-chan time.Time {
		if calls.Add(1) == 1 {
			close(held)
			<-release
		}
		return make(chan time.Time)
	})
	ev := deliveredEvent()
	d := events.Delivery{Event: ev, Attempt: 1}
	first := make(chan events.Outcome, 1)
	go func() { first <- h.core.Deliver(context.Background(), d) }()
	<-held
	n := calls.Load()
	beforeFolders := entries(t, filepath.Join(h.cfg.Runs, h.sc.ID))
	if got := h.core.Deliver(context.Background(), d); got != events.Fail("a run for this event is starting; try again later") {
		t.Fatal(got)
	}
	req := runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "manual-event"}, Cause: events.Cause{ID: ev.ID}}
	if r, e := h.core.Run(context.Background(), h.sc, req); r != (store.Run{}) || !errors.Is(e, runs.ErrStarting) {
		t.Fatal(r, e)
	}
	if calls.Load() != n {
		t.Fatal("duplicate began git")
	}
	if len(runList(t, h, h.sc)) != 0 || len(h.sink.capture.Events()) != 0 || !reflect.DeepEqual(beforeFolders, entries(t, filepath.Join(h.cfg.Runs, h.sc.ID))) {
		t.Fatal("overlapping duplicate changed artifacts")
	}
	req.Cause.ID = "evt_1111111111111111"
	independent, e := h.core.Run(context.Background(), h.sc, req)
	must(t, e)
	other, e := h.st.Create(context.Background(), store.Draft{Owner: "other", Name: "independent", Repo: h.sc.Repo, Ref: "main"})
	must(t, e)
	req.Cause.ID = ev.ID
	different, e := h.core.Run(context.Background(), other, req)
	must(t, e)
	close(release)
	if got := <-first; got != events.OK() {
		t.Fatal(got)
	}
	rs := runList(t, h, h.sc)
	if len(rs) != 2 {
		t.Fatal(rs)
	}
	var r store.Run
	for _, x := range rs {
		if x.Event == ev.ID {
			r = x
		}
	}
	before := calls.Load()
	until(t, func() bool { return len(h.sink.capture.Events()) == 3 })
	snapshot := h.sink.capture.Events()
	folders := entries(t, h.cfg.Runs)
	check := func() {
		t.Helper()
		if got := h.core.Deliver(context.Background(), d); got != events.OK() {
			t.Fatal(got)
		}
		if x, e := h.core.Run(context.Background(), h.sc, runs.Request{Cause: events.Cause{ID: ev.ID}}); x != (store.Run{}) || !errors.Is(e, store.ErrDelivered) {
			t.Fatal(x, e)
		}
		if calls.Load() != before {
			t.Fatal("duplicate git")
		}
	}
	check()
	if !reflect.DeepEqual(folders, entries(t, h.cfg.Runs)) || !reflect.DeepEqual(snapshot, h.sink.capture.Events()) {
		t.Fatal("duplicate changed artifacts")
	}
	_, e = h.core.Cancel(context.Background(), r.ID)
	must(t, e)
	check()
	_, e = h.core.Cancel(context.Background(), independent.ID)
	must(t, e)
	_, e = h.core.Cancel(context.Background(), different.ID)
	must(t, e)
	h.setNow(h.readNow().Add(48 * time.Hour))
	must(t, h.core.Prune(context.Background()))
	if !h.core.Gone(r) {
		t.Fatal("not pruned")
	}
	check()
}

// R-2N1J-5PBX: delivery runs use the run core's bounds, endings and keeping.
func TestDeliveryUsesRunBoundsAndEndings(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel", "drain-waits", "output", "tree"} {
		t.Run(mode, func(t *testing.T) {
			script := "import sys\nsys.stdout.write('x'*2000);sys.stdout.flush()\n" + waitScript
			h := fixture(t, script, map[string]string{"a.txt": strings.Repeat("a", 600), "b.txt": strings.Repeat("b", 600)})
			subscribe(t, h, h.sc)
			if mode == "tree" {
				g, e := git.Find(h.cfg.Path, func() []string { return h.env })
				must(t, e)
				s := settings.Defaults()
				s.TreeMaxBytes = 1024
				h.limit = limits.New(s, limits.Clock{After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
				h.cfg.Source = source.New(source.Config{Repos: filepath.Dir(h.repo), Git: g, Limits: h.limit})
				h.core = runs.New(h.cfg)
			}
			ctx := identity.NewContext(context.Background(), identity.Caller{RequestID: "bounded-delivery"})
			if got := h.core.Deliver(ctx, events.Delivery{Event: deliveredEvent(), Attempt: 1}); got != events.OK() {
				t.Fatal(got)
			}
			rs := runList(t, h, h.sc)
			if len(rs) != 1 {
				t.Fatal(rs)
			}
			r := rs[0]
			if mode == "tree" {
				if r.Status != store.StatusFailed || r.Reason != store.ReasonTooLarge {
					t.Fatal(r)
				}
				if string(read(t, filepath.Join(h.core.Folder(r), runs.TreeDir, "a.txt"))) != strings.Repeat("a", 600) {
					t.Fatal("partial archive")
				}
				return
			}
			until(t, func() bool { a, _ := h.core.Sizes(r); return a == h.cfg.OutputMaxBytes })
			want := store.StatusKilled
			switch mode {
			case "timeout":
				if d := <-h.durations; d != 9*time.Second {
					t.Fatal(d)
				}
				(<-h.timers) <- time.Time{}
				want = store.StatusTimedOut
			case "cancel":
				_, e := h.core.Cancel(context.Background(), r.ID)
				must(t, e)
			case "output":
				h.release(r)
				want = store.StatusExited
			case "drain-waits":
				done := make(chan struct{})
				go func() { h.core.Drain(context.Background()); close(done) }()
				until(t, func() bool {
					return h.core.Deliver(context.Background(), events.Delivery{}) == events.Fail("scripts is stopping; try again later")
				})
				select {
				case <-done:
					t.Fatal("drain did not wait")
				default:
				}
				h.release(r)
				<-done
				want = store.StatusExited
			}
			ev := h.finished(r.ID)
			ended := h.record(r.ID)
			if ended.Status != want || !ended.Truncated || ended.StdoutBytes != 1024 || string(read(t, filepath.Join(h.core.Folder(r), runs.StdoutFile))) != strings.Repeat("x", 1024) || ev.RequestID != "bounded-delivery" || ev.User != h.sc.Owner {
				t.Fatal(ended, ev)
			}
			h.setNow(h.readNow().Add(48 * time.Hour))
			h.addRecord(store.StatusExited, h.readNow())
			must(t, h.core.Prune(context.Background()))
			if !h.core.Gone(r) {
				t.Fatal("event run not pruned")
			}
		})
	}
}

// R-2PHB-X8TB R-2O9F-JH2M: partial deliveries retain earlier records for retries.
func TestDeliveryPartialFailureRetryAndDrainCutoff(t *testing.T) {
	for _, mode := range []string{"catalog", "drain"} {
		t.Run(mode, func(t *testing.T) {
			h := fixture(t, waitScript)
			subscribe(t, h, h.sc)
			ev := deliveredEvent()
			d := events.Delivery{Event: ev, Attempt: 1}
			if got := h.core.Deliver(context.Background(), d); got != events.OK() {
				t.Fatal(got)
			}
			first := runList(t, h, h.sc)[0]
			_, err := h.core.Cancel(context.Background(), first.ID)
			must(t, err)
			second, e := h.st.Create(context.Background(), store.Draft{Owner: "other", Name: "second", Repo: h.sc.Repo, Ref: "main"})
			must(t, e)
			subscribe(t, h, second)
			var calls atomic.Int32
			drained := make(chan struct{})
			h.sourceAfter(func(time.Duration) <-chan time.Time {
				if calls.Add(1) == 1 {
					if mode == "catalog" {
						h.db.SetFailing(true)
					} else {
						ctx, cancel := context.WithCancel(context.Background())
						cancel()
						go func() { h.core.Drain(ctx); close(drained) }()
						until(t, func() bool {
							return h.core.Deliver(context.Background(), events.Delivery{}) == events.Fail("scripts is stopping; try again later")
						})
					}
				}
				return make(chan time.Time)
			})
			// The prior core still owns its live run; the replacement core shares the store.
			want := events.Fail(store.Unreachable)
			if mode == "drain" {
				want = events.Fail("scripts is stopping; try again later")
			}
			if got := h.core.Deliver(context.Background(), d); got != want {
				t.Fatal(got, want)
			}
			h.db.SetFailing(false)
			if mode == "drain" {
				<-drained
			}
			if got := runList(t, h, h.sc); len(got) != 1 || got[0].ID != first.ID {
				t.Fatal(got)
			}
			if got := runList(t, h, second); len(got) != 0 {
				t.Fatal(got)
			}
			ds, e := os.ReadDir(filepath.Join(h.cfg.Runs, second.ID))
			if e != nil && !os.IsNotExist(e) {
				t.Fatal(e)
			}
			if len(ds) != 0 {
				t.Fatal(ds)
			}
			if mode == "catalog" {
				if got := h.core.Deliver(context.Background(), d); got != events.OK() {
					t.Fatal(got)
				}
				rs := runList(t, h, second)
				if len(rs) != 1 || rs[0].Event != ev.ID {
					t.Fatal(rs)
				}
			}
		})
	}
}

// R-SAF4-MMNZ: the final catalog boundary also refuses a competing recorded pair.
func TestDeliveredAtAdmissionCleansStartedProcess(t *testing.T) {
	h := fixture(t, "import os\nopen(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'pid'),'w').write(str(os.getpid()))\n"+waitScript)
	ev := deliveredEvent()
	var pid string
	h.cfg.ScriptAfter = func(time.Duration) <-chan time.Time {
		folder := filepath.Join(h.cfg.Runs, h.sc.ID, "run_0101010101010101")
		path := filepath.Join(folder, runs.OutDir, "pid")
		until(t, func() bool { return fileExists(path) && len(read(t, path)) > 0 })
		pid = string(read(t, path))
		_, e := h.st.AddRun(context.Background(), store.Run{ID: "run_ffffffffffffffff", Script: h.sc.ID, SHA: h.sha, Ref: h.sc.Ref, User: h.sc.Owner, RequestID: "competitor", Trigger: store.TriggerEvent, Event: ev.ID, Status: store.StatusFailed, Reason: store.ReasonStartFailed, Started: h.readNow(), Finished: h.readNow()})
		must(t, e)
		return make(chan time.Time)
	}
	h.core = runs.New(h.cfg)
	r, e := h.core.Run(context.Background(), h.sc, runs.Request{Caller: identity.Caller{UserID: h.sc.Owner}, Cause: events.Cause{ID: ev.ID}})
	if r != (store.Run{}) || !errors.Is(e, store.ErrDelivered) || errors.Is(e, runs.ErrDraining) || errors.Is(e, limits.ErrHalted) {
		t.Fatal(r, e)
	}
	if !processGone(pid) {
		t.Fatal("refused process alive", pid)
	}
	if _, e := os.Lstat(filepath.Join(h.cfg.Runs, h.sc.ID, "run_0101010101010101")); !os.IsNotExist(e) {
		t.Fatal("refused folder survived", e)
	}
	if got := h.sink.capture.Events(); len(got) != 0 {
		t.Fatal(got)
	}
}

// R-GX4M-H6P8
func TestDeliveryOverlappingPatternsMakeOneCanonicalRun(t *testing.T) {
	h := fixture(t, waitScript)
	other, err := h.st.Create(context.Background(), store.Draft{Owner: "other", Name: "unmatched", Repo: h.sc.Repo, Ref: "main"})
	must(t, err)
	subscribe(t, h, other)
	for _, pattern := range []string{"cron.*.fired", "cron.hourly.fired"} {
		_, err = h.st.Subscribe(context.Background(), h.sc.ID, pattern)
		must(t, err)
	}
	ev := deliveredEvent()
	ev.Name = "cron.hourly.fired"
	if got := h.core.Deliver(context.Background(), events.Delivery{Event: ev, Attempt: 1}); got != events.OK() {
		t.Fatal(got)
	}
	records := runList(t, h, h.sc)
	if len(records) != 1 || records[0].Script != h.sc.ID || records[0].Event != ev.ID || len(runList(t, h, other)) != 0 {
		t.Fatal(records, runList(t, h, other))
	}
	canonical, err := ev.MarshalJSON()
	must(t, err)
	if got := read(t, filepath.Join(h.core.Folder(records[0]), runs.InputFile)); string(got) != string(canonical) {
		t.Fatalf("input %s; want %s", got, canonical)
	}
	h.release(records[0])
	h.finished(records[0].ID)
	for i, name := range []string{"cron.fired", "cron.a.b.fired"} {
		ev.Name = name
		ev.ID = fmt.Sprintf("evt_%016x", i+1)
		if got := h.core.Deliver(context.Background(), events.Delivery{Event: ev, Attempt: 1}); got != events.Skip() {
			t.Fatal(name, got)
		}
		if got := runList(t, h, h.sc); len(got) != 1 || got[0].ID != records[0].ID || len(runList(t, h, other)) != 0 {
			t.Fatal(name, got)
		}
	}
}

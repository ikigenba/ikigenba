package runs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

type admissionRand struct {
	reads   atomic.Int64
	bytes   atomic.Int64
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	next    byte
}

func (r *admissionRand) Read(p []byte) (int, error) {
	r.reads.Add(1)
	r.once.Do(func() {
		if r.entered != nil {
			close(r.entered)
			<-r.release
		}
	})
	r.bytes.Add(int64(len(p)))
	for i := range p {
		p[i] = 0x5a + r.next
	}
	r.next++
	return len(p), nil
}

type admissionResult struct {
	run store.Run
	err error
}

func admissionCall(ctx context.Context, f *coreFixture, req Request) <-chan admissionResult {
	ch := make(chan admissionResult, 1)
	go func() { r, e := f.c.Run(ctx, f.p, req); ch <- admissionResult{r, e} }()
	return ch
}
func admissionHeld(f *coreFixture) *admissionRand {
	r := &admissionRand{entered: make(chan struct{}), release: make(chan struct{})}
	f.cfg.Rand = r
	f.rebuild()
	return r
}
func admissionError(t *testing.T, result admissionResult, want error) {
	t.Helper()
	testEqual(t, result.run, store.Run{})
	if result.err == nil || (want != nil && !errors.Is(result.err, want)) {
		t.Fatalf("error %v want %v", result.err, want)
	}
}

// D04's catalog-error classification, with unrelated run refusals and cancellation excluded for these live-context calls.
func admissionCatalogError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected catalog error")
	}
	for _, sentinel := range []error{store.ErrNotFound, store.ErrNameTaken, store.ErrEnded, store.ErrNotSubscribed, store.ErrDelivered, ErrDraining, ErrStarting, ErrQueueFull, ErrNoCgroup, ErrCutOff, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, sentinel) {
			t.Fatalf("got refusal %v instead of catalog error", err)
		}
	}
}

func admissionQuiet(t *testing.T, f *coreFixture) {
	t.Helper()
	testEqual(t, len(f.timers), 0)
	if e := f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, e := range f.sink.capture.Events() {
		if e.Name == "run.started" || e.Name == "run.finished" {
			t.Fatal(e)
		}
	}
	rs, e := f.s.Runs(context.Background(), f.p.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, len(rs), 0)
	if entries, e := os.ReadDir(f.cfg.Runs); e == nil && len(entries) > 0 {
		for _, entry := range entries {
			sub, e := os.ReadDir(filepath.Join(f.cfg.Runs, entry.Name()))
			if e != nil || len(sub) > 0 {
				t.Fatal(entries, e)
			}
		}
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
}

// Synchronize the test with entry to Drain; assertions use its public effects.
func admissionDraining(t *testing.T, c *Core, deadline bool) {
	t.Helper()
	for {
		c.mu.Lock()
		ready := c.draining && (!deadline || c.deadline)
		changed := c.changed
		c.mu.Unlock()
		if ready {
			return
		}
		testReceive(t, changed)
	}
}
func admissionProvider(t *testing.T, f *coreFixture) (<-chan struct{}, chan<- struct{}) {
	entered := make(chan struct{}, 20)
	release := make(chan struct{}, 20)
	server := provider(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			answer(w, "done")
		case <-r.Context().Done():
		}
	})
	f.cfg.BaseURL = server.URL
	return entered, release
}

// R-NVIA-BUHE R-NWQ6-PM83 R-11FE-SYL4
func TestAdmissionRefusalOrder(t *testing.T) {
	for _, drain := range []bool{false, true} {
		t.Run(fmt.Sprint(drain), func(t *testing.T) {
			f := newCoreFixture(t)
			r := &admissionRand{}
			f.cfg.Rand = r
			f.cfg.Unavailable = "unavailable reason"
			f.d.SetFailing(true)
			f.rebuild()
			if drain {
				f.c.Drain(context.Background())
			}
			req := f.request()
			req.Cause = events.Cause{ID: "event-one"}
			run, e := f.c.Run(context.Background(), f.p, req)
			want := ErrNoCgroup
			if drain {
				want = ErrDraining
			} else {
				testEqual(t, e.Error(), fmt.Sprintf(NoRuns, f.cfg.Unavailable))
			}
			admissionError(t, admissionResult{run, e}, want)
			testEqual(t, r.bytes.Load(), int64(0))
			f.d.SetFailing(false)
			admissionQuiet(t, f)
		})
	}
}

// R-OIOD-LHKL
func TestAdmissionRandomSerialized(t *testing.T) {
	f := newCoreFixture(t)
	now := make(chan struct{}, 4)
	f.cfg.Now = func() time.Time { now <- struct{}{}; return testInstant }
	r := admissionHeld(f)
	req := f.request()
	req.Input = []byte("{x")
	a := admissionCall(context.Background(), f, req)
	testReceive(t, r.entered)
	b := admissionCall(context.Background(), f, req)
	testReceive(t, now)
	testReceive(t, now)
	testEqual(t, r.reads.Load(), int64(1))
	close(r.release)
	ra, rb := testReceive(t, a), testReceive(t, b)
	if ra.err != nil || rb.err != nil {
		t.Fatal(ra.err, rb.err)
	}
	testEqual(t, ra.run.ID, "prr_5a5a5a5a5a5a5a5a")
	testEqual(t, rb.run.ID, "prr_5b5b5b5b5b5b5b5b")
	testEqual(t, r.bytes.Load(), int64(16))
}

// R-0HX0-OMQ0 R-PD5V-5R3Y R-PZ42-1MGG R-0XRP-NND1 R-PI1G-OU2Q
func TestAdmissionHeldFailures(t *testing.T) {
	for _, kind := range []string{"cancel", "store-delete", "core-delete", "catalog-running", "catalog-failed"} {
		t.Run(kind, func(t *testing.T) {
			f := newCoreFixture(t)
			admissionProvider(t, f)
			f.cfg.Cgroup = t.TempDir()
			r := admissionHeld(f)
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			req := f.request()
			if kind == "catalog-failed" {
				req.Input = []byte("{x")
			}
			ch := admissionCall(ctx, f, req)
			testReceive(t, r.entered)
			want := error(nil)
			switch kind {
			case "cancel":
				want = errors.New("call cancelled")
				cancel(want)
			case "store-delete":
				want = store.ErrNotFound
				if e := f.s.Delete(context.Background(), f.p.ID); e != nil {
					t.Fatal(e)
				}
			case "core-delete":
				want = store.ErrNotFound
				if e := f.c.Delete(context.Background(), f.p.ID); e != nil {
					t.Fatal(e)
				}
			default:
				f.d.SetFailing(true)
			}
			close(r.release)
			res := testReceive(t, ch)
			admissionError(t, res, want)
			if kind == "catalog-running" || kind == "catalog-failed" {
				admissionCatalogError(t, res.err)
			}
			if errors.Is(res.err, ErrDraining) || errors.Is(res.err, ErrCutOff) {
				t.Fatal(res.err)
			}
			f.d.SetFailing(false)
			if kind == "core-delete" {
				if _, e := os.Lstat(filepath.Join(f.cfg.Runs, f.p.ID)); !errors.Is(e, os.ErrNotExist) {
					t.Fatal(e)
				}
			}
			if _, e := os.Lstat(filepath.Join(f.cfg.Cgroup, "prr_5a5a5a5a5a5a5a5a")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal(e)
			}
			if _, e := f.s.RunByID(context.Background(), "prr_5a5a5a5a5a5a5a5a"); !errors.Is(e, store.ErrNotFound) {
				t.Fatal(e)
			}
			if _, e := os.Lstat(filepath.Join(f.cfg.Runs, f.p.ID, "prr_5a5a5a5a5a5a5a5a")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal(e)
			}
			if e := f.w.Flush(context.Background()); e != nil {
				t.Fatal(e)
			}
			for _, ev := range f.sink.capture.Events() {
				if ev.Name == "run.started" || ev.Name == "run.finished" {
					t.Fatal(ev)
				}
			}
		})
	}
}

// R-0J4X-2EGP R-PI1G-OU2Q
func TestAdmissionFolderPermission(t *testing.T) {
	f := newCoreFixture(t)
	if e := os.Mkdir(f.cfg.Runs, 0500); e != nil {
		t.Fatal(e)
	}
	info, statErr := os.Stat(f.cfg.Runs)
	if statErr != nil {
		t.Fatal(statErr)
	}
	t.Cleanup(func() {
		if e := os.Chmod(f.cfg.Runs, info.Mode().Perm()|0200); e != nil {
			t.Error(e)
		}
	})
	run, e := f.c.Run(context.Background(), f.p, f.request())
	admissionError(t, admissionResult{run, e}, nil)
	for _, sentinel := range []error{ErrDraining, ErrStarting, ErrQueueFull, ErrNoCgroup, ErrCutOff, store.ErrNotFound, store.ErrNameTaken, store.ErrEnded, store.ErrDelivered, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(e, sentinel) {
			t.Fatal(e, sentinel)
		}
	}
	admissionQuiet(t, f)
}

// R-0VBW-W3VN
func TestAdmissionDeliveredCatalogFailure(t *testing.T) {
	f := newCoreFixture(t)
	r := &admissionRand{}
	f.cfg.Rand = r
	f.rebuild()
	f.d.SetFailing(true)
	req := f.request()
	req.Cause = events.Cause{ID: "event-one"}
	run, e := f.c.Run(context.Background(), f.p, req)
	admissionError(t, admissionResult{run, e}, nil)
	admissionCatalogError(t, e)
	testEqual(t, r.bytes.Load(), int64(0))
	f.d.SetFailing(false)
	admissionQuiet(t, f)
}

var _ io.Reader = (*admissionRand)(nil)

// R-0SW4-4KE9 R-11FE-SYL4
func TestAdmissionDeliveredBeforeQueue(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.MaxActive = 1
	f.cfg.MaxQueued = 1
	entered, _ := admissionProvider(t, f)
	random := &admissionRand{}
	f.cfg.Rand = random
	f.rebuild()
	req := f.request()
	req.Cause = events.Cause{ID: "event-one"}
	a := f.start(t, f.p, req)
	testReceive(t, entered)
	queued := f.start(t, f.p, f.request())
	testEqual(t, queued.Status, store.StatusQueued)
	for _, state := range []string{"running", "ended", "pruned"} {
		if state == "ended" {
			if _, err := f.c.Cancel(context.Background(), a.ID); err != nil {
				t.Fatal(err)
			}
			testReceive(t, entered)
			replacement := f.start(t, f.p, f.request())
			testEqual(t, replacement.Status, store.StatusQueued)
		}
		if state == "pruned" {
			if err := f.s.DeleteRun(context.Background(), a.ID); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.w.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		beforeEvents := len(f.sink.capture.Events())
		beforeBytes := random.bytes.Load()
		beforeRecords, err := f.s.Runs(context.Background(), f.p.ID)
		if err != nil {
			t.Fatal(err)
		}
		run, err := f.c.Run(context.Background(), f.p, req)
		admissionError(t, admissionResult{run, err}, store.ErrDelivered)
		if errors.Is(err, ErrDraining) || errors.Is(err, ErrCutOff) {
			t.Fatal(err)
		}
		testEqual(t, random.bytes.Load(), beforeBytes)
		if err := f.w.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		testEqual(t, len(f.sink.capture.Events()), beforeEvents)
		afterRecords, err := f.s.Runs(context.Background(), f.p.ID)
		if err != nil {
			t.Fatal(err)
		}
		testEqual(t, afterRecords, beforeRecords)
	}
}

// R-0SW4-4KE9 R-PI1G-OU2Q
func TestAdmissionDeliveredAddRace(t *testing.T) {
	f := newCoreFixture(t)
	r := admissionHeld(f)
	req := f.request()
	req.Input = []byte("{x")
	req.Cause = events.Cause{ID: "event-one"}
	ch := admissionCall(context.Background(), f, req)
	testReceive(t, r.entered)
	winner, e := f.s.AddRun(context.Background(), store.Run{ID: "prr_0102030405060708", Prompt: f.p.ID, Model: f.p.Model, User: req.Caller.UserID, RequestID: req.Caller.RequestID, Trigger: store.TriggerEvent, Event: req.Cause.ID, Started: testInstant.Truncate(time.Second), Status: store.StatusFailed, Reason: store.ReasonStartFailed, Finished: testInstant.Truncate(time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	close(r.release)
	res := testReceive(t, ch)
	admissionError(t, res, store.ErrDelivered)
	if errors.Is(res.err, ErrCutOff) || errors.Is(res.err, ErrDraining) {
		t.Fatal(res.err)
	}
	rs, e := f.s.Runs(context.Background(), f.p.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, rs, []store.Run{winner})
	if _, e := os.Lstat(filepath.Join(f.cfg.Runs, f.p.ID, "prr_5a5a5a5a5a5a5a5a")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	if e := f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, ev := range f.sink.capture.Events() {
		if ev.Name == "run.started" || ev.Name == "run.finished" {
			t.Fatal(ev)
		}
	}
}

// R-0U40-IC4Y
func TestAdmissionEventInProgress(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.MaxActive = 3
	entered, release := admissionProvider(t, f)
	now := make(chan struct{}, 8)
	f.cfg.Now = func() time.Time { now <- struct{}{}; return testInstant }
	r := admissionHeld(f)
	req := f.request()
	req.Cause = events.Cause{ID: "event-one"}
	a := admissionCall(context.Background(), f, req)
	testReceive(t, r.entered)
	testReceive(t, now)
	run, e := f.c.Run(context.Background(), f.p, req)
	admissionError(t, admissionResult{run, e}, ErrStarting)
	testEqual(t, len(now), 0)
	testEqual(t, r.reads.Load(), int64(1))
	admissionQuiet(t, f)
	other := req
	other.Cause.ID = "event-two"
	b := admissionCall(context.Background(), f, other)
	testReceive(t, now)
	p, e := f.s.Create(context.Background(), store.Draft{Owner: f.p.Owner, OwnerEmail: f.p.OwnerEmail, Name: "other", Model: f.p.Model, Prompt: "other"})
	if e != nil {
		t.Fatal(e)
	}
	c := make(chan admissionResult, 1)
	go func() { run, e := f.c.Run(context.Background(), p, req); c <- admissionResult{run, e} }()
	testReceive(t, now)
	close(r.release)
	for _, ch := range []<-chan admissionResult{a, b, c} {
		res := testReceive(t, ch)
		if res.err != nil || res.run.Status != store.StatusRunning {
			t.Fatal(res)
		}
	}

	for i := 0; i < 3; i++ {
		testReceive(t, entered)
		release <- struct{}{}
	}
	f.c.Drain(context.Background())
}

func admissionFinished(t *testing.T, f *coreFixture, id, status, reason string) store.Run {
	t.Helper()
	event := f.event(t, "run.finished", id)
	r, e := f.s.RunByID(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, r.Status, status)
	testEqual(t, r.Reason, reason)
	testEqual(t, r.ExitCode, 0)
	testEqual(t, event.Attrs, FinishedAttrs(r, f.cfg.Now().Sub(testInstant)))
	testEqual(t, event.User, r.User)
	testEqual(t, event.RequestID, r.RequestID)
	return r
}
func admissionDrain(ctx context.Context, f *coreFixture) <-chan struct{} {
	done := make(chan struct{})
	go func() { f.c.Drain(ctx); close(done) }()
	return done
}
func admissionStillDraining(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("drain returned before held work")
	default:
	}
}

// R-ODSS-2ELT R-0YZM-1F3Q R-G3BD-LQKK
func TestAdmissionDrainQueued(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.MaxActive = 1
	entered, release := admissionProvider(t, f)
	f.rebuild()
	a := f.start(t, f.p, f.request())
	testReceive(t, entered)
	b := f.start(t, f.p, f.request())
	done := admissionDrain(context.Background(), f)
	r := admissionFinished(t, f, b.ID, store.StatusFailed, store.ReasonQueueAbandoned)
	testEqual(t, r.Usage, store.Usage{})
	testEqual(t, []int64{r.StdoutBytes, r.StderrBytes}, []int64{0, 0})
	if r.Truncated() {
		t.Fatal(r)
	}
	testEqual(t, r.Finished, testInstant.Truncate(time.Second))
	testEqual(t, testRead(t, filepath.Join(f.c.Folder(b), InputFile)), f.request().Input)
	entries, e := os.ReadDir(f.c.Folder(b))
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, len(entries), 2)
	admissionUnstarted(t, f, b)
	admissionStillDraining(t, done)
	release <- struct{}{}
	testReceive(t, done)
	admissionFinished(t, f, a.ID, store.StatusExited, "")
	testEqual(t, len(entered), 0)
	if e := f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, ev := range f.sink.capture.Events() {
		if ev.Name == "run.started" && ev.Attrs["prompt_run"] == b.ID {
			t.Fatal(ev)
		}
	}
}

// R-G0VK-U736 R-G23H-7YTV R-QA35-HK4P R-G3BD-LQKK R-0HX0-OMQ0
func TestAdmissionDrainDeadline(t *testing.T) {
	f := newCoreFixture(t)
	entered, _ := admissionProvider(t, f)
	f.cfg.MaxActive = 1
	var offset atomic.Int64
	f.cfg.Now = func() time.Time { return testInstant.Add(time.Duration(offset.Load())) }
	f.cfg.KeepCount = 1
	old, e := f.s.AddRun(context.Background(), store.Run{ID: "prr_1112131415161718", Prompt: f.p.ID, Model: f.p.Model, User: f.p.Owner, Trigger: store.TriggerManual, Started: testInstant.Add(-40 * 24 * time.Hour), Finished: testInstant.Add(-40 * 24 * time.Hour), Status: store.StatusFailed, Reason: store.ReasonStartFailed})
	if e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(f.c.Folder(old), 0700); e != nil {
		t.Fatal(e)
	}
	f.rebuild()
	a := f.start(t, f.p, f.request())
	testReceive(t, entered)
	r := &admissionRand{entered: make(chan struct{}), release: make(chan struct{})}
	f.c.cfg.Rand = r
	call := admissionCall(context.Background(), f, f.request())
	testReceive(t, r.entered)
	ctx, cancel := context.WithCancel(context.Background())
	done := admissionDrain(ctx, f)
	admissionDraining(t, f.c, false)
	offset.Store(int64(10 * time.Second))
	cancel()
	admissionDraining(t, f.c, true)
	ended := admissionFinished(t, f, a.ID, store.StatusKilled, "")
	testEqual(t, ended.Finished, testInstant.Add(10*time.Second).Truncate(time.Second))
	admissionStillDraining(t, done)
	close(r.release)
	admissionError(t, testReceive(t, call), ErrCutOff)
	testReceive(t, done)
	if _, e := f.s.RunByID(context.Background(), old.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := os.Lstat(f.c.Folder(old)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	if e := f.w.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	rs, e := f.s.Runs(context.Background(), f.p.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, len(rs), 1)
	count := 0
	for _, ev := range f.sink.capture.Events() {
		if ev.Name == "run.finished" && ev.Attrs["prompt_run"] == a.ID {
			count++
		}
	}
	testEqual(t, count, 1)
	if _, e := os.Lstat(filepath.Join(f.cfg.Runs, f.p.ID, "prr_5a5a5a5a5a5a5a5a")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
}

// R-G4J9-ZIB9 R-0QGB-D0WV R-0YZM-1F3Q
func TestAdmissionDrainInProgress(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		t.Run(fmt.Sprint(occupied), func(t *testing.T) {
			f := newCoreFixture(t)
			f.cfg.MaxActive = 1
			var offset atomic.Int64
			f.cfg.Now = func() time.Time { return testInstant.Add(time.Duration(offset.Load())) }
			entered, release := admissionProvider(t, f)
			f.rebuild()
			var active store.Run
			if occupied {
				active = f.start(t, f.p, f.request())
				testReceive(t, entered)
			}
			r := &admissionRand{entered: make(chan struct{}), release: make(chan struct{})}
			f.c.cfg.Rand = r
			call := admissionCall(context.Background(), f, f.request())
			testReceive(t, r.entered)
			done := admissionDrain(context.Background(), f)
			admissionDraining(t, f.c, false)
			admissionStillDraining(t, done)
			offset.Store(int64(10 * time.Second))
			close(r.release)
			res := testReceive(t, call)
			if res.err != nil {
				t.Fatal(res.err)
			}
			if occupied {
				testEqual(t, res.run.Status, store.StatusFailed)
				testEqual(t, res.run.Reason, store.ReasonQueueAbandoned)
				testEqual(t, res.run.Usage, store.Usage{})
				testEqual(t, res.run.Finished, testInstant.Add(10*time.Second).Truncate(time.Second))
				testEqual(t, []int64{res.run.StdoutBytes, res.run.StderrBytes}, []int64{0, 0})
				if res.run.Truncated() {
					t.Fatal(res.run)
				}
				entries, e := os.ReadDir(f.c.Folder(res.run))
				if e != nil {
					t.Fatal(e)
				}
				testEqual(t, len(entries), 2)
				admissionFinished(t, f, res.run.ID, store.StatusFailed, store.ReasonQueueAbandoned)
				admissionUnstarted(t, f, res.run)
				admissionStillDraining(t, done)
				release <- struct{}{}
				testReceive(t, done)
				admissionFinished(t, f, active.ID, store.StatusExited, "")
				testEqual(t, len(entered), 0)
			} else {
				testEqual(t, res.run.Status, store.StatusRunning)
				testReceive(t, entered)
				admissionStillDraining(t, done)
				release <- struct{}{}
				testReceive(t, done)
				admissionFinished(t, f, res.run.ID, store.StatusExited, "")
			}
		})
	}
}

func admissionUnstarted(t *testing.T, f *coreFixture, r store.Run) {
	t.Helper()
	testEqual(t, testRead(t, filepath.Join(f.c.Folder(r), InputFile)), f.request().Input)
	work, e := os.ReadDir(filepath.Join(f.c.Folder(r), WorkDir))
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, len(work), 0)
	for _, name := range []string{StdoutFile, StderrFile} {
		if _, e := os.Lstat(filepath.Join(f.c.Folder(r), name)); !errors.Is(e, os.ErrNotExist) {
			t.Fatal(e)
		}
	}
	testEqual(t, r.Prompt, f.p.ID)
	testEqual(t, r.Model, f.p.Model)
	testEqual(t, r.User, f.request().Caller.UserID)
	testEqual(t, r.RequestID, f.request().Caller.RequestID)
	testEqual(t, r.Trigger, store.TriggerManual)
	testEqual(t, r.Event, "")
	testEqual(t, r.Started, testInstant.Truncate(time.Second))
}

// R-0HX0-OMQ0 R-PI1G-OU2Q
func TestAdmissionAlreadyCancelledCause(t *testing.T) {
	for _, event := range []bool{false, true} {
		t.Run(fmt.Sprint(event), func(t *testing.T) {
			f := newCoreFixture(t)
			random := &admissionRand{}
			f.cfg.Rand = random
			f.rebuild()
			cause := errors.New("caller ended before admission")
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(cause)
			req := f.request()
			if event {
				req.Cause = events.Cause{ID: "cancelled-event"}
			}
			run, err := f.c.Run(ctx, f.p, req)
			admissionError(t, admissionResult{run, err}, cause)
			if errors.Is(err, ErrDraining) {
				t.Fatal(err)
			}
			testEqual(t, random.bytes.Load(), int64(0))
			admissionQuiet(t, f)
		})
	}
}

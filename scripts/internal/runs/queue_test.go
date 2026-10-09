package runs_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func queuedHarness(t *testing.T) *harness {
	t.Helper()
	h := fixture(t, "import os\nprint(os.getpid(),flush=True)\n"+waitScript)
	h.cfg.MaxActive = 1
	h.cfg.MaxQueued = 8
	h.cfg.KeepCount = 20
	h.core = runs.New(h.cfg)
	return h
}
func assertQueued(t *testing.T, h *harness, r store.Run) {
	t.Helper()
	if r.Status != store.StatusQueued || h.record(r.ID) != r || r.SHA != h.sha {
		t.Fatalf("queued record %+v", r)
	}
	if got := entries(t, h.core.Folder(r)); !reflect.DeepEqual(got, []string{runs.InputFile, runs.OutDir, runs.TreeDir}) {
		t.Fatal(got)
	}
	if a, b := h.core.Sizes(r); a != 0 || b != 0 {
		t.Fatal(a, b)
	}
	for _, e := range h.sink.capture.Events() {
		if e.Attrs["run"] == r.ID {
			t.Fatal("queued event", e)
		}
	}
	must(t, filepath.WalkDir(filepath.Join(h.core.Folder(r), runs.TreeDir), func(_ string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e == nil && info.Mode().Perm()&0222 != 0 {
			t.Error("writable queued tree")
		}
		return e
	}))
}
func assertNoStart(t *testing.T, h *harness, id string) {
	t.Helper()
	for _, e := range h.sink.capture.Events() {
		if e.Attrs["run"] == id && e.Name == "run.started" {
			t.Fatal(e)
		}
	}
}

// R-Y1O4-NB79 R-YPTQ-MF47 R-0OJW-T44H
func TestUnavailableAndDistinctAdmissionSentinels(t *testing.T) {
	all := []error{runs.ErrNoCgroup, runs.ErrQueueFull, runs.ErrDraining, runs.ErrStarting, store.ErrNotFound, store.ErrDelivered, store.ErrEnded, limits.ErrHalted, context.Canceled}
	for i, a := range all[:2] {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range all {
			if i != j && (errors.Is(a, b) || errors.Is(b, a)) {
				t.Fatal(a, b)
			}
		}
	}
	h := queuedHarness(t)
	h.cfg.Unavailable = "no control group delegated"
	h.cfg.Rand = strings.NewReader("")
	h.core = runs.New(h.cfg)
	r, e := h.core.Run(context.Background(), h.sc, runs.Request{})
	if r != (store.Run{}) || !errors.Is(e, runs.ErrNoCgroup) || e.Error() != fmt.Sprintf(runs.NoRuns, "no control group delegated") {
		t.Fatal(r, e)
	}
	noNewWork(t, h)
	h.core.Drain(context.Background())
	_, e = h.core.Run(context.Background(), h.sc, runs.Request{})
	if !errors.Is(e, runs.ErrDraining) {
		t.Fatal(e)
	}
}

// R-Y6JQ-6E61 R-Y7RM-K5WQ R-Y8ZI-XXNF R-Y0G8-9JGK R-XQP1-7DJ0
func TestQueueStartsFIFOWithTimersAndTrailOrder(t *testing.T) {
	h := queuedHarness(t)
	a := h.run(nil)
	b := h.run([]byte(`{"queued":true}`))
	c := h.run(nil)
	assertQueued(t, h, b)
	assertQueued(t, h, c)
	if string(read(t, filepath.Join(h.core.Folder(b), runs.InputFile))) != `{"queued":true}` {
		t.Fatal("input")
	}
	if len(h.timers) != 1 {
		t.Fatal("queued timer", len(h.timers))
	}
	originalStarted := b.Started
	h.setNow(h.readNow().Add(3 * time.Second))
	h.release(a)
	h.finished(a.ID)
	until(t, func() bool { return h.record(b.ID).Status == store.StatusRunning })
	until(t, func() bool {
		for _, e := range h.sink.capture.Events() {
			if e.Name == "run.started" && e.Attrs["run"] == b.ID {
				return true
			}
		}
		return false
	})
	if h.record(c.ID).Status != store.StatusQueued || h.record(b.ID).Started != originalStarted || len(h.timers) != 2 {
		t.Fatal("promotion metadata")
	}
	if got := entries(t, h.core.Folder(b)); !reflect.DeepEqual(got, []string{runs.InputFile, runs.OutDir, runs.StderrFile, runs.StdoutFile, runs.TreeDir}) {
		t.Fatal(got)
	}
	finishA, startB := -1, -1
	for i, e := range h.sink.capture.Events() {
		if e.Name == "run.finished" && e.Attrs["run"] == a.ID {
			finishA = i
		}
		if e.Name == "run.started" && e.Attrs["run"] == b.ID {
			startB = i
		}
	}
	if finishA < 0 || startB <= finishA {
		t.Fatal("event order", finishA, startB)
	}
	h.setNow(h.readNow().Add(2 * time.Second))
	h.release(b)
	ev := h.finished(b.ID)
	if ev.Attrs["duration_us"] != int64(5*time.Second/time.Microsecond) {
		t.Fatal(ev)
	}
	until(t, func() bool { return h.record(c.ID).Status == store.StatusRunning })
	h.release(c)
	h.finished(c.ID)
}

// R-YS9J-DYLL R-0OJW-T44H
func TestQueueFullRefusesBeforePreparationAndDuplicateWins(t *testing.T) {
	h := queuedHarness(t)
	h.cfg.MaxQueued = 1
	h.core = runs.New(h.cfg)
	a, e := h.core.Run(context.Background(), h.sc, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "request"}, Cause: events.Cause{ID: "evt_1111111111111111"}})
	must(t, e)
	b := h.run(nil)
	assertQueued(t, h, b)
	before := entries(t, filepath.Dir(h.core.Folder(b)))
	r, e := h.core.Run(context.Background(), h.sc, runs.Request{})
	if r != (store.Run{}) || !errors.Is(e, runs.ErrQueueFull) || e.Error() != fmt.Sprintf(runs.QueueFull, int64(1)) {
		t.Fatal(r, e)
	}
	if !reflect.DeepEqual(before, entries(t, filepath.Dir(h.core.Folder(b)))) {
		t.Fatal("folder created")
	}
	_, e = h.core.Run(context.Background(), h.sc, runs.Request{Cause: events.Cause{ID: "evt_1111111111111111"}})
	if !errors.Is(e, store.ErrDelivered) {
		t.Fatal(e)
	}
	if h.record(a.ID).Status != store.StatusRunning || h.record(b.ID).Status != store.StatusQueued {
		t.Fatal("admission changed existing runs")
	}
	h.release(a)
	h.finished(a.ID)
	until(t, func() bool { return h.record(b.ID).Status == store.StatusRunning })
	h.release(b)
	h.finished(b.ID)
}

// R-YF30-USCW R-YCN8-38VI R-XRWX-L59P
func TestCancelQueuedLeavesOtherRunsAndPrunesBeforeEvent(t *testing.T) {
	h := queuedHarness(t)
	h.cfg.KeepCount = 1
	h.core = runs.New(h.cfg)
	old := h.addRecord(store.StatusExited, h.readNow().Add(-72*time.Hour))
	must(t, os.MkdirAll(h.core.Folder(old), 0700))
	a := h.run(nil)
	b := h.run(nil)
	c := h.run(nil)
	ended, e := h.core.Cancel(context.Background(), b.ID)
	must(t, e)
	ev := h.finished(b.ID)
	if ended.Status != store.StatusKilled || ended.ExitCode != 0 || ended.StdoutBytes != 0 || ended.StderrBytes != 0 || ended.Truncated || ev.User != "owner" || ev.RequestID != "request" {
		t.Fatal(ended, ev)
	}
	assertNoStart(t, h, b.ID)
	if !h.core.Gone(old) {
		t.Fatal("prune not complete")
	}
	_, e = h.st.RunByID(context.Background(), old.ID)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if h.record(a.ID).Status != store.StatusRunning || h.record(c.ID).Status != store.StatusQueued {
		t.Fatal("cancel advanced queue")
	}
	if _, e = h.core.Cancel(context.Background(), b.ID); !errors.Is(e, store.ErrEnded) {
		t.Fatal(e)
	}
	h.release(a)
	h.finished(a.ID)
	until(t, func() bool { return h.record(c.ID).Status == store.StatusRunning })
	h.release(c)
	h.finished(c.ID)
}

// R-YGAX-8K3L R-XZ8B-VRPV
func TestForeignQueuedCancelAndRecover(t *testing.T) {
	for _, recovering := range []bool{false, true} {
		t.Run(fmt.Sprint(recovering), func(t *testing.T) {
			h := queuedHarness(t)
			now := h.readNow()
			r, e := h.st.AddRun(context.Background(), store.Run{ID: "run_aaaaaaaaaaaaaaaa", Script: h.sc.ID, SHA: h.sha, Ref: h.sc.Ref, User: "foreign-owner", RequestID: "foreign-request", Trigger: store.TriggerManual, Status: store.StatusQueued, Started: now})
			must(t, e)
			must(t, os.MkdirAll(h.core.Folder(r), 0700))
			must(t, os.WriteFile(filepath.Join(h.core.Folder(r), runs.InputFile), []byte("{}"), 0600))
			snapshot := runFolderSnapshot(t, h.core.Folder(r))
			h.setNow(now.Add(-time.Hour))
			if recovering {
				must(t, h.core.Recover(context.Background()))
			} else {
				_, e = h.core.Cancel(context.Background(), r.ID)
				must(t, e)
			}
			ev := h.finished(r.ID)
			ended := h.record(r.ID)
			status, reason := store.StatusKilled, ""
			if recovering {
				status, reason = store.StatusFailed, store.ReasonQueueAbandoned
			}
			if ended.Status != status || ended.Reason != reason || ended.Finished != r.Started || ended.StdoutBytes != 0 || ended.StderrBytes != 0 || ended.Truncated || ev.Attrs["duration_us"] != int64(0) || ev.User != r.User || ev.RequestID != r.RequestID {
				t.Fatal(ended, ev)
			}
			if !reflect.DeepEqual(snapshot, runFolderSnapshot(t, h.core.Folder(r))) {
				t.Fatal("folder changed")
			}
			assertNoStart(t, h, r.ID)
		})
	}
}

// R-YIQQ-03KZ R-XVKM-QGHS R-XWSJ-488H
func TestDrainAbandonsQueueBeforeRunningRunEnds(t *testing.T) {
	h := queuedHarness(t)
	a := h.run(nil)
	b := h.run(nil)
	snapshot := runFolderSnapshot(t, h.core.Folder(b))
	drained := make(chan struct{})
	go func() { h.core.Drain(context.Background()); close(drained) }()
	h.finished(b.ID)
	if ended := h.record(b.ID); ended.Status != store.StatusFailed || ended.Reason != store.ReasonQueueAbandoned || ended.StdoutBytes != 0 || ended.StderrBytes != 0 || ended.Truncated {
		t.Fatal(ended)
	}
	assertNoStart(t, h, b.ID)
	if !reflect.DeepEqual(snapshot, runFolderSnapshot(t, h.core.Folder(b))) || h.record(a.ID).Status != store.StatusRunning {
		t.Fatal("drain changed folders or killed active run")
	}
	select {
	case <-drained:
		t.Fatal("early drain")
	default:
	}
	h.release(a)
	h.finished(a.ID)
	select {
	case <-drained:
	case <-time.After(10 * time.Second):
		t.Fatal("drain deadline")
	}
}

// R-YJYM-DVBO
func TestPreparationCompletingAfterDrainAbandonsInsteadOfQueuing(t *testing.T) {
	for _, backwards := range []bool{false, true} {
		t.Run(fmt.Sprint(backwards), func(t *testing.T) {
			h := queuedHarness(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			h.sourceAfter(func(time.Duration) <-chan time.Time {
				// Each preparation resolves the ref, then archives its tree.
				if calls.Add(1) == 4 {
					close(entered)
					<-release
				}
				return make(chan time.Time)
			})
			a := h.run(nil)
			started := h.readNow().UTC().Truncate(time.Second)
			caller := identity.Caller{UserID: "late-owner", RequestID: "late-request"}
			result := make(chan store.Run, 1)
			errs := make(chan error, 1)
			go func() {
				r, e := h.core.Run(context.Background(), h.sc, runs.Request{Input: []byte(`{"late":true}`), Caller: caller, Cause: events.Cause{ID: "late-event"}})
				result <- r
				errs <- e
			}()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("archive did not reach injected timer")
			}
			drained := make(chan struct{})
			go func() { h.core.Drain(context.Background()); close(drained) }()
			until(t, func() bool {
				return h.core.Deliver(context.Background(), events.Delivery{}) == events.Fail(runs.Stopping)
			})
			now := started.Add(3 * time.Second)
			if backwards {
				now = started.Add(-time.Second)
			}
			h.setNow(now)
			close(release)
			var r store.Run
			select {
			case r = <-result:
			case <-time.After(10 * time.Second):
				t.Fatal("prepared call did not return")
			}
			must(t, <-errs)
			finished := maxTimeForQueueTest(now, started)
			if h.record(r.ID) != r || r.Status != store.StatusFailed || r.Reason != store.ReasonQueueAbandoned || r.SHA != h.sha || r.Ref != h.sc.Ref || r.User != caller.UserID || r.RequestID != caller.RequestID || r.Trigger != store.TriggerEvent || r.Event != "late-event" || r.Started != started || r.Finished != finished || r.StdoutBytes != 0 || r.StderrBytes != 0 || r.Truncated {
				t.Fatal(r)
			}
			if string(read(t, filepath.Join(h.core.Folder(r), runs.InputFile))) != `{"late":true}` || !reflect.DeepEqual(entries(t, h.core.Folder(r)), []string{runs.InputFile, runs.OutDir, runs.TreeDir}) || len(entries(t, filepath.Join(h.core.Folder(r), runs.OutDir))) != 0 || len(h.timers) != 1 {
				t.Fatal("abandoned preparation folder or process")
			}
			must(t, filepath.WalkDir(filepath.Join(h.core.Folder(r), runs.TreeDir), func(_ string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				i, e := d.Info()
				if e == nil && i.Mode().Perm()&0222 != 0 {
					t.Error("writable abandoned tree")
				}
				return e
			}))
			h.finished(r.ID)
			assertNoStart(t, h, r.ID)
			h.release(a)
			h.finished(a.ID)
			select {
			case <-drained:
			case <-time.After(10 * time.Second):
				t.Fatal("drain did not finish")
			}
		})
	}
}

func maxTimeForQueueTest(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

// R-YA7F-BPE4 R-YCN8-38VI
func TestQueuedLaunchFailurePassesSlotOn(t *testing.T) {
	h := queuedHarness(t)
	python, e := exec.LookPath(runner.Interpreter)
	must(t, e)
	bin := t.TempDir()
	link := filepath.Join(bin, runner.Interpreter)
	must(t, os.Symlink(python, link))
	h.cfg.Path = bin
	h.core = runs.New(h.cfg)
	a := h.run(nil)
	b := h.run(nil)
	c := h.run(nil)
	must(t, os.Remove(link))
	h.release(a)
	h.finished(a.ID)
	for _, r := range []store.Run{b, c} {
		h.finished(r.ID)
		ended := h.record(r.ID)
		if ended.Status != store.StatusFailed || ended.Reason != store.ReasonStartFailed || ended.ExitCode != 0 || ended.StdoutBytes != 0 || ended.StderrBytes != 0 || ended.Truncated {
			t.Fatal(ended)
		}
		assertNoStart(t, h, r.ID)
		if got := entries(t, h.core.Folder(r)); !reflect.DeepEqual(got, []string{runs.InputFile, runs.OutDir, runs.TreeDir}) {
			t.Fatal(got)
		}
	}
}

// R-W56E-B9YK R-XPH4-TLSB
func TestCoreSuppliesAndRemovesPerRunCgroup(t *testing.T) {
	h := queuedHarness(t)
	h.cfg.Cgroup = t.TempDir()
	h.cfg.RunMemoryMaxBytes = 123456
	h.cfg.RunPidsMax = 7
	h.core = runs.New(h.cfg)
	a := h.run(nil)
	b := h.run(nil)
	check := func(r store.Run) {
		t.Helper()
		group := filepath.Join(h.cfg.Cgroup, r.ID)
		until(t, func() bool { return fileSizeForTest(filepath.Join(h.core.Folder(r), runs.StdoutFile)) > 0 })
		pid := strings.TrimSpace(string(read(t, filepath.Join(h.core.Folder(r), runs.StdoutFile))))
		if string(read(t, filepath.Join(group, "cgroup.procs"))) != pid || string(read(t, filepath.Join(group, "memory.max"))) != "123456" || string(read(t, filepath.Join(group, "pids.max"))) != "7" {
			t.Fatal("cgroup wiring")
		}
	}
	check(a)
	h.release(a)
	h.finished(a.ID)
	if _, e := os.Stat(filepath.Join(h.cfg.Cgroup, a.ID)); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	until(t, func() bool { return h.record(b.ID).Status == store.StatusRunning })
	check(b)
	h.release(b)
	h.finished(b.ID)
	if got := entries(t, h.cfg.Cgroup); len(got) != 0 {
		t.Fatal(got)
	}
}

// R-XT4T-YX0E
func TestDeleteRemovesItsQueueBeforeFreeingSlots(t *testing.T) {
	h := queuedHarness(t)
	a := h.run(nil)
	b := h.run(nil)
	other, e := h.st.Create(context.Background(), store.Draft{Owner: "owner", Name: "survivor", Repo: h.sc.Repo, Ref: "main"})
	must(t, e)
	r, e := h.core.Run(context.Background(), other, runs.Request{Caller: identity.Caller{UserID: "owner", RequestID: "other"}})
	must(t, e)
	assertQueued(t, h, r)
	otherFolder := runFolderSnapshot(t, h.core.Folder(r))
	must(t, h.core.Delete(context.Background(), h.sc.ID))
	until(t, func() bool {
		finished := map[string]bool{}
		for _, ev := range h.sink.capture.Events() {
			if ev.Name == "run.finished" {
				id, _ := ev.Attrs["run"].(string)
				finished[id] = true
			}
		}
		return finished[a.ID] && finished[b.ID]
	})
	for _, target := range []store.Run{a, b} {
		if _, e := h.st.RunByID(context.Background(), target.ID); !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
	assertNoStart(t, h, b.ID)
	if _, e := os.Stat(filepath.Join(h.cfg.Runs, h.sc.ID)); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	// A queued survivor eventually starting proves deletion's freed slot cannot launch b.
	until(t, func() bool { return h.record(r.ID).Status == store.StatusRunning })
	after := runFolderSnapshot(t, h.core.Folder(r))
	for path, entry := range otherFolder {
		if after[path] != entry {
			t.Fatal("survivor folder entry changed", path)
		}
	}
	h.release(r)
	h.finished(r.ID)
	assertNoStart(t, h, b.ID)
}
func fileSizeForTest(path string) int64 {
	s, e := os.Stat(path)
	if e != nil {
		return 0
	}
	return s.Size()
}

// R-RK61-HGTW
func TestQueuedCatalogStartFailureKillsGroupAndPassesSlot(t *testing.T) {
	h := queuedHarness(t)
	h.cfg.Cgroup = t.TempDir()
	original := h.cfg.ScriptAfter
	var calls atomic.Int64
	h.cfg.ScriptAfter = func(d time.Duration) <-chan time.Time {
		n := calls.Add(1)
		if n == 2 {
			h.db.SetFailing(true)
		}
		if n == 3 {
			h.db.SetFailing(false)
		}
		return original(d)
	}
	h.core = runs.New(h.cfg)
	a := h.run(nil)
	b := h.run(nil)
	c := h.run(nil)
	h.release(a)
	h.finished(a.ID)
	until(t, func() bool { return calls.Load() == 3 })
	until(t, func() bool { return h.record(c.ID).Status == store.StatusRunning })
	if h.record(b.ID).Status != store.StatusQueued {
		t.Fatal("start failure changed record")
	}
	assertNoStart(t, h, b.ID)
	for _, e := range h.sink.capture.Events() {
		if e.Attrs["run"] == b.ID {
			t.Fatal(e)
		}
	}
	if _, e := os.Stat(filepath.Join(h.cfg.Cgroup, b.ID)); !os.IsNotExist(e) {
		t.Fatal("failed admission group left", e)
	}
	h.release(c)
	h.finished(c.ID)
}

// R-YOLU-8NDI
func TestAdmissionCopyConstants(t *testing.T) {
	const admissionCopy = runs.Stopping + runs.Starting + runs.NoEventID + runs.NoRuns + runs.QueueFull
	_ = admissionCopy
	for _, c := range []struct{ format, verb string }{{runs.NoRuns, "%s"}, {runs.QueueFull, "%d"}} {
		if strings.Count(c.format, c.verb) != 1 || strings.Count(c.format, "%") != 1 {
			t.Fatal("invalid admission format", c.format)
		}
	}
}

package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

func filesRecord(t *testing.T, f *coreFixture, n int, status string, at time.Time) store.Run {
	t.Helper()
	r := store.Run{ID: fmt.Sprintf("prr_%016x", n), Prompt: f.p.ID, Model: f.p.Model, User: "alice", RequestID: "saved-request", Trigger: store.TriggerManual, Status: status, Started: at.UTC().Truncate(time.Second)}
	terminal := status != store.StatusRunning && status != store.StatusQueued && status != store.StatusFailed
	if terminal {
		r.Status = store.StatusRunning
	}
	if status == store.StatusFailed {
		r.Finished = r.Started
		r.Reason = store.ReasonStartFailed
	}
	var err error
	r, err = f.s.AddRun(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if terminal {
		ending := store.Ending{Status: status, Finished: r.Started}
		if status == store.StatusExited {
			ending.ExitCode = 2
			ending.StdoutBytes = 4
		}
		r, err = f.s.FinishRun(context.Background(), r.ID, ending)
		if err != nil {
			t.Fatal(err)
		}
	}
	return r
}
func filesWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func filesSnapshot(t *testing.T, path string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		v := info.Mode().String()
		if info.Mode().IsRegular() {
			v += ":" + string(testRead(t, p))
		}
		got[p] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func filesNoEvents(t *testing.T, f *coreFixture) {
	t.Helper()
	f.w.Shutdown(context.Background(), "test finished")
	for _, e := range f.sink.capture.Events() {
		if e.Name == "run.started" || e.Name == "run.finished" {
			t.Fatal(e)
		}
	}
}
func filesEndingEvent(t *testing.T, f *coreFixture, r store.Run, d time.Duration) {
	t.Helper()
	e := f.event(t, "run.finished", r.ID)
	testEqual(t, e.Attrs, FinishedAttrs(r, d))
	testEqual(t, e.User, r.User)
	testEqual(t, e.RequestID, r.RequestID)
}

// R-NLR3-9OJU R-NRUL-6J9B R-NT2H-KB00 R-NUAD-Y2QP
func TestCoreFileQueries(t *testing.T) {
	f := newCoreFixture(t)
	running := filesRecord(t, f, 1, store.StatusRunning, testInstant)
	path := f.c.Folder(running)
	testEqual(t, path, filepath.Join(f.cfg.Runs, running.Prompt, running.ID))
	testEqual(t, f.c.Gone(running), true)
	if _, err := os.Lstat(f.cfg.Runs); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	filesWrite(t, filepath.Join(path, StdoutFile), []byte("12345"))
	testEqual(t, f.c.Gone(running), false)
	a, b := f.c.Sizes(running)
	testEqual(t, []int64{a, b}, []int64{5, 0})
	filesWrite(t, filepath.Join(path, StderrFile), []byte("stderr"))
	a, b = f.c.Sizes(running)
	testEqual(t, []int64{a, b}, []int64{5, 6})
	if err := os.Remove(filepath.Join(path, StderrFile)); err != nil {
		t.Fatal(err)
	}
	stored, err := f.s.RunByID(context.Background(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, stored, running)
	// Nonregular entries, including links to regular files, contribute no bytes.
	if err := os.Remove(filepath.Join(path, StdoutFile)); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	filesWrite(t, target, []byte("longer"))
	if err := os.Symlink(target, filepath.Join(path, StdoutFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, StderrFile), 0700); err != nil {
		t.Fatal(err)
	}
	a, b = f.c.Sizes(running)
	testEqual(t, []int64{a, b}, []int64{0, 0})
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	testEqual(t, f.c.Gone(running), true)
	stored, err = f.s.RunByID(context.Background(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, stored, running)
	for _, kind := range []string{"file", "link"} {
		if kind == "file" {
			filesWrite(t, path, []byte("entry"))
		} else {
			if err := os.Symlink(filepath.Dir(target), path); err != nil {
				t.Fatal(err)
			}
		}
		testEqual(t, f.c.Gone(running), true)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	for i, status := range []string{store.StatusQueued, store.StatusExited, store.StatusFailed, store.StatusKilled, store.StatusTimedOut} {
		r := store.Run{ID: fmt.Sprintf("prr_%016x", i+10), Prompt: f.p.ID, Status: status, StdoutBytes: 4, StderrBytes: 7}
		a, b = f.c.Sizes(r)
		testEqual(t, []int64{a, b}, []int64{4, 7})
	}
	ended := filesRecord(t, f, 2, store.StatusExited, testInstant)
	a, b = f.c.Sizes(ended)
	testEqual(t, []int64{a, b}, []int64{4, 0})
}

// R-OCKV-OMV4 R-1O82-EMWJ
func TestCoreCancelUnownedRecord(t *testing.T) {
	for _, status := range []string{store.StatusRunning, store.StatusQueued} {
		t.Run(status, func(t *testing.T) {
			f := newCoreFixture(t)
			r := filesRecord(t, f, 1, status, testInstant.Add(-time.Hour))
			folder := f.c.Folder(r)
			filesWrite(t, filepath.Join(folder, StdoutFile), []byte("five!"))
			filesWrite(t, filepath.Join(folder, StderrFile), []byte("err"))
			cost := agentkit.Cost(7)
			line, err := json.Marshal(agentkit.LogRecord{Type: agentkit.RecordUsage, Usage: &agentkit.Usage{InputTokens: 5}, Cost: &cost})
			if err != nil {
				t.Fatal(err)
			}
			filesWrite(t, filepath.Join(folder, TranscriptFile), line)
			before := filesSnapshot(t, folder)
			ended, err := f.c.Cancel(context.Background(), r.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := r
			want.Status = store.StatusKilled
			want.Finished = testInstant.UTC().Truncate(time.Second)
			want.StdoutBytes = 0
			want.StderrBytes = 0
			want.Usage = store.Usage{}
			if status == store.StatusRunning {
				want.StdoutBytes = 5
				want.StderrBytes = 3
				want.Usage = store.Usage{Calls: 1, InputTokens: 5, CostNanos: 7}
			}
			testEqual(t, ended, want)
			saved, err := f.s.RunByID(context.Background(), r.ID)
			if err != nil {
				t.Fatal(err)
			}
			testEqual(t, saved, want)
			filesEndingEvent(t, f, ended, testInstant.Sub(r.Started))
			testEqual(t, filesSnapshot(t, folder), before)
			select {
			case <-f.timers:
				t.Fatal("started process")
			default:
			}
		})
	}
}

// R-PRSN-R00A R-PU8G-IJHO
func TestCoreCancelRefusals(t *testing.T) {
	f := newCoreFixture(t)
	for i, status := range []string{store.StatusExited, store.StatusFailed, store.StatusKilled, store.StatusTimedOut} {
		r := filesRecord(t, f, i+1, status, testInstant)
		got, err := f.c.Cancel(context.Background(), r.ID)
		testEqual(t, got, store.Run{})
		if !errors.Is(err, store.ErrEnded) {
			t.Fatal(err)
		}
		saved, err := f.s.RunByID(context.Background(), r.ID)
		if err != nil {
			t.Fatal(err)
		}
		testEqual(t, saved, r)
	}
	_, err := f.c.Cancel(context.Background(), "prr_ffffffffffffffff")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	filesNoEvents(t, f)
}

// R-1QNV-66DX
func TestCoreRecoverRecords(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.Runs = filepath.Join(t.TempDir(), "absent", "nested", "runs")
	f.rebuild()
	if err := f.c.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(f.cfg.Runs)
	if err != nil || !info.IsDir() {
		t.Fatal(info, err)
	}
	var rs []store.Run
	for i, status := range []string{store.StatusRunning, store.StatusRunning, store.StatusQueued, store.StatusRunning} {
		at := testInstant.Add(-time.Hour)
		if i == 3 {
			at = testInstant.Add(time.Hour)
		}
		r := filesRecord(t, f, i+1, status, at)
		rs = append(rs, r)
		filesWrite(t, filepath.Join(f.c.Folder(r), InputFile), []byte("saved"))
		filesWrite(t, filepath.Join(f.c.Folder(r), StdoutFile), []byte("12345"))
		filesWrite(t, filepath.Join(f.c.Folder(r), StderrFile), []byte("err"))
	}
	cost := agentkit.Cost(7)
	line, err := json.Marshal(agentkit.LogRecord{Type: agentkit.RecordUsage, Usage: &agentkit.Usage{InputTokens: 5, CachedTokens: 2, OutputTokens: 3, ReasoningTokens: 1}, Cost: &cost})
	if err != nil {
		t.Fatal(err)
	}
	filesWrite(t, filepath.Join(f.c.Folder(rs[0]), TranscriptFile), append(append(append(line, '\n'), line...), []byte("\npartial{")...))
	before := filesSnapshot(t, f.cfg.Runs)
	if err := f.c.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i, r := range rs {
		want := r
		want.Status = store.StatusKilled
		want.Finished = testInstant.UTC().Truncate(time.Second)
		if want.Finished.Before(r.Started) {
			want.Finished = r.Started
		}
		want.StdoutBytes = 5
		want.StderrBytes = 3
		if i == 0 {
			want.Usage = store.Usage{Calls: 2, InputTokens: 10, CachedTokens: 4, OutputTokens: 6, ReasoningTokens: 2, CostNanos: 14}
		}
		if r.Status == store.StatusQueued {
			want.Status = store.StatusFailed
			want.Reason = store.ReasonQueueAbandoned
			want.StdoutBytes = 0
			want.StderrBytes = 0
		}
		got, err := f.s.RunByID(context.Background(), r.ID)
		if err != nil {
			t.Fatal(err)
		}
		testEqual(t, got, want)
		filesEndingEvent(t, f, got, testInstant.Sub(r.Started))
	}
	testEqual(t, filesSnapshot(t, f.cfg.Runs), before)
	select {
	case <-f.timers:
		t.Fatal("recovery started process")
	default:
	}
	f.d.SetFailing(true)
	err = f.c.Recover(context.Background())
	f.d.SetFailing(false)
	if err == nil {
		t.Fatal("catalog failure ignored")
	}
}

// R-Q2RR-6XOJ
func TestCorePruneSelection(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.KeepDays = 1
	f.cfg.KeepCount = 1
	f.rebuild()
	rs := []store.Run{filesRecord(t, f, 1, store.StatusExited, testInstant.Add(-5*24*time.Hour)), filesRecord(t, f, 2, store.StatusExited, testInstant.Add(-4*24*time.Hour)), filesRecord(t, f, 3, store.StatusRunning, testInstant.Add(-3*24*time.Hour)), filesRecord(t, f, 4, store.StatusQueued, testInstant.Add(-2*24*time.Hour)), filesRecord(t, f, 5, store.StatusExited, testInstant)}
	for _, r := range rs {
		filesWrite(t, filepath.Join(f.c.Folder(r), InputFile), []byte(r.ID))
	}
	expected, err := f.s.PastKeeping(context.Background(), testInstant, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, len(expected), 2)
	removed := map[string]bool{}
	for _, r := range expected {
		removed[r.ID] = true
	}
	out, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()
	diagnostic, err := os.Create(filepath.Join(t.TempDir(), "err"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = diagnostic.Close() }()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, diagnostic
	err = f.c.Prune(context.Background())
	os.Stdout, os.Stderr = oldOut, oldErr
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []*os.File{out, diagnostic} {
		info, err := file.Stat()
		if err != nil || info.Size() != 0 {
			t.Fatal(info, err)
		}
	}
	for _, r := range rs {
		got, err := f.s.RunByID(context.Background(), r.ID)
		if removed[r.ID] {
			if !errors.Is(err, store.ErrNotFound) || !f.c.Gone(r) {
				t.Fatal(got, err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			testEqual(t, got, r)
			testEqual(t, string(testRead(t, filepath.Join(f.c.Folder(r), InputFile))), r.ID)
		}
	}
	info, err := os.Stat(filepath.Join(f.cfg.Runs, f.p.ID))
	if err != nil || !info.IsDir() {
		t.Fatal(info, err)
	}
	f.d.SetFailing(true)
	err = f.c.Prune(context.Background())
	f.d.SetFailing(false)
	if err == nil {
		t.Fatal("catalog failure ignored")
	}
	filesNoEvents(t, f)
}

// R-Q3ZN-KPF8
func TestCorePruneKeepsUnremovable(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.KeepDays = 1
	f.cfg.KeepCount = 1
	f.rebuild()
	blocked := filesRecord(t, f, 1, store.StatusExited, testInstant.Add(-4*24*time.Hour))
	other, err := f.s.Create(context.Background(), store.Draft{Owner: f.p.Owner, OwnerEmail: f.p.OwnerEmail, Name: "other", Model: f.p.Model, Prompt: "other"})
	if err != nil {
		t.Fatal(err)
	}
	saved := f.p
	f.p = other
	removable := filesRecord(t, f, 2, store.StatusExited, testInstant.Add(-4*24*time.Hour))
	_ = filesRecord(t, f, 3, store.StatusExited, testInstant)
	f.p = saved
	_ = filesRecord(t, f, 4, store.StatusExited, testInstant)
	for _, r := range []store.Run{blocked, removable} {
		filesWrite(t, filepath.Join(f.c.Folder(r), InputFile), []byte("keep"))
	}
	parent := filepath.Dir(f.c.Folder(blocked))
	original, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, original.Mode().Perm()&^0200); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, original.Mode().Perm()) })
	if err := f.c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.RunByID(context.Background(), blocked.ID)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, got, blocked)
	testEqual(t, f.c.Gone(blocked), false)
	_, err = f.s.RunByID(context.Background(), removable.ID)
	if !errors.Is(err, store.ErrNotFound) || !f.c.Gone(removable) {
		t.Fatal(err)
	}
}

// R-Q0BY-FE75
func TestCoreRemovalPreservesOutside(t *testing.T) {
	for _, method := range []string{"prune", "delete"} {
		t.Run(method, func(t *testing.T) {
			f := newCoreFixture(t)
			f.cfg.KeepDays = 1
			f.cfg.KeepCount = 1
			f.rebuild()
			r := filesRecord(t, f, 1, store.StatusExited, testInstant.Add(-4*24*time.Hour))
			_ = filesRecord(t, f, 2, store.StatusExited, testInstant)
			target := filepath.Join(t.TempDir(), "outside")
			filesWrite(t, target, []byte("unaltered"))
			if err := os.Chmod(target, 0400); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			nested := filepath.Join(f.c.Folder(r), WorkDir, "locked")
			filesWrite(t, filepath.Join(nested, "inside"), []byte("data"))
			if err := os.Symlink(target, filepath.Join(nested, "link")); err != nil {
				t.Fatal(err)
			}
			locked, err := os.Stat(nested)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(nested, locked.Mode().Perm()&^0200); err != nil {
				t.Fatal(err)
			}
			if method == "prune" {
				err = f.c.Prune(context.Background())
			} else {
				err = f.c.Delete(context.Background(), f.p.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			testEqual(t, f.c.Gone(r), true)
			after, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			testEqual(t, after.Mode(), info.Mode())
			testEqual(t, string(testRead(t, target)), "unaltered")
		})
	}
}

// R-Q57J-YH5X
func TestCoreKeepingWaitsForExplicitPruneOrEnding(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.KeepDays = 1
	f.cfg.KeepCount = 1
	var mu sync.Mutex
	now := testInstant
	f.cfg.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	f.rebuild()
	old := filesRecord(t, f, 1, store.StatusExited, testInstant.Add(-time.Hour))
	_ = filesRecord(t, f, 2, store.StatusExited, testInstant)
	filesWrite(t, filepath.Join(f.c.Folder(old), InputFile), []byte("old"))
	if err := f.c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	now = testInstant.Add(3 * 24 * time.Hour)
	mu.Unlock()
	entered, release := make(chan struct{}), make(chan struct{})
	server := provider(t, func(w http.ResponseWriter, _ *http.Request) { close(entered); <-release; answer(w, "done") })
	f.cfg.BaseURL = server.URL
	f.rebuild()
	r := f.start(t, f.p, f.request())
	testReceive(t, entered)
	_, err := f.c.Cancel(context.Background(), old.ID)
	if !errors.Is(err, store.ErrEnded) {
		t.Fatal(err)
	}
	_ = f.c.Folder(old)
	testEqual(t, f.c.Gone(old), false)
	a, b := f.c.Sizes(old)
	testEqual(t, []int64{a, b}, []int64{4, 0})
	got, err := f.s.RunByID(context.Background(), old.ID)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, got, old)
	testEqual(t, string(testRead(t, filepath.Join(f.c.Folder(old), InputFile))), "old")
	close(release)
	f.event(t, "run.finished", r.ID)
	_, err = f.s.RunByID(context.Background(), old.ID)
	if !errors.Is(err, store.ErrNotFound) || !f.c.Gone(old) {
		t.Fatal(err)
	}
}

// R-PU8G-IJHO
func TestCoreCancelMissingLeavesActiveAlone(t *testing.T) {
	f := newCoreFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server := provider(t, func(w http.ResponseWriter, _ *http.Request) { close(entered); <-release; answer(w, "survived") })
	f.cfg.BaseURL = server.URL
	f.rebuild()
	r := f.start(t, f.p, f.request())
	testReceive(t, entered)
	f.event(t, "run.started", r.ID)
	_, err := f.c.Cancel(context.Background(), "prr_ffffffffffffffff")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	got, err := f.s.RunByID(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, got, r)
	select {
	case e := <-f.sink.events:
		if e.Name == "run.finished" {
			t.Fatal(e)
		}
	default:
	}
	close(release)
	f.event(t, "run.finished", r.ID)
	got, err = f.s.RunByID(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, got.Status, store.StatusExited)
	testEqual(t, got.ExitCode, 0)
	testEqual(t, string(testRead(t, filepath.Join(f.c.Folder(r), StdoutFile))), "survived")
}

// R-Q0BY-FE75
func TestCorePrunesRealChildLockedWorkAndLink(t *testing.T) {
	f := newCoreFixture(t)
	f.cfg.KeepDays = 1
	f.cfg.KeepCount = 1
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Path = filepath.Dir(bash)
	target := filepath.Join(t.TempDir(), "outside")
	filesWrite(t, target, []byte("external bytes"))
	if err := os.Chmod(target, 0400); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	command := `mkdir locked; printf '%s' retained > locked/inside; ln -s ` + "'" + strings.ReplaceAll(target, "'", "'\"'\"'") + "'" + ` locked/link; chmod 500 locked`
	round := 0
	server := provider(t, func(w http.ResponseWriter, _ *http.Request) {
		round++
		if round == 1 {
			toolAnswer(w, "Bash", map[string]any{"command": command})
		} else {
			answer(w, "done")
		}
	})
	f.cfg.BaseURL = server.URL
	f.rebuild()
	p := f.p
	p.Tools = []string{agent.GroupBash}
	r := f.start(t, p, f.request())
	f.event(t, "run.finished", r.ID)
	link := filepath.Join(f.c.Folder(r), WorkDir, "locked", "link")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, got, target)
	testEqual(t, string(testRead(t, filepath.Join(f.c.Folder(r), WorkDir, "locked", "inside"))), "retained")
	_ = filesRecord(t, f, 55, store.StatusExited, testInstant.Add(time.Hour))
	f.cfg.Now = func() time.Time { return testInstant.Add(3 * 24 * time.Hour) }
	f.rebuild()
	if err := f.c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	testEqual(t, f.c.Gone(r), true)
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	testEqual(t, after.Mode(), before.Mode())
	testEqual(t, string(testRead(t, target)), "external bytes")
}

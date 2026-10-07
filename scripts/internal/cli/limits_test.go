package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func limitsRestart(t *testing.T, old *runHarness) *runHarness {
	t.Helper()
	h := newHarness(t)
	h.p.Dir = old.p.Dir
	h.repo = old.repo
	h.sha = old.sha
	h.set("REPOS_DIR", filepath.Dir(old.repo))
	h.p.Rand = repeatingRandom(0xa7)
	return h
}
func TestLimitsTreeAcrossRestartAndFrozenEnvironment(t *testing.T) {
	// R-M54Z-47JO R-RP1N-0JSO
	h := newHarness(t)
	script := "pass\n" + "#" + strings.Repeat("x", 1018) + "\n"
	if len(script) != 1025 {
		t.Fatal(len(script))
	}
	h.repository(script)
	h.set("TREE_MAX_BYTES", "1024")
	h.start()
	c := &trailClient{h: h}
	_, _ = c.ok("create", map[string]any{"name": "tree", "repo": "rep_0102030405060708"})
	h.set("TREE_MAX_BYTES", "2048")
	failed, _ := c.ok("run", map[string]any{"name": "tree"})
	if failed["status"] != "failed" || failed["reason"] != "too_large" {
		t.Fatal(failed)
	}
	h.stop()
	next := limitsRestart(t, h)
	next.set("TREE_MAX_BYTES", "2048")
	next.start()
	c = &trailClient{h: next}
	r, _ := c.ok("run", map[string]any{"name": "tree"})
	ended := c.ended(r["id"])
	if ended["status"] != "exited" || ended["exit_code"] != float64(0) {
		t.Fatal(ended)
	}
	prior, _ := c.ok("result", map[string]any{"run": failed["id"]})
	if prior["status"] != "failed" || prior["reason"] != "too_large" {
		t.Fatal(prior)
	}
	next.stop()
}
func TestLimitsStreamsAcrossRestart(t *testing.T) {
	// R-M6CV-HZAD
	h := newHarness(t)
	h.repository("import sys\nsys.stdout.write('s'*2000)\nsys.stderr.write('e'*2000)\n")
	h.set("OUTPUT_MAX_BYTES", "1024")
	h.start()
	c := &trailClient{h: h}
	_, _ = c.ok("create", map[string]any{"name": "output", "repo": "rep_0102030405060708"})
	h.set("OUTPUT_MAX_BYTES", "4096")
	r, _ := c.ok("run", map[string]any{"name": "output"})
	v := c.ended(r["id"])
	if v["stdout"] != strings.Repeat("s", 1024) || v["stderr"] != strings.Repeat("e", 1024) || v["truncated"] != true {
		t.Fatal(v)
	}
	h.stop()
	next := limitsRestart(t, h)
	next.set("OUTPUT_MAX_BYTES", "4096")
	next.start()
	c = &trailClient{h: next}
	r, _ = c.ok("run", map[string]any{"name": "output"})
	v = c.ended(r["id"])
	if v["stdout"] != strings.Repeat("s", 2000) || v["stderr"] != strings.Repeat("e", 2000) || v["truncated"] != false {
		t.Fatal(v)
	}
	next.stop()
}
func TestLimitsIndependentTimers(t *testing.T) {
	// R-LQSZ-UOI1 R-LS0W-8G8Q
	for _, seconds := range []struct {
		operation, script string
		op, sc            time.Duration
	}{{"60", "30", 60 * time.Second, 30 * time.Second}, {"9223372036854775807", "9223372036854775807", time.Duration(1<<63 - 1), time.Duration(1<<63 - 1)}} {
		t.Run(seconds.operation, func(t *testing.T) {
			h := newHarness(t)
			h.repository(gatePython)
			h.set("OPERATION_SECONDS", seconds.operation)
			h.set("SCRIPT_SECONDS", seconds.script)
			h.start()
			c := &trailClient{h: h}
			sc, _ := c.ok("create", map[string]any{"name": "timers", "repo": "rep_0102030405060708"})
			if len(h.gitDurations) != 1 || len(h.durations) != 0 {
				t.Fatalf("create timers git %d script %d", len(h.gitDurations), len(h.durations))
			}
			h.set("OPERATION_SECONDS", "1")
			h.set("SCRIPT_SECONDS", "1")
			r, _ := c.ok("run", map[string]any{"name": "timers"})
			if len(h.gitDurations) != 3 {
				t.Fatalf("owner, resolve, archive want 3 timers got %d", len(h.gitDurations))
			}
			select {
			case d := <-h.durations:
				if d != seconds.sc {
					t.Fatal(d)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("no script timer")
			}
			for i := 0; i < 3; i++ {
				if d := <-h.gitDurations; d != seconds.op {
					t.Fatal(d)
				}
			}
			if len(h.durations) != 0 {
				t.Fatal("extra script timer")
			}
			releaseRun(t, h, sc["id"], r["id"])
			c.ended(r["id"])
			h.stop()
			if len(h.gitDurations) != 0 || len(h.durations) != 0 {
				t.Fatal("non-git operation timer or extra script timer")
			}
		})
	}
}
func TestLimitsAlreadyExpiredOperation(t *testing.T) {
	// R-ICIV-C2AO
	h := newHarness(t)
	h.repository("pass\n")
	var mu sync.Mutex
	expired := false
	h.p.After = func(time.Duration) <-chan time.Time {
		mu.Lock()
		defer mu.Unlock()
		ch := make(chan time.Time, 1)
		if expired {
			ch <- h.now
		}
		return ch
	}
	h.start()
	c := &trailClient{h: h}
	_, _ = c.ok("create", map[string]any{"name": "expire", "repo": "rep_0102030405060708"})
	mu.Lock()
	expired = true
	mu.Unlock()
	r, _ := c.ok("run", map[string]any{"name": "expire"})
	if !store.ValidRunID(r["id"].(string)) || r["status"] != "failed" || r["reason"] != "timed_out" {
		t.Fatal(r)
	}
	assertNoProcess(t, "HOME="+h.root)
	if len(h.durations) != 0 {
		t.Fatal("started script despite expired git")
	}
	h.stop()
}
func TestLimitsPruneUsesStartupSettings(t *testing.T) {
	// R-LPL3-GWRC R-RP1N-0JSO
	h := newHarness(t)
	h.repository("pass\n")
	h.p.Sink = &h.sink.capture
	var mu sync.Mutex
	now := h.now.Add(-5 * 24 * time.Hour)
	h.p.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	h.start()
	c := &trailClient{h: h}
	sc, _ := c.ok("create", map[string]any{"name": "keep", "repo": "rep_0102030405060708"})
	var ids []string
	origins := map[any]string{}
	for _, age := range []time.Duration{5 * 24 * time.Hour, 4 * 24 * time.Hour, 2 * 24 * time.Hour, 24 * time.Hour, 0} {
		mu.Lock()
		now = h.now.Add(-age)
		mu.Unlock()
		r, origin := c.ok("run", map[string]any{"name": "keep"})
		c.ended(r["id"])
		waitLimitsFinished(t, c.h, r["id"])
		ids = append(ids, r["id"].(string))
		origins[r["id"]] = origin
	}
	h.stop()
	for _, e := range h.sink.capture.Events() {
		if strings.HasPrefix(e.Name, "run.") {
			origin, ok := origins[e.Attrs["run"]]
			if !ok || origin != e.RequestID {
				t.Fatal(e)
			}
		}
	}
	database := filepath.Join(h.p.Dir, "state", "scripts.db")
	sDB, e := db.Open(context.Background(), db.Config{Path: database, Migrations: scripts.Migrations(), Now: func() time.Time { return h.now }})
	mustCLI(t, e)
	s := store.New(sDB, store.Config{Now: func() time.Time { return h.now }, Rand: &countingRandom{}})
	past, e := s.PastKeeping(context.Background(), h.now, 3, 2)
	mustCLI(t, e)
	mustCLI(t, sDB.Close())
	if len(past) != 2 {
		t.Fatal(past)
	}
	removed := map[string]bool{}
	for _, r := range past {
		removed[r.ID] = true
	}
	if !removed[ids[0]] || !removed[ids[1]] {
		t.Fatal(past)
	}
	next := limitsRestart(t, h)
	next.p.Sink = &next.sink.capture
	mu.Lock()
	now = h.now
	mu.Unlock()
	next.p.Now = h.p.Now
	next.set("RUN_KEEP_DAYS", "3")
	next.set("RUN_KEEP_COUNT", "2")
	next.start()
	next.set("RUN_KEEP_DAYS", "100")
	next.set("RUN_KEEP_COUNT", "100")
	c = &trailClient{h: next}
	assertKeptRuns(t, c, sc["id"].(string), "keep", ids, removed)
	// The two-day run is beyond the count but inside the three-day age limit.
	// At the ending prune it is now older than three days; the one-day run is still inside.
	mu.Lock()
	now = h.now.Add(36 * time.Hour)
	mu.Unlock()
	r, origin := c.ok("run", map[string]any{"name": "keep"})
	c.ended(r["id"])
	waitLimitsFinished(t, c.h, r["id"])
	ids = append(ids, r["id"].(string))
	removed[ids[2]] = true
	assertKeptRuns(t, c, sc["id"].(string), "keep", ids, removed)
	next.stop()
	// Startup pruning and the ending prune emit no domain event of their own.
	es := next.sink.capture.Events()
	for _, e := range es {
		if domain(e) {
			if !strings.HasPrefix(e.Name, "run.") || e.Attrs["run"] != r["id"] || e.RequestID != origin || e.User != "owner" {
				t.Fatalf("prune emitted a domain event: %#v", e)
			}
		}
	}
	trailEvent(t, es, "run.started", r["id"])
	trailEvent(t, es, "run.finished", r["id"])
	// A later process uses its new age and count values over the same durable state.
	third := limitsRestart(t, next)
	third.p.Rand = repeatingRandom(0xc8)
	third.p.Now = h.p.Now
	third.set("RUN_KEEP_DAYS", "1")
	third.set("RUN_KEEP_COUNT", "1")
	third.start()
	c = &trailClient{h: third}
	removed[ids[3]] = true
	removed[ids[4]] = true
	assertKeptRuns(t, c, sc["id"].(string), "keep", ids, removed)
	third.stop()
}
func assertKeptRuns(t *testing.T, c *trailClient, script, name string, ids []string, removed map[string]bool) {
	t.Helper()
	v, _ := c.ok("runs", map[string]any{"name": name})
	entries := v["runs"].([]any)
	kept := map[string]bool{}
	for _, entry := range entries {
		id := entry.(map[string]any)["id"].(string)
		if removed[id] {
			t.Fatalf("pruned record remained %s", id)
		}
		kept[id] = true
	}
	expected := 0
	for _, id := range ids {
		_, e := os.Stat(filepath.Join(c.h.p.Dir, "state", "runs", script, id))
		if removed[id] {
			if !os.IsNotExist(e) {
				t.Fatalf("pruned folder %s: %v", id, e)
			}
		} else {
			expected++
			if !kept[id] {
				t.Fatalf("kept record removed %s", id)
			}
			mustCLI(t, e)
		}
	}
	if len(entries) != expected {
		t.Fatalf("kept %d runs want %d", len(entries), expected)
	}
}

func TestLimitsPruneCountAtStartup(t *testing.T) {
	// R-LPL3-GWRC R-RP1N-0JSO
	h := newHarness(t)
	h.repository("pass\n")
	h.start()
	c := &trailClient{h: h}
	sc, _ := c.ok("create", map[string]any{"name": "count", "repo": "rep_0102030405060708"})
	var ids []string
	for i := 0; i < 3; i++ {
		r, _ := c.ok("run", map[string]any{"name": "count"})
		c.ended(r["id"])
		waitLimitsFinished(t, c.h, r["id"])
		ids = append(ids, r["id"].(string))
	}
	h.stop()
	next := limitsRestart(t, h)
	next.p.Now = func() time.Time { return h.now.Add(4 * 24 * time.Hour) }
	next.set("RUN_KEEP_DAYS", "3")
	next.set("RUN_KEEP_COUNT", "2")
	next.start()
	next.set("RUN_KEEP_COUNT", "100")
	c = &trailClient{h: next}
	removed := map[string]bool{ids[0]: true}
	assertKeptRuns(t, c, sc["id"].(string), "count", ids, removed)
	r, _ := c.ok("run", map[string]any{"name": "count"})
	c.ended(r["id"])
	waitLimitsFinished(t, c.h, r["id"])
	ids = append(ids, r["id"].(string))
	removed[ids[1]] = true
	assertKeptRuns(t, c, sc["id"].(string), "count", ids, removed)
	next.stop()
}

// A final status precedes its prune; the matching event is the completion boundary.
func waitLimitsFinished(t *testing.T, h *runHarness, id any) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		for _, e := range h.sink.capture.Events() {
			if e.Name == "run.finished" && e.Attrs["run"] == id {
				return
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("missing completed prune for run %v", id)
		default:
			runtime.Gosched()
		}
	}
}

package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func releaseQueued(t *testing.T, h *runHarness, script, run any) {
	t.Helper()
	mustCLI(t, os.WriteFile(filepath.Join(h.p.Dir, "state", "runs", script.(string), run.(string), "out", "release"), nil, 0600))
}
func noRunStarted(t *testing.T, es []telemetry.Event, run any) {
	t.Helper()
	for _, ev := range es {
		if ev.Name == "run.started" && ev.Attrs["run"] == run {
			t.Fatal(ev)
		}
	}
}
func assertQueuedFinished(t *testing.T, es []telemetry.Event, run any, origin, status, reason string) {
	t.Helper()
	e := trailEvent(t, es, "run.finished", run)
	want := telemetry.Attrs{"run": run, "status": status, "duration_us": int64(0), "truncated": false}
	if reason != "" {
		want["reason"] = reason
	}
	expectAttrs(t, e, want)
	if e.RequestID != origin || e.User != "owner" {
		t.Fatal(e)
	}
}

func TestQueueAdmissionPromotionAndFrozenBounds(t *testing.T) {
	// R-LGBN-X90K R-RP1N-0JSO R-TODC-EF3T R-TPL8-S6UI
	h := newHarness(t)
	h.repository(waitingMain)
	h.set("RUN_MAX_ACTIVE", "1")
	h.set("RUN_MAX_QUEUED", "1")
	h.set("RUN_MEMORY_MAX_BYTES", "1000000")
	h.set("RUN_PIDS_MAX", "7")
	h.p.Sink = &h.sink.capture
	h.start()
	c := &trailClient{h: h}
	sc, _ := c.ok("create", map[string]any{"name": "queue", "repo": "rep_0102030405060708"})
	first, firstOrigin := c.ok("run", map[string]any{"name": "queue"})
	if first["status"] != "running" {
		t.Fatal(first)
	}
	h.set("RUN_MAX_ACTIVE", "2")
	h.set("RUN_MAX_QUEUED", "2")
	h.set("RUN_MEMORY_MAX_BYTES", "2000000")
	h.set("RUN_PIDS_MAX", "8")
	h.set("RUNS_MEMORY_MAX_BYTES", "1000000")
	h.set("RUNS_CPU_PERCENT", "50")
	for file, want := range map[string]string{"memory.max": "1000000", "pids.max": "7"} {
		b, e := os.ReadFile(filepath.Clean(filepath.Join(h.p.Cgroup, "runs", first["id"].(string), file)))
		mustCLI(t, e)
		if strings.TrimSpace(string(b)) != want {
			t.Fatal(file, string(b))
		}
	}
	second, secondOrigin := c.ok("run", map[string]any{"name": "queue"})
	if second["status"] != "queued" {
		t.Fatal(second)
	}
	r, e := h.result("run", map[string]any{"name": "queue"})
	mustCLI(t, e)
	b, e := r.MarshalJSON()
	mustCLI(t, e)
	if !r.IsError() || !strings.Contains(string(b), "the run queue is full (1 queued); try again later") {
		t.Fatal(string(b))
	}
	noRunStarted(t, h.sink.capture.Events(), second["id"])
	releaseQueued(t, h, sc["id"], first["id"])
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		found := false
		for _, e := range h.sink.capture.Events() {
			if e.Name == "run.started" && e.Attrs["run"] == second["id"] {
				found = true
			}
		}
		if found {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("queue did not promote")
		default:
		}
	}
	releaseQueued(t, h, sc["id"], second["id"])
	c.ended(second["id"])
	h.stop()
	es := h.sink.capture.Events()
	start := trailEvent(t, es, "run.started", second["id"])
	expectAttrs(t, start, telemetry.Attrs{"run": second["id"], "script": sc["id"], "sha": second["sha"], "trigger": "manual"})
	if start.RequestID != secondOrigin || start.User != "owner" {
		t.Fatal(start)
	}
	state := 0
	for _, e := range es {
		if e.Name == "tool.called" && e.RequestID == secondOrigin {
			if state != 0 {
				t.Fatal("early queued event")
			}
			state = 1
		}
		if e.Name == "run.finished" && e.Attrs["run"] == first["id"] {
			if state != 1 {
				t.Fatal("first finish order")
			}
			state = 2
		}
		if e.Name == "run.started" && e.Attrs["run"] == second["id"] {
			if state != 2 {
				t.Fatal("promotion order")
			}
			state = 3
		}
		if e.Name == "run.finished" && e.Attrs["run"] == second["id"] {
			if state != 3 || e.Attrs["status"] != "exited" || e.Attrs["exit_code"] != int64(0) || e.RequestID != secondOrigin || e.User != "owner" {
				t.Fatal(e)
			}
			state = 4
		}
	}
	if state != 4 {
		t.Fatal(state)
	}
	firstEnd := trailEvent(t, es, "run.finished", first["id"])
	if firstEnd.RequestID != firstOrigin {
		t.Fatal(firstEnd)
	}
	for file, want := range map[string]string{"memory.max": "536870912", "cpu.max": "100000 100000"} {
		b, e := os.ReadFile(filepath.Clean(filepath.Join(h.p.Cgroup, "runs", file)))
		mustCLI(t, e)
		if string(b) != want {
			t.Fatal(file, string(b))
		}
	}
}

func TestQueuedCancelAndDeleteTrail(t *testing.T) {
	// R-RLDX-V8KL
	for _, action := range []string{"cancel", "delete"} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t)
			h.repository(waitingMain)
			h.set("RUN_MAX_ACTIVE", "1")
			h.p.Sink = &h.sink.capture
			h.start()
			c := &trailClient{h: h}
			sc, _ := c.ok("create", map[string]any{"name": "queue", "repo": "rep_0102030405060708"})
			first, _ := c.ok("run", map[string]any{"name": "queue"})
			second, origin := c.ok("run", map[string]any{"name": "queue"})
			if second["status"] != "queued" {
				t.Fatal(second)
			}
			args := map[string]any{"run": second["id"]}
			if action == "delete" {
				args = map[string]any{"name": "queue"}
			}
			_, actionID := c.ok(action, args)
			if action == "cancel" {
				releaseQueued(t, h, sc["id"], first["id"])
				c.ended(first["id"])
			}
			h.stop()
			es := h.sink.capture.Events()
			assertQueuedFinished(t, es, second["id"], origin, "killed", "")
			noRunStarted(t, es, second["id"])
			seen := false
			for _, e := range trailWindow(t, es, actionID) {
				if e.Name == "run.finished" && e.Attrs["run"] == second["id"] {
					seen = true
				}
				if e.Name == "tool.called" && !seen {
					t.Fatal("ending outside cancel/delete window")
				}
				if e.Name == "script.deleted" && !seen {
					t.Fatal("delete before ending")
				}
			}
		})
	}
}

func TestDrainAbandonsQueueBeforeRunningProcessEnds(t *testing.T) {
	// R-LIRG-OSHY R-TS11-JQBW
	h := newHarness(t)
	h.repository(waitingMain)
	h.set("RUN_MAX_ACTIVE", "1")
	h.set("DRAIN_SECONDS", "5")
	h.p.Sink = &h.sink.capture
	h.start()
	c := &trailClient{h: h}
	sc, _ := c.ok("create", map[string]any{"name": "queue", "repo": "rep_0102030405060708"})
	first, _ := c.ok("run", map[string]any{"name": "queue"})
	second, origin := c.ok("run", map[string]any{"name": "queue"})
	h.cancel(context.Canceled)
	handle, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, e)
	defer func() { mustCLI(t, handle.Close()) }()
	st := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		r, e := st.RunByID(context.Background(), second["id"].(string))
		mustCLI(t, e)
		if r.Status == store.StatusFailed && r.Reason == store.ReasonQueueAbandoned {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("queue was not abandoned promptly")
		default:
		}
	}
	r, e := st.RunByID(context.Background(), first["id"].(string))
	mustCLI(t, e)
	if r.Status != store.StatusRunning {
		t.Fatal(r)
	}
	folder := filepath.Join(h.p.Dir, "state", "runs", sc["id"].(string), second["id"].(string))
	for _, name := range []string{"stdout", "stderr"} {
		if _, e := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(e) {
			t.Fatalf("queued %s %v", name, e)
		}
	}
	releaseQueued(t, h, sc["id"], first["id"])
	if code := h.finish(); code != cli.ExitSuccess || h.stderr.String() != "" {
		t.Fatal(code, h.stderr.String())
	}
	es := h.sink.capture.Events()
	assertQueuedFinished(t, es, second["id"], origin, "failed", "queue_abandoned")
	noRunStarted(t, es, second["id"])
	ended := false
	for _, e := range es {
		if e.Name == "run.finished" && e.Attrs["run"] == second["id"] {
			ended = true
		}
		if e.Name == "service.stopping" && !ended {
			t.Fatal("stop before queue abandoned")
		}
	}
}

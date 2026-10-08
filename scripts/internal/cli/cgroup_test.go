package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func TestCgroupPreparedBeforeReady(t *testing.T) {
	// R-HBE1-NQ3X
	for _, cpu := range []string{"", "250", strconv.FormatInt(math.MaxInt64, 10)} {
		t.Run(cpu, func(t *testing.T) {
			h := newHarness(t)
			memory, quota := "536870912", "100000"
			if cpu != "" {
				h.set("RUNS_CPU_PERCENT", cpu)
				h.set("RUNS_MEMORY_MAX_BYTES", "1000000")
				memory = "1000000"
				quota = "250000"
			}
			if cpu == strconv.FormatInt(math.MaxInt64, 10) {
				quota = "max"
			}
			h.start()
			for path, want := range map[string]string{"main/cgroup.procs": "123", "cgroup.subtree_control": "+cpu +memory +pids", "runs/memory.max": memory, "runs/cpu.max": quota + " 100000", "runs/cgroup.subtree_control": "+memory +pids"} {
				b, e := os.ReadFile(filepath.Clean(filepath.Join(h.p.Cgroup, path)))
				mustCLI(t, e)
				if string(b) != want {
					t.Fatalf("%s: %q want %q", path, b, want)
				}
			}
			h.stop()
			if h.stderr.String() != "" {
				t.Fatal(h.stderr.String())
			}
		})
	}
}

func TestUnavailableRunsLeaveStateUntouched(t *testing.T) {
	// R-HCLY-1HUM R-HIPF-YCK3
	for _, kind := range []string{"empty", "shared", "unicode-space", "missing-procs", "write-failure"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.repository("pass\n")
			group := h.p.Cgroup
			switch kind {
			case "empty":
				h.p.Cgroup = ""
			case "shared":
				mustCLI(t, os.WriteFile(filepath.Join(group, "cgroup.procs"), []byte("123 456\n"), 0600))
			case "unicode-space":
				mustCLI(t, os.WriteFile(filepath.Join(group, "cgroup.procs"), []byte("123\u00a0"), 0600))
			case "missing-procs":
				mustCLI(t, os.Remove(filepath.Join(group, "cgroup.procs")))
			case "write-failure":
				mustCLI(t, os.Mkdir(filepath.Join(group, "cgroup.subtree_control"), 0700))
			}
			before := outsideSnapshot(t, group)
			h.start()
			afterStart := outsideSnapshot(t, group)
			if kind != "write-failure" && !reflect.DeepEqual(before, afterStart) {
				t.Fatal("invalid group changed")
			}
			writes := h.stderr.snapshot()
			if len(writes) != 1 {
				t.Fatalf("diagnostic writes %q", writes)
			}
			diagnostic := string(writes[0])
			prefix := "scripts: runs are unavailable: "
			if !strings.HasPrefix(diagnostic, prefix) || !strings.HasSuffix(diagnostic, "\n") || strings.Count(diagnostic, "\n") != 1 {
				t.Fatal(diagnostic)
			}
			reason := strings.TrimSuffix(strings.TrimPrefix(diagnostic, prefix), "\n")
			if reason == "" || kind == "empty" && reason != "no control group was found" {
				t.Fatal(reason)
			}
			h.create("unavailable")
			for _, args := range []map[string]any{{"name": "unavailable"}, {"name": "unavailable", "ref": "main", "input": map[string]any{"value": 1}}} {
				r, e := h.result("run", args)
				mustCLI(t, e)
				b, e := r.MarshalJSON()
				mustCLI(t, e)
				var object struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				mustCLI(t, json.Unmarshal(b, &object))
				if !r.IsError() || len(object.Content) != 1 || object.Content[0].Text != "runs are unavailable: "+reason {
					t.Fatal(string(b))
				}
			}
			if a := h.call("runs", map[string]any{"name": "unavailable"})["runs"].([]any); len(a) != 0 {
				t.Fatal(a)
			}
			h.call("show", map[string]any{"name": "unavailable"})
			h.call("list", nil)
			entries, e := os.ReadDir(filepath.Join(h.p.Dir, "state", "runs"))
			mustCLI(t, e)
			if len(entries) != 0 {
				t.Fatal(entries)
			}
			h.stop()
			if !reflect.DeepEqual(afterStart, outsideSnapshot(t, group)) {
				t.Fatal("unavailable group changed after start")
			}
		})
	}
}

func TestCommandCgroupUntouched(t *testing.T) {
	// R-L909-MMKE
	for _, args := range [][]string{{"--version"}, {"manifest"}, {"--help"}, {"bogus"}, {"db", "status"}} {
		h := newHarness(t)
		before := outsideSnapshot(t, h.p.Cgroup)
		h.p.Args = args
		cli.Run(context.Background(), h.p)
		if !reflect.DeepEqual(before, outsideSnapshot(t, h.p.Cgroup)) {
			t.Fatal("command changed cgroup")
		}
	}
}

func TestEarlyRefusalsLeaveCgroupUntouched(t *testing.T) {
	// R-HA65-9YD8
	for _, kind := range []string{"setting", "activation", "sockets", "inherit", "git", "python", "db", "runs"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			before := outsideSnapshot(t, h.p.Cgroup)
			switch kind {
			case "setting":
				h.set("RUN_MAX_ACTIVE", "0")
			case "activation":
				h.set("LISTEN_PID", "other")
			case "sockets":
				h.set("LISTEN_FDS", "2")
			case "inherit":
				h.p.Inherit = func(uintptr) (net.Listener, error) { return nil, fmt.Errorf("bad descriptor") }
			case "git":
				h.set("PATH", filepath.Dir(h.python))
			case "python":
				h.set("PATH", filepath.Dir(h.git))
			case "db":
				mustCLI(t, os.MkdirAll(filepath.Join(h.p.Dir, "state"), 0700))
				mustCLI(t, os.WriteFile(filepath.Join(h.p.Dir, "state", "scripts.db"), []byte("not a database"), 0600))
			case "runs":
				mustCLI(t, os.MkdirAll(filepath.Join(h.p.Dir, "state"), 0700))
				mustCLI(t, os.WriteFile(filepath.Join(h.p.Dir, "state", "runs"), nil, 0600))
			}
			code := cli.Run(context.Background(), h.p)
			if code == cli.ExitSuccess || strings.Contains(h.stderr.String(), "runs are unavailable:") || !reflect.DeepEqual(before, outsideSnapshot(t, h.p.Cgroup)) {
				t.Fatalf("refusal %d %s", code, h.stderr.String())
			}
		})
	}
}

func TestQueuedRecoveryPrecedesServiceStart(t *testing.T) {
	// R-LHJK-B0R9 R-TT8X-XI2L
	h := newHarness(t)
	h.p.Sink = &h.sink.capture
	handle, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, e)
	st := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	sc, e := st.Create(context.Background(), store.Draft{Owner: "owner", Name: "oldqueue", Repo: "rep_0102030405060708", Ref: "main"})
	mustCLI(t, e)
	ids := []string{"run_1122334455667788", "run_8877665544332211"}
	for i, id := range ids {
		started := h.now.Add(-2 * time.Second)
		if i == 1 {
			started = h.now.Add(2 * time.Second)
		}
		_, e = st.AddRun(context.Background(), store.Run{ID: id, Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", Trigger: store.TriggerManual, Status: store.StatusQueued, User: "previous", RequestID: "earlier", Started: started})
		mustCLI(t, e)
		folder := filepath.Join(h.p.Dir, "state", "runs", sc.ID, id)
		mustCLI(t, os.MkdirAll(folder, 0700))
		mustCLI(t, os.WriteFile(filepath.Join(folder, "kept"), []byte("untouched"), 0600))
	}
	mustCLI(t, handle.Close())
	before := outsideSnapshot(t, filepath.Join(h.p.Dir, "state", "runs"))
	h.start()
	for _, id := range ids {
		r := h.call("result", map[string]any{"run": id})
		if r["status"] != "failed" || r["reason"] != "queue_abandoned" {
			t.Fatal(r)
		}
		if _, ok := r["exit_code"]; ok {
			t.Fatal(r)
		}
	}
	if !reflect.DeepEqual(before, outsideSnapshot(t, filepath.Join(h.p.Dir, "state", "runs"))) {
		t.Fatal("recovery changed folder")
	}
	h.stop()
	es := h.sink.capture.Events()
	seen := 0
	for _, ev := range es {
		if ev.Name == "run.started" {
			t.Fatal(ev)
		}
		if ev.Name == "run.finished" {
			seen++
		}
		if ev.Name == "service.started" && seen != 2 {
			t.Fatal("late recovery")
		}
	}
	for i, id := range ids {
		ev := trailEvent(t, es, "run.finished", id)
		duration := h.now.Sub(h.now.Add(-2 * time.Second).UTC().Truncate(time.Second)).Microseconds()
		if i == 1 {
			duration = 0
		}
		expectAttrs(t, ev, telemetry.Attrs{"run": id, "status": "failed", "reason": "queue_abandoned", "duration_us": duration, "truncated": false})
		if ev.RequestID != "earlier" || ev.User != "previous" {
			t.Fatal(ev)
		}
	}
}

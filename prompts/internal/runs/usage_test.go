package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"golang.org/x/sys/unix"
)

func usageRecord(u agentkit.Usage, n int64) agentkit.LogRecord {
	cost := agentkit.Cost(n)
	return agentkit.LogRecord{Type: agentkit.RecordUsage, Usage: &u, Cost: &cost}
}
func usageBytes(t *testing.T, records ...agentkit.LogRecord) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, r := range records {
		line, e := json.Marshal(r)
		if e != nil {
			t.Fatal(e)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}
func usageRunning(t *testing.T, f *coreFixture) store.Run {
	t.Helper()
	r, e := f.s.AddRun(context.Background(), store.Run{ID: "prr_123456789abcdef0", Prompt: f.p.ID, Model: f.p.Model, User: f.p.Owner, RequestID: "usage-request", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: testInstant})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(f.c.Folder(r), 0700); e != nil {
		t.Fatal(e)
	}
	return r
}
func usageWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}

// R-1LS9-N3F5 R-OIHI-4913
func TestUsageRecoverEntriesAndLongLines(t *testing.T) {
	for _, mode := range []string{"absent", "directory", "fifo", "link-zero", "link-file", "open-error", "megabyte", "oversize", "boundary"} {
		t.Run(mode, func(t *testing.T) {
			f := newCoreFixture(t)
			r := usageRunning(t, f)
			path := filepath.Join(f.c.Folder(r), TranscriptFile)
			want := store.Usage{}
			record := usageRecord(agentkit.Usage{InputTokens: 5}, 7)
			switch mode {
			case "directory":
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			case "fifo":
				if e := unix.Mkfifo(path, 0600); e != nil {
					t.Fatal(e)
				}
			case "link-zero":
				if e := os.Symlink("/dev/zero", path); e != nil {
					t.Fatal(e)
				}
			case "link-file":
				target := filepath.Join(t.TempDir(), "target")
				usageWrite(t, target, usageBytes(t, record))
				if e := os.Symlink(target, path); e != nil {
					t.Fatal(e)
				}
			case "open-error":
				if e := os.Remove(f.c.Folder(r)); e != nil {
					t.Fatal(e)
				}
				usageWrite(t, f.c.Folder(r), nil)
			case "megabyte":
				record.ID = strings.Repeat("a", 1048576)
				usageWrite(t, path, bytes.TrimSuffix(usageBytes(t, record), []byte("\n")))
				want = store.Usage{Calls: 1, InputTokens: 5, CostNanos: 7}
			case "oversize":
				record.ID = strings.Repeat("a", 16777217)
				usageWrite(t, path, append(usageBytes(t, record), usageBytes(t, usageRecord(agentkit.Usage{InputTokens: 5}, 0))...))
				want = store.Usage{Calls: 1, InputTokens: 5}
			case "boundary":
				record.ID = "a"
				withID := usageBytes(t, record)
				record.ID = strings.Repeat("a", 16777216-(len(withID)-2))
				line := usageBytes(t, record)
				if len(line)-1 != 16777216 {
					t.Fatal(len(line))
				}
				usageWrite(t, path, line)
				want = store.Usage{Calls: 1, InputTokens: 5, CostNanos: 7}
			}
			if e := f.c.Recover(context.Background()); e != nil {
				t.Fatal(e)
			}
			ended, e := f.s.RunByID(context.Background(), r.ID)
			if e != nil || ended.Status != store.StatusKilled {
				t.Fatal(ended, e)
			}
			testEqual(t, ended.Usage, want)
			ev := f.event(t, "run.finished", r.ID)
			testEqual(t, ev.Attrs, FinishedAttrs(ended, testInstant.Sub(r.Started)))
		})
	}
}

// R-1LS9-N3F5 R-1RVR-JY4M
func TestUsageRejectsWholeInvalidRecordsAndSaturates(t *testing.T) {
	f := newCoreFixture(t)
	r := usageRunning(t, f)
	path := filepath.Join(f.c.Folder(r), TranscriptFile)
	good := usageRecord(agentkit.Usage{InputTokens: 5}, 7)
	noCost := usageRecord(agentkit.Usage{InputTokens: 100}, 0)
	noCost.Cost = nil
	noUsage := good
	noUsage.Usage = nil
	records := []agentkit.LogRecord{good, noCost, noUsage}
	for i := range 5 {
		u := agentkit.Usage{InputTokens: 100, CachedTokens: 100, OutputTokens: 100, ReasoningTokens: 100}
		n := int64(100)
		switch i {
		case 0:
			u.InputTokens = -3
		case 1:
			u.CachedTokens = -3
		case 2:
			u.OutputTokens = -3
		case 3:
			u.ReasoningTokens = -3
		case 4:
			n = -3
		}
		records = append(records, usageRecord(u, n))
	}
	for range 2 {
		records = append(records, usageRecord(agentkit.Usage{OutputTokens: math.MaxInt64}, 0))
	}
	records = append(records, agentkit.LogRecord{Type: agentkit.RecordToolResult}, agentkit.LogRecord{Type: agentkit.RecordToolUse}, agentkit.LogRecord{Type: agentkit.RecordTurnEnd, Usage: good.Usage, Cost: good.Cost})
	b := usageBytes(t, records...)
	b = append(b, []byte("not JSON\n{\"type\":\"usage\",\"usage\":")...)
	usageWrite(t, path, b)
	if e := f.c.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	ended, e := f.s.RunByID(context.Background(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, ended.Usage, store.Usage{Calls: 3, ToolCalls: 1, InputTokens: 5, OutputTokens: math.MaxInt64, CostNanos: 7})
	f.event(t, "run.finished", r.ID)
	// Every token/cost sum saturates independently. Counts share the checked addition.
	maxUsage := agentkit.Usage{InputTokens: math.MaxInt64, CachedTokens: math.MaxInt64, OutputTokens: math.MaxInt64, ReasoningTokens: math.MaxInt64}
	usageWrite(t, path, usageBytes(t, usageRecord(maxUsage, math.MaxInt64), usageRecord(maxUsage, math.MaxInt64), good))
	testEqual(t, readUsage(path), store.Usage{Calls: 3, InputTokens: math.MaxInt64, CachedTokens: math.MaxInt64, OutputTokens: math.MaxInt64, ReasoningTokens: math.MaxInt64, CostNanos: math.MaxInt64})
	for _, pair := range [][2]int64{{math.MaxInt64 - 1, 1}, {math.MaxInt64 - 1, 2}, {math.MaxInt64, 1}} {
		testEqual(t, addUsage(pair[0], pair[1]), int64(math.MaxInt64))
	}
}

// R-1N06-0V5U R-1LS9-N3F5
func TestUsageRealChildEndings(t *testing.T) {
	for _, mode := range []string{"exited", "cancel", "timer", "limit"} {
		t.Run(mode, func(t *testing.T) {
			f := newCoreFixture(t)
			second := make(chan struct{})
			var calls atomic.Int64
			server := provider(t, func(w http.ResponseWriter, req *http.Request) {
				call := calls.Add(1)
				if call == 1 || mode == "limit" {
					toolAnswer(w, "Glob", map[string]any{"pattern": "*"})
					return
				}
				close(second)
				if mode == "exited" {
					answer(w, "done")
					return
				}
				<-req.Context().Done()
			})
			f.cfg.BaseURL = server.URL
			f.rebuild()
			p := f.p
			p.Tools = []string{agent.GroupFiles}
			r := f.start(t, p, f.request())
			if mode == "cancel" || mode == "timer" {
				testReceive(t, second)
				if mode == "cancel" {
					if _, e := f.c.Cancel(context.Background(), r.ID); e != nil {
						t.Fatal(e)
					}
				} else {
					testReceive(t, f.timers) <- testInstant
				}
			}
			f.event(t, "run.finished", r.ID)
			ended, e := f.s.RunByID(context.Background(), r.ID)
			if e != nil {
				t.Fatal(e)
			}
			expected := store.Usage{}
			uses := 0
			for _, record := range readRecords(t, filepath.Join(f.c.Folder(r), TranscriptFile)) {
				switch record.Type {
				case agentkit.RecordUsage:
					if record.Usage == nil || record.Cost == nil {
						t.Fatal(record)
					}
					expected.Calls++
					expected.InputTokens += record.Usage.InputTokens
					expected.CachedTokens += record.Usage.CachedTokens
					expected.OutputTokens += record.Usage.OutputTokens
					expected.ReasoningTokens += record.Usage.ReasoningTokens
					expected.CostNanos += int64(*record.Cost)
				case agentkit.RecordToolResult:
					expected.ToolCalls++
				case agentkit.RecordToolUse:
					uses++
				}
			}
			testEqual(t, ended.Usage, expected)
			testEqual(t, expected.ToolCalls, int64(1))
			if mode == "exited" || mode == "limit" {
				testEqual(t, expected.Calls, int64(2))
				testEqual(t, expected.InputTokens, int64(6))
				testEqual(t, expected.OutputTokens, int64(4))
				testEqual(t, ended.Status, store.StatusExited)
			} else {
				testEqual(t, expected.Calls, int64(1))
				testEqual(t, expected.InputTokens, int64(3))
				testEqual(t, expected.OutputTokens, int64(2))
			}
			if mode == "cancel" {
				testEqual(t, ended.Status, store.StatusKilled)
			}
			if mode == "timer" {
				testEqual(t, ended.Status, store.StatusTimedOut)
			}
			if mode == "limit" {
				testEqual(t, uses, 2)
				testEqual(t, ended.ExitCode, agent.ExitLimit)
			}
		})
	}
}

// R-1N06-0V5U
func TestUsageCancelRecoveredRunningRecord(t *testing.T) {
	f := newCoreFixture(t)
	r := usageRunning(t, f)
	usageWrite(t, filepath.Join(f.c.Folder(r), TranscriptFile), usageBytes(t, usageRecord(agentkit.Usage{InputTokens: 11, CachedTokens: 13, OutputTokens: 17, ReasoningTokens: 19}, 23)))
	ended, e := f.c.Cancel(context.Background(), r.ID)
	if e != nil {
		t.Fatal(e)
	}
	testEqual(t, ended.Status, store.StatusKilled)
	testEqual(t, ended.Usage, store.Usage{Calls: 1, InputTokens: 11, CachedTokens: 13, OutputTokens: 17, ReasoningTokens: 19, CostNanos: 23})
	f.event(t, "run.finished", r.ID)
}

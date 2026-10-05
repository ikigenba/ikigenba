package maintenance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/repos/internal/git"
)

// Once Command has read its environment, keep Cycle's select from judging
// the run until the test has observed and reaped the real git process.
type lateMaintenanceContext struct {
	context.Context
	armed atomic.Bool
	gate  chan struct{}
}

func (c *lateMaintenanceContext) Done() <-chan struct{} {
	if c.armed.Load() {
		select {
		case <-c.gate:
		case <-c.Context.Done():
		}
	}
	return c.Context.Done()
}

type maintenanceTraceEvent struct {
	Event string   `json:"event"`
	SID   string   `json:"sid"`
	Argv  []string `json:"argv"`
	Code  int      `json:"code"`
}

func awaitMaintenanceNaturalExit(t *testing.T, trace string) {
	t.Helper()
	ctx := testContext(t)
	for {
		data, err := os.ReadFile(filepath.Clean(trace))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		var sid string
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			var event maintenanceTraceEvent
			if json.Unmarshal(line, &event) != nil {
				continue // The final line may still be being written by git.
			}
			if sid == "" && event.Event == "start" && len(event.Argv) == 2 && event.Argv[0] == "git" && event.Argv[1] == "gc" {
				sid = event.SID
			}
			if sid == "" || event.Event != "exit" || event.SID != sid {
				continue
			}
			if event.Code != 0 {
				t.Fatalf("maintenance git exited with code %d", event.Code)
			}
			pidStart := strings.LastIndex(sid, "-P")
			if pidStart < 0 {
				t.Fatalf("git session lacks PID: %q", sid)
			}
			pid, err := strconv.ParseInt(sid[pidStart+2:], 16, 32)
			if err != nil || pid <= 0 {
				t.Fatalf("git session PID: %q: %v", sid, err)
			}
			if errors.Is(syscall.Kill(int(pid), 0), syscall.ESRCH) {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("maintenance git did not exit successfully and get reaped")
		default:
			runtime.Gosched()
		}
	}
}

func TestMaintenanceDeadlineAfterNaturalExit(t *testing.T) {
	// R-1CGV-35IN
	f := fixture(t, 2)
	first, second := f.repos[0].ID, f.repos[1].ID
	trace, global := filepath.Join(f.dir, "gc-trace.json"), filepath.Join(f.dir, "global")
	gitOut(t, f.g, f.dir, "config", "--file", global, "trace2.eventTarget", trace)
	for i, variable := range f.env {
		if strings.HasPrefix(variable, "GIT_CONFIG_GLOBAL=") {
			f.env[i] = "GIT_CONFIG_GLOBAL=" + global
		}
	}
	ctx := &lateMaintenanceContext{Context: maintenanceContext(t), gate: make(chan struct{})}
	defer func() {
		select {
		case <-ctx.gate:
		default:
			close(ctx.gate)
		}
	}()
	var err error
	f.g, err = git.Find(strings.TrimPrefix(f.env[0], "PATH="), func() []string {
		ctx.armed.Store(true)
		return append([]string(nil), f.env...)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Git = f.g
	before, err := f.st.Size(testContext(t), first)
	if err != nil {
		t.Fatal(err)
	}
	start := f.clock.Now()
	held := make(chan string, 2)
	releaseSecond := make(chan struct{})
	defer close(releaseSecond)
	f.cfg.Hold = func(ctx context.Context, id string) {
		held <- id
		if id == second {
			select {
			case <-releaseSecond:
			case <-ctx.Done():
			}
		}
	}
	done := runCycle(ctx, f.cfg)
	if got := receive(t, held); got != first {
		t.Fatal("unexpected first repository", got)
	}
	deadline := timer(t, f.clock, 23*time.Second)
	awaitMaintenanceNaturalExit(t, trace)
	if !f.lim.Busy(first) || f.lim.Pressure().Write.Active != 1 {
		t.Fatal("maintenance grant released before its result was judged")
	}
	after, err := f.st.Size(testContext(t), first)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(1234567 * time.Nanosecond)
	deadline.fire <- start
	close(ctx.gate)
	if got := receive(t, held); got != second {
		t.Fatal("cycle did not continue to next repository", got)
	}
	if f.lim.Busy(first) || f.lim.Pressure().Write.Active != 1 {
		t.Fatal("first repository's grant was not released before next maintenance")
	}
	releaseSecond <- struct{}{}
	timer(t, f.clock, 23*time.Second)
	receive(t, done)
	firstEvents := repoEvents(t, f, first)
	if len(firstEvents) != 1 {
		t.Fatalf("late deadline events: %+v", firstEvents)
	}
	event := firstEvents[0]
	if event.Name != "maintenance.finished" || event.Attrs["repo"] != first || event.Attrs["size_before"] != before || event.Attrs["size_after"] != after || event.Attrs["duration_us"] != int64(1234) || event.RequestID != "" || event.User != "" {
		t.Fatalf("late deadline finished event: %+v", event)
	}
	secondEvents := repoEvents(t, f, second)
	if len(secondEvents) != 1 || secondEvents[0].Name != "maintenance.finished" {
		t.Fatalf("next repository events: %+v", secondEvents)
	}
	assertIdle(t, f)
}

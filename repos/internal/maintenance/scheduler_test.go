package maintenance_test

import (
	"context"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/maintenance"
)

func TestInitialIntervalAndStop(t *testing.T) {
	// R-GA58-4Q2I R-H5UM-2RCK R-H9IB-82KN
	for _, hours := range []int64{1, math.MaxInt64} {
		t.Run(time.Duration(hours).String(), func(t *testing.T) {
			f := fixture(t, 1)
			s := f.lim.Settings()
			s.MaintenanceHours = hours
			f.lim = limits.New(s, limits.Clock{Now: f.clock.Now, After: f.clock.After})
			f.cfg.Limits = f.lim
			held := make(chan string, 1)
			f.cfg.Hold = func(_ context.Context, id string) { held <- id }
			before := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "count-objects", "-v")
			var initialCalls atomic.Int64
			f.clock.after = func(timerRequest) { initialCalls.Add(1) }
			schedule := maintenance.Start(f.cfg)
			defer schedule.Stop(context.Background())
			if got := initialCalls.Load(); got != 1 {
				t.Fatalf("initial After calls at Start return: %d, want 1", got)
			}
			want := time.Hour
			if hours == math.MaxInt64 {
				want = time.Duration(math.MaxInt64)
			}
			var first timerRequest
			select {
			case first = <-f.clock.requests:
			default:
				t.Fatal("initial After did not return before Start returned")
			}
			if first.duration != want {
				t.Fatalf("initial interval %v, want %v", first.duration, want)
			}
			absent(t, f.clock.requests)
			absent(t, held)
			if len(events(t, f)) != 0 {
				t.Fatal("event before interval")
			}
			if got := gitOut(t, f.g, f.st.Dir(f.repos[0].ID), "count-objects", "-v"); got != before {
				t.Fatal("git before interval")
			}
			schedule.Stop(context.Background())
			schedule.Stop(context.Background())
			first.fire <- f.clock.Now()
			absent(t, held)
			absent(t, f.clock.requests)
			if len(events(t, f)) != 0 {
				t.Fatal("event after stop")
			}
		})
	}
}
func TestScheduledCyclesNeverOverlapAndOverdueBeginsAtEnd(t *testing.T) {
	// R-GBD4-IHT7 R-GEC3-5B5O R-GFJZ-J2WD
	f := fixture(t, 1)
	held := make(chan string, 4)
	release := make(chan struct{})
	f.cfg.Hold = func(ctx context.Context, id string) {
		held <- id
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	schedule := maintenance.Start(f.cfg)
	defer schedule.Stop(testContext(t))
	timer(t, f.clock, time.Hour).fire <- f.clock.Now()
	second := timer(t, f.clock, time.Hour)
	receive(t, held)
	second.fire <- f.clock.Now()
	absent(t, held)
	absent(t, f.clock.requests)
	if f.lim.Pressure().Write.Active != 1 {
		t.Fatal("overlapping cycles")
	}
	release <- struct{}{}
	timer(t, f.clock, 23*time.Second)
	third := timer(t, f.clock, time.Hour)
	receive(t, held)
	if len(repoEvents(t, f, f.repos[0].ID)) != 1 {
		t.Fatal("next cycle began before first ended")
	}
	release <- struct{}{}
	timer(t, f.clock, 23*time.Second)
	schedule.Stop(testContext(t))
	third.fire <- f.clock.Now()
	absent(t, held)
	absent(t, f.clock.requests)
	if len(repoEvents(t, f, f.repos[0].ID)) != 2 {
		t.Fatal("incorrect cycle count")
	}
}
func TestCycleMaintainsAllAvailableSequentially(t *testing.T) {
	// R-GGRV-WUN2 R-GHZS-AMDR R-GJ7O-OE4G R-GQJ2-Z0KM R-GSYV-QK20 R-1YBL-CQ4A R-GZR4-5WN3 R-GVEO-I3JE
	f := fixture(t, 2)
	before := make(map[string]string)
	for _, repo := range f.repos {
		before[repo.ID] = gitOut(t, f.g, f.st.Dir(repo.ID), "count-objects", "-v")
		if strings.Contains(before[repo.ID], "count: 0\n") || !strings.Contains(before[repo.ID], "packs: 0\n") {
			t.Fatal("fixture must begin with loose, uncollected objects")
		}
	}
	held := make(chan string, 2)
	release := make(chan struct{})
	f.cfg.Hold = func(ctx context.Context, id string) {
		held <- id
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	done := runCycle(maintenanceContext(t), f.cfg)
	first := receive(t, held)
	if !f.lim.Busy(first) || f.lim.Pressure().Write.Active != 1 || f.lim.Pressure().Write.Queued != 0 {
		t.Fatal("grant not active at Hold")
	}
	absent(t, done)
	absent(t, held)
	absent(t, f.clock.requests)
	if got := gitOut(t, f.g, f.st.Dir(first), "count-objects", "-v"); got != before[first] {
		t.Fatalf("git changed repository before Hold returned: %s", got)
	}
	release <- struct{}{}
	timer(t, f.clock, 23*time.Second)
	second := receive(t, held)
	if got := gitOut(t, f.g, f.st.Dir(second), "count-objects", "-v"); got != before[second] {
		t.Fatalf("git changed second repository before Hold returned: %s", got)
	}
	if got := gitOut(t, f.g, f.st.Dir(first), "count-objects", "-v"); got == before[first] || !strings.Contains(got, "count: 0\n") || !strings.Contains(got, "packs: 1\n") {
		t.Fatalf("released first Hold did not allow collection: %s", got)
	}
	if first == second {
		t.Fatal("same repository twice")
	}
	if f.lim.Busy(first) || !f.lim.Busy(second) || f.lim.Pressure().Write.Active != 1 {
		t.Fatal("repositories overlap")
	}
	if len(repoEvents(t, f, first)) != 1 {
		t.Fatal("preceding maintenance not ended")
	}
	absent(t, done)
	absent(t, f.clock.requests)
	release <- struct{}{}
	timer(t, f.clock, 23*time.Second)
	receive(t, done)
	for _, r := range f.repos {
		assertFinished(t, f, r.ID)
		if got := gitOut(t, f.g, f.st.Dir(r.ID), "count-objects", "-v"); got == before[r.ID] || !strings.Contains(got, "count: 0\n") || !strings.Contains(got, "packs: 1\n") {
			t.Fatalf("released Hold did not allow collection: %s", got)
		}
	}
	absent(t, held)
	assertIdle(t, f)
}
func TestStopLetsActiveMaintenanceFinishButTouchesNoNextRepository(t *testing.T) {
	// R-H72I-GJ39 R-GU6S-4BSP R-GR7T-HIG8
	f := fixture(t, 2)
	held := make(chan context.Context, 1)
	release := make(chan struct{})
	f.cfg.Hold = func(ctx context.Context, _ string) {
		held <- ctx
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	schedule := maintenance.Start(f.cfg)
	timer(t, f.clock, time.Hour).fire <- f.clock.Now()
	timer(t, f.clock, time.Hour)
	holdCtx := receive(t, held)
	f.lim.Drain()
	stopCtx, cancel := context.WithCancel(maintenanceContext(t))
	defer cancel()
	stopped := make(chan struct{})
	go func() { schedule.Stop(stopCtx); close(stopped) }()
	if holdCtx.Err() != nil {
		t.Fatal("Hold canceled at stop initiation")
	}
	absent(t, stopped)
	close(release)
	timer(t, f.clock, 23*time.Second)
	receive(t, stopped)
	es := events(t, f)
	if len(es) != 1 || es[0].Name != "maintenance.finished" {
		t.Fatalf("stop events: %+v", es)
	}
	absent(t, held)
	assertIdle(t, f)
}
func TestStopDeadlineCancelsHoldAndWaitsForMaintenance(t *testing.T) {
	// R-GR7T-HIG8 R-GSFP-VA6X R-H0AA-16I6 R-H1I6-EY8V
	f := fixture(t, 2)
	held := make(chan context.Context, 1)
	f.cfg.Hold = func(ctx context.Context, _ string) { held <- ctx; <-ctx.Done() }
	schedule := maintenance.Start(f.cfg)
	timer(t, f.clock, time.Hour).fire <- f.clock.Now()
	timer(t, f.clock, time.Hour)
	holdCtx := receive(t, held)
	f.lim.Drain()
	stopCtx, cancel := context.WithCancel(maintenanceContext(t))
	stopped := make(chan struct{})
	go func() { schedule.Stop(stopCtx); close(stopped) }()
	if holdCtx.Err() != nil {
		t.Fatal("Hold canceled before deadline")
	}
	absent(t, stopped)
	cancel()
	receive(t, stopped)
	if holdCtx.Err() == nil {
		t.Fatal("Hold was not canceled")
	}
	if len(events(t, f)) != 0 {
		t.Fatal("canceled maintenance recorded event")
	}
	absent(t, f.clock.requests)
	assertIdle(t, f)
}
func TestCanceledCycleEndsHoldWithoutStartingGit(t *testing.T) {
	// R-IIUM-PSSC R-GSFP-VA6X R-GR7T-HIG8
	f := fixture(t, 2)
	held := make(chan context.Context, 1)
	f.cfg.Hold = func(ctx context.Context, _ string) { held <- ctx; <-ctx.Done() }
	ctx, cancel := context.WithCancel(maintenanceContext(t))
	done := runCycle(ctx, f.cfg)
	holdCtx := receive(t, held)
	if holdCtx.Err() != nil {
		t.Fatal("Hold prematurely canceled")
	}
	cancel()
	receive(t, done)
	if holdCtx.Err() == nil {
		t.Fatal("Hold context remained live")
	}
	if len(events(t, f)) != 0 {
		t.Fatal("canceled cycle recorded event")
	}
	absent(t, f.clock.requests)
	assertIdle(t, f)
}
func TestFailedAllReturnsAndScheduleContinues(t *testing.T) {
	// R-4V9W-JYZG
	f := fixture(t, 1)
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	maintenance.Cycle(maintenanceContext(t), f.cfg)
	schedule := maintenance.Start(f.cfg)
	timer(t, f.clock, time.Hour).fire <- f.clock.Now()
	timer(t, f.clock, time.Hour).fire <- f.clock.Now()
	timer(t, f.clock, time.Hour)
	schedule.Stop(context.Background())
	if len(events(t, f)) != 0 {
		t.Fatal("failed All recorded event")
	}
	assertIdle(t, f)
}

func TestDrainingScheduleRearmsIntervalsUntilStop(t *testing.T) {
	// R-GBD4-IHT7 R-GEC3-5B5O R-GU6S-4BSP R-H5UM-2RCK
	f := fixture(t, 1)
	id := f.repos[0].ID
	before := gitOut(t, f.g, f.st.Dir(id), "count-objects", "-v")
	held := make(chan string, 1)
	f.cfg.Hold = func(_ context.Context, id string) { held <- id }
	f.lim.Drain()
	schedule := maintenance.Start(f.cfg)
	defer schedule.Stop(context.Background())
	first := timer(t, f.clock, time.Hour)
	first.fire <- f.clock.Now()
	second := timer(t, f.clock, time.Hour)
	second.fire <- f.clock.Now()
	third := timer(t, f.clock, time.Hour)
	absent(t, held)
	if len(events(t, f)) != 0 {
		t.Fatal("drained cycles recorded events")
	}
	if got := gitOut(t, f.g, f.st.Dir(id), "count-objects", "-v"); got != before {
		t.Fatal("drained schedule ran git")
	}
	assertIdle(t, f)
	schedule.Stop(context.Background())
	third.fire <- f.clock.Now()
	absent(t, f.clock.requests)
	absent(t, held)
	if len(events(t, f)) != 0 {
		t.Fatal("stopped schedule recorded events")
	}
}

package maintenance_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/maintenance"
)

func TestUnavailableRepositoryPassedByUntouched(t *testing.T) {
	// R-GF0T-NT1A
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{true: "missing", false: "damaged"}[missing], func(t *testing.T) {
			f := fixture(t, 2)
			r := f.repos[0]
			dir := f.st.Dir(r.ID)
			if missing {
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(filepath.Join(dir, "HEAD")); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(dir, "marker"), "unchanged")
			}
			if err := f.st.Verify(testContext(t), f.cfg.Telemetry); err != nil {
				t.Fatal(err)
			}
			prior := len(events(t, f))
			held := make(chan string, 2)
			f.cfg.Hold = func(_ context.Context, id string) { held <- id }
			maintenance.Cycle(maintenanceContext(t), f.cfg)
			if got := receive(t, held); got != f.repos[1].ID {
				t.Fatal("unavailable Hold", got)
			}
			absent(t, held)
			es := events(t, f)[prior:]
			if len(es) != 1 || es[0].Attrs["repo"] != f.repos[1].ID || es[0].Name != "maintenance.finished" {
				t.Fatalf("events: %+v", es)
			}
			timer(t, f.clock, 23*time.Second)
			absent(t, f.clock.requests)
			if missing {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatal("missing directory recreated", err)
				}
			} else {
				root, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				data, err := root.ReadFile("marker")
				if closeErr := root.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
				if err != nil || string(data) != "unchanged" {
					t.Fatal("directory changed", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "HEAD")); !os.IsNotExist(err) {
					t.Fatal("HEAD recreated")
				}
			}
			assertIdle(t, f)
		})
	}
}

func TestMaintenanceUsesSharedWriteSlotsLockAndNoReadLock(t *testing.T) {
	// R-GLNH-FXLU R-GMVD-TPCJ R-GO3A-7H38 R-GPB6-L8TX R-GRQZ-CSBB R-1X3O-YYDL
	for _, mode := range []string{"lock", "slots", "fetch"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture(t, 1)
			id := f.repos[0].ID
			var heldGrants []*limits.Grant
			acquire := func(repo string, op limits.Op, lock bool) {
				g, err := f.lim.Acquire(testContext(t), repo, op, lock)
				if err != nil {
					t.Fatal(err)
				}
				heldGrants = append(heldGrants, g)
			}
			switch mode {
			case "lock":
				acquire(id, limits.Push, true)
			case "slots":
				acquire("another", limits.Push, false)
				acquire("third", limits.Maintenance, true)
			case "fetch":
				acquire(id, limits.Fetch, false)
			}
			held := make(chan string, 1)
			release := make(chan struct{})
			f.cfg.Hold = func(ctx context.Context, id string) {
				held <- id
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			f.clock.after = func(r timerRequest) {
				if r.duration == 17*time.Second && f.lim.Pressure().Write.Queued != 1 {
					t.Error("not queued before queue After")
				}
			}
			done := runCycle(maintenanceContext(t), f.cfg)
			if mode != "fetch" {
				timer(t, f.clock, 17*time.Second)
				absent(t, held)
				absent(t, done)
				if len(events(t, f)) != 0 {
					t.Fatal("event before grant")
				}
				f.clock.advance(12345 * time.Nanosecond)
				for _, g := range heldGrants {
					g.Release()
				}
			}
			if got := receive(t, held); got != id {
				t.Fatal("Hold id", got)
			}
			if !f.lim.Busy(id) {
				t.Fatal("maintenance not busy")
			}
			if len(repoEvents(t, f, id)) != map[bool]int{true: 0, false: 1}[mode == "fetch"] {
				t.Fatal("wrong waited events")
			}
			if mode == "fetch" {
				if f.lim.Pressure().Read.Active != 1 {
					t.Fatal("read ended before maintenance")
				}
			} else {
				e := repoEvents(t, f, id)[0]
				if e.Name != "operation.waited" || e.Attrs["operation"] != "maintenance" || e.Attrs["wait_us"] != int64(12) || e.RequestID != "" || e.User != "" {
					t.Fatalf("waited: %+v", e)
				}
			}
			absent(t, f.clock.requests)
			close(release)
			timer(t, f.clock, 23*time.Second)
			receive(t, done)
			for _, g := range heldGrants {
				g.Release()
			}
			assertFinished(t, f, id)
			assertIdle(t, f)
		})
	}
}

func TestQueueRefusalsContinueCycle(t *testing.T) {
	// R-GL4B-KNQR
	for _, mode := range []string{"queue_length", "queue_seconds"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture(t, 2)
			s := f.lim.Settings()
			s.QueueLength = 1
			if mode == "queue_length" {
				s.WriteSlots = 1
			}
			f.lim = limits.New(s, limits.Clock{Now: f.clock.Now, After: f.clock.After})
			f.cfg.Limits = f.lim
			repo := f.repos[0].ID
			if mode == "queue_length" {
				repo = "occupied"
			}
			grant, err := f.lim.Acquire(testContext(t), repo, limits.Push, true)
			if err != nil {
				t.Fatal(err)
			}
			defer grant.Release()
			var queuedDone <-chan struct{}
			var cancel context.CancelFunc
			if mode == "queue_length" {
				ctx, c := context.WithCancel(maintenanceContext(t))
				cancel = c
				done := make(chan struct{})
				queuedDone = done
				go func() {
					g, _ := f.lim.Acquire(ctx, "existing-waiter", limits.Push, true)
					if g != nil {
						g.Release()
					}
					close(done)
				}()
				timer(t, f.clock, 17*time.Second)
			}
			done := runCycle(maintenanceContext(t), f.cfg)
			if mode == "queue_seconds" {
				timer(t, f.clock, 17*time.Second).fire <- f.clock.Now()
			}
			receive(t, done)
			es := repoEvents(t, f, f.repos[0].ID)
			if len(es) != 1 || es[0].Name != "operation.rejected" || es[0].Attrs["limit"] != mode || es[0].Attrs["operation"] != "maintenance" {
				t.Fatalf("refusal: %+v", es)
			}
			second := repoEvents(t, f, f.repos[1].ID)
			if mode == "queue_seconds" {
				if len(second) != 1 || second[0].Name != "maintenance.finished" {
					t.Fatalf("next repository: %+v", second)
				}
			} else {
				if len(second) != 1 || second[0].Attrs["limit"] != mode {
					t.Fatalf("cycle did not reach next: %+v", second)
				}
				cancel()
				receive(t, queuedDone)
			}
			grant.Release()
			assertIdle(t, f)
		})
	}
}

func TestWaitingMaintenanceGivenUpOnEachStopPath(t *testing.T) {
	// R-2NE4-IXXW R-GU6S-4BSP R-GSYV-QK20 R-GYJ7-S4WE
	for _, mode := range []string{"drain", "cycle", "schedule"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture(t, 2)
			g, err := f.lim.Acquire(testContext(t), f.repos[0].ID, limits.Push, true)
			if err != nil {
				t.Fatal(err)
			}
			defer g.Release()
			held := make(chan string, 2)
			f.cfg.Hold = func(_ context.Context, id string) { held <- id }
			ctx, cancel := context.WithCancel(maintenanceContext(t))
			defer cancel()
			var done <-chan struct{}
			var schedule *maintenance.Scheduler
			if mode == "schedule" {
				schedule = maintenance.Start(f.cfg)
				timer(t, f.clock, time.Hour).fire <- f.clock.Now()
				timer(t, f.clock, time.Hour)
			} else {
				done = runCycle(ctx, f.cfg)
			}
			timer(t, f.clock, 17*time.Second)
			switch mode {
			case "drain":
				f.lim.Drain()
			case "cycle":
				cancel()
			case "schedule":
				schedule.Stop(context.Background())
			}
			if done != nil {
				receive(t, done)
			}
			absent(t, held)
			es := events(t, f)
			if len(es) != 1 || es[0].Name != "operation.rejected" || es[0].Attrs["repo"] != f.repos[0].ID || es[0].Attrs["operation"] != "maintenance" || es[0].Attrs["limit"] != "draining" || es[0].RequestID != "" || es[0].User != "" {
				t.Fatalf("drain events: %+v", es)
			}
			g.Release()
			assertIdle(t, f)
		})
	}
}

func TestRepositoryReresolvedAfterWait(t *testing.T) {
	// R-2OM0-WPOL R-1X3O-YYDL
	f := fixture(t, 2)
	id := f.repos[0].ID
	release, ok := f.lim.TryHold(id)
	if !ok {
		t.Fatal("hold refused")
	}
	held := make(chan string, 2)
	f.cfg.Hold = func(_ context.Context, id string) { held <- id }
	done := runCycle(maintenanceContext(t), f.cfg)
	timer(t, f.clock, 17*time.Second)
	if err := f.st.Delete(testContext(t), id); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(time.Second)
	release()
	receive(t, done)
	if got := receive(t, held); got != f.repos[1].ID {
		t.Fatal("deleted repository maintained", got)
	}
	absent(t, held)
	if len(repoEvents(t, f, id)) != 0 {
		t.Fatal("deleted repository event")
	}
	assertFinished(t, f, f.repos[1].ID)
	assertIdle(t, f)
}

func TestStorageErrorsReleaseGrantAndContinue(t *testing.T) {
	// R-2PTX-AHFA
	for _, point := range []string{"find", "before", "after"} {
		t.Run(point, func(t *testing.T) {
			f := fixture(t, 2)
			second, err := f.lim.Acquire(testContext(t), f.repos[1].ID, limits.Push, true)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Release()
			var done <-chan struct{}
			var grant *limits.Grant
			switch point {
			case "find":
				var err error
				grant, err = f.lim.Acquire(testContext(t), f.repos[0].ID, limits.Push, true)
				if err != nil {
					t.Fatal(err)
				}
				done = runCycle(maintenanceContext(t), f.cfg)
				timer(t, f.clock, 17*time.Second)
				if err = f.st.Close(); err != nil {
					t.Fatal(err)
				}
				grant.Release()
			case "before":
				f.cfg.Hold = func(context.Context, string) {
					if err := f.st.Close(); err != nil {
						t.Error(err)
					}
				}
				done = runCycle(maintenanceContext(t), f.cfg)
			case "after":
				calls := 0
				f.clock.nowHook = func() time.Time {
					calls++
					if calls == 2 {
						if err := f.st.Close(); err != nil {
							t.Error(err)
						}
					}
					return time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
				}
				done = runCycle(maintenanceContext(t), f.cfg)
			}
			if point == "after" {
				timer(t, f.clock, 23*time.Second)
			}
			timer(t, f.clock, 17*time.Second)
			if f.lim.Pressure().Write.Queued != 1 {
				t.Fatal("cycle did not continue to next repository")
			}
			second.Release()
			receive(t, done)
			if len(events(t, f)) != 0 {
				t.Fatal("failed storage recorded event")
			}
			assertIdle(t, f)
		})
	}
}

func TestRealGCRefsPacksExpiryAndMeasurements(t *testing.T) {
	// R-GTNM-91XM R-GVEO-I3JE R-GWMK-VVA3 R-GXUH-9N0S R-GXBB-ED5P
	for _, objects := range []string{"reachable", "unreachable"} {
		t.Run(objects, func(t *testing.T) {
			f := fixture(t, 1)
			id := f.repos[0].ID
			dir := f.st.Dir(id)
			refs := gitOut(t, f.g, dir, "show-ref", "--head")
			reachable := strings.Fields(gitOut(t, f.g, dir, "rev-list", "--objects", "--all"))
			var old, recent string
			if objects == "unreachable" {
				old = strings.TrimSpace(gitInput(t, f.g, dir, "old-unreachable", "hash-object", "-w", "--stdin"))
				recent = strings.TrimSpace(gitInput(t, f.g, dir, "recent-unreachable", "hash-object", "-w", "--stdin"))
				epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
				if err := os.Chtimes(filepath.Join(dir, "objects", old[:2], old[2:]), epoch, epoch); err != nil {
					t.Fatal(err)
				}
			}
			var before int64
			f.cfg.Hold = func(ctx context.Context, _ string) {
				writeFile(t, filepath.Join(dir, "measurement"), "measured after hold")
				var err error
				before, err = f.st.Size(ctx, id)
				if err != nil {
					t.Error(err)
				}
			}
			calls := 0
			start := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
			f.clock.nowHook = func() time.Time {
				calls++
				if calls == 1 {
					return start
				}
				return start.Add(1234567 * time.Nanosecond)
			}
			maintenance.Cycle(maintenanceContext(t), f.cfg)
			e := assertFinished(t, f, id)
			after, err := f.st.Size(testContext(t), id)
			if err != nil {
				t.Fatal(err)
			}
			if e.Attrs["size_before"] != before || e.Attrs["size_after"] != after || e.Attrs["duration_us"] != int64(1234) || e.RequestID != "" || e.User != "" {
				t.Fatalf("finished: %+v before=%d after=%d", e, before, after)
			}
			if got := gitOut(t, f.g, dir, "show-ref", "--head"); got != refs {
				t.Fatal("refs changed")
			}
			for _, object := range reachable {
				if len(object) == 40 {
					gitOut(t, f.g, dir, "cat-file", "-e", object)
				}
			}
			if objects == "reachable" {
				count := gitOut(t, f.g, dir, "count-objects", "-v")
				if !strings.Contains(count, "count: 0\n") || !strings.Contains(count, "packs: 1\n") {
					t.Fatal("collection outcome", count)
				}
			} else {
				if _, err := f.g.Output(testContext(t), dir, "cat-file", "-e", old); err == nil {
					t.Fatal("old object survived")
				}
				gitOut(t, f.g, dir, "cat-file", "-e", recent)
			}
			assertIdle(t, f)
		})
	}
}

func TestAlreadyDeliveredOperationDeadlineSkipsGitAndContinues(t *testing.T) {
	// R-H0Z0-JODS R-GYJ7-S4WE R-GSYV-QK20
	f := fixture(t, 2)
	first := f.repos[0].ID
	before := gitOut(t, f.g, f.st.Dir(first), "count-objects", "-v")
	deadlines := 0
	f.clock.after = func(r timerRequest) {
		if r.duration == 23*time.Second {
			deadlines++
			if deadlines == 1 {
				r.fire <- time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
			}
		}
	}
	maintenance.Cycle(maintenanceContext(t), f.cfg)
	es := repoEvents(t, f, first)
	if len(es) != 1 || es[0].Name != "operation.timed_out" || es[0].Attrs["operation"] != "maintenance" || es[0].Attrs["limit"] != "operation_seconds" || es[0].RequestID != "" || es[0].User != "" {
		t.Fatalf("timeout: %+v", es)
	}
	if got := gitOut(t, f.g, f.st.Dir(first), "count-objects", "-v"); got != before {
		t.Fatal("git ran after expired deadline")
	}
	assertFinished(t, f, f.repos[1].ID)
	assertIdle(t, f)
}

func TestFailedGitSilentWithDiscardedOutputAndNextRepository(t *testing.T) {
	// R-H3ET-B7V6 R-IHMQ-C11N R-GSYV-QK20
	f := fixture(t, 2)
	dir := f.st.Dir(f.repos[0].ID)
	writeFile(t, filepath.Join(dir, "config"), "[broken\n")
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := outRead.Close(); err != nil {
			t.Error(err)
		}
	}()
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := errRead.Close(); err != nil {
			t.Error(err)
		}
	}()
	priorOut, priorErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outWrite, errWrite
	maintenance.Cycle(maintenanceContext(t), f.cfg)
	os.Stdout, os.Stderr = priorOut, priorErr
	if err := outWrite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := errWrite.Close(); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if _, err := io.Copy(&output, outRead); err != nil {
		t.Fatal(err)
	}
	var diagnostics strings.Builder
	if _, err := io.Copy(&diagnostics, errRead); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 || diagnostics.Len() != 0 {
		t.Fatal("git reached process streams", output.String(), diagnostics.String())
	}
	if len(repoEvents(t, f, f.repos[0].ID)) != 0 {
		t.Fatal("git failure recorded event")
	}
	assertFinished(t, f, f.repos[1].ID)
	assertIdle(t, f)
}

func TestGitCannotReadProcessStdin(t *testing.T) {
	// R-IHMQ-C11N
	f := fixture(t, 0)
	repo, err := f.st.Create(testContext(t), "owner", "stdin")
	if err != nil {
		t.Fatal(err)
	}
	f.repos = append(f.repos, repo)
	id := repo.ID
	dir := f.st.Dir(id)
	// Git's include loads the child's descriptor zero. The test owns the
	// malformed input file and never reads the original process stdin. An
	// empty repository avoids pack-objects reading its internal revision list
	// from its own stdin through the same include.
	gitOut(t, f.g, dir, "config", "--add", "include.path", "/dev/stdin")
	root, err := os.OpenRoot(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := root.WriteFile("stdin-fixture", []byte("[malformed stdin configuration\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := root.Open("stdin-fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := input.Close(); err != nil {
			t.Error(err)
		}
	}()
	// Read-back with the file explicitly forwarded proves that this real git
	// fixture detects a connection to process stdin.
	control := f.g.Command(testContext(t), dir, nil, "config", "--list")
	control.Stdin = input
	output, err := control.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "bad config line") {
		t.Fatalf("forwarded stdin control did not reject malformed include: %v, %s", err, output)
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	priorInput := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = priorInput }()
	maintenance.Cycle(maintenanceContext(t), f.cfg)
	assertFinished(t, f, id)
	position, err := input.Seek(0, io.SeekCurrent)
	if err != nil || position != 0 {
		t.Fatalf("maintenance consumed process stdin: offset=%d error=%v", position, err)
	}
	assertIdle(t, f)
}

package runs_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

type retryStore struct {
	Store    runs.Store
	op       string
	failures int
	err      error
	calls    int
	ids      []string
	mu       sync.Mutex
}

func (s *retryStore) AddRun(ctx context.Context, r store.Run) (store.Run, error) {
	return s.Store.AddRun(ctx, r)
}
func (s *retryStore) RunningRuns(ctx context.Context) ([]store.Run, error) {
	return s.Store.RunningRuns(ctx)
}

func (s *retryStore) refuse(op, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if op != s.op {
		return nil
	}
	s.calls++
	s.ids = append(s.ids, id)
	if s.calls <= s.failures {
		return s.err
	}
	return nil
}
func (s *retryStore) Collect(ctx context.Context, id string, step store.Step) (*store.Collection, error) {
	if err := s.refuse("Collect", id); err != nil {
		return nil, err
	}
	return s.Store.Collect(ctx, id, step)
}
func (s *retryStore) TidyFiles(ctx context.Context) error { return s.Store.TidyFiles(ctx) }
func (s *retryStore) FinishRun(ctx context.Context, id string, counts store.Counts) (store.Run, error) {
	if err := s.refuse("FinishRun", id); err != nil {
		return store.Run{}, err
	}
	return s.Store.FinishRun(ctx, id, counts)
}
func (s *retryStore) FailRun(ctx context.Context, id, reason string) (store.Run, error) {
	if err := s.refuse("FailRun", id); err != nil {
		return store.Run{}, err
	}
	return s.Store.FailRun(ctx, id, reason)
}

type diagnosticCapture struct {
	mu    sync.Mutex
	lines []string
}

func (c *diagnosticCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, string(p))
	return len(p), nil
}
func (c *diagnosticCapture) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}
func retryEngine(t *testing.T, f *fixture, s runs.Store, sc source.Scraper, sleep func(context.Context, time.Duration), out *diagnosticCapture) *runs.Engine {
	t.Helper()
	cfg := runs.Config{s, scrapers.New(sc), f.w, f.em, sleep, out, func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }, 0, 0, ""}
	e := runs.New(cfg)
	var _ events.Handler = e.Deliver
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.Drain(ctx)
	})
	return e
}

// R-2XD3-5S71 R-QKCB-RXSE R-QLK8-5PJ3 R-QMS4-JH9S R-QP7X-B0R6 R-R2MT-IHWT R-R3UP-W9NI R-R52M-A1E7 R-R6AI-NT4W R-3EFO-IKKR R-3FNK-WCBG R-3GVH-A425
func TestWriteRetriesAndDiagnostics(t *testing.T) {
	if runs.WriteRetries != 5 || runs.WriteBackoff != time.Second || runs.Retrying == "" || runs.GaveUp == "" || strings.Contains(runs.Retrying+runs.GaveUp, "\n") {
		t.Fatal("retry declarations")
	}
	for _, op := range []string{"FinishRun", "FailRun"} {
		for _, mode := range []string{"success", "exhausted", "other", "clean"} {
			t.Run(op+"/"+mode, func(t *testing.T) {
				f := setup(t)
				failures := 2
				if op == "FinishRun" {
					failures = 3
				}
				err := errors.Join(store.ErrUnreachable, store.ErrLocked, errors.New("chosen\nerror"))
				switch mode {
				case "exhausted":
					failures = runs.WriteRetries + 1
				case "other":
					failures = 1
					err = errors.Join(store.ErrUnreachable, errors.New("chosen\nerror"))
				case "clean":
					failures = 0
				}
				s := &retryStore{Store: f.st, op: op, failures: failures, err: err}
				out := new(diagnosticCapture)
				pauses := []time.Duration{}
				sleep := func(_ context.Context, d time.Duration) {
					pauses = append(pauses, d)
					if len(out.snapshot()) != len(pauses) {
						t.Error("diagnostic did not precede pause")
					}
				}

				sc := fake{"fake", func(_ context.Context, _ func([]source.Row) error) error {

					if op == "FailRun" {
						return errors.New("source failed")
					}
					return nil
				}}
				e := retryEngine(t, f, s, sc, sleep, out)
				r, startErr := e.Start(context.Background(), caller(), "fake")
				if startErr != nil {
					t.Fatal(startErr)
				}
				if mode == "other" || mode == "exhausted" {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					for len(out.snapshot()) != failures {
						select {
						case <-ctx.Done():
							t.Fatal("ending did not give up")
						default:
							runtime.Gosched()
						}
					}
				} else {
					ended(t, f.st, r.ID)
				}
				e.Drain(context.Background())
				s.mu.Lock()
				calls := s.calls
				ids := append([]string(nil), s.ids...)

				s.mu.Unlock()
				wantCalls := failures + 1
				if mode == "other" || mode == "exhausted" {
					wantCalls = failures
				}
				if calls != wantCalls {
					t.Fatalf("calls %d want %d", calls, wantCalls)
				}
				for _, id := range ids {
					if id != r.ID {
						t.Fatal("retried different run", ids)
					}
				}
				wantPauses := failures
				if mode == "other" {
					wantPauses = 0
				}
				if mode == "exhausted" {
					wantPauses = runs.WriteRetries
				}
				wantDurations := []time.Duration{}
				for i := 0; i < wantPauses; i++ {
					wantDurations = append(wantDurations, runs.WriteBackoff<<i)
				}
				if !reflect.DeepEqual(pauses, wantDurations) {
					t.Fatal(pauses, wantDurations)
				}
				lines := out.snapshot()
				if len(lines) != failures {
					t.Fatal(lines)
				}
				for i, line := range lines {
					wording := runs.Retrying
					if i >= wantPauses {
						wording = runs.GaveUp
					}
					want := "scrapes: " + r.ID + " " + op + ": " + wording + ": " + strings.ReplaceAll(err.Error(), "\n", "; ") + "\n"
					if line != want {
						t.Fatalf("%q want %q", line, want)
					}
				}

				got, getErr := f.st.GetRun(context.Background(), r.ID)
				if getErr != nil {
					t.Fatal(getErr)
				}
				status, reason := store.StatusDone, ""
				finished := true
				if op == "FailRun" {
					status, reason = store.StatusFailed, store.ReasonSource
				}
				if mode == "other" || mode == "exhausted" {
					status, reason, finished = store.StatusRunning, "", false
				}
				if got.Status != status || got.Reason != reason {
					t.Fatal(got)
				}
				f.flush(t)
				wantEvents := 1
				if finished {
					wantEvents = 2
				}
				if len(f.ec.Events()) != wantEvents || len(f.tc.Events()) != wantEvents+map[bool]int{true: 2, false: 0}[op == "FinishRun"] {
					t.Fatal(f.ec.Events(), f.tc.Events())
				}

			})
		}
	}
}

func TestRetryWaitsForEachInjectedPause(t *testing.T) {
	for _, op := range []string{"FinishRun"} {
		t.Run(op, func(t *testing.T) {
			f := setup(t)
			failures := 2
			if op == "FinishRun" {
				failures = 3
			}
			s := &retryStore{Store: f.st, op: op, failures: failures, err: errors.Join(store.ErrLocked, store.ErrUnreachable)}
			out := new(diagnosticCapture)
			paused := make(chan time.Duration)
			release := make(chan struct{})
			batchResult := make(chan error, 1)
			sc := fake{"fake", func(_ context.Context, batch func([]source.Row) error) error {
				err := batch([]source.Row{{SourceID: "kept"}})
				batchResult <- err
				return err
			}}
			sleep := func(ctx context.Context, d time.Duration) {
				select {
				case paused <- d:
				case <-ctx.Done():
					return
				}
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			e := retryEngine(t, f, s, sc, sleep, out)
			r, err := e.Start(context.Background(), caller(), "fake")
			if err != nil {
				t.Fatal(err)
			}
			for i := range failures {
				if d := receive(t, paused); d != runs.WriteBackoff<<i {
					t.Fatal(d)
				}
				s.mu.Lock()
				calls := s.calls
				s.mu.Unlock()
				if calls != i+1 || len(out.snapshot()) != i+1 {
					t.Fatal("retry advanced before pause returned", calls, out.snapshot())
				}
				stored, err := f.st.GetRun(context.Background(), r.ID)
				if err != nil || stored.Status != store.StatusRunning {
					t.Fatal(stored, err)
				}

				release <- struct{}{}
			}
			if err := receive(t, batchResult); err != nil {
				t.Fatal(err)
			}
			done := ended(t, f.st, r.ID)
			e.Drain(context.Background())
			if done.Status != store.StatusDone || done.Rows != 1 {
				t.Fatal(done)
			}
			if _, err := f.st.GetRow(context.Background(), r.ID, "kept"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDrainDuringWritePause(t *testing.T) {
	for _, op := range []string{"FinishRun", "FailRun"} {
		t.Run(op, func(t *testing.T) {
			f := setup(t)
			s := &retryStore{Store: f.st, op: op, failures: 100, err: errors.Join(store.ErrLocked, store.ErrUnreachable)}
			out := new(diagnosticCapture)
			paused := make(chan context.Context, 1)
			released := make(chan struct{})
			workerDone := make(chan struct{})
			sleep := func(ctx context.Context, _ time.Duration) { paused <- ctx; <-ctx.Done(); <-released }
			sc := fake{"fake", func(_ context.Context, _ func([]source.Row) error) error {
				defer close(workerDone)

				if op == "FailRun" {
					return errors.New("source")
				}
				return nil
			}}
			e := retryEngine(t, f, s, sc, sleep, out)
			r, err := e.Start(context.Background(), caller(), "fake")
			if err != nil {
				t.Fatal(err)
			}
			pausectx := receive(t, paused)
			ctx, cancel := context.WithCancel(context.Background())
			observed := &observedContext{Context: ctx, seen: make(chan struct{})}
			drained := make(chan struct{})
			go func() { e.Drain(observed); close(drained) }()
			receive(t, observed.seen)
			if pausectx.Err() != nil {
				t.Fatal("ending cancelled before drain deadline")
			}
			cancel()
			receive(t, pausectx.Done())
			receive(t, drained)
			close(released)
			receive(t, workerDone)
			e.Drain(context.Background())
			s.mu.Lock()
			calls := s.calls
			s.mu.Unlock()
			if calls != 1 {
				t.Fatal(calls)
			}
			if len(out.snapshot()) != 1 {
				t.Fatal(out.snapshot())
			}
			got, err := f.st.GetRun(context.Background(), r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != store.StatusRunning {
				t.Fatal("ending changed at drain", got)
			}
		})
	}
}

func TestEndingPauseKeepsEndingAndAllowsOtherStarts(t *testing.T) {
	for _, op := range []string{"FinishRun", "FailRun"} {
		t.Run(op, func(t *testing.T) {
			f := setup(t)
			s := &retryStore{Store: f.st, op: op, failures: 1, err: errors.Join(store.ErrUnreachable, store.ErrLocked)}
			out := new(diagnosticCapture)
			paused := make(chan context.Context, 1)
			release := make(chan struct{})
			otherEntered := make(chan struct{})
			sleep := func(ctx context.Context, _ time.Duration) { paused <- ctx; <-release }
			sc := fake{"fake", func(context.Context, func([]source.Row) error) error {
				if op == "FailRun" {
					return errors.New("source")
				}
				return nil
			}}
			other := fake{"other", func(ctx context.Context, _ func([]source.Row) error) error {
				close(otherEntered)
				<-ctx.Done()
				return ctx.Err()
			}}
			cfg := runs.Config{s, scrapers.New(sc, other), f.w, f.em, sleep, out, func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }, 0, 0, ""}
			e := runs.New(cfg)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				e.Drain(ctx)
			})
			r, err := e.Start(context.Background(), caller(), "fake")
			if err != nil {
				t.Fatal(err)
			}
			pausectx := receive(t, paused)
			started := make(chan error, 1)
			go func() { _, err := e.Start(context.Background(), caller(), "other"); started <- err }()
			if err := receive(t, started); err != nil {
				t.Fatal(err)
			}
			receive(t, otherEntered)
			_, err = e.Start(context.Background(), caller(), "fake")
			var rf *runs.Refusal
			if !errors.As(err, &rf) || rf.Word != runs.RefusalBusy {
				t.Fatal(err)
			}
			observed := &observedContext{Context: context.Background(), seen: make(chan struct{})}
			drained := make(chan struct{})
			go func() { e.Drain(observed); close(drained) }()
			receive(t, observed.seen)
			select {
			case <-drained:
				t.Fatal("drain abandoned pending ending")
			default:
			}
			if pausectx.Err() != nil {
				t.Fatal("ending pause cancelled at drain start")
			}
			close(release)
			receive(t, drained)
			got, err := f.st.GetRun(context.Background(), r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if op == "FinishRun" {
				if got.Status != store.StatusDone {
					t.Fatal(got)
				}
			} else if got.Status != store.StatusFailed || got.Reason != store.ReasonSource {
				t.Fatal(got)
			}
			f.flush(t)
			finished := 0
			for _, ev := range f.ec.Events() {
				if ev.Name == "scrape.finished" && ev.Attrs["run"] == r.ID {
					finished++
				}
			}
			if finished != 1 {
				t.Fatal(f.ec.Events())
			}
		})
	}
}

type heldSuccessfulEnding struct {
	runs.Store
	stored  chan store.Run
	release chan struct{}
}

func (s *heldSuccessfulEnding) FinishRun(ctx context.Context, id string, counts store.Counts) (store.Run, error) {
	r, err := s.Store.FinishRun(ctx, id, counts)
	s.stored <- r
	<-s.release
	return r, err
}

// R-6IFH-K1FU
func TestSuccessfulEndingCrossingDrainDeadlineEmitsFinished(t *testing.T) {
	f := setup(t)
	s := &heldSuccessfulEnding{Store: f.st, stored: make(chan store.Run, 1), release: make(chan struct{})}
	out := new(diagnosticCapture)
	sc := fake{"fake", func(context.Context, func([]source.Row) error) error { return nil }}
	e := retryEngine(t, f, s, sc, func(context.Context, time.Duration) { t.Error("successful ending paused") }, out)
	r, err := e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	stored := receive(t, s.stored)
	if stored.ID != r.ID || stored.Status != store.StatusDone {
		t.Fatal(stored)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	drained := make(chan struct{})
	go func() { e.Drain(ctx); close(drained) }()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("successful in-flight write blocked drain deadline")
	}
	f.flush(t)
	if len(f.tc.Events()) != 3 || len(f.ec.Events()) != 1 {
		t.Fatal("finished published before ending returned", f.tc.Events(), f.ec.Events())
	}
	close(s.release)
	waitctx, waitcancel := context.WithTimeout(context.Background(), time.Second)
	defer waitcancel()
	for {
		f.flush(t)
		if len(f.tc.Events()) >= 4 && len(f.ec.Events()) >= 2 {
			break
		}
		select {
		case <-waitctx.Done():
			t.Fatal("successful ending lost finished event", f.tc.Events(), f.ec.Events())
		default:
			runtime.Gosched()
		}
	}
	for _, list := range [][]string{{f.tc.Events()[0].Name, f.tc.Events()[3].Name}, {f.ec.Events()[0].Name, f.ec.Events()[1].Name}} {
		if !reflect.DeepEqual(list, []string{"scrape.started", "scrape.finished"}) {
			t.Fatal(list)
		}
	}
	if len(f.tc.Events()) != 4 || len(f.ec.Events()) != 2 {
		t.Fatal(f.tc.Events(), f.ec.Events())
	}
	if len(out.snapshot()) != 0 {
		t.Fatal(out.snapshot())
	}
	got, err := f.st.GetRun(context.Background(), r.ID)
	if err != nil || got != stored {
		t.Fatal("drain replaced successful ending", got, err)
	}
}

// R-QKCB-RXSE
func TestNilSleepBuildPauseEndsOnDrain(t *testing.T) {
	f := setup(t)
	now := func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }
	st := store.New(f.d, store.Config{Now: now, Rand: new(random), Runs: f.dir, ChunkBytes: 1})
	sleeping := make(chan struct{})
	result := make(chan error, 1)
	s := &observedBuildPause{Store: st, sleeping: sleeping}
	e := runs.New(runs.Config{Store: s, Scrapers: scrapers.New(fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
		err := b([]source.Row{{SourceID: "one"}})
		result <- err
		return err
	}}), Telemetry: f.w, Events: f.em, Now: now, Budget: 1})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.Drain(ctx)
	})
	r, err := e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	// The observed context signals when the default Sleep evaluates its Done
	// channel, after its long budget pause has begun. A no-op default never
	// reaches this observation; a default ignoring cancellation cannot release
	// the batch within the drain's deadline.
	receive(t, sleeping)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	drained := make(chan struct{})
	go func() { e.Drain(ctx); close(drained) }()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("default pause did not cancel", err)
		}
	case <-ctx.Done():
		t.Fatal("default pause ignored cancellation")
	}
	receive(t, drained)
	done, err := st.GetRun(context.Background(), r.ID)
	if err != nil || done.Status != store.StatusFailed || done.Reason != store.ReasonInterrupted {
		t.Fatal(done, err)
	}
}

type observedBuildPause struct {
	runs.Store
	sleeping chan struct{}
}

func (s *observedBuildPause) Collect(ctx context.Context, id string, step store.Step) (*store.Collection, error) {
	observed := &observedContext{Context: ctx, seen: s.sleeping}
	return s.Store.Collect(ctx, id, func(ctx context.Context, n int64, took time.Duration) error {
		observed.Context = ctx
		return step(observed, n, took)
	})
}

// R-3I3D-NVSU
func TestDrainDeadlineWhileStoreCallBlocked(t *testing.T) {
	for _, op := range []string{"Start", "Deliver", "RunningRuns", "FailRun", "TidyFiles"} {
		t.Run(op, func(t *testing.T) {
			f := setup(t)
			if op == "FailRun" {
				if _, err := f.st.AddRun(context.Background(), store.Run{Scraper: "old", UserID: "old"}); err != nil {
					t.Fatal(err)
				}
			}
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			storeOp := op
			if op == "Start" || op == "Deliver" {
				storeOp = "AddRun"
			}
			s := &heldEngineStore{Store: f.st, op: storeOp, entered: entered, release: release}
			e := runs.New(runs.Config{Store: s, Scrapers: scrapers.New(fake{"fake", func(ctx context.Context, _ func([]source.Row) error) error { <-ctx.Done(); return ctx.Err() }}), Telemetry: f.w, Events: f.em, Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }})
			returned := make(chan struct{})
			t.Cleanup(func() {
				unblock()
				receive(t, returned)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				e.Drain(ctx)
			})
			go func() {
				defer close(returned)
				switch op {
				case "Deliver":
					_ = e.Deliver(context.Background(), events.Delivery{Event: events.Event{ID: "blocked-delivery", Name: runs.FiredEvent("fake")}})
				case "Start":
					_, _ = e.Start(context.Background(), caller(), "fake")
				default:
					_ = e.Recover(context.Background())
				}
			}()
			receive(t, entered)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			drained := make(chan struct{})
			go func() { e.Drain(ctx); close(drained) }()
			select {
			case <-drained:
			case <-time.After(time.Second):
				t.Fatal("blocked store call prevented drain deadline")
			}
			unblock()
			receive(t, returned)
			endctx, endcancel := context.WithTimeout(context.Background(), time.Second)
			defer endcancel()
			e.Drain(endctx)
		})
	}
}

type heldEngineStore struct {
	runs.Store
	op      string
	entered chan struct{}
	release chan struct{}
}

func (s *heldEngineStore) hold(op string) {
	if op == s.op {
		close(s.entered)
		<-s.release
	}
}
func (s *heldEngineStore) AddRun(ctx context.Context, r store.Run) (store.Run, error) {
	s.hold("AddRun")
	return s.Store.AddRun(ctx, r)
}
func (s *heldEngineStore) RunningRuns(ctx context.Context) ([]store.Run, error) {
	s.hold("RunningRuns")
	return s.Store.RunningRuns(ctx)
}
func (s *heldEngineStore) FailRun(ctx context.Context, id, reason string) (store.Run, error) {
	s.hold("FailRun")
	return s.Store.FailRun(ctx, id, reason)
}
func (s *heldEngineStore) TidyFiles(ctx context.Context) error {
	s.hold("TidyFiles")
	return s.Store.TidyFiles(ctx)
}

// R-3I3D-NVSU R-2DWR-Y34J
func TestSuccessfulStartCrossingDrainDeadlineIsInterrupted(t *testing.T) {
	for _, delivery := range []bool{false, true} {
		t.Run(map[bool]string{false: "Start", true: "Deliver"}[delivery], func(t *testing.T) {
			f := setup(t)
			stored := make(chan store.Run, 1)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			s := &heldSuccessfulStart{Store: f.st, stored: stored, release: release}
			e := runs.New(runs.Config{Store: s, Scrapers: scrapers.New(fake{"fake", func(ctx context.Context, _ func([]source.Row) error) error {
				if ctx.Err() == nil {
					t.Error("late starter launched uncanceled scraper")
				}
				return ctx.Err()
			}}), Telemetry: f.w, Events: f.em, Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }})
			returned := make(chan struct{})
			t.Cleanup(func() {
				unblock()
				receive(t, returned)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				e.Drain(ctx)
			})
			go func() {
				defer close(returned)
				if delivery {
					_ = e.Deliver(context.Background(), events.Delivery{Event: events.Event{ID: "late-delivery", Name: runs.FiredEvent("fake")}})
				} else {
					_, _ = e.Start(context.Background(), caller(), "fake")
				}
			}()
			r := receive(t, stored)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			drained := make(chan struct{})
			go func() { e.Drain(ctx); close(drained) }()
			receive(t, drained)
			unblock()
			receive(t, returned)
			done := ended(t, f.st, r.ID)
			if done.Status != store.StatusFailed || done.Reason != store.ReasonInterrupted {
				t.Fatal(done)
			}
			e.Drain(context.Background())
			f.flush(t)
			telemetryNames, busNames := []string{}, []string{}
			for _, ev := range f.tc.Events() {
				telemetryNames = append(telemetryNames, ev.Name)
			}
			for _, ev := range f.ec.Events() {
				busNames = append(busNames, ev.Name)
			}
			for _, list := range [][]string{telemetryNames, busNames} {
				if !reflect.DeepEqual(list, []string{"scrape.started", "scrape.finished"}) {
					t.Fatal(list)
				}
			}
		})
	}
}

type heldSuccessfulStart struct {
	runs.Store
	stored  chan store.Run
	release chan struct{}
}

func (s *heldSuccessfulStart) AddRun(ctx context.Context, r store.Run) (store.Run, error) {
	r, err := s.Store.AddRun(ctx, r)
	s.stored <- r
	<-s.release
	return r, err
}

// R-3I3D-NVSU
func TestDrainDeadlineWhileDiagnosticBlocked(t *testing.T) {
	f := setup(t)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	out := &heldDiagnostic{entered: make(chan struct{}), release: release, exited: make(chan struct{})}
	s := &retryStore{Store: f.st, op: "Collect", failures: 1, err: errors.New("chosen error")}
	e := runs.New(runs.Config{Store: s, Scrapers: scrapers.New(fake{"fake", func(context.Context, func([]source.Row) error) error {
		t.Error("refused build walked source")
		return nil
	}}), Telemetry: f.w, Events: f.em, Stderr: out, Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }})
	t.Cleanup(func() {
		unblock()
		receive(t, out.exited)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.Drain(ctx)
	})
	if _, err := e.Start(context.Background(), caller(), "fake"); err != nil {
		t.Fatal(err)
	}
	receive(t, out.entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	drained := make(chan struct{})
	go func() { e.Drain(ctx); close(drained) }()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("blocked diagnostic prevented drain deadline")
	}
	unblock()
	receive(t, out.exited)
}

type heldDiagnostic struct {
	entered chan struct{}
	release chan struct{}
	exited  chan struct{}
}

func (w *heldDiagnostic) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	close(w.exited)
	return len(p), nil
}

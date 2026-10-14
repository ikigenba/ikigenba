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
	rows     [][]source.Row
	mu       sync.Mutex
}

func (s *retryStore) AddRun(ctx context.Context, r store.Run) (store.Run, error) {
	return s.Store.AddRun(ctx, r)
}
func (s *retryStore) RunningRuns(ctx context.Context) ([]store.Run, error) {
	return s.Store.RunningRuns(ctx)
}

func (s *retryStore) refuse(op, id string, rows []source.Row) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if op != s.op {
		return nil
	}
	s.calls++
	s.ids = append(s.ids, id)
	s.rows = append(s.rows, append([]source.Row(nil), rows...))
	if s.calls <= s.failures {
		return s.err
	}
	return nil
}
func (s *retryStore) PutRows(ctx context.Context, id string, rows []source.Row) error {
	if err := s.refuse("PutRows", id, rows); err != nil {
		return err
	}
	return s.Store.PutRows(ctx, id, rows)
}
func (s *retryStore) FinishRun(ctx context.Context, id string) (store.Run, error) {
	if err := s.refuse("FinishRun", id, nil); err != nil {
		return store.Run{}, err
	}
	return s.Store.FinishRun(ctx, id)
}
func (s *retryStore) FailRun(ctx context.Context, id, reason string) (store.Run, error) {
	if err := s.refuse("FailRun", id, nil); err != nil {
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
	cfg := runs.Config{s, scrapers.New(sc), f.w, f.em, sleep, out}
	e := runs.New(cfg)
	var _ events.Handler = e.Deliver
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.Drain(ctx)
	})
	return e
}

// R-L3O3-9N6P R-L4VZ-NEXE R-L63W-16O3 R-L8JO-SQ5H R-LAZH-K9MV R-LC7D-Y1DK R-LDFA-BT49
func TestWriteRetriesAndDiagnostics(t *testing.T) {
	if runs.WriteRetries != 5 || runs.WriteBackoff != time.Second || runs.Retrying == "" || runs.GaveUp == "" || strings.Contains(runs.Retrying+runs.GaveUp, "\n") {
		t.Fatal("retry declarations")
	}
	for _, op := range []string{"PutRows", "FinishRun", "FailRun"} {
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
				batch := []source.Row{{SourceID: "first", Raw: []byte(`{"value":"private"}`)}, {SourceID: "second"}}
				sc := fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
					if op == "PutRows" {
						return b(batch)
					}
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
				if (mode == "other" || mode == "exhausted") && op != "PutRows" {
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
				rows := append([][]source.Row(nil), s.rows...)
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
				if op == "PutRows" {
					for _, got := range rows {
						if !reflect.DeepEqual(got, batch) {
							t.Fatal("retried different rows", got)
						}
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
					if op == "PutRows" {
						status, reason = store.StatusFailed, store.ReasonStore
					} else {
						status, reason, finished = store.StatusRunning, "", false
					}
				}
				if got.Status != status || got.Reason != reason {
					t.Fatal(got)
				}
				f.flush(t)
				wantEvents := 1
				if finished {
					wantEvents = 2
				}
				if len(f.ec.Events()) != wantEvents || len(f.tc.Events()) != wantEvents {
					t.Fatal(f.ec.Events(), f.tc.Events())
				}
				if status == store.StatusDone && op == "PutRows" && got.Rows != int64(len(batch)) {
					t.Fatal(got)
				}
			})
		}
	}
}

// R-L7BS-EYES R-L8JO-SQ5H R-LAZH-K9MV R-LC7D-Y1DK R-LDFA-BT49
func TestRetryWaitsForEachInjectedPause(t *testing.T) {
	for _, op := range []string{"PutRows", "FinishRun"} {
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
				if op == "PutRows" {
					if _, err := f.st.GetRow(context.Background(), r.ID, "kept"); !errors.Is(err, store.ErrNotFound) {
						t.Fatal("batch stored before retry", err)
					}
					select {
					case err := <-batchResult:
						t.Fatal("batch returned before retry", err)
					default:
					}
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

// R-L7BS-EYES R-L8JO-SQ5H R-LC7D-Y1DK R-LDFA-BT49
func TestFailingHandleRefusesBatchWithoutPause(t *testing.T) {
	f := setup(t)
	out := new(diagnosticCapture)
	batchResult := make(chan error, 1)
	sc := fake{"fake", func(_ context.Context, batch func([]source.Row) error) error {
		f.d.SetFailing(true)
		err := batch([]source.Row{{SourceID: "refused"}})
		f.d.SetFailing(false)
		batchResult <- err
		return err
	}}
	e := retryEngine(t, f, f.st, sc, func(context.Context, time.Duration) { t.Error("non-lock error paused") }, out)
	r, err := e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	batchErr := receive(t, batchResult)
	if !errors.Is(batchErr, store.ErrUnreachable) || errors.Is(batchErr, store.ErrLocked) {
		t.Fatal(batchErr)
	}
	done := ended(t, f.st, r.ID)
	e.Drain(context.Background())
	if done.Status != store.StatusFailed || done.Reason != store.ReasonStore {
		t.Fatal(done)
	}
	want := "scrapes: " + r.ID + " PutRows: " + runs.GaveUp + ": " + strings.ReplaceAll(batchErr.Error(), "\n", "; ") + "\n"
	if lines := out.snapshot(); !reflect.DeepEqual(lines, []string{want}) {
		t.Fatal(lines, want)
	}
}

// R-L8JO-SQ5H R-LAZH-K9MV R-LDFA-BT49 R-LFV3-3CLN
func TestDrainDuringWritePause(t *testing.T) {
	for _, op := range []string{"PutRows", "FinishRun", "FailRun"} {
		t.Run(op, func(t *testing.T) {
			f := setup(t)
			s := &retryStore{Store: f.st, op: op, failures: 100, err: errors.Join(store.ErrLocked, store.ErrUnreachable)}
			out := new(diagnosticCapture)
			paused := make(chan context.Context, 1)
			released := make(chan struct{})
			workerDone := make(chan struct{})
			sleep := func(ctx context.Context, _ time.Duration) { paused <- ctx; <-ctx.Done(); <-released }
			sc := fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
				defer close(workerDone)
				if op == "PutRows" {
					return b([]source.Row{{SourceID: "first"}})
				}
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
			if op != "PutRows" && pausectx.Err() != nil {
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
			if op == "PutRows" {
				if got.Status != store.StatusFailed || got.Reason != store.ReasonInterrupted {
					t.Fatal(got)
				}
			} else if got.Status != store.StatusRunning {
				t.Fatal("ending changed at drain", got)
			}
		})
	}
}

// R-LAZH-K9MV R-LFV3-3CLN
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
			cfg := runs.Config{s, scrapers.New(sc, other), f.w, f.em, sleep, out}
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

func (s *heldSuccessfulEnding) FinishRun(ctx context.Context, id string) (store.Run, error) {
	r, err := s.Store.FinishRun(ctx, id)
	s.stored <- r
	<-s.release
	return r, err
}

// R-LAZH-K9MV R-LFV3-3CLN R-LDFA-BT49 R-6IFH-K1FU
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
	if len(f.tc.Events()) != 1 || len(f.ec.Events()) != 1 {
		t.Fatal("finished published before ending returned", f.tc.Events(), f.ec.Events())
	}
	close(s.release)
	waitctx, waitcancel := context.WithTimeout(context.Background(), time.Second)
	defer waitcancel()
	for {
		f.flush(t)
		if len(f.tc.Events()) >= 2 && len(f.ec.Events()) >= 2 {
			break
		}
		select {
		case <-waitctx.Done():
			t.Fatal("successful ending lost finished event", f.tc.Events(), f.ec.Events())
		default:
			runtime.Gosched()
		}
	}
	for _, list := range [][]string{{f.tc.Events()[0].Name, f.tc.Events()[1].Name}, {f.ec.Events()[0].Name, f.ec.Events()[1].Name}} {
		if !reflect.DeepEqual(list, []string{"scrape.started", "scrape.finished"}) {
			t.Fatal(list)
		}
	}
	if len(f.tc.Events()) != 2 || len(f.ec.Events()) != 2 {
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

type heldDiagnostic struct {
	entered chan struct{}
	release chan struct{}
}

func (w *heldDiagnostic) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(p), nil
}

// R-L4VZ-NEXE
func TestNilSleepEndsImmediatelyForCancelledWork(t *testing.T) {
	f := setup(t)
	s := &retryStore{Store: f.st, op: "PutRows", failures: 1, err: errors.Join(store.ErrUnreachable, store.ErrLocked)}
	out := &heldDiagnostic{entered: make(chan struct{}), release: make(chan struct{})}
	work := make(chan context.Context, 1)
	sc := fake{"fake", func(ctx context.Context, b func([]source.Row) error) error {
		work <- ctx
		return b([]source.Row{{SourceID: "first"}})
	}}
	e := runs.New(runs.Config{Store: s, Scrapers: scrapers.New(sc), Telemetry: f.w, Events: f.em, Stderr: out})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.Drain(ctx)
	})
	r, err := e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	workctx := receive(t, work)
	receive(t, out.entered)
	observed := &observedContext{Context: context.Background(), seen: make(chan struct{})}
	drained := make(chan struct{})
	go func() { e.Drain(observed); close(drained) }()
	receive(t, observed.seen)
	receive(t, workctx.Done())
	// The diagnostic precedes Sleep; releasing it now gives the default Sleep
	// an already-cancelled context, so it must not wait its one-second timer.
	close(out.release)
	select {
	case <-drained:
	case <-time.After(time.Second / 2):
		t.Fatal("nil Sleep ignored cancelled context")
	}
	s.mu.Lock()
	calls := s.calls
	s.mu.Unlock()
	if calls != 1 {
		t.Fatal(calls)
	}
	got, err := f.st.GetRun(context.Background(), r.ID)
	if err != nil || got.Status != store.StatusFailed || got.Reason != store.ReasonInterrupted {
		t.Fatal(got, err)
	}
}

package passes_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	candidates "github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

type fillWriter func([]byte) (int, error)

func (w fillWriter) Write(p []byte) (int, error) { return w(p) }

func fillEngine(f *fixture, out io.Writer) {
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Stderr: out})
}
func assertClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	default:
		t.Fatal("channel not already closed")
	}
}
func waitFillDrain(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("drain did not cut off fill")
	}
}
func assertOpen(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("channel closed early")
	default:
	}
}

// holdFillWriter lets FillFacts enter its injected busy retry, without making
// a test sleep or relying on a worker's scheduling.
func holdFillTransaction(t *testing.T, f *fixture) (<-chan struct{}, func()) {
	t.Helper()
	ctx := context.Background()
	second, err := db.Open(ctx, db.Config{Path: f.path, Migrations: candidates.Migrations(), Now: func() time.Time { return time.Unix(100000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	locked, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- second.Write(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE candidates SET name=name"); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	waitSignal(t, locked)
	var once sync.Once
	unlock := func() {
		once.Do(func() {
			close(release)
			if err := <-finished; err != nil {
				t.Error(err)
			}
			if err := second.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(unlock)
	return release, unlock
}

func holdFillWriter(t *testing.T, f *fixture) (<-chan struct{}, func()) {
	t.Helper()
	release, unlock := holdFillTransaction(t, f)
	asleep := make(chan struct{})
	var first sync.Once
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return time.Unix(100000, 0) }, Rand: &random{}, Sleep: func(ctx context.Context, _ time.Duration) {
		first.Do(func() { close(asleep) })
		select {
		case <-ctx.Done():
		case <-release:
		}
	}})
	return asleep, unlock
}

// R-3T2Z-OUBV R-3RV3-B2L6 R-3WQO-U5JY
func TestFillRepairsWholeCorpusAndEndsBeforeDiagnostic(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, true)
	ids := seedEmails(t, f, 2*passes.BatchSize+1, func(i int) string { return fmt.Sprintf("owner@fill%d.test", i) })
	es := make([]store.Enrichment, 0, len(ids))
	for i, id := range ids {
		found, err := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: fmt.Sprintf("https://fill%d.test/", i)})
		if err != nil {
			t.Fatal(err)
		}
		es = append(es, store.Enrichment{Candidate: id, Kind: sources.WebsiteCheck, Inputs: fmt.Sprint(i), Found: found})
	}
	if _, err := f.st.AddEnrichments(t.Context(), es); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Write(t.Context(), func(tx *sql.Tx) error { _, err := tx.ExecContext(t.Context(), "DELETE FROM facts"); return err }); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finishWrite := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finishWrite)
	var writes [][]byte
	var judge store.Pass
	var judgeErr error
	fillEngine(f, fillWriter(func(p []byte) (int, error) {
		writes = append(writes, bytes.Clone(p))
		if len(f.requests()) != 0 || len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
			t.Error("fill contacted sibling or emitted event")
		}
		judge, judgeErr = f.eng.Judge(context.Background(), identity.Caller{UserID: "owner"})
		close(entered)
		<-release
		return len(p), nil
	}))
	done := f.eng.StartFill(t.Context())
	waitSignal(t, entered)
	assertOpen(t, done)
	if judgeErr != nil {
		t.Fatal("diagnostic handed over before fill ended", judgeErr)
	}
	finishWrite()
	waitSignal(t, done)
	if len(writes) != 1 || string(writes[0]) != "candidates: facts filled\n" {
		t.Fatal(writes)
	}
	for i, id := range ids {
		got, err := f.st.GetFacts(t.Context(), id)
		if err != nil || got.Website != sources.WebsiteHTTPS || got.WebsiteURL != fmt.Sprintf("https://fill%d.test/", i) {
			t.Fatal(id, got, err)
		}
	}
	f.ended(judge.ID)
}

// R-3O7E-5RD3 R-3WQO-U5JY
func TestFillFailureAndNilDiagnostics(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"failure", "failure nil", "success nil"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, nil, false)
			if mode != "success nil" {
				f.d.SetFailing(true)
			}
			var writes [][]byte
			var judge store.Pass
			var judgeErr error
			var out io.Writer
			if mode == "failure" {
				expected := f.d.Write(t.Context(), func(*sql.Tx) error { return nil })
				out = fillWriter(func(p []byte) (int, error) {
					writes = append(writes, bytes.Clone(p))
					f.d.SetFailing(false)
					if !strings.HasPrefix(string(p), "candidates: facts fill failed\n\n") || !strings.HasSuffix(string(p), "\n") || strings.Count(string(p), "\n") != 3 || !strings.Contains(string(p), strings.ReplaceAll(expected.Error(), "\n", " ")) {
						t.Error(string(p), expected)
					}
					judge, judgeErr = f.eng.Judge(context.Background(), identity.Caller{UserID: "owner"})
					return len(p), nil
				})
			}
			fillEngine(f, out)
			waitSignal(t, f.eng.StartFill(t.Context()))
			f.d.SetFailing(false)
			if mode == "failure" {
				if len(writes) != 1 || judgeErr != nil {
					t.Fatal(writes, judgeErr)
				}
				f.ended(judge.ID)
			}
		})
	}
}

// R-3T2Z-OUBV R-12GP-CNA5 R-3XYL-7XAN R-3Z6H-LP1C
func TestFillCancellationAndDrainWhileWriterHeld(t *testing.T) {
	t.Parallel()
	for _, drain := range []bool{false, true} {
		t.Run(fmt.Sprint(drain), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, nil, false)
			asleep, unlock := holdFillWriter(t, f)
			var output bytes.Buffer
			fillEngine(f, &output)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := f.eng.StartFill(ctx)
			assertOpen(t, done)
			waitSignal(t, asleep)
			assertClosed(t, f.eng.StartFill(t.Context()))
			assertOpen(t, done)
			if drain {
				drained := make(chan struct{})
				go func() { f.eng.Drain(context.Background()); close(drained) }()
				waitFillDrain(t, drained)
			} else {
				cancel()
			}
			waitSignal(t, done)
			if output.Len() != 0 {
				t.Fatal(output.String())
			}
			// The fill ends before the other handle's write transaction is released.
			unlock()
			if drain {
				assertClosed(t, f.eng.StartFill(t.Context()))
			}
		})
	}
}

// R-3UAW-2M2K R-40ED-ZGS1 R-3RV3-B2L6
func TestFillBusyRefusalOrderingAndNoStartParksRead(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected sibling read %s", r.URL.Path)
		w.WriteHeader(404)
	}, true)
	ids := seedEmails(t, f, 1, func(int) string { return "owner@fill.test" })
	ks := seedWalkParks(t, f, 1)
	asleep, unlock := holdFillWriter(t, f)
	fillEngine(f, nil)
	// Give the valid walk everything it needs without making Places requests.
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Places: walkClient(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected Places request")
		return nil, errors.New("unexpected")
	})})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := f.eng.StartFill(ctx)
	waitSignal(t, asleep)
	before := f.list()
	caller := identity.Caller{UserID: "owner"}
	calls := []func() (store.Pass, error){
		func() (store.Pass, error) { return f.eng.Judge(t.Context(), caller) },
		func() (store.Pass, error) { return f.eng.Ingest(t.Context(), caller, "") },
		func() (store.Pass, error) { return f.eng.Enrich(t.Context(), caller, "") },
		func() (store.Pass, error) {
			return f.eng.EnrichTarget(t.Context(), caller, enrichers.KindLinkFacts, ids[0], false)
		},
		func() (store.Pass, error) { return f.eng.EditParkRules(t.Context(), caller, store.ParkRulesEdit{}) },
		func() (store.Pass, error) { return f.eng.Walk(t.Context(), caller, parkIDs(ks), false) },
	}
	for _, call := range calls {
		p, err := call()
		assertRefusal(t, p, err, passes.RefusalBusy)
	}
	p, err := f.eng.Ingest(t.Context(), caller, "nope")
	assertRefusal(t, p, err, passes.RefusalInvalid)
	p, err = f.eng.Enrich(t.Context(), caller, "nope")
	assertRefusal(t, p, err, passes.RefusalInvalid)
	p, err = f.eng.Enrich(t.Context(), caller, sources.WebsiteCheck)
	assertRefusal(t, p, err, passes.RefusalUnavailable)
	p, err = f.eng.EnrichTarget(t.Context(), caller, enrichers.KindLinkFacts, "nope", false)
	assertRefusal(t, p, err, passes.RefusalNotFound)
	zero := int64(0)
	p, err = f.eng.EditParkRules(t.Context(), caller, store.ParkRulesEdit{Distance: &zero})
	assertRefusal(t, p, err, passes.RefusalInvalid)
	p, ok := f.eng.StartParks(t.Context(), "start")
	if ok || !reflect.DeepEqual(p, store.Pass{}) {
		t.Fatal(p, ok)
	}
	if !reflect.DeepEqual(before, f.list()) || len(f.requests()) != 0 {
		t.Fatal("refused calls changed passes or read scrapes")
	}
	f.flush()
	if len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
		t.Fatal("refused calls emitted events")
	}
	cancel()
	waitSignal(t, done)
	unlock()
	p, err = f.eng.Judge(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	f.ended(p.ID)
}

// R-3UAW-2M2K
func TestFillKeepsMissingScrapesRefusalAheadOfBusy(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	asleep, unlock := holdFillWriter(t, f)
	fillEngine(f, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := f.eng.StartFill(ctx)
	waitSignal(t, asleep)
	p, err := f.eng.EditParkRules(t.Context(), identity.Caller{UserID: "owner"}, store.ParkRulesEdit{})
	assertRefusal(t, p, err, passes.RefusalUnavailable)
	cancel()
	waitSignal(t, done)
	unlock()
}

// R-3VIS-GDT9
func TestFillDeliveryClassificationDedupeAndRecordReadBeforeBusy(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"done", "missing", "unfinished", "socket failure", "no client", "duplicate", "store failure", "parks missing", "parks done", "parks unfinished"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/runs/fill-run/record" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				switch mode {
				case "missing", "parks missing":
					w.WriteHeader(404)
				case "unfinished", "parks unfinished":
					record(w, "fill-run", 0, store.StatusRunning)
				default:
					record(w, "fill-run", 0, store.StatusDone)
				}
			}, mode != "no client")
			if mode == "socket failure" {
				f.sc = newFixture(t, nil, true).sc
			}
			if mode == "duplicate" {
				p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "fill-run", Delivery: "old-event"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.st.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
					t.Fatal(err)
				}
			}
			asleep, unlock := holdFillWriter(t, f)
			fillEngine(f, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := f.eng.StartFill(ctx)
			waitSignal(t, asleep)
			before := f.list()
			if mode == "store failure" {
				f.d.SetFailing(true)
			}
			scraper := "fmcsa"
			if strings.HasPrefix(mode, "parks ") {
				scraper = parks.Scraper
			}
			d := events.Delivery{Event: events.Event{ID: "fill-event", Name: trail.Accepted, Attrs: events.Attrs{"scraper": scraper, "run": "fill-run", "status": store.StatusDone}}}
			want := events.Fail(passes.Busy)
			switch mode {
			case "missing", "unfinished", "duplicate", "parks missing", "parks unfinished":
				want = events.Skip()
			case "store failure":
				want = events.Fail(store.Unreachable)
			}
			if got := f.eng.Deliver(t.Context(), d); got != want {
				t.Fatal(got, want)
			}
			f.d.SetFailing(false)
			if !reflect.DeepEqual(before, f.list()) {
				t.Fatal("delivery recorded pass during fill")
			}
			expectedReads := 1
			switch mode {
			case "socket failure", "no client", "duplicate", "store failure":
				expectedReads = 0
			}
			if len(f.requests()) != expectedReads {
				t.Fatal(f.requests())
			}
			if got := f.eng.Deliver(t.Context(), judgeDelivery("fill-judge")); got != events.Fail(passes.Busy) {
				t.Fatal(got)
			}
			d.Event.Name = "cron.other.fired"
			if got := f.eng.Deliver(t.Context(), d); got != events.Skip() {
				t.Fatal(got)
			}
			d.Event.Name = trail.Accepted
			d.Event.Attrs["scraper"] = "other"
			if got := f.eng.Deliver(t.Context(), d); got != events.Skip() {
				t.Fatal(got)
			}
			f.flush()
			if len(f.ec.Events()) != 0 {
				t.Fatal("fill delivery emitted lifecycle event")
			}
			cancel()
			waitSignal(t, done)
			unlock()
			if got := f.eng.Deliver(t.Context(), judgeDelivery("fill-judge")); got != events.OK() {
				t.Fatal(got)
			}
		})
	}
}

// R-3Z6H-LP1C R-3RV3-B2L6 R-41MA-D8IQ
func TestFillCannotInterruptRunningIngestOrItsFollowOn(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var onceEntered sync.Once
	var once sync.Once
	finishRows := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finishRows)
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/current") {
			record(w, "fill-ingest", 0, store.StatusDone)
			return
		}
		if r.URL.Path == "/runs/fill-ingest/rows" {
			onceEntered.Do(func() { close(entered) })
			<-release
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}, true)
	var writes bytes.Buffer
	fillEngine(f, &writes)
	p, err := f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, "fmcsa")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	assertClosed(t, f.eng.StartFill(t.Context()))
	full, err := f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	finishRows()
	f.ended(p.ID)
	f.ended(full.ID)
	follow := waitFollow(t, f, p.ID)
	if follow.Kind != store.KindEnrich || follow.Origin != store.OriginPass {
		t.Fatal(follow)
	}
	if writes.Len() != 0 {
		t.Fatal(writes.String())
	}
}

// R-41MA-D8IQ R-3Z6H-LP1C
func TestConcurrentFillAdmissionAndEngineMethods(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, true)
	asleep, unlock := holdFillWriter(t, f)
	fillEngine(f, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	const racers = 12
	results := make(chan (<-chan struct{}), racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Go(func() { <-start; results <- f.eng.StartFill(ctx) })
	}
	close(start)
	wg.Wait()
	close(results)
	var first <-chan struct{}
	for done := range results {
		select {
		case <-done:
		default:
			if first != nil {
				t.Fatal("two racing fills began")
			}
			first = done
		}
	}
	if first == nil {
		t.Fatal("no racing fill began")
	}
	waitSignal(t, asleep)
	caller := identity.Caller{UserID: "owner"}
	for _, call := range []func(){
		func() { p, e := f.eng.Ingest(t.Context(), caller, ""); assertRefusal(t, p, e, passes.RefusalBusy) },
		func() { p, e := f.eng.Judge(t.Context(), caller); assertRefusal(t, p, e, passes.RefusalBusy) },
		func() { p, e := f.eng.Enrich(t.Context(), caller, ""); assertRefusal(t, p, e, passes.RefusalBusy) },
		func() {
			p, e := f.eng.EnrichTarget(t.Context(), caller, "bad", "bad", false)
			assertRefusal(t, p, e, passes.RefusalInvalid)
		},
		func() {
			p, e := f.eng.EditParkRules(t.Context(), caller, store.ParkRulesEdit{})
			assertRefusal(t, p, e, passes.RefusalBusy)
		},
		func() {
			p, ok := f.eng.StartParks(t.Context(), "concurrent")
			if ok || p.ID != "" {
				t.Error(p, ok)
			}
		},
		func() {
			if got := f.eng.Deliver(t.Context(), judgeDelivery("concurrent")); got != events.Fail(passes.Busy) {
				t.Error(got)
			}
		},
		func() {
			if err := f.eng.Recover(t.Context()); err != nil {
				t.Error(err)
			}
		},
	} {
		wg.Go(call)
	}
	wg.Wait()
	assertClosed(t, f.eng.StartFill(t.Context()))
	assertOpen(t, first)
	drained := make(chan struct{})
	go func() { f.eng.Drain(context.Background()); close(drained) }()
	waitFillDrain(t, drained)
	waitSignal(t, first)
	unlock()
	assertClosed(t, f.eng.StartFill(t.Context()))
}

// R-41MA-D8IQ
func TestFillRacesIngestWithoutLosingFollowOn(t *testing.T) {
	t.Parallel()
	for range 12 {
		f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/current") {
				record(w, "race-ingest", 0, store.StatusDone)
				return
			}
			if r.URL.Path != "/runs/race-ingest/rows" {
				t.Errorf("unexpected path %s", r.URL.Path)
			}
		}, true)
		start := make(chan struct{})
		done := make(chan (<-chan struct{}), 1)
		type answer struct {
			p   store.Pass
			err error
		}
		result := make(chan answer, 1)
		go func() { <-start; done <- f.eng.StartFill(t.Context()) }()
		go func() {
			<-start
			p, err := f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, "fmcsa")
			result <- answer{p, err}
		}()
		close(start)
		a := <-result
		waitSignal(t, <-done)
		if a.err != nil {
			assertRefusal(t, a.p, a.err, passes.RefusalBusy)
		} else {
			ended := f.ended(a.p.ID)
			if ended.Status != store.StatusDone {
				t.Fatal(ended)
			}
			follow := waitFollow(t, f, a.p.ID)
			if follow.Kind != store.KindEnrich {
				t.Fatal(follow)
			}
		}
		f.eng.Drain(context.Background())
	}
}

// R-41MA-D8IQ
func TestFillRacesJudge(t *testing.T) {
	t.Parallel()
	for range 12 {
		f := newFixture(t, nil, false)
		start := make(chan struct{})
		done := make(chan (<-chan struct{}), 1)
		type answer struct {
			p   store.Pass
			err error
		}
		result := make(chan answer, 1)
		go func() { <-start; done <- f.eng.StartFill(t.Context()) }()
		go func() {
			<-start
			p, err := f.eng.Judge(t.Context(), identity.Caller{UserID: "owner"})
			result <- answer{p, err}
		}()
		close(start)
		a := <-result
		waitSignal(t, <-done)
		if a.err != nil {
			assertRefusal(t, a.p, a.err, passes.RefusalBusy)
		} else {
			ended := f.ended(a.p.ID)
			if ended.Status != store.StatusDone {
				t.Fatal(ended)
			}
		}
		f.eng.Drain(context.Background())
	}
}

// R-3WQO-U5JY R-3XYL-7XAN
func TestFillDiagnosticDoesNotKeepDrainWaiting(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	fillEngine(f, fillWriter(func(p []byte) (int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		f.eng.Drain(ctx)
		if ctx.Err() != nil {
			t.Error("drain waited after fill had ended")
		}
		return len(p), nil
	}))
	waitSignal(t, f.eng.StartFill(t.Context()))
}

// R-3T2Z-OUBV R-3RV3-B2L6 R-3Z6H-LP1C
func TestFillAdmitsRecoveredPassWhileItsReadStillWaits(t *testing.T) {
	t.Parallel()
	readStarted, readEnded := make(chan struct{}), make(chan struct{})
	f := newFixture(t, func(_ http.ResponseWriter, r *http.Request) {
		close(readStarted)
		<-r.Context().Done()
		close(readEnded)
	}, true)
	asleep := make(chan struct{})
	var once sync.Once
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return time.Unix(100000, 0) }, Rand: &random{}, Sleep: func(ctx context.Context, _ time.Duration) { once.Do(func() { close(asleep) }); <-ctx.Done() }})
	fillEngine(f, nil)
	p, err := f.eng.Ingest(t.Context(), identity.Caller{UserID: "owner"}, "fmcsa")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, readStarted)
	assertClosed(t, f.eng.StartFill(t.Context()))
	if err = f.eng.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.st.GetPass(t.Context(), p.ID)
	if err != nil || recovered.Status != store.StatusFailed || recovered.Reason != store.ReasonInterrupted {
		t.Fatal(recovered, err)
	}
	_, unlock := holdFillTransaction(t, f)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := f.eng.StartFill(ctx)
	assertOpen(t, done)
	waitSignal(t, asleep)
	assertOpen(t, readEnded)
	cancel()
	waitSignal(t, done)
	unlock()
	f.eng.Drain(context.Background())
	waitSignal(t, readEnded)
}

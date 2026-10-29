package passes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// R-SGG7-YQ5M R-3D03-LUMS R-3XQE-3Y8L R-TKAA-NXHG R-1AS2-ED4A
func TestScraperIngestScopeAndFollowOn(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}, 2), make(chan struct{})
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/scrapers/fmcsa/current" {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			record(w, "run_a", 1, store.StatusDone)
			return
		}
		if r.URL.Path != "/runs/run_a/rows" {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		row(w, 1)
	}, true)
	var once sync.Once
	defer once.Do(func() { close(release) })
	// Corpus and scraper admission occupy independent scopes.
	caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "hand-request"}
	full, err := f.eng.Ingest(t.Context(), caller, "")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	p, err := f.eng.Ingest(t.Context(), caller, "fmcsa")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	if p.Kind != store.KindIngest || p.Origin != store.OriginIngest || p.Trigger != store.TriggerIngest || p.Scraper != "fmcsa" || p.UserID != caller.UserID || p.UserEmail != caller.Email || p.RequestID != caller.RequestID || p.Status != store.StatusRunning || p.Delivery != "" {
		t.Fatal(p)
	}
	duplicate, err := f.eng.Ingest(t.Context(), caller, "fmcsa")
	assertRefusal(t, duplicate, err, passes.RefusalBusy)
	duplicate, err = f.eng.Ingest(t.Context(), caller, "")
	assertRefusal(t, duplicate, err, passes.RefusalBusy)
	once.Do(func() { close(release) })
	if end := f.ended(p.ID); end.Status != store.StatusDone || end.Judged != 0 || end.Rows != 1 {
		t.Fatal(end)
	}
	if end := f.ended(full.ID); end.Status != store.StatusDone || end.Judged != 1 {
		t.Fatal(end)
	}
	for _, r := range f.requests() {
		if r.user != caller.UserID || r.email != caller.Email || r.id != caller.RequestID {
			t.Fatal(r)
		}
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		for _, v := range f.list() {
			if v.Trigger == p.ID {
				if v.Kind != store.KindEnrich || v.Origin != store.OriginPass {
					t.Fatal(v)
				}
				return
			}
		}
		select {
		case <-f.changed:
		case <-deadline.C:
			t.Fatal("no scraper follow-on")
		}
	}
}

// R-SGG7-YQ5M R-3NZ7-1SB1 R-3SUS-KV9T
func TestIngestInvalidPrecedesUnavailable(t *testing.T) {
	f := newFixture(t, nil, false)
	for _, scraper := range []string{"FMCSA", "nope"} {
		p, err := f.eng.Ingest(t.Context(), identity.Caller{}, scraper)
		assertRefusal(t, p, err, passes.RefusalInvalid)
	}
	p, err := f.eng.Ingest(t.Context(), identity.Caller{}, "fmcsa")
	assertRefusal(t, p, err, passes.RefusalUnavailable)
	if len(f.list()) != 0 || len(f.requests()) != 0 {
		t.Fatal("refusal side effects")
	}
}

// R-4066-VHPZ R-42LZ-N17D R-1D7V-5WLO R-0ZSY-YFG1
func TestHandJudgeWithoutScrapes(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ids := seedTarget(t, f, 2)
	caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "judge-request"}
	p, err := f.eng.Judge(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != store.KindJudge || p.Origin != store.OriginJudge || p.Trigger != store.TriggerJudgeTool || p.Scraper != "" || p.Status != store.StatusRunning || p.UserID != caller.UserID || p.UserEmail != caller.Email || p.RequestID != caller.RequestID || p.Delivery != "" {
		t.Fatal(p)
	}
	end := f.ended(p.ID)
	if end.Status != store.StatusDone || end.Judged != 2 || len(f.requests()) != 0 {
		t.Fatal(end, f.requests())
	}
	for _, id := range ids {
		j, err := f.st.GetJudgments(t.Context(), id)
		if err != nil || j.Calculated.Before(p.Started) || j.Calculated.After(end.Finished) {
			t.Fatal(j, err)
		}
	}
	f.eng.Drain(context.Background())
	if ps := f.list(); len(ps) != 1 {
		t.Fatal(ps)
	}
	q, err := f.eng.Judge(t.Context(), caller)
	assertRefusal(t, q, err, passes.RefusalStopping)
}

// R-4066-VHPZ R-DTXE-DYY4 R-3SUS-KV9T
func TestHandJudgeBusyAndStoreFailure(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	if _, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge, Delivery: "pending"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			p, err := f.eng.Judge(t.Context(), identity.Caller{UserID: "owner"})
			assertRefusal(t, p, err, passes.RefusalBusy)
		})
	}
	wg.Wait()
	before := f.list()
	f.d.SetFailing(true)
	p, err := f.eng.Judge(t.Context(), identity.Caller{UserID: "owner"})
	f.d.SetFailing(false)
	var rf *passes.Refusal
	if !reflect.DeepEqual(p, store.Pass{}) || !errors.Is(err, store.ErrUnreachable) || errors.As(err, &rf) || !reflect.DeepEqual(before, f.list()) {
		t.Fatal(err)
	}
	f.flush()
	if len(f.requests()) != 0 || len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 {
		t.Fatal("failed judge had side effects")
	}
}

// R-V6ZM-A3XT R-UJTJ-0GUM
func TestCampaignProspectListPrecedenceAndLegacyMapping(t *testing.T) {
	for i, tc := range []struct {
		attrs events.Attrs
		want  string
	}{
		{events.Attrs{"prospect_list": "pl_0123456789abcdef", "cohort": "coh_fedcba9876543210"}, "pl_0123456789abcdef"},
		{events.Attrs{"prospect_list": "", "cohort": "coh_fedcba9876543210"}, ""},
		{events.Attrs{"cohort": "coh_0123456789abcdef"}, "pl_0123456789abcdef"},
		{events.Attrs{"prospect_list": 7, "cohort": "coh_0123456789abcdef"}, "pl_0123456789abcdef"},
		{events.Attrs{"cohort": ""}, ""}, {events.Attrs{}, ""},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newFixture(t, nil, false)
			id := campaignCandidate(t, f, "business")
			tc.attrs["candidate"] = id
			tc.attrs["campaign"] = "campaign"
			d := events.Delivery{Event: events.Event{Name: trail.AcceptedAdded, Time: time.Unix(10, 0), Attrs: tc.attrs}}
			if out := f.eng.DeliverCampaign(t.Context(), d); out != events.OK() {
				t.Fatal(out)
			}
			uses := campaignUses(t, f, id)
			if len(uses) != 1 || uses[0].ProspectList != tc.want {
				t.Fatal(uses)
			}
		})
	}
}

// R-SOKW-PW7E R-SS8L-V7FH R-8WDK-MWRK
func TestHandRecordCarriesScraperAndJudgeCaller(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{store.OriginIngest, store.OriginJudge, store.OriginEnrich, store.OriginRun, store.OriginCron, store.OriginPass} {
		p := store.Pass{Origin: origin, Target: "target", EnrichKind: "kind", Scraper: "fmcsa", UserID: "owner", UserEmail: "owner@example.test"}
		r := passes.RecordOf(p)
		if r.Scraper != p.Scraper {
			t.Fatal(r)
		}
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"target":"target","enrich_kind":"kind","scraper":"fmcsa","status":`) {
			t.Fatal(string(raw))
		}
		if string(fields["scraper"]) != `"fmcsa"` {
			t.Fatal(string(raw))
		}

		hand := origin == store.OriginIngest || origin == store.OriginJudge || origin == store.OriginEnrich
		if hand && (r.User != p.UserID || r.Email != p.UserEmail) || !hand && (r.User != "" || r.Email != "") {
			t.Fatal(r)
		}
	}
	// The targeted entry point admits the new list namespace.
	f := newFixture(t, nil, false)
	ids := seedTarget(t, f, 1)
	p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts, ids[0], false)
	if err != nil || p.Kind != store.KindEnrichTarget {
		t.Fatal(p, err)
	}
	f.ended(p.ID)
}

// R-42LZ-N17D R-4066-VHPZ R-DTXE-DYY4
func TestConcurrentHandJudgeStartsReturnBeforeWorkEnds(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var calls atomic.Int64
	f.st = store.New(f.d, store.Config{Now: func() time.Time {
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		return time.Unix(100000, 0)
	}, Rand: &random{}})
	f.eng = passes.New(passes.Config{Store: f.st, Telemetry: f.w, Events: f.em})
	type result struct {
		pass store.Pass
		err  error
	}
	const n = 16
	results := make(chan result, n)
	start := make(chan struct{})
	var ready, finished sync.WaitGroup
	ready.Add(n)
	for range n {
		finished.Go(func() {
			ready.Done()
			<-start
			p, err := f.eng.Judge(t.Context(), identity.Caller{UserID: "owner"})
			results <- result{p, err}
		})
	}
	ready.Wait()
	close(start)
	waitSignal(t, entered)
	admitted := make(chan struct{})
	go func() { finished.Wait(); close(admitted) }()
	waitSignal(t, admitted)
	close(results)
	var winner store.Pass
	successes := 0
	for r := range results {
		if r.err != nil {
			assertRefusal(t, r.pass, r.err, passes.RefusalBusy)
			continue
		}
		successes++
		winner = r.pass
	}
	if successes != 1 || winner.Status != store.StatusRunning {
		t.Fatal(successes, winner)
	}
	saved, err := f.st.GetPass(t.Context(), winner.ID)
	if err != nil || saved.Status != store.StatusRunning || len(f.list()) != 1 {
		t.Fatal(saved, err)
	}
	once.Do(func() { close(release) })
	if end := f.ended(winner.ID); end.Status != store.StatusDone {
		t.Fatal(end)
	}
}

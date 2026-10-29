package passes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

type parkState struct {
	Prior        []parks.Prior
	Parks        []store.Park
	Parcels      [][]store.ParkParcel
	Counties     []string
	Run          string
	Active, Gone int64
}

func parkReadSnapshot(t *testing.T, st *store.Store) parkState {
	t.Helper()
	ctx := context.Background()
	var s parkState
	var err error
	s.Prior, err = st.PriorParks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range s.Prior {
		park, e := st.GetPark(ctx, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		parcels, e := st.ParkParcels(ctx, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		s.Parks = append(s.Parks, park)
		s.Parcels = append(s.Parcels, parcels)
	}
	s.Counties, err = st.ParkCountiesRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Run, err = st.ParksRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Active, s.Gone, err = st.CountParks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// R-DYSZ-X1WW
func TestParksDedupeIgnoresPassKindOriginAndStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	specs := []store.Pass{
		{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "parcel_run"},
		{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"},
		{Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge, Delivery: "parcel_event"},
	}
	for i, spec := range specs {
		for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
			t.Run(fmt.Sprintf("%d/%s", i, status), func(t *testing.T) {
				f := newFixture(t, nil, true)
				p, err := f.st.AddPass(ctx, spec)
				if err != nil {
					t.Fatal(err)
				}
				switch status {
				case store.StatusDone:
					_, err = f.st.FinishPass(ctx, p.ID, store.Counts{})
				case store.StatusFailed:
					_, err = f.st.FailPass(ctx, p.ID, store.ReasonInterrupted)
				}
				if err != nil {
					t.Fatal(err)
				}
				before := f.list()
				d := events.Delivery{Event: events.Event{ID: "parcel_event", Name: trail.Accepted, Attrs: events.Attrs{"scraper": parks.Scraper, "run": "parcel_run", "status": store.StatusDone}}}
				if spec.Trigger == store.TriggerIngest {
					d.Event.Attrs["run"] = store.TriggerIngest
				}
				if got := f.eng.Deliver(ctx, d); got != events.Skip() {
					t.Fatal(got)
				}
				f.flush()
				if !reflect.DeepEqual(before, f.list()) || len(f.requests()) != 0 || len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
					t.Fatal("dedupe had effects")
				}
			})
		}
	}
	t.Run("unreadable", func(t *testing.T) {
		f := newFixture(t, nil, true)
		f.d.SetFailing(true)
		if got := parcelDelivery(f, "parcel_event"); got != events.Fail(store.Unreachable) {
			t.Fatal(got)
		}
		f.d.SetFailing(false)
		f.flush()
		if len(f.list()) != 0 || len(f.requests()) != 0 || len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
			t.Fatal("failed lookup had effects")
		}
	})
}

// R-TJ2E-A5QR R-E4WH-TWMD
func TestParksLaneIndependentOfAllCandidateLanes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, operation := range []string{"delivery", "rules"} {
		t.Run(operation, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/record") || strings.HasSuffix(r.URL.Path, "/current") {
					parcelRecord(w, 0, store.StatusDone)
					return
				}
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}, true)
			for _, p := range []store.Pass{
				{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"},
				{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "held_candidate_run", Delivery: "candidate_delivery"},
				{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "owner"},
				{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "owner"},
			} {
				if _, err := f.st.AddPass(ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			ids := seedTarget(t, f, 1)
			if _, err := f.st.AddPass(ctx, store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Target: ids[0], EnrichKind: enrichers.KindLinkFacts, UserID: "owner"}); err != nil {
				t.Fatal(err)
			}
			var p store.Pass
			if operation == "delivery" {
				if got := parcelDelivery(f, "parcel_event"); got != events.OK() {
					t.Fatal(got)
				}
				for _, x := range f.list() {
					if x.Kind == store.KindParks {
						p = x
					}
				}
			} else {
				var err error
				p, err = f.eng.EditParkRules(ctx, identity.Caller{UserID: "owner"}, store.ParkRulesEdit{})
				if err != nil {
					t.Fatal(err)
				}
			}
			waitSignal(t, entered)
			running, err := f.st.RunningPasses(ctx)
			if err != nil || len(running) != 6 {
				t.Fatal(running, err)
			}
			close(release)
			if p = f.ended(p.ID); p.Status != store.StatusDone {
				t.Fatal(p)
			}
		})
	}
	for _, origin := range []string{store.OriginStart, store.OriginRules, store.OriginRun} {
		t.Run("busy/"+origin, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) { parcelRecord(w, 0, store.StatusDone) }, true)
			p := store.Pass{Kind: store.KindParks, Origin: origin, Trigger: "other_run"}
			if origin == store.OriginRun {
				p.Delivery = "held_delivery"
			}
			if origin == store.OriginRules {
				p.Trigger = store.TriggerParkRules
				p.UserID = "other_owner"
			}
			if _, err := f.st.AddPass(ctx, p); err != nil {
				t.Fatal(err)
			}
			before := f.list()
			rules, cs, err := f.st.ParkRules(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got := parcelDelivery(f, "parcel_event"); got != events.Fail(passes.Busy) {
				t.Fatal(got)
			}
			refused, err := f.eng.EditParkRules(ctx, identity.Caller{UserID: "owner"}, store.ParkRulesEdit{})
			assertRefusal(t, refused, err, passes.RefusalBusy)
			after, ac, err := f.st.ParkRules(ctx)
			if err != nil || !reflect.DeepEqual(rules, after) || !reflect.DeepEqual(cs, ac) || !reflect.DeepEqual(before, f.list()) {
				t.Fatal("busy changed state", err)
			}
			f.flush()
			if len(f.ec.Events()) != 0 || len(f.requests()) != 1 {
				t.Fatal(f.ec.Events(), f.requests())
			}
		})
	}
}

// R-DTXE-DYY4
func TestConcurrentParkRuleEditsAndMixedEngineMethods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	entered := make(chan struct{}, 20)
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/current") {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		parcelRecord(w, 0, store.StatusDone)
	}, true)
	ids := seedTarget(t, f, 1)
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.eng.EditParkRules(ctx, identity.Caller{UserID: "owner"}, store.ParkRulesEdit{})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			var refusal *passes.Refusal
			if !errors.As(err, &refusal) || refusal.Word != passes.RefusalBusy {
				t.Fatal(err)
			}
		}
	}
	if successes != 1 {
		t.Fatal(successes)
	}
	waitSignal(t, entered)
	mixed := []func(){
		func() { _, _ = f.eng.Ingest(ctx, identity.Caller{UserID: "owner"}, "fmcsa") },
		func() { _, _ = f.eng.Judge(ctx, identity.Caller{UserID: "owner"}) },
		func() { _, _ = f.eng.Enrich(ctx, identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts) },
		func() {
			_, _ = f.eng.EnrichTarget(ctx, identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts, ids[0], false)
		},
		func() { _, _ = f.eng.EditParkRules(ctx, identity.Caller{UserID: "owner"}, store.ParkRulesEdit{}) },
		func() { _, _ = f.eng.StartParks(ctx, "concurrent_start") },
		func() { _ = parcelDelivery(f, "mixed_event") },
		func() { _ = f.eng.Recover(ctx) },
		func() { f.eng.Drain(ctx) },
	}
	for _, fn := range mixed {
		wg.Add(1)
		go func(fn func()) { defer wg.Done(); fn() }(fn)
	}
	wg.Wait()
	for _, p := range f.list() {
		if p.Status == store.StatusRunning {
			t.Fatal(p)
		}
	}
}

// R-3BZB-PF2P R-KQVS-KMMF
func TestParksSnapshotsKeysAndPreservesCandidateState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			parcelRecord(w, 3, store.StatusDone)
			return
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		for i := range 3 {
			parcel(w, i, "Candidate County", "unmatched")
		}
	}, true)
	// The only industrial rule here is an existing candidate's address key.
	_, ids, err := f.st.Attach(ctx, "candidate_seed", false, []store.Observation{{Scraper: "fmcsa", SourceID: "known", Digest: "known", Reading: sources.Reading{Name: "Existing Business", NameKey: "existingbusiness", Street: "0 Industrial Street", Zip: "55072", Email: "owner@existing.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.DeriveAmong(ctx, enrichers.KindLinkFacts, ids, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.Judge(ctx, ids); err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.AddPage(ctx, store.Page{Candidate: ids[0], URL: "https://existing.test/", Title: "fixture", Text: "kept bytes", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err = f.st.AddCampaignUse(ctx, ids[0], "campaign", "", time.Unix(100000, 0)); err != nil {
		t.Fatal(err)
	}
	list, err := f.st.CreateProspectList(ctx, store.ProspectList{Name: "Saved owner search", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.st.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[0], Why: "fixture ruling", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.st.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: ids[0], Why: "fixture inclusion", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: list.ID, Candidate: ids[0], Why: "fixture reject", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.OpenProspectList(ctx, list.ID, "owner", ""); err != nil {
		t.Fatal(err)
	}
	initialCandidates := candidateReadSnapshot(t, f.st)
	if got := parcelDelivery(f, "candidate_parcels"); got != events.OK() {
		t.Fatal(got)
	}
	waitSignal(t, entered)
	if !reflect.DeepEqual(initialCandidates, candidateReadSnapshot(t, f.st)) {
		t.Fatal("recording parks changed candidates")
	}
	// A later candidate would classify two more parcels if keys were read during work.
	observations := []store.Observation{}
	for i := 1; i < 3; i++ {
		observations = append(observations, store.Observation{Scraper: "fmcsa", SourceID: fmt.Sprint(i), Digest: fmt.Sprint(i), Reading: sources.Reading{Name: fmt.Sprintf("Late Business %d", i), NameKey: fmt.Sprintf("late%d", i), Street: fmt.Sprintf("%d Industrial Street", i), Zip: "55072"}})
	}
	if _, _, err = f.st.Attach(ctx, "late_seed", false, observations); err != nil {
		t.Fatal(err)
	}
	before := candidateReadSnapshot(t, f.st)
	close(release)
	p := f.ended(f.list()[0].ID)
	if p.Status != store.StatusDone || p.ByCandidate != 1 || p.ByLabel != 0 || p.ByValue != 0 || p.ParksCreated != 0 {
		t.Fatal(p)
	}
	after := candidateReadSnapshot(t, f.st)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("candidate state changed\nbefore %s\nafter %s", before, after)
	}
	if len(f.list()) != 1 {
		t.Fatal(f.list())
	}
}
func candidateReadSnapshot(t *testing.T, st *store.Store) []byte {
	t.Helper()
	ctx := context.Background()
	candidates, err := st.ListCandidates(ctx, store.CandidateFilter{}, "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	all := []any{candidates}
	keep := func(v any, err error) {
		t.Helper()
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		all = append(all, v)
	}
	for _, c := range candidates {
		keep(st.GetCandidate(ctx, c.ID))
		keep(st.Links(ctx, c.ID))
		keep(st.Estimates(ctx, c.ID))
		keep(st.GetJudgments(ctx, c.ID))
		keep(st.Rulings(ctx, c.ID))
		keep(st.Enrichments(ctx, c.ID))
		keep(st.GetFacts(ctx, c.ID))
		keep(st.Pages(ctx, c.ID))
		keep(st.Rejects(ctx, c.ID))
		keep(st.CampaignUses(ctx, c.ID))
		ps, e := st.Pages(ctx, c.ID)
		if e != nil {
			t.Fatal(e)
		}
		for _, p := range ps {
			keep(st.GetPage(ctx, c.ID, p.Number))
		}
	}
	keep(st.ListProspectLists(ctx, "", "", 20))
	lists, err := st.ListProspectLists(ctx, "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range lists {
		keep(st.GetProspectList(ctx, list.ID))
		keep(st.Opens(ctx, list.ID))
	}
	b, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// R-TQ38-2EBU R-3D78-36TE R-YDHH-CBMA
func TestParksComposesClassificationClustersAndMixedPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var rows []map[string]any
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			parcelRecord(w, len(rows), store.StatusDone)
			return
		}
		for _, m := range rows {
			_ = json.NewEncoder(w).Encode(m)
		}
	}, true)
	expected := newFixture(t, nil, false)
	county, label := seedLabel(t, f)
	seed := func(g *fixture) {
		_, _, err := g.st.Attach(ctx, "business_seed", false, []store.Observation{{Scraper: "fmcsa", SourceID: "existing_business", Digest: "known", Reading: sources.Reading{Name: "Candidate address", NameKey: "candidateaddress", Street: "3 Industrial Street", Zip: "55072"}}})
		if err != nil {
			t.Fatal(err)
		}
		prior := []parks.Parcel{}
		for i := range 6 {
			lat, lon := 45+float64(i/3)+float64(i%3)*0.0001, -93.0
			value := int64(parks.SeedValueThreshold + 1)
			prior = append(prior, parks.Parcel{PIN: fmt.Sprintf("prior-%d", i), County: "Fallback County", City: "Fixture Town", Zip: "55072", Street: fmt.Sprintf("%d Industrial Street", i), Lat: &lat, Lon: &lon, BuildingValue: &value, Rule: parks.RuleValue})
		}
		rules, _, err := g.st.ParkRules(ctx)
		if err != nil {
			t.Fatal(err)
		}
		p, err := g.st.AddPass(ctx, store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "old_parcels"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = g.st.ApplyParks(ctx, p.ID, "old_parcels", []string{"Fallback County"}, parks.Match(parks.Clusters(prior, rules), nil), store.Counts{ParcelsRead: 6, ByValue: 6, ParksCreated: 2})
		if err != nil {
			t.Fatal(err)
		}
	}
	seed(f)
	seed(expected)
	for i := range 4 {
		m := parcelObject(i, "Fallback County", "unmatched")
		m["lat"] = 45 + float64(i)*0.0001
		pin := fmt.Sprintf("prior-%d", i)
		if i == 3 {
			pin = "candidate-new"
		}
		m["source_id"] = pin
		facts := m["facts"].(map[string]any)
		facts["state_pin"] = pin
		if i < 3 {
			facts["emv_bldg"] = int64(parks.SeedValueThreshold + 1)
			facts["homestead"] = "No"
		}
		rows = append(rows, m)
	}
	for i := range 3 {
		m := parcelObject(10+i, county, label)
		m["lat"] = 47 + float64(i)*0.0001
		rows = append(rows, m)
	}
	noPoint := parcelObject(20, county, label)
	delete(noPoint, "lat")
	delete(noPoint, "lon")
	rows = append(rows, noPoint)
	rows = append(rows, parcelObject(21, "Nonindustrial County", "unmatched"))
	rules, _, err := expected.st.ParkRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := expected.st.PriorParks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{parks.AddressKey("3 Industrial Street", "55072"): true}
	kept := []parks.Parcel{}
	for _, m := range rows {
		b, e := json.Marshal(m)
		if e != nil {
			t.Fatal(e)
		}
		var row scrapes.Row
		if e = json.Unmarshal(b, &row); e != nil {
			t.Fatal(e)
		}
		row.SourceID = m["source_id"].(string)
		p := parks.ParcelOf(row)
		p.Rule = parks.Classify(p, rules, keys)
		if p.Rule != "" {
			kept = append(kept, p)
		}
	}
	plan := parks.Match(parks.Clusters(kept, rules), prior)
	if len(plan.News) != 1 || len(plan.Updates) != 1 || len(plan.Gone) != 1 || plan.Grown != 1 {
		t.Fatal(plan)
	}
	cs := []string{county, "Fallback County", "Nonindustrial County"}
	sort.Strings(cs)
	p, err := expected.st.AddPass(ctx, store.Pass{Kind: store.KindParks, Origin: store.OriginRun, Trigger: "parcel_run", Delivery: "mixed_parcels"})
	if err != nil {
		t.Fatal(err)
	}
	counts := store.Counts{ParcelsRead: 9, ByLabel: 4, ByValue: 3, ByCandidate: 1, ParksCreated: 1, ParksUpdated: 1, ParksGone: 1, ParksGrown: 1}
	_, err = expected.st.ApplyParks(ctx, p.ID, "parcel_run", cs, plan, counts)
	if err != nil {
		t.Fatal(err)
	}
	if got := parcelDelivery(f, "mixed_parcels"); got != events.OK() {
		t.Fatal(got)
	}
	var actual store.Pass
	for _, p := range f.list() {
		if p.Trigger == "parcel_run" {
			actual = f.ended(p.ID)
		}
	}
	if actual.Status != store.StatusDone || actual.ParcelRun != "parcel_run" || actual.ParcelsRead != 9 || actual.ByLabel != 4 || actual.ByValue != 3 || actual.ByCandidate != 1 || actual.ParksCreated != 1 || actual.ParksUpdated != 1 || actual.ParksGone != 1 || actual.ParksGrown != 1 {
		t.Fatal(actual)
	}
	assertParkZeroOtherCounts(t, actual)
	if want, got := parkReadSnapshot(t, expected.st), parkReadSnapshot(t, f.st); !reflect.DeepEqual(want, got) {
		t.Fatal(want, got)
	}
}

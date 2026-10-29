package store_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func baseWalkPass() store.Pass {
	return store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "owner", UserEmail: "owner@test", RequestID: "walk-request"}
}

func baseSweep(t *testing.T, f *contractFixture, park string, found ...places.Place) (store.Pass, store.ParkWalk) {
	t.Helper()
	ctx := context.Background()
	p, err := f.s.AddPass(ctx, baseWalkPass())
	contractOK(t, err)
	xs := []places.Found{}
	for _, x := range found {
		xs = append(xs, places.Found{Place: x, Circle: places.Circle{Lat: 45, Lon: -93, Radius: 50}})
	}
	w, err := f.s.RecordSweep(ctx, store.Sweep{Pass: p.ID, Park: park, Found: xs, Circles: 2, Requests: 3, Cost: 2 * places.PriceNanos, CeilingReached: true})
	contractOK(t, err)
	return p, w
}

// R-F1DI-OTWO R-FA1W-3DCK R-F694-7WVG
func TestPlacesLinksKeepRecordDisplayAndNeverBecomeGone(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	k := seedParks(t, f)[0]
	before, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	f.now = f.now.Add(time.Hour)
	x := places.Place{ID: "matched-place", Name: "Places alternate name", Address: "78 Different Road, Other, MN 55072, USA", Website: "https://business.test/other", Phone: "3205550199"}
	p, _ := baseSweep(t, f, k.ID, x, places.Place{ID: "sole-place", Name: "Places only business", Address: "1 Solo Street, Solo, MN 55092, USA", Phone: "3205550123"})
	got, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	contractEqual(t, got.Name, x.Name)
	contractEqual(t, got.NameSource, places.Name)
	contractEqual(t, got.Names, append([]string{x.Name}, before.Names...))
	contractEqual(t, got.Email, before.Email)
	contractEqual(t, got.Updated, f.now.UTC().Truncate(time.Second))
	links, err := f.s.Links(ctx, id)
	contractOK(t, err)
	var placeLink store.Link
	for _, l := range links {
		if l.Scraper == places.Name {
			placeLink = l
		} else {
			contractEqual(t, []string{l.Walk, l.Park}, []string{"", ""})
		}
	}
	contractEqual(t, placeLink.SourceID, x.ID)
	contractEqual(t, []string{placeLink.Walk, placeLink.Park, placeLink.Run, placeLink.Digest}, []string{p.ID, k.ID, "", ""})
	contractEqual(t, placeLink.Reading, places.Reading(x))
	shape := struct {
		Candidate, Scraper, SourceID, Run, Digest string
		Reading                                   sources.Reading
		Reason                                    string
		Confidence                                float64
		FirstSeen, LastSeen                       time.Time
		Gone                                      bool
		Walk, Park                                string
	}(placeLink)
	contractEqual(t, store.Link(shape), placeLink)
	ps, err := f.s.ParkPlaces(ctx, k.ID)
	contractOK(t, err)
	contractEqual(t, len(ps), 2)
	soleFound := false
	soleID := ""
	for _, pl := range ps {
		if pl.PlaceID != "sole-place" {
			continue
		}
		soleFound = true
		soleID = pl.Candidate
		c, err := f.s.GetCandidate(ctx, pl.Candidate)
		contractOK(t, err)
		contractEqual(t, c.Name, pl.Name)
		contractEqual(t, c.Names, []string{pl.Name})
		contractEqual(t, c.Phone, pl.Phone)
		contractEqual(t, c.Street, "1 Solo Street")
	}
	contractEqual(t, soleFound, true)
	// Public records supply email and tags while the place still supplies the name and location.
	f.now = f.now.Add(time.Hour)
	o := contractObservation("government-row", "PLACES ONLY BUSINESS LLC", f.now)
	o.Reading.Zip = "55092"
	o.Reading.Street = "54 Government Road"
	ids := contractAttach(t, f, "government-run", o)
	contractEqual(t, ids, []string{soleID})
	c, err := f.s.GetCandidate(ctx, soleID)
	contractOK(t, err)
	contractEqual(t, c.Name, "Places only business")
	contractEqual(t, c.Names, append([]string{"Places only business"}, o.Reading.Names...))
	contractEqual(t, c.Street, "1 Solo Street")
	contractEqual(t, c.Phone, "3205550123")
	contractEqual(t, c.Updated, f.now.UTC().Truncate(time.Second))
	// A later record update cannot take missing fields from the Places link.
	f.now = f.now.Add(time.Hour)
	o.Digest = "updated-government-digest"
	o.Reading.Phone, o.Reading.Email, o.Reading.Class = "", "", ""
	o.Reading.People, o.Reading.Cargo = nil, nil
	contractEqual(t, contractAttach(t, f, "later-government-run", o), ids)
	c, err = f.s.GetCandidate(ctx, soleID)
	contractOK(t, err)
	contractEqual(t, c.Name, "Places only business")
	contractEqual(t, c.Names, append([]string{"Places only business"}, o.Reading.Names...))
	contractEqual(t, c.Phone, "3205550123")
	contractEqual(t, c.Email, "")
	contractEqual(t, c.Class, "")
	contractEqual(t, c.People, []sources.Sourced[sources.Person]{})
	contractEqual(t, c.Cargo, []string{})
	for _, args := range [][2]string{{places.Name, "any-run"}, {"", "any-run"}, {"fmcsa", ""}} {
		cursor, c, err := f.s.MarkGone(ctx, args[0], args[1], "", 100)
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, cursor, "")
		contractEqual(t, c, store.Counts{})
		current, err := f.s.Links(ctx, id)
		contractOK(t, err)
		contractEqual(t, current, links)
	}
}

// R-W9Z0-IFGV R-W3VI-LKRE R-ONLR-C520
func TestGetParkReadsNewestWalkIncludingCeiling(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	k := seedParks(t, f)[0]
	contractEqual(t, k.NewCandidates, int64(0))
	contractEqual(t, k.CeilingReached, false)
	for i := range 2 {
		f.now = f.now.Add(time.Hour)
		p, w := baseSweep(t, f, k.ID, places.Place{ID: "same-place", Name: "Walk business"})
		_, err := f.s.FinishWalk(ctx, p.ID, store.Counts{ParksWalked: 1, Found: w.Found, NewCandidates: w.NewCandidates, Requests: w.Requests, Circles: w.Circles, Cost: w.Cost}, true)
		contractOK(t, err)
		got, err := f.s.GetPark(ctx, k.ID)
		contractOK(t, err)
		want := k
		want.Walked, want.WalkRun, want.Found, want.NewCandidates, want.CeilingReached = w.Walked, w.Pass, w.Found, w.NewCandidates, w.CeilingReached
		contractEqual(t, got, want)
		contractEqual(t, got.NewCandidates, int64(1-i))
		f.reopen()
		got, err = f.s.GetPark(ctx, k.ID)
		contractOK(t, err)
		contractEqual(t, got, want)
		walked, err := f.s.ListParks(ctx, store.ParkFilter{Walked: parks.WalkedWalked}, "", "", "", 100)
		contractOK(t, err)
		contractEqual(t, walked, []store.Park{want})
	}
	// Derivation writes the park and its parcels, retaining its walk history.
	want, err := f.s.GetPark(ctx, k.ID)
	contractOK(t, err)
	applyParkPlan(t, f, parks.Plan{Gone: []string{k.ID}})
	got, err := f.s.GetPark(ctx, k.ID)
	contractOK(t, err)
	want.State = parks.StateGone
	want.Updated = f.now.UTC().Truncate(time.Second)
	contractEqual(t, got, want)
}

// R-W07T-G9JB R-OR9G-HGA3 R-64U1-LEO8 R-OQ1K-3OJE
func TestWalkCeilingRuleEditsAreAtomicAndPersistent(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	seedParks(t, f)
	before := parkState(t, f)
	ceiling, err := f.s.WalkCeiling(ctx)
	contractOK(t, err)
	contractEqual(t, ceiling, int64(places.SeedCeiling))
	p, err := f.s.SetParkRules(ctx, store.ParkRulesEdit{WalkCeiling: i64(250)}, parkPass(store.OriginRules))
	contractOK(t, err)
	contractEqual(t, parkState(t, f), before)
	f.reopen()
	ceiling, err = f.s.WalkCeiling(ctx)
	contractOK(t, err)
	contractEqual(t, ceiling, int64(250))
	for _, n := range []int64{0, 5001} {
		out, err := f.s.SetParkRules(ctx, store.ParkRulesEdit{WalkCeiling: i64(n)}, parkPass(store.OriginRules))
		contractError(t, err, store.ErrInvalid)
		contractEqual(t, out, store.Pass{})
	}
	out, err := f.s.SetParkRules(ctx, store.ParkRulesEdit{WalkCeiling: i64(300), Distance: i64(800)}, parkPass(store.OriginRules))
	contractError(t, err, store.ErrBusy)
	contractEqual(t, out, store.Pass{})
	contractEqual(t, parkState(t, f), before)
	ceiling, err = f.s.WalkCeiling(ctx)
	contractOK(t, err)
	contractEqual(t, ceiling, int64(250))
	_, err = f.s.FailPass(ctx, p.ID, store.ReasonStore)
	contractOK(t, err)
	p, err = f.s.SetParkRules(ctx, store.ParkRulesEdit{Distance: i64(800)}, parkPass(store.OriginRules))
	contractOK(t, err)
	ceiling, err = f.s.WalkCeiling(ctx)
	contractOK(t, err)
	contractEqual(t, ceiling, int64(250))
	contractEqual(t, p.Kind, store.KindParks)
}

// R-VROI-RVCG R-VU4B-JETU R-VWK4-AYB8
func TestGenericPassEndingsPreserveWalkAndParkRecords(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, err := f.s.AddPage(ctx, keptPage(id, "https://kept.test/page"))
	contractOK(t, err)
	k := seedParks(t, f)[0]
	p, w := baseSweep(t, f, k.ID, places.Place{ID: "kept-place", Name: "Kept place"})
	_, err = f.s.FinishWalk(ctx, p.ID, store.Counts{ParksWalked: 1, Circles: w.Circles, Requests: w.Requests, Found: w.Found, NewCandidates: w.NewCandidates, Cost: w.Cost}, true)
	contractOK(t, err)
	deriveAll(t, f.s)
	_, _, err = f.s.JudgeAll(ctx, "", 100, f.now)
	contractOK(t, err)
	list, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "Kept list", Size: 100, CreatedUserID: "owner"})
	contractOK(t, err)
	_, err = f.s.Reject(ctx, store.Reject{ProspectList: list.ID, Candidate: id, Kind: store.KindReject, Why: "kept reason", UserID: "owner"})
	contractOK(t, err)
	_, err = f.s.OpenProspectList(ctx, list.ID, "owner", "owner@test")
	contractOK(t, err)
	contractOK(t, f.s.AddCampaignUse(ctx, id, "kept-campaign", list.ID, f.now))
	contractOK(t, f.s.RejectCampaignUse(ctx, id, "kept-campaign", "kept reason", f.now))
	contractOK(t, f.s.DeliverCampaignUse(ctx, id, "kept-campaign", f.now))
	endingPasses := []string{}
	snapshot := func() []any {
		s := contractState(t, f)
		s.passes = slices.DeleteFunc(s.passes, func(p store.Pass) bool { return slices.Contains(endingPasses, p.ID) })
		ps, e := f.s.ParkPlaces(ctx, k.ID)
		contractOK(t, e)
		ws, e := f.s.ParkWalks(ctx, k.ID)
		contractOK(t, e)
		pw, e := f.s.PassWalks(ctx, p.ID)
		contractOK(t, e)
		pages, e := f.s.Pages(ctx, id)
		contractOK(t, e)
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		ceiling, e := f.s.WalkCeiling(ctx)
		contractOK(t, e)
		lists, e := f.s.ListProspectLists(ctx, "", "", 100)
		contractOK(t, e)
		read, e := f.s.ReadProspectList(ctx, list.ID)
		contractOK(t, e)
		rejects, e := f.s.Rejects(ctx, id)
		contractOK(t, e)
		opens, e := f.s.Opens(ctx, list.ID)
		contractOK(t, e)
		uses, e := f.s.CampaignUses(ctx, id)
		contractOK(t, e)
		return []any{s, parkState(t, f), ps, ws, pw, pages, facts, ceiling, lists, read, rejects, opens, uses}
	}
	before := snapshot()
	for _, reason := range []string{"", store.ReasonScrapes, store.ReasonInterrupted, store.ReasonStore, store.ReasonPrompts, store.ReasonPlaces} {
		p, err := f.s.AddPass(ctx, enrichPass())
		contractOK(t, err)
		endingPasses = append(endingPasses, p.ID)
		f.now = f.now.Add(time.Minute)
		want := p
		want.Finished = f.now.UTC().Truncate(time.Second)
		var got store.Pass
		if reason == "" {
			got, err = f.s.FinishPass(ctx, p.ID, store.Counts{Rows: 3, Changed: 1})
			want.Status, want.Rows, want.Changed = store.StatusDone, 3, 1
		} else {
			got, err = f.s.FailPass(ctx, p.ID, reason)
			want.Status, want.Reason = store.StatusFailed, reason
		}
		contractOK(t, err)
		contractEqual(t, got, want)
		contractEqual(t, snapshot(), before)
		f.reopen()
		got, err = f.s.GetPass(ctx, p.ID)
		contractOK(t, err)
		contractEqual(t, got, want)
		contractEqual(t, snapshot(), before)
	}
}

// R-VWK4-AYB8
func TestWalkPassReturnedRecordsKeepStatusInvariants(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	want := []store.Pass{}
	for _, failed := range []bool{false, true} {
		p, err := f.s.AddPass(ctx, baseWalkPass())
		contractOK(t, err)
		contractEqual(t, p.Finished, time.Time{})
		contractEqual(t, p.Reason, "")
		c := store.Counts{ParksWalked: 1, Circles: 2, Requests: 3, Found: 4, NewCandidates: 5, Cost: places.PriceNanos}
		if failed {
			p, err = f.s.FailWalk(ctx, p.ID, store.ReasonPlaces, c)
		} else {
			p, err = f.s.FinishWalk(ctx, p.ID, c, true)
		}
		contractOK(t, err)
		contractEqual(t, p.Finished.Location(), time.UTC)
		contractEqual(t, p.Finished.Nanosecond(), 0)
		contractEqual(t, p.Started.Location(), time.UTC)
		contractEqual(t, p.Started.Nanosecond(), 0)
		contractEqual(t, p.ParksWalked, c.ParksWalked)
		contractEqual(t, p.Circles, c.Circles)
		contractEqual(t, p.Requests, c.Requests)
		contractEqual(t, p.Found, c.Found)
		contractEqual(t, p.NewCandidates, c.NewCandidates)
		contractEqual(t, p.Cost, c.Cost)
		contractEqual(t, []string{p.Target, p.EnrichKind, p.ParcelRun}, []string{"", "", ""})
		contractEqual(t, p.CeilingReached, !failed)
		if failed {
			contractEqual(t, p.Status, store.StatusFailed)
			contractEqual(t, p.Reason, store.ReasonPlaces)
		} else {
			contractEqual(t, p.Status, store.StatusDone)
			contractEqual(t, p.Reason, "")
		}
		got, err := f.s.GetPass(ctx, p.ID)
		contractOK(t, err)
		contractEqual(t, got, p)
		want = append(want, p)
	}
	slices.Reverse(want)
	got, err := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, err)
	contractEqual(t, got, want)
	got, err = f.s.RunningPasses(ctx)
	contractOK(t, err)
	contractEqual(t, got, []store.Pass{})
}

// R-EP8R-Z5D9 R-64U1-LEO8 R-W7J7-QVZH
func TestKnownCountiesFollowLatestDerivationAndStoredLabels(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	applyParkPlan(t, f, parks.Plan{})
	p, err := f.s.AddPass(ctx, parkPass(store.OriginStart))
	contractOK(t, err)
	_, err = f.s.ApplyParks(ctx, p.ID, "read-one", []string{"Chisago", "New County", "New County"}, parks.Plan{}, store.Counts{})
	contractOK(t, err)
	county := "Chisago"
	p, err = f.s.SetParkRules(ctx, store.ParkRulesEdit{County: &county}, parkPass(store.OriginRules))
	contractOK(t, err)
	rules, got, err := f.s.ParkRules(ctx)
	contractOK(t, err)
	contractEqual(t, slices.Contains(got, county), true)
	_, held := rules.Labels[county]
	contractEqual(t, held, false)
	want := []string{"Aitkin", "Anoka", "Chisago", "Isanti", "Mille Lacs", "Morrison", "New County", "Sherburne", "St. Louis", "Stearns"}
	contractEqual(t, got, want)
	_, err = f.s.ApplyParks(ctx, p.ID, "read-two", []string{"New County"}, parks.Plan{}, store.Counts{})
	contractOK(t, err)
	f.reopen()
	_, got, err = f.s.ParkRules(ctx)
	contractOK(t, err)
	want = slices.DeleteFunc(want, func(s string) bool { return s == county })
	contractEqual(t, got, want)
	read, err := f.s.ParkCountiesRead(ctx)
	contractOK(t, err)
	contractEqual(t, read, []string{"New County"})
}

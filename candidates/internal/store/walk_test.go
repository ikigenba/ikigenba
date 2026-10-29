package store_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func walkPass(t *testing.T, f *contractFixture) store.Pass {
	t.Helper()
	p, e := f.s.AddPass(context.Background(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "walker", UserEmail: "walker@example.test", RequestID: "walk-request"})
	contractOK(t, e)
	return p
}
func walkFound(id, name, phone, website string) places.Found {
	return places.Found{Place: places.Place{ID: id, Name: name, Address: "15 Main St, Example, MN 55056, USA", PrimaryType: "manufacturer", Website: website, Phone: phone}, Circle: places.Circle{Lat: 45, Lon: -93, Radius: 120}}
}
func walkSweep(t *testing.T, f *contractFixture, p store.Pass, k store.Park, found ...places.Found) store.ParkWalk {
	t.Helper()
	w, e := f.s.RecordSweep(context.Background(), store.Sweep{Pass: p.ID, Park: k.ID, Found: found, Circles: 1, Requests: 1, Cost: places.PriceNanos})
	contractOK(t, e)
	return w
}

// R-P3GG-B5P1 R-P4OC-OXFQ R-P5W9-2P6F
func TestWalkPublicShapes(t *testing.T) {
	contractEqual(t, []string{store.KindWalk, store.OriginWalk, store.TriggerWalk, store.ReasonPlaces, store.LinkPhone, store.LinkDomain}, []string{"walk", "walk", "walk", "places", "phone", "domain"})
	p := store.Place{Walk: "walk", Park: "park", PlaceID: "place", Name: "name", Address: "address", PrimaryType: "type", Website: "website", Phone: "phone", Lat: 1, Lon: 2, Radius: 3, Candidate: "candidate", New: true, At: time.Time{}}
	contractEqual(t, store.Place(struct {
		Walk, Park, PlaceID, Name, Address, PrimaryType, Website, Phone string
		Lat, Lon, Radius                                                float64
		Candidate                                                       string
		New                                                             bool
		At                                                              time.Time
	}(p)), p)
	w := store.ParkWalk{Pass: "pass", Park: "park", Name: "name", Walked: time.Time{}, Circles: 1, Requests: 2, Found: 3, NewCandidates: 4, Cost: 5, CeilingReached: true}
	contractEqual(t, store.ParkWalk(struct {
		Pass, Park, Name                              string
		Walked                                        time.Time
		Circles, Requests, Found, NewCandidates, Cost int64
		CeilingReached                                bool
	}(w)), w)
	c := store.FoundCandidate{ID: "id", Name: "name", Status: store.StatusActive, New: true}
	contractEqual(t, store.FoundCandidate(struct {
		ID, Name, Status string
		New              bool
	}(c)), c)
	s := store.Sweep{Pass: "pass", Park: "park", Found: []places.Found{walkFound("one", "name", "", "")}, Circles: 1, Requests: 2, Cost: 3, CeilingReached: true}
	contractEqual(t, store.Sweep(struct {
		Pass, Park              string
		Found                   []places.Found
		Circles, Requests, Cost int64
		CeilingReached          bool
	}(s)), s)
	var _ interface {
		RecordSweep(context.Context, store.Sweep) (store.ParkWalk, error)
		FinishWalk(context.Context, string, store.Counts, bool) (store.Pass, error)
		FailWalk(context.Context, string, string, store.Counts) (store.Pass, error)
		PassWalks(context.Context, string) ([]store.ParkWalk, error)
		ParkWalks(context.Context, string) ([]store.ParkWalk, error)
		ParkPlaces(context.Context, string) ([]store.Place, error)
		ParkCandidates(context.Context, string) ([]store.FoundCandidate, error)
		CandidatePark(context.Context, string) (string, error)
		WalkCeiling(context.Context) (int64, error)
	} = (*store.Store)(nil)
}

// R-UWXV-8JOI R-UY5R-MBF7 R-UZDO-035W R-V1TG-RMNA R-V31D-5EDZ R-0YMT-HFK2 R-0ZUP-V7AR R-VIOF-L0A2 R-OSHC-V80S
func TestWalkRecordingAndReadOrderPersist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	ks := seedParks(t, f)
	p := walkPass(t, f)
	at := f.now.UTC().Truncate(time.Second)
	first := walkFound("z-first", "Zeta", "3205550100", "https://zeta.example")
	second := walkFound("a-second", "Alpha", "3205550100", "")
	w := walkSweep(t, f, p, ks[0], first, second, first)
	contractEqual(t, w, store.ParkWalk{Pass: p.ID, Park: ks[0].ID, Name: ks[0].Name, Walked: at, Circles: 1, Requests: 1, Found: 2, NewCandidates: 1, Cost: places.PriceNanos})
	// The same walk never records a place twice, even under another park.
	other := walkSweep(t, f, p, ks[1], first)
	contractEqual(t, other.Found, int64(0))
	contractEqual(t, other.NewCandidates, int64(0))
	f.reopen()
	ws, e := f.s.PassWalks(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, ws, []store.ParkWalk{w, other})
	xs, e := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, len(xs), 2)
	contractEqual(t, xs[0].PlaceID, second.Place.ID)
	contractEqual(t, xs[1].PlaceID, first.Place.ID)
	id := xs[0].Candidate
	contractEqual(t, xs[1].Candidate, id)
	contractEqual(t, xs[0].New, false)
	contractEqual(t, xs[1].New, true)
	for _, x := range xs {
		fnd := first
		if x.PlaceID == second.Place.ID {
			fnd = second
		}
		contractEqual(t, x, store.Place{Walk: p.ID, Park: ks[0].ID, PlaceID: fnd.Place.ID, Name: fnd.Place.Name, Address: fnd.Place.Address, PrimaryType: fnd.Place.PrimaryType, Website: fnd.Place.Website, Phone: fnd.Place.Phone, Lat: fnd.Circle.Lat, Lon: fnd.Circle.Lon, Radius: fnd.Circle.Radius, Candidate: id, New: x.New, At: at})
	}
	cs, e := f.s.ParkCandidates(ctx, ks[0].ID)
	contractOK(t, e)
	candidate, e := f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, cs, []store.FoundCandidate{{ID: id, Name: candidate.Name, Status: store.StatusActive, New: true}})
	park, e := f.s.CandidatePark(ctx, id)
	contractOK(t, e)
	contractEqual(t, park, ks[0].ID)
	k, e := f.s.GetPark(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, k.Walked, at)
	contractEqual(t, k.WalkRun, p.ID)
	contractEqual(t, k.Found, w.Found)
	contractEqual(t, k.NewCandidates, w.NewCandidates)
	contractEqual(t, k.CeilingReached, false)
	parcels, e := f.s.ParkParcels(ctx, k.ID)
	contractOK(t, e)
	contractEqual(t, k.Parcels, int64(len(parcels)))
	contractEqual(t, store.ValidParkID(k.ID), true)
	contractEqual(t, store.ValidPassID(k.WalkRun), true)
	for _, stamp := range []time.Time{k.Walked, k.Created, k.Updated} {
		contractEqual(t, stamp.Location(), time.UTC)
		contractEqual(t, stamp.Nanosecond(), 0)
	}
	_, e = f.s.FinishWalk(ctx, p.ID, store.Counts{ParksWalked: 2, Circles: 2, Requests: 2, Found: 2, NewCandidates: 1, Cost: 2 * places.PriceNanos}, false)
	contractOK(t, e)
	// Equal-second walks use recorded order as their final tie breaker.
	p2 := walkPass(t, f)
	replacement := first
	replacement.Place.Name = "Later"
	w2, e := f.s.RecordSweep(ctx, store.Sweep{Pass: p2.ID, Park: ks[0].ID, Found: []places.Found{replacement}, Circles: 2, Requests: 3, Cost: 2 * places.PriceNanos, CeilingReached: true})
	contractOK(t, e)
	contractEqual(t, w2.NewCandidates, int64(0))
	f.reopen()
	ws, e = f.s.ParkWalks(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, ws, []store.ParkWalk{w2, w})
	xs, e = f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, len(xs), 3)
	for i := 1; i < len(xs); i++ {
		if xs[i-1].At.Before(xs[i].At) || xs[i-1].At.Equal(xs[i].At) && xs[i-1].Walk > xs[i].Walk {
			t.Fatal(xs)
		}
	}
	k, e = f.s.GetPark(ctx, k.ID)
	contractOK(t, e)
	contractEqual(t, k.WalkRun, p2.ID)
	contractEqual(t, k.CeilingReached, true)
	active, e := f.s.ListParks(ctx, store.ParkFilter{Walked: parks.WalkedWalked}, parks.SortValue, "", "", 100)
	contractOK(t, e)
	contractEqual(t, len(active), 2)
	for _, missing := range []string{"", "missing"} {
		ws, e = f.s.ParkWalks(ctx, missing)
		contractOK(t, e)
		contractEqual(t, ws, []store.ParkWalk{})
		ws, e = f.s.PassWalks(ctx, missing)
		contractOK(t, e)
		contractEqual(t, ws, []store.ParkWalk{})
		xs, e = f.s.ParkPlaces(ctx, missing)
		contractOK(t, e)
		contractEqual(t, xs, []store.Place{})
		cs, e = f.s.ParkCandidates(ctx, missing)
		contractOK(t, e)
		contractEqual(t, cs, []store.FoundCandidate{})
	}
}

// R-O7BZ-BD5K R-UITE-KIHK
func TestWalkEndingsAndFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	id := contractSeed(t, f)
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
	contractOK(t, err)
	k := seedParks(t, f)[0]
	prior := walkPass(t, f)
	walkSweep(t, f, prior, k, walkFound("preserved", "Preserved", "3205550101", ""))
	_, err = f.s.FinishWalk(ctx, prior.ID, store.Counts{ParksWalked: 1, Circles: 1, Requests: 1, Found: 1, Cost: places.PriceNanos}, false)
	contractOK(t, err)
	snapshot := func() []any {
		state := contractState(t, f)
		state.passes = nil
		ps, e := f.s.ListPasses(ctx, "", "", 100)
		contractOK(t, e)
		other := []store.Pass{}
		for _, pass := range ps {
			if pass.Kind != store.KindWalk || pass.ID == prior.ID {
				other = append(other, pass)
			}
		}
		xs, e := f.s.ParkPlaces(ctx, k.ID)
		contractOK(t, e)
		ws, e := f.s.ParkWalks(ctx, k.ID)
		contractOK(t, e)
		pw, e := f.s.PassWalks(ctx, prior.ID)
		contractOK(t, e)
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		return []any{state, other, parkState(t, f), xs, ws, pw, facts}
	}
	before := snapshot()
	p := walkPass(t, f)
	counts := store.Counts{Rows: 4, New: 3, ParksWalked: 1, Circles: 2, Requests: 3, Found: 4, NewCandidates: 2, Cost: 77}
	f.now = f.now.Add(time.Hour)
	done, e := f.s.FinishWalk(ctx, p.ID, counts, true)
	contractOK(t, e)
	contractEqual(t, done.Status, store.StatusDone)
	contractEqual(t, done.CeilingReached, true)
	contractEqual(t, done.Rows, int64(4))
	contractEqual(t, done.Found, int64(4))
	contractEqual(t, done.Finished, f.now.UTC().Truncate(time.Second))
	want := p
	want.Status = store.StatusDone
	want.Finished = f.now.UTC().Truncate(time.Second)
	want.Rows = counts.Rows
	want.New = counts.New
	want.ParksWalked = counts.ParksWalked
	want.Circles = counts.Circles
	want.Requests = counts.Requests
	want.Found = counts.Found
	want.NewCandidates = counts.NewCandidates
	want.Cost = counts.Cost
	want.CeilingReached = true
	contractEqual(t, done, want)
	contractEqual(t, snapshot(), before)
	f.reopen()
	got, e := f.s.GetPass(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, got, done)
	contractEqual(t, snapshot(), before)
	for _, reason := range []string{store.ReasonPlaces, store.ReasonScrapes, store.ReasonStore, store.ReasonPrompts, store.ReasonInterrupted} {
		p = walkPass(t, f)
		f.now = f.now.Add(time.Minute)
		failed, e := f.s.FailWalk(ctx, p.ID, reason, counts)
		contractOK(t, e)
		contractEqual(t, failed.Status, store.StatusFailed)
		contractEqual(t, failed.Reason, reason)
		contractEqual(t, failed.Rows, int64(0))
		contractEqual(t, failed.New, int64(0))
		contractEqual(t, failed.ParksWalked, counts.ParksWalked)
		contractEqual(t, failed.Circles, counts.Circles)
		contractEqual(t, failed.Requests, counts.Requests)
		contractEqual(t, failed.Found, counts.Found)
		contractEqual(t, failed.NewCandidates, counts.NewCandidates)
		contractEqual(t, failed.Cost, counts.Cost)
		contractEqual(t, failed.CeilingReached, false)
		want = p
		want.Status = store.StatusFailed
		want.Reason = reason
		want.Finished = f.now.UTC().Truncate(time.Second)
		want.ParksWalked = counts.ParksWalked
		want.Circles = counts.Circles
		want.Requests = counts.Requests
		want.Found = counts.Found
		want.NewCandidates = counts.NewCandidates
		want.Cost = counts.Cost
		contractEqual(t, failed, want)
		contractEqual(t, snapshot(), before)
		f.reopen()
		got, e = f.s.GetPass(ctx, p.ID)
		contractOK(t, e)
		contractEqual(t, got, failed)
		contractEqual(t, snapshot(), before)
	}
	for _, call := range []func(string, store.Counts) error{func(id string, c store.Counts) error { _, e := f.s.FinishWalk(ctx, id, c, true); return e }, func(id string, c store.Counts) error { _, e := f.s.FailWalk(ctx, id, store.ReasonPlaces, c); return e }} {
		contractError(t, call("missing", store.Counts{}), store.ErrNotFound)
		contractError(t, call("missing", store.Counts{Circles: -1}), store.ErrUnreachable)
		contractError(t, call(p.ID, store.Counts{}), store.ErrEnded)
	}
	p = walkPass(t, f)
	_, e = f.s.FailWalk(ctx, p.ID, "", counts)
	contractError(t, e, store.ErrUnreachable)
	_, e = f.s.FailWalk(ctx, p.ID, "unknown", counts)
	contractError(t, e, store.ErrUnreachable)
	judge, e := f.s.AddPass(ctx, store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "owner"})
	contractOK(t, e)
	_, e = f.s.FinishWalk(ctx, judge.ID, store.Counts{}, true)
	contractError(t, e, store.ErrEnded)
	_, e = f.s.FailWalk(ctx, judge.ID, store.ReasonPlaces, store.Counts{})
	contractError(t, e, store.ErrEnded)
}

// R-0L7X-9YEF R-UWXV-8JOI R-V0LK-DUWL R-O3OA-61XH
func TestWalkMatchingPriorityAndDisplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	k := seedParks(t, f)[0]
	// Two records can share a phone without sharing their source's attach keys.
	r := sources.Reading{Name: "Record Older", Names: []string{"Record Alias"}, Phone: "3205550111", PhoneKey: "3205550111", DomainKey: "older.example", Street: "record street", City: "Record City", State: "MN", Zip: "55056", NameKey: "recordolder", NameKeys: []string{"recordolder"}}
	older := contractAttach(t, f, "record-run", store.Observation{Scraper: "fmcsa", SourceID: "older", Digest: "old", Reading: r})[0]
	f.now = f.now.Add(time.Second)
	r2 := r
	r2.Name = "Record Newer"
	r2.NameKey = "recordnewer"
	r2.NameKeys = []string{"recordnewer"}
	r2.DomainKey = "newer.example"
	newer := contractAttach(t, f, "record-run", store.Observation{Scraper: "fmcsa", SourceID: "newer", Digest: "new", Reading: r2})[0]
	contractEqual(t, older != newer, true)
	// Gone links and owner exclusions remain matchable under the places rule.
	_, _, e := f.s.MarkGone(ctx, "fmcsa", "next-run", "", 100)
	contractOK(t, e)
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: older, Why: "owner", UserID: "owner"})
	contractOK(t, e)
	before, e := f.s.GetCandidate(ctx, older)
	contractOK(t, e)
	f.now = f.now.Add(time.Hour)
	p := walkPass(t, f)
	a := walkFound("phone-first", "Place Phone", "3205550111", "https://newer.example/page")
	b := walkFound("domain-second", "Domain Only", "", "https://newer.example/home")
	c := walkFound("name-first", "Shared Place", "", "")
	d := walkFound("name-second", "Shared Place LLC", "", "")
	social1 := walkFound("social-first", "Unrelated One", "", "https://facebook.com/one")
	social2 := walkFound("social-second", "Unrelated Two", "", "https://facebook.com/two")
	w := walkSweep(t, f, p, k, b, a, c, d, social1, social2)
	contractEqual(t, w.Found, int64(6))
	contractEqual(t, w.NewCandidates, int64(3))
	xs, e := f.s.ParkPlaces(ctx, k.ID)
	contractOK(t, e)
	byID := map[string]store.Place{}
	for _, x := range xs {
		byID[x.PlaceID] = x
	}
	contractEqual(t, byID[a.Place.ID].Candidate, older)
	contractEqual(t, byID[b.Place.ID].Candidate, newer)
	contractEqual(t, byID[c.Place.ID].Candidate, byID[d.Place.ID].Candidate)
	contractEqual(t, byID[c.Place.ID].New, true)
	contractEqual(t, byID[d.Place.ID].New, false)
	contractEqual(t, byID[social1.Place.ID].Candidate != byID[social2.Place.ID].Candidate, true)
	after, e := f.s.GetCandidate(ctx, older)
	contractOK(t, e)
	contractEqual(t, after.Name, a.Place.Name)
	contractEqual(t, after.AddressSource, places.Name)
	contractEqual(t, after.Created, before.Created)
	contractEqual(t, after.Updated, f.now.UTC().Truncate(time.Second))
	for _, pair := range []struct {
		id, source, reason string
		confidence         float64
	}{{older, a.Place.ID, store.LinkPhone, .85}, {newer, b.Place.ID, store.LinkDomain, .85}, {byID[d.Place.ID].Candidate, d.Place.ID, store.LinkMatchKey, .8}, {byID[c.Place.ID].Candidate, c.Place.ID, store.LinkSourceKey, 1}} {
		ls, e := f.s.Links(ctx, pair.id)
		contractOK(t, e)
		var l store.Link
		for _, x := range ls {
			if x.Scraper == places.Name && x.SourceID == pair.source {
				l = x
			}
		}
		contractEqual(t, l.Reason, pair.reason)
		contractEqual(t, l.Confidence, pair.confidence)
		contractEqual(t, l.Run, "")
		contractEqual(t, l.Digest, "")
		contractEqual(t, l.Walk, p.ID)
		contractEqual(t, l.Park, k.ID)
		contractEqual(t, l.Gone, false)
	}
	// Another walk's source key wins even when a changed phone matches another business.
	_, e = f.s.FinishWalk(ctx, p.ID, store.Counts{}, false)
	contractOK(t, e)
	ls, e := f.s.Links(ctx, older)
	contractOK(t, e)
	var oldLink store.Link
	for _, l := range ls {
		if l.SourceID == a.Place.ID {
			oldLink = l
		}
	}
	f.now = f.now.Add(time.Hour)
	p2 := walkPass(t, f)
	a.Place.Phone = "3205550999"
	a.Place.Name = "Changed Place Name"
	a.Place.Website = "https://changed.example"
	walkSweep(t, f, p2, k, a)
	f.reopen()
	ls, e = f.s.Links(ctx, older)
	contractOK(t, e)
	for _, l := range ls {
		if l.SourceID == a.Place.ID {
			contractEqual(t, l.Candidate, oldLink.Candidate)
			contractEqual(t, l.Reason, oldLink.Reason)
			contractEqual(t, l.Confidence, oldLink.Confidence)
			contractEqual(t, l.FirstSeen, oldLink.FirstSeen)
			contractEqual(t, l.LastSeen, f.now.UTC().Truncate(time.Second))
			contractEqual(t, l.Reading, places.Reading(a.Place))
			contractEqual(t, l.Walk, p2.ID)
		}
	}
	after, e = f.s.GetCandidate(ctx, older)
	contractOK(t, e)
	contractEqual(t, after.Name, a.Place.Name)
	contractEqual(t, after.Phone, a.Place.Phone)
	contractEqual(t, after.NameSource, places.Name)
	contractEqual(t, after.Created, before.Created)
	contractEqual(t, after.Updated, f.now.UTC().Truncate(time.Second))
	// Places-only candidates use their place's display.
	made, e := f.s.GetCandidate(ctx, byID[c.Place.ID].Candidate)
	contractOK(t, e)
	contractEqual(t, made.Street, "15 Main St")
	contractEqual(t, made.City, "Example")
	contractEqual(t, made.State, "MN")
	contractEqual(t, made.Zip, "55056")
	rulings, e := f.s.Rulings(ctx, older)
	contractOK(t, e)
	contractEqual(t, len(rulings), 1)
	contractEqual(t, rulings[0].Kind, store.KindExclude)
}

// R-0Q3I-T1D7 R-UZDO-035W R-UY5R-MBF7
func TestWalkSweepRefusalsAndUnansweredSweep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	ks := seedParks(t, f)
	p := walkPass(t, f)
	base := store.Sweep{Pass: p.ID, Park: ks[0].ID, Found: []places.Found{walkFound("place", "Name", "", "")}, Circles: 1, Requests: 1, Cost: places.PriceNanos}
	for _, change := range []func(*store.Sweep){func(s *store.Sweep) { s.Pass = "" }, func(s *store.Sweep) { s.Park = "" }, func(s *store.Sweep) { s.Circles = -1 }, func(s *store.Sweep) { s.Requests = -1 }, func(s *store.Sweep) { s.Cost = -1 }, func(s *store.Sweep) { s.Found = []places.Found{{}} }} {
		s := base
		change(&s)
		w, e := f.s.RecordSweep(ctx, s)
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, w, store.ParkWalk{})
	}
	for _, change := range []func(*store.Sweep){func(s *store.Sweep) { s.Pass = "missing" }, func(s *store.Sweep) { s.Park = "missing" }} {
		s := base
		change(&s)
		_, e := f.s.RecordSweep(ctx, s)
		contractError(t, e, store.ErrNotFound)
	}
	f.rnd = &contractRandom{fail: true}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	_, e := f.s.RecordSweep(ctx, base)
	contractError(t, e, store.ErrUnreachable)
	f.rnd = &contractRandom{next: 100}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	f.reopen()
	xs, e := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, xs, []store.Place{})
	ws, e := f.s.ParkWalks(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, ws, []store.ParkWalk{})
	// A failed first request accounts its request, but does not mark a park walked.
	w, e := f.s.RecordSweep(ctx, store.Sweep{Pass: p.ID, Park: ks[0].ID, Requests: 1, CeilingReached: true})
	contractOK(t, e)
	contractEqual(t, w, store.ParkWalk{Pass: p.ID, Park: ks[0].ID, Name: ks[0].Name, Requests: 1})
	ws, e = f.s.ParkWalks(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, ws, []store.ParkWalk{})
	_, e = f.s.FailWalk(ctx, p.ID, store.ReasonPlaces, store.Counts{Requests: 1})
	contractOK(t, e)
	_, e = f.s.RecordSweep(ctx, base)
	contractError(t, e, store.ErrEnded)
	judge, e := f.s.AddPass(ctx, store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "owner"})
	contractOK(t, e)
	base.Pass = judge.ID
	_, e = f.s.RecordSweep(ctx, base)
	contractError(t, e, store.ErrEnded)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, e = f.s.RecordSweep(canceled, base)
	contractError(t, e, store.ErrUnreachable)
	contractEqual(t, errors.Is(e, context.Canceled), true)
	f.d.SetFailing(true)
	_, e = f.s.RecordSweep(ctx, base)
	contractError(t, e, store.ErrUnreachable)
	f.d.SetFailing(false)
}

// R-12AI-MQS5 R-O8JV-P4W9 R-O9RS-2WMY R-VIOF-L0A2 R-0ZUP-V7AR
func TestWalkHistorySurvivesOtherWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	ks := seedParks(t, f)
	p := walkPass(t, f)
	ceiling, e := f.s.WalkCeiling(ctx)
	contractOK(t, e)
	contractEqual(t, ceiling, int64(places.SeedCeiling))
	walkSweep(t, f, p, ks[0], walkFound("one", "Original Name", "3205550199", ""))
	_, e = f.s.FinishWalk(ctx, p.ID, store.Counts{}, false)
	contractOK(t, e)
	xs, e := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	ws, e := f.s.ParkWalks(ctx, ks[0].ID)
	contractOK(t, e)
	id := xs[0].Candidate
	survivor := contractAttach(t, f, "record-run", store.Observation{Scraper: "fmcsa", SourceID: "survivor", Digest: "digest", Reading: sources.Reading{Name: "Survivor"}})[0]
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: id, Why: "same", UserID: "owner"})
	contractOK(t, e)
	park, e := f.s.CandidatePark(ctx, id)
	contractOK(t, e)
	contractEqual(t, park, "")
	park, e = f.s.CandidatePark(ctx, survivor)
	contractOK(t, e)
	contractEqual(t, park, ks[0].ID)
	found, e := f.s.ParkCandidates(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, found, []store.FoundCandidate{{ID: id, Status: store.StatusMerged, New: true}})
	for _, scraper := range []string{"fmcsa", "ppp", "form5500", "osha_ita"} {
		_, _, e = f.s.MarkGone(ctx, scraper, "another", "", 100)
		contractOK(t, e)
	}
	_, _, e = f.s.MarkGone(ctx, places.Name, "another", "", 100)
	contractError(t, e, store.ErrUnreachable)
	ls, e := f.s.Links(ctx, survivor)
	contractOK(t, e)
	for _, l := range ls {
		if l.Scraper == places.Name {
			contractEqual(t, l.Gone, false)
		}
	}
	applyParkPlan(t, f, parks.Plan{Gone: []string{ks[0].ID}})
	ceiling = 250
	rulesPass, e := f.s.SetParkRules(ctx, store.ParkRulesEdit{WalkCeiling: &ceiling}, parkPass(store.OriginRules))
	contractOK(t, e)
	_, e = f.s.FailPass(ctx, rulesPass.ID, store.ReasonStore)
	contractOK(t, e)
	p2 := walkPass(t, f)
	_, e = f.s.FinishWalk(ctx, p2.ID, store.Counts{}, false)
	contractOK(t, e)
	for {
		n, e := f.s.PrunePasses(ctx, 1, 100)
		contractOK(t, e)
		if n == 0 {
			break
		}
	}
	f.reopen()
	after, e := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, after, xs)
	afterW, e := f.s.ParkWalks(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, afterW, ws)
	afterW, e = f.s.PassWalks(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, afterW, ws)
	ceiling, e = f.s.WalkCeiling(ctx)
	contractOK(t, e)
	contractEqual(t, ceiling, int64(250))
	_, e = f.s.CandidatePark(ctx, "missing")
	contractError(t, e, store.ErrNotFound)
}

// R-UDXT-1FIS
func TestWalkSweepAndFailureSerialize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for range 8 {
		f := contractDB(t)
		k := seedParks(t, f)[0]
		p := walkPass(t, f)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var sweepErr, endErr error
		go func() {
			defer wg.Done()
			<-start
			_, sweepErr = f.s.RecordSweep(ctx, store.Sweep{Pass: p.ID, Park: k.ID, Found: []places.Found{walkFound("race", "Race", "", "")}, Circles: 1, Requests: 1, Cost: places.PriceNanos})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, endErr = f.s.FailWalk(ctx, p.ID, store.ReasonInterrupted, store.Counts{Requests: 1})
		}()
		close(start)
		wg.Wait()
		contractOK(t, endErr)
		if sweepErr != nil {
			contractError(t, sweepErr, store.ErrEnded)
		}
		f.reopen()
		xs, e := f.s.ParkPlaces(ctx, k.ID)
		contractOK(t, e)
		ws, e := f.s.ParkWalks(ctx, k.ID)
		contractOK(t, e)
		if sweepErr == nil {
			contractEqual(t, len(xs), 1)
			contractEqual(t, len(ws), 1)
		} else {
			contractEqual(t, len(xs), 0)
			contractEqual(t, len(ws), 0)
		}
	}
}

// R-VIOF-L0A2 R-V31D-5EDZ R-V1TG-RMNA R-0YMT-HFK2 R-O3OA-61XH
func TestWalkStableCandidateParkAndHistoryOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	ks := seedParks(t, f)
	p := walkPass(t, f)
	w1 := walkSweep(t, f, p, ks[0], walkFound("z-key", "Same", "3205550160", ""))
	w2 := walkSweep(t, f, p, ks[1], walkFound("a-key", "Same", "3205550160", ""))
	xs, e := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	id := xs[0].Candidate
	k, e := f.s.CandidatePark(ctx, id)
	contractOK(t, e)
	contractEqual(t, k, ks[1].ID)
	_, e = f.s.FinishWalk(ctx, p.ID, store.Counts{}, false)
	contractOK(t, e)
	f.now = f.now.Add(time.Hour)
	p2 := walkPass(t, f)
	w3 := walkSweep(t, f, p2, ks[0], walkFound("z-key", "Updated Same", "3205550160", ""))
	k, e = f.s.CandidatePark(ctx, id)
	contractOK(t, e)
	contractEqual(t, k, ks[1].ID)
	// Two records at one time use sequence; places preserve distinct walk snapshots.
	w4 := walkSweep(t, f, p2, ks[0], walkFound("b-key", "Beta", "3205550160", ""))
	f.reopen()
	ws, e := f.s.ParkWalks(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, ws, []store.ParkWalk{w4, w3, w1})
	ws, e = f.s.PassWalks(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, ws, []store.ParkWalk{w1, w2})
	xs, e = f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	contractEqual(t, len(xs), 3)
	contractEqual(t, xs[0].PlaceID, "b-key")
	contractEqual(t, xs[1].PlaceID, "z-key")
	contractEqual(t, xs[0].Walk, p2.ID)
	contractEqual(t, xs[1].Walk, p2.ID)
	contractEqual(t, xs[2].Walk, p.ID)
	contractEqual(t, xs[2].Name, "Same")
}

// R-UDXT-1FIS R-O8JV-P4W9
func TestWalkMergeAndSweepSerialize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for range 4 {
		f := contractDB(t)
		k := seedParks(t, f)[0]
		survivor := contractAttach(t, f, "records", store.Observation{Scraper: "fmcsa", SourceID: "survivor", Digest: "one", Reading: sources.Reading{Name: "Keep"}})[0]
		other := contractAttach(t, f, "records", store.Observation{Scraper: "fmcsa", SourceID: "other", Digest: "two", Reading: sources.Reading{Name: "Other", PhoneKey: "3205550177"}})[0]
		p := walkPass(t, f)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var sweepErr, mergeErr error
		go func() {
			defer wg.Done()
			<-start
			_, sweepErr = f.s.RecordSweep(ctx, store.Sweep{Pass: p.ID, Park: k.ID, Found: []places.Found{walkFound("merge-race", "Place", "3205550177", "")}, Circles: 1, Requests: 1, Cost: places.PriceNanos})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _, mergeErr = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: other, Why: "same", UserID: "owner"})
		}()
		close(start)
		wg.Wait()
		contractOK(t, sweepErr)
		contractOK(t, mergeErr)
		f.reopen()
		ls, e := f.s.Links(ctx, survivor)
		contractOK(t, e)
		var found bool
		for _, l := range ls {
			if l.Scraper == places.Name {
				found = true
				contractEqual(t, l.SourceID, "merge-race")
			}
		}
		contractEqual(t, found, true)
		xs, e := f.s.ParkPlaces(ctx, k.ID)
		contractOK(t, e)
		contractEqual(t, len(xs), 1)
		if xs[0].Candidate != survivor && xs[0].Candidate != other {
			t.Fatal(xs)
		}
	}
}

// R-0ZUP-V7AR R-0YMT-HFK2
func TestWalkFoundCandidatesAndPlaceFinalOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	k := seedParks(t, f)[0]
	names := []string{"Zeta", "Same", "Same", "Alpha"}
	phones := []string{"3205550104", "3205550105", "3205550106", "3205550107"}
	ids := []string{}
	for i, name := range names {
		ids = append(ids, contractAttach(t, f, "records", store.Observation{Scraper: "fmcsa", SourceID: phones[i], Digest: "digest", Reading: sources.Reading{Name: name, PhoneKey: phones[i]}})[0])
	}
	p := walkPass(t, f)
	found := []places.Found{walkFound("z-last-key", "Equal place name", phones[0], ""), walkFound("a-first-key", "Equal place name", phones[0], ""), walkFound("same-one", "Other one", phones[1], ""), walkFound("same-two", "Other two", phones[2], ""), walkFound("alpha-one", "Other three", phones[3], ""), walkFound("beta-new", "Beta", "3205550108", ""), walkFound("beta-joined", "Beta second", "3205550108", "")}
	walkSweep(t, f, p, k, found...)
	f.reopen()
	xs, e := f.s.ParkPlaces(ctx, k.ID)
	contractOK(t, e)
	contractEqual(t, len(xs), len(found))
	byID := map[string]store.Place{}
	for _, x := range xs {
		byID[x.PlaceID] = x
	}
	contractEqual(t, xs[2].PlaceID, "a-first-key")
	contractEqual(t, xs[3].PlaceID, "z-last-key")
	beta := byID["beta-new"].Candidate
	cs, e := f.s.ParkCandidates(ctx, k.ID)
	contractOK(t, e)
	contractEqual(t, cs, []store.FoundCandidate{{ID: beta, Name: "Beta second", Status: store.StatusActive, New: true}, {ID: ids[0], Name: "Equal place name", Status: store.StatusActive}, {ID: ids[1], Name: "Other one", Status: store.StatusActive}, {ID: ids[3], Name: "Other three", Status: store.StatusActive}, {ID: ids[2], Name: "Other two", Status: store.StatusActive}})
	contractEqual(t, byID["z-last-key"].New, false)
	contractEqual(t, byID["a-first-key"].New, false)
	contractEqual(t, byID["beta-new"].New, true)
	contractEqual(t, byID["beta-joined"].New, false)
}

// R-V1TG-RMNA R-V31D-5EDZ
func TestWalkHistoryReadsCurrentParkName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	k := seedParks(t, f)[0]
	p := walkPass(t, f)
	w := walkSweep(t, f, p, k, walkFound("one", "Business", "", ""))
	contractEqual(t, w.Name, parks.Name(k.Town, k.Number, 1))
	applyParkPlan(t, f, parks.Plan{News: []parks.New{{Cluster: parkCluster("new-neighbor", k.Town, k.County, k.Zip, 50), Number: 2}}})
	f.reopen()
	want := w
	want.Name = parks.Name(k.Town, k.Number, 2)
	contractEqual(t, want.Name != w.Name, true)
	pw, e := f.s.PassWalks(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, pw, []store.ParkWalk{want})
	kw, e := f.s.ParkWalks(ctx, k.ID)
	contractOK(t, e)
	contractEqual(t, kw, []store.ParkWalk{want})
}

// R-UWXV-8JOI R-0Q3I-T1D7
func TestWalkSecondCandidateMintFailureRollsBackSweep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	id := contractSeed(t, f)
	ks := seedParks(t, f)
	k := ks[0]
	p := walkPass(t, f)
	// Keep existing records on every surface the failed sweep can mutate.
	walkSweep(t, f, p, k, walkFound("existing", "Existing", "3205550101", ""))
	before := contractState(t, f)
	parksBefore := parkState(t, f)
	placesBefore, e := f.s.ParkPlaces(ctx, k.ID)
	contractOK(t, e)
	walksBefore, e := f.s.ParkWalks(ctx, k.ID)
	contractOK(t, e)
	passWalksBefore, e := f.s.PassWalks(ctx, p.ID)
	contractOK(t, e)
	linksBefore, e := f.s.Links(ctx, id)
	contractOK(t, e)
	firstID := store.CandidateIDPrefix + "f0f0f0f0f0f0f0f0"
	rnd := &contractMeter{reader: io.MultiReader(bytes.NewReader(bytes.Repeat([]byte{0xf0}, 8)), &contractRandom{fail: true})}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: rnd})
	out, e := f.s.RecordSweep(ctx, store.Sweep{Pass: p.ID, Park: k.ID, Found: []places.Found{walkFound("first-new", "First new", "", ""), walkFound("second-new", "Second new", "", "")}, Circles: 1, Requests: 1, Cost: places.PriceNanos})
	contractError(t, e, store.ErrUnreachable)
	contractEqual(t, out, store.ParkWalk{})
	contractEqual(t, rnd.total, 8)
	contractEqual(t, len(rnd.requests), 2)
	f.reopen()
	contractEqual(t, contractState(t, f), before)
	contractEqual(t, parkState(t, f), parksBefore)
	xs, e := f.s.ParkPlaces(ctx, k.ID)
	contractOK(t, e)
	contractEqual(t, xs, placesBefore)
	ws, e := f.s.ParkWalks(ctx, k.ID)
	contractOK(t, e)
	contractEqual(t, ws, walksBefore)
	pw, e := f.s.PassWalks(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, pw, passWalksBefore)
	ls, e := f.s.Links(ctx, id)
	contractOK(t, e)
	contractEqual(t, ls, linksBefore)
	_, e = f.s.GetCandidate(ctx, firstID)
	contractError(t, e, store.ErrNotFound)
	// A clean retry must encounter neither a leftover source link nor a place.
	retried := walkSweep(t, f, p, k, walkFound("first-new", "First new", "", ""), walkFound("second-new", "Second new", "", ""))
	contractEqual(t, retried.Found, int64(2))
	contractEqual(t, retried.NewCandidates, int64(2))
}

// R-0L7X-9YEF
func TestWalkNameMatchRequiresPlaceAndIncludesAlias(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	k := seedParks(t, f)[0]
	canonical := sources.NameKey("Canonical Business")
	alias := sources.NameKey("Trading Alias")
	id := contractAttach(t, f, "records", store.Observation{Scraper: "fmcsa", SourceID: "record", Digest: "digest", Reading: sources.Reading{Name: "Canonical Business", Names: []string{"Canonical Business", "Trading Alias"}, NameKey: canonical, NameKeys: []string{canonical, alias}, State: "MN", Zip: "55056"}})[0]
	p := walkPass(t, f)
	wrongState := walkFound("wrong-state", "Canonical Business", "", "")
	wrongState.Place.Address = "1 Other St, Other, WI 55056, USA"
	wrongZIP := walkFound("wrong-zip", "Canonical Business", "", "")
	wrongZIP.Place.Address = "1 Other St, Other, MN 55057, USA"
	noPlace := walkFound("no-place", "Canonical Business", "", "")
	noPlace.Place.Address = "unstructured address"
	matchingAlias := walkFound("matching-alias", "Trading Alias LLC", "", "")
	walkSweep(t, f, p, k, wrongState, wrongZIP, noPlace, matchingAlias)
	f.reopen()
	xs, e := f.s.ParkPlaces(ctx, k.ID)
	contractOK(t, e)
	contractEqual(t, len(xs), 4)
	for _, x := range xs {
		if x.PlaceID == matchingAlias.Place.ID {
			contractEqual(t, x.Candidate, id)
			contractEqual(t, x.New, false)
		} else {
			contractEqual(t, x.Candidate != id, true)
			contractEqual(t, x.New, true)
		}
	}
	ls, e := f.s.Links(ctx, id)
	contractOK(t, e)
	for _, l := range ls {
		if l.Scraper == places.Name {
			contractEqual(t, l.SourceID, matchingAlias.Place.ID)
			contractEqual(t, l.Reason, store.LinkMatchKey)
			contractEqual(t, l.Confidence, .8)
		}
	}
}

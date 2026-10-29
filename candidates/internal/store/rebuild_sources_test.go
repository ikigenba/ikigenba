package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-E7A7-IPD8
func TestRebuildMethod(t *testing.T) {
	f := contractDB(t)
	var api interface {
		Rebuild(context.Context, string, int) (string, error)
	} = f.s
	last, err := api.Rebuild(context.Background(), "", 1)
	contractOK(t, err)
	contractEqual(t, last, "")
}

type rebuildCandidateState struct {
	Candidate   store.Candidate
	Links       []store.Link
	Estimates   []sources.Estimate
	Judgments   store.Judgments
	NoJudgments bool
	Facts       store.Facts
	NoFacts     bool
	Enrichments []store.Enrichment
	Rulings     []store.Ruling
	Pages       []store.Page
	Uses        []store.CampaignUse
}

type rebuildParkState struct {
	Park   store.Park
	Places []store.Place
	Walks  []store.ParkWalk
}

type rebuildState struct {
	Candidates map[string]rebuildCandidateState
	Passes     []store.Pass
	Parks      []rebuildParkState
}

func readRebuildState(t *testing.T, f *contractFixture, ids, parkIDs []string) rebuildState {
	t.Helper()
	ctx := context.Background()
	out := rebuildState{Candidates: map[string]rebuildCandidateState{}}
	var err error
	out.Passes, err = f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, err)
	for _, id := range ids {
		var c rebuildCandidateState
		c.Candidate, err = f.s.GetCandidate(ctx, id)
		contractOK(t, err)
		c.Links, err = f.s.Links(ctx, id)
		contractOK(t, err)
		c.Estimates, err = f.s.Estimates(ctx, id)
		contractOK(t, err)
		c.Judgments, err = f.s.GetJudgments(ctx, id)
		c.NoJudgments = errors.Is(err, store.ErrNotFound)
		if !c.NoJudgments {
			contractOK(t, err)
		}
		c.Facts, err = f.s.GetFacts(ctx, id)
		c.NoFacts = errors.Is(err, store.ErrNotFound)
		if !c.NoFacts {
			contractOK(t, err)
		}
		c.Enrichments, err = f.s.Enrichments(ctx, id)
		contractOK(t, err)
		c.Rulings, err = f.s.Rulings(ctx, id)
		contractOK(t, err)
		c.Pages, err = f.s.Pages(ctx, id)
		contractOK(t, err)
		c.Uses, err = f.s.CampaignUses(ctx, id)
		contractOK(t, err)
		out.Candidates[id] = c
	}
	for _, id := range parkIDs {
		var p rebuildParkState
		p.Park, err = f.s.GetPark(ctx, id)
		contractOK(t, err)
		p.Places, err = f.s.ParkPlaces(ctx, id)
		contractOK(t, err)
		p.Walks, err = f.s.ParkWalks(ctx, id)
		contractOK(t, err)
		out.Parks = append(out.Parks, p)
	}
	return out
}

func rebuildObservation(key string, at time.Time) store.Observation {
	o := contractObservation(key, key, at)
	o.Scraper = "fmcsa"
	o.Reading.PhoneKey, o.Reading.DomainKey = "", ""
	return o
}

type rebuildFixture struct {
	f                    *contractFixture
	active, all, parks   []string
	orphan, place, route string
	pristine             map[string]store.Candidate
	placeLink            store.Link
}

func seedRebuild(t *testing.T) rebuildFixture {
	t.Helper()
	ctx := context.Background()
	f := contractDB(t)
	f.s = routeStore(f, "rebuild-base")
	ks := seedParks(t, f)
	p := walkPass(t, f)
	found := walkFound("rebuild-place", "Walk business", "3205550199", "https://walkbusiness.test/")
	earlier := found
	earlier.Place.Name = "Earlier walk business"
	walkSweep(t, f, p, ks[0], earlier)
	_, err := f.s.FinishWalk(ctx, p.ID, store.Counts{Found: 1, NewCandidates: 1}, false)
	contractOK(t, err)
	ps, err := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, err)
	r := rebuildFixture{f: f, place: ps[0].Candidate, pristine: map[string]store.Candidate{}}
	f.now = f.now.Add(time.Minute)
	p = walkPass(t, f)
	walkSweep(t, f, p, ks[0], found)
	_, err = f.s.FinishWalk(ctx, p.ID, store.Counts{Found: 1}, false)
	contractOK(t, err)
	for _, k := range ks {
		r.parks = append(r.parks, k.ID)
	}
	r.active = append(r.active, r.place)
	for _, key := range []string{"route-business", "untouched-scraper", "orphan-business", "excluded-business", "merged-business"} {
		id := contractAttach(t, f, "rebuild-run", rebuildObservation(key, f.now))[0]
		r.all = append(r.all, id)
		if key != "merged-business" {
			r.active = append(r.active, id)
		}
	}
	r.route, r.orphan = r.all[0], r.all[2]
	merged := r.all[4]
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: r.all[3], Other: merged, Why: "same business", UserID: "owner"})
	contractOK(t, err)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: r.all[3], Why: "standing exclusion", UserID: "owner"})
	contractOK(t, err)
	r.all = append(r.all, r.place)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 42, "55056": 50}))
	for _, id := range r.active {
		_, err = f.s.DeriveAmong(ctx, enrichers.KindLinkFacts, []string{id}, true)
		contractOK(t, err)
		_, err = f.s.AddPage(ctx, store.Page{Candidate: id, URL: "https://kept.test/" + id, Text: "kept body", UserID: "owner"})
		contractOK(t, err)
		contractOK(t, f.s.AddCampaignUse(ctx, id, "campaign-"+id, "prospect-list", f.now))
	}
	subject, err := f.s.Subject(ctx, r.route)
	contractOK(t, err)
	appendRoute(t, f.s, r.route, (enrichers.DriveRoute{}).Inputs(subject), enrichers.RouteFound{Minutes: ptrInt(40), Precision: routes.PrecisionBuilding, Address: "7 Main St S, Rush City, MN 55069, USA"})
	_, _, err = f.s.JudgeAll(ctx, "", 100, f.now)
	contractOK(t, err)
	for _, id := range r.all {
		c, err := f.s.GetCandidate(ctx, id)
		contractOK(t, err)
		r.pristine[id] = c
	}
	ls, err := f.s.Links(ctx, r.place)
	contractOK(t, err)
	r.placeLink = ls[0]
	// A previous reading lacks newly added members and has only its earlier
	// matching keys. Rebuild must repair both the reading and its keyed lookups.
	old := places.Reading(found.Place)
	old.NameKey, old.NameKeys, old.PhoneKey, old.DomainKey = "obsolete-place", nil, "3205550188", "obsolete.test"
	raw, err := json.Marshal(old)
	contractOK(t, err)
	var members map[string]json.RawMessage
	contractOK(t, json.Unmarshal(raw, &members))
	delete(members, "NameKeys")
	delete(members, "Category")
	raw, err = json.Marshal(members)
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, "UPDATE links SET reading=?,name_key=?,phone_key=?,domain_key=?,ein_key=? WHERE scraper=? AND source_id=?", string(raw), "obsolete-place", "3205550188", "obsolete.test", "obsolete-ein", places.Name, found.Place.ID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, "DELETE FROM link_name_keys WHERE scraper=? AND source_id=?", places.Name, found.Place.ID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, "INSERT INTO link_name_keys(scraper,source_id,position,name_key) VALUES (?,?,0,?)", places.Name, found.Place.ID, "obsolete-place"); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, "DELETE FROM links WHERE candidate=?", r.orphan); e != nil {
			return e
		}
		for _, id := range r.active {
			if _, e := tx.ExecContext(ctx, "UPDATE candidates SET name=?,street=? WHERE id=?", "damaged-"+id, "damaged street", id); e != nil {
				return e
			}
		}
		return nil
	}))
	slices.Sort(r.active)
	f.now = f.now.Add(2 * time.Hour)
	return r
}

// R-DBKT-KO36 R-EN4W-HQ09
func TestRebuildRepairsOnlyItsPageAndPersistsIdempotently(t *testing.T) {
	r := seedRebuild(t)
	f := r.f
	ctx := context.Background()
	want := readRebuildState(t, f, r.all, r.parks)
	after := ""
	for start := 0; start < len(r.active); start += 2 {
		end := min(start+2, len(r.active))
		last, err := f.s.Rebuild(ctx, after, 2)
		contractOK(t, err)
		contractEqual(t, last, r.active[end-1])
		for _, id := range r.active[start:end] {
			if id == r.orphan {
				continue
			}
			c := want.Candidates[id]
			c.Candidate = r.pristine[id]
			c.Candidate.Updated = f.now.UTC().Truncate(time.Second)
			if id == r.place {
				c.Links = []store.Link{r.placeLink}
			}
			want.Candidates[id] = c
		}
		contractEqual(t, readRebuildState(t, f, r.all, r.parks), want)
		after = last
	}
	last, err := f.s.Rebuild(ctx, after, 2)
	contractOK(t, err)
	contractEqual(t, last, "")
	contractEqual(t, readRebuildState(t, f, r.all, r.parks), want)
	f.reopen()
	f.s = routeStore(f, "rebuild-base")
	contractEqual(t, readRebuildState(t, f, r.all, r.parks), want)
	f.now = f.now.Add(time.Hour)
	after = ""
	for {
		last, err = f.s.Rebuild(ctx, after, 2)
		contractOK(t, err)
		if last == "" {
			break
		}
		after = last
	}
	contractEqual(t, readRebuildState(t, f, r.all, r.parks), want)
}

// R-DBKT-KO36
func TestRebuildRestoresAllPlaceMatchingKeys(t *testing.T) {
	r := seedRebuild(t)
	f := r.f
	ctx := context.Background()
	_, err := f.s.Rebuild(ctx, "", 100)
	contractOK(t, err)
	f.reopen()
	f.s = routeStore(f, "rebuild-base")
	// Ordinary Attach consults the restored name-key index at the place.
	o := rebuildObservation("name-probe", f.now)
	o.Reading = sources.Reading{Name: "Walk business", NameKey: sources.NameKey("Walk business"), NameKeys: []string{sources.NameKey("Walk business")}, State: "MN", Zip: "55056"}
	ids := contractAttach(t, f, "name-probe-run", o)
	contractEqual(t, ids, []string{r.place})
	ks, err := f.s.ListParks(ctx, store.ParkFilter{}, "", "", "", 100)
	contractOK(t, err)
	p := walkPass(t, f)
	phone := walkFound("phone-probe", "Unrelated phone name", "3205550199", "")
	domain := walkFound("domain-probe", "Unrelated domain name", "", "https://walkbusiness.test/other")
	w := walkSweep(t, f, p, ks[0], phone, domain)
	contractEqual(t, w.NewCandidates, int64(0))
	ls, err := f.s.Links(ctx, r.place)
	contractOK(t, err)
	for _, key := range []string{"phone-probe", "domain-probe"} {
		if !slices.ContainsFunc(ls, func(l store.Link) bool { return l.SourceID == key }) {
			t.Fatalf("repaired key did not attach %s", key)
		}
	}
	// Neither the removed EIN key nor the obsolete name index may match.
	for _, kind := range []string{"ein", "name"} {
		o := rebuildObservation("obsolete-"+kind, f.now)
		o.Reading = sources.Reading{Name: "Obsolete", State: "MN", Zip: "55056"}
		if kind == "ein" {
			o.Reading.EINKey = "obsolete-ein"
		} else {
			o.Reading.NameKey, o.Reading.NameKeys = "obsolete-place", []string{"obsolete-place"}
		}
		ids = contractAttach(t, f, "obsolete-run", o)
		if ids[0] == r.place {
			t.Fatalf("obsolete %s key still matches", kind)
		}
	}
	stalePhone := walkFound("obsolete-phone-probe", "Obsolete phone business", "3205550188", "")
	staleDomain := walkFound("obsolete-domain-probe", "Obsolete domain business", "", "https://obsolete.test/")
	w = walkSweep(t, f, p, ks[0], stalePhone, staleDomain)
	contractEqual(t, w.NewCandidates, int64(2))
}

// R-SKPK-UU2P
func TestRebuildRejectsInvalidPageWithoutChangingRecords(t *testing.T) {
	r := seedRebuild(t)
	before := readRebuildState(t, r.f, r.all, r.parks)
	for _, n := range []int{0, -1, -20} {
		last, err := r.f.s.Rebuild(context.Background(), r.active[0], n)
		contractEqual(t, last, "")
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, readRebuildState(t, r.f, r.all, r.parks), before)
	}
}

type rebuildConcurrentResult struct {
	Cursor string
	Counts store.Counts
	IDs    []string
	Walk   store.ParkWalk
	State  rebuildState
}

func rebuildConcurrentFixture(t *testing.T) (*contractFixture, []string, store.Observation, store.Sweep) {
	t.Helper()
	f := contractDB(t)
	ks := seedParks(t, f)
	o := rebuildObservation("concurrent-source", f.now)
	o.Reading.PhoneKey, o.Reading.Phone = "3205550144", "3205550144"
	id := contractAttach(t, f, "before-run", o)[0]
	p := walkPass(t, f)
	old := walkFound("concurrent-place", "Before place", "3205550144", "https://before.test/")
	walkSweep(t, f, p, ks[0], old)
	_, err := f.s.FinishWalk(context.Background(), p.ID, store.Counts{Found: 1}, false)
	contractOK(t, err)
	p = walkPass(t, f)
	f.now = f.now.Add(time.Hour)
	o.Digest, o.Reading.Name, o.Reading.Names = "updated-digest", "After source", []string{"After source"}
	newPlace := walkFound("concurrent-place", "After place", "3205550144", "https://after.test/")
	sweep := store.Sweep{Pass: p.ID, Park: ks[0].ID, Found: []places.Found{newPlace}, Circles: 1, Requests: 1, Cost: places.PriceNanos}
	return f, []string{id, ks[0].ID}, o, sweep
}

// R-SLXH-8LTE
func TestRebuildSerializesWithAttachAndRecordSweep(t *testing.T) {
	ctx := context.Background()
	run := func(order []int) rebuildConcurrentResult {
		f, keys, obs, sweep := rebuildConcurrentFixture(t)
		var result rebuildConcurrentResult
		var errs [3]error
		calls := []func(){
			func() { result.Cursor, errs[0] = f.s.Rebuild(ctx, "", 1) },
			func() {
				result.Counts, result.IDs, errs[1] = f.s.Attach(ctx, "after-run", true, []store.Observation{obs})
			},
			func() { result.Walk, errs[2] = f.s.RecordSweep(ctx, sweep) },
		}
		if order == nil {
			start := make(chan struct{})
			var wg sync.WaitGroup
			for _, call := range calls {
				wg.Go(func() { <-start; call() })
			}
			close(start)
			wg.Wait()
		} else {
			for _, i := range order {
				calls[i]()
			}
		}
		for _, err := range errs {
			contractOK(t, err)
		}
		result.State = readRebuildState(t, f, keys[:1], keys[1:])
		return result
	}
	var serial []rebuildConcurrentResult
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		serial = append(serial, run(order))
	}
	for range 4 {
		got := run(nil)
		if !slices.ContainsFunc(serial, func(want rebuildConcurrentResult) bool { return reflect.DeepEqual(got, want) }) {
			t.Fatalf("concurrent rebuild/attach/sweep has no sequential outcome: %#v", got)
		}
	}
}

package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func parkPass(origin string) store.Pass {
	p := store.Pass{Kind: store.KindParks, Origin: origin, Trigger: "parcel-run"}
	switch origin {
	case store.OriginRun:
		p.Delivery = "parcel-event"
	case store.OriginRules:
		p.Trigger = store.TriggerParkRules
		p.UserID = "owner"
	}
	return p
}
func parkCluster(pin, town, county, zip string, value int64) parks.Cluster {
	lat, lon := 45.0, -93.0
	v := value
	return parks.Cluster{County: county, Town: town, Zip: zip, Lat: lat, Lon: lon, Radius: 10, BuildingValue: value, Parcels: []parks.Parcel{{PIN: pin, Street: "1 Main", City: town, County: county, Zip: zip, Lat: &lat, Lon: &lon, BuildingValue: &v, Rule: parks.RuleValue}}}
}
func applyParkPlan(t *testing.T, f *contractFixture, plan parks.Plan) store.Pass {
	t.Helper()
	p, e := f.s.AddPass(context.Background(), parkPass(store.OriginStart))
	contractOK(t, e)
	p, e = f.s.ApplyParks(context.Background(), p.ID, "parcel-run", []string{"county", "county"}, plan, store.Counts{ParcelsRead: 4, ByLabel: 1, ByValue: 2, ByCandidate: 1, ParksCreated: int64(len(plan.News)), ParksUpdated: int64(len(plan.Updates)), ParksGone: int64(len(plan.Gone)), ParksGrown: plan.Grown})
	contractOK(t, e)
	return p
}
func seedParks(t *testing.T, f *contractFixture) []store.Park {
	t.Helper()
	applyParkPlan(t, f, parks.Plan{News: []parks.New{{Cluster: parkCluster("first", "Town", "county", "55056", 100), Number: 1}, {Cluster: parkCluster("second", "Other", "other", "55092", 200), Number: 1}, {Cluster: parkCluster("third", "Far", "county", "", 300), Number: 1}}})
	ps, e := f.s.ListParks(context.Background(), store.ParkFilter{}, parks.SortValue, "", "", 100)
	contractOK(t, e)
	return ps
}

// R-5MM8-0QKM R-SVLQ-I4SH R-ONLR-C520 R-5P20-SA20 R-5Q9X-61SP R-OQ1K-3OJE
func TestParkRecordShapesAndVocabulary(t *testing.T) {
	t.Parallel()
	contractEqual(t, []string{store.KindParks, store.OriginRules, store.OriginStart, store.TriggerParkRules, store.ParkIDPrefix}, []string{"parks", "rules", "start", "edit_park_rules", "park_"})
	valid := store.ParkIDPrefix + "0123456789abcdef"
	contractEqual(t, store.ValidParkID(valid), true)
	for _, s := range []string{"", store.ParkIDPrefix, store.ParkIDPrefix + "0123456789ABCDEF", store.ParkIDPrefix + "0123456789abcde", store.ParkIDPrefix + "0123456789abcdef0", store.CandidateIDPrefix + "0123456789abcdef", store.ProspectListIDPrefix + "0123456789abcdef"} {
		contractEqual(t, store.ValidParkID(s), false)
	}
	p := store.Park{ID: valid, Name: "name", County: "county", Town: "town", Zip: "55056", State: parks.StateActive, Number: 1, Lat: 45, Lon: -93, Radius: 2, Parcels: 3, BuildingValue: 4, Minutes: i64(5), GrownFrom: 2, Walked: time.Time{}, WalkRun: "run", Found: 6, Run: "run", Created: time.Time{}, Updated: time.Time{}}
	contractEqual(t, store.Park(struct {
		ID, Name, County, Town, Zip, State string
		Number                             int64
		Lat, Lon, Radius                   float64
		Parcels, BuildingValue             int64
		Minutes                            *int64
		GrownFrom                          int64
		Walked                             time.Time
		WalkRun                            string
		Found, NewCandidates               int64
		CeilingReached                     bool
		Run                                string
		Created, Updated                   time.Time
	}(p)), p)
	q := store.ParkParcel{PIN: "pin", Owners: []string{"one"}, Street: "street", City: "city", Zip: "55056", BuildingValue: i64(1), Rule: parks.RuleValue, Lat: 45, Lon: -93}
	contractEqual(t, store.ParkParcel(struct {
		PIN               string
		Owners            []string
		Street, City, Zip string
		BuildingValue     *int64
		Rule              string
		Lat, Lon          float64
	}(q)), q)
	f := store.ParkFilter{County: "county", Walked: parks.WalkedUnwalked, Gone: parks.GoneAll}
	contractEqual(t, store.ParkFilter(struct{ County, Walked, Gone string }(f)), f)
	county := "county"
	e := store.ParkRulesEdit{Distance: i64(1), MinParcels: i64(2), ValueThreshold: i64(0), County: &county, Labels: []string{"label"}}
	contractEqual(t, store.ParkRulesEdit(struct {
		Distance, MinParcels, ValueThreshold *int64
		County                               *string
		Labels                               []string
		WalkCeiling                          *int64
	}(e)), e)
}
func seedRules() parks.Rules {
	return parks.Rules{Distance: parks.SeedDistance, MinParcels: parks.SeedMinParcels, ValueThreshold: parks.SeedValueThreshold, Labels: map[string][]string{"Chisago": {"3a Industrial Land And Building"}, "Stearns": {"3a Industrial Land And Building"}, "Anoka": {"3a INDUSTIAL PREFERENTIAL"}, "Aitkin": {"Industrial Preferred"}, "St. Louis": {"Industrial Preferred", "Industrial Non-Preferred"}, "Sherburne": {"Industrial Preferred", "Industrial Non-Preferred", "INDUSTRIAL (with Buildings)", "Industrial - Bare Land"}, "Mille Lacs": {"Indust Land/Building"}, "Isanti": {"3A-Commercial/Industrial/Public Utility"}, "Morrison": {"3A-Commercial/Industrial/Public Utility"}}}
}

// R-64U1-LEO8 R-TRB4-G62J R-W07T-G9JB R-OR9G-HGA3 R-EP8R-Z5D9 R-57ZF-FHOA
func TestParkRulesSeedEditAndRefusal(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	r, cs, e := f.s.ParkRules(ctx)
	contractOK(t, e)
	contractEqual(t, r, seedRules())
	contractEqual(t, cs, []string{"Aitkin", "Anoka", "Chisago", "Isanti", "Mille Lacs", "Morrison", "Sherburne", "St. Louis", "Stearns"})
	county := "Carlton"
	edit := store.ParkRulesEdit{Distance: i64(800), MinParcels: i64(4), ValueThreshold: i64(0), County: &county, Labels: []string{"Second", "First"}}
	p, e := f.s.SetParkRules(ctx, edit, parkPass(store.OriginRules))
	contractOK(t, e)
	contractEqual(t, p.Kind, store.KindParks)
	contractEqual(t, p.Origin, store.OriginRules)
	contractEqual(t, p.Status, store.StatusRunning)
	r.Labels[county] = edit.Labels
	r.Distance = 800
	r.MinParcels = 4
	r.ValueThreshold = 0
	got, cs, e := f.s.ParkRules(ctx)
	contractOK(t, e)
	contractEqual(t, got, r)
	if !slices.Contains(cs, county) {
		t.Fatal(cs)
	}
	f.reopen()
	got, _, e = f.s.ParkRules(ctx)
	contractOK(t, e)
	contractEqual(t, got, r)
	_, e = f.s.SetParkRules(ctx, store.ParkRulesEdit{Distance: i64(900)}, parkPass(store.OriginRules))
	contractError(t, e, store.ErrBusy)
	for _, bad := range []store.ParkRulesEdit{{Distance: i64(0)}, {MinParcels: i64(101)}, {ValueThreshold: i64(-1)}, {Labels: []string{"label"}}, {County: &county, Labels: []string{"bad,label"}}, {County: &county, Labels: []string{"same", "same"}}} {
		out, e := f.s.SetParkRules(ctx, bad, parkPass(store.OriginRules))
		contractError(t, e, store.ErrInvalid)
		contractEqual(t, out, store.Pass{})
	}
	badCounty := " bad"
	_, e = f.s.SetParkRules(ctx, store.ParkRulesEdit{County: &badCounty}, parkPass(store.OriginRules))
	contractError(t, e, store.ErrInvalid)
	total := f.rnd.(*contractRandom).total
	_, e = f.s.SetParkRules(ctx, store.ParkRulesEdit{Distance: i64(0)}, parkPass(store.OriginStart))
	contractError(t, e, store.ErrUnreachable)
	contractEqual(t, f.rnd.(*contractRandom).total, total)
	got, _, e = f.s.ParkRules(ctx)
	contractOK(t, e)
	contractEqual(t, got, r)
	_, e = f.s.FailPass(ctx, p.ID, store.ReasonScrapes)
	contractOK(t, e)
	county = "Chisago"
	p, e = f.s.SetParkRules(ctx, store.ParkRulesEdit{County: &county}, parkPass(store.OriginRules))
	contractOK(t, e)
	delete(r.Labels, county)
	got, cs, e = f.s.ParkRules(ctx)
	contractOK(t, e)
	contractEqual(t, got, r)
	contractEqual(t, slices.Contains(cs, county), false)
	_, e = f.s.FailPass(ctx, p.ID, store.ReasonScrapes)
	contractOK(t, e)
	p, e = f.s.SetParkRules(ctx, store.ParkRulesEdit{}, parkPass(store.OriginRules))
	contractOK(t, e)
	got, _, e = f.s.ParkRules(ctx)
	contractOK(t, e)
	contractEqual(t, got, r)
	contractEqual(t, p.Kind, store.KindParks)
}

// R-W3VI-LKRE R-W7J7-QVZH R-W9Z0-IFGV R-6014-87Q9 R-WCET-9YY9 R-6B07-O5EI R-EP8R-Z5D9
func TestParkDerivationPersistenceAndReads(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	cl := parkCluster("b", "Town", "county", "55056", 200)
	q := cl.Parcels[0]
	q.PIN = "a"
	q.BuildingValue = nil
	q.Owners = []string{"Owner One", "Owner Two"}
	cl.Parcels = append(cl.Parcels, q)
	q.PIN = "c"
	q.BuildingValue = i64(200)
	cl.Parcels = append(cl.Parcels, q)
	cl.BuildingValue = 400
	pass := applyParkPlan(t, f, parks.Plan{News: []parks.New{{Cluster: cl, Number: 1}}})
	contractEqual(t, pass.ParcelRun, "parcel-run")
	contractEqual(t, pass.ParcelsRead, int64(4))
	contractEqual(t, pass.ByLabel, int64(1))
	contractEqual(t, pass.ByValue, int64(2))
	contractEqual(t, pass.ByCandidate, int64(1))
	contractEqual(t, pass.ParksCreated, int64(1))
	prior, e := f.s.PriorParks(ctx)
	contractOK(t, e)
	contractEqual(t, len(prior), 1)
	id := prior[0].ID
	contractEqual(t, prior[0].PINs, []string{"a", "b", "c"})
	p, e := f.s.GetPark(ctx, id)
	contractOK(t, e)
	contractEqual(t, p.Name, "Town")
	contractEqual(t, p.Minutes, (*int64)(nil))
	contractEqual(t, p.Number, int64(1))
	contractEqual(t, p.County, cl.County)
	contractEqual(t, p.Town, cl.Town)
	contractEqual(t, p.Zip, cl.Zip)
	contractEqual(t, p.Lat, cl.Lat)
	contractEqual(t, p.Lon, cl.Lon)
	contractEqual(t, p.Radius, cl.Radius)
	contractEqual(t, p.BuildingValue, cl.BuildingValue)
	contractEqual(t, p.Parcels, int64(3))
	contractEqual(t, p.GrownFrom, int64(0))
	contractEqual(t, p.State, parks.StateActive)
	contractEqual(t, p.Run, pass.ParcelRun)
	contractEqual(t, p.Created, f.now.UTC().Truncate(time.Second))
	contractEqual(t, p.Updated, p.Created)
	contractEqual(t, p.Walked, time.Time{})
	contractEqual(t, p.WalkRun, "")
	contractEqual(t, p.Found, int64(0))
	contractEqual(t, p.NewCandidates, int64(0))
	contractEqual(t, p.CeilingReached, false)
	contractEqual(t, store.ValidParkID(p.ID), true)
	parcels, e := f.s.ParkParcels(ctx, id)
	contractOK(t, e)
	contractEqual(t, []string{parcels[0].PIN, parcels[1].PIN, parcels[2].PIN}, []string{"b", "c", "a"})
	contractEqual(t, parcels[0].Owners, []string{})
	contractEqual(t, parcels[1].Owners, q.Owners)
	contractEqual(t, parcels[0].Lat, *cl.Parcels[0].Lat)
	contractEqual(t, parcels[0].Lon, *cl.Parcels[0].Lon)
	contractEqual(t, parcels[0].Rule, cl.Parcels[0].Rule)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55056": 42}))
	p, e = f.s.GetPark(ctx, id)
	contractOK(t, e)
	contractEqual(t, p.Minutes, i64(42))
	f.reopen()
	got, e := f.s.GetPark(ctx, id)
	contractOK(t, e)
	contractEqual(t, got, p)
	f.now = f.now.Add(time.Hour)
	cl.Town = "Changed town"
	cl.County = "Changed county"
	cl.Zip = "55092"
	cl.Parcels = cl.Parcels[:1]
	cl.Lat = 46
	cl.Lon = -94
	cl.Radius = 30
	cl.BuildingValue = 500
	parcelLat, parcelLon := 46.1, -94.1
	cl.Parcels[0] = parks.Parcel{PIN: "b", Street: "replacement street", City: "replacement city", Zip: "55092", County: cl.County, Lat: &parcelLat, Lon: &parcelLon, Owners: []string{"replacement owner", "second replacement owner"}, BuildingValue: i64(500), Rule: parks.RuleValue}
	applyParkPlan(t, f, parks.Plan{Updates: []parks.Update{{ID: id, Cluster: cl, GrownFrom: 2}}, Grown: 1})
	got, e = f.s.GetPark(ctx, id)
	contractOK(t, e)
	want := p
	want.Zip = "55092"
	want.Lat, want.Lon, want.Radius = 46, -94, 30
	want.Parcels, want.BuildingValue, want.GrownFrom = 1, 500, 2
	want.Minutes = nil
	want.Updated = f.now.UTC().Truncate(time.Second)
	contractEqual(t, got, want)
	parcels, e = f.s.ParkParcels(ctx, id)
	contractOK(t, e)
	contractEqual(t, parcels, []store.ParkParcel{{PIN: "b", Owners: []string{"replacement owner", "second replacement owner"}, Street: "replacement street", City: "replacement city", Zip: "55092", BuildingValue: i64(500), Rule: parks.RuleValue, Lat: 46.1, Lon: -94.1}})
	applyParkPlan(t, f, parks.Plan{Gone: []string{id}, News: []parks.New{{Cluster: parkCluster("new", "Town", "county", "", 1), Number: 2}}})
	got, e = f.s.GetPark(ctx, id)
	contractOK(t, e)
	contractEqual(t, got.State, parks.StateGone)
	contractEqual(t, got.Name, "Town 1")
	after, e := f.s.ParkParcels(ctx, id)
	contractOK(t, e)
	contractEqual(t, after, parcels)
	prior, e = f.s.PriorParks(ctx)
	contractOK(t, e)
	for _, q := range prior {
		if q.ID == id {
			contractEqual(t, q.PINs, []string{"b"})
			contractEqual(t, q.GrownFrom, int64(2))
			contractEqual(t, q.State, parks.StateGone)
		}
	}
	active, gone, e := f.s.CountParks(ctx)
	contractOK(t, e)
	contractEqual(t, []int64{active, gone}, []int64{1, 1})
	held, e := f.s.ParkCountiesHeld(ctx)
	contractOK(t, e)
	contractEqual(t, held, []string{"county"})
	read, e := f.s.ParkCountiesRead(ctx)
	contractOK(t, e)
	contractEqual(t, read, []string{"county"})
	run, e := f.s.ParksRun(ctx)
	contractOK(t, e)
	contractEqual(t, run, "parcel-run")
	applyParkPlan(t, f, parks.Plan{Updates: []parks.Update{{ID: id, Cluster: cl, GrownFrom: 2}}})
	got, e = f.s.GetPark(ctx, id)
	contractOK(t, e)
	contractEqual(t, got.State, parks.StateActive)
	applyParkPlan(t, f, parks.Plan{})
	for {
		n, e := f.s.PrunePasses(ctx, 1, 10)
		contractOK(t, e)
		if n == 0 {
			break
		}
	}
	run, e = f.s.ParksRun(ctx)
	contractOK(t, e)
	contractEqual(t, run, "parcel-run")
	f.reopen()
	run, e = f.s.ParksRun(ctx)
	contractOK(t, e)
	contractEqual(t, run, "parcel-run")
	_, e = f.s.GetPark(ctx, "missing")
	contractError(t, e, store.ErrNotFound)
	missing, e := f.s.ParkParcels(ctx, "missing")
	contractError(t, e, store.ErrNotFound)
	contractEqual(t, missing, []store.ParkParcel(nil))
}

// R-64WP-RAP1 R-664M-52FQ R-67CI-IU6F R-6JJI-CJLD R-TMFI-X33R R-TNNF-AUUG
func TestParkWindows(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	ps := seedParks(t, f)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55056": 12, "55092": 40}))
	for _, order := range []string{"", parks.SortMinutes, parks.SortValue, parks.SortParcels} {
		all, e := f.s.ListParks(ctx, store.ParkFilter{}, order, "", "", 100)
		contractOK(t, e)
		contractEqual(t, len(all), 3)
		if order == "" || order == parks.SortMinutes {
			contractEqual(t, all[0].Minutes, i64(12))
			contractEqual(t, all[1].Minutes, i64(40))
			contractEqual(t, all[2].Minutes, (*int64)(nil))
		}
		if order == parks.SortValue {
			contractEqual(t, []int64{all[0].BuildingValue, all[1].BuildingValue, all[2].BuildingValue}, []int64{300, 200, 100})
		}
		key := func(p store.Park) string {
			switch order {
			case parks.SortValue:
				return fmt.Sprint(p.BuildingValue)
			case parks.SortParcels:
				return fmt.Sprint(p.Parcels)
			default:
				if p.Minutes == nil {
					return ""
				}
				return fmt.Sprint(*p.Minutes)
			}
		}
		got := []store.Park{}
		k, id := "", ""
		for {
			page, e := f.s.ListParks(ctx, store.ParkFilter{}, order, k, id, 2)
			contractOK(t, e)
			if len(page) == 0 {
				break
			}
			got = append(got, page...)
			edge := page[len(page)-1]
			k, id = key(edge), edge.ID
		}
		contractEqual(t, got, all)
		reverse, e := f.s.ListParksReversed(ctx, store.ParkFilter{}, order, "ignored", "", 100)
		contractOK(t, e)
		slices.Reverse(reverse)
		contractEqual(t, reverse, all)
		for i, p := range all {
			page, e := f.s.ListParksReversed(ctx, store.ParkFilter{}, order, key(p), p.ID, 100)
			contractOK(t, e)
			want := slices.Clone(all[:i])
			slices.Reverse(want)
			contractEqual(t, page, want)
		}
		edge := all[1]
		missingID := edge.ID + "z"
		forward, e := f.s.ListParks(ctx, store.ParkFilter{}, order, key(edge), missingID, 100)
		contractOK(t, e)
		contractEqual(t, forward, all[2:])
		back, e := f.s.ListParksReversed(ctx, store.ParkFilter{}, order, key(edge), missingID, 100)
		contractOK(t, e)
		want := slices.Clone(all[:2])
		slices.Reverse(want)
		contractEqual(t, back, want)
	}
	applyParkPlan(t, f, parks.Plan{Gone: []string{ps[0].ID}})
	for _, tc := range []struct {
		f store.ParkFilter
		n int
	}{{store.ParkFilter{}, 2}, {store.ParkFilter{Walked: parks.WalkedUnwalked}, 2}, {store.ParkFilter{Walked: parks.WalkedWalked}, 0}, {store.ParkFilter{Gone: parks.GoneGone}, 1}, {store.ParkFilter{Gone: parks.GoneAll}, 3}, {store.ParkFilter{County: "other", Gone: parks.GoneAll}, 1}, {store.ParkFilter{County: "missing"}, 0}} {
		got, e := f.s.ListParks(ctx, tc.f, "", "", "", 100)
		contractOK(t, e)
		contractEqual(t, len(got), tc.n)
	}
	for _, reverse := range []bool{false, true} {
		call := f.s.ListParks
		if reverse {
			call = f.s.ListParksReversed
		}
		for _, tc := range []struct {
			f        store.ParkFilter
			s, k, id string
			n        int
		}{{n: 0}, {n: 1, s: "bad"}, {n: 1, f: store.ParkFilter{Walked: "bad"}}, {n: 1, f: store.ParkFilter{Gone: "bad"}}, {n: 1, k: "12.5", id: "id"}, {n: 1, s: parks.SortValue, id: "id"}, {n: 1, s: parks.SortParcels, id: "id"}} {
			out, e := call(ctx, tc.f, tc.s, tc.k, tc.id, tc.n)
			contractError(t, e, store.ErrUnreachable)
			contractEqual(t, out, []store.Park(nil))
		}
		for _, s := range []string{"", parks.SortMinutes, parks.SortValue, parks.SortParcels} {
			_, e := call(ctx, store.ParkFilter{}, s, "12", "id", 1)
			contractOK(t, e)
		}
		_, e := call(ctx, store.ParkFilter{}, "", "bad", "", 1)
		contractOK(t, e)
	}
}

// R-62GW-ZR7N R-WDMP-NQOY R-56RJ-1PXL
func TestApplyParksRefusalsAndAtomicMint(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	ps := seedParks(t, f)
	p, e := f.s.AddPass(ctx, parkPass(store.OriginStart))
	contractOK(t, e)
	before, e := f.s.PriorParks(ctx)
	contractOK(t, e)
	cl := parkCluster("new", "Town", "county", "", 1)
	badPoint := cl
	badPoint.Parcels = slices.Clone(cl.Parcels)
	badPoint.Parcels[0].Lat = nil
	badRule := cl
	badRule.Parcels = slices.Clone(cl.Parcels)
	badRule.Parcels[0].Rule = "bad"
	duplicate := cl
	duplicate.Parcels = append(slices.Clone(cl.Parcels), cl.Parcels[0])
	for _, tc := range []struct {
		run  string
		plan parks.Plan
		c    store.Counts
	}{{plan: parks.Plan{}}, {run: "run", c: store.Counts{ParksGrown: -1}}, {run: "run", plan: parks.Plan{News: []parks.New{{Cluster: badPoint, Number: 1}}}}, {run: "run", plan: parks.Plan{News: []parks.New{{Cluster: badRule, Number: 1}}}}, {run: "run", plan: parks.Plan{News: []parks.New{{Cluster: duplicate, Number: 1}}}}, {run: "run", plan: parks.Plan{News: []parks.New{{Cluster: cl, Number: 0}}}}, {run: "run", plan: parks.Plan{Gone: []string{"missing"}}}, {run: "run", plan: parks.Plan{Updates: []parks.Update{{ID: "missing", Cluster: cl}}}}, {run: "run", plan: parks.Plan{Gone: []string{ps[0].ID, ps[0].ID}}}, {run: "run", plan: parks.Plan{Gone: []string{ps[0].ID}, Updates: []parks.Update{{ID: ps[0].ID, Cluster: cl}}}}} {
		for _, pass := range []string{p.ID, "missing"} {
			out, e := f.s.ApplyParks(ctx, pass, tc.run, nil, tc.plan, tc.c)
			contractError(t, e, store.ErrUnreachable)
			contractEqual(t, out, store.Pass{})
		}
		after, e := f.s.PriorParks(ctx)
		contractOK(t, e)
		contractEqual(t, after, before)
	}
	_, e = f.s.ApplyParks(ctx, "missing", "run", nil, parks.Plan{}, store.Counts{})
	contractError(t, e, store.ErrNotFound)
	ordinary := contractNewPass(t, f, "ordinary")
	_, e = f.s.ApplyParks(ctx, ordinary.ID, "run", nil, parks.Plan{}, store.Counts{})
	contractError(t, e, store.ErrNotFound)
	// A second park's exhausted attempts roll back the first park and all updates.
	meter := &contractMeter{reader: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 72))}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: meter})
	out, e := f.s.ApplyParks(ctx, p.ID, "run", nil, parks.Plan{Updates: []parks.Update{{ID: ps[0].ID, Cluster: cl}}, News: []parks.New{{Cluster: cl, Number: 2}, {Cluster: cl, Number: 3}}}, store.Counts{})
	contractError(t, e, store.ErrUnreachable)
	contractEqual(t, out, store.Pass{})
	contractEqual(t, meter.total, 72)
	contractEqual(t, meter.requests, []int{8, 8, 8, 8, 8, 8, 8, 8, 8})
	after, e := f.s.PriorParks(ctx)
	contractOK(t, e)
	contractEqual(t, after, before)
	held, e := f.s.GetPass(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, held, p)
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: io.LimitReader(bytes.NewReader(nil), 0)})
	_, e = f.s.ApplyParks(ctx, p.ID, "run", nil, parks.Plan{News: []parks.New{{Cluster: cl, Number: 2}}}, store.Counts{})
	contractError(t, e, store.ErrUnreachable)
	_, e = f.s.FailPass(ctx, p.ID, store.ReasonStore)
	contractOK(t, e)
	_, e = f.s.ApplyParks(ctx, p.ID, "run", nil, parks.Plan{}, store.Counts{})
	contractError(t, e, store.ErrEnded)
	after, e = f.s.PriorParks(ctx)
	contractOK(t, e)
	contractEqual(t, after, before)
}

// R-W2NM-7T0P R-OHI9-FACJ R-VP8Q-0BV2 R-P28J-XDYC R-VU4B-JETU R-VWK4-AYB8
func TestParkPassConcurrencyAndOrigins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Each ordinary kind can start beside parks, in either recording order.
	for _, parksFirst := range []bool{false, true} {
		f := contractDB(t)
		inputs := []store.Pass{
			{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"},
			{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "owner"},
			enrichPass(),
			targetedPass(store.CandidateIDPrefix+"0123456789abcdef", enrichers.Kinds()[0]),
		}
		if parksFirst {
			inputs = append([]store.Pass{parkPass(store.OriginStart)}, inputs...)
		} else {
			inputs = append(inputs, parkPass(store.OriginStart))
		}
		for _, input := range inputs {
			got, e := f.s.AddPass(ctx, input)
			contractOK(t, e)
			contractEqual(t, got.Kind, input.Kind)
		}
		running, e := f.s.RunningPasses(ctx)
		contractOK(t, e)
		contractEqual(t, len(running), len(inputs))
	}

	for _, first := range []string{store.OriginRun, store.OriginRules, store.OriginStart} {
		f := contractDB(t)
		p, e := f.s.AddPass(ctx, parkPass(first))
		contractOK(t, e)
		contractEqual(t, p.ParcelRun, "")
		for _, origin := range []string{store.OriginRun, store.OriginRules, store.OriginStart} {
			q := parkPass(origin)
			q.Trigger += "next"
			if origin == store.OriginRules {
				q.Trigger = store.TriggerParkRules
			}
			q.Delivery += "next"
			if origin != store.OriginRun {
				q.Delivery = ""
			}
			_, e = f.s.AddPass(ctx, q)
			contractError(t, e, store.ErrBusy)
		}
		ordinary, e := f.s.AddPass(ctx, enrichPass())
		contractOK(t, e)
		contractEqual(t, ordinary.Kind, store.KindEnrich)
		_, e = f.s.FailPass(ctx, p.ID, store.ReasonScrapes)
		contractOK(t, e)
		if first == store.OriginRun {
			q := parkPass(first)
			q.Delivery = "different"
			_, e = f.s.AddPass(ctx, q)
			contractError(t, e, store.ErrDelivered)
		}
	}
	f := contractDB(t)
	start := make(chan struct{})
	results := make(chan error, 8)
	winning := make(chan int64, 8)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			<-start
			var e error
			if i%2 == 0 {
				_, e = f.s.SetParkRules(ctx, store.ParkRulesEdit{Distance: i64(int64(800 + i))}, parkPass(store.OriginRules))
			} else {
				_, e = f.s.AddPass(ctx, parkPass(store.OriginStart))
			}
			if e == nil {
				distance := int64(0)
				if i%2 == 0 {
					distance = int64(800 + i)
				}
				winning <- distance
			}
			results <- e
		})
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else {
			contractError(t, e, store.ErrBusy)
		}
	}
	contractEqual(t, success, 1)
	winner := <-winning
	r, _, e := f.s.ParkRules(ctx)
	contractOK(t, e)
	wantDistance := int64(parks.SeedDistance)
	wantOrigin := store.OriginStart
	if winner != 0 {
		wantDistance = winner
		wantOrigin = store.OriginRules
	}
	contractEqual(t, r.Distance, wantDistance)
	running, e := f.s.RunningPasses(ctx)
	contractOK(t, e)
	contractEqual(t, len(running), 1)
	contractEqual(t, running[0].Origin, wantOrigin)
	f = contractDB(t)
	p, e := f.s.AddPass(ctx, parkPass(store.OriginStart))
	contractOK(t, e)
	start = make(chan struct{})
	results = make(chan error, 2)
	wg.Go(func() {
		<-start
		_, e := f.s.ApplyParks(ctx, p.ID, "run", nil, parks.Plan{News: []parks.New{{Cluster: parkCluster("pin", "Town", "county", "", 1), Number: 1}}}, store.Counts{})
		results <- e
	})
	wg.Go(func() { <-start; _, e := f.s.FailPass(ctx, p.ID, store.ReasonStore); results <- e })
	close(start)
	wg.Wait()
	close(results)
	success = 0
	for e := range results {
		if e == nil {
			success++
		} else {
			contractError(t, e, store.ErrEnded)
		}
	}
	contractEqual(t, success, 1)
	p, e = f.s.GetPass(ctx, p.ID)
	contractOK(t, e)
	a, _, e := f.s.CountParks(ctx)
	contractOK(t, e)
	if p.Status == store.StatusFailed {
		contractEqual(t, a, int64(0))
		contractEqual(t, p.ParcelRun, "")
	} else {
		contractEqual(t, a, int64(1))
		contractEqual(t, p.ParcelRun, "run")
	}
}

// R-5YT7-UFZK R-TOVB-OML5 R-5K6F-9738
func TestActiveAddressKeysPages(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	ids := seedBoundedCandidates(t, f, 5)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE candidates SET street=?,zip=CASE WHEN id=? THEN '' ELSE zip END,status=CASE WHEN id=? THEN ? ELSE status END", "1 Main", ids[1], ids[4], store.StatusMerged)
		return e
	}))
	keys, cursor, e := f.s.ActiveAddressKeys(ctx, "", 2)
	contractOK(t, e)
	contractEqual(t, keys, []string{parks.AddressKey("1 Main", "55072")})
	contractEqual(t, cursor, ids[1])
	keys, cursor, e = f.s.ActiveAddressKeys(ctx, cursor, 2)
	contractOK(t, e)
	contractEqual(t, keys, []string{parks.AddressKey("1 Main", "55072"), parks.AddressKey("1 Main", "55072")})
	contractEqual(t, cursor, ids[3])
	keys, cursor, e = f.s.ActiveAddressKeys(ctx, cursor, 2)
	contractOK(t, e)
	contractEqual(t, keys, []string{})
	contractEqual(t, cursor, "")
	keys, cursor, e = f.s.ActiveAddressKeys(ctx, "", 0)
	contractError(t, e, store.ErrUnreachable)
	contractEqual(t, keys, []string(nil))
	contractEqual(t, cursor, "")
}

func i64(n int64) *int64 { return &n }

// R-5SPP-XLA3
func TestParkStoreMethodSignatures(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	signature1 := struct {
		call func(context.Context) (parks.Rules, []string, error)
	}{f.s.ParkRules}
	if signature1.call == nil {
		t.Fatal("missing function")
	}
	signature2 := struct {
		call func(context.Context, store.ParkRulesEdit, store.Pass) (store.Pass, error)
	}{f.s.SetParkRules}
	if signature2.call == nil {
		t.Fatal("missing function")
	}
	signature3 := struct {
		call func(context.Context, string, int) ([]string, string, error)
	}{f.s.ActiveAddressKeys}
	if signature3.call == nil {
		t.Fatal("missing function")
	}
	signature4 := struct {
		call func(context.Context) ([]parks.Prior, error)
	}{f.s.PriorParks}
	if signature4.call == nil {
		t.Fatal("missing function")
	}
	signature5 := struct {
		call func(context.Context, string, string, []string, parks.Plan, store.Counts) (store.Pass, error)
	}{f.s.ApplyParks}
	if signature5.call == nil {
		t.Fatal("missing function")
	}
	signature6 := struct {
		call func(context.Context) ([]string, error)
	}{f.s.ParkCountiesRead}
	if signature6.call == nil {
		t.Fatal("missing function")
	}
	signature7 := struct {
		call func(context.Context, store.ParkFilter, string, string, string, int) ([]store.Park, error)
	}{f.s.ListParks}
	if signature7.call == nil {
		t.Fatal("missing function")
	}
	signature8 := struct {
		call func(context.Context, store.ParkFilter, string, string, string, int) ([]store.Park, error)
	}{f.s.ListParksReversed}
	if signature8.call == nil {
		t.Fatal("missing function")
	}
	signature9 := struct {
		call func(context.Context, string) (store.Park, error)
	}{f.s.GetPark}
	if signature9.call == nil {
		t.Fatal("missing function")
	}
	signature10 := struct {
		call func(context.Context, string) ([]store.ParkParcel, error)
	}{f.s.ParkParcels}
	if signature10.call == nil {
		t.Fatal("missing function")
	}
	signature11 := struct {
		call func(context.Context) (int64, int64, error)
	}{f.s.CountParks}
	if signature11.call == nil {
		t.Fatal("missing function")
	}
	signature12 := struct {
		call func(context.Context) ([]string, error)
	}{f.s.ParkCountiesHeld}
	if signature12.call == nil {
		t.Fatal("missing function")
	}
	signature13 := struct {
		call func(context.Context) (string, error)
	}{f.s.ParksRun}
	if signature13.call == nil {
		t.Fatal("missing function")
	}
}

// R-57ZF-FHOA
func TestEmptyParkStore(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	ps, e := f.s.ListParks(ctx, store.ParkFilter{}, "", "", "", 1)
	contractOK(t, e)
	contractEqual(t, ps, []store.Park{})
	ps, e = f.s.ListParksReversed(ctx, store.ParkFilter{}, "", "", "", 1)
	contractOK(t, e)
	contractEqual(t, ps, []store.Park{})
	prior, e := f.s.PriorParks(ctx)
	contractOK(t, e)
	contractEqual(t, prior, []parks.Prior{})
	for _, call := range []func(context.Context) ([]string, error){f.s.ParkCountiesRead, f.s.ParkCountiesHeld} {
		cs, e := call(ctx)
		contractOK(t, e)
		contractEqual(t, cs, []string{})
	}
	keys, cursor, e := f.s.ActiveAddressKeys(ctx, "", 1)
	contractOK(t, e)
	contractEqual(t, keys, []string{})
	contractEqual(t, cursor, "")
	run, e := f.s.ParksRun(ctx)
	contractOK(t, e)
	contractEqual(t, run, "")
	a, g, e := f.s.CountParks(ctx)
	contractOK(t, e)
	contractEqual(t, []int64{a, g}, []int64{0, 0})
	p, e := f.s.GetPark(ctx, "missing")
	contractError(t, e, store.ErrNotFound)
	contractEqual(t, p, store.Park{})
	parcels, e := f.s.ParkParcels(ctx, "missing")
	contractError(t, e, store.ErrNotFound)
	contractEqual(t, parcels, []store.ParkParcel(nil))
}

type heldParkState struct {
	Prior          []parks.Prior
	Parcels        map[string][]store.ParkParcel
	Rules          parks.Rules
	Counties, Read []string
	Run            string
}

func parkState(t *testing.T, f *contractFixture) heldParkState {
	t.Helper()
	ctx := context.Background()
	s := heldParkState{Parcels: map[string][]store.ParkParcel{}}
	var e error
	s.Prior, e = f.s.PriorParks(ctx)
	contractOK(t, e)
	for _, p := range s.Prior {
		s.Parcels[p.ID], e = f.s.ParkParcels(ctx, p.ID)
		contractOK(t, e)
	}
	s.Rules, s.Counties, e = f.s.ParkRules(ctx)
	contractOK(t, e)
	s.Read, e = f.s.ParkCountiesRead(ctx)
	contractOK(t, e)
	s.Run, e = f.s.ParksRun(ctx)
	contractOK(t, e)
	return s
}

// R-68HQ-QPWB
func TestParkStateSurvivesOtherWrites(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	seedParks(t, f)
	before := parkState(t, f)
	checks := []func() error{
		func() error {
			_, _, e := f.s.Attach(ctx, "new-run", false, []store.Observation{contractObservation("other-row", "Other business", f.now)})
			return e
		},
		func() error { _, _, e := f.s.MarkGone(ctx, "source-z", "no-run", "", 100); return e },
		func() error {
			_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: id, Why: "fixture", UserID: "owner"})
			return e
		},
		func() error { _, e := f.s.Judge(ctx, []string{id}); return e },
		func() error { _, _, e := f.s.JudgeAll(ctx, "", 100, f.now); return e },
		func() error {
			p, e := f.s.AddPass(ctx, enrichPass())
			if e != nil {
				return e
			}
			_, e = f.s.FinishPass(ctx, p.ID, store.Counts{Derived: 1})
			return e
		},
		func() error {
			p, e := f.s.AddPass(ctx, enrichPass())
			if e != nil {
				return e
			}
			_, e = f.s.FailPass(ctx, p.ID, store.ReasonStore)
			return e
		},
		func() error { return f.s.ReplaceZipDrive(ctx, map[string]int64{"55056": 12}) },
		func() error {
			_, e := f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id)})
			return e
		},
		func() error { _, _, e := f.s.Derive(ctx, enrichers.KindLinkFacts, "", true, 100); return e },
		func() error { _, e := f.s.DeriveAmong(ctx, enrichers.KindLinkFacts, []string{id}, true); return e },
		func() error { _, e := f.s.FillFacts(ctx, "", 100); return e },
		func() error {
			_, e := f.s.AddPage(ctx, store.Page{Candidate: id, URL: "https://kept.test/", UserID: "owner"})
			return e
		},
		func() error { return f.s.AddCampaignUse(ctx, id, "campaign", "", f.now) },
		func() error { return f.s.RejectCampaignUse(ctx, id, "campaign", "why", f.now) },
		func() error { return f.s.DeliverCampaignUse(ctx, id, "campaign", f.now) },
		func() error {
			p := contractNewPass(t, f, "newest")
			_, e := f.s.FinishPass(ctx, p.ID, store.Counts{})
			if e != nil {
				return e
			}
			for {
				n, e := f.s.PrunePasses(ctx, 1, 100)
				if e != nil {
					return e
				}
				if n == 0 {
					return nil
				}
			}
		},
	}
	for i, call := range checks {
		contractOK(t, call())
		t.Run(fmt.Sprint(i), func(t *testing.T) { contractEqual(t, parkState(t, f), before) })
	}
	f.reopen()
	contractEqual(t, parkState(t, f), before)
}

// R-KQVS-KMMF
func TestParkStoreCallsPreserveCandidateRecords(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, e := f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id)})
	contractOK(t, e)
	page, e := f.s.AddPage(ctx, store.Page{Candidate: id, URL: "https://kept.test/", Title: "Kept title", Text: "Kept body", UserID: "owner"})
	contractOK(t, e)
	list, e := f.s.CreateProspectList(ctx, store.ProspectList{Name: "Saved list", Size: 100, CreatedUserID: "owner"})
	contractOK(t, e)
	_, e = f.s.Reject(ctx, store.Reject{ProspectList: list.ID, Candidate: id, Kind: store.KindReject, Why: "reason", UserID: "owner"})
	contractOK(t, e)
	_, e = f.s.OpenProspectList(ctx, list.ID, "owner", "owner@test")
	contractOK(t, e)
	contractOK(t, f.s.AddCampaignUse(ctx, id, "campaign", list.ID, f.now))
	contractOK(t, f.s.RejectCampaignUse(ctx, id, "campaign", "reason", f.now))
	contractOK(t, f.s.DeliverCampaignUse(ctx, id, "campaign", f.now))
	snapshot := func() []any {
		basic := contractState(t, f)
		basic.passes = nil
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		pages, e := f.s.Pages(ctx, id)
		contractOK(t, e)
		body, e := f.s.GetPage(ctx, id, page.Number)
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
		return []any{basic, facts, pages, body, lists, read, rejects, opens, uses}
	}
	before := snapshot()
	seedParks(t, f)
	contractEqual(t, snapshot(), before)
	ps, e := f.s.ListParks(ctx, store.ParkFilter{}, "", "", "", 100)
	contractOK(t, e)
	for _, p := range ps {
		_, e = f.s.GetPark(ctx, p.ID)
		contractOK(t, e)
		_, e = f.s.ParkParcels(ctx, p.ID)
		contractOK(t, e)
	}
	_, e = f.s.ListParksReversed(ctx, store.ParkFilter{}, "", "", "", 100)
	contractOK(t, e)
	_, _, e = f.s.CountParks(ctx)
	contractOK(t, e)
	_, e = f.s.ParkCountiesHeld(ctx)
	contractOK(t, e)
	_, e = f.s.ParkCountiesRead(ctx)
	contractOK(t, e)
	_, e = f.s.ParksRun(ctx)
	contractOK(t, e)
	_, _, e = f.s.ParkRules(ctx)
	contractOK(t, e)
	_, _, e = f.s.ActiveAddressKeys(ctx, "", 100)
	contractOK(t, e)
	_, e = f.s.PriorParks(ctx)
	contractOK(t, e)
	p, e := f.s.SetParkRules(ctx, store.ParkRulesEdit{Distance: i64(800)}, parkPass(store.OriginRules))
	contractOK(t, e)
	_, e = f.s.ApplyParks(ctx, p.ID, "other-parcel-run", []string{"county"}, parks.Plan{Gone: []string{ps[0].ID}}, store.Counts{ParksGone: 1})
	contractOK(t, e)
	contractEqual(t, snapshot(), before)
	passes, e := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, e)
	for _, p := range passes {
		contractEqual(t, p.Kind, store.KindParks)
	}
	f.reopen()
	contractEqual(t, snapshot(), before)
}

// R-TNNF-AUUG R-64WP-RAP1 R-664M-52FQ R-6JJI-CJLD
func TestParkWindowIndependentOrdersAndTies(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	values := []int64{100, 300, 200, 300, 100, 200}
	counts := []int{1, 3, 2, 2, 3, 1}
	zips := []string{"55056", "55056", "55092", "", "", "55092"}
	plan := parks.Plan{}
	for i := range 6 {
		cl := parkCluster(fmt.Sprint(i), fmt.Sprint(i), "county", zips[i], values[i])
		q := cl.Parcels[0]
		cl.Parcels = nil
		for j := range counts[i] {
			q.PIN = fmt.Sprintf("%d-%d", i, j)
			cl.Parcels = append(cl.Parcels, q)
		}
		plan.News = append(plan.News, parks.New{Cluster: cl, Number: 1})
	}
	applyParkPlan(t, f, plan)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55056": 12, "55092": 40}))
	prior, e := f.s.PriorParks(ctx)
	contractOK(t, e)
	ids := []string{}
	for _, p := range prior {
		ids = append(ids, p.ID)
	}
	for _, tc := range []struct {
		order   string
		indices []int
	}{{parks.SortMinutes, []int{0, 1, 2, 5, 3, 4}}, {parks.SortValue, []int{1, 3, 2, 5, 0, 4}}, {parks.SortParcels, []int{1, 4, 2, 3, 0, 5}}} {
		want := []string{}
		for _, i := range tc.indices {
			want = append(want, ids[i])
		}
		all, e := f.s.ListParks(ctx, store.ParkFilter{}, tc.order, "", "", 100)
		contractOK(t, e)
		got := []string{}
		for _, p := range all {
			got = append(got, p.ID)
		}
		contractEqual(t, got, want)
		for edge, p := range all {
			key := ""
			switch tc.order {
			case parks.SortMinutes:
				if p.Minutes != nil {
					key = fmt.Sprint(*p.Minutes)
				}
			case parks.SortValue:
				key = fmt.Sprint(p.BuildingValue)
			case parks.SortParcels:
				key = fmt.Sprint(p.Parcels)
			}
			forward, e := f.s.ListParks(ctx, store.ParkFilter{}, tc.order, key, p.ID, 2)
			contractOK(t, e)
			got = []string{}
			for _, p := range forward {
				got = append(got, p.ID)
			}
			contractEqual(t, got, want[edge+1:min(edge+3, len(want))])
			reverse, e := f.s.ListParksReversed(ctx, store.ParkFilter{}, tc.order, key, p.ID, 2)
			contractOK(t, e)
			got = []string{}
			for _, p := range reverse {
				got = append(got, p.ID)
			}
			expected := slices.Clone(want[max(0, edge-2):edge])
			slices.Reverse(expected)
			contractEqual(t, got, expected)
		}
	}
}

package tools_test

import (
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-10QH-FGWX R-0YAO-NXFJ
func TestCandidateWebsiteFollowsPlaceAndShowsCheckOrigin(t *testing.T) {
	f := newFixture(t)
	park := seedParks(t, f, 1)[0]
	var err error
	place := places.Place{ID: "source-place", Name: "Chosen shop", Address: "42 Main, Town, MN 55001, USA", Website: "https://shop.test/"}
	sweep := func() string {
		t.Helper()
		pass, e := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: caller.UserID})
		if e != nil {
			t.Fatal(e)
		}
		_, err = f.st.RecordSweep(t.Context(), store.Sweep{Pass: pass.ID, Park: park.ID, Found: []places.Found{{Place: place}}, Circles: 1, Requests: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, e = f.st.FinishWalk(t.Context(), pass.ID, store.Counts{ParksWalked: 1, Circles: 1, Requests: 1, Found: 1}, false); e != nil {
			t.Fatal(e)
		}
		ps, e := f.st.ParkPlaces(t.Context(), park.ID)
		if e != nil || len(ps) == 0 {
			t.Fatal(ps, e)
		}
		return ps[0].Candidate
	}
	id := sweep()
	read := func() *tools.Facts {
		t.Helper()
		r := f.call("candidate", tools.CandidateArgs{Candidate: id})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		d := decodeList[tools.Detail](t, r)
		if d.Enrichments == nil || d.Enrichments.Facts == nil {
			t.Fatal(d)
		}
		return d.Enrichments.Facts
	}
	assert := func(source, state, url, checked string) {
		t.Helper()
		v := read()
		if v.WebsiteSource != source || v.Website != state || v.WebsiteURL != url || v.WebsiteCheckedURL != checked {
			t.Fatal(v)
		}
	}
	assert(store.WebsiteByPlaces, sources.WebsiteUnknown, "", "")
	add := func(from, landed string) {
		t.Helper()
		f.now = f.now.Add(time.Second)
		_, e := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: "shop.test", Inputs: website.Inputs(place.Website, ""), Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: landed, From: from})}})
		if e != nil {
			t.Fatal(e)
		}
	}
	add(place.Website, "https://www.shop.test/")
	assert(store.WebsiteByCheck, sources.WebsiteHTTPS, "https://www.shop.test/", place.Website)
	add(place.Website, place.Website)
	assert(store.WebsiteByCheck, sources.WebsiteHTTPS, place.Website, "")
	// A failed check of the old Places URL leaves a new URL unchecked.
	f.now = f.now.Add(time.Second)
	if _, err = f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: "shop.test", Inputs: website.Inputs(place.Website, ""), Found: encodedJSON(website.Result{State: sources.WebsiteNone})}}); err != nil {
		t.Fatal(err)
	}
	assert("", sources.WebsiteUnknown, "", "")
	// A changed Places URL must be decided from the records visible at this read.
	f.now = f.now.Add(time.Second)
	place.Website = "https://new-shop.test/"
	sweep()
	assert(store.WebsiteByPlaces, sources.WebsiteUnknown, "", "")
}

package store_test

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func enrichmentCandidate(t *testing.T, f *contractFixture, key, email string) string {
	t.Helper()
	obs := contractObservation(key, key, f.now.UTC().Truncate(time.Second))
	obs.Reading.PhoneKey = ""
	obs.Reading.DomainKey = ""
	obs.Reading.Email = email
	return contractAttach(t, f, "run-"+key, obs)[0]
}
func websiteFound(r website.Result) json.RawMessage {
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	return b
}
func websiteDecoded(e store.Enrichment) website.Result {
	var r website.Result
	if err := json.Unmarshal(e.Found, &r); err != nil {
		panic(err)
	}
	return r
}
func websiteResult(id, state string) store.Enrichment {
	return store.Enrichment{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "business.test", Source: "business.test", Found: websiteFound(website.Result{State: state, URL: "https://business.test/", StatusCode: 200}), Cost: .02}
}

// R-GDOT-3LA9 R-P56L-P5D0 R-X1YM-ROEQ R-GHCI-8WIC
func TestEnrichmentShapesAndMethods(t *testing.T) {
	e := websiteResult("candidate", sources.WebsiteHTTPS)
	contractEqual(t, store.Enrichment(struct {
		Candidate, Kind, Inputs, Source string
		Found                           json.RawMessage
		Cost                            float64
		At                              time.Time
	}(e)), e)
	d := store.Due{Candidate: "candidate", Inputs: "input"}
	contractEqual(t, store.Due(struct{ Candidate, Inputs string }(d)), d)
	f := store.Facts{Candidate: "candidate", Written: []store.Written{{Kind: sources.WebsiteCheck, At: time.Time{}}}}
	contractEqual(t, store.Facts(struct {
		Candidate                          string
		KnownSince                         time.Time
		GovernmentMatch                    string
		NewestFiling                       time.Time
		Categories                         []sources.Category
		Records                            []sources.Category
		FleetVerdict                       string
		PowerUnits, Drivers                *int64
		Website, WebsiteURL, WebsiteSource string
		Minutes                            *int64
		Brief                              string
		Written                            []store.Written
	}(f)), f)
	contractEqual(t, store.Written(struct {
		Kind string
		At   time.Time
	}(f.Written[0])), f.Written[0])
	st := contractDB(t).s
	checkMethods := func(
		add func(context.Context, []store.Enrichment) ([]store.Enrichment, error),
		due func(context.Context, string, string, int) ([]store.Due, error),
		derive func(context.Context, string, string, bool, int) (string, int64, error),
	) {
		if add == nil || due == nil || derive == nil {
			t.Fatal("missing method")
		}
	}
	checkMethods(st.AddEnrichments, st.DueEnrichments, st.Derive)
}

// R-X9A1-2AUW R-GR3P-B2FW R-GSBL-OU6L R-GX77-7X5D R-Q3GO-6J5N R-Q74D-BUDQ
func TestEnrichmentAppendAndMark(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, err := f.s.GetFacts(ctx, id)
	contractError(t, err, store.ErrNotFound)
	mark, err := f.s.EnrichmentMark(ctx)
	contractOK(t, err)
	contractEqual(t, mark, int64(0))
	e := websiteResult(id, sources.WebsiteHTTPS)
	e.Found = append([]byte("  "), e.Found...)
	got, err := f.s.AddEnrichments(ctx, []store.Enrichment{e, websiteResult("missing", sources.WebsiteNone)})
	contractOK(t, err)
	contractEqual(t, len(got), 1)
	e = websiteResult(id, sources.WebsiteHTTPS)
	e.At = f.now.UTC().Truncate(time.Second)
	contractEqual(t, got[0], e)
	found, err := f.s.EnrichmentAfter(ctx, e.Kind, e.Inputs, mark)
	contractOK(t, err)
	contractEqual(t, found, e)
	mark, err = f.s.EnrichmentMark(ctx)
	contractOK(t, err)
	_, err = f.s.EnrichmentAfter(ctx, e.Kind, e.Inputs, mark)
	contractError(t, err, store.ErrNotFound)
	_, err = f.s.EnrichmentAfter(ctx, e.Kind, e.Inputs, -1)
	contractError(t, err, store.ErrUnreachable)
	f.reopen()
	es, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	contractEqual(t, es, []store.Enrichment{e})
	facts, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, facts.Website, sources.WebsiteHTTPS)
	empty, err := f.s.AddEnrichments(ctx, nil)
	contractOK(t, err)
	contractEqual(t, empty, []store.Enrichment{})
	_, err = f.s.Enrichments(ctx, "missing")
	contractError(t, err, store.ErrNotFound)
}

// R-OMW3-YL8L
func TestInvalidEnrichmentsAreAtomic(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	good := websiteResult(id, sources.WebsiteHTTPS)
	bad := []store.Enrichment{good, good, good, good, good, good, good}
	bad[0].Kind = "bad"
	bad[1].Found = json.RawMessage(`{}`)
	bad[2].Inputs = ""
	bad[3].Cost = -1
	bad[4].Cost = math.NaN()
	bad[5].Cost = math.Inf(1)
	bad[6].Found = json.RawMessage(`null`)
	for _, b := range bad {
		out, err := f.s.AddEnrichments(ctx, []store.Enrichment{good, b})
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, out, []store.Enrichment(nil))
		es, err := f.s.Enrichments(ctx, id)
		contractOK(t, err)
		contractEqual(t, len(es), 0)
	}
}

// R-OO40-CCZA R-P6EI-2X3P R-O45V-JGI2 R-JB0U-TP20
func TestDueUsesInputs(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := enrichmentCandidate(t, f, "due", "office@business.test")
	ds, err := f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 1)
	contractOK(t, err)
	contractEqual(t, ds, []store.Due{{Candidate: id, Inputs: "business.test"}})
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
	contractOK(t, err)
	f.now = f.now.AddDate(10, 0, 0)
	ds, err = f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 1)
	contractOK(t, err)
	contractEqual(t, ds, []store.Due{})
	_, err = f.s.DueEnrichments(ctx, "invalid", "", 1)
	contractError(t, err, store.ErrUnreachable)
	_, err = f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 0)
	contractError(t, err, store.ErrUnreachable)
}

// R-XAHX-G2LL R-OPBW-Q4PZ R-M10O-KUCX R-X5MB-WZMT R-XCXQ-7M2Z
func TestDeriveFactsAndFill(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	c, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{c.Zip: 84}))
	last, n, err := f.s.Derive(ctx, enrichers.KindDriveMinutes, "", false, 1)
	contractOK(t, err)
	contractEqual(t, last, id)
	contractEqual(t, n, int64(1))
	facts, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, *facts.Minutes, int64(84))
	contractEqual(t, facts.Website, sources.WebsiteUnknown)
	_, n, err = f.s.Derive(ctx, enrichers.KindDriveMinutes, "", false, 1)
	contractOK(t, err)
	contractEqual(t, n, int64(0))
	_, n, err = f.s.Derive(ctx, enrichers.KindDriveMinutes, "", true, 1)
	contractOK(t, err)
	contractEqual(t, n, int64(1))
	last, n, err = f.s.Derive(ctx, enrichers.KindDriveMinutes, id, false, 1)
	contractOK(t, err)
	contractEqual(t, last, "")
	contractEqual(t, n, int64(0))
	_, _, err = f.s.Derive(ctx, sources.WebsiteCheck, "", false, 1)
	contractError(t, err, store.ErrUnreachable)
	_, _, err = f.s.Derive(ctx, enrichers.KindDriveMinutes, "", false, 0)
	contractError(t, err, store.ErrUnreachable)
	_, _, err = f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 10)
	contractOK(t, err)
	before, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractOK(t, f.s.FillFacts(ctx))
	after, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, after, before)
	f.reopen()
	after, err = f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, after, before)
}

func deriveAll(t *testing.T, s *store.Store) {
	t.Helper()
	for _, kind := range []string{enrichers.KindDriveMinutes, enrichers.KindLinkFacts} {
		_, _, err := s.Derive(context.Background(), kind, "", true, 1000)
		contractOK(t, err)
	}
}

func onlyWebsite(es []store.Enrichment) []store.Enrichment {
	out := []store.Enrichment{}
	for _, e := range es {
		if e.Kind == sources.WebsiteCheck {
			out = append(out, e)
		}
	}
	return out
}

// R-P6EI-2X3P R-OO40-CCZA R-XAHX-G2LL R-M10O-KUCX
func TestDueWindowsAndDerivedSubject(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := []string{}
	for _, key := range []string{"a", "b", "c"} {
		ids = append(ids, enrichmentCandidate(t, f, key, "office@"+key+".test"))
	}
	all := []store.Due{}
	cursor := ""
	for {
		ds, err := f.s.DueEnrichments(ctx, sources.WebsiteCheck, cursor, 2)
		contractOK(t, err)
		if len(ds) == 0 {
			break
		}
		all = append(all, ds...)
		cursor = ds[len(ds)-1].Candidate
	}
	contractEqual(t, len(all), 3)
	for i := 1; i < len(all); i++ {
		if all[i-1].Candidate >= all[i].Candidate {
			t.Fatal("window ordering")
		}
	}
	for _, d := range all {
		e := websiteResult(d.Candidate, sources.WebsiteNone)
		e.Inputs = d.Inputs
		_, err := f.s.AddEnrichments(ctx, []store.Enrichment{e})
		contractOK(t, err)
	}
	ds, err := f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 10)
	contractOK(t, err)
	contractEqual(t, ds, []store.Due{})
	// The next result's inputs are intentionally obsolete, so only this candidate is due.
	e := websiteResult(ids[0], sources.WebsiteNone)
	e.Inputs = "previous.test"
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{e})
	contractOK(t, err)
	ds, err = f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 10)
	contractOK(t, err)
	contractEqual(t, len(ds), 1)
	contractEqual(t, ds[0].Candidate, ids[0])
	// Compare the store's derived result to the pure function over the complete public subject.
	c, err := f.s.GetCandidate(ctx, ids[0])
	contractOK(t, err)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{c.Zip: 0}))
	_, _, err = f.s.MarkGone(ctx, "source-z", "later")
	contractOK(t, err)
	links, err := f.s.Links(ctx, c.ID)
	contractOK(t, err)
	su := enrichers.Subject{ID: c.ID, Name: c.Name, City: c.City, State: c.State, Email: c.Email, Zip: c.Zip, Minutes: ptrInt(0), Links: []enrichers.Link{}}
	for _, l := range links {
		su.Links = append(su.Links, enrichers.Link{Scraper: l.Scraper, SourceID: l.SourceID, Gone: l.Gone, Reading: l.Reading})
	}
	for _, der := range []enrichers.Deriver{enrichers.DriveMinutes{}, enrichers.LinkFacts{}} {
		_, _, err = f.s.Derive(ctx, der.Kind(), "", true, 10)
		contractOK(t, err)
		es, err := f.s.Enrichments(ctx, c.ID)
		contractOK(t, err)
		var got store.Enrichment
		for _, e := range es {
			if e.Kind == der.Kind() {
				got = e
				break
			}
		}
		expected := der.Derive(su)
		contractEqual(t, got.Inputs, der.Inputs(su))
		contractEqual(t, got.Found, expected.Found)
		contractEqual(t, got.Source, expected.Source)
		contractEqual(t, got.Cost, expected.Cost)
	}
}

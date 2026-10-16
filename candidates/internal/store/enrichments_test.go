package store_test

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-IYTU-ZZN2 R-J01R-DRDR
func TestEnrichmentShapeAndMethods(t *testing.T) {
	e := store.Enrichment{Candidate: "candidate", Kind: sources.WebsiteCheck, State: sources.WebsiteHTTP, URL: "http://business.test/", Title: "Business", StatusCode: 200, Size: 15, Error: "failure", Source: "business.test", Cost: 0.25, At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	shape := struct {
		Candidate, Kind, State, URL, Title string
		StatusCode                         int
		Size                               int64
		Error, Source                      string
		Cost                               float64
		At                                 time.Time
	}(e)
	contractEqual(t, store.Enrichment(shape), e)
	f := contractDB(t)
	methods := struct {
		add   func(context.Context, []store.Enrichment) ([]store.Enrichment, error)
		list  func(context.Context, string) ([]store.Enrichment, error)
		due   func(context.Context, string, string, int64, int64, int) ([]store.Candidate, error)
		mark  func(context.Context) (int64, error)
		after func(context.Context, string, string, int64) (store.Enrichment, error)
	}{f.s.AddEnrichments, f.s.Enrichments, f.s.DueEnrichments, f.s.EnrichmentMark, f.s.EnrichmentAfter}
	if methods.add == nil || methods.list == nil || methods.due == nil || methods.mark == nil || methods.after == nil {
		t.Fatal("missing method")
	}
}

func enrichmentCandidate(t *testing.T, f *contractFixture, key, email string) string {
	t.Helper()
	obs := contractObservation(key, key, f.now.UTC().Truncate(time.Second))
	obs.Reading.PhoneKey = ""
	obs.Reading.DomainKey = ""
	obs.Reading.Email = email
	return contractAttach(t, f, "run-"+key, obs)[0]
}
func websiteResult(id, state string) store.Enrichment {
	return store.Enrichment{Candidate: id, Kind: sources.WebsiteCheck, State: state, URL: "https://business.test/", Title: "Business", StatusCode: 200, Size: 100, Source: "business.test", Cost: 0.02}
}

// R-3VR0-VQ0M R-J7D5-ODTX R-ITY9-GWOA
func TestEnrichmentAppendPersistsAndSkipsAbsentCandidates(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	active := contractSeed(t, f)
	other := enrichmentCandidate(t, f, "distinct-other", "office@other.test")
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: active, Other: other, UserID: "owner", Why: "verified merge"})
	contractOK(t, err)
	before := contractState(t, f)
	first := websiteResult(active, sources.WebsiteHTTPS)
	first.At = f.now.AddDate(-1, 0, 0)
	first.URL = "https://business.test/\xff"
	first.Title = "title\xfe"
	first.Error = "error\xff\xfe"
	last := websiteResult(active, sources.WebsiteNone)
	last.URL = ""
	last.Title = ""
	last.StatusCode = 0
	inputs := []store.Enrichment{first, websiteResult(other, sources.WebsiteHTTP), websiteResult("missing", sources.WebsiteNone), last}
	reads := 0
	f.s = store.New(f.d, store.Config{Now: func() time.Time { reads++; return f.now }, Rand: f.rnd})
	got, err := f.s.AddEnrichments(ctx, inputs)
	contractOK(t, err)
	contractEqual(t, reads, 1)
	first.At = f.now.UTC().Truncate(time.Second)
	first.URL = string([]rune(first.URL))
	first.Title = string([]rune(first.Title))
	first.Error = string([]rune(first.Error))
	last.At = first.At
	contractEqual(t, got, []store.Enrichment{first, last})
	after := contractState(t, f)
	after.enrichments = before.enrichments
	contractEqual(t, after, before)
	f.reopen()
	for _, id := range []string{active, other} {
		es, e := f.s.Enrichments(ctx, id)
		contractOK(t, e)
		want := []store.Enrichment{}
		if id == active {
			want = []store.Enrichment{last, first}
		}
		contractEqual(t, es, want)
		for _, result := range es {
			if result.At.Location() != time.UTC || result.At.Nanosecond() != 0 {
				t.Fatal(result.At)
			}
		}
	}
	empty, err := f.s.AddEnrichments(ctx, nil)
	contractOK(t, err)
	contractEqual(t, empty, []store.Enrichment{})
	empty, err = f.s.Enrichments(ctx, "missing")
	contractError(t, err, store.ErrNotFound)
	if empty != nil {
		t.Fatal(empty)
	}
}

// R-3WYX-9HRB
func TestEnrichmentInvalidBatchChangesNothing(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := enrichmentCandidate(t, f, "business", "mail@business.test")
	good := websiteResult(id, sources.WebsiteHTTPS)
	saved, err := f.s.AddEnrichments(ctx, []store.Enrichment{good})
	contractOK(t, err)
	bad := []store.Enrichment{good, good, good, good, good, good, good, good}
	bad[0].Kind = "other"
	bad[1].State = sources.WebsiteUnknown
	bad[2].StatusCode = -1
	bad[3].Size = -1
	bad[4].Cost = -0.1
	bad[5].Cost = math.NaN()
	bad[6].Cost = math.Inf(1)
	bad[7].Cost = math.Inf(-1)
	for _, e := range bad {
		out, e2 := f.s.AddEnrichments(ctx, []store.Enrichment{good, e, good})
		contractError(t, e2, store.ErrUnreachable)
		if out != nil {
			t.Fatal(out)
		}
		got, e2 := f.s.Enrichments(ctx, id)
		contractOK(t, e2)
		contractEqual(t, got, saved)
	}
	f.reopen()
	got, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	contractEqual(t, got, saved)
}

// R-3Y6T-N9I0
func TestEnrichmentMarksFollowRecordingOrderAcrossMerge(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := enrichmentCandidate(t, f, "first", "mail@first.test")
	b := enrichmentCandidate(t, f, "second", "mail@second.test")
	zero, err := f.s.EnrichmentMark(ctx)
	contractOK(t, err)
	contractEqual(t, zero, int64(0))
	absent, err := f.s.EnrichmentAfter(ctx, sources.WebsiteCheck, "business.test", zero)
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, absent, store.Enrichment{})
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(a, sources.WebsiteHTTPS)})
	contractOK(t, err)
	mark, err := f.s.EnrichmentMark(ctx)
	contractOK(t, err)
	if mark < 0 {
		t.Fatal(mark)
	}
	absent, err = f.s.EnrichmentAfter(ctx, sources.WebsiteCheck, "business.test", mark)
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, absent, store.Enrichment{})
	// Keep the same clock: the later recording, rather than At, defines newest.
	added, err := f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(b, sources.WebsiteHTTP), websiteResult(a, sources.WebsiteNone)})
	contractOK(t, err)
	got, err := f.s.EnrichmentAfter(ctx, sources.WebsiteCheck, "business.test", mark)
	contractOK(t, err)
	contractEqual(t, got, added[1])
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: b, Other: a, UserID: "owner", Why: "verified merge"})
	contractOK(t, err)
	got, err = f.s.EnrichmentAfter(ctx, sources.WebsiteCheck, "business.test", mark)
	contractOK(t, err)
	want := added[1]
	want.Candidate = b
	contractEqual(t, got, want)
	for _, args := range []struct {
		kind, domain string
		mark         int64
	}{{"other", "business.test", zero}, {sources.WebsiteCheck, "other.test", zero}, {sources.WebsiteCheck, "business.test", math.MaxInt64}} {
		got, err = f.s.EnrichmentAfter(ctx, args.kind, args.domain, args.mark)
		contractError(t, err, store.ErrNotFound)
		contractEqual(t, got, store.Enrichment{})
	}
	got, err = f.s.EnrichmentAfter(ctx, sources.WebsiteCheck, "business.test", -1)
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, got, store.Enrichment{})
	f.reopen()
	got, err = f.s.EnrichmentAfter(ctx, sources.WebsiteCheck, "business.test", mark)
	contractOK(t, err)
	contractEqual(t, got, want)
}

// R-J8L2-25KM R-J9SY-FXBB R-JB0U-TP20
func TestDueEnrichmentWindowsAndAges(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	now := f.now
	var dueIDs []string
	var allIDs []string
	for i, tc := range []struct {
		state string
		age   int
		due   bool
	}{{sources.WebsiteHTTPS, 91, true}, {sources.WebsiteHTTPS, 90, true}, {sources.WebsiteHTTPS, 89, false}, {sources.WebsiteHTTP, 90, true}, {sources.WebsiteNone, 31, true}, {sources.WebsiteNone, 30, true}, {sources.WebsiteNone, 29, false}} {
		f.now = now
		id := enrichmentCandidate(t, f, string(rune('a'+i)), "mail@business.test")
		allIDs = append(allIDs, id)
		f.now = now.AddDate(0, 0, -tc.age)
		_, err := f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, tc.state)})
		contractOK(t, err)
		if tc.due {
			dueIDs = append(dueIDs, id)
		}
	}
	f.now = now
	never := enrichmentCandidate(t, f, "never", "mail@business.test")
	dueIDs = append(dueIDs, never)
	allIDs = append(allIDs, never)
	_ = enrichmentCandidate(t, f, "empty-email", "")
	merged := enrichmentCandidate(t, f, "merged", "mail@business.test")
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: never, Other: merged, UserID: "owner", Why: "verified merge"})
	contractOK(t, err)
	slices.Sort(dueIDs)
	slices.Sort(allIDs)
	read := func(live, none int64, n int) []string {
		t.Helper()
		var ids []string
		after := ""
		for {
			cs, e := f.s.DueEnrichments(ctx, sources.WebsiteCheck, after, live, none, n)
			contractOK(t, e)
			if cs == nil {
				t.Fatal("nil due slice")
			}
			if len(cs) == 0 {
				break
			}
			for _, c := range cs {
				want, e2 := f.s.GetCandidate(ctx, c.ID)
				contractOK(t, e2)
				contractEqual(t, c, want)
				ids = append(ids, c.ID)
			}
			after = cs[len(cs)-1].ID
		}
		return ids
	}
	contractEqual(t, read(90, 30, 2), dueIDs)
	contractEqual(t, read(0, 0, 3), allIDs)
	contractEqual(t, read(math.MaxInt64, math.MaxInt64, 2), []string{never})
	contractEqual(t, read(math.MinInt64, math.MinInt64, 2), allIDs)
	// A new result at the very same time supersedes an old result by record order.
	f.now = now.AddDate(0, 0, -100)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(never, sources.WebsiteNone)})
	contractOK(t, err)
	f.now = now
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(never, sources.WebsiteHTTPS)})
	contractOK(t, err)
	filtered := read(90, 30, 2)
	if slices.Contains(filtered, never) {
		t.Fatal("older result overrode newest")
	}
	for _, n := range []int{0, -1} {
		cs, e := f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 90, 30, n)
		contractError(t, e, store.ErrUnreachable)
		if cs != nil {
			t.Fatal(cs)
		}
	}
	// A cutoff before year one cannot make a result due even when it
	// was recorded at that cutoff.
	f.now = time.Date(0, 12, 31, 0, 0, 0, 0, time.UTC)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(never, sources.WebsiteHTTPS)})
	contractOK(t, err)
	f.now = time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC)
	reads := 0
	f.s = store.New(f.d, store.Config{Now: func() time.Time { reads++; return f.now }, Rand: f.rnd})
	cs, err := f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 2, 2, 100)
	contractEqual(t, reads, 1)
	contractOK(t, err)
	contractEqual(t, cs, []store.Candidate{})
	cs, err = f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 1, 1, 100)
	contractOK(t, err)
	expected, err := f.s.GetCandidate(ctx, never)
	contractOK(t, err)
	contractEqual(t, cs, []store.Candidate{expected})
}

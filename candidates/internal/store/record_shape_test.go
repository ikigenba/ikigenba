package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-COAJ-JMQY R-CPIF-XEHN
func TestStoredRecordShapes(t *testing.T) {
	n, confidence := int64(3), 0.8
	a := store.Assessment{Candidate: "candidate", Operating: &confidence, OperatingKey: 800000, Evidence: []sources.Evidence{{Weight: confidence}}, RevenueLow: &n, RevenueHigh: &n, RevenueConfidence: &confidence, KnownSince: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), GovernmentMatch: "pattern", NewestFiling: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Categories: []sources.Category{{Name: sources.CategoryOther, Source: "source"}}, FleetVerdict: sources.FleetPass, PowerUnits: &n, Drivers: &n, Minutes: &n, Excluded: true, Rules: sources.Rules, Calculated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	shaped := struct {
		Candidate               string
		Operating               *float64
		OperatingKey            int64
		Evidence                []sources.Evidence
		RevenueLow, RevenueHigh *int64
		RevenueConfidence       *float64
		KnownSince              time.Time
		GovernmentMatch         string
		NewestFiling            time.Time
		Categories              []sources.Category
		FleetVerdict            string
		PowerUnits, Drivers     *int64
		Minutes                 *int64
		Excluded                bool
		Rules                   int64
		Calculated              time.Time
	}(a)
	contractEqual(t, store.Assessment(shaped), a)
	counts := store.Counts{Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9}
	contractEqual(t, store.Counts(struct{ Rows, New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged int64 }(counts)), counts)
	o := store.Observation{Scraper: "source", SourceID: "row", Digest: "digest", Reading: sources.Reading{Name: "business"}}
	contractEqual(t, store.Observation(struct {
		Scraper, SourceID, Digest string
		Reading                   sources.Reading
	}(o)), o)
	f := store.CandidateFilter{Name: "business", Zip: "12345", Phone: "phone"}
	contractEqual(t, store.CandidateFilter(struct{ Name, Zip, Phone string }(f)), f)
	r := store.Ranked{Candidate: store.Candidate{ID: "candidate"}, Assessment: a}
	contractEqual(t, store.Ranked(struct {
		Candidate  store.Candidate
		Assessment store.Assessment
	}(r)), r)
}

// R-CQQC-B68C
func TestStoreReadMethodSignatures(t *testing.T) {
	f := contractDB(t)
	methods := struct {
		zipWrite   func(context.Context, map[string]int64) error
		zipRead    func(context.Context, string) (int64, error)
		candidate  func(context.Context, string) (store.Candidate, error)
		links      func(context.Context, string) ([]store.Link, error)
		estimates  func(context.Context, string) ([]sources.Estimate, error)
		assessment func(context.Context, string) (store.Assessment, error)
		rulings    func(context.Context, string) ([]store.Ruling, error)
		window     func(context.Context, store.CandidateFilter, string, string, int) ([]store.Candidate, error)
	}{zipWrite: f.s.ReplaceZipDrive, zipRead: f.s.ZipMinutes, candidate: f.s.GetCandidate, links: f.s.Links, estimates: f.s.Estimates, assessment: f.s.GetAssessment, rulings: f.s.Rulings, window: f.s.ListCandidates}
	if methods.zipWrite == nil || methods.zipRead == nil || methods.candidate == nil || methods.links == nil || methods.estimates == nil || methods.assessment == nil || methods.rulings == nil || methods.window == nil {
		t.Fatal("missing store method")
	}

}

// R-CRY8-OXZ1
func TestForwardWindowsRefuseNonpositiveCounts(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	for _, n := range []int{0, -1, -100} {
		ps, err := f.s.ListPasses(ctx, "", "", n)
		contractError(t, err, store.ErrUnreachable)
		if ps != nil {
			t.Fatal(ps)
		}
		cs, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", n)
		contractError(t, err, store.ErrUnreachable)
		if cs != nil {
			t.Fatal(cs)
		}
		ks, err := f.s.ListCohorts(ctx, "", "", n)
		contractError(t, err, store.ErrUnreachable)
		if ks != nil {
			t.Fatal(ks)
		}
	}
}

// R-4CRF-R3RJ
func TestRulingRecordShape(t *testing.T) {
	r := store.Ruling{Kind: store.KindSplit, Candidate: "candidate", Other: "other", Keys: []string{"source:row"}, Why: "owner reason", UserID: "owner", UserEmail: "owner@example.test", At: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
	shaped := struct {
		Kind, Candidate, Other string
		Keys                   []string
		Why, UserID, UserEmail string
		At                     time.Time
	}(r)
	contractEqual(t, store.Ruling(shaped), r)
}

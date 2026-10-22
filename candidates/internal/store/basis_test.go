package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-KW90-K114 R-KZWP-PC97 R-XHXN-JN9Q
func TestBasisOfResults(t *testing.T) {
	type basisWord string
	var records basisWord = store.BasisRecords
	var partial basisWord = store.BasisPartial
	var enriched basisWord = store.BasisEnriched
	contractEqual(t, []basisWord{records, partial, enriched}, []basisWord{"records", "partial", "enriched"})
	contractEqual(t, []string{store.BasisRecords, store.BasisPartial, store.BasisEnriched}, []string{"records", "partial", "enriched"})
	basis := store.BasisOf
	brief := briefResult(t, "candidate-one", "operating business", false)
	failedBrief := briefResult(t, "candidate-two", "", true)
	route := basisRoute(t, ptrInt(84))
	noRoute := basisRoute(t, nil)
	cases := []struct {
		es   []store.Enrichment
		want string
	}{
		{nil, store.BasisRecords}, {[]store.Enrichment{}, store.BasisRecords},
		{[]store.Enrichment{{Kind: sources.WebsiteCheck}, {Kind: enrichers.KindLinkFacts}, {Kind: enrichers.KindDriveMinutes}, {Kind: "unrecognized"}}, store.BasisRecords},
		{[]store.Enrichment{{Kind: enrichers.KindWebsiteSearch, Found: json.RawMessage(`{"status":"failed"}`)}}, store.BasisPartial},
		{[]store.Enrichment{{Kind: enrichers.KindSiteText}}, store.BasisPartial},
		{[]store.Enrichment{brief}, store.BasisPartial}, {[]store.Enrichment{failedBrief}, store.BasisPartial},
		{[]store.Enrichment{route}, store.BasisPartial}, {[]store.Enrichment{noRoute}, store.BasisPartial},
		{[]store.Enrichment{route, failedBrief}, store.BasisPartial},
		{[]store.Enrichment{brief, noRoute}, store.BasisPartial},
		{[]store.Enrichment{brief, {Kind: enrichers.KindDriveRoute, Found: json.RawMessage(`{`)}}, store.BasisPartial},
		{[]store.Enrichment{brief, route}, store.BasisEnriched},
		{[]store.Enrichment{brief, basisRoute(t, ptrInt(0))}, store.BasisEnriched},
		{[]store.Enrichment{failedBrief, noRoute, brief, route}, store.BasisEnriched},
	}
	for i, tc := range cases {
		contractEqual(t, basis(tc.es), tc.want)
		reverse := append([]store.Enrichment{}, tc.es...)
		for j := range reverse {
			reverse[j].Candidate = "unrelated-candidate"
			reverse[j].Inputs = "unrelated-input"
			reverse[j].Source = "unrelated-source"
			reverse[j].Cost = 19
			reverse[j].At = time.Date(2020+i, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		for a, b := 0, len(reverse)-1; a < b; a, b = a+1, b-1 {
			reverse[a], reverse[b] = reverse[b], reverse[a]
		}
		contractEqual(t, basis(reverse), tc.want)
	}
}

func basisRoute(t *testing.T, minutes *int64) store.Enrichment {
	t.Helper()
	raw, err := json.Marshal(enrichers.RouteFound{Minutes: minutes, Why: "route outcome"})
	contractOK(t, err)
	return store.Enrichment{Kind: enrichers.KindDriveRoute, Inputs: "route-inputs", Source: enrichers.SourceGoogleRoutes, Found: raw}
}

// R-KZWP-PC97 R-XJ5J-XF0F
func TestKindsOfEverySubset(t *testing.T) {
	kinds := store.KindsOf
	all := []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief, enrichers.KindDriveRoute}
	for mask := 0; mask < 16; mask++ {
		es := []store.Enrichment{{Kind: sources.WebsiteCheck}, {Kind: enrichers.KindLinkFacts}, {Kind: enrichers.KindDriveMinutes}}
		wantPresent, wantMissing := []string{}, []string{}
		for i, k := range all {
			if mask&(1<<i) != 0 {
				wantPresent = append(wantPresent, k)
			} else {
				wantMissing = append(wantMissing, k)
			}
		}
		for i := len(all) - 1; i >= 0; i-- {
			if mask&(1<<i) != 0 {
				es = append(es, store.Enrichment{Kind: all[i], Found: json.RawMessage(`null`)}, store.Enrichment{Kind: all[i], Found: json.RawMessage(`{`)})
			}
		}
		present, missing := kinds(es)
		contractEqual(t, present, wantPresent)
		contractEqual(t, missing, wantMissing)
	}
	for _, es := range [][]store.Enrichment{nil, {}} {
		present, missing := kinds(es)
		contractEqual(t, present, []string{})
		contractEqual(t, missing, all)
	}
}

// R-KK20-QBM6
func TestBasisFiltersJSON(t *testing.T) {
	for _, tc := range []struct {
		f    store.ProspectListFilters
		want string
	}{
		{store.ProspectListFilters{}, `{}`}, {store.ProspectListFilters{Government: new(bool)}, `{"government":false}`},
		{store.ProspectListFilters{RevenueMin: ptrInt(5), Categories: []string{}}, `{"revenue_min":5}`},
		{store.ProspectListFilters{Website: []string{sources.WebsiteHTTPS, sources.WebsiteNone}, WebsiteUnknown: true}, `{"website":["https","none"],"website_unknown":true}`},
		{store.ProspectListFilters{MinCampaignAgeDays: ptrInt(0)}, `{"min_campaign_age_days":0}`},
		{store.ProspectListFilters{Basis: []string{store.BasisRecords, store.BasisEnriched}}, `{"basis":["records","enriched"]}`},
		{store.ProspectListFilters{Basis: []string{}}, `{}`},
	} {
		raw, err := json.Marshal(tc.f)
		contractOK(t, err)
		contractEqual(t, string(raw), tc.want)
	}
	f := store.ProspectListFilters{RevenueMin: ptrInt(1), RevenueMax: ptrInt(2), RevenueUnknown: true, MaxMinutes: ptrInt(3), MinutesUnknown: true, Categories: []string{sources.CategoryOther}, CategoryUnknown: true, Fleet: sources.FleetPass, FleetUnknown: true, Website: []string{sources.WebsiteHTTPS}, WebsiteUnknown: true, Government: new(bool), MaxFilingAgeDays: ptrInt(4), FilingUnknown: true, MinCampaignAgeDays: ptrInt(5), Class: sources.ClassMixed, ClassUnknown: true, Basis: []string{store.BasisPartial}, ShowExcluded: true}
	contractEqual(t, store.ProspectListFilters(struct {
		RevenueMin         *int64   `json:"revenue_min,omitempty"`
		RevenueMax         *int64   `json:"revenue_max,omitempty"`
		RevenueUnknown     bool     `json:"revenue_unknown,omitempty"`
		MaxMinutes         *int64   `json:"max_minutes,omitempty"`
		MinutesUnknown     bool     `json:"minutes_unknown,omitempty"`
		Categories         []string `json:"categories,omitempty"`
		CategoryUnknown    bool     `json:"category_unknown,omitempty"`
		Fleet              string   `json:"fleet,omitempty"`
		FleetUnknown       bool     `json:"fleet_unknown,omitempty"`
		Website            []string `json:"website,omitempty"`
		WebsiteUnknown     bool     `json:"website_unknown,omitempty"`
		Government         *bool    `json:"government,omitempty"`
		MaxFilingAgeDays   *int64   `json:"max_filing_age_days,omitempty"`
		FilingUnknown      bool     `json:"filing_unknown,omitempty"`
		MinCampaignAgeDays *int64   `json:"min_campaign_age_days,omitempty"`
		Class              string   `json:"class,omitempty"`
		ClassUnknown       bool     `json:"class_unknown,omitempty"`
		Basis              []string `json:"basis,omitempty"`
		ShowExcluded       bool     `json:"show_excluded,omitempty"`
	}(f)), f)

	raw, err := json.Marshal(f)
	contractOK(t, err)
	contractEqual(t, string(raw), `{"revenue_min":1,"revenue_max":2,"revenue_unknown":true,"max_minutes":3,"minutes_unknown":true,"categories":["other"],"category_unknown":true,"fleet":"pass","fleet_unknown":true,"website":["https"],"website_unknown":true,"government":false,"max_filing_age_days":4,"filing_unknown":true,"min_campaign_age_days":5,"class":"mixed","class_unknown":true,"basis":["partial"],"show_excluded":true}`)
	var got store.ProspectListFilters
	contractOK(t, json.Unmarshal(raw, &got))
	contractEqual(t, got, f)
}

// R-XKDG-B6R4
func TestBasisFilterValidation(t *testing.T) {
	// Other filter fields are exercised by TestProspectListValidators; this adds the new basis dimension and its interaction with invalid existing fields.
	for _, basis := range [][]string{nil, {}, {store.BasisRecords}, {store.BasisPartial}, {store.BasisEnriched}, {store.BasisRecords, store.BasisPartial, store.BasisEnriched}, {store.BasisEnriched, store.BasisEnriched}} {
		f := store.ProspectListFilters{Basis: basis}
		contractEqual(t, store.ValidProspectListFilters(f), true)
		f.RevenueMin = ptrInt(-1)
		contractEqual(t, store.ValidProspectListFilters(f), false)
	}
	for _, basis := range [][]string{{"Enriched"}, {"unknown"}, {""}, {store.BasisRecords, "unknown"}} {
		contractEqual(t, store.ValidProspectListFilters(store.ProspectListFilters{Basis: basis}), false)
	}
}

// R-KMHT-HV3K
func TestBasisFilterNormalForm(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	filters := store.ProspectListFilters{Categories: []string{sources.CategoryTrucking, sources.CategoryOther, sources.CategoryTrucking}, Basis: []string{store.BasisEnriched, store.BasisRecords, store.BasisPartial, store.BasisEnriched}, RevenueUnknown: true, Website: []string{sources.WebsiteNone, sources.WebsiteHTTPS}, ShowExcluded: true}
	p, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "normal form", Size: 30, Filters: filters, CreatedUserID: "creator"})
	contractOK(t, err)
	want := filters
	want.Categories = []string{sources.CategoryOther, sources.CategoryTrucking}
	want.Basis = []string{store.BasisRecords, store.BasisPartial, store.BasisEnriched}
	contractEqual(t, p.Filters, want)
	contractEqual(t, p.Order, []store.ProspectListOrder{{Field: store.OrderOperatingConfidence, Direction: store.DirectionDesc}})
	f.now = f.now.Add(time.Hour)
	edited, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: p.ID, Filters: &filters, UserID: "editor"})
	contractOK(t, err)
	contractEqual(t, edited, p)
	empty := store.ProspectListFilters{Categories: []string{}, Basis: []string{}}
	edited, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: p.ID, Filters: &empty, UserID: "editor"})
	contractOK(t, err)
	contractEqual(t, edited.Filters, store.ProspectListFilters{})
}

// R-XLLC-OYHT R-KIU4-CJVH
func TestProspectListBasisMembership(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	records := enrichmentCandidate(t, f, "records", "records@example.test")
	partial := enrichmentCandidate(t, f, "partial", "partial@example.test")
	enriched := enrichmentCandidate(t, f, "enriched", "enriched@example.test")
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, partial, "partial summary", false), briefResult(t, enriched, "complete summary", false), siteTextResult(t, enriched, enrichers.RunExited)})
	contractOK(t, err)
	route := basisRoute(t, ptrInt(84))
	route.Candidate = enriched
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{route})
	contractOK(t, err)
	deriveAll(t, f.s)
	_, err = f.s.Judge(ctx, []string{records, partial, enriched})
	contractOK(t, err)
	p := prospectListCreate(t, f, "basis membership")
	for _, tc := range []struct {
		basis []string
		ids   map[string]string
	}{
		{nil, map[string]string{records: store.BasisRecords, partial: store.BasisPartial, enriched: store.BasisEnriched}},
		{[]string{}, map[string]string{records: store.BasisRecords, partial: store.BasisPartial, enriched: store.BasisEnriched}},
		{[]string{store.BasisEnriched}, map[string]string{enriched: store.BasisEnriched}},
		{[]string{store.BasisRecords, store.BasisPartial}, map[string]string{records: store.BasisRecords, partial: store.BasisPartial}},
	} {
		filters := store.ProspectListFilters{Basis: tc.basis}
		_, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: p.ID, Filters: &filters, UserID: "editor"})
		contractOK(t, err)
		read, err := f.s.ReadProspectList(ctx, p.ID)
		contractOK(t, err)
		got := map[string]string{}
		for _, m := range read.Members {
			got[m.Candidate.ID] = m.Basis
			facts, err := f.s.GetFacts(ctx, m.Candidate.ID)
			contractOK(t, err)
			contractEqual(t, m.Facts, facts)
			c, err := f.s.GetCandidate(ctx, m.Candidate.ID)
			contractOK(t, err)
			contractEqual(t, m.Candidate, c)
			j, err := f.s.GetJudgments(ctx, m.Candidate.ID)
			contractOK(t, err)
			contractEqual(t, m.Judgments, j)
			var site, brief time.Time
			for _, w := range facts.Written {
				if w.Kind == enrichers.KindSiteText {
					site = w.At
				}
				if w.Kind == enrichers.KindBrief {
					brief = w.At
				}
			}
			contractEqual(t, m.SiteText, site)
			contractEqual(t, m.Brief, brief)
		}
		contractEqual(t, got, tc.ids)
	}
}

// R-LAVT-59XG
func TestOpenProspectListErrorsLeaveNoOpen(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	p := prospectListCreate(t, f, "open errors")
	deleted := prospectListCreate(t, f, "deleted")
	_, err := f.s.DeleteProspectList(ctx, deleted.ID)
	contractOK(t, err)
	candidate := contractSeed(t, f)
	_, err = f.s.OpenProspectList(ctx, p.ID, "owner", "owner@example.test")
	contractOK(t, err)
	_, err = f.s.Reject(ctx, store.Reject{ProspectList: p.ID, Candidate: candidate, Kind: store.KindReject, Why: "test reject", UserID: "owner"})
	contractOK(t, err)
	snapshot := func() any {
		lists, e := f.s.ListProspectLists(ctx, "", "", 100)
		contractOK(t, e)
		rejects, e := f.s.Rejects(ctx, candidate)
		contractOK(t, e)
		opens := map[string][]store.Open{}
		for _, id := range []string{p.ID, deleted.ID, "invalid", "pl_0000000000000000"} {
			os, e := f.s.Opens(ctx, id)
			contractOK(t, e)
			opens[id] = os
		}
		return struct {
			Records contractSnapshot
			Lists   []store.ProspectList
			Rejects []store.Reject
			Opens   map[string][]store.Open
		}{contractState(t, f), lists, rejects, opens}
	}

	for _, tc := range []struct {
		id, user string
		want     error
	}{
		{p.ID, "", store.ErrUnreachable}, {"invalid", "", store.ErrUnreachable}, {deleted.ID, "", store.ErrUnreachable},
		{"invalid", "owner", store.ErrNotFound}, {"pl_0000000000000000", "owner", store.ErrNotFound}, {deleted.ID, "owner", store.ErrNotFound},
	} {
		before := snapshot()
		read, err := f.s.OpenProspectList(ctx, tc.id, tc.user, "owner@example.test")
		contractError(t, err, tc.want)
		contractEqual(t, read, store.ProspectListRead{})
		contractEqual(t, snapshot(), before)
	}
	before := snapshot()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	read, err := f.s.OpenProspectList(canceled, p.ID, "owner", "owner@example.test")
	if err == nil {
		t.Fatal("canceled open succeeded")
	}
	contractEqual(t, read, store.ProspectListRead{})
	contractEqual(t, snapshot(), before)

}

// R-KIU4-CJVH
func TestBasisRecordShapes(t *testing.T) {
	counts := store.Counts{Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Derived: 10, Due: 11, Fetched: 12, HTTPS: 13, HTTP: 14, None: 15, Runs: 16, RunsFailed: 17, Pages: 18, Cost: 19}
	contractEqual(t, store.Counts(struct{ Rows, New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged, Derived, Due, Fetched, HTTPS, HTTP, None, Runs, RunsFailed, Pages, Cost int64 }(counts)), counts)
	observation := store.Observation{Scraper: "scraper", SourceID: "row", Digest: "digest", Reading: sources.Reading{Name: "sample business"}}
	contractEqual(t, store.Observation(struct {
		Scraper, SourceID, Digest string
		Reading                   sources.Reading
	}(observation)), observation)
	filter := store.CandidateFilter{Name: "business", Zip: "55072", Phone: "3205550101"}
	contractEqual(t, store.CandidateFilter(struct{ Name, Zip, Phone string }(filter)), filter)
	ranked := store.Ranked{Candidate: store.Candidate{ID: "sample"}, Judgments: store.Judgments{Candidate: "sample"}, Facts: store.Facts{Candidate: "sample"}, SiteText: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Brief: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), Basis: store.BasisPartial}
	contractEqual(t, store.Ranked(struct {
		Candidate store.Candidate
		Judgments store.Judgments
		Facts     store.Facts
		SiteText  time.Time
		Brief     time.Time
		Basis     string
	}(ranked)), ranked)
}

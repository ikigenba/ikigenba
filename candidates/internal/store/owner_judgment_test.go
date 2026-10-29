package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func ownerJudgeDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func recordForJudgment(t *testing.T, f *contractFixture, id string, values store.OwnerValues, cleared ...string) (store.OwnerRecord, store.Candidate) {
	t.Helper()
	r, c, err := f.s.AddOwnerRecord(context.Background(), store.OwnerRecord{Candidate: id, Set: values, Cleared: cleared, Note: "Observed business", UserID: "owner-user"})
	contractOK(t, err)
	return r, c
}

func ownerJudge(t *testing.T, f *contractFixture, id string) store.Judgments {
	t.Helper()
	_, err := f.s.Judge(context.Background(), []string{id})
	contractOK(t, err)
	got, err := f.s.GetJudgments(context.Background(), id)
	contractOK(t, err)
	return got
}

func ownerCarrier(t *testing.T, f *contractFixture, key string) string {
	t.Helper()
	o := contractObservation(key, "Carrier "+key, ownerJudgeDate(2025, 6, 1))
	o.Scraper = fmcsa.Name
	o.Reading.Estimate = nil
	o.Reading.Evidence = []sources.Evidence{{Name: sources.EvidenceFMCSAActive, Source: fmcsa.Name, Weight: .6}, {Name: sources.EvidenceOwnDomain, Source: fmcsa.Name, Weight: .2}}
	return contractAttach(t, f, "carrier-run", o)[0]
}

// R-0ECS-60SY R-0VFD-IT6O R-0GSK-XKAC R-0I0H-BC11
func TestOwnerJudgmentEvidenceAgesClearsAndFollowsMerges(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	f.now = ownerJudgeDate(2026, 10, 1)
	name := "Owner founded business"
	_, founded := recordForJudgment(t, f, "", store.OwnerValues{Name: &name})
	f.now = ownerJudgeDate(2026, 10, 15)
	got := ownerJudge(t, f, founded.ID)
	ownerEvidence := sources.Evidence{Name: sources.EvidenceOwnerRecord, Source: sources.Owner, Weight: .6, Description: sources.DescribeOwnerRecord}
	contractEqual(t, got.Evidence, []sources.Evidence{ownerEvidence})
	near(t, got.Operating, .6)
	contractEqual(t, got.OperatingKey, int64(600000))

	f.now = ownerJudgeDate(2019, 6, 1)
	name = "Old owner business"
	_, old := recordForJudgment(t, f, "", store.OwnerValues{Name: &name})
	f.now = ownerJudgeDate(2026, 10, 15)
	got = ownerJudge(t, f, old.ID)
	contractEqual(t, got.Evidence, []sources.Evidence{})
	contractEqual(t, got.Operating, (*float64)(nil))
	contractEqual(t, got.OperatingKey, int64(-1))
	f.now = ownerJudgeDate(2023, 10, 15)
	phone := "3205550199"
	recordForJudgment(t, f, old.ID, store.OwnerValues{Phone: &phone})
	f.now = ownerJudgeDate(2026, 10, 15)
	got = ownerJudge(t, f, old.ID)
	aged := ownerEvidence
	aged.Weight = .6 * judging.Freshness(ownerJudgeDate(2023, 10, 15), f.now)
	contractEqual(t, got.Evidence, []sources.Evidence{aged})
	contractEqual(t, got.Operating, judging.Operating([]sources.Evidence{aged}))

	carrier := ownerCarrier(t, f, "clear")
	near(t, ownerJudge(t, f, carrier).Operating, .68)
	f.now = ownerJudgeDate(2026, 10, 1)
	recordForJudgment(t, f, carrier, store.OwnerValues{}, sources.FactPhone)
	f.now = ownerJudgeDate(2026, 10, 15)
	got = ownerJudge(t, f, carrier)
	expected := []sources.Evidence{{Name: sources.EvidenceFMCSAActive, Source: fmcsa.Name, Weight: .6}, ownerEvidence, {Name: sources.EvidenceOwnDomain, Source: fmcsa.Name, Weight: .2}}
	contractEqual(t, got.Evidence, expected)
	near(t, got.Operating, .872)

	survivor := ownerCarrier(t, f, "survivor")
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: founded.ID, Why: "Same business", UserID: "owner-user"})
	contractOK(t, err)
	got = ownerJudge(t, f, survivor)
	contractEqual(t, got.Evidence, expected)
	f.reopen()
	persisted, err := f.s.GetJudgments(ctx, survivor)
	contractOK(t, err)
	contractEqual(t, persisted, got)
}

// R-0ECS-60SY R-0XV6-ACO2 R-0I0H-BC11 R-0FKO-JSJN
func TestOwnerJudgmentHeadcountOverridesPickAndOneSeriesYear(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = ownerJudgeDate(2026, 10, 1)
	hs := []sources.Headcount{trendCount(oshaita.Name, sources.HeadcountEmployees, 20, 2022), trendCount(oshaita.Name, sources.HeadcountEmployees, 15, 2025), trendCount(oshaita.Name, sources.HeadcountEmployees, 8, 2025)}
	var id string
	for i, h := range hs {
		o := contractObservation(string(rune('a'+i)), "Owner employee business", h.AsOf)
		o.Scraper = h.Scraper
		o.Reading.Estimate, o.Reading.Evidence = nil, nil
		o.Reading.Headcount = &h
		id = contractAttach(t, f, "site-run", o)[0]
	}
	_, _, err := f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 100)
	contractOK(t, err)
	facts, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	at := f.now
	for _, tc := range []struct {
		value            *store.OwnerHeadcount
		low              int64
		series           []judging.Series
		from, to, change int64
	}{
		{&store.OwnerHeadcount{Count: 18, Year: 2025}, 18, []judging.Series{{Kind: sources.HeadcountEmployees, Scraper: sources.Owner, Figures: []judging.Figure{{Year: 2022, Count: 20}, {Year: 2025, Count: 18}}}}, 20, 18, -2},
		{&store.OwnerHeadcount{Count: 40, Year: 2024}, 40, []judging.Series{{Kind: sources.HeadcountEmployees, Scraper: oshaita.Name, Figures: []judging.Figure{{Year: 2022, Count: 20}, {Year: 2024, Count: 40}, {Year: 2025, Count: 23}}}}, 20, 23, 3},
		{nil, 23, []judging.Series{{Kind: sources.HeadcountEmployees, Scraper: oshaita.Name, Figures: []judging.Figure{{Year: 2022, Count: 20}, {Year: 2025, Count: 23}}}}, 20, 23, 3},
	} {
		at = at.Add(time.Second)
		f.now = at
		if tc.value == nil {
			recordForJudgment(t, f, id, store.OwnerValues{}, sources.FactHeadcount)
		} else {
			recordForJudgment(t, f, id, store.OwnerValues{Headcount: tc.value})
		}
		f.now = ownerJudgeDate(2026, 10, 15)
		got := ownerJudge(t, f, id)
		contractEqual(t, got.HeadcountLow, ptrInt(tc.low))
		contractEqual(t, got.HeadcountHigh, ptrInt(tc.low))
		scraper := sources.Owner
		if tc.value == nil {
			scraper = oshaita.Name
		}
		contractEqual(t, got.HeadcountBasis, sources.HeadcountEmployees+":"+scraper)
		contractEqual(t, got.HeadcountSeries, tc.series)
		contractEqual(t, got.Trend, judging.TrendFlat)
		contractEqual(t, got.TrendKind, sources.HeadcountEmployees)
		contractEqual(t, got.TrendFromYear, ptrInt(2022))
		contractEqual(t, got.TrendToYear, ptrInt(2025))
		contractEqual(t, got.TrendFrom, ptrInt(tc.from))
		contractEqual(t, got.TrendTo, ptrInt(tc.to))
		contractEqual(t, got.TrendChange, ptrInt(tc.change))
		current, err := f.s.GetFacts(ctx, id)
		contractOK(t, err)
		contractEqual(t, current.Headcounts, facts.Headcounts)
	}
}

func ownerLoan(t *testing.T, f *contractFixture) string {
	t.Helper()
	date := ownerJudgeDate(2020, 4, 9)
	o := contractObservation("loan", "Owner revenue business", date)
	o.Scraper = ppp.Name
	o.Reading.Estimate = nil
	o.Reading.Evidence = []sources.Evidence{{Name: sources.EvidencePPPLoan, Source: ppp.Name, Weight: .2}}
	o.Reading.Category = sources.CategoryConstruction
	o.Reading.Headcount = &sources.Headcount{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, AsOf: date}
	id := contractAttach(t, f, "loan-run", o)[0]
	_, _, err := f.s.Derive(context.Background(), enrichers.KindLinkFacts, "", false, 100)
	contractOK(t, err)
	return id
}

// R-0ECS-60SY R-0XV6-ACO2 R-0FKO-JSJN R-0I0H-BC11
func TestOwnerJudgmentHeadcountZeroAndEmployeeBand(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = ownerJudgeDate(2026, 10, 1)
	id := ownerLoan(t, f)
	for _, count := range []int{10, 0} {
		f.now = f.now.Add(time.Second)
		recordForJudgment(t, f, id, store.OwnerValues{Headcount: &store.OwnerHeadcount{Count: count, Year: 2024}})
		f.now = ownerJudgeDate(2026, 10, 15)
		got := ownerJudge(t, f, id)
		contractEqual(t, got.HeadcountLow, ptrInt(int64(count)))
		contractEqual(t, got.HeadcountHigh, ptrInt(int64(count)))
		contractEqual(t, got.HeadcountBasis, sources.HeadcountEmployees+":"+sources.Owner)
		expected := []judging.Series{{Kind: sources.HeadcountJobs, Scraper: ppp.Name, Figures: []judging.Figure{{Year: 2020, Count: 26}}}}
		estimates, err := f.s.Estimates(ctx, id)
		contractOK(t, err)
		if count > 0 {
			expected = append([]judging.Series{{Kind: sources.HeadcountEmployees, Scraper: sources.Owner, Figures: []judging.Figure{{Year: 2024, Count: 10}}}}, expected...)
			assertHeadcountJudgment(t, got, 1500000, 3500000, .5, 10, 10, sources.HeadcountEmployees+":"+sources.Owner)
			contractEqual(t, len(estimates), 1)
			contractEqual(t, estimates[0].Source, judging.HeadcountSource)
			contractEqual(t, estimates[0].Low, int64(1500000))
			contractEqual(t, estimates[0].High, ptrInt(3500000))
			contractEqual(t, estimates[0].Confidence, .5)
			contractEqual(t, estimates[0].Filed, ownerJudgeDate(2024, 12, 31))
		} else {
			contractEqual(t, len(estimates), 0)
			contractEqual(t, got.RevenueLow, (*int64)(nil))
			contractEqual(t, got.RevenueHigh, (*int64)(nil))
			contractEqual(t, got.RevenueConfidence, (*float64)(nil))
		}
		contractEqual(t, got.HeadcountSeries, expected)
	}
}

// R-0ECS-60SY R-0Z32-O4ER R-0I0H-BC11 R-0FKO-JSJN
func TestOwnerJudgmentRevenueReplacesCombinedButKeepsEstimates(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = ownerJudgeDate(2026, 10, 1)
	id := ownerLoan(t, f)
	baseline := ownerJudge(t, f, id)
	original, err := f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, len(original), 1)
	contractEqual(t, original[0].Source, judging.HeadcountSource)
	contractEqual(t, original[0].Low, int64(3900000))
	contractEqual(t, original[0].High, ptrInt(9100000))
	contractEqual(t, original[0].Confidence, .4)
	at := f.now
	for _, tc := range []struct {
		revenue    *store.OwnerRevenue
		low, high  *int64
		confidence float64
		unranged   int64
	}{
		{&store.OwnerRevenue{High: ptrInt(2000000)}, nil, ptrInt(2000000), 1, 1},
		{&store.OwnerRevenue{Low: ptrInt(5000000)}, ptrInt(5000000), nil, 1, 0},
		{&store.OwnerRevenue{Low: ptrInt(1000000), High: ptrInt(2000000)}, ptrInt(1000000), ptrInt(2000000), 1, 0},
		{nil, ptrInt(3900000), ptrInt(9100000), .4, 0},
	} {
		at = at.Add(time.Second)
		f.now = at
		if tc.revenue == nil {
			recordForJudgment(t, f, id, store.OwnerValues{}, sources.FactRevenue)
		} else {
			recordForJudgment(t, f, id, store.OwnerValues{Revenue: tc.revenue})
		}
		f.now = ownerJudgeDate(2026, 10, 15)
		counts, err := f.s.Judge(ctx, []string{id})
		contractOK(t, err)
		contractEqual(t, counts, store.Counts{Judged: 1, Unranged: tc.unranged})
		got, err := f.s.GetJudgments(ctx, id)
		contractOK(t, err)
		contractEqual(t, got.RevenueLow, tc.low)
		contractEqual(t, got.RevenueHigh, tc.high)
		near(t, got.RevenueConfidence, tc.confidence)
		contractEqual(t, got.HeadcountLow, baseline.HeadcountLow)
		estimates, err := f.s.Estimates(ctx, id)
		contractOK(t, err)
		contractEqual(t, estimates, original)
	}
	carrier := contractObservation("truck", "Owner truck revenue", ownerJudgeDate(2025, 6, 1))
	carrier.Reading.Estimate = &sources.Estimate{Source: fmcsa.Name, Low: 2100000, High: ptrInt(3500000), Confidence: .7}
	carrier.Reading.Evidence = []sources.Evidence{{Name: sources.EvidenceFMCSAActive, Source: fmcsa.Name, Weight: .6}}
	truckID := contractAttach(t, f, "truck-run", carrier)[0]
	recordForJudgment(t, f, truckID, store.OwnerValues{Revenue: &store.OwnerRevenue{Low: ptrInt(1000000), High: ptrInt(2000000)}})
	got := ownerJudge(t, f, truckID)
	contractEqual(t, got.RevenueLow, ptrInt(1000000))
	contractEqual(t, got.RevenueHigh, ptrInt(2000000))
	near(t, got.RevenueConfidence, 1)
	estimates, err := f.s.Estimates(ctx, truckID)
	contractOK(t, err)
	contractEqual(t, estimates, []sources.Estimate{*carrier.Reading.Estimate})
}

// R-10AZ-1W5G R-0I0H-BC11
func TestOwnerRecordSynchronouslyJudgesOnlyItsCandidateAndPersists(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = ownerJudgeDate(2026, 10, 1)
	ids := contractAttach(t, f, "initial", contractObservation("first", "First business", f.now), contractObservation("second", "Second business", f.now))
	_, err := f.s.Judge(ctx, ids)
	contractOK(t, err)
	other, err := f.s.GetJudgments(ctx, ids[1])
	contractOK(t, err)
	otherEstimates, err := f.s.Estimates(ctx, ids[1])
	contractOK(t, err)
	passes, err := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, err)
	f.now = f.now.Add(time.Second)
	name := "Founded employees business"
	record, c := recordForJudgment(t, f, "", store.OwnerValues{Name: &name, Headcount: &store.OwnerHeadcount{Count: 10, Year: 2024}})
	got, err := f.s.GetJudgments(ctx, c.ID)
	contractOK(t, err)
	contractEqual(t, got.Evidence, []sources.Evidence{{Name: sources.EvidenceOwnerRecord, Source: sources.Owner, Weight: .6, Description: sources.DescribeOwnerRecord}})
	near(t, got.Operating, .6)
	contractEqual(t, got.HeadcountLow, ptrInt(10))
	contractEqual(t, got.HeadcountHigh, ptrInt(10))
	contractEqual(t, got.HeadcountBasis, sources.HeadcountEmployees+":"+sources.Owner)
	contractEqual(t, got.Rules, sources.Rules)
	contractEqual(t, got.Calculated, record.At)
	foundedEstimates, err := f.s.Estimates(ctx, c.ID)
	contractOK(t, err)
	contractEqual(t, len(foundedEstimates), 1)
	f.now = f.now.Add(time.Second)
	record, _ = recordForJudgment(t, f, ids[0], store.OwnerValues{Revenue: &store.OwnerRevenue{High: ptrInt(2000000)}})
	ceiling, err := f.s.GetJudgments(ctx, ids[0])
	contractOK(t, err)
	contractEqual(t, ceiling.RevenueLow, (*int64)(nil))
	contractEqual(t, ceiling.RevenueHigh, ptrInt(2000000))
	near(t, ceiling.RevenueConfidence, 1)
	contractEqual(t, ceiling.Calculated, record.At)
	estimates, err := f.s.Estimates(ctx, ids[0])
	contractOK(t, err)
	for range 2 {
		untouched, err := f.s.GetJudgments(ctx, ids[1])
		contractOK(t, err)
		contractEqual(t, untouched, other)
		untouchedEstimates, err := f.s.Estimates(ctx, ids[1])
		contractOK(t, err)
		contractEqual(t, untouchedEstimates, otherEstimates)
		afterPasses, err := f.s.ListPasses(ctx, "", "", 100)
		contractOK(t, err)
		contractEqual(t, afterPasses, passes)
		persisted, err := f.s.GetJudgments(ctx, c.ID)
		contractOK(t, err)
		contractEqual(t, persisted, got)
		es, err := f.s.Estimates(ctx, c.ID)
		contractOK(t, err)
		contractEqual(t, es, foundedEstimates)
		persisted, err = f.s.GetJudgments(ctx, ids[0])
		contractOK(t, err)
		contractEqual(t, persisted, ceiling)
		es, err = f.s.Estimates(ctx, ids[0])
		contractOK(t, err)
		contractEqual(t, es, estimates)
		f.reopen()
	}
}

// R-0J8D-P3RQ R-0I0H-BC11
func TestOwnerJudgmentUsesOneClockAndSkipsMergedAndUnselected(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = ownerJudgeDate(2026, 10, 1)
	name := "Selected owner business"
	_, selected := recordForJudgment(t, f, "", store.OwnerValues{Name: &name})
	name = "Untouched owner business"
	_, untouched := recordForJudgment(t, f, "", store.OwnerValues{Name: &name})
	name = "Merged owner business"
	_, merged := recordForJudgment(t, f, "", store.OwnerValues{Name: &name})
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: untouched.ID, Other: merged.ID, Why: "Same business", UserID: "owner-user"})
	contractOK(t, err)
	before, err := f.s.GetJudgments(ctx, untouched.ID)
	contractOK(t, err)
	estimates, err := f.s.Estimates(ctx, untouched.ID)
	contractOK(t, err)
	candidatesBefore, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, err)
	linksBefore, err := f.s.Links(ctx, selected.ID)
	contractOK(t, err)
	at := time.Date(2026, 10, 15, 1, 2, 3, 456, time.FixedZone("test-zone", 3600))
	reads := 0
	f.s = store.New(f.d, store.Config{Now: func() time.Time { reads++; return at.Add(time.Duration(reads-1) * time.Hour) }, Rand: f.rnd})
	counts, err := f.s.Judge(ctx, []string{selected.ID, selected.ID, "missing", merged.ID})
	contractOK(t, err)
	contractEqual(t, counts, store.Counts{Judged: 1, Unranged: 1})
	contractEqual(t, reads, 1)
	got, err := f.s.GetJudgments(ctx, selected.ID)
	contractOK(t, err)
	contractEqual(t, got.Calculated, at.UTC().Truncate(time.Second))
	after, err := f.s.GetJudgments(ctx, untouched.ID)
	contractOK(t, err)
	contractEqual(t, after, before)
	es, err := f.s.Estimates(ctx, untouched.ID)
	contractOK(t, err)
	contractEqual(t, es, estimates)
	candidatesAfter, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, err)
	contractEqual(t, candidatesAfter, candidatesBefore)
	linksAfter, err := f.s.Links(ctx, selected.ID)
	contractOK(t, err)
	contractEqual(t, linksAfter, linksBefore)
	f.reopen()
	persisted, err := f.s.GetJudgments(ctx, selected.ID)
	contractOK(t, err)
	contractEqual(t, persisted, got)
}

// R-0GSK-XKAC R-0ECS-60SY
func TestOwnerJudgmentDeduplicatesEvidenceWithRowWinningEqualWeight(t *testing.T) {
	for _, weight := range []float64{.4, .6, .7} {
		f := contractDB(t)
		f.now = ownerJudgeDate(2026, 10, 1)
		o := contractObservation("duplicate", "Owner duplicate evidence", ownerJudgeDate(2025, 6, 1))
		o.Reading.Estimate = nil
		row := sources.Evidence{Name: sources.EvidenceOwnerRecord, Source: "fixture-source", Weight: weight, Description: "Fixture evidence"}
		o.Reading.Evidence = []sources.Evidence{row}
		id := contractAttach(t, f, "duplicate-run", o)[0]
		recordForJudgment(t, f, id, store.OwnerValues{}, sources.FactPhone)
		f.now = ownerJudgeDate(2026, 10, 15)
		got := ownerJudge(t, f, id)
		expected := row
		if weight < .6 {
			expected = sources.Evidence{Name: sources.EvidenceOwnerRecord, Source: sources.Owner, Weight: .6, Description: sources.DescribeOwnerRecord}
		}
		contractEqual(t, got.Evidence, []sources.Evidence{expected})
	}
}

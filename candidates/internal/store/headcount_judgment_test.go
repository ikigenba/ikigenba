package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-5HX1-ZJFE R-5J4Y-DB63 R-5KCU-R2WS R-5LKR-4UNH
func TestPPPHeadcountRequiresDerivedFacts(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	date := time.Date(2020, 4, 9, 0, 0, 0, 0, time.UTC)
	o := contractObservation("loan-headcount", "Construction company", date)
	o.Scraper = ppp.Name
	o.Reading.Estimate = nil
	o.Reading.Category = sources.CategoryConstruction
	o.Reading.Evidence = []sources.Evidence{{Name: sources.EvidencePPPLoan, Source: ppp.Name, Weight: .2}}
	o.Reading.Headcount = &sources.Headcount{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, AsOf: date}
	id := contractAttach(t, f, "loan-run", o)[0]
	counts, err := f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	contractEqual(t, counts, store.Counts{Judged: 1, Unranged: 1})
	j, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	near(t, j.Operating, .2)
	contractEqual(t, j.RevenueLow, (*int64)(nil))
	contractEqual(t, j.RevenueHigh, (*int64)(nil))
	contractEqual(t, j.RevenueConfidence, (*float64)(nil))
	contractEqual(t, j.HeadcountLow, (*int64)(nil))
	contractEqual(t, j.HeadcountHigh, (*int64)(nil))
	contractEqual(t, j.HeadcountBasis, "")
	estimates, err := f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, len(estimates), 0)
	_, _, err = f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 100)
	contractOK(t, err)
	counts, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	contractEqual(t, counts, store.Counts{Judged: 1})
	j, err = f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	near(t, j.Operating, .2)
	assertHeadcountJudgment(t, j, 3900000, 9100000, .4, 26, 26, sources.HeadcountJobs+":"+ppp.Name)
	estimates, err = f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, len(estimates), 1)
	contractEqual(t, estimates[0].Source, judging.HeadcountSource)
	contractEqual(t, estimates[0].Low, int64(3900000))
	contractEqual(t, estimates[0].High, ptrInt(9100000))
	contractEqual(t, estimates[0].Confidence, .4)
	contractEqual(t, estimates[0].Filed, date)
	contractEqual(t, estimates[0].Formula, sources.Rules)
	f.reopen()
	persisted, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	contractEqual(t, persisted, j)
	carrier := o
	carrier.Scraper = fmcsa.Name
	carrier.SourceID = "carrier-headcount"
	carrier.Reading.Headcount = nil
	carrier.Reading.Category = ""
	carrier.Reading.Filed = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	carrier.Reading.Estimate = &sources.Estimate{Source: fmcsa.Name, Low: 2100000, High: ptrInt(3500000), Confidence: .7}
	active := sources.Evidence{Name: sources.EvidenceFMCSAActive, Source: fmcsa.Name, Weight: .6}
	activity := sources.Evidence{Name: sources.EvidenceFMCSAActivity, Source: fmcsa.Name, Weight: .3}
	domain := sources.Evidence{Name: sources.EvidenceOwnDomain, Source: fmcsa.Name, Weight: .2}
	carrier.Reading.Evidence = []sources.Evidence{active, activity, domain}
	contractAttach(t, f, "carrier-run", carrier)
	_, _, err = f.s.Derive(ctx, enrichers.KindLinkFacts, "", true, 100)
	contractOK(t, err)
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	combined, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	near(t, combined.Operating, .8208)
	contractEqual(t, combined.Evidence, append([]sources.Evidence{active, activity, domain}, o.Reading.Evidence...))
	assertHeadcountJudgment(t, combined, 2754545, 5536364, .7, 26, 26, sources.HeadcountJobs+":"+ppp.Name)
	estimates, err = f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, len(estimates), 2)
	contractEqual(t, estimates[0], *carrier.Reading.Estimate)
	contractEqual(t, estimates[1].Source, judging.HeadcountSource)
	_, _, err = f.s.MarkGone(ctx, ppp.Name, "absent")
	contractOK(t, err)
	_, _, err = f.s.Derive(ctx, enrichers.KindLinkFacts, "", true, 100)
	contractOK(t, err)
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	alone, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	near(t, alone.Operating, .776)
	contractEqual(t, alone.RevenueLow, ptrInt(2100000))
	contractEqual(t, alone.RevenueHigh, ptrInt(3500000))
	near(t, alone.RevenueConfidence, .7)
	contractEqual(t, alone.HeadcountLow, (*int64)(nil))
	contractEqual(t, alone.HeadcountHigh, (*int64)(nil))
	contractEqual(t, alone.HeadcountBasis, "")
	estimates, err = f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, estimates, []sources.Estimate{*carrier.Reading.Estimate})
}

func assertHeadcountJudgment(t *testing.T, j store.Judgments, low, high int64, confidence float64, headLow, headHigh int64, basis string) {
	t.Helper()
	contractEqual(t, j.RevenueLow, ptrInt(low))
	contractEqual(t, j.RevenueHigh, ptrInt(high))
	near(t, j.RevenueConfidence, confidence)
	contractEqual(t, j.HeadcountLow, ptrInt(headLow))
	contractEqual(t, j.HeadcountHigh, ptrInt(headHigh))
	contractEqual(t, j.HeadcountBasis, basis)
	contractEqual(t, j.Rules, sources.Rules)
}

// R-62NC-HN17 R-5HX1-ZJFE R-5KCU-R2WS
func TestHeadcountRevenueNumericalCases(t *testing.T) {
	for _, tc := range []struct {
		name              string
		jobs, drivers     int64
		category          string
		truck             bool
		low, high         int64
		confidence        float64
		headLow, headHigh int64
	}{
		{"drivers_default", 0, 20, "", false, 2000000, 5000000, .3, 20, 20},
		{"trucks_and_drivers", 0, 12, sources.CategoryTrucking, true, 1482000, 2470000, .7, 12, 12},
		{"agreeing_kinds", 30, 28, sources.CategoryConstruction, false, 4500000, 10500000, .7, 30, 30},
		{"disagreeing_kinds", 100, 40, sources.CategoryConstruction, false, 6000000, 35000000, .4, 40, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			f.now = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
			date := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
			o := contractObservation("drivers", "Revenue company", date)
			o.Scraper = fmcsa.Name
			o.Reading.Estimate = nil
			o.Reading.Category = tc.category
			if tc.jobs > 0 {
				o.Reading.Category = ""
			}
			o.Reading.Headcount = &sources.Headcount{Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: tc.drivers, AsOf: date}
			if tc.truck {
				o.Reading.Estimate = &sources.Estimate{Source: fmcsa.Name, Low: 1500000, High: ptrInt(2500000), Confidence: .7}
			}
			id := contractAttach(t, f, "driver-run", o)[0]
			basis := sources.HeadcountDrivers + ":" + fmcsa.Name
			if tc.jobs > 0 {
				p := o
				p.Scraper = ppp.Name
				p.SourceID = "jobs"
				p.Reading.Category = tc.category
				p.Reading.Headcount = &sources.Headcount{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: tc.jobs}
				contractAttach(t, f, "job-run", p)
				basis = sources.HeadcountJobs + ":" + ppp.Name + " " + basis
			}
			_, _, err := f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 100)
			contractOK(t, err)
			_, err = f.s.Judge(ctx, []string{id})
			contractOK(t, err)
			j, err := f.s.GetJudgments(ctx, id)
			contractOK(t, err)
			assertHeadcountJudgment(t, j, tc.low, tc.high, tc.confidence, tc.headLow, tc.headHigh, basis)
			estimates, err := f.s.Estimates(ctx, id)
			contractOK(t, err)
			n := 1
			if tc.truck {
				n = 2
				contractEqual(t, estimates[0], *o.Reading.Estimate)
			}
			contractEqual(t, len(estimates), n)
			head := estimates[n-1]
			contractEqual(t, head.Source, judging.HeadcountSource)
			if tc.truck {
				contractEqual(t, head.Low, int64(1440000))
				contractEqual(t, head.High, ptrInt(2400000))
				contractEqual(t, head.Confidence, .3)
			} else {
				contractEqual(t, head.Low, tc.low)
				contractEqual(t, head.High, ptrInt(tc.high))
				contractEqual(t, head.Confidence, tc.confidence)
			}
			if tc.jobs == 0 {
				contractEqual(t, head.Filed, date)
			}
			if tc.name == "drivers_default" {
				zero := 0
				found, err := json.Marshal(enrichers.BriefFound{Run: "brief-run", Status: enrichers.RunExited, ExitCode: &zero, Summary: "Test summary", Categories: []enrichers.BriefCategory{}, Labels: []string{"underground utility contractor"}})
				contractOK(t, err)
				_, err = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: enrichers.KindBrief, Inputs: "brief-input", Source: enrichers.SourcePrompts, Found: found}})
				contractOK(t, err)
				before, err := f.s.GetJudgments(ctx, id)
				contractOK(t, err)
				contractEqual(t, before, j)
				_, err = f.s.Judge(ctx, []string{id})
				contractOK(t, err)
				j, err = f.s.GetJudgments(ctx, id)
				contractOK(t, err)
				assertHeadcountJudgment(t, j, 4000000, 7000000, .3, 20, 20, basis)
				estimates, err = f.s.Estimates(ctx, id)
				contractOK(t, err)
				contractEqual(t, len(estimates), 1)
				contractEqual(t, estimates[0].Low, int64(4000000))
				contractEqual(t, estimates[0].High, ptrInt(7000000))
			}
		})
	}
}

// R-5QGC-NXM9
func TestJudgeUsesStoredFactsWithChangedZipAndRawWebsite(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	o := contractObservation("stored-facts", "Stored facts", f.now)
	id := contractAttach(t, f, "first", o)[0]
	contractAttach(t, f, "other", contractObservation("other", "Other company", f.now))
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{o.Reading.Zip: 15}))
	_, _, err := f.s.Derive(ctx, enrichers.KindDriveMinutes, "", false, 100)
	contractOK(t, err)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{o.Reading.Zip: 80}))
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,inputs,found,source,at,cost) VALUES (?,?,?,?,?,?,0)`, id, sources.WebsiteCheck, "raw-site", string(websiteFound(website.Result{State: sources.WebsiteHTTPS, URL: "https://raw.test/"})), "raw-site", f.now.Format(time.RFC3339))
		return e
	}))
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	j, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	for _, e := range j.Evidence {
		if e.Name == sources.EvidenceWebsiteLive {
			t.Fatal("judge used raw enrichment instead of facts")
		}
	}
	contractEqual(t, factsAt(ctx, t, f.s, id).Minutes, ptrInt(15))
}

package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-P8CN-YQK2
func TestTrendMigrationBytes(t *testing.T) {
	got, err := fs.ReadFile(candidates.Migrations(), "0022_headcount_trend.sql")
	contractOK(t, err)
	contractEqual(t, string(got), "ALTER TABLE judgments ADD COLUMN headcount_series TEXT NOT NULL DEFAULT '[]';\nALTER TABLE judgments ADD COLUMN trend TEXT NOT NULL DEFAULT 'unknown';\nALTER TABLE judgments ADD COLUMN trend_kind TEXT NOT NULL DEFAULT '';\nALTER TABLE judgments ADD COLUMN trend_from_year INTEGER;\nALTER TABLE judgments ADD COLUMN trend_to_year INTEGER;\nALTER TABLE judgments ADD COLUMN trend_from INTEGER;\nALTER TABLE judgments ADD COLUMN trend_to INTEGER;\nALTER TABLE judgments ADD COLUMN trend_change INTEGER;\n")
}

func trendCount(scraper, kind string, count, year int64) sources.Headcount {
	return sources.Headcount{Scraper: scraper, Kind: kind, Count: count, AsOf: time.Date(int(year), 12, 31, 0, 0, 0, 0, time.UTC)}
}

func assertTrend(t *testing.T, got store.Judgments, hs []sources.Headcount) {
	t.Helper()
	series := judging.HeadcountSeries(hs)
	trend := judging.HeadcountTrend(series)
	contractEqual(t, got.HeadcountSeries, series)
	contractEqual(t, got.Trend, trend.Direction)
	contractEqual(t, got.TrendKind, trend.Kind)
	if trend.Direction == judging.TrendUnknown {
		contractEqual(t, got.TrendFromYear, (*int64)(nil))
		contractEqual(t, got.TrendToYear, (*int64)(nil))
		contractEqual(t, got.TrendFrom, (*int64)(nil))
		contractEqual(t, got.TrendTo, (*int64)(nil))
		contractEqual(t, got.TrendChange, (*int64)(nil))
	} else {
		contractEqual(t, got.TrendFromYear, &trend.FromYear)
		contractEqual(t, got.TrendToYear, &trend.ToYear)
		contractEqual(t, got.TrendFrom, &trend.From)
		contractEqual(t, got.TrendTo, &trend.To)
		contractEqual(t, got.TrendChange, &trend.Change)
	}
	if got.HeadcountSeries == nil {
		t.Fatal("nil series")
	}
	for _, s := range got.HeadcountSeries {
		if s.Figures == nil {
			t.Fatal("nil figures")
		}
	}
}

// R-P74R-KYTD R-P5WV-772O R-0I0H-BC11
func TestJudgePersistsSeriesAndTrendFromStoredFacts(t *testing.T) {
	cases := [][]sources.Headcount{
		{trendCount(oshaita.Name, sources.HeadcountEmployees, 100, 2021), trendCount(oshaita.Name, sources.HeadcountEmployees, 131, 2025), trendCount(ppp.Name, sources.HeadcountJobs, 90, 2020)},
		{trendCount(oshaita.Name, sources.HeadcountEmployees, 20, 2022), trendCount(oshaita.Name, sources.HeadcountEmployees, 23, 2025)},
		{trendCount(oshaita.Name, sources.HeadcountEmployees, 60, 2022), trendCount(oshaita.Name, sources.HeadcountEmployees, 40, 2025)},
		{trendCount(form5500.Name, sources.HeadcountParticipants, math.MaxInt64, 2025)},
		{},
	}
	for _, hs := range cases {
		f := contractDB(t)
		ctx := context.Background()
		id := contractAttach(t, f, "run", contractObservation("series", "Series business", f.now))[0]
		_, err := f.s.FillFacts(ctx, "", 100)
		contractOK(t, err)
		setTrendFacts(t, f, id, hs)
		_, err = f.s.Judge(ctx, []string{id})
		contractOK(t, err)
		before, err := f.s.GetJudgments(ctx, id)
		contractOK(t, err)
		assertTrend(t, before, hs)
		contractEqual(t, before.Calculated, f.now.UTC().Truncate(time.Second))
		f.reopen()
		got, err := f.s.GetJudgments(ctx, id)
		contractOK(t, err)
		contractEqual(t, got, before)
		assertTrend(t, got, hs)
	}
}

func setTrendFacts(t *testing.T, f *contractFixture, id string, hs []sources.Headcount) {
	t.Helper()
	// Facts use the persisted date vocabulary, rather than Headcount's JSON field names.
	rows := make([]map[string]any, 0, len(hs))
	for _, h := range hs {
		rows = append(rows, map[string]any{"scraper": h.Scraper, "kind": h.Kind, "count": h.Count, "date": h.AsOf.Format(time.RFC3339)})
	}
	b, err := json.Marshal(rows)
	contractOK(t, err)
	contractOK(t, f.d.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), "INSERT INTO facts(candidate,headcounts) VALUES (?,?) ON CONFLICT(candidate) DO UPDATE SET headcounts=excluded.headcounts", id, string(b))
		return e
	}))
}

// R-0LO6-GN94
func TestJudgeSeriesRequiresDerivedHeadcountsAndSumsSites(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	counts := []sources.Headcount{trendCount(oshaita.Name, sources.HeadcountEmployees, 20, 2022), trendCount(oshaita.Name, sources.HeadcountEmployees, 15, 2025), trendCount(oshaita.Name, sources.HeadcountEmployees, 8, 2025), trendCount(form5500.Name, sources.HeadcountParticipants, 20, 2024)}
	var id string
	for i, h := range counts {
		o := contractObservation(string(rune('a'+i)), "Site business", h.AsOf)
		o.Scraper = h.Scraper
		o.Reading.Estimate = nil
		o.Reading.Headcount = &h
		id = contractAttach(t, f, "run", o)[0]
	}
	_, err := f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	got, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	assertTrend(t, got, nil)
	_, _, err = f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 100)
	contractOK(t, err)
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	got, err = f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	contractEqual(t, got.HeadcountSeries, []judging.Series{{Kind: sources.HeadcountEmployees, Scraper: oshaita.Name, Figures: []judging.Figure{{Year: 2022, Count: 20}, {Year: 2025, Count: 23}}}, {Kind: sources.HeadcountParticipants, Scraper: form5500.Name, Figures: []judging.Figure{{Year: 2024, Count: 20}}}})
	contractEqual(t, got.Trend, judging.TrendFlat)
	contractEqual(t, got.TrendFrom, ptrInt(20))
	contractEqual(t, got.TrendTo, ptrInt(23))
	contractEqual(t, got.TrendChange, ptrInt(3))
}

// trendPriorJudgment performs the previous release's complete judgments upsert.
func trendPriorJudgment(ctx context.Context, tx *sql.Tx, id, at string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO judgments(candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,excluded,rules,calculated,headcount_low,headcount_high,headcount_basis) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(candidate) DO UPDATE SET operating=excluded.operating,operating_key=excluded.operating_key,evidence=excluded.evidence,revenue_low=excluded.revenue_low,revenue_high=excluded.revenue_high,revenue_confidence=excluded.revenue_confidence,excluded=excluded.excluded,rules=excluded.rules,calculated=excluded.calculated,headcount_low=excluded.headcount_low,headcount_high=excluded.headcount_high,headcount_basis=excluded.headcount_basis`, id, .7, int64(700000), `[{"Name":"prior-evidence","Source":"prior-source","Weight":0.7,"Description":"prior-description"}]`, int64(123), int64(456), .4, true, int64(112), at, int64(140), int64(140), "prior-basis")
	return err
}
func assertTrendPrior(t *testing.T, s *store.Store, id, at string, hs []sources.Headcount) {
	t.Helper()
	got, err := s.GetJudgments(context.Background(), id)
	contractOK(t, err)
	wantAt, err := time.Parse(time.RFC3339, at)
	contractOK(t, err)
	op, confidence := .7, .4
	want := store.Judgments{Candidate: id, Operating: &op, OperatingKey: 700000, Evidence: []sources.Evidence{{Name: "prior-evidence", Source: "prior-source", Weight: .7, Description: "prior-description"}}, RevenueLow: ptrInt(123), RevenueHigh: ptrInt(456), RevenueConfidence: &confidence, HeadcountLow: ptrInt(140), HeadcountHigh: ptrInt(140), HeadcountBasis: "prior-basis", Excluded: true, Rules: 112, Calculated: wantAt.UTC()}
	want.HeadcountSeries = judging.HeadcountSeries(hs)
	tr := judging.HeadcountTrend(want.HeadcountSeries)
	want.Trend, want.TrendKind = tr.Direction, tr.Kind
	if tr.Direction != judging.TrendUnknown {
		want.TrendFromYear, want.TrendToYear, want.TrendFrom, want.TrendTo, want.TrendChange = &tr.FromYear, &tr.ToYear, &tr.From, &tr.To, &tr.Change
	}
	contractEqual(t, got, want)
}

// R-P9KK-CIAR R-P5WV-772O
func TestTrendUpgradeReadsPreviousJudgments(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0021_link_keys.sql")})
	contractOK(t, err)
	at := "2026-04-03T06:07:08.123+02:00"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		if e := writePriorCandidate(ctx, tx, "prior"); e != nil {
			return e
		}
		return trendPriorJudgment(ctx, tx, "prior", at)
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	assertTrendPrior(t, store.New(d, store.Config{}), "prior", at, nil)
}

// R-E8FQ-9PRL R-P5WV-772O
func TestTrendPreviousWriterDefaultsAndPreservesAcrossReopen(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	old := contractAttach(t, f, "run", contractObservation("old", "Current series", f.now))[0]
	added := contractAttach(t, f, "run", contractObservation("added", "Rollback series", f.now))[0]
	_, err := f.s.FillFacts(ctx, "", 100)
	contractOK(t, err)
	hs := []sources.Headcount{trendCount(oshaita.Name, sources.HeadcountEmployees, 100, 2021), trendCount(oshaita.Name, sources.HeadcountEmployees, 131, 2025)}
	setTrendFacts(t, f, old, hs)
	_, err = f.s.Judge(ctx, []string{old})
	contractOK(t, err)
	before, err := f.s.GetJudgments(ctx, old)
	contractOK(t, err)
	contractEqual(t, before.HeadcountSeries, []judging.Series{{Kind: sources.HeadcountEmployees, Scraper: oshaita.Name, Figures: []judging.Figure{{Year: 2021, Count: 100}, {Year: 2025, Count: 131}}}})
	contractEqual(t, before.Trend, judging.TrendGrowing)
	contractEqual(t, before.TrendKind, sources.HeadcountEmployees)
	contractEqual(t, before.TrendFromYear, ptrInt(2021))
	contractEqual(t, before.TrendToYear, ptrInt(2025))
	contractEqual(t, before.TrendFrom, ptrInt(100))
	contractEqual(t, before.TrendTo, ptrInt(131))
	contractEqual(t, before.TrendChange, ptrInt(31))
	at := "2026-05-06T07:08:09.456-05:00"
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if e := trendPriorJudgment(ctx, tx, old, at); e != nil {
			return e
		}
		return trendPriorJudgment(ctx, tx, added, at)
	}))
	check := func() { assertTrendPrior(t, f.s, old, at, hs); assertTrendPrior(t, f.s, added, at, nil) }
	check()
	f.reopen()
	check()
}

// R-0LO6-GN94
func TestJudgeSeriesDerivedNumericalCases(t *testing.T) {
	for _, tc := range []struct {
		hs        []sources.Headcount
		series    []judging.Series
		direction string
		change    *int64
	}{
		{[]sources.Headcount{trendCount(oshaita.Name, sources.HeadcountEmployees, 327, 2022), trendCount(oshaita.Name, sources.HeadcountEmployees, 400, 2025), trendCount(oshaita.Name, sources.HeadcountEmployees, 2, 2025)}, []judging.Series{{Kind: sources.HeadcountEmployees, Scraper: oshaita.Name, Figures: []judging.Figure{{Year: 2022, Count: 327}, {Year: 2025, Count: 402}}}}, judging.TrendGrowing, ptrInt(75)},
		{[]sources.Headcount{trendCount(ppp.Name, sources.HeadcountJobs, 26, 2020), trendCount(ppp.Name, sources.HeadcountJobs, 30, 2021)}, []judging.Series{{Kind: sources.HeadcountJobs, Scraper: ppp.Name, Figures: []judging.Figure{{Year: 2020, Count: 26}, {Year: 2021, Count: 30}}}}, judging.TrendUnknown, nil},
		{[]sources.Headcount{trendCount("fmcsa", sources.HeadcountDrivers, 20, 2025)}, []judging.Series{}, judging.TrendUnknown, nil},
	} {
		f := contractDB(t)
		ctx := context.Background()
		f.now = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
		var id string
		for i, h := range tc.hs {
			o := contractObservation(string(rune('a'+i)), "Numerical series", h.AsOf)
			o.Scraper = h.Scraper
			o.Reading.Estimate = nil
			o.Reading.Headcount = &h
			id = contractAttach(t, f, "run", o)[0]
		}
		_, _, err := f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 100)
		contractOK(t, err)
		_, err = f.s.Judge(ctx, []string{id})
		contractOK(t, err)
		j, err := f.s.GetJudgments(ctx, id)
		contractOK(t, err)
		contractEqual(t, j.HeadcountSeries, tc.series)
		contractEqual(t, j.Trend, tc.direction)
		contractEqual(t, j.TrendChange, tc.change)
		assertTrend(t, j, tc.hs)
		if tc.hs[0].Kind == sources.HeadcountDrivers {
			contractEqual(t, j.HeadcountLow, ptrInt(20))
		}
	}
}

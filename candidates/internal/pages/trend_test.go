package pages

import (
	"bytes"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-48UF-VKWZ
func TestSeriesRowShapes(_ *testing.T) {
	_ = struct {
		Kind, Source string
		Figures      []FigureCell
	}(SeriesRow{})
	_ = struct{ Year, Count string }(FigureCell{})
}

// R-4CI5-0W52 R-0H83-B51T R-0TF3-4UGR
func TestTrendProjections(t *testing.T) {
	for _, tc := range []struct {
		trend            string
		from, to, change int64
		text             string
	}{
		{judging.TrendGrowing, 100, 131, 31, "+31"},
		{judging.TrendShrinking, 60, 40, -20, "-20"},
		{judging.TrendFlat, 20, 20, 0, "0"},
		{judging.TrendShrinking, 0, math.MinInt64, math.MinInt64, "-9223372036854775808"},
	} {
		a := store.Judgments{Trend: tc.trend, TrendKind: sources.HeadcountEmployees, TrendFromYear: new(int64(2021)), TrendToYear: new(int64(2025)), TrendFrom: &tc.from, TrendTo: &tc.to, TrendChange: &tc.change}
		got := judgmentsView(a)
		if got.Trend != tc.trend || got.TrendKind != sources.HeadcountEmployees || got.TrendFromYear != "2021" || got.TrendToYear != "2025" || got.TrendFrom != strconv.FormatInt(tc.from, 10) || got.TrendTo != strconv.FormatInt(tc.to, 10) || got.TrendChange != tc.text {
			t.Fatal(got)
		}
		member := memberRow(store.Ranked{Judgments: a}, 0)
		if member.Trend != tc.trend || member.TrendChange != tc.text {
			t.Fatal(member)
		}
	}
	a := store.Judgments{Trend: judging.TrendUnknown}
	got := judgmentsView(a)
	if got.Trend != judging.TrendUnknown || got.TrendKind != "" || got.TrendFromYear != "" || got.TrendToYear != "" || got.TrendFrom != "" || got.TrendTo != "" || got.TrendChange != "" {
		t.Fatal(got)
	}
	member := memberRow(store.Ranked{Judgments: a}, 0)
	if member.Trend != judging.TrendUnknown || member.TrendChange != "" {
		t.Fatal(member)
	}
}

// R-0G06-XDB4
func TestSeriesProjection(t *testing.T) {
	for _, tc := range []struct {
		in    []judging.Series
		years []string
		rows  []SeriesRow
	}{
		{nil, []string{}, []SeriesRow{}},
		{[]judging.Series{}, []string{}, []SeriesRow{}},
		{[]judging.Series{{Kind: "empty", Scraper: "empty-source"}}, []string{}, []SeriesRow{{"empty", "empty-source", []FigureCell{}}}},
		{[]judging.Series{{Kind: "one", Scraper: "one-source", Figures: []judging.Figure{{Year: 2025, Count: 23}}}}, []string{"2025"}, []SeriesRow{{"one", "one-source", []FigureCell{{"2025", "23"}}}}},
		{[]judging.Series{{Kind: sources.HeadcountEmployees, Scraper: "chosen-osha", Figures: []judging.Figure{{Year: 2025, Count: 131}, {Year: 2021, Count: 100}, {Year: 2023, Count: 120}, {Year: 2023, Count: 999}}}, {Kind: sources.HeadcountJobs, Scraper: "chosen-ppp", Figures: []judging.Figure{{Year: 2020, Count: 26}}}}, []string{"2020", "2021", "2022", "2023", "2024", "2025"}, []SeriesRow{{sources.HeadcountEmployees, "chosen-osha", []FigureCell{{"2020", ""}, {"2021", "100"}, {"2022", ""}, {"2023", "120"}, {"2024", ""}, {"2025", "131"}}}, {sources.HeadcountJobs, "chosen-ppp", []FigureCell{{"2020", "26"}, {"2021", ""}, {"2022", ""}, {"2023", ""}, {"2024", ""}, {"2025", ""}}}}},
	} {
		got := judgmentsView(store.Judgments{HeadcountSeries: tc.in})
		if !reflect.DeepEqual(got.HeadcountYears, tc.years) || !reflect.DeepEqual(got.HeadcountSeries, tc.rows) {
			t.Fatal(got, tc)
		}
	}
}

// R-I7XC-KKPT
func TestTrendFiltersProjection(t *testing.T) {
	for _, values := range [][]string{nil, {judging.TrendGrowing, judging.TrendShrinking}} {
		for _, unknown := range []bool{false, true} {
			got := filtersView(store.ProspectListFilters{Trend: values, TrendUnknown: unknown})
			if !reflect.DeepEqual(got.Trend, append([]string{}, values...)) || got.TrendUnknown != unknown {
				t.Fatal(got)
			}
		}
	}
	cfg, _ := pageFixture(t)
	for _, tc := range []struct{ input, want []string }{
		{[]string{judging.TrendShrinking, judging.TrendGrowing, judging.TrendShrinking}, []string{judging.TrendGrowing, judging.TrendShrinking}},
		{[]string{judging.TrendFlat, judging.TrendGrowing}, []string{judging.TrendGrowing, judging.TrendFlat}},
		{[]string{judging.TrendShrinking, judging.TrendFlat, judging.TrendGrowing}, []string{judging.TrendGrowing, judging.TrendFlat, judging.TrendShrinking}},
	} {
		pl, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "chosen-trends-" + strconv.Itoa(len(tc.input)) + tc.input[0], Size: 10, CreatedUserID: "owner", Filters: store.ProspectListFilters{Trend: tc.input}})
		if err != nil {
			t.Fatal(err)
		}
		if got := pageProspectList(t, cfg, pl.ID).ProspectList.Filters.Trend; !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got, tc.want)
		}
	}
}

// R-J14U-H32I
func TestTrendTemplateStatesAndValues(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, data any, values ...string) {
		t.Helper()
		var b bytes.Buffer
		if err := set.templates.ExecuteTemplate(&b, name, data); err != nil {
			t.Fatal(err)
		}
		for _, v := range values {
			if !strings.Contains(b.String(), v) {
				t.Fatalf("missing supplied value %q", v)
			}
		}
	}
	render("candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Judgments: &JudgmentsView{Trend: "chosen-candidate-trend"}}, "chosen-candidate-trend")
	render("prospect_list", ProspectListData{Members: []MemberRow{{Trend: "chosen-row-trend", TrendChange: "chosen-row-change"}}, ProspectList: ProspectListView{Filters: FiltersView{Trend: []string{"chosen-filter-one", "chosen-filter-two"}}}}, "chosen-row-trend", "chosen-row-change", "chosen-filter-one", "chosen-filter-two")
	for _, trend := range []string{judging.TrendGrowing, judging.TrendFlat, judging.TrendShrinking, judging.TrendUnknown} {
		for n := 0; n <= 3; n++ {
			j := JudgmentsView{Trend: trend}
			values := []string{trend}
			if trend != judging.TrendUnknown {
				j.TrendKind = "chosen-trend-kind"
				j.TrendFromYear = "chosen-start-year"
				j.TrendToYear = "chosen-end-year"
				j.TrendFrom = "chosen-start-count"
				j.TrendTo = "chosen-end-count"
				j.TrendChange = "chosen-change"
				values = append(values, j.TrendKind, j.TrendFromYear, j.TrendToYear, j.TrendFrom, j.TrendTo, j.TrendChange)
			}
			for i := 0; i < n; i++ {
				year := "chosen-year-" + strconv.Itoa(i)
				j.HeadcountYears = append(j.HeadcountYears, year)
				values = append(values, year)
			}
			for i := 0; i < n; i++ {
				row := SeriesRow{Kind: "chosen-kind-" + strconv.Itoa(i), Source: "chosen-source-" + strconv.Itoa(i)}
				values = append(values, row.Kind, row.Source)
				for k, year := range j.HeadcountYears {
					count := ""
					if n == 1 || (i+k)%2 == 0 {
						count = "chosen-cell-" + strconv.Itoa(i) + "-" + strconv.Itoa(k)
						values = append(values, count)
					}
					row.Figures = append(row.Figures, FigureCell{year, count})
				}
				j.HeadcountSeries = append(j.HeadcountSeries, row)
			}
			render("candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Judgments: &j}, values...)
		}
		for _, unknown := range []bool{false, true} {
			for _, filters := range [][]string{nil, {judging.TrendGrowing, judging.TrendFlat, judging.TrendShrinking}} {
				m := MemberRow{Trend: trend}
				values := append([]string{trend}, filters...)
				if trend != judging.TrendUnknown {
					m.TrendChange = "chosen-member-change"
					values = append(values, m.TrendChange)
				}
				render("prospect_list", ProspectListData{Members: []MemberRow{m}, ProspectList: ProspectListView{Filters: FiltersView{Trend: filters, TrendUnknown: unknown}}}, values...)
			}
		}
	}
}

// R-0G06-XDB4 R-0H83-B51T R-0TF3-4UGR
func TestJudgedTrendOnCandidateAndProspectList(t *testing.T) {
	cfg, _ := pageFixture(t)
	_, ids, err := cfg.Store.Attach(t.Context(), "chosen-trend-run", false, []store.Observation{{Scraper: "chosen", SourceID: "chosen-trend-row", Digest: "chosen-trend-digest", Reading: sources.Reading{Name: "chosen-trend-business", NameKey: "chosen-trend-business"}}})
	if err != nil {
		t.Fatal(err)
	}
	found := enrichers.LinkFound{Categories: []enrichers.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: []enrichers.Headcount{{Kind: sources.HeadcountEmployees, Scraper: "chosen-osha", Count: 100, Date: "2021-12-31"}, {Kind: sources.HeadcountEmployees, Scraper: "chosen-osha", Count: 131, Date: "2025-12-31"}}}
	if _, err = cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindLinkFacts, Source: enrichers.SourceLinks, Inputs: "chosen-trend-input", Found: encodedJSON(found)}}); err != nil {
		t.Fatal(err)
	}
	if err = judgePageFixture(t.Context(), cfg.Store); err != nil {
		t.Fatal(err)
	}
	stored, err := cfg.Store.GetJudgments(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	got := capturedCandidate(t, cfg, ids[0]).Judgments
	if stored.Trend != judging.TrendGrowing || got.Trend != judging.TrendGrowing || got.TrendKind != sources.HeadcountEmployees || got.TrendFromYear != "2021" || got.TrendToYear != "2025" || got.TrendFrom != "100" || got.TrendTo != "131" || got.TrendChange != "+31" {
		t.Fatal(got, stored)
	}
	if !reflect.DeepEqual(got.HeadcountYears, []string{"2021", "2022", "2023", "2024", "2025"}) || !reflect.DeepEqual(got.HeadcountSeries, []SeriesRow{{sources.HeadcountEmployees, "chosen-osha", []FigureCell{{"2021", "100"}, {"2022", ""}, {"2023", ""}, {"2024", ""}, {"2025", "131"}}}}) {
		t.Fatal(got)
	}
	pl, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "chosen-trend-members", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	members := pageProspectList(t, cfg, pl.ID).Members
	if len(members) != 1 || members[0].Trend != got.Trend || members[0].TrendChange != got.TrendChange {
		t.Fatal(members, got)
	}
}

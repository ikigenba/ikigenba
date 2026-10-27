package judging_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-25LD-ALZ9 R-26T9-ODPY
func TestTrendContract(t *testing.T) {
	var thresholds = []int8{judging.TrendYearsBack, judging.TrendWindowYears, judging.TrendMinYears, judging.TrendDivisor, judging.TrendFloor}
	if !reflect.DeepEqual(thresholds, []int8{3, 4, 2, 5, 5}) {
		t.Fatal(thresholds)
	}
	if !reflect.DeepEqual([]string{judging.TrendGrowing, judging.TrendFlat, judging.TrendShrinking, judging.TrendUnknown}, []string{"growing", "flat", "shrinking", "unknown"}) {
		t.Fatal("directions")
	}
	// Positional construction exercises the declared field order and types.
	s := judging.Series{sources.HeadcountEmployees, "source", []judging.Figure{{2022, 100}, {2025, 120}}}
	want := judging.Trend{judging.TrendGrowing, sources.HeadcountEmployees, 2022, 2025, 100, 120, 20}
	if got := judging.HeadcountTrend([]judging.Series{s}); got != want {
		t.Fatal(got)
	}
	if got := judging.HeadcountSeries(nil); got == nil || len(got) != 0 {
		t.Fatal(got)
	}
}

func annual(kind, scraper string, year int, count int64) sources.Headcount {
	return sources.Headcount{Kind: kind, Scraper: scraper, Count: count, AsOf: time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)}
}

// R-NRJU-AL6U
func TestHeadcountSeries(t *testing.T) {
	e, p, j := sources.HeadcountEmployees, sources.HeadcountParticipants, sources.HeadcountJobs
	h := annual
	s := func(kind, scraper string, figures ...judging.Figure) judging.Series {
		return judging.Series{Kind: kind, Scraper: scraper, Figures: figures}
	}
	early := h(p, "a", 2023, 21)
	early.AsOf = time.Date(2023, 6, 30, 0, 0, 0, 0, time.UTC)
	west := h(j, "a", 2024, 26)
	west.AsOf = time.Date(2024, 12, 31, 21, 0, 0, 0, time.FixedZone("west", -6*3600))
	for _, tc := range []struct {
		name  string
		input []sources.Headcount
		want  []judging.Series
	}{
		{"empty", nil, []judging.Series{}},
		{"ignored", []sources.Headcount{h(sources.HeadcountDrivers, "a", 2025, 40), h(sources.HeadcountSnippet, "a", 2025, 9), {Kind: j, Count: 26}, {Kind: e, Count: 15}, h(j, "a", 2020, 0), h(j, "a", 2020, -3), h("payroll", "a", 2020, 5)}, []judging.Series{}},
		{"gaps and order", []sources.Headcount{h(e, "a", 2025, 131), h(e, "a", 2021, 100), h(e, "a", 2023, 120)}, []judging.Series{s(e, "a", judging.Figure{2021, 100}, judging.Figure{2023, 120}, judging.Figure{2025, 131})}},
		{"sites repeat", []sources.Headcount{employee("a", 283, h(e, "a", 2025, 1).AsOf, 566000), employee("a", 120, h(e, "a", 2025, 1).AsOf, 240000), employee("a", 60, h(e, "a", 2025, 1).AsOf, 120000), employee("a", 35, h(e, "a", 2025, 1).AsOf, 70000), employee("a", 25, h(e, "a", 2025, 1).AsOf, 50000), employee("a", 357, h(e, "a", 2024, 1).AsOf, 715864), employee("a", 357, h(e, "a", 2024, 1).AsOf, 715864)}, []judging.Series{s(e, "a", judging.Figure{2024, 357}, judging.Figure{2025, 523})}},
		{"nil hours sites", []sources.Headcount{h(e, "a", 2025, 15), h(e, "a", 2022, 20), h(e, "a", 2025, 8)}, []judging.Series{s(e, "a", judging.Figure{2022, 20}, judging.Figure{2025, 23})}},
		{"different scrapers", []sources.Headcount{h(e, "a", 2023, 10), h(e, "b", 2023, 12)}, []judging.Series{s(e, "b", judging.Figure{2023, 12})}},
		{"largest within year", []sources.Headcount{early, h(p, "b", 2023, 12)}, []judging.Series{s(p, "a", judging.Figure{2023, 21})}},
		{"later larger within year", []sources.Headcount{early, h(p, "b", 2023, 30)}, []judging.Series{s(p, "b", judging.Figure{2023, 30})}},
		{"employee date does not decide", []sources.Headcount{employee("a", 50, early.AsOf), h(e, "a", 2023, 40)}, []judging.Series{s(e, "a", judging.Figure{2023, 50})}},
		{"two loans same year", []sources.Headcount{{Kind: j, Scraper: "a", Count: 30, AsOf: early.AsOf}, h(j, "b", 2023, 20)}, []judging.Series{s(j, "a", judging.Figure{2023, 30})}},
		{"equal counts first", []sources.Headcount{h(p, "a", 2023, 21), h(p, "b", 2023, 21)}, []judging.Series{s(p, "a", judging.Figure{2023, 21})}},
		{"newest scraper", []sources.Headcount{h(j, "b", 2021, 30), h(j, "a", 2020, 26), {Kind: j, Count: 99}}, []judging.Series{s(j, "b", judging.Figure{2020, 26}, judging.Figure{2021, 30})}},
		{"kind rank", []sources.Headcount{h(j, "c", 2020, 30), h(p, "b", 2024, 20), h(e, "a", 2025, 23)}, []judging.Series{s(e, "a", judging.Figure{2025, 23}), s(p, "b", judging.Figure{2024, 20}), s(j, "c", judging.Figure{2020, 30})}},
		{"UTC year", []sources.Headcount{west}, []judging.Series{s(j, "a", judging.Figure{2025, 26})}},
		{"sum saturation", []sources.Headcount{h(e, "a", 2025, math.MaxInt64), h(e, "a", 2025, 1)}, []judging.Series{s(e, "a", judging.Figure{2025, math.MaxInt64})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.input)
			if got := judging.HeadcountSeries(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
			after, _ := json.Marshal(tc.input)
			if string(before) != string(after) {
				t.Fatal("input changed")
			}
		})
	}
}

// R-NSRQ-OCXJ
func TestHeadcountTrend(t *testing.T) {
	for _, tc := range []struct {
		from, to  int64
		direction string
		change    int64
	}{
		{20, 23, judging.TrendFlat, 3}, {60, 40, judging.TrendShrinking, -20}, {47, 32, judging.TrendShrinking, -15}, {261, 243, judging.TrendFlat, -18}, {393, 534, judging.TrendGrowing, 141}, {327, 402, judging.TrendGrowing, 75},
		{100, 120, judging.TrendGrowing, 20}, {100, 119, judging.TrendFlat, 19}, {100, 80, judging.TrendShrinking, -20}, {100, 81, judging.TrendFlat, -19}, {25, 30, judging.TrendGrowing, 5}, {20, 24, judging.TrendFlat, 4}, {30, 35, judging.TrendFlat, 5}, {25, 20, judging.TrendShrinking, -5}, {20, 16, judging.TrendFlat, -4}, {5, 6, judging.TrendFlat, 1},
		{1, math.MaxInt64, judging.TrendGrowing, math.MaxInt64 - 1}, {math.MaxInt64, 1, judging.TrendShrinking, 1 - math.MaxInt64}, {math.MaxInt64 - 1, math.MaxInt64, judging.TrendFlat, 1}, {math.MaxInt64 / 2, math.MaxInt64, judging.TrendGrowing, math.MaxInt64 - math.MaxInt64/2}, {math.MaxInt64, math.MaxInt64 / 2, judging.TrendShrinking, math.MaxInt64/2 - math.MaxInt64}, {-1, math.MaxInt64, judging.TrendGrowing, math.MaxInt64}, {math.MinInt64, 1, judging.TrendGrowing, math.MaxInt64}, {math.MaxInt64, -2, judging.TrendShrinking, math.MinInt64},
	} {
		input := []judging.Series{{Kind: sources.HeadcountEmployees, Figures: []judging.Figure{{2022, tc.from}, {2025, tc.to}}}}
		want := judging.Trend{tc.direction, sources.HeadcountEmployees, 2022, 2025, tc.from, tc.to, tc.change}
		if got := judging.HeadcountTrend(input); got != want {
			t.Fatalf("%+v got %+v want %+v", tc, got, want)
		}
	}
}

// R-NSRQ-OCXJ
func TestTrendComparisonWindowAndKind(t *testing.T) {
	s := func(kind string, figures ...judging.Figure) judging.Series {
		return judging.Series{Kind: kind, Figures: figures}
	}
	e, p, j := sources.HeadcountEmployees, sources.HeadcountParticipants, sources.HeadcountJobs
	unknown := judging.Trend{Direction: judging.TrendUnknown}
	for _, tc := range []struct {
		name  string
		input []judging.Series
		want  judging.Trend
	}{
		{"nil", nil, unknown}, {"empty", []judging.Series{}, unknown}, {"no figures", []judging.Series{s(e)}, unknown},
		{"one", []judging.Series{s(e, judging.Figure{2025, 23})}, unknown},
		{"one year", []judging.Series{s(e, judging.Figure{2024, 20}, judging.Figure{2025, 23})}, unknown},
		{"old", []judging.Series{s(e, judging.Figure{2019, 10}, judging.Figure{2020, 40}, judging.Figure{2025, 60})}, unknown},
		{"three before four", []judging.Series{s(e, judging.Figure{2021, 100}, judging.Figure{2022, 110}, judging.Figure{2025, 131})}, judging.Trend{judging.TrendFlat, e, 2022, 2025, 110, 131, 21}},
		{"four before two unsorted", []judging.Series{s(e, judging.Figure{2025, 131}, judging.Figure{2023, 120}, judging.Figure{2021, 100})}, judging.Trend{judging.TrendGrowing, e, 2021, 2025, 100, 131, 31}},
		{"two before one", []judging.Series{s(e, judging.Figure{2020, 50}, judging.Figure{2023, 140}, judging.Figure{2024, 150}, judging.Figure{2025, 141})}, judging.Trend{judging.TrendFlat, e, 2023, 2025, 140, 141, 1}},
		{"duplicate years first", []judging.Series{s(e, judging.Figure{2022, 20}, judging.Figure{2022, 30}, judging.Figure{2025, 23}, judging.Figure{2025, 40})}, judging.Trend{judging.TrendFlat, e, 2022, 2025, 20, 23, 3}},
		{"fall through", []judging.Series{s(e, judging.Figure{2025, 23}), s(p, judging.Figure{2022, 20}, judging.Figure{2025, 30})}, judging.Trend{judging.TrendGrowing, p, 2022, 2025, 20, 30, 10}},
		{"given kind order", []judging.Series{s(j, judging.Figure{2020, 26}, judging.Figure{2023, 40}), s(e, judging.Figure{2022, 20}, judging.Figure{2025, 23})}, judging.Trend{judging.TrendGrowing, j, 2020, 2023, 26, 40, 14}},
		{"year lower bound", []judging.Series{s(e, judging.Figure{math.MinInt64, 20}, judging.Figure{math.MinInt64 + 3, 30})}, judging.Trend{judging.TrendGrowing, e, math.MinInt64, math.MinInt64 + 3, 20, 30, 10}},
		{"year overflow gap", []judging.Series{s(e, judging.Figure{math.MinInt64, 20}, judging.Figure{math.MaxInt64, 30})}, unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.input)
			if got := judging.HeadcountTrend(tc.input); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
			after, _ := json.Marshal(tc.input)
			if string(before) != string(after) {
				t.Fatal("input changed")
			}
		})
	}
}

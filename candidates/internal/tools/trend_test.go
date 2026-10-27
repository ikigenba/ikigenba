package tools_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-LER2-G790 R-LFYY-TYZP R-LIER-LIH3
func TestTrendCandidateMembersAndOrdering(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(6)
	hs := [][]enrichers.Headcount{
		{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 100, Date: "2021-12-31"}, {Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 131, Date: "2025-12-31"}, {Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"}},
		{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 60, Date: "2022-12-31"}, {Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 40, Date: "2025-12-31"}},
		{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 20, Date: "2022-12-31"}, {Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 20, Date: "2025-12-31"}},
		{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"}},
		{},
		{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 20, Date: "2022-12-31"}, {Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 23, Date: "2025-12-31"}},
	}
	for i, h := range hs {
		putBasisResult(t, f, ids[i], enrichers.KindLinkFacts, enrichers.LinkFound{Categories: []enrichers.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: h})
	}
	if err := walkJudgments(t.Context(), f.st, f.now); err != nil {
		t.Fatal(err)
	}
	dirs := []string{judging.TrendGrowing, judging.TrendShrinking, judging.TrendFlat, judging.TrendUnknown, judging.TrendUnknown, judging.TrendFlat}
	changes := []*int64{new(int64(31)), new(int64(-20)), new(int64(0)), nil, nil, new(int64(3))}
	for i, id := range ids {
		d := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
		j := d.Judgments
		if j == nil || j.Trend != dirs[i] || !reflect.DeepEqual(j.TrendChange, changes[i]) {
			t.Fatal(i, j)
		}
		if i == 0 {
			want := []tools.Series{{Kind: sources.HeadcountEmployees, Scraper: oshaita.Name, Figures: []tools.Figure{{Year: 2021, Count: 100}, {Year: 2025, Count: 131}}}, {Kind: sources.HeadcountJobs, Scraper: ppp.Name, Figures: []tools.Figure{{Year: 2020, Count: 26}}}}
			if !reflect.DeepEqual(j.HeadcountSeries, want) || j.TrendKind != sources.HeadcountEmployees || j.TrendFromYear == nil || *j.TrendFromYear != 2021 || j.TrendToYear == nil || *j.TrendToYear != 2025 || j.TrendFrom == nil || *j.TrendFrom != 100 || j.TrendTo == nil || *j.TrendTo != 131 {
				t.Fatal(j)
			}
		}
		if changes[i] == nil && (j.TrendKind != "" || j.TrendFromYear != nil || j.TrendToYear != nil || j.TrendFrom != nil || j.TrendTo != nil) {
			t.Fatal(j)
		}
		if i == 3 {
			want := []tools.Series{{Kind: sources.HeadcountJobs, Scraper: ppp.Name, Figures: []tools.Figure{{Year: 2020, Count: 26}}}}
			if !reflect.DeepEqual(j.HeadcountSeries, want) {
				t.Fatal(j)
			}
		}
		if i == 4 && (j.HeadcountSeries == nil || len(j.HeadcountSeries) != 0) {
			t.Fatal(j)
		}
	}
	for _, dir := range []string{store.DirectionAsc, store.DirectionDesc} {
		order := []tools.OrderArgs{{Field: store.OrderTrend, Direction: dir}}
		k := decodeList[tools.ProspectList](t, f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "trend order", Order: &order, Size: new(100)}))
		if !reflect.DeepEqual(k.Order, []store.ProspectListOrder{{Field: store.OrderTrend, Direction: dir}}) {
			t.Fatal(k)
		}
		got := peekBasis(t, f, k.ID)
		if len(got.Members) != len(ids) {
			t.Fatal(got)
		}
		for i, m := range got.Members {
			n := slices.Index(ids, m.Candidate.ID)
			if n < 0 || m.Trend != dirs[n] || !reflect.DeepEqual(m.TrendChange, changes[n]) {
				t.Fatal(m)
			}
			if i > 0 {
				p := got.Members[i-1]
				if p.TrendChange == nil && m.TrendChange != nil {
					t.Fatal(got.Members)
				}
				if p.TrendChange != nil && m.TrendChange != nil && ((dir == store.DirectionAsc && *p.TrendChange > *m.TrendChange) || (dir == store.DirectionDesc && *p.TrendChange < *m.TrendChange)) {
					t.Fatal(got.Members)
				}
			}
		}
	}
}

// R-LH6V-7QQE
func TestTrendFiltersValidateNormalizeAndReplace(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(4)
	for i, counts := range [][2]int64{{20, 40}, {40, 20}, {20, 20}} {
		putBasisResult(t, f, ids[i], enrichers.KindLinkFacts, enrichers.LinkFound{Categories: []enrichers.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: []enrichers.Headcount{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: counts[0], Date: "2022-12-31"}, {Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: counts[1], Date: "2025-12-31"}}})
	}
	if err := walkJudgments(t.Context(), f.st, f.now); err != nil {
		t.Fatal(err)
	}
	all := f.create("all trends")
	baseline := peekBasis(t, f, all.ID).Members
	vals := []string{judging.TrendFlat, judging.TrendGrowing, judging.TrendFlat}
	for _, unknown := range []bool{false, true} {
		k := decodeList[tools.ProspectList](t, f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "filtered trends", Filters: &tools.FilterArgs{Categories: []string{}, Trend: &vals, TrendUnknown: unknown}}))
		want := []string{judging.TrendGrowing, judging.TrendFlat}
		if !reflect.DeepEqual(k.Filters.Trend, want) || k.Filters.TrendUnknown != unknown {
			t.Fatal(k)
		}
		got := peekBasis(t, f, k.ID)
		if !reflect.DeepEqual(got.ProspectList.Filters, k.Filters) {
			t.Fatal(got)
		}
		expected := []tools.Member{}
		for _, m := range baseline {
			if slices.Contains(want, m.Trend) || (unknown && m.Trend == judging.TrendUnknown) {
				expected = append(expected, m)
			}
		}
		if !reflect.DeepEqual(got.Members, expected) {
			t.Fatal(got.Members, expected)
		}
		for _, args := range []map[string]any{{"prospect_list": k.ID}, {"prospect_list": k.ID, "filters": nil}} {
			v := decodeList[tools.ProspectList](t, f.call("edit_prospect_list", args))
			if !reflect.DeepEqual(v.Filters, k.Filters) {
				t.Fatal(v)
			}
		}
		for _, filters := range []map[string]any{{}, {"government": true}} {
			v := decodeList[tools.ProspectList](t, f.call("edit_prospect_list", map[string]any{"prospect_list": k.ID, "filters": filters}))
			if v.Filters.Trend != nil {
				t.Fatal(v)
			}
		}
	}
	empty := []string{}
	k := decodeList[tools.ProspectList](t, f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "empty trend", Filters: &tools.FilterArgs{Categories: []string{}, Trend: &empty}}))
	b, err := json.Marshal(k.Filters)
	if err != nil || string(b) != "{}" {
		t.Fatal(string(b), err)
	}
	for _, bad := range []string{judging.TrendUnknown, "chosen-invalid-trend"} {
		v := []string{bad}
		f.d.SetFailing(true)
		refusal(t, f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "bad trend", Filters: &tools.FilterArgs{Categories: []string{}, Trend: &v}}), passes.RefusalInvalid, tools.RefuseInvalid)
		refusal(t, f.call("edit_prospect_list", tools.EditProspectListArgs{ProspectList: k.ID, Filters: &tools.FilterArgs{Categories: []string{}, Trend: &v}}), passes.RefusalInvalid, tools.RefuseInvalid)
		f.d.SetFailing(false)
	}
}

// R-07OD-NXA2
func TestTrendNullAcceptedThroughMCP(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	for _, trend := range []any{nil, []string{}} {
		result := f.call("create_prospect_list", map[string]any{"name": "no trend restriction", "filters": map[string]any{"trend": trend}})
		if result.IsError() {
			t.Fatal(decode(t, result))
		}
		k := decodeList[tools.ProspectList](t, result)
		got := peekBasis(t, f, k.ID)
		if got.ProspectList.Filters.Trend != nil || len(got.Members) != len(ids) {
			t.Fatal(got)
		}
		result = f.call("edit_prospect_list", map[string]any{"prospect_list": k.ID, "filters": map[string]any{"trend": trend}})
		if result.IsError() {
			t.Fatal(decode(t, result))
		}
		if got = peekBasis(t, f, k.ID); got.ProspectList.Filters.Trend != nil || len(got.Members) != len(ids) {
			t.Fatal(got)
		}
	}
}

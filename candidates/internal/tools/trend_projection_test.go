package tools

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-07OD-NXA2
func TestTrendFilterPresence(t *testing.T) {
	for _, raw := range []string{`{}`, `{"trend":null}`, `{"trend":[]}`, `{"trend":["shrinking","growing"],"trend_unknown":true}`, `{"trend":["unknown"]}`, `{"trend":["chosen-invalid"]}`} {
		var a FilterArgs
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			t.Fatal(err)
		}
		got := filterOf(a)
		var want []string
		if a.Trend != nil {
			want = append([]string{}, (*a.Trend)...)
		}
		if !reflect.DeepEqual(got.Trend, want) || got.TrendUnknown != a.TrendUnknown {
			t.Fatal(got, a)
		}
		valid := true
		for _, s := range want {
			if s != judging.TrendGrowing && s != judging.TrendFlat && s != judging.TrendShrinking {
				valid = false
			}
		}
		if store.ValidProspectListFilters(got) != valid {
			t.Fatal(got)
		}
	}
}

// R-ZXX6-LRCI R-ZWPA-7ZLT R-ZZ52-ZJ37
func TestTrendAnswerEncodings(t *testing.T) {
	_ = Series{"kind", "scraper", []Figure{{2021, 100}}}
	_ = Figure{2025, 131}
	for _, tc := range []struct {
		v    any
		want string
	}{
		{Series{}, `{"kind":"","scraper":"","figures":[]}`},
		{Series{Kind: "kind", Scraper: "scraper", Figures: []Figure{}}, `{"kind":"kind","scraper":"scraper","figures":[]}`},
		{Series{Kind: "kind", Scraper: "scraper", Figures: []Figure{{Year: 2021, Count: 100}, {Year: 2025, Count: 131}}}, `{"kind":"kind","scraper":"scraper","figures":[{"year":2021,"count":100},{"year":2025,"count":131}]}`},
		{Judgments{HeadcountSeries: []Series{{Kind: "kind", Scraper: "scraper", Figures: []Figure{{Year: 2021, Count: 100}}}}, Trend: "direction", TrendKind: "kind", TrendFromYear: new(int64(2021)), TrendToYear: new(int64(2025)), TrendFrom: new(int64(100)), TrendTo: new(int64(131)), TrendChange: new(int64(31))}, `{"operating_evidence":[],"headcount_series":[{"kind":"kind","scraper":"scraper","figures":[{"year":2021,"count":100}]}],"trend":"direction","trend_kind":"kind","trend_from_year":2021,"trend_to_year":2025,"trend_from":100,"trend_to":131,"trend_change":31,"excluded":false,"rules":0,"calculated":""}`},
		{Judgments{TrendChange: new(int64(0))}, `{"operating_evidence":[],"headcount_series":[],"trend":"","trend_change":0,"excluded":false,"rules":0,"calculated":""}`},
	} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Fatal(string(b), tc.want, err)
		}
	}
	for _, p := range []*int64{nil, new(int64(0)), new(int64(-20))} {
		b, err := json.Marshal(Member{Trend: "chosen-trend", TrendChange: p})
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]json.RawMessage
		if err = json.Unmarshal(b, &obj); err != nil {
			t.Fatal(err)
		}
		if string(obj["trend"]) != `"chosen-trend"` {
			t.Fatal(string(b))
		}
		v, ok := obj["trend_change"]
		if ok != (p != nil) {
			t.Fatal(string(b))
		}
		if p != nil {
			want, _ := json.Marshal(*p)
			if string(v) != string(want) {
				t.Fatal(string(b))
			}
		}
	}
}

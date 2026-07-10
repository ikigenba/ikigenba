package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dop251/goja"
)

type landingRow struct {
	Slug          string `json:"slug"`
	URL           string `json:"url"`
	Public        bool   `json:"public"`
	CreatedBy     string `json:"createdBy"`
	CreatedAt     string `json:"createdAt"`
	CreatedAtSort string `json:"createdAtSort"`
}

func landingEval(t *testing.T, expression string, out any) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "share", "www", "static", "landing.js"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := goja.New()
	if _, err := runtime.RunString(string(source)); err != nil {
		t.Fatal(err)
	}
	value, err := runtime.RunString("JSON.stringify(" + expression + ")")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(value.String()), out); err != nil {
		t.Fatal(err)
	}
}

func landingJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func fixtures() []landingRow {
	return []landingRow{
		{Slug: "alpha", CreatedBy: "zoe", CreatedAt: "Dec 2025", CreatedAtSort: "2025-12-01T00:00:00Z"},
		{Slug: "alpine", CreatedBy: "amy", CreatedAt: "Jan 2020", CreatedAtSort: "2020-01-01T00:00:00Z"},
		{Slug: "beta", CreatedBy: "amy", CreatedAt: "Nov 2024", CreatedAtSort: "2024-11-01T00:00:00Z"},
	}
}

func TestFilterSitesSubsequenceAndOrder(t *testing.T) {
	// R-HU67-LJBW
	var got []landingRow
	landingEval(t, "SitesLanding.filterSites("+landingJSON(t, fixtures())+", 'Aa')", &got)
	if len(got) != 2 || got[0].Slug != "alpha" || got[1].Slug != "beta" {
		t.Fatalf("filter result = %#v", got)
	}
}

func TestFilterSitesIsCaseInsensitiveAndEmptyCopiesAll(t *testing.T) {
	// R-HVE3-ZB2L
	var got []landingRow
	landingEval(t, "SitesLanding.filterSites("+landingJSON(t, fixtures())+", 'LP')", &got)
	if len(got) != 2 || got[0].Slug != "alpha" || got[1].Slug != "alpine" {
		t.Fatalf("case-insensitive filter result = %#v", got)
	}
}

func TestSortRowsByNameAndTieBreak(t *testing.T) {
	// R-HWM0-D2TA
	rows := []landingRow{{Slug: "zeta", CreatedBy: "same"}, {Slug: "alpha", CreatedBy: "same"}, {Slug: "beta", CreatedBy: "other"}}
	var got []landingRow
	landingEval(t, "SitesLanding.sortRows("+landingJSON(t, rows)+", 'createdBy', 'asc')", &got)
	if got[0].Slug != "beta" || got[1].Slug != "alpha" || got[2].Slug != "zeta" {
		t.Fatalf("sorted rows = %#v", got)
	}
}

func TestSortRowsCreatedAtUsesSortableTimestamp(t *testing.T) {
	// R-I1HL-W5S2
	var got []landingRow
	landingEval(t, "SitesLanding.sortRows("+landingJSON(t, fixtures())+", 'createdAt', 'asc')", &got)
	if got[0].Slug != "alpine" || got[1].Slug != "beta" || got[2].Slug != "alpha" {
		t.Fatalf("chronological sort = %#v", got)
	}
}

func TestSortRowsDescending(t *testing.T) {
	// R-HXTW-QUJZ
	var got []landingRow
	landingEval(t, "SitesLanding.sortRows("+landingJSON(t, fixtures())+", 'name', 'desc')", &got)
	if got[0].Slug != "beta" || got[2].Slug != "alpha" {
		t.Fatalf("descending name sort = %#v", got)
	}
}

func TestNextSortTogglesActiveColumnAndStartsNewColumnAscending(t *testing.T) {
	// R-HZ1T-4MAO
	var got struct{ SortKey, Dir string }
	landingEval(t, "SitesLanding.nextSort({sortKey:'name',dir:'asc'}, 'name')", &got)
	if got.SortKey != "name" || got.Dir != "desc" {
		t.Fatalf("toggle = %#v", got)
	}
	// R-I2PI-9XIR
	landingEval(t, "SitesLanding.nextSort({sortKey:'name',dir:'desc'}, 'createdBy')", &got)
	if got.SortKey != "createdBy" || got.Dir != "asc" {
		t.Fatalf("new column = %#v", got)
	}
}

func TestPaginateSlicesRequestedPage(t *testing.T) {
	// R-I3XE-NP9G
	rows := make([]landingRow, 12)
	for i := range rows {
		rows[i].Slug = string(rune('a' + i))
	}
	var got []landingRow
	landingEval(t, "SitesLanding.paginate("+landingJSON(t, rows)+", 2, 10)", &got)
	if len(got) != 2 || got[0].Slug != "k" || got[1].Slug != "l" {
		t.Fatalf("page = %#v", got)
	}
}

func TestDefaultState(t *testing.T) {
	// R-I55B-1H05
	var got struct {
		Query, SortKey, Dir string
		Page                int
	}
	landingEval(t, "SitesLanding.defaultState()", &got)
	if got.Query != "" || got.SortKey != "createdAt" || got.Dir != "desc" || got.Page != 1 {
		t.Fatalf("default = %#v", got)
	}
}

func TestReduceResetsAndPreservesPageAsAppropriate(t *testing.T) {
	// R-I6D7-F8QU
	var query struct {
		Query, SortKey, Dir string
		Page                int
	}
	landingEval(t, "SitesLanding.reduce({query:'a',sortKey:'name',dir:'asc',page:3},{type:'setQuery',query:'b'})", &query)
	if query.Query != "b" || query.Page != 1 || query.SortKey != "name" {
		t.Fatalf("set query = %#v", query)
	}
	// R-I7L3-T0HJ
	var page struct {
		Query, SortKey, Dir string
		Page                int
	}
	landingEval(t, "SitesLanding.reduce({query:'a',sortKey:'name',dir:'asc',page:3},{type:'setPage',page:2})", &page)
	if page.Page != 2 || page.Query != "a" || page.SortKey != "name" {
		t.Fatalf("set page = %#v", page)
	}
}

func TestReduceClearRestoresDefaultState(t *testing.T) {
	// R-I8T0-6S88
	var got struct {
		Query, SortKey, Dir string
		Page                int
	}
	landingEval(t, "SitesLanding.reduce({query:'x',sortKey:'name',dir:'asc',page:7},{type:'clear'})", &got)
	if got.Query != "" || got.SortKey != "createdAt" || got.Dir != "desc" || got.Page != 1 {
		t.Fatalf("clear = %#v", got)
	}
}

func TestComputeViewEmptyAndNoMatchStates(t *testing.T) {
	// R-IA0W-KJYX
	var empty struct{ ShowControls, Empty, NoMatch, ShowPager bool }
	landingEval(t, "SitesLanding.computeView([], SitesLanding.defaultState())", &empty)
	if empty.ShowControls || !empty.Empty || empty.NoMatch || empty.ShowPager {
		t.Fatalf("empty view = %#v", empty)
	}
	// R-IB8S-YBPM
	var noMatch struct{ ShowControls, Empty, NoMatch, ShowPager bool }
	landingEval(t, "SitesLanding.computeView("+landingJSON(t, fixtures())+", {query:'zzz',sortKey:'name',dir:'asc',page:1})", &noMatch)
	if !noMatch.ShowControls || noMatch.Empty || !noMatch.NoMatch || noMatch.ShowPager {
		t.Fatalf("no-match view = %#v", noMatch)
	}
}

func TestComputeViewPaginatesClampsAndReportsRange(t *testing.T) {
	// R-7V8B-GA0T
	rows := make([]landingRow, 11)
	for i := range rows {
		rows[i] = landingRow{Slug: string(rune('a' + i)), CreatedAtSort: "2025-01-01T00:00:00Z"}
	}
	var got struct {
		Rows                                []landingRow
		ShowPager                           bool
		Page, PageCount, RangeFrom, RangeTo int
	}
	landingEval(t, "SitesLanding.computeView("+landingJSON(t, rows)+", {query:'',sortKey:'name',dir:'asc',page:99})", &got)
	if !got.ShowPager || got.Page != 2 || got.PageCount != 2 || got.RangeFrom != 11 || got.RangeTo != 11 || len(got.Rows) != 1 || got.Rows[0].Slug != "k" {
		t.Fatalf("view = %#v", got)
	}
}

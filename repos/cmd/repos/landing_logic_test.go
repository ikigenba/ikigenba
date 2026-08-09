package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

func TestLandingJSFilterUsesSubsequenceAcrossTheKey(t *testing.T) {
	// R-S7DH-V7ZG
	const rows = `[{key:"code/demo"},{key:"code/alpha"},{key:"sites/blog"}]`
	var got []string
	runLandingJS(t, `ReposLanding.filterRepos(`+rows+`,"cdm").map(row => row.key)`, &got)
	if want := []string{"code/demo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filter cdm = %v, want %v", got, want)
	}
	runLandingJS(t, `ReposLanding.filterRepos(`+rows+`,"c/a").map(row => row.key)`, &got)
	if want := []string{"code/alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filter c/a = %v, want %v", got, want)
	}
}

func TestLandingJSFilterIgnoresCase(t *testing.T) {
	// R-S8LE-8ZQ5
	const rows = `[{key:"code/demo"},{key:"sites/blog"}]`
	for query, want := range map[string]string{"CDM": "code/demo", "Sites/B": "sites/blog"} {
		var got []string
		runLandingJS(t, `ReposLanding.filterRepos(`+rows+`,`+jsString(t, query)+`).map(row => row.key)`, &got)
		if !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("filter %q = %v, want [%s]", query, got, want)
		}
	}
}

func TestLandingJSEmptyFilterPreservesEveryRow(t *testing.T) {
	// R-S9TA-MRGU
	var got []string
	runLandingJS(t, `ReposLanding.filterRepos([{key:"sites/z"},{key:"code/a"},{key:"prompts/m"}],"").map(row => row.key)`, &got)
	want := []string{"sites/z", "code/a", "prompts/m"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty filter = %v, want %v", got, want)
	}
}

func TestLandingJSFilterNeverReranksMatches(t *testing.T) {
	// R-SB17-0J7J
	var got []string
	runLandingJS(t, `ReposLanding.filterRepos([{key:"sites/zapp"},{key:"code/app"}],"app").map(row => row.key)`, &got)
	want := []string{"sites/zapp", "code/app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter order = %v, want %v", got, want)
	}
}

func TestLandingJSSortOrdersKeysInBothDirections(t *testing.T) {
	// R-SC93-EAY8
	const rows = `[{key:"sites/blog",name:"a"},{key:"code/demo",name:"z"},{key:"prompts/weekly",name:"m"}]`
	var ascending, descending []string
	runLandingJS(t, `ReposLanding.sortRows(`+rows+`,"asc").map(row => row.key)`, &ascending)
	runLandingJS(t, `ReposLanding.sortRows(`+rows+`,"desc").map(row => row.key)`, &descending)
	wantAscending := []string{"code/demo", "prompts/weekly", "sites/blog"}
	wantDescending := []string{"sites/blog", "prompts/weekly", "code/demo"}
	if !reflect.DeepEqual(ascending, wantAscending) || !reflect.DeepEqual(descending, wantDescending) {
		t.Fatalf("sort asc=%v desc=%v, want asc=%v desc=%v", ascending, descending, wantAscending, wantDescending)
	}
}

func TestLandingJSNextSortAlwaysFlipsDirection(t *testing.T) {
	// R-SDGZ-S2OX
	var got []string
	runLandingJS(t, `[ReposLanding.nextSort({dir:"asc"}).dir, ReposLanding.nextSort({dir:"desc"}).dir]`, &got)
	if want := []string{"desc", "asc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("next sort directions = %v, want %v", got, want)
	}
}

func TestLandingJSPaginateUsesExactSliceBoundaries(t *testing.T) {
	// R-SEOW-5UFM
	var got [][]int
	runLandingJS(t, `(() => {
		const rows = Array.from({length:34}, (_, index) => index + 1);
		return [ReposLanding.paginate(rows,1,10), ReposLanding.paginate(rows,4,10), ReposLanding.paginate(rows,5,10)];
	})()`, &got)
	if !reflect.DeepEqual(got[0], []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) ||
		!reflect.DeepEqual(got[1], []int{31, 32, 33, 34}) || len(got[2]) != 0 {
		t.Fatalf("pagination boundaries = %v", got)
	}
}

func TestLandingJSViewShowsPagerOnlyBeyondTenMatches(t *testing.T) {
	// R-SFWS-JM6B
	var got []bool
	runLandingJS(t, `(() => {
		const rows = count => Array.from({length:count}, (_, index) => ({key:"code/"+String(index).padStart(2,"0")}));
		return [ReposLanding.computeView(rows(10),ReposLanding.defaultState()).showPager,
			ReposLanding.computeView(rows(11),ReposLanding.defaultState()).showPager];
	})()`, &got)
	if want := []bool{false, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("showPager for 10 and 11 rows = %v, want %v", got, want)
	}
}

func TestLandingJSViewDerivesPageLabelsRangesAndClamps(t *testing.T) {
	// R-SH4O-XDX0
	type view struct {
		Page       int    `json:"page"`
		PageCount  int    `json:"pageCount"`
		PageLabel  string `json:"pageLabel"`
		RangeFrom  int    `json:"rangeFrom"`
		RangeTo    int    `json:"rangeTo"`
		RangeLabel string `json:"rangeLabel"`
	}
	var got []view
	runLandingJS(t, `(() => {
		const rows = Array.from({length:34}, (_, index) => ({key:"code/"+String(index).padStart(2,"0")}));
		return [ReposLanding.computeView(rows,{query:"",dir:"asc",page:4}),
			ReposLanding.computeView(rows,{query:"",dir:"asc",page:9})];
	})()`, &got)
	want := view{Page: 4, PageCount: 4, PageLabel: "Page 4 of 4", RangeFrom: 31, RangeTo: 34, RangeLabel: "31–34 of 34"}
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Fatalf("page views = %#v, want two %#v", got, want)
	}
}

func TestLandingJSViewDistinguishesEmptyFromNoMatch(t *testing.T) {
	// R-SICL-B5NP
	type emptyState struct {
		ShowControls bool `json:"showControls"`
		Empty        bool `json:"empty"`
		NoMatch      bool `json:"noMatch"`
		ShowPager    bool `json:"showPager"`
	}
	var got []emptyState
	runLandingJS(t, `[ReposLanding.computeView([],ReposLanding.defaultState()),
		ReposLanding.computeView([{key:"code/demo"}],{query:"zzz",dir:"asc",page:1})]`, &got)
	want := []emptyState{
		{ShowControls: false, Empty: true, NoMatch: false, ShowPager: false},
		{ShowControls: true, Empty: false, NoMatch: true, ShowPager: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty states = %#v, want %#v", got, want)
	}
}

func TestLandingJSClearRestoresTheExactDefaultState(t *testing.T) {
	// R-SJKH-OXEE
	type state struct {
		Query string `json:"query"`
		Dir   string `json:"dir"`
		Page  int    `json:"page"`
	}
	var got []state
	runLandingJS(t, `[ReposLanding.defaultState(),
		ReposLanding.reduce({query:"demo",dir:"desc",page:7},{type:"clear"})]`, &got)
	want := state{Query: "", Dir: "asc", Page: 1}
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Fatalf("default and clear = %#v, want two %#v", got, want)
	}
}

func TestLandingJSReducerResetsOnlyTheRequiredPages(t *testing.T) {
	// R-SKSE-2P53
	var got []int
	runLandingJS(t, `(() => {
		const state = {query:"",dir:"asc",page:3};
		return [ReposLanding.reduce(state,{type:"setQuery",query:"demo"}).page,
			ReposLanding.reduce(state,{type:"setSort",dir:"desc"}).page,
			ReposLanding.reduce(state,{type:"setPage",page:2}).page];
	})()`, &got)
	if want := []int{1, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reduced pages = %v, want %v", got, want)
	}
}

func TestLandingJSCloneURLHasExactlyOneJoinSlash(t *testing.T) {
	// R-SN86-U8MH
	var got string
	runLandingJS(t, `ReposLanding.cloneURL("https://acct.example.com","/srv/repos/git/code/demo.git")`, &got)
	want := "https://acct.example.com/srv/repos/git/code/demo.git"
	if got != want {
		t.Fatalf("clone URL = %q, want %q", got, want)
	}
}

func TestLandingShipsAndLoadsItsJavaScript(t *testing.T) {
	// R-SOG3-80D6
	path := filepath.Join("..", "..", "share", "www", "static", "landing.js")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat shipped landing JavaScript: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("shipped landing JavaScript is empty")
	}
	body := newLandingFixture(t).render(t)
	if !strings.Contains(body, `<script src="static/landing.js" defer></script>`) {
		t.Fatalf("rendered landing does not defer its shipped JavaScript:\n%s", body)
	}
}

func runLandingJS(t *testing.T, expression string, target any) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "share", "www", "static", "landing.js"))
	if err != nil {
		t.Fatalf("read landing JavaScript: %v", err)
	}
	runtime := goja.New()
	if _, err := runtime.RunString(string(source)); err != nil {
		t.Fatalf("load landing JavaScript: %v", err)
	}
	value, err := runtime.RunString("JSON.stringify(" + expression + ")")
	if err != nil {
		t.Fatalf("evaluate landing JavaScript %q: %v", expression, err)
	}
	if err := json.Unmarshal([]byte(value.String()), target); err != nil {
		t.Fatalf("decode landing JavaScript result %s: %v", value.String(), err)
	}
}

func jsString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

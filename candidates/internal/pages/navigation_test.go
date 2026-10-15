package pages

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-TE20-WB0X R-TNT7-YGYH R-THPQ-1M90 R-TCU4-IJA8 R-TF9X-A2RM R-TQ90-Q0FV
func TestNavigationWalksAndTail(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit + 1, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 1, 3*paging.DefaultLimit + 2} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			cfg, ranked, passes := navigationFixture(t, count)
			// Every key ties, including both sides of every window boundary.
			for i := 1; i < count; i++ {
				if ranked[i].Assessment.OperatingKey != ranked[0].Assessment.OperatingKey || !passes[i].Started.Equal(passes[0].Started) {
					t.Fatal("fixture must exercise tie breakers")
				}
			}
			for _, short := range []bool{true, false} {
				for _, other := range navigationOtherValues(ranked, passes, !short) {
					t.Run(strconv.FormatBool(short)+"/"+other, func(t *testing.T) {
						navigationWalk(t, cfg, ranked, passes, short, other)
					})
				}
			}
		})
	}
}

func navigationFixture(t *testing.T, count int) (Config, []store.Ranked, []store.Pass) {
	t.Helper()
	cfg, _ := pageFixture(t)
	ctx := context.Background()
	hi := int64(24000000)
	for i := 0; i < count; i++ {
		key := strconv.Itoa(i)
		name := "navigation-business-" + key
		reading := sources.Reading{Name: name, Names: []string{name}, NameKey: name, Zip: "55072", Fleet: true, Filed: fixtureTime(), Class: sources.ClassPrivate, Estimate: &sources.Estimate{Source: "navigation", Low: 14400000, High: &hi, Confidence: .7, Filed: fixtureTime(), Formula: 1}}
		_, ids, err := cfg.Store.Attach(ctx, "navigation-run", false, []store.Observation{{Scraper: "navigation", SourceID: key, Digest: key, Reading: reading}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = cfg.Store.Judge(ctx, ids); err != nil {
			t.Fatal(err)
		}
		p, err := cfg.Store.AddPass(ctx, store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "navigation-starter"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = cfg.Store.FinishPass(ctx, p.ID, store.Counts{Rows: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	ranked, err := cfg.Store.Shortlist(ctx, store.ShortlistFilter{}, 0, "", count+1)
	if err != nil || len(ranked) != count {
		t.Fatal("shortlist fixture", len(ranked), err)
	}
	passes, err := cfg.Store.ListPasses(ctx, "", "", count+1)
	if err != nil || len(passes) != count {
		t.Fatal("passes fixture", len(passes), err)
	}
	return cfg, ranked, passes
}

func navigationToken(ranked []store.Ranked, passes []store.Pass, short, before bool, index int) string {
	suffix := ""
	if before {
		suffix = "-before"
	}
	if short {
		e := ranked[index]
		return (paging.Token{Tool: "shortlist" + suffix}).Encode(e.Assessment.OperatingKey, e.Candidate.ID)
	}
	p := passes[index]
	return (paging.Token{Tool: "passes" + suffix}).Encode(p.Started.UTC().Format(time.RFC3339), p.ID)
}

func navigationOtherValues(ranked []store.Ranked, passes []store.Pass, short bool) []string {
	values := []string{"", navigationToken(ranked, passes, short, false, paging.DefaultLimit-1), "last"}
	if len(ranked) > paging.DefaultLimit+1 {
		values = append(values, navigationToken(ranked, passes, short, true, len(ranked)-1))
	}
	return values
}

func navigationBounds(ranked []store.Ranked, passes []store.Pass, short bool, value string) (int, int) {
	count := len(ranked)
	if value == "" {
		return 0, min(count, paging.DefaultLimit)
	}
	if value == "last" {
		return count - paging.DefaultLimit, count
	}
	for i := 0; i < count; i++ {
		if value == navigationToken(ranked, passes, short, false, i) {
			return i + 1, min(count, i+1+paging.DefaultLimit)
		}
		if value == navigationToken(ranked, passes, short, true, i) {
			return max(0, i-paging.DefaultLimit), i
		}
	}
	panic("navigation fixture token not found")
}

func navigationExpected(cfg Config, ranked []store.Ranked, passes []store.Pass, sp, pp string) LandingData {
	data := LandingData{Banner: bannerFor(cfg), Shortlist: []ShortlistRow{}, Passes: []PassRow{}, ShortlistPage: sp, PassesPage: pp}
	start, end := navigationBounds(ranked, passes, true, sp)
	for i := start; i < end; i++ {
		data.Shortlist = append(data.Shortlist, shortlistRow(ranked[i], i, 0))
	}
	if sp != "" && start < end {
		data.ShortlistPrev = navigationToken(ranked, passes, true, true, start)
	}
	if end < len(ranked) {
		data.ShortlistNext = navigationToken(ranked, passes, true, false, end-1)
	}
	start, end = navigationBounds(ranked, passes, false, pp)
	for i := start; i < end; i++ {
		data.Passes = append(data.Passes, passRow(passes[i]))
	}
	if pp != "" && start < end {
		data.PassesPrev = navigationToken(ranked, passes, false, true, start)
	}
	if end < len(passes) {
		data.PassesNext = navigationToken(ranked, passes, false, false, end-1)
	}
	return data
}

func navigationPath(short bool, value, other string) string {
	sp, pp := other, value
	if short {
		sp, pp = value, other
	}
	return "/?" + (url.Values{"shortlist_page": {sp}, "passes_page": {pp}}).Encode()
}

// Observe the handler's template data through a template function, then
// check its actual HTTP body against the unmodified embedded template.
func navigationLanding(t *testing.T, cfg Config, path string, want LandingData) LandingData {
	t.Helper()
	captureSet, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var got LandingData
	calls := 0
	cloned, err := captureSet.templates.Funcs(template.FuncMap{"navigationCapture": func(d LandingData) string { got = d; calls++; return "" }}).Parse(`{{define "landing"}}{{navigationCapture .}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	captured := cfg
	captured.Pages = &Set{templates: cloned}
	response := requestPage(captured, http.MethodGet, path, "reader")
	if response.Code != http.StatusOK || calls != 1 {
		t.Fatal("capture", path, response.Code, calls)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("landing data %s\ngot: %+v\nwant: %+v", path, got, want)
	}
	var expected bytes.Buffer
	if err = cfg.Pages.templates.ExecuteTemplate(&expected, "landing", got); err != nil {
		t.Fatal(err)
	}
	response = requestPage(cfg, http.MethodGet, path, "reader")
	if response.Code != http.StatusOK || response.Body.String() != expected.String() {
		t.Fatal("rendered landing differs", path, response.Code)
	}
	return got
}

func navigationWalk(t *testing.T, cfg Config, ranked []store.Ranked, passes []store.Pass, short bool, other string) {
	t.Helper()
	var windows []LandingData
	value := ""
	for step := 0; ; step++ {
		if step > len(ranked)/paging.DefaultLimit+1 {
			t.Fatal("forward walk did not terminate")
		}
		sp, pp := other, value
		if short {
			sp, pp = value, other
		}
		got := navigationLanding(t, cfg, navigationPath(short, value, other), navigationExpected(cfg, ranked, passes, sp, pp))
		windows = append(windows, got)
		next := got.PassesNext
		if short {
			next = got.ShortlistNext
		}
		if next == "" {
			break
		}
		value = next
	}
	if len(windows) != (len(ranked)+paging.DefaultLimit-1)/paging.DefaultLimit {
		t.Fatal("forward window count", len(windows))
	}
	// Follow each actual Prev, requiring precisely the preceding forward
	// window, including full row data and the shortlist's absolute ranks.
	for j := len(windows) - 1; j > 0; j-- {
		prev := windows[j].PassesPrev
		if short {
			prev = windows[j].ShortlistPrev
		}
		if prev == "" {
			t.Fatal("missing backward token", j)
		}
		if j == 1 {
			response := requestPage(cfg, http.MethodGet, navigationPath(short, prev, other), "reader")
			location := "/"
			if other != "" {
				param := "shortlist_page"
				if short {
					param = "passes_page"
				}
				location = "/?" + param + "=" + other
			}
			if response.Code != http.StatusSeeOther || !reflect.DeepEqual(response.Header().Values("Location"), []string{location}) || response.Body.Len() != 0 {
				t.Fatal("backward first-window redirect", response)
			}
		} else {
			sp, pp := other, prev
			if short {
				sp, pp = prev, other
			}
			got := navigationLanding(t, cfg, navigationPath(short, prev, other), navigationExpected(cfg, ranked, passes, sp, pp))
			if short && !reflect.DeepEqual(got.Shortlist, windows[j-1].Shortlist) || !short && !reflect.DeepEqual(got.Passes, windows[j-1].Passes) {
				t.Fatal("backward window differs", j)
			}
			windows[j-1] = got
		}
	}
	sp, pp := other, "last"
	if short {
		sp, pp = "last", other
	}
	last := navigationLanding(t, cfg, navigationPath(short, "last", other), navigationExpected(cfg, ranked, passes, sp, pp))
	// Derive the tail independently from the full forward walk's rows.
	if short {
		all := []ShortlistRow{}
		for _, window := range windows {
			all = append(all, window.Shortlist...)
		}
		if !reflect.DeepEqual(last.Shortlist, all[len(all)-paging.DefaultLimit:]) || last.ShortlistPage != "last" || last.ShortlistNext != "" || last.ShortlistPrev != navigationToken(ranked, passes, true, true, len(all)-paging.DefaultLimit) {
			t.Fatal("shortlist last is not forward-walk tail")
		}
		for i, row := range all {
			if row.Rank != int64(i+1) {
				t.Fatal("discontinuous rank", i, row.Rank)
			}
		}
	} else {
		all := []PassRow{}
		for _, window := range windows {
			all = append(all, window.Passes...)
		}
		if !reflect.DeepEqual(last.Passes, all[len(all)-paging.DefaultLimit:]) || last.PassesPage != "last" || last.PassesNext != "" || last.PassesPrev != navigationToken(ranked, passes, false, true, len(all)-paging.DefaultLimit) {
			t.Fatal("passes last is not forward-walk tail")
		}
	}
}

package pages

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-55P6-B7TM: underflow drops just that list's value, for both methods.
func TestLandingUnderflowRedirects(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1} {
		cfg, _ := pageFixture(t)
		ctx := context.Background()
		for i := 0; i < count; i++ {
			p, err := cfg.Store.AddPass(ctx, store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "starter"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = cfg.Store.FinishPass(ctx, p.ID, store.Counts{}); err != nil {
				t.Fatal(err)
			}
		}
		passes, err := cfg.Store.ListPasses(ctx, "", "", count+1)
		if err != nil {
			t.Fatal(err)
		}
		after := (paging.Token{Tool: "passes"}).Encode("2099-01-01T00:00:00Z", "pass_position")
		before := (paging.Token{Tool: "passes-before"}).Encode("2099-01-01T00:00:00Z", "pass_position")
		if len(passes) > 0 {
			before = (paging.Token{Tool: "passes-before"}).Encode(passes[0].Started.UTC().Format(time.RFC3339), passes[0].ID)
		}
		shortAfter := (paging.Token{Tool: "shortlist"}).Encode(int64(1), "candidate_position")
		shortBefore := (paging.Token{Tool: "shortlist-before"}).Encode(int64(1), "candidate_position")
		cases := []struct{ sp, pp, location string }{
			{"last", "", "/"},
			{shortBefore, "", "/"},
			{"last", after, "/?passes_page=" + after},
			{"last", before, "/"},
			{"", before, "/"},
			{shortAfter, before, "/?shortlist_page=" + shortAfter},
		}
		if count <= paging.DefaultLimit {
			cases = append(cases, struct{ sp, pp, location string }{"last", "last", "/"})
			cases = append(cases, struct{ sp, pp, location string }{shortAfter, "last", "/?shortlist_page=" + shortAfter})
		} else {
			cases = append(cases, struct{ sp, pp, location string }{"last", "last", "/?passes_page=last"})
		}
		for _, tc := range cases {
			path := "/?" + (url.Values{"shortlist_page": {tc.sp}, "passes_page": {tc.pp}}).Encode() + "&shortlist_page=bogus&passes_page=bogus&extra=value"
			for _, method := range []string{"GET", "HEAD"} {
				got := requestPage(cfg, method, path, "reader")
				if got.Code != 303 || len(got.Header().Values("Location")) != 1 || got.Header().Get("Location") != tc.location || got.Body.Len() != 0 {
					t.Fatalf("count=%d %s %s: status=%d location=%q body=%q", count, method, path, got.Code, got.Header().Get("Location"), got.Body.String())
				}
			}
		}
	}
}

// R-TBM8-4RJJ R-HUCX-NMIV R-TGHT-NUIB: empty after-windows carry no prev or next.
func TestLandingEmptyAfterWindows(t *testing.T) {
	cfg, _ := pageFixture(t)
	sp := (paging.Token{Tool: "shortlist"}).Encode(int64(42), "candidate_position")
	pp := (paging.Token{Tool: "passes"}).Encode("2020-01-01T00:00:00Z", "pass_position")
	data := LandingData{Banner: bannerFor(cfg), Shortlist: []ShortlistRow{}, ShortlistPage: sp, Passes: []PassRow{}, PassesPage: pp}
	comparePage(t, cfg, "/?"+(url.Values{"shortlist_page": {sp}, "passes_page": {pp}}).Encode(), "landing", 200, data)
}

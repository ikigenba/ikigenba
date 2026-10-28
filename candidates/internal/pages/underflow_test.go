package pages

import (
	"context"
	"net/url"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-1MOI-YITR R-0M3J-HEKX R-464Z-NBFU: underflow drops just that list's value, for both methods.
func TestLandingUnderflowRedirects(t *testing.T) {
	for _, prospectListCount := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1} {
		for _, passCount := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1} {
			cfg, _ := pageFixture(t)
			ctx := context.Background()
			for i := 0; i < prospectListCount; i++ {
				if _, err := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "underflow-prospect_list", Size: store.ProspectListSizeDefault, CreatedUserID: "starter"}); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < passCount; i++ {
				p, err := cfg.Store.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "starter"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = cfg.Store.FinishPass(ctx, p.ID, store.Counts{}); err != nil {
					t.Fatal(err)
				}
			}
			// Positions need not correspond to a stored entry.
			prospectListAfter := (paging.Token{Tool: "prospect_lists"}).Encode("zzzz", "pl_position")
			prospectListBefore := (paging.Token{Tool: "prospect_lists-before"}).Encode("aaaa", "pl_position")
			passAfter := (paging.Token{Tool: "passes"}).Encode("1900-01-01T00:00:00Z", "pass_position")
			passBefore := (paging.Token{Tool: "passes-before"}).Encode("2099-01-01T00:00:00Z", "pass_position")
			type value struct {
				text       string
				underflows bool
			}
			cvs := []value{{"", false}, {prospectListAfter, false}, {prospectListBefore, true}, {"last", prospectListCount <= paging.DefaultLimit}}
			pvs := []value{{"", false}, {passAfter, false}, {passBefore, true}, {"last", passCount <= paging.DefaultLimit}}
			for _, cv := range cvs {
				for _, pv := range pvs {
					if !cv.underflows && !pv.underflows {
						continue
					}
					location := "/"
					if !cv.underflows && cv.text != "" {
						location = "/?prospect_lists_page=" + cv.text
					}
					if !pv.underflows && pv.text != "" {
						location = "/?passes_page=" + pv.text
					}
					path := "/?" + (url.Values{"prospect_lists_page": {cv.text, "bogus"}, "passes_page": {pv.text, "bogus"}, "extra": {"value"}}).Encode()
					for _, method := range []string{"GET", "HEAD"} {
						got := requestPage(cfg, method, path, "reader")
						if got.Code != 303 || len(got.Header().Values("Location")) != 1 || got.Header().Get("Location") != location || got.Body.Len() != 0 {
							t.Fatalf("counts=%d/%d %s %s: status=%d location=%q want=%q body=%q", prospectListCount, passCount, method, path, got.Code, got.Header().Get("Location"), location, got.Body.String())
						}
					}
				}
			}
		}
	}
}

// R-QR7J-1X12 R-0M3J-HEKX R-464Z-NBFU: empty after-windows carry no prev or next.
func TestLandingEmptyAfterWindows(t *testing.T) {
	cfg, _ := pageFixture(t)
	sp := (paging.Token{Tool: "prospect_lists"}).Encode("navigation-prospect_list", "pl_position")
	pp := (paging.Token{Tool: "passes"}).Encode("2020-01-01T00:00:00Z", "pass_position")
	data := LandingData{Banner: bannerFor(cfg), ProspectLists: []ProspectListRow{}, ProspectListsPage: sp, Passes: []PassRow{}, PassesPage: pp}
	comparePage(t, cfg, "/?"+(url.Values{"prospect_lists_page": {sp}, "passes_page": {pp}}).Encode(), "landing", 200, data)
}

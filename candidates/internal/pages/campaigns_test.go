package pages

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-S1KY-PFZ8 R-Z5NV-19P2 R-9SBD-VETV
func TestCampaignHistoryPages(t *testing.T) {
	cfg, db := pageFixture(t)
	ctx := t.Context()
	_, ids, err := cfg.Store.Attach(ctx, "campaign-fixture", false, []store.Observation{
		{Scraper: "fixture", SourceID: "one", Digest: "one", Reading: sources.Reading{Name: "chosen-first", NameKey: "one"}},
		{Scraper: "fixture", SourceID: "two", Digest: "two", Reading: sources.Reading{Name: "chosen-second", NameKey: "two"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	if got := capturedCandidate(t, cfg, id); got.Campaigns == nil || len(got.Campaigns) != 0 {
		t.Fatal(got.Campaigns)
	}
	prospectList, err := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "chosen-prospect_list-name", Size: 10, CreatedUserID: "chosen-owner"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "services.json")
	cfg.ServicesPath = path
	writeServices := func(base string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"services":[{"name":"campaigns","description":"","socket":"","enabled":true,"mcp":false,"url":"`+base+`"}]}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeServices("https://chosen-campaigns.test/")
	at := time.Date(2026, 10, 5, 10, 0, 7, 987, time.FixedZone("offset", 3600))
	if err := cfg.Store.AddCampaignUse(ctx, id, "chosen-first-campaign", prospectList.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Store.AddCampaignUse(ctx, id, "chosen-second-campaign", "", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Store.DeliverCampaignUse(ctx, id, "chosen-third-campaign", at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Store.RejectCampaignUse(ctx, id, "chosen-first-campaign", "chosen-reason", at.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Store.DeliverCampaignUse(ctx, id, "chosen-first-campaign", at.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got := capturedCandidate(t, cfg, id)
	want := []CampaignRow{
		{Campaign: "chosen-second-campaign", CampaignPage: "https://chosen-campaigns.test/campaigns/chosen-second-campaign", Added: "2026-10-05T10:00:07Z", AddedText: "2026-10-05 10:00"},
		{Campaign: "chosen-first-campaign", CampaignPage: "https://chosen-campaigns.test/campaigns/chosen-first-campaign", ProspectList: prospectList.ID, ProspectListName: prospectList.Name, ProspectListPage: "/prospect-lists/" + prospectList.ID, Added: "2026-10-05T09:00:07Z", AddedText: "2026-10-05 09:00", Rejected: true, RejectedAt: "2026-10-05T12:00:07Z", RejectedAtText: "2026-10-05 12:00", Reason: "chosen-reason", Delivered: "2026-10-05T13:00:07Z", DeliveredText: "2026-10-05 13:00"},
		{Campaign: "chosen-third-campaign", CampaignPage: "https://chosen-campaigns.test/campaigns/chosen-third-campaign", Delivered: "2026-10-05T11:00:07Z", DeliveredText: "2026-10-05 11:00"},
	}
	if !reflect.DeepEqual(got.Campaigns, want) {
		t.Fatalf("got %#v want %#v", got.Campaigns, want)
	}
	writeServices("https://changed-campaigns.test//")
	if row := capturedCandidate(t, cfg, id).Campaigns[0]; row.CampaignPage != "https://changed-campaigns.test/campaigns/chosen-second-campaign" {
		t.Fatal(row)
	}
	cfg.ServicesPath = ""
	if _, err := cfg.Store.DeleteProspectList(ctx, prospectList.ID); err != nil {
		t.Fatal(err)
	}
	got = capturedCandidate(t, cfg, id)
	if row := got.Campaigns[1]; row.CampaignPage != "" || row.ProspectList != prospectList.ID || row.ProspectListName != "" || row.ProspectListPage != "" {
		t.Fatal(row)
	}
	zero, err := campaignRow(ctx, cfg, store.CampaignUse{Campaign: "chosen-zero"})
	if err != nil || !reflect.DeepEqual(zero, CampaignRow{Campaign: "chosen-zero"}) {
		t.Fatal(zero, err)
	}
	if _, _, err := cfg.Store.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[1], Other: id, UserID: "owner", Why: "chosen-merge"}); err != nil {
		t.Fatal(err)
	}
	if merged := capturedCandidate(t, cfg, id); merged.Campaigns == nil || len(merged.Campaigns) != 0 {
		t.Fatal(merged.Campaigns)
	}
	if err := cfg.Store.AddCampaignUse(ctx, id, "chosen-after-merge", "", at); err != nil {
		t.Fatal(err)
	}
	if survivor := capturedCandidate(t, cfg, ids[1]); len(survivor.Campaigns) != 4 {
		t.Fatal(survivor.Campaigns)
	}
	db.SetFailing(true)
	if _, err := campaignRow(ctx, cfg, store.CampaignUse{ProspectList: prospectList.ID}); err == nil {
		t.Fatal("expected store error")
	}
	db.SetFailing(false)
}

// R-YA78-4I0X R-IRDN-EX4Y
func TestCampaignFiltersAndTemplates(t *testing.T) {
	cfg, _ := pageFixture(t)
	for _, n := range []int64{0, 90} {
		f := store.ProspectListFilters{MinCampaignAgeDays: &n}
		want := strconv.FormatInt(n, 10)
		if v := filtersView(f); v.MinCampaignAgeDays != want {
			t.Fatal(v)
		}
		c, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "chosen-filter", Filters: f, Size: 10, CreatedUserID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		if d := pageProspectList(t, cfg, c.ID); d.ProspectList.Filters.MinCampaignAgeDays != want {
			t.Fatal(d)
		}
	}
	if v := filtersView(store.ProspectListFilters{}); v.MinCampaignAgeDays != "" {
		t.Fatal(v)
	}
	for _, status := range []string{store.StatusActive, store.StatusMerged} {
		for _, rows := range [][]CampaignRow{nil, {}, {{Campaign: "chosen-campaign-id", CampaignPage: "https://chosen.test/campaign", ProspectList: "chosen-prospect_list-id", ProspectListName: "chosen-prospect_list-name", ProspectListPage: "/prospect-lists/chosen-prospect_list-page", Added: "machine-start-token", AddedText: "display-start-token", Rejected: true, RejectedAt: "machine-rejection-token", RejectedAtText: "display-rejection-token", Reason: "chosen-reason", Delivered: "machine-delivery-token", DeliveredText: "display-delivery-token"}}, {{Campaign: "chosen-no-details", ProspectList: "chosen-deleted-prospect_list"}}, {{Campaign: "chosen-empty-reason", Rejected: true}}} {
			var b bytes.Buffer
			if err := cfg.Pages.templates.ExecuteTemplate(&b, "candidate", CandidateData{Candidate: CandidateView{Status: status}, Campaigns: rows}); err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				for _, s := range []string{r.Campaign, r.CampaignPage, r.ProspectList, r.ProspectListName, r.ProspectListPage, r.Added, r.AddedText, r.RejectedAt, r.RejectedAtText, r.Reason, r.Delivered, r.DeliveredText} {
					if s != "" && !strings.Contains(b.String(), s) {
						t.Fatal("row value missing", s)
					}
				}
			}
		}
	}
	for _, value := range []string{"", "78234"} {
		var b bytes.Buffer
		if err := cfg.Pages.templates.ExecuteTemplate(&b, "prospect_list", ProspectListData{ProspectList: ProspectListView{Filters: FiltersView{MinCampaignAgeDays: value}}}); err != nil {
			t.Fatal(err)
		}
		if value != "" && !strings.Contains(b.String(), value) {
			t.Fatal("filter value missing")
		}
	}
}

// R-S1KY-PFZ8
func TestCampaignRowShape(_ *testing.T) {
	_ = struct {
		Campaign, CampaignPage, ProspectList, ProspectListName, ProspectListPage, Added, AddedText string
		Rejected                                                                                   bool
		RejectedAt, RejectedAtText, Reason, Delivered, DeliveredText                               string
	}(CampaignRow{})
}

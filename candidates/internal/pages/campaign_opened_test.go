package pages

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-4LYD-N383 R-3MU8-TRBZ
func TestCampaignPresenceViewsAndTemplates(t *testing.T) {
	cfg, _ := pageFixture(t)
	for _, f := range []store.ProspectListFilters{{}, {CampaignAny: "no", Campaign: "camp_0123456789abcdef", CampaignNot: true, Opened: "yes"}} {
		v := filtersView(f)
		if v.CampaignAny != f.CampaignAny || v.Campaign != f.Campaign || v.CampaignNot != f.CampaignNot || v.Opened != f.Opened {
			t.Fatal(v)
		}
		made, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "presence", Filters: f, Size: 10, CreatedUserID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		got := pageProspectList(t, cfg, made.ID).ProspectList.Filters
		if got.CampaignAny != f.CampaignAny || got.Campaign != f.Campaign || got.CampaignNot != f.CampaignNot || got.Opened != f.Opened {
			t.Fatal(got)
		}
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, opened := range []bool{false, true} {
		for _, delivered := range []bool{false, true} {
			for _, rejected := range []bool{false, true} {
				r := CampaignRow{Campaign: "chosen-campaign", Rejected: rejected}
				if opened {
					r.Opened = "chosen-opened-machine"
					r.OpenedText = "chosen-opened-display"
				}
				if delivered {
					r.Delivered = "chosen-delivered-machine"
					r.DeliveredText = "chosen-delivered-display"
				}
				var body bytes.Buffer
				if err := loaded.templates.ExecuteTemplate(&body, "candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Campaigns: []CampaignRow{r}}); err != nil {
					t.Fatal(err)
				}
				for _, s := range []string{r.Opened, r.OpenedText} {
					if s != "" && !strings.Contains(body.String(), s) {
						t.Fatal(s, body.String())
					}
				}
			}
		}
	}
	for _, any := range []string{"yes", "no", ""} {
		for _, opened := range []string{"yes", "no", ""} {
			for _, campaign := range []struct {
				id  string
				not bool
			}{{"", false}, {"chosen-filter-campaign", false}, {"chosen-filter-campaign", true}} {
				var body bytes.Buffer
				f := FiltersView{CampaignAny: any, Opened: opened, Campaign: campaign.id, CampaignNot: campaign.not}
				if err := loaded.templates.ExecuteTemplate(&body, "prospect_list", ProspectListData{ProspectList: ProspectListView{Filters: f}}); err != nil {
					t.Fatal(err)
				}
				if campaign.id != "" && !strings.Contains(body.String(), campaign.id) {
					t.Fatal(body.String())
				}
			}
		}
	}
}

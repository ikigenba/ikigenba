package tools_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-3GJ3-M4V9 R-ML95-4DLF R-VN8F-PJ7Z
func TestCampaignAnswerHistory(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	id := ids[0]
	k := f.create("chosen-campaign-prospect_list")
	at := time.Date(2026, 10, 5, 10, 0, 7, 123456, time.FixedZone("offset", 3600))
	read := func(id string) tools.Detail {
		t.Helper()
		r := f.call("candidate", tools.CandidateArgs{Candidate: id})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		return decodeList[tools.Detail](t, r)
	}
	if got := read(id); got.Campaigns == nil || len(got.Campaigns) != 0 {
		t.Fatal(got.Campaigns)
	}
	if err := f.st.AddCampaignUse(t.Context(), id, "chosen-p", k.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := f.st.AddCampaignUse(t.Context(), id, "chosen-q", "", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	want := []tools.CampaignUse{{Campaign: "chosen-q", Added: "2026-10-05T10:00:07Z"}, {Campaign: "chosen-p", ProspectList: k.ID, Added: "2026-10-05T09:00:07Z"}}
	if got := read(id); !reflect.DeepEqual(got.Campaigns, want) {
		t.Fatal(got.Campaigns, want)
	}
	checkCampaignWire := func(want []tools.CampaignUse) {
		t.Helper()
		var raw struct {
			Campaigns []json.RawMessage `json:"campaigns"`
		}
		if err := json.Unmarshal(decode(t, f.call("candidate", tools.CandidateArgs{Candidate: id})).Structured, &raw); err != nil {
			t.Fatal(err)
		}
		if len(raw.Campaigns) != len(want) {
			t.Fatal(raw.Campaigns)
		}
		for i, use := range want {
			b, err := json.Marshal(use)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw.Campaigns[i]) != string(b) {
				t.Fatal(string(raw.Campaigns[i]), string(b))
			}
		}
	}
	checkCampaignWire(want)
	if err := f.st.RejectCampaignUse(t.Context(), id, "chosen-p", "chosen-reason", at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DeliverCampaignUse(t.Context(), id, "chosen-q", at.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DeliverCampaignUse(t.Context(), id, "chosen-s", at.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	want[0].Delivered = "2026-10-05T12:00:07Z"
	want[1].Rejected = "2026-10-05T11:00:07Z"
	want[1].Reason = "chosen-reason"
	want = append(want, tools.CampaignUse{Campaign: "chosen-s", Delivered: "2026-10-05T13:00:07Z"})
	checkCampaignWire(want)
	if got := read(id); !reflect.DeepEqual(got.Campaigns, want) {
		t.Fatal(got.Campaigns, want)
	}
	f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[1], Other: id, Why: "chosen-merge"})
	if got := read(id); got.Campaigns == nil || len(got.Campaigns) != 0 {
		t.Fatal(got.Campaigns)
	}
	if got := read(ids[1]); !reflect.DeepEqual(got.Campaigns, want) {
		t.Fatal(got.Campaigns)
	}
	if err := f.st.AddCampaignUse(t.Context(), id, "chosen-after-merge", "", at); err != nil {
		t.Fatal(err)
	}
	if got := read(ids[1]); len(got.Campaigns) != 4 {
		t.Fatal(got.Campaigns)
	}
	if got := read(id); got.Campaigns == nil || len(got.Campaigns) != 0 {
		t.Fatal(got.Campaigns)
	}
	for _, tc := range []struct {
		v    tools.CampaignUse
		want string
	}{
		{tools.CampaignUse{}, `{"campaign":"","prospect_list":""}`},
		{tools.CampaignUse{"campaign", "prospect_list", "added", "rejected", "reason", "delivered"}, `{"campaign":"campaign","prospect_list":"prospect_list","added":"added","rejected":"rejected","reason":"reason","delivered":"delivered"}`},
	} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Fatal(string(b), err)
		}
	}
}

// R-X5PS-GSY3 R-EYXP-XAFA
func TestCampaignFilterToolLifecycle(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(7)
	date := time.Date(2026, 10, 20, 0, 1, 0, 0, time.UTC)
	f.now = date
	old := date.AddDate(0, 0, -30)
	fresh := date.AddDate(0, 0, -29)
	writes := []func() error{
		func() error {
			return f.st.AddCampaignUse(t.Context(), ids[1], "old-add", "", time.Date(old.Year(), old.Month(), old.Day(), 23, 59, 0, 0, time.UTC))
		},
		func() error {
			return f.st.AddCampaignUse(t.Context(), ids[2], "fresh-add", "", time.Date(fresh.Year(), fresh.Month(), fresh.Day(), 0, 0, 0, 0, time.UTC))
		},
		func() error {
			return f.st.RejectCampaignUse(t.Context(), ids[3], "fresh-reject", "chosen-reason", time.Date(fresh.Year(), fresh.Month(), fresh.Day(), 12, 0, 0, 0, time.UTC))
		},
		func() error {
			return f.st.DeliverCampaignUse(t.Context(), ids[4], "old-deliver", time.Date(old.Year(), old.Month(), old.Day(), 23, 59, 0, 0, time.UTC))
		},
		func() error {
			return f.st.DeliverCampaignUse(t.Context(), ids[5], "fresh-deliver", time.Date(fresh.Year(), fresh.Month(), fresh.Day(), 0, 0, 0, 0, time.UTC))
		},
		func() error {
			return f.st.AddCampaignUse(t.Context(), ids[6], "offset-add", "", time.Date(old.Year(), old.Month(), old.Day(), 20, 0, 0, 0, time.FixedZone("offset", -5*3600)))
		},
	}
	for _, write := range writes {
		if err := write(); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []uint32{0, 30} {
		k := decodeList[tools.ProspectList](t, f.call("create_prospect_list", map[string]any{"name": "chosen-age-filter", "filters": map[string]any{"min_campaign_age_days": n}}))
		if k.Filters.MinCampaignAgeDays == nil || *k.Filters.MinCampaignAgeDays != int64(n) {
			t.Fatal(k)
		}
		for _, now := range []time.Time{date, date.Add(23*time.Hour + 58*time.Minute)} {
			f.now = now
			got := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: k.ID, Peek: true}))
			if got.ProspectList.Filters.MinCampaignAgeDays == nil || *got.ProspectList.Filters.MinCampaignAgeDays != int64(n) {
				t.Fatal(got)
			}
			want := ids
			if n == 30 {
				want = []string{ids[0], ids[1], ids[4]}
			}
			members := []string{}
			for _, m := range got.Members {
				members = append(members, m.Candidate.ID)
			}
			slices.Sort(members)
			want = slices.Clone(want)
			slices.Sort(want)
			if !reflect.DeepEqual(members, want) {
				t.Fatal(members, want)
			}
		}
		for _, args := range []map[string]any{{"prospect_list": k.ID}, {"prospect_list": k.ID, "filters": nil}} {
			got := decodeList[tools.ProspectList](t, f.call("edit_prospect_list", args))
			if got.Filters.MinCampaignAgeDays == nil || *got.Filters.MinCampaignAgeDays != int64(n) {
				t.Fatal(got)
			}
		}
		for _, filters := range []map[string]any{{}, {"min_campaign_age_days": nil}, {"show_excluded": true}} {
			f.call("edit_prospect_list", map[string]any{"prospect_list": k.ID, "filters": map[string]any{"min_campaign_age_days": n}})
			got := decodeList[tools.ProspectList](t, f.call("edit_prospect_list", map[string]any{"prospect_list": k.ID, "filters": filters}))
			if got.Filters.MinCampaignAgeDays != nil {
				t.Fatal(got)
			}
		}
	}
	for _, filters := range []map[string]any{{}, {"min_campaign_age_days": nil}} {
		got := decodeList[tools.ProspectList](t, f.call("create_prospect_list", map[string]any{"name": "chosen-no-filter", "filters": filters}))
		if got.Filters.MinCampaignAgeDays != nil {
			t.Fatal(got)
		}
		read := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: got.ID, Peek: true}))
		if len(read.Members) != 7 {
			t.Fatal(read)
		}
	}
	before := decodeList[tools.ProspectListList](t, f.call("prospect_lists", tools.ProspectListsArgs{}))
	r := f.call("create_prospect_list", map[string]any{"name": "chosen-negative", "filters": map[string]any{"min_campaign_age_days": -1}})
	if !r.IsError() || len(decode(t, r).Structured) != 0 {
		t.Fatal(decode(t, r))
	}
	text := decode(t, r).Content[0].Text
	for _, word := range []string{passes.RefusalBusy, passes.RefusalInvalid, passes.RefusalNotFound, passes.RefusalStopping, passes.RefusalUnavailable} {
		if text == word || strings.HasPrefix(text, word+":") {
			t.Fatal(text)
		}
	}
	after := decodeList[tools.ProspectListList](t, f.call("prospect_lists", tools.ProspectListsArgs{}))
	if !reflect.DeepEqual(before, after) {
		t.Fatal(before, after)
	}
}

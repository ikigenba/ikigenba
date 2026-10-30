package tools_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"

	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-DDU5-2I22 R-7ZCJ-CGLF
func TestCampaignPresenceFiltersThroughTools(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(6)
	const k = "camp_0123456789abcdef"
	const l = "camp_fedcba9876543210"
	for _, i := range []int{0, 1, 3, 4} {
		campaign := k
		if i == 4 {
			campaign = l
		}
		if err := f.st.AddCampaignUse(t.Context(), ids[i], campaign, "", f.now); err != nil {
			t.Fatal(err)
		}
		if i != 0 {
			if err := f.st.MarkCampaignUseOpened(t.Context(), ids[i], campaign, f.now); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.st.RejectCampaignUse(t.Context(), ids[1], k, "rejected", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DeliverCampaignUse(t.Context(), ids[2], k, f.now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		filters map[string]any
		indices []int
	}{
		{map[string]any{}, []int{0, 1, 2, 3, 4, 5}},
		{map[string]any{"campaign": k}, []int{0, 3}},
		{map[string]any{"campaign": k, "campaign_not": true}, []int{1, 2, 4, 5}},
		{map[string]any{"campaign": k, "opened": "yes"}, []int{3}},
		{map[string]any{"campaign": k, "opened": "no"}, []int{0}},
		{map[string]any{"campaign": k, "campaign_not": true, "opened": "yes"}, []int{1}},
		{map[string]any{"campaign": k, "campaign_not": true, "opened": "no"}, []int{2, 4, 5}},
		{map[string]any{"campaign_any": "yes"}, []int{0, 3, 4}},
		{map[string]any{"campaign_any": "no"}, []int{1, 2, 5}},
		{map[string]any{"opened": "yes"}, []int{1, 3, 4}},
	} {
		made := decodeList[tools.ProspectList](t, f.call("create_prospect_list", map[string]any{"name": "presence", "filters": tc.filters}))
		read := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: made.ID, Peek: true}))
		got, want := []string{}, []string{}
		for _, m := range read.Members {
			got = append(got, m.Candidate.ID)
		}
		for _, i := range tc.indices {
			want = append(want, ids[i])
		}
		slices.Sort(got)
		slices.Sort(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%v: got %v want %v", tc.filters, got, want)
		}
	}
	filters := map[string]any{"campaign_any": "yes", "campaign": k, "campaign_not": true, "opened": "no"}
	made := decodeList[tools.ProspectList](t, f.call("create_prospect_list", map[string]any{"name": "round trip", "filters": filters}))
	check := func(v tools.ProspectList) {
		t.Helper()
		if v.Filters.CampaignAny != "yes" || v.Filters.Campaign != k || !v.Filters.CampaignNot || v.Filters.Opened != "no" {
			t.Fatal(v.Filters)
		}
	}
	check(made)
	check(decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: made.ID, Peek: true})).ProspectList)
	for _, a := range []map[string]any{{"prospect_list": made.ID}, {"prospect_list": made.ID, "filters": nil}} {
		check(decodeList[tools.ProspectList](t, f.call("edit_prospect_list", a)))
	}
	wireFilters := func(r mcp.Result) map[string]json.RawMessage {
		t.Helper()
		if r.IsError() {
			t.Fatal(r)
		}
		var value struct {
			Filters map[string]json.RawMessage `json:"filters"`
		}
		if err := json.Unmarshal(decode(t, r).Structured, &value); err != nil {
			t.Fatal(err)
		}
		return value.Filters
	}
	for _, replacement := range []map[string]any{{}, {"campaign": k, "campaign_not": false, "opened": ""}, {"show_excluded": true}} {
		f.call("edit_prospect_list", map[string]any{"prospect_list": made.ID, "filters": filters})
		wire := wireFilters(f.call("edit_prospect_list", map[string]any{"prospect_list": made.ID, "filters": replacement}))
		for _, key := range []string{"campaign_any", "campaign_not", "opened"} {
			if _, ok := wire[key]; ok {
				t.Fatal(wire)
			}
		}
		if replacement["campaign"] == k {
			if string(wire["campaign"]) != `"`+k+`"` {
				t.Fatal(wire)
			}
		} else if _, ok := wire["campaign"]; ok {
			t.Fatal(wire)
		}
	}
	created := wireFilters(f.call("create_prospect_list", map[string]any{"name": "false omitted", "filters": map[string]any{"campaign": k, "campaign_not": false, "opened": ""}}))
	if len(created) != 1 || string(created["campaign"]) != `"`+k+`"` {
		t.Fatal(created)
	}
	before := decodeList[tools.ProspectListList](t, f.call("prospect_lists", tools.ProspectListsArgs{}))
	bad := f.call("create_prospect_list", map[string]any{"name": "bad schema", "filters": map[string]any{"campaign_not": "true"}})
	schemaRefusal(t, bad)
	after := decodeList[tools.ProspectListList](t, f.call("prospect_lists", tools.ProspectListsArgs{}))
	if !reflect.DeepEqual(before, after) {
		t.Fatal(before, after)
	}
	f.d.SetFailing(true)
	for _, invalid := range []map[string]any{{"campaign_not": true}, {"campaign": "camp_0123456789abcde"}, {"campaign_any": "sometimes"}, {"opened": "sometimes"}} {
		refusal(t, f.call("create_prospect_list", map[string]any{"name": "invalid", "filters": invalid}), "invalid", tools.RefuseInvalid)
		refusal(t, f.call("edit_prospect_list", map[string]any{"prospect_list": made.ID, "filters": invalid}), "invalid", tools.RefuseInvalid)
	}
}

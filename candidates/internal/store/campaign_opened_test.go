package store_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-8CQ7-UVAK R-8DY4-8N19 R-7Y3F-9ME8 R-7ZBB-NE4X R-80J8-15VM R-846X-6H3P
func TestCampaignOpenedEarliestPreservesHistoryAndFollowsMerge(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	at := time.Date(2026, 10, 15, 12, 30, 45, 987654321, time.FixedZone("input", -5*3600))
	check := func(want store.CampaignUse) {
		t.Helper()
		contractEqual(t, campaignUses(t, f, id), []store.CampaignUse{want})
	}
	mark := f.s.MarkCampaignUseOpened
	contractOK(t, mark(ctx, id, "opened", at))
	want := store.CampaignUse{Candidate: id, Campaign: "opened", Opened: at.UTC().Truncate(time.Second)}
	check(want)
	for _, v := range []time.Time{at, at.Add(time.Hour), at.UTC().Truncate(time.Second)} {
		contractOK(t, mark(ctx, id, "opened", v))
		check(want)
	}
	contractOK(t, f.s.AddCampaignUse(ctx, id, "opened", "list", at.Add(time.Hour)))
	want.Added = at.Add(time.Hour).UTC().Truncate(time.Second)
	want.ProspectList = "list"
	check(want)
	contractOK(t, f.s.RejectCampaignUse(ctx, id, "opened", "reason", at.Add(2*time.Hour)))
	want.Rejected = at.Add(2 * time.Hour).UTC().Truncate(time.Second)
	want.Reason = "reason"
	check(want)
	contractOK(t, f.s.DeliverCampaignUse(ctx, id, "opened", at.Add(3*time.Hour)))
	want.Delivered = at.Add(3 * time.Hour).UTC().Truncate(time.Second)
	check(want)
	contractOK(t, mark(ctx, id, "opened", at.Add(-time.Hour)))
	want.Opened = at.Add(-time.Hour).UTC().Truncate(time.Second)
	check(want)
	f.reopen()
	check(want)
	other := contractAttach(t, f, "run", contractObservation("opened-chain", "other", f.now))[0]
	next := contractAttach(t, f, "run", contractObservation("opened-chain-next", "next", f.now))[0]
	for _, r := range []store.Ruling{{Kind: store.KindMerge, Candidate: id, Other: other, Why: "same", UserID: "owner"}, {Kind: store.KindMerge, Candidate: next, Other: id, Why: "same", UserID: "owner"}} {
		_, _, err := f.s.Decide(ctx, r)
		contractOK(t, err)
	}
	contractOK(t, f.s.MarkCampaignUseOpened(ctx, other, "opened", at.Add(-2*time.Hour)))
	want.Candidate = next
	want.Opened = at.Add(-2 * time.Hour).UTC().Truncate(time.Second)
	contractEqual(t, campaignUses(t, f, next), []store.CampaignUse{want})
	contractEqual(t, campaignUses(t, f, id), []store.CampaignUse{})
	contractEqual(t, campaignUses(t, f, other), []store.CampaignUse{})
}

// R-4Z5H-LV5V
func TestCampaignOpenedMergeChoosesEarliestNonzeroAndKeepsOtherFields(t *testing.T) {
	for _, pair := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 2}, {2, 1}, {1, 1}} {
		t.Run(fmt.Sprint(pair), func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			s := contractSeed(t, f)
			o := contractAttach(t, f, "run", contractObservation("opened-other", "other", f.now))[0]
			contractOK(t, f.s.AddCampaignUse(ctx, s, "shared", "survivor", f.now))
			contractOK(t, f.s.AddCampaignUse(ctx, o, "shared", "other", f.now.Add(-time.Hour)))
			if pair[0] > 0 {
				contractOK(t, f.s.MarkCampaignUseOpened(ctx, s, "shared", f.now.Add(time.Duration(pair[0])*time.Hour)))
			}
			if pair[1] > 0 {
				contractOK(t, f.s.MarkCampaignUseOpened(ctx, o, "shared", f.now.Add(time.Duration(pair[1])*time.Hour)))
			}
			contractOK(t, f.s.MarkCampaignUseOpened(ctx, o, "only-other", f.now))
			_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: s, Other: o, Why: "same", UserID: "owner"})
			contractOK(t, err)
			earliest := pair[0]
			if earliest == 0 || pair[1] > 0 && pair[1] < earliest {
				earliest = pair[1]
			}
			opened := time.Time{}
			if earliest > 0 {
				opened = f.now.Add(time.Duration(earliest) * time.Hour).UTC().Truncate(time.Second)
			}
			want := []store.CampaignUse{{Candidate: s, Campaign: "shared", ProspectList: "other", Added: f.now.Add(-time.Hour).UTC().Truncate(time.Second), Opened: opened}, {Candidate: s, Campaign: "only-other", Opened: f.now.UTC().Truncate(time.Second)}}
			contractEqual(t, campaignUses(t, f, s), want)
			contractEqual(t, campaignUses(t, f, o), []store.CampaignUse{})
			f.reopen()
			contractEqual(t, campaignUses(t, f, s), want)
		})
	}
}

// R-ITYS-QDLY R-IV6P-45CN R-IWEL-HX3C R-IXMH-VOU1 R-IYUE-9GKQ R-J02A-N8BF
func TestCampaignMembershipAndOpenedFiltersUseLiveHistory(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	const campaign = "camp_0123456789abcdef"
	const other = "camp_fedcba9876543210"
	// Candidate states: none, active, rejected, rejection only, delivery only,
	// open only, active/open, rejected/open, other active/open, active/other open.
	ids := []string{}
	for i := range 10 {
		ids = append(ids, contractAttach(t, f, "run", contractObservation(fmt.Sprint(i), fmt.Sprint(i), f.now))[0])
	}
	for _, i := range []int{1, 2, 6, 7, 9} {
		contractOK(t, f.s.AddCampaignUse(ctx, ids[i], campaign, "", f.now))
	}
	for _, i := range []int{2, 3, 7} {
		contractOK(t, f.s.RejectCampaignUse(ctx, ids[i], campaign, "reason", f.now))
	}
	contractOK(t, f.s.DeliverCampaignUse(ctx, ids[4], campaign, f.now))
	for _, i := range []int{5, 6, 7} {
		contractOK(t, f.s.MarkCampaignUseOpened(ctx, ids[i], campaign, f.now))
	}
	contractOK(t, f.s.AddCampaignUse(ctx, ids[8], other, "", f.now))
	contractOK(t, f.s.MarkCampaignUseOpened(ctx, ids[8], other, f.now))
	contractOK(t, f.s.MarkCampaignUseOpened(ctx, ids[9], other, f.now))
	_, err := f.s.Judge(ctx, ids)
	contractOK(t, err)
	k := prospectListCreate(t, f, "campaign membership")
	for _, tc := range []struct {
		name    string
		filters store.ProspectListFilters
		want    []int
	}{
		{"any", store.ProspectListFilters{}, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{"any yes", store.ProspectListFilters{CampaignAny: "yes"}, []int{1, 6, 8, 9}},
		{"any no", store.ProspectListFilters{CampaignAny: "no"}, []int{0, 2, 3, 4, 5, 7}},
		{"campaign", store.ProspectListFilters{Campaign: campaign}, []int{1, 6, 9}},
		{"campaign not", store.ProspectListFilters{Campaign: campaign, CampaignNot: true}, []int{0, 2, 3, 4, 5, 7, 8}},
		{"open anywhere", store.ProspectListFilters{Opened: "yes"}, []int{5, 6, 7, 8, 9}},
		{"unopened anywhere", store.ProspectListFilters{Opened: "no"}, []int{0, 1, 2, 3, 4}},
		{"in and open", store.ProspectListFilters{Campaign: campaign, Opened: "yes"}, []int{6}},
		{"not in and open", store.ProspectListFilters{Campaign: campaign, CampaignNot: true, Opened: "yes"}, []int{5, 7}},
		{"not in and unopened", store.ProspectListFilters{Campaign: campaign, CampaignNot: true, Opened: "no"}, []int{0, 2, 3, 4, 8}},
		{"in and unopened", store.ProspectListFilters{Campaign: campaign, Opened: "no"}, []int{1, 9}},
		{"any no and opened", store.ProspectListFilters{CampaignAny: "no", Opened: "yes"}, []int{5, 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &tc.filters, UserID: "owner"})
			contractOK(t, err)
			r := prospectListRead(t, f, k)
			want := map[string]bool{}
			for _, i := range tc.want {
				want[ids[i]] = true
			}
			contractEqual(t, r.Matching, int64(len(want)))
			contractEqual(t, len(r.Members), len(want))
			for _, m := range r.Members {
				if !want[m.Candidate.ID] {
					t.Fatalf("unexpected member %s", m.Candidate.ID)
				}
			}
		})
	}
	// Changes are visible without judging again, including a rejection removing membership.
	filters := store.ProspectListFilters{CampaignAny: "no"}
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	contractOK(t, f.s.RejectCampaignUse(ctx, ids[1], campaign, "removed", f.now))
	contractEqual(t, prospectListRead(t, f, k).Matching, int64(7))
	filters = store.ProspectListFilters{Campaign: campaign, Opened: "yes"}
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	contractOK(t, f.s.MarkCampaignUseOpened(ctx, ids[9], campaign, f.now))
	contractEqual(t, prospectListRead(t, f, k).Matching, int64(2))
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[9], Why: "excluded", UserID: "owner"})
	contractOK(t, err)
	contractEqual(t, prospectListRead(t, f, k).Matching, int64(1))
	f.reopen()
	contractEqual(t, prospectListRead(t, f, k).Matching, int64(1))
}

// R-J7DO-XURL R-J8LL-BMIA R-J4XW-6BA7 R-J9TH-PE8Z
func TestCampaignFirstFailingFilterPrecedenceAndDefinitionSnapshots(t *testing.T) {
	const campaign = "camp_0123456789abcdef"
	for _, tc := range []struct {
		name    string
		filters store.ProspectListFilters
		added   bool
		want    string
	}{
		{"age before any", store.ProspectListFilters{MinCampaignAgeDays: ptrInt(30), CampaignAny: "no"}, true, "min_campaign_age_days"},
		{"any before campaign", store.ProspectListFilters{CampaignAny: "yes", Campaign: campaign}, false, "campaign_any"},
		{"campaign before opened", store.ProspectListFilters{Campaign: campaign, Opened: "yes"}, false, "campaign"},
		{"opened before class", store.ProspectListFilters{Opened: "yes", Class: sources.ClassPrivate}, false, "opened"},
		{"inverted campaign", store.ProspectListFilters{Campaign: campaign, CampaignNot: true}, true, "campaign"},
		{"opened scoped absence", store.ProspectListFilters{Campaign: campaign, CampaignNot: true, Opened: "yes"}, false, "opened"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			id := contractSeed(t, f)
			k := prospectListCreate(t, f, "reason")
			openList(t, f, k.ID)
			prior := listOpens(t, f, k.ID)[0]
			if tc.added {
				contractOK(t, f.s.AddCampaignUse(ctx, id, campaign, "", f.now))
			}
			k, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &tc.filters, UserID: "owner"})
			contractOK(t, err)
			r := prospectListRead(t, f, k)
			contractEqual(t, r.Matching, int64(0))
			contractEqual(t, r.Changes.Since, prior.At)
			contractEqual(t, r.Changes.DefinitionChanged, []string{store.DefinitionFilters})
			contractEqual(t, len(r.Changes.Left), 1)
			contractEqual(t, r.Changes.Left[0].Why, store.WhyFilter)
			contractEqual(t, r.Changes.Left[0].Filter, tc.want)
			openList(t, f, k.ID)
			contractEqual(t, listOpens(t, f, k.ID)[0].Filters, tc.filters)
			contractEqual(t, prospectListRead(t, f, k).Changes.Left, []store.Left{})
		})
	}
}

// R-ZNYC-RTTH R-STA0-5N4B
func TestCampaignOpenedDoesNotResetEffectiveAge(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	contractOK(t, f.s.MarkCampaignUseOpened(ctx, id, "opened-only", f.now))
	k, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "age", Size: 100, CreatedUserID: "owner", Filters: store.ProspectListFilters{MinCampaignAgeDays: ptrInt(30)}})
	contractOK(t, err)
	contractEqual(t, prospectListRead(t, f, k).Matching, int64(1))
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &store.ProspectListFilters{MinCampaignAgeDays: ptrInt(math.MaxInt64)}, UserID: "owner"})
	contractOK(t, err)
	contractEqual(t, prospectListRead(t, f, k).Matching, int64(0))
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &store.ProspectListFilters{MinCampaignAgeDays: ptrInt(30)}, UserID: "owner"})
	contractOK(t, err)
	contractOK(t, f.s.AddCampaignUse(ctx, id, "opened-only", "", f.now.Add(-31*24*time.Hour)))
	contractEqual(t, prospectListRead(t, f, k).Matching, int64(1))
}

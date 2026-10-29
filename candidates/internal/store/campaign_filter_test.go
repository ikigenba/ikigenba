package store_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-ZNYC-RTTH R-STA0-5N4B R-LYM7-MTO1 R-6XO5-XRXE R-OW5O-5154
func TestCampaignFilterCalendarBoundariesAndEffectiveTimes(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 0, 0, 1, 0, time.UTC)
	type use struct{ added, rejected, delivered string }
	cases := []struct {
		name string
		uses []use
		pass bool
	}{
		{"never", nil, true}, {"boundary", []use{{added: "2026-09-15T23:59:59Z"}}, true}, {"recent", []use{{added: "2026-09-16T00:00:00Z"}}, false},
		{"rejected", []use{{added: "2026-10-05T00:00:00Z", rejected: "2026-10-06T00:00:00Z"}}, false},
		{"reject fallback", []use{{rejected: "2026-09-01T00:00:00Z", delivered: "2026-10-05T00:00:00Z"}}, true},
		{"deliver fallback", []use{{delivered: "2026-10-05T00:00:00Z"}}, false},
		{"earlier delivery", []use{{rejected: "2026-10-05T00:00:00Z", delivered: "2026-09-01T00:00:00Z"}}, true},
		{"reject only", []use{{rejected: "2026-10-05T00:00:00Z"}}, false},
		{"future", []use{{added: "2026-10-16T00:00:00Z"}}, false},
		{"mixed", []use{{added: "2026-08-01T00:00:00Z"}, {added: "2026-10-05T00:00:00Z"}}, false},
		{"mixed fallback", []use{{added: "2026-08-01T00:00:00Z"}, {delivered: "2026-10-05T00:00:00Z"}}, false},
		{"added prevails", []use{{added: "2026-09-01T00:00:00Z", rejected: "2026-10-05T00:00:00Z", delivered: "2026-10-05T00:00:00Z"}}, true},
	}
	ids := []string{}
	want := []string{}
	for _, tc := range cases {
		id := contractAttach(t, f, "run", contractObservation(tc.name, tc.name, f.now))[0]
		ids = append(ids, id)
		if tc.pass {
			want = append(want, id)
		}
		for i, u := range tc.uses {
			campaign := []string{"campaign-a", "campaign-b"}[i]
			parse := func(s string) time.Time { v, err := time.Parse(time.RFC3339, s); contractOK(t, err); return v }
			if u.added != "" {
				contractOK(t, f.s.AddCampaignUse(ctx, id, campaign, "", parse(u.added)))
			}
			if u.rejected != "" {
				contractOK(t, f.s.RejectCampaignUse(ctx, id, campaign, "reason", parse(u.rejected)))
			}
			if u.delivered != "" {
				contractOK(t, f.s.DeliverCampaignUse(ctx, id, campaign, parse(u.delivered)))
			}
		}
	}
	_, _, err := f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, err)
	k, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "campaign filter", Size: 100, CreatedUserID: "owner", Filters: store.ProspectListFilters{MinCampaignAgeDays: ptrInt(30)}})
	contractOK(t, err)
	check := func(expected []string) {
		t.Helper()
		r, err := f.s.ReadProspectList(ctx, k.ID)
		contractOK(t, err)
		got := prospectListIDs(r.Members)
		contractEqual(t, len(got), len(expected))
		for _, id := range expected {
			present := false
			for _, v := range got {
				if v == id {
					present = true
				}
			}
			if !present {
				t.Fatalf("candidate %s missing from %v", id, got)
			}
		}
		contractEqual(t, r.Matching, int64(len(expected)))
	}
	check(want)
	f.reopen()
	check(want)
	for _, age := range []*int64{ptrInt(0), nil} {
		k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &store.ProspectListFilters{MinCampaignAgeDays: age}, UserID: "owner"})
		contractOK(t, err)
		check(ids)
	}
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &store.ProspectListFilters{MinCampaignAgeDays: ptrInt(30)}, UserID: "owner"})
	contractOK(t, err)
	// A local date of October 14 at this offset is the UTC read date October 15.
	f.now = time.Date(2026, 10, 14, 23, 59, 59, 0, time.FixedZone("local", -5*3600))
	check(want)
	f.now = time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	check(ids)
	for _, n := range []int64{0, 1, math.MaxInt64} {
		contractEqual(t, store.ValidProspectListFilters(store.ProspectListFilters{MinCampaignAgeDays: &n}), true)
	}
	bad := store.ProspectListFilters{MinCampaignAgeDays: ptrInt(-1)}
	contractEqual(t, store.ValidProspectListFilters(bad), false)
	_, err = f.s.CreateProspectList(ctx, store.ProspectList{Name: "invalid", Size: 1, Filters: bad, CreatedUserID: "owner"})
	contractError(t, err, store.ErrInvalid)
	_, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &bad, UserID: "owner"})
	contractError(t, err, store.ErrInvalid)
}

// R-ZNYC-RTTH R-6XO5-XRXE R-LYM7-MTO1
func TestCampaignFilterReadsLiveHistoryWithoutRejudge(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 0, 0, 1, 0, time.UTC)
	id := contractSeed(t, f)
	k, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "live history", Size: 100, CreatedUserID: "owner", Filters: store.ProspectListFilters{MinCampaignAgeDays: ptrInt(30)}})
	contractOK(t, err)
	check := func(n int64) {
		t.Helper()
		r, err := f.s.ReadProspectList(ctx, k.ID)
		contractOK(t, err)
		contractEqual(t, r.Matching, n)
		contractEqual(t, len(r.Members), int(n))
		if n == 1 {
			contractEqual(t, r.Members[0].Candidate.ID, id)
		}
	}
	check(1)
	at := time.Date(2026, 10, 5, 23, 59, 59, 0, time.UTC)
	contractOK(t, f.s.AddCampaignUse(ctx, id, "campaign", "", at))
	check(0)
	contractOK(t, f.s.RejectCampaignUse(ctx, id, "campaign", "rejected", f.now))
	check(0)
	_, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, UserID: "owner", Filters: &store.ProspectListFilters{MinCampaignAgeDays: ptrInt(10)}})
	contractOK(t, err)
	check(1)
	_, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, UserID: "owner", Filters: &store.ProspectListFilters{MinCampaignAgeDays: ptrInt(30)}})
	contractOK(t, err)
	check(0)
	f.now = time.Date(2026, 11, 4, 0, 0, 1, 0, time.UTC)
	check(1)
}

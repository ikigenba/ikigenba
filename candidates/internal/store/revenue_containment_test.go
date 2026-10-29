package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-LW6E-VA6N R-LYM7-MTO1 R-IITT-5P49 R-LXEB-91XC R-IQ57-GBKF
func TestProspectListRevenueContainmentAndDepartures(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	estimates := []struct{ low, high *int64 }{
		{ptrInt(5000000), ptrInt(10000000)},
		{ptrInt(8000000), ptrInt(80000000)},
		{ptrInt(4999999), ptrInt(9000000)},
		{ptrInt(5000000), ptrInt(10000001)},
		{ptrInt(6000000), nil},
		{ptrInt(4999999), nil},
		{nil, nil},
		{ptrInt(0), ptrInt(10000000)},
		{ptrInt(-1), ptrInt(5000000)},
		{ptrInt(0), nil},
		{ptrInt(10000000), ptrInt(10000000)},
		{ptrInt(4999999), ptrInt(80000000)},
	}
	ids := []string{}
	for i := range estimates {
		ids = append(ids, enrichmentCandidate(t, f, fmt.Sprint(i), fmt.Sprintf("revenue%d@test.invalid", i)))
	}
	_, err := f.s.Judge(ctx, ids)
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		for i, e := range estimates {
			if _, err := tx.ExecContext(ctx, "UPDATE judgments SET revenue_low=?,revenue_high=? WHERE candidate=?", e.low, e.high, ids[i]); err != nil {
				return err
			}
		}
		return nil
	}))
	for _, tc := range []struct {
		name        string
		filters     store.ProspectListFilters
		passing     []int
		failMinimum []int
	}{
		{"no bounds", store.ProspectListFilters{}, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, nil},
		{"switch alone", store.ProspectListFilters{RevenueUnknown: true}, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, nil},
		{"window", store.ProspectListFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000)}, []int{0, 10}, []int{2, 5, 6, 7, 8, 9, 11}},
		{"window admitting unknowns", store.ProspectListFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000), RevenueUnknown: true}, []int{0, 4, 6, 10}, []int{2, 5, 7, 8, 9, 11}},
		{"minimum", store.ProspectListFilters{RevenueMin: ptrInt(5000000)}, []int{0, 1, 3, 4, 10}, []int{2, 5, 6, 7, 8, 9, 11}},
		{"minimum admitting unknowns", store.ProspectListFilters{RevenueMin: ptrInt(5000000), RevenueUnknown: true}, []int{0, 1, 3, 4, 6, 10}, []int{2, 5, 7, 8, 9, 11}},
		{"maximum", store.ProspectListFilters{RevenueMax: ptrInt(10000000)}, []int{0, 2, 7, 10}, nil},
		{"maximum admitting unknowns", store.ProspectListFilters{RevenueMax: ptrInt(10000000), RevenueUnknown: true}, []int{0, 2, 4, 5, 6, 7, 9, 10}, nil},
		{"equal bounds", store.ProspectListFilters{RevenueMin: ptrInt(10000000), RevenueMax: ptrInt(10000000)}, []int{10}, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 11}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := prospectListCreate(t, f, tc.name)
			before := openList(t, f, k.ID)
			contractEqual(t, len(before.Members), len(ids))
			k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &tc.filters, UserID: "owner"})
			contractOK(t, err)
			f.reopen()
			got := prospectListRead(t, f, k)
			want := []string{}
			for _, i := range tc.passing {
				want = append(want, ids[i])
			}
			slices.Sort(want)
			actual := prospectListIDs(got.Members)
			slices.Sort(actual)
			contractEqual(t, actual, want)
			contractEqual(t, got.Matching, int64(len(want)))
			wantLeft := []store.Left{}
			for _, m := range before.Members {
				i := slices.Index(ids, m.Candidate.ID)
				if slices.Contains(tc.passing, i) {
					continue
				}
				filter := "revenue_max"
				if slices.Contains(tc.failMinimum, i) {
					filter = "revenue_min"
				}
				wantLeft = append(wantLeft, store.Left{Candidate: m.Candidate.ID, Name: m.Candidate.Name, BasisThen: m.Basis, BasisNow: m.Basis, Why: store.WhyFilter, Filter: filter})
			}
			contractEqual(t, got.Changes.Left, wantLeft)
			contractEqual(t, got.Changes.Joined, []store.Joined{})
			combined := tc.filters
			combined.MaxMinutes = ptrInt(0)
			combinedList := prospectListCreate(t, f, tc.name+" combined")
			openList(t, f, combinedList.ID)
			combinedList, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: combinedList.ID, Filters: &combined, UserID: "owner"})
			contractOK(t, err)
			combinedRead := prospectListRead(t, f, combinedList)
			contractEqual(t, combinedRead.Matching, int64(0))
			wantCombinedLeft := []store.Left{}
			for _, m := range before.Members {
				i := slices.Index(ids, m.Candidate.ID)
				filter := "revenue_max"
				if slices.Contains(tc.passing, i) {
					filter = "max_minutes"
				} else if slices.Contains(tc.failMinimum, i) {
					filter = "revenue_min"
				}
				wantCombinedLeft = append(wantCombinedLeft, store.Left{Candidate: m.Candidate.ID, Name: m.Candidate.Name, BasisThen: m.Basis, BasisNow: m.Basis, Why: store.WhyFilter, Filter: filter})
			}
			contractEqual(t, combinedRead.Changes.Left, wantCombinedLeft)
			opened := openList(t, f, k.ID)
			contractEqual(t, opened, got)
			contractEqual(t, listOpens(t, f, k.ID)[0].Passing, prospectListIDs(got.Members))
		})
	}
}

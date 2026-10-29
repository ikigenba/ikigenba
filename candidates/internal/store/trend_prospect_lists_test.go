package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-AZ9G-DH4V R-LYM7-MTO1 R-IITT-5P49 R-B0HC-R8VK R-OUXR-R9EF
func TestProspectListTrendFilterMatrix(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := prospectListPopulation(t, f)
	directions := []string{judging.TrendGrowing, judging.TrendFlat, judging.TrendShrinking, judging.TrendUnknown, judging.TrendGrowing}
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, "UPDATE judgments SET trend=? WHERE candidate=?", directions[i], id); err != nil {
				return err
			}
		}
		return nil
	}))
	k := prospectListCreate(t, f, "trend matrix")
	for _, trends := range [][]string{nil, {}, {judging.TrendGrowing}, {judging.TrendGrowing, judging.TrendFlat}, {judging.TrendShrinking}, {judging.TrendGrowing, judging.TrendGrowing}} {
		for _, unknown := range []bool{false, true} {
			filters := store.ProspectListFilters{Trend: trends, TrendUnknown: unknown}
			contractEqual(t, store.ValidProspectListFilters(filters), true)
			var err error
			k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
			contractOK(t, err)
			got := prospectListRead(t, f, k)
			want := []string{}
			for i, id := range ids {
				if len(trends) == 0 || slices.Contains(trends, directions[i]) || directions[i] == judging.TrendUnknown && unknown {
					want = append(want, id)
				}
			}
			actual := prospectListIDs(got.Members)
			slices.Sort(actual)
			slices.Sort(want)
			contractEqual(t, actual, want)
			contractEqual(t, got.Matching, int64(len(want)))
		}
	}
	for _, bad := range []string{judging.TrendUnknown, "", "Growing"} {
		contractEqual(t, store.ValidProspectListFilters(store.ProspectListFilters{Trend: []string{bad}}), false)
	}
	contractEqual(t, store.OrderTrend, "trend")
}

// R-B5CY-ABUC R-B1P9-50M9
func TestProspectListTrendOrderChangesUnknownsAndTies(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	changes := []int64{141, 75, 31, 3, 1, 0, -15, -18, -20, 31}
	ids := []string{}
	for i := 0; i <= len(changes); i++ {
		ids = append(ids, enrichmentCandidate(t, f, fmt.Sprint(i), fmt.Sprintf("trend%d@test.invalid", i)))
	}
	_, err := f.s.Judge(ctx, ids)
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		for i, change := range changes {
			if _, err := tx.ExecContext(ctx, "UPDATE judgments SET trend_change=? WHERE candidate=?", change, ids[i]); err != nil {
				return err
			}
		}
		return nil
	}))
	k := prospectListCreate(t, f, "trend order")
	for _, direction := range []string{store.DirectionAsc, store.DirectionDesc} {
		order := []store.ProspectListOrder{{Field: store.OrderTrend, Direction: direction}}
		contractEqual(t, store.ValidProspectListOrder(order), true)
		k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Order: &order, UserID: "owner"})
		contractOK(t, err)
		want := slices.Clone(ids[:len(changes)])
		values := map[string]int64{}
		for i, c := range changes {
			values[ids[i]] = c
		}
		slices.SortFunc(want, func(a, b string) int {
			if values[a] == values[b] {
				if a < b {
					return -1
				}
				if a > b {
					return 1
				}
				return 0
			}
			if values[a] < values[b] {
				if direction == store.DirectionAsc {
					return -1
				}
				return 1
			}
			if direction == store.DirectionAsc {
				return 1
			}
			return -1
		})
		want = append(want, ids[len(changes)])
		contractEqual(t, prospectListIDs(prospectListRead(t, f, k).Members), want)
	}
	contractEqual(t, store.ValidProspectListOrder([]store.ProspectListOrder{{Field: store.OrderTrend, Direction: store.DirectionAsc}, {Field: store.OrderTrend, Direction: store.DirectionDesc}}), false)
}

// R-OW5O-5154 R-ZPJT-SKZZ R-ZRZM-K4HD R-INPE-OS31 R-ZT7I-XW82
func TestProspectListTrendJSONNormalFormAndOpening(t *testing.T) {
	for _, tc := range []struct {
		f    store.ProspectListFilters
		want string
	}{
		{store.ProspectListFilters{Trend: []string{judging.TrendGrowing, judging.TrendFlat}, TrendUnknown: true}, `{"trend":["growing","flat"],"trend_unknown":true}`},
		{store.ProspectListFilters{TrendUnknown: true}, `{"trend_unknown":true}`},
		{store.ProspectListFilters{Trend: []string{}}, `{}`},
	} {
		raw, err := json.Marshal(tc.f)
		contractOK(t, err)
		contractEqual(t, string(raw), tc.want)
	}
	f := contractDB(t)
	ctx := context.Background()
	filters := store.ProspectListFilters{Trend: []string{judging.TrendShrinking, judging.TrendFlat, judging.TrendGrowing, judging.TrendFlat}}
	k, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "normalized trends", Size: 100, Filters: filters, CreatedUserID: "owner"})
	contractOK(t, err)
	contractEqual(t, k.Filters.Trend, []string{judging.TrendGrowing, judging.TrendFlat, judging.TrendShrinking})
	openList(t, f, k.ID)
	filters.Trend = []string{judging.TrendFlat, judging.TrendGrowing, judging.TrendShrinking}
	same, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "editor"})
	contractOK(t, err)
	contractEqual(t, same, k)
	contractEqual(t, prospectListRead(t, f, k).Changes.DefinitionChanged, []string{})
	f.reopen()
	contractEqual(t, listOpens(t, f, k.ID)[0].Filters, k.Filters)
	filters.Trend = []string{}
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "editor"})
	contractOK(t, err)
	contractEqual(t, k.Filters.Trend, []string(nil))
}

// R-LXEB-91XC R-IQ57-GBKF
func TestProspectListTrendDeparturePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		filters store.ProspectListFilters
		want    string
	}{
		{"website before trend", store.ProspectListFilters{Website: []string{sources.WebsiteHTTPS}, Trend: []string{judging.TrendGrowing}}, "website"},
		{"trend before government", store.ProspectListFilters{Trend: []string{judging.TrendGrowing}, Government: new(bool)}, "trend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := contractDB(t)
			ids := prospectListPopulation(t, f)
			k := prospectListCreate(t, f, "departures")
			openList(t, f, k.ID)
			var err error
			k, err = f.s.EditProspectList(context.Background(), store.ProspectListEdit{ID: k.ID, Filters: &tc.filters, UserID: "owner"})
			contractOK(t, err)
			for _, left := range prospectListRead(t, f, k).Changes.Left {
				if left.Candidate == ids[0] {
					contractEqual(t, left.Filter, tc.want)
					contractEqual(t, left.Why, store.WhyFilter)
					return
				}
			}
			t.Fatal("expected departure")
		})
	}
}

// R-PC0D-41S5
func TestPreviousWriterProspectListFiltersDropTrend(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	k, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "previous writer", Size: 30, Filters: store.ProspectListFilters{Trend: []string{judging.TrendGrowing}, TrendUnknown: true}, CreatedUserID: "owner"})
	contractOK(t, err)
	raw := `{"revenue_min":123,"website":["https"],"website_unknown":true}`
	insertedID := "pl_abcdefabcdefabcd"
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO prospect_lists (id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed) SELECT ?,name,?,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM prospect_lists WHERE id=?`, insertedID, raw, k.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE prospect_lists SET filters=? WHERE id=?`, raw, k.ID)
		return err
	}))
	for _, id := range []string{k.ID, insertedID} {
		got, err := f.s.GetProspectList(ctx, id)
		contractOK(t, err)
		want := k
		want.ID = id
		want.Filters = store.ProspectListFilters{RevenueMin: ptrInt(123), Website: []string{sources.WebsiteHTTPS}, WebsiteUnknown: true}
		contractEqual(t, got, want)
	}
	f.reopen()
	got, err := f.s.GetProspectList(ctx, k.ID)
	contractOK(t, err)
	contractEqual(t, got.Filters.Trend, []string(nil))
	contractEqual(t, got.Filters.TrendUnknown, false)
}

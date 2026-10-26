package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func openList(t *testing.T, f *contractFixture, id string) store.ProspectListRead {
	t.Helper()
	r, err := f.s.OpenProspectList(context.Background(), id, "reader", "reader@example.test")
	contractOK(t, err)
	return r
}
func listOpens(t *testing.T, f *contractFixture, id string) []store.Open {
	t.Helper()
	r, err := f.s.Opens(context.Background(), id)
	contractOK(t, err)
	if r == nil {
		t.Fatal("nil opens")
	}
	return r
}
func emptyChanges() store.Changes {
	return store.Changes{DefinitionChanged: []string{}, Joined: []store.Joined{}, Left: []store.Left{}}
}

// R-L607-M6YO R-3C8B-UG2Y R-NW7S-1GHY R-NXFO-F88N R-XUIO-0E44 R-BGL2-0169 R-BJ0U-RKNN
func TestProspectListOpenSnapshotAndPersistence(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	prospectListPopulation(t, f)
	k := prospectListCreate(t, f, "opened list")
	size := int64(2)
	var err error
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Size: &size, UserID: "owner"})
	contractOK(t, err)
	before := prospectListRead(t, f, k)
	contractEqual(t, before.Changes, emptyChanges())
	contractEqual(t, prospectListRead(t, f, k), before)
	contractEqual(t, len(listOpens(t, f, k.ID)), 0)
	f.now = f.now.Add(123456789 * time.Nanosecond)
	recordsBefore := contractState(t, f)
	got := openList(t, f, k.ID)
	contractEqual(t, contractState(t, f), recordsBefore)
	current, err := f.s.GetProspectList(ctx, k.ID)
	contractOK(t, err)
	contractEqual(t, current, k)
	contractEqual(t, got, before)
	os := listOpens(t, f, k.ID)
	contractEqual(t, len(os), 1)
	o := os[0]
	contractEqual(t, o.ProspectList, k.ID)
	contractEqual(t, o.At, f.now.UTC().Truncate(time.Second))
	contractEqual(t, o.At.Location(), time.UTC)
	contractEqual(t, o.UserID, "reader")
	contractEqual(t, o.UserEmail, "reader@example.test")
	contractEqual(t, o.Filters, k.Filters)
	contractEqual(t, o.Order, k.Order)
	contractEqual(t, o.Size, k.Size)
	wantMembers := []store.OpenMember{}
	for _, m := range got.Members {
		wantMembers = append(wantMembers, store.OpenMember{Candidate: m.Candidate.ID, Basis: m.Basis})
	}
	contractEqual(t, o.Members, wantMembers)
	all, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "all", Size: 100, CreatedUserID: "owner"})
	contractOK(t, err)
	contractEqual(t, o.Passing, prospectListIDs(prospectListRead(t, f, all).Members))
	other := openList(t, f, all.ID)
	contractEqual(t, other.Changes, emptyChanges())
	if listOpens(t, f, all.ID)[0].Seq <= o.Seq {
		t.Fatal("sequence not global")
	}
	unchanged := prospectListRead(t, f, k)
	contractEqual(t, unchanged.Changes.Since, o.At)
	contractEqual(t, unchanged.Changes.Joined, []store.Joined{})
	contractEqual(t, unchanged.Changes.Left, []store.Left{})
	f.reopen()
	contractEqual(t, listOpens(t, f, k.ID), os)
	contractEqual(t, prospectListRead(t, f, k), unchanged)
	f.now = f.now.Add(time.Second)
	contractEqual(t, openList(t, f, k.ID), unchanged)
	two := listOpens(t, f, k.ID)
	contractEqual(t, len(two), 2)
	contractEqual(t, two[1], o)
	if two[0].Seq <= two[1].Seq {
		t.Fatal("opens not newest first")
	}
	_, err = f.s.DeleteProspectList(ctx, k.ID)
	contractOK(t, err)
	contractEqual(t, listOpens(t, f, k.ID), two)
	f.reopen()
	contractEqual(t, listOpens(t, f, k.ID), two)
	for _, id := range []string{"", "invalid", "pl_0000000000000000"} {
		contractEqual(t, len(listOpens(t, f, id)), 0)
	}
}

// R-LAVT-59XG R-3IBT-RASF
func TestProspectListOpenRefusalsAndEmptySnapshots(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	k := prospectListCreate(t, f, "empty")
	for _, tc := range []struct {
		id, user string
		want     error
	}{{k.ID, "", store.ErrUnreachable}, {"bad", "", store.ErrUnreachable}, {"bad", "reader", store.ErrNotFound}, {"pl_0000000000000000", "reader", store.ErrNotFound}} {
		got, err := f.s.OpenProspectList(ctx, tc.id, tc.user, "")
		contractError(t, err, tc.want)
		contractEqual(t, got, store.ProspectListRead{})
		contractEqual(t, len(listOpens(t, f, k.ID)), 0)
	}
	got := openList(t, f, k.ID)
	contractEqual(t, got.Changes, emptyChanges())
	os := listOpens(t, f, k.ID)
	contractEqual(t, os[0].Members, []store.OpenMember{})
	contractEqual(t, os[0].Passing, []string{})
	name := "renamed"
	_, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Name: &name, UserID: "owner"})
	contractOK(t, err)
	_, err = f.s.ReadProspectList(ctx, k.ID)
	contractOK(t, err)
	_, _, err = f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, err)
	_, err = f.s.PrunePasses(ctx, 1, 500)
	contractOK(t, err)
	contractEqual(t, listOpens(t, f, k.ID), os)
	_, err = f.s.DeleteProspectList(ctx, k.ID)
	contractOK(t, err)
	failed, err := f.s.OpenProspectList(ctx, k.ID, "reader", "")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, failed, store.ProspectListRead{})
	contractEqual(t, listOpens(t, f, k.ID), os)
}

// R-KTT7-SHJQ
func TestConcurrentProspectListOpensSerialize(t *testing.T) {
	f := contractDB(t)
	k := prospectListCreate(t, f, "concurrent opens")
	var wg sync.WaitGroup
	results := make([]store.ProspectListRead, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Go(func() { results[i], errs[i] = f.s.OpenProspectList(context.Background(), k.ID, "reader", "") })
	}
	wg.Wait()
	zero := 0
	for i, r := range results {
		contractOK(t, errs[i])
		if r.Changes.Since.IsZero() {
			zero++
		} else {
			contractEqual(t, r.Changes.Since, f.now.UTC().Truncate(time.Second))
		}
	}
	contractEqual(t, zero, 1)
	contractEqual(t, len(listOpens(t, f, k.ID)), 2)
}

// R-BGL2-0169
func TestProspectListDefinitionChangesCompareNormalizedSnapshot(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	k := prospectListCreate(t, f, "definitions")
	filters := store.ProspectListFilters{Categories: []string{sources.CategoryTrucking, sources.CategoryConstruction}, Basis: []string{store.BasisEnriched, store.BasisRecords}, Website: []string{sources.WebsiteHTTPS, sources.WebsiteHTTP}}
	k, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	openList(t, f, k.ID)
	original := k
	filters.Categories = []string{sources.CategoryConstruction, sources.CategoryTrucking}
	filters.Basis = []string{store.BasisRecords, store.BasisEnriched}
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	contractEqual(t, prospectListRead(t, f, k).Changes.DefinitionChanged, []string{})
	filters.Website = []string{sources.WebsiteHTTP, sources.WebsiteHTTPS}
	order := []store.ProspectListOrder{{Field: store.OrderRevenueLow, Direction: store.DirectionAsc}}
	size := int64(2)
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, Order: &order, Size: &size, UserID: "owner"})
	contractOK(t, err)
	contractEqual(t, prospectListRead(t, f, k).Changes.DefinitionChanged, []string{store.DefinitionFilters, store.DefinitionOrder, store.DefinitionSize})
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &original.Filters, Order: &original.Order, Size: &original.Size, UserID: "owner"})
	contractOK(t, err)
	contractEqual(t, prospectListRead(t, f, k).Changes.DefinitionChanged, []string{})
}

// R-BHSY-DSWY R-BCXC-UPY6 R-BJ0U-RKNN
func TestProspectListJoinedAndLeftReasons(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := prospectListPopulation(t, f)
	k := prospectListCreate(t, f, "changes")
	size := int64(1)
	k, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Size: &size, UserID: "owner"})
	contractOK(t, err)
	first := openList(t, f, k.ID)
	firstID := first.Members[0].Candidate.ID
	_, err = f.s.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: k.ID, Candidate: firstID, UserID: "owner", Why: "not suitable"})
	contractOK(t, err)
	r := prospectListRead(t, f, k)
	contractEqual(t, len(r.Changes.Joined), 1)
	contractEqual(t, r.Changes.Joined[0], store.Joined{Candidate: r.Members[0].Candidate.ID, Name: r.Members[0].Candidate.Name, Basis: r.Members[0].Basis, Why: store.WhyMovedUp})
	contractEqual(t, r.Changes.Left, []store.Left{{Candidate: firstID, Name: first.Members[0].Candidate.Name, BasisThen: store.BasisRecords, BasisNow: store.BasisRecords, Why: store.WhyRejected}})
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: firstID, UserID: "owner", Why: "excluded"})
	contractOK(t, err)
	contractEqual(t, prospectListRead(t, f, k).Changes.Left[0].Why, store.WhyExcluded)
	survivor := ids[0]
	if survivor == firstID {
		survivor = ids[1]
	}
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: firstID, UserID: "owner", Why: "merged"})
	contractOK(t, err)
	gone := prospectListRead(t, f, k).Changes.Left[0]
	contractEqual(t, gone.Why, store.WhyGone)
	contractEqual(t, gone.BasisNow, "")
	contractEqual(t, gone.Filter, "")
}

// R-XWKG-4W62 R-BCXC-UPY6
func TestProspectListFirstFailingFilterInChanges(t *testing.T) {
	for _, tc := range []struct {
		name    string
		index   int
		filters store.ProspectListFilters
		want    string
	}{
		{"low", 2, store.ProspectListFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000)}, "revenue_min"},
		{"unknown low", 4, store.ProspectListFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000)}, "revenue_min"},
		{"high", 3, store.ProspectListFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000)}, "revenue_max"},
		{"unknown high", 4, store.ProspectListFilters{RevenueMax: ptrInt(10000000)}, "revenue_max"},
		{"minutes before fleet", 0, store.ProspectListFilters{MaxMinutes: ptrInt(1), Fleet: sources.FleetFail}, "max_minutes"},
		{"category", 1, store.ProspectListFilters{Categories: []string{sources.CategoryTrucking}}, "categories"},
		{"category unknown", 1, store.ProspectListFilters{CategoryUnknown: true}, "category_unknown"},
		{"fleet", 0, store.ProspectListFilters{Fleet: sources.FleetFail}, "fleet"},
		{"website", 0, store.ProspectListFilters{Website: []string{sources.WebsiteHTTPS}}, "website"},
		{"government", 0, store.ProspectListFilters{Government: new(bool)}, "government"},
		{"filing", 0, store.ProspectListFilters{MaxFilingAgeDays: ptrInt(1)}, "max_filing_age_days"},
		{"campaign", 0, store.ProspectListFilters{MinCampaignAgeDays: ptrInt(30)}, "min_campaign_age_days"},
		{"class before basis", 0, store.ProspectListFilters{Class: sources.ClassPrivate, Basis: []string{store.BasisEnriched}}, "class"},
		{"basis", 0, store.ProspectListFilters{Basis: []string{store.BasisEnriched}}, "basis"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			ids := prospectListPopulation(t, f)
			k := prospectListCreate(t, f, "filters")
			openList(t, f, k.ID)
			if tc.want == "min_campaign_age_days" {
				err := f.s.AddCampaignUse(ctx, ids[tc.index], "campaign", "", f.now)
				contractOK(t, err)
			}
			k, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &tc.filters, UserID: "owner"})
			contractOK(t, err)
			changes := prospectListRead(t, f, k).Changes
			found := false
			for _, left := range changes.Left {
				if left.Candidate == ids[tc.index] {
					found = true
					contractEqual(t, left.Why, store.WhyFilter)
					contractEqual(t, left.Filter, tc.want)
				}
			}
			if !found {
				t.Fatal("expected candidate did not leave")
			}
		})
	}
}

// R-BFD5-M9FK
func TestProspectListByBasisCountsOnlyMembers(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := prospectListPopulation(t, f)
	k := prospectListCreate(t, f, "basis counts")
	initial := prospectListRead(t, f, k)
	first := initial.Members[0].Candidate.ID
	third := initial.Members[2].Candidate.ID
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, first, "enriched", false), briefResult(t, third, "partial", false)})
	contractOK(t, err)
	minutes := int64(10)
	appendRoute(t, f.s, first, "route", enrichers.RouteFound{Minutes: &minutes})
	size := int64(2)
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Size: &size, UserID: "owner"})
	contractOK(t, err)
	r := prospectListRead(t, f, k)
	contractEqual(t, r.ByBasis, store.BasisCounts{Records: 1, Enriched: 1})
	contractEqual(t, r.Matching, int64(len(ids)))
	filters := store.ProspectListFilters{Basis: []string{store.BasisPartial}}
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	contractEqual(t, prospectListRead(t, f, k).ByBasis, store.BasisCounts{Partial: 1})
	for _, id := range ids {
		_, err = f.s.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: k.ID, Candidate: id, UserID: "owner", Why: fmt.Sprint(id)})
		contractOK(t, err)
	}
	contractEqual(t, prospectListRead(t, f, k).ByBasis, store.BasisCounts{})
}

// R-L14M-33ZW R-L2CI-GVQL R-L3KE-UNHA R-NRC6-IDJ6 R-NTRZ-9X0K R-3B0F-GOC9 R-KL9X-43CV
func TestOpenRecordShapesByUse(t *testing.T) {
	type counts struct{ Records, Partial, Enriched int64 }
	type changes struct {
		Since             time.Time
		DefinitionChanged []string
		Joined            []store.Joined
		Left              []store.Left
	}
	type joined struct{ Candidate, Name, Basis, Why string }
	type left struct{ Candidate, Name, BasisThen, BasisNow, Why, Filter string }
	type member struct{ Candidate, Basis string }
	type open struct {
		Seq               int64
		ProspectList      string
		At                time.Time
		UserID, UserEmail string
		Filters           store.ProspectListFilters
		Order             []store.ProspectListOrder
		Size              int64
		Members           []store.OpenMember
		Passing           []string
	}
	type read struct {
		ProspectList store.ProspectList
		Matching     int64
		Classified   int64
		ByBasis      store.BasisCounts
		Changes      store.Changes
		Members      []store.Ranked
		Rejects      []store.Reject
	}
	contractEqual(t, counts(store.BasisCounts{}), counts{})
	contractEqual(t, changes(store.Changes{}), changes{})
	contractEqual(t, joined(store.Joined{}), joined{})
	contractEqual(t, left(store.Left{}), left{})
	contractEqual(t, member(store.OpenMember{}), member{})
	contractEqual(t, open(store.Open{}), open{})
	contractEqual(t, read(store.ProspectListRead{}), read{})
}

// R-BHSY-DSWY R-BCXC-UPY6
func TestProspectListNewBoundaryPassesAndCut(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	equal := contractAttach(t, f, "equal", contractObservation("equal", "Same second", time.Time{}))[0]
	deriveAll(t, f.s)
	_, _, err := f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, err)
	filters := store.ProspectListFilters{Basis: []string{store.BasisEnriched}}
	k, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "new boundary", Size: 100, Filters: filters, CreatedUserID: "owner"})
	contractOK(t, err)
	openList(t, f, k.ID)
	f.now = f.now.Add(time.Second)
	newer := contractAttach(t, f, "newer", contractObservation("newer", "Later second", time.Time{}))[0]
	deriveAll(t, f.s)
	_, _, err = f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, err)
	filters = store.ProspectListFilters{}
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	r := prospectListRead(t, f, k)
	contractEqual(t, len(r.Changes.Joined), 2)
	for i, j := range r.Changes.Joined {
		contractEqual(t, j.Candidate, r.Members[i].Candidate.ID)
		contractEqual(t, j.Name, r.Members[i].Candidate.Name)
		contractEqual(t, j.Basis, store.BasisRecords)
		switch j.Candidate {
		case equal:
			contractEqual(t, j.Why, store.WhyPasses)
		case newer:
			contractEqual(t, j.Why, store.WhyNew)
		default:
			t.Fatal("unexpected joined member")
		}
	}
	before := openList(t, f, k.ID)
	size := int64(1)
	k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Size: &size, UserID: "owner"})
	contractOK(t, err)
	r = prospectListRead(t, f, k)
	contractEqual(t, r.Changes.Joined, []store.Joined{})
	contractEqual(t, r.Changes.Left, []store.Left{{Candidate: before.Members[1].Candidate.ID, Name: before.Members[1].Candidate.Name, BasisThen: store.BasisRecords, BasisNow: store.BasisRecords, Why: store.WhyCut}})
}

// R-BCXC-UPY6
func TestProspectListRouteLeavingRetainsThenAndNowBasis(t *testing.T) {
	f := contractDB(t)
	f.s = routeStore(f, "test origin")
	ctx := context.Background()
	id := contractSeed(t, f)
	deriveAll(t, f.s)
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, "good brief", false)})
	contractOK(t, err)
	_, _, err = f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, err)
	filters := store.ProspectListFilters{MaxMinutes: ptrInt(100), MinutesUnknown: true}
	k, err := f.s.CreateProspectList(ctx, store.ProspectList{Name: "route changed", Size: 100, Filters: filters, CreatedUserID: "owner"})
	contractOK(t, err)
	before := openList(t, f, k.ID)
	contractEqual(t, len(before.Members), 1)
	f.now = f.now.Add(time.Second)
	minutes := int64(101)
	subject, err := f.s.Subject(ctx, id)
	contractOK(t, err)
	appendRoute(t, f.s, id, (enrichers.DriveRoute{}).Inputs(subject), enrichers.RouteFound{Minutes: &minutes})
	r := prospectListRead(t, f, k)
	contractEqual(t, r.Changes.Left, []store.Left{{Candidate: id, Name: before.Members[0].Candidate.Name, BasisThen: store.BasisPartial, BasisNow: store.BasisEnriched, Why: store.WhyFilter, Filter: "max_minutes"}})
}

// R-KXGW-XSRT R-NQ4A-4LSH
func TestProspectListChangeVocabulary(t *testing.T) {
	type changeWord string
	contractEqual(t, []changeWord{store.DefinitionFilters, store.DefinitionOrder, store.DefinitionSize}, []changeWord{"filters", "order", "size"})
	contractEqual(t, []changeWord{store.WhyNew, store.WhyPasses, store.WhyMovedUp, store.WhyFilter, store.WhyRejected, store.WhyExcluded, store.WhyCut, store.WhyGone}, []changeWord{"new", "passes", "moved_up", "filter", "rejected", "excluded", "cut", "gone"})
}

// R-LAVT-59XG
func TestProspectListOpenFailureDoesNotRecord(t *testing.T) {
	f := contractDB(t)
	k := prospectListCreate(t, f, "failing reads")
	original := openList(t, f, k.ID)
	os := listOpens(t, f, k.ID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := f.s.OpenProspectList(ctx, k.ID, "reader", "")
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, got, store.ProspectListRead{})
	contractEqual(t, listOpens(t, f, k.ID), os)
	contractOK(t, f.d.Close())
	got, err = f.s.OpenProspectList(context.Background(), k.ID, "reader", "")
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, got, store.ProspectListRead{})
	f.open()
	contractEqual(t, listOpens(t, f, k.ID), os)
	contractEqual(t, prospectListRead(t, f, k).Members, original.Members)
}

// R-NW7S-1GHY R-XUIO-0E44
func TestProspectListOpenUsesOneClockSnapshot(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	k := prospectListCreate(t, f, "clock snapshot")
	calls := 0
	instant := f.now
	f.s = store.New(f.d, store.Config{Now: func() time.Time { calls++; return instant.Add(time.Duration(calls-1) * 24 * time.Hour) }, Rand: f.rnd})
	expected, err := f.s.ReadProspectList(ctx, k.ID)
	contractOK(t, err)
	contractEqual(t, calls, 1)
	calls = 0
	got := openList(t, f, k.ID)
	contractEqual(t, calls, 1)
	contractEqual(t, got, expected)
	contractEqual(t, listOpens(t, f, k.ID)[0].At, instant.UTC().Truncate(time.Second))
	contractEqual(t, calls, 1)
}

// R-BHSY-DSWY
func TestProspectListJoinedTransitionTriggers(t *testing.T) {
	for _, trigger := range []string{"brief", "unreject", "include", "campaign age", "size", "order", "filter with previous passing", "filter previously failed"} {
		t.Run(trigger, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			ids := prospectListPopulation(t, f)
			k := prospectListCreate(t, f, "joined transitions")
			target := ids[1]
			filters := store.ProspectListFilters{}
			size := int64(100)
			switch trigger {
			case "brief":
				filters.Categories = []string{sources.CategoryRetail}
			case "unreject":
				_, err := f.s.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: k.ID, Candidate: target, UserID: "owner", Why: "set aside"})
				contractOK(t, err)
			case "include":
				_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: target, UserID: "owner", Why: "set aside"})
				contractOK(t, err)
			case "campaign age":
				filters.MinCampaignAgeDays = ptrInt(30)
				contractOK(t, f.s.AddCampaignUse(ctx, target, "recent campaign", "", f.now))
			case "size", "order", "filter with previous passing":
				size = 1
			case "filter previously failed":
				filters.Class = sources.ClassForHire
			}
			var err error
			k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, Size: &size, UserID: "owner"})
			contractOK(t, err)
			before := openList(t, f, k.ID)
			opensBefore := listOpens(t, f, k.ID)
			f.now = f.now.Add(time.Second)
			wantWhy := store.WhyPasses
			switch trigger {
			case "brief":
				_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, target, "now retail", false, sources.CategoryRetail)})
				contractOK(t, err)
			case "unreject":
				_, err = f.s.Reject(ctx, store.Reject{Kind: store.KindUnreject, ProspectList: k.ID, Candidate: target, UserID: "owner", Why: "restored"})
				contractOK(t, err)
			case "include":
				_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: target, UserID: "owner", Why: "restored"})
				contractOK(t, err)
			case "campaign age":
				f.now = f.now.AddDate(0, 0, 30)
			case "size", "filter with previous passing":
				size = 100
				wantWhy = store.WhyMovedUp
				if trigger == "filter with previous passing" {
					filters.ShowExcluded = true
				}
				k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Size: &size, Filters: &filters, UserID: "owner"})
				contractOK(t, err)
			case "order":
				wantWhy = store.WhyMovedUp
				order := []store.ProspectListOrder{{Field: store.OrderOperatingConfidence, Direction: store.DirectionAsc}}
				k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Order: &order, UserID: "owner"})
				contractOK(t, err)
			case "filter previously failed":
				filters = store.ProspectListFilters{}
				k, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
				contractOK(t, err)
			}
			after := prospectListRead(t, f, k)
			contractEqual(t, listOpens(t, f, k.ID), opensBefore)
			old := map[string]bool{}
			for _, m := range before.Members {
				old[m.Candidate.ID] = true
			}
			want := []store.Joined{}
			for _, m := range after.Members {
				if !old[m.Candidate.ID] {
					want = append(want, store.Joined{Candidate: m.Candidate.ID, Name: m.Candidate.Name, Basis: m.Basis, Why: wantWhy})
				}
			}
			if len(want) == 0 {
				t.Fatal("fixture produced no joining member")
			}
			contractEqual(t, after.Changes.Joined, want)
			if trigger == "campaign age" || trigger == "unreject" || trigger == "include" || trigger == "brief" || trigger == "filter previously failed" {
				found := false
				for _, j := range after.Changes.Joined {
					if j.Candidate == target {
						found = true
					}
				}
				if !found {
					t.Fatal("expected target did not join")
				}
			}
		})
	}
}

// R-BCXC-UPY6
func TestProspectListLeftRetainsPriorOrderAndRejectPrecedesFilter(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	prospectListPopulation(t, f)
	k := prospectListCreate(t, f, "ordered departures")
	before := openList(t, f, k.ID)
	for _, m := range before.Members {
		_, err := f.s.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: k.ID, Candidate: m.Candidate.ID, UserID: "owner", Why: "rejected"})
		contractOK(t, err)
	}
	filters := store.ProspectListFilters{Basis: []string{store.BasisEnriched}}
	k, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	r := prospectListRead(t, f, k)
	want := []store.Left{}
	for _, m := range before.Members {
		want = append(want, store.Left{Candidate: m.Candidate.ID, Name: m.Candidate.Name, BasisThen: m.Basis, BasisNow: m.Basis, Why: store.WhyRejected})
	}
	contractEqual(t, r.Changes.Left, want)
}

// R-BGL2-0169 R-BJ0U-RKNN
func TestProspectListLatestOpenAndReorderedMembership(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	prospectListPopulation(t, f)
	k := prospectListCreate(t, f, "same members")
	first := openList(t, f, k.ID)
	contractEqual(t, first.Changes, emptyChanges())
	firstAt := f.now.UTC().Truncate(time.Second)
	f.now = f.now.Add(time.Second)
	order := []store.ProspectListOrder{{Field: store.OrderOperatingConfidence, Direction: store.DirectionAsc}}
	k, err := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Order: &order, UserID: "owner"})
	contractOK(t, err)
	reordered := prospectListRead(t, f, k)
	if len(reordered.Members) < 2 || reordered.Members[0].Candidate.ID == first.Members[0].Candidate.ID {
		t.Fatal("fixture did not reorder members")
	}
	contractEqual(t, reordered.Changes, store.Changes{Since: firstAt, DefinitionChanged: []string{store.DefinitionOrder}, Joined: []store.Joined{}, Left: []store.Left{}})
	second := openList(t, f, k.ID)
	contractEqual(t, second, reordered)
	secondAt := f.now.UTC().Truncate(time.Second)
	f.now = f.now.Add(time.Second)
	contractEqual(t, prospectListRead(t, f, k).Changes, store.Changes{Since: secondAt, DefinitionChanged: []string{}, Joined: []store.Joined{}, Left: []store.Left{}})
}

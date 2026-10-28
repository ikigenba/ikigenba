package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-QOSZ-EMBG R-NDAT-X3NE R-1GL1-1O4A R-3GAA-W7C0 R-PPFW-B9TB R-0NBF-V6BM R-B0HC-R8VK R-B1P9-50M9
func TestProspectListValidators(t *testing.T) {
	contractEqual(t, []string{store.ProspectListIDPrefix, store.OrderOperatingConfidence, store.OrderRevenueLow, store.OrderRevenueConfidence, store.OrderDriveMinutes, store.OrderNewestFiling, store.OrderKnownSince, store.DirectionDesc, store.DirectionAsc, store.KindReject, store.KindUnreject}, []string{"pl_", "operating_confidence", "revenue_low", "revenue_confidence", "drive_minutes", "newest_filing", "known_since", "desc", "asc", "reject", "unreject"})
	contractEqual(t, []int{store.ProspectListSizeMin, store.ProspectListSizeMax, store.ProspectListSizeDefault}, []int{1, 100, 30})
	for _, s := range []string{"pl_0123456789abcdef", "pl_0000000000000000"} {
		contractEqual(t, store.ValidProspectListID(s), true)
	}
	for _, s := range []string{"pl_0123456789ABCDEF", "pl_0123456789abcde", "pl_0123456789abcdef0", "cand_0123456789abcdef", "pl_", ""} {
		contractEqual(t, store.ValidProspectListID(s), false)
	}
	for _, s := range []string{"", " ", "\t\n", "\u2003"} {
		contractEqual(t, store.ValidProspectListName(s), false)
	}
	contractEqual(t, store.ValidProspectListName(" a "), true)
	for _, n := range []int64{-1, 0, 1, 30, 100, 101} {
		contractEqual(t, store.ValidProspectListSize(n), n >= 1 && n <= 100)
	}
	good := []store.ProspectListFilters{{}, {RevenueUnknown: true, MinutesUnknown: true, CategoryUnknown: true, FleetUnknown: true, WebsiteUnknown: true, TrendUnknown: true, FilingUnknown: true, ClassUnknown: true, ShowExcluded: true}, {RevenueMin: ptrInt(5), RevenueMax: ptrInt(5)}, {Categories: []string{sources.CategoryConstruction, sources.CategoryTrucking, sources.CategoryAgriculture, sources.CategoryManufacturing, sources.CategoryWholesale, sources.CategoryMining, sources.CategoryWaste, sources.CategoryOther, sources.CategoryOther}}}
	for _, f := range good {
		contractEqual(t, store.ValidProspectListFilters(f), true)
	}
	for _, category := range []string{
		sources.CategoryConstruction, sources.CategoryTrucking, sources.CategoryAgriculture,
		sources.CategoryManufacturing, sources.CategoryWholesale, sources.CategoryMining,
		sources.CategoryWaste, sources.CategoryOther, sources.CategoryUtilities,
		sources.CategoryRetail, sources.CategoryOtherTransportation, sources.CategoryInformation,
		sources.CategoryFinance, sources.CategoryRealEstate, sources.CategoryProfessional,
		sources.CategoryAdministrative, sources.CategoryEducation, sources.CategoryHealthCare,
		sources.CategoryArts, sources.CategoryFood, sources.CategoryRepair,
		sources.CategoryReligious, sources.CategoryGovernment,
	} {
		contractEqual(t, store.ValidProspectListFilters(store.ProspectListFilters{Categories: []string{category}}), true)
	}
	contractEqual(t, store.ValidProspectListFilters(store.ProspectListFilters{Categories: []string{sources.CategoryGovernment, sources.CategoryEducation}}), true)
	bad := []store.ProspectListFilters{{RevenueMin: ptrInt(6), RevenueMax: ptrInt(5)}, {Categories: []string{""}}, {Categories: []string{"trucking"}}, {Categories: []string{"shops"}}, {Categories: []string{"Retail"}}, {Fleet: sources.FleetUnknown}, {Fleet: "PASS"}, {Class: sources.ClassUnknown}, {Class: "other"}}
	for i := 0; i < 5; i++ {
		f := store.ProspectListFilters{}
		fields := []**int64{&f.RevenueMin, &f.RevenueMax, &f.MaxMinutes, &f.MaxFilingAgeDays, &f.MinCampaignAgeDays}
		*fields[i] = ptrInt(0)
		contractEqual(t, store.ValidProspectListFilters(f), true)
		*fields[i] = ptrInt(-1)
		bad = append(bad, f)
	}
	for _, states := range [][]string{{}, {sources.WebsiteNone, sources.WebsiteNone}, {sources.WebsiteUnknown}, {""}, {"HTTPS"}} {
		bad = append(bad, store.ProspectListFilters{Website: states})
	}
	for _, states := range [][]string{{sources.WebsiteHTTPS}, {sources.WebsiteHTTP}, {sources.WebsiteNone}, {sources.WebsiteHTTPS, sources.WebsiteHTTP}} {
		contractEqual(t, store.ValidProspectListFilters(store.ProspectListFilters{Website: states}), true)
	}
	for _, f := range bad {
		contractEqual(t, store.ValidProspectListFilters(f), false)
	}
	for _, fleet := range []string{sources.FleetPass, sources.FleetFail} {
		contractEqual(t, store.ValidProspectListFilters(store.ProspectListFilters{Fleet: fleet}), true)
	}
	for _, class := range []string{sources.ClassForHire, sources.ClassPrivate, sources.ClassMixed} {
		contractEqual(t, store.ValidProspectListFilters(store.ProspectListFilters{Class: class}), true)
	}
	all := []store.ProspectListOrder{}
	for _, field := range []string{store.OrderOperatingConfidence, store.OrderRevenueLow, store.OrderRevenueConfidence, store.OrderDriveMinutes, store.OrderTrend, store.OrderNewestFiling, store.OrderKnownSince} {
		all = append(all, store.ProspectListOrder{Field: field, Direction: store.DirectionAsc})
	}
	for _, o := range [][]store.ProspectListOrder{nil, {}, all, {{Field: store.OrderRevenueLow, Direction: store.DirectionDesc}}} {
		contractEqual(t, store.ValidProspectListOrder(o), true)
	}
	for _, o := range [][]store.ProspectListOrder{{{Field: "minutes", Direction: store.DirectionAsc}}, {{Direction: store.DirectionAsc}}, {{Field: store.OrderRevenueLow, Direction: "DESC"}}, {{Field: store.OrderRevenueLow}}, {{Field: store.OrderRevenueLow, Direction: store.DirectionAsc}, {Field: store.OrderRevenueLow, Direction: store.DirectionDesc}}} {
		contractEqual(t, store.ValidProspectListOrder(o), false)
	}
}

// R-OW5O-5154 R-199M-R1O4
func TestProspectListJSON(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{{store.ProspectListFilters{}, `{}`}, {store.ProspectListFilters{Government: new(bool)}, `{"government":false}`}, {store.ProspectListFilters{RevenueMin: ptrInt(5), Categories: []string{}}, `{"revenue_min":5}`}, {store.ProspectListFilters{MinCampaignAgeDays: ptrInt(0)}, `{"min_campaign_age_days":0}`}, {store.ProspectListOrder{}, `{"field":"","direction":""}`}} {
		b, e := json.Marshal(tc.v)
		contractOK(t, e)
		contractEqual(t, string(b), tc.want)
	}
	f := store.ProspectListFilters{RevenueMin: ptrInt(1), RevenueMax: ptrInt(2), RevenueUnknown: true, MaxMinutes: ptrInt(3), MinutesUnknown: true, Categories: []string{sources.CategoryOther}, CategoryUnknown: true, Fleet: sources.FleetPass, FleetUnknown: true, Website: []string{sources.WebsiteHTTPS, sources.WebsiteNone}, WebsiteUnknown: true, Government: new(bool), MaxFilingAgeDays: ptrInt(4), FilingUnknown: true, Class: sources.ClassPrivate, ClassUnknown: true, ShowExcluded: true}
	b, e := json.Marshal(f)
	contractOK(t, e)
	var round store.ProspectListFilters
	contractOK(t, json.Unmarshal(b, &round))
	contractEqual(t, round, f)
	contractEqual(t, string(b), `{"revenue_min":1,"revenue_max":2,"revenue_unknown":true,"max_minutes":3,"minutes_unknown":true,"categories":["other"],"category_unknown":true,"fleet":"pass","fleet_unknown":true,"website":["https","none"],"website_unknown":true,"government":false,"max_filing_age_days":4,"filing_unknown":true,"class":"private","class_unknown":true,"show_excluded":true}`)
}

func prospectListCreate(t *testing.T, f *contractFixture, name string) store.ProspectList {
	t.Helper()
	c, e := f.s.CreateProspectList(context.Background(), store.ProspectList{Name: name, Size: store.ProspectListSizeDefault, CreatedUserID: "creator", CreatedUserEmail: "creator@example.test"})
	contractOK(t, e)
	return c
}
func prospectListRead(t *testing.T, f *contractFixture, c store.ProspectList) store.ProspectListRead {
	t.Helper()
	r, e := f.s.ReadProspectList(context.Background(), c.ID)
	contractOK(t, e)
	contractEqual(t, r.ProspectList, c)
	if r.Members == nil || r.Rejects == nil {
		t.Fatal("nil prospectList read list")
	}
	return r
}
func prospectListIDs(rs []store.Ranked) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.Candidate.ID)
	}
	return out
}

// R-VPA4-0O28 R-N5ZF-MH78 R-RD6Z-215C R-ZQRQ-6CQO R-ZRZM-K4HD R-3LZI-WM0I R-Z0S9-I6QA R-ZUFF-BNYR R-U3YU-1G7L R-YM5G-WXTY
func TestProspectListCreateEditPersistence(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	in := store.ProspectList{ID: "ignored", Name: " Name ", Size: store.ProspectListSizeDefault, Filters: store.ProspectListFilters{Categories: []string{sources.CategoryTrucking, sources.CategoryOther, sources.CategoryTrucking}}, CreatedUserID: "creator", CreatedUserEmail: "creator@example.test", ChangedUserID: "ignored", Changed: time.Time{}}
	c, e := f.s.CreateProspectList(ctx, in)
	contractOK(t, e)
	want := in
	want.ID = c.ID
	want.Filters.Categories = []string{sources.CategoryOther, sources.CategoryTrucking}
	want.Order = []store.ProspectListOrder{{Field: store.OrderOperatingConfidence, Direction: store.DirectionDesc}}
	want.Created = f.now.UTC().Truncate(time.Second)
	want.Changed = want.Created
	want.ChangedUserID = in.CreatedUserID
	want.ChangedUserEmail = in.CreatedUserEmail
	contractEqual(t, store.ValidProspectListID(c.ID), true)
	contractEqual(t, c, want)
	other, e := f.s.CreateProspectList(ctx, in)
	contractOK(t, e)
	if c.ID == other.ID {
		t.Fatal("reused id")
	}
	f.now = f.now.Add(time.Hour)
	for _, edit := range []store.ProspectListEdit{{ID: c.ID, UserID: "editor"}, {ID: c.ID, UserID: "editor", Filters: &in.Filters}} {
		got, e := f.s.EditProspectList(ctx, edit)
		contractOK(t, e)
		contractEqual(t, got, c)
	}
	name := "New"
	size := int64(2)
	filters := store.ProspectListFilters{MaxMinutes: ptrInt(5)}
	order := []store.ProspectListOrder{{Field: store.OrderRevenueLow, Direction: store.DirectionAsc}}
	edit := store.ProspectListEdit{ID: c.ID, Name: &name, Size: &size, Filters: &filters, Order: &order, UserID: "editor", UserEmail: "editor@example.test"}
	got, e := f.s.EditProspectList(ctx, edit)
	contractOK(t, e)
	want.Name = name
	want.Size = size
	want.Filters = filters
	want.Order = order
	want.Changed = f.now.UTC().Truncate(time.Second)
	want.ChangedUserID = edit.UserID
	want.ChangedUserEmail = edit.UserEmail
	contractEqual(t, got, want)
	f.now = f.now.Add(time.Hour)
	repeated, e := f.s.EditProspectList(ctx, edit)
	contractOK(t, e)
	contractEqual(t, repeated, got)
	f.reopen()
	persisted, e := f.s.GetProspectList(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, persisted, got)
	emptyOrder := []store.ProspectListOrder{}
	cleared, e := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &store.ProspectListFilters{}, Order: &emptyOrder, UserID: "clear"})
	contractOK(t, e)
	contractEqual(t, cleared.Filters, store.ProspectListFilters{})
	contractEqual(t, cleared.Order, c.Order)
	unchanged, e := f.s.GetProspectList(ctx, other.ID)
	contractOK(t, e)
	contractEqual(t, unchanged, other)
}

// R-M2YN-DTH0
func TestProspectListInvalidArgumentsAndMissingRecords(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	original := prospectListCreate(t, f, "Original")
	before := f.rnd.(*contractRandom).calls
	for _, in := range []store.ProspectList{{}, {Name: "x", Size: 1}, {Name: " ", Size: 1, CreatedUserID: "owner"}, {Name: "x", CreatedUserID: "owner"}, {Name: "x", Size: 1, Filters: store.ProspectListFilters{Fleet: sources.FleetUnknown}, CreatedUserID: "owner"}, {Name: "x", Size: 1, Order: []store.ProspectListOrder{{Field: "bad"}}, CreatedUserID: "owner"}} {
		got, e := f.s.CreateProspectList(ctx, in)
		want := store.ErrInvalid
		if in.CreatedUserID == "" {
			want = store.ErrUnreachable
		}
		contractError(t, e, want)
		contractEqual(t, got, store.ProspectList{})
	}
	blank := " "
	zero := int64(0)
	badFilters := store.ProspectListFilters{MaxMinutes: ptrInt(-1)}
	badOrder := []store.ProspectListOrder{{Field: "bad"}}
	for _, edit := range []store.ProspectListEdit{{ID: "missing"}, {ID: "missing", Name: &blank, UserID: "owner"}, {ID: "missing", Size: &zero, UserID: "owner"}, {ID: "missing", Filters: &badFilters, UserID: "owner"}, {ID: "missing", Order: &badOrder, UserID: "owner"}, {ID: "missing", UserID: "owner"}} {
		got, e := f.s.EditProspectList(ctx, edit)
		want := store.ErrInvalid
		if edit.UserID == "" {
			want = store.ErrUnreachable
		} else if edit.Name == nil && edit.Size == nil && edit.Filters == nil && edit.Order == nil {
			want = store.ErrNotFound
		}
		contractError(t, e, want)
		contractEqual(t, got, store.ProspectList{})
	}
	for _, id := range []string{"missing", "pl_ffffffffffffffff"} {
		got, e := f.s.GetProspectList(ctx, id)
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, got, store.ProspectList{})
		got, e = f.s.DeleteProspectList(ctx, id)
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, got, store.ProspectList{})
		read, e := f.s.ReadProspectList(ctx, id)
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, read, store.ProspectListRead{})
	}
	contractEqual(t, f.rnd.(*contractRandom).calls, before)
	got, e := f.s.GetProspectList(ctx, original.ID)
	contractOK(t, e)
	contractEqual(t, got, original)
}

// R-SINK-28CY R-MCPU-FZEK R-0UMU-5SRS R-NT5I-W4AF
func TestProspectListRejectHistoryAndDeletion(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	c := prospectListCreate(t, f, "First")
	other := prospectListCreate(t, f, "Other")
	ids := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now), contractObservation("b", "Beta", f.now))
	id := ids[0]
	empty, e := f.s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, empty, []store.Reject{})
	request := store.Reject{Kind: store.KindUnreject, ProspectList: c.ID, ProspectListName: "ignored", Candidate: id, Why: "because", UserID: "owner", UserEmail: "owner@example.test", At: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)}
	noop, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	contractEqual(t, noop, store.Reject{Kind: store.KindUnreject, ProspectList: c.ID, ProspectListName: c.Name, Candidate: id})
	request.Kind = store.KindReject
	first, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	want := request
	want.ProspectListName = c.Name
	want.At = f.now.UTC().Truncate(time.Second)
	contractEqual(t, first, want)
	f.now = f.now.Add(time.Hour)
	request.Why = "different"
	again, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	contractEqual(t, again, first)
	request.Kind = store.KindUnreject
	second, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	contractEqual(t, second.At, f.now.UTC().Truncate(time.Second))
	contractEqual(t, len(prospectListRead(t, f, c).Rejects), 0)
	request.Kind = store.KindReject
	third, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	otherReq := request
	otherReq.ProspectList = other.ID
	fourth, e := f.s.Reject(ctx, otherReq)
	contractOK(t, e)
	history, e := f.s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, history, []store.Reject{fourth, third, second, first})
	contractEqual(t, prospectListRead(t, f, c).Rejects, []store.Reject{third})
	contractEqual(t, prospectListRead(t, f, other).Rejects, []store.Reject{fourth})
	request.Candidate = ids[1]
	fifth, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	contractEqual(t, prospectListRead(t, f, c).Rejects, []store.Reject{fifth, third})
	renamed := "Renamed"
	c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Name: &renamed, UserID: "editor"})
	contractOK(t, e)
	third.ProspectListName = renamed
	fifth.ProspectListName = renamed
	contractEqual(t, prospectListRead(t, f, c).Rejects, []store.Reject{fifth, third})
	f.reopen()
	history, e = f.s.Rejects(ctx, id)
	contractOK(t, e)
	for i := 1; i < len(history); i++ {
		contractEqual(t, history[i].ProspectListName, renamed)
	}
	deleted, e := f.s.DeleteProspectList(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, deleted, c)
	f.reopen()
	_, e = f.s.GetProspectList(ctx, c.ID)
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.ReadProspectList(ctx, c.ID)
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.DeleteProspectList(ctx, c.ID)
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, UserID: "owner"})
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.Reject(ctx, request)
	contractError(t, e, store.ErrNotFound)
	history, e = f.s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(history), 4)
	contractEqual(t, history[0], fourth)
	for _, r := range history[1:] {
		contractEqual(t, r.ProspectListName, "")
	}
	for _, reversed := range []bool{false, true} {
		var cs []store.ProspectList
		if reversed {
			cs, e = f.s.ListProspectListsReversed(ctx, "", "", 100)
		} else {
			cs, e = f.s.ListProspectLists(ctx, "", "", 100)
		}
		contractOK(t, e)
		contractEqual(t, cs, []store.ProspectList{other})
	}
	missing, e := f.s.Rejects(ctx, "missing")
	contractError(t, e, store.ErrNotFound)
	contractEqual(t, missing, []store.Reject(nil))
}

// R-P11W-NUZF
func TestProspectListRejectRefusals(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	c := prospectListCreate(t, f, "ProspectList")
	ids := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now), contractObservation("b", "Beta", f.now))
	_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same", UserID: "owner"})
	contractOK(t, e)
	merged := ""
	for _, id := range ids {
		candidate, e := f.s.GetCandidate(ctx, id)
		contractOK(t, e)
		if candidate.Status == store.StatusMerged {
			merged = id
		}
	}
	base := store.Reject{Kind: store.KindReject, ProspectList: c.ID, Candidate: ids[0], Why: "reason", UserID: "owner"}
	for _, tc := range []struct {
		modify func(*store.Reject)
		want   error
	}{{func(r *store.Reject) { r.UserID = ""; r.Why = "" }, store.ErrUnreachable}, {func(r *store.Reject) { r.Kind = "bad"; r.Why = "" }, store.ErrUnreachable}, {func(r *store.Reject) { r.Why = "\t\n"; r.ProspectList = "missing" }, store.ErrInvalid}, {func(r *store.Reject) { r.ProspectList = "bad"; r.Candidate = "missing" }, store.ErrNotFound}, {func(r *store.Reject) { r.Candidate = "missing" }, store.ErrNotFound}, {func(r *store.Reject) { r.Candidate = merged }, store.ErrInvalid}, {func(r *store.Reject) { r.Candidate = merged; r.Kind = store.KindUnreject }, store.ErrInvalid}} {
		r := base
		tc.modify(&r)
		got, e := f.s.Reject(ctx, r)
		contractError(t, e, tc.want)
		contractEqual(t, got, store.Reject{})
	}
	for _, id := range ids {
		rs, e := f.s.Rejects(ctx, id)
		contractOK(t, e)
		contractEqual(t, rs, []store.Reject{})
	}
}

// R-MAA1-OFX6
func TestProspectListIDRandomnessAndRetiredIDs(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	meter := &contractMeter{reader: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8))}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: meter})
	c := prospectListCreate(t, f, "First")
	contractEqual(t, c.ID, "pl_5a5a5a5a5a5a5a5a")
	contractEqual(t, meter.requests, []int{8})
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	id := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now))[0]
	_, e := f.s.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: c.ID, Candidate: id, Why: "reason", UserID: "owner"})
	contractOK(t, e)
	_, e = f.s.DeleteProspectList(ctx, c.ID)
	contractOK(t, e)
	meter = &contractMeter{reader: bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 8), bytes.Repeat([]byte{0x6b}, 8)...))}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: meter})
	next := prospectListCreate(t, f, "Next")
	contractEqual(t, next.ID, "pl_6b6b6b6b6b6b6b6b")
	contractEqual(t, meter.requests, []int{8, 8})
	for _, data := range [][]byte{bytes.Repeat([]byte{0x5a}, 64), {}} {
		meter = &contractMeter{reader: bytes.NewReader(data)}
		f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: meter})
		got, e := f.s.CreateProspectList(ctx, store.ProspectList{Name: "Fail", Size: 1, CreatedUserID: "owner"})
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, got, store.ProspectList{})
		contractEqual(t, meter.total, len(data))
		if len(data) > 0 {
			contractEqual(t, len(meter.requests), 8)
		}
		cs, e := f.s.ListProspectLists(ctx, "", "", 100)
		contractOK(t, e)
		contractEqual(t, cs, []store.ProspectList{next})
	}
}

// R-3TXZ-TM0W R-3BNI-31WH R-S40R-GZGM R-TRRU-7QSN R-NVLB-NNRT
func TestProspectListWindowsAndPlans(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	all := []store.ProspectList{}
	for _, name := range []string{"same", "same", "same", "z", "Z", "é"} {
		all = append(all, prospectListCreate(t, f, name))
	}
	slices.SortFunc(all, func(a, b store.ProspectList) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, reverse := range []bool{false, true} {
		want := slices.Clone(all)
		if reverse {
			slices.Reverse(want)
		}
		got := []store.ProspectList{}
		name, id := "ignored", ""
		for {
			var window []store.ProspectList
			var e error
			if reverse {
				window, e = f.s.ListProspectListsReversed(ctx, name, id, 2)
			} else {
				window, e = f.s.ListProspectLists(ctx, name, id, 2)
			}
			contractOK(t, e)
			if window == nil {
				t.Fatal("nil window")
			}
			if len(window) == 0 {
				break
			}
			got = append(got, window...)
			last := window[len(window)-1]
			name, id = last.Name, last.ID
		}
		contractEqual(t, got, want)
	}
	for _, pos := range [][2]string{{"same", "pl_0202020202020203"}, {"t", "any"}, {"", "any"}, {"zz", "any"}} {
		for _, reverse := range []bool{false, true} {
			want := []store.ProspectList{}
			for _, c := range all {
				cmp := strings.Compare(c.Name, pos[0])
				if cmp == 0 {
					cmp = strings.Compare(c.ID, pos[1])
				}
				if (!reverse && cmp > 0) || (reverse && cmp < 0) {
					want = append(want, c)
				}
			}
			var got []store.ProspectList
			var e error
			if reverse {
				slices.Reverse(want)
				got, e = f.s.ListProspectListsReversed(ctx, pos[0], pos[1], 100)
			} else {
				got, e = f.s.ListProspectLists(ctx, pos[0], pos[1], 100)
			}
			contractOK(t, e)
			contractEqual(t, got, want)
		}
	}
	for _, tc := range []struct {
		q              windowStatement
		args           []any
		prefix, suffix string
	}{{store.ProspectListsFirst, []any{2}, "SCAN ", " INDEX prospect_lists_window"}, {store.ProspectListsAfter, []any{"same", "id", 2}, "SEARCH ", " INDEX prospect_lists_window ((name,id)>(?,?))"}, {store.ProspectListsLast, []any{2}, "SCAN ", " INDEX prospect_lists_window"}, {store.ProspectListsPreceding, []any{"same", "id", 2}, "SEARCH ", " INDEX prospect_lists_window ((name,id)<(?,?))"}} {
		plan := statementPlan(t, f, tc.q, tc.args...)
		if len(plan) == 0 || !strings.HasPrefix(plan[0], tc.prefix) || !strings.HasSuffix(plan[0], tc.suffix) {
			t.Fatalf("plan %v", plan)
		}
		for i, p := range plan {
			if strings.Contains(p, "USE TEMP B-TREE") || (strings.HasPrefix(p, "SCAN ") && (i > 0 || tc.prefix != "SCAN ")) {
				t.Fatalf("unbounded plan %v", plan)
			}
		}
	}
}

// R-KTT7-SHJQ
func TestConcurrentProspectListWrites(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	c := prospectListCreate(t, f, "Concurrent")
	id := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now))[0]
	var wg sync.WaitGroup
	answers := make(chan store.Reject, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			r, e := f.s.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: c.ID, Candidate: id, Why: "reason", UserID: "owner"})
			answers <- r
			errs <- e
		})
	}
	wg.Wait()
	close(answers)
	close(errs)
	for e := range errs {
		contractOK(t, e)
	}
	history, e := f.s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(history), 1)
	for r := range answers {
		contractEqual(t, r, history[0])
	}
	deletes := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, e := f.s.DeleteProspectList(ctx, c.ID); deletes <- e })
	}
	wg.Wait()
	close(deletes)
	success, missing := 0, 0
	for e := range deletes {
		if e == nil {
			success++
		} else {
			contractError(t, e, store.ErrNotFound)
			missing++
		}
	}
	contractEqual(t, success, 1)
	contractEqual(t, missing, 1)
}

func prospectListPopulation(t *testing.T, f *contractFixture) []string {
	t.Helper()
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55070": 120, "55071": 121, "55072": 0}))
	ids := []string{}
	for i := range 5 {
		o := contractObservation(fmt.Sprint(i), fmt.Sprintf("ProspectList business %d", i), time.Time{})
		o.Scraper = "fmcsa"
		o.Reading.FleetVerdict = sources.FleetUnknown
		o.Reading.Estimate = nil
		o.Reading.Class = ""
		o.Reading.Zip = "99999"
		o.Reading.Cargo = nil
		switch i {
		case 0:
			o.Reading.Zip = "55070"
			o.Reading.Class = sources.ClassForHire
			o.Reading.Filed = time.Date(2024, 10, 15, 0, 0, 0, 0, time.UTC)
			o.Reading.Category = sources.CategoryTrucking
			o.Reading.FleetVerdict = sources.FleetPass
			o.Reading.GovernmentMatch = "government"
			o.Reading.KnownSince = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		case 1:
			o.Reading.Zip = "55071"
			o.Reading.Class = sources.ClassPrivate
			o.Reading.Filed = time.Date(2024, 10, 14, 0, 0, 0, 0, time.UTC)
			o.Reading.Category = sources.CategoryOther
			o.Reading.FleetVerdict = sources.FleetFail
			o.Reading.KnownSince = time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
		case 2:
			o.Reading.Zip = "55072"
			o.Reading.Filed = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
			o.Reading.Category = sources.CategoryConstruction
			o.Reading.GovernmentMatch = "government"
			o.Reading.KnownSince = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		case 3:
			o.Reading.Class = sources.ClassMixed
			o.Reading.Filed = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
		case 4:
			o.Reading.Class = sources.ClassUnknown
		}
		if i < 4 {
			low := []int64{5000000, 10000000, 4999999, 10000001}[i]
			o.Reading.Estimate = &sources.Estimate{Source: o.Scraper, Low: low, Confidence: []float64{.8, .2, .5, .2}[i]}
			o.Reading.Evidence = []sources.Evidence{{Name: sources.EvidenceOwnDomain, Source: o.Scraper, Weight: []float64{.8, .2, .8, .1}[i]}}
		}
		ids = append(ids, contractAttach(t, f, "run", o)[0])
	}
	deriveAll(t, f.s)
	_, _, e := f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, e)
	return ids
}

// R-LW6E-VA6N R-37ZS-XQOE R-02L5-D2PT R-ZLIK-0AC3 R-QTOK-XPA8 R-UR4X-B3AS R-PAT3-Q0WZ R-42HA-I07R R-LYM7-MTO1 R-4KYZ-HR3B
func TestProspectListFilterBoundariesAndUnknownSwitches(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := prospectListPopulation(t, f)
	c := prospectListCreate(t, f, "Filters")
	yes, no := true, false
	for _, tc := range []struct {
		name    string
		filter  store.ProspectListFilters
		indices []int
	}{
		{"none", store.ProspectListFilters{}, []int{0, 1, 2, 3, 4}},
		{"other switches alone", store.ProspectListFilters{RevenueUnknown: true, MinutesUnknown: true, FleetUnknown: true, WebsiteUnknown: true, TrendUnknown: true, FilingUnknown: true, ClassUnknown: true}, []int{0, 1, 2, 3, 4}},
		{"category unknown alone", store.ProspectListFilters{CategoryUnknown: true}, []int{3, 4}},
		{"all switches alone", store.ProspectListFilters{RevenueUnknown: true, MinutesUnknown: true, CategoryUnknown: true, FleetUnknown: true, WebsiteUnknown: true, TrendUnknown: true, FilingUnknown: true, ClassUnknown: true}, []int{3, 4}},
		{"revenue bounds", store.ProspectListFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000)}, []int{}},
		{"revenue unknown", store.ProspectListFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000), RevenueUnknown: true}, []int{0, 1, 3, 4}},
		{"revenue only max", store.ProspectListFilters{RevenueMax: ptrInt(10000000)}, []int{}},
		{"drive", store.ProspectListFilters{MaxMinutes: ptrInt(120)}, []int{0, 2}},
		{"drive unknown", store.ProspectListFilters{MaxMinutes: ptrInt(120), MinutesUnknown: true}, []int{0, 2, 3, 4}},
		{"category", store.ProspectListFilters{Categories: []string{sources.CategoryTrucking}}, []int{0}},
		{"category unknown", store.ProspectListFilters{Categories: []string{sources.CategoryTrucking}, CategoryUnknown: true}, []int{0, 3, 4}},
		{"categories or", store.ProspectListFilters{Categories: []string{sources.CategoryTrucking, sources.CategoryConstruction}}, []int{0, 2}},
		{"fleet pass", store.ProspectListFilters{Fleet: sources.FleetPass}, []int{0}},
		{"fleet unknown", store.ProspectListFilters{Fleet: sources.FleetPass, FleetUnknown: true}, []int{0, 2, 3, 4}},
		{"fleet fail", store.ProspectListFilters{Fleet: sources.FleetFail}, []int{1}},
		{"government", store.ProspectListFilters{Government: &yes}, []int{0, 2}},
		{"nongovernment", store.ProspectListFilters{Government: &no}, []int{1, 3, 4}},
		{"filing", store.ProspectListFilters{MaxFilingAgeDays: ptrInt(730)}, []int{0, 2, 3}},
		{"filing unknown", store.ProspectListFilters{MaxFilingAgeDays: ptrInt(730), FilingUnknown: true}, []int{0, 2, 3, 4}},
		{"filing today", store.ProspectListFilters{MaxFilingAgeDays: ptrInt(0)}, []int{2, 3}},
		{"filing maximum", store.ProspectListFilters{MaxFilingAgeDays: ptrInt(math.MaxInt64)}, []int{0, 1, 2, 3}},
		{"class", store.ProspectListFilters{Class: sources.ClassForHire}, []int{0}},
		{"class unknown", store.ProspectListFilters{Class: sources.ClassForHire, ClassUnknown: true}, []int{0, 2, 4}},
		{"combined", store.ProspectListFilters{MaxMinutes: ptrInt(120), RevenueMin: ptrInt(5000000), Fleet: sources.FleetPass, Government: &yes, MaxFilingAgeDays: ptrInt(730), Class: sources.ClassForHire, Categories: []string{sources.CategoryTrucking}}, []int{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e error
			c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &tc.filter, UserID: "editor"})
			contractOK(t, e)
			r := prospectListRead(t, f, c)
			got := prospectListIDs(r.Members)
			want := []string{}
			for _, i := range tc.indices {
				want = append(want, ids[i])
			}
			slices.Sort(got)
			slices.Sort(want)
			contractEqual(t, got, want)
			contractEqual(t, r.Matching, int64(len(want)))
		})
	}
	// Exclusion still applies with no other filter, including unknown-only categories.
	_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[3], Why: "exclude unknown", UserID: "owner"})
	contractOK(t, e)
	for _, tc := range []struct {
		filters store.ProspectListFilters
		count   int64
	}{
		{store.ProspectListFilters{}, 4},
		{store.ProspectListFilters{RevenueUnknown: true, MinutesUnknown: true, FleetUnknown: true, WebsiteUnknown: true, TrendUnknown: true, FilingUnknown: true, ClassUnknown: true}, 4},
		{store.ProspectListFilters{CategoryUnknown: true}, 1},
		{store.ProspectListFilters{CategoryUnknown: true, ShowExcluded: true}, 2},
	} {
		c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &tc.filters, UserID: "owner"})
		contractOK(t, e)
		contractEqual(t, prospectListRead(t, f, c).Matching, tc.count)
	}
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: ids[3], Why: "restore unknown", UserID: "owner"})
	contractOK(t, e)
	filter := store.ProspectListFilters{MaxFilingAgeDays: ptrInt(730)}
	c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &filter, UserID: "editor"})
	contractOK(t, e)
	f.now = time.Date(2026, 10, 15, 23, 59, 59, 0, time.FixedZone("read", -5*60*60))
	r := prospectListRead(t, f, c)
	got := prospectListIDs(r.Members)
	slices.Sort(got)
	want := []string{ids[2], ids[3]}
	slices.Sort(want)
	contractEqual(t, got, want)
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[2], Why: "exclude", UserID: "owner"})
	contractOK(t, e)
	r = prospectListRead(t, f, c)
	contractEqual(t, prospectListIDs(r.Members), []string{ids[3]})
	filter.ShowExcluded = true
	c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &filter, UserID: "editor"})
	contractOK(t, e)
	r = prospectListRead(t, f, c)
	contractEqual(t, r.Matching, int64(2))
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: ids[2], Why: "include", UserID: "owner"})
	contractOK(t, e)
}

// R-B5CY-ABUC R-YM5G-WXTY
func TestProspectListOrderFieldsDirectionsUnknownsAndTies(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := prospectListPopulation(t, f)
	c := prospectListCreate(t, f, "Order")
	for _, tc := range []struct {
		field     string
		asc, desc []int
	}{
		{store.OrderOperatingConfidence, []int{3, 1, 0, 2, 4}, []int{0, 2, 1, 3, 4}},
		{store.OrderRevenueLow, []int{2, 0, 1, 3, 4}, []int{3, 1, 0, 2, 4}},
		{store.OrderRevenueConfidence, []int{1, 3, 2, 0, 4}, []int{0, 2, 1, 3, 4}},
		{store.OrderDriveMinutes, []int{2, 0, 1, 3, 4}, []int{1, 0, 2, 3, 4}},
		{store.OrderNewestFiling, []int{1, 0, 3, 2, 4}, []int{2, 3, 0, 1, 4}},
		{store.OrderKnownSince, []int{0, 1, 2, 3, 4}, []int{2, 1, 0, 3, 4}},
	} {
		for _, direction := range []string{store.DirectionAsc, store.DirectionDesc} {
			t.Run(tc.field+direction, func(t *testing.T) {
				order := []store.ProspectListOrder{{Field: tc.field, Direction: direction}}
				var e error
				c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Order: &order, UserID: "owner"})
				contractOK(t, e)
				indices := tc.asc
				if direction == store.DirectionDesc {
					indices = tc.desc
				}
				want := []string{}
				for _, i := range indices {
					want = append(want, ids[i])
				}
				contractEqual(t, prospectListIDs(prospectListRead(t, f, c).Members), want)
			})
		}
	}
	order := []store.ProspectListOrder{{Field: store.OrderRevenueConfidence, Direction: store.DirectionAsc}, {Field: store.OrderOperatingConfidence, Direction: store.DirectionDesc}}
	c, e := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Order: &order, UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, prospectListIDs(prospectListRead(t, f, c).Members), []string{ids[1], ids[3], ids[2], ids[0], ids[4]})
}

// R-KL9X-43CV R-OQ2T-7XB6 R-UP2C-5BPC R-NT5I-W4AF
func TestProspectListMembershipReadsStoredJudgmentsAndStandingRejects(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := prospectListPopulation(t, f)
	unjudged := contractAttach(t, f, "run", contractObservation("unjudged", "Unjudged business", f.now))[0]
	c := prospectListCreate(t, f, "Membership")
	size := int64(2)
	c, e := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Size: &size, UserID: "owner"})
	contractOK(t, e)
	r := prospectListRead(t, f, c)
	contractEqual(t, r.Matching, int64(5))
	contractEqual(t, prospectListIDs(r.Members), []string{ids[0], ids[2]})
	for _, member := range r.Members {
		candidate, e := f.s.GetCandidate(ctx, member.Candidate.ID)
		contractOK(t, e)
		judgments, e := f.s.GetJudgments(ctx, candidate.ID)
		contractOK(t, e)
		contractEqual(t, member, store.Ranked{Candidate: candidate, Judgments: judgments, Facts: factsAt(ctx, t, f.s, candidate.ID), Basis: store.BasisRecords})
	}
	_ = unjudged
	request := store.Reject{Kind: store.KindReject, ProspectList: c.ID, Candidate: ids[0], Why: "owner rejected", UserID: "owner"}
	first, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	r = prospectListRead(t, f, c)
	contractEqual(t, r.Matching, int64(4))
	contractEqual(t, prospectListIDs(r.Members), []string{ids[2], ids[1]})
	contractEqual(t, r.Rejects, []store.Reject{first})
	request.Kind = store.KindUnreject
	_, e = f.s.Reject(ctx, request)
	contractOK(t, e)
	r = prospectListRead(t, f, c)
	contractEqual(t, r.Matching, int64(5))
	contractEqual(t, prospectListIDs(r.Members), []string{ids[0], ids[2]})
	filters := store.ProspectListFilters{RevenueMin: ptrInt(10000000)}
	c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	request.Kind = store.KindReject
	_, e = f.s.Reject(ctx, request)
	contractOK(t, e)
	filters = store.ProspectListFilters{}
	c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, prospectListRead(t, f, c).Matching, int64(4))
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[1], Other: ids[0], Why: "same", UserID: "owner"})
	contractOK(t, e)
	// The merged candidate's reject stays visible and its history stays available.
	merged, e := f.s.GetCandidate(ctx, ids[0])
	contractOK(t, e)
	if merged.Status != store.StatusMerged {
		t.Fatal("fixture candidate did not merge")
	}
	r = prospectListRead(t, f, c)
	contractEqual(t, len(r.Rejects), 1)
	contractEqual(t, r.Rejects[0].Candidate, ids[0])
	rs, e := f.s.Rejects(ctx, ids[0])
	contractOK(t, e)
	contractEqual(t, len(rs), 3)
	f.reopen()
	persisted := prospectListRead(t, f, c)
	contractEqual(t, persisted, r)
	// Replacing drive times does not rewrite the judgment used for filtering.
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{}))
	r = prospectListRead(t, f, c)
	contractEqual(t, r, persisted)
	empty := prospectListCreate(t, f, "Empty")
	filters = store.ProspectListFilters{RevenueMin: ptrInt(math.MaxInt64)}
	empty, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: empty.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	read := prospectListRead(t, f, empty)
	contractEqual(t, read.Members, []store.Ranked{})
	contractEqual(t, read.Rejects, []store.Reject{})
	contractEqual(t, read.Matching, int64(0))
}

// R-OQ2T-7XB6
func TestProspectListReadUsesOneClockSnapshot(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	prospectListPopulation(t, f)
	c := prospectListCreate(t, f, "Clock")
	filter := store.ProspectListFilters{MaxFilingAgeDays: ptrInt(730)}
	c, e := f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &filter, UserID: "owner"})
	contractOK(t, e)
	calls := 0
	f.s = store.New(f.d, store.Config{Rand: f.rnd, Now: func() time.Time { calls++; return f.now.Add(time.Duration(calls-1) * 24 * time.Hour) }})
	r := prospectListRead(t, f, c)
	contractEqual(t, calls, 1)
	contractEqual(t, r.Matching, int64(3))
}

// R-VPA4-0O28 R-N5ZF-MH78 R-SINK-28CY R-KL9X-43CV R-OW5O-5154 R-199M-R1O4
func TestProspectListRecordShapesByUse(t *testing.T) {
	type prospectList struct {
		ID, Name                        string
		Size                            int64
		Filters                         store.ProspectListFilters
		Order                           []store.ProspectListOrder
		CreatedUserID, CreatedUserEmail string
		Created                         time.Time
		ChangedUserID, ChangedUserEmail string
		Changed                         time.Time
	}
	type edit struct {
		ID                string
		Name              *string
		Size              *int64
		Filters           *store.ProspectListFilters
		Order             *[]store.ProspectListOrder
		UserID, UserEmail string
	}
	type reject struct {
		Kind, ProspectList, ProspectListName, Candidate, Why, UserID, UserEmail string
		At                                                                      time.Time
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
	type order struct{ Field, Direction string }
	type filters struct {
		RevenueMin, RevenueMax *int64
		RevenueUnknown         bool
		MaxMinutes             *int64
		MinutesUnknown         bool
		Categories             []string
		CategoryUnknown        bool
		Fleet                  string
		FleetUnknown           bool
		Website                []string
		WebsiteUnknown         bool
		Trend                  []string
		TrendUnknown           bool
		Government             *bool
		MaxFilingAgeDays       *int64
		FilingUnknown          bool
		MinCampaignAgeDays     *int64
		Class                  string
		ClassUnknown           bool
		Basis                  []string
		ShowExcluded           bool
	}
	contractEqual(t, filters(store.ProspectListFilters{}), filters{})
	contractEqual(t, prospectList(store.ProspectList{}), prospectList{})
	contractEqual(t, edit(store.ProspectListEdit{}), edit{})
	contractEqual(t, reject(store.Reject{}), reject{})
	contractEqual(t, read(store.ProspectListRead{}), read{})
	contractEqual(t, order(store.ProspectListOrder{}), order{})
}

// R-VANB-FF5W R-XWJK-VR9D R-2CAE-ZPEC R-VKEI-HL3G
func TestProspectListExportedWindowStatements(t *testing.T) {
	const first windowStatement = store.ProspectListsFirst
	const after windowStatement = store.ProspectListsAfter
	const last windowStatement = store.ProspectListsLast
	const preceding windowStatement = store.ProspectListsPreceding
	f := contractDB(t)
	ctx := context.Background()
	for _, name := range []string{"same", "same", "A", "z"} {
		prospectListCreate(t, f, name)
	}
	full, e := f.s.ListProspectLists(ctx, "", "", 100)
	contractOK(t, e)
	for _, tc := range []struct {
		q        windowStatement
		args     []any
		name, id string
		reverse  bool
	}{{first, []any{2}, "", "", false}, {last, []any{2}, "", "", true}, {after, []any{full[1].Name, full[1].ID, 2}, full[1].Name, full[1].ID, false}, {preceding, []any{full[2].Name, full[2].ID, 2}, full[2].Name, full[2].ID, true}, {after, []any{"t", "arbitrary", 2}, "t", "arbitrary", false}, {preceding, []any{"t", "arbitrary", 2}, "t", "arbitrary", true}} {
		var cs []store.ProspectList
		if tc.reverse {
			cs, e = f.s.ListProspectListsReversed(ctx, tc.name, tc.id, 2)
		} else {
			cs, e = f.s.ListProspectLists(ctx, tc.name, tc.id, 2)
		}
		contractOK(t, e)
		want := []string{}
		for _, c := range cs {
			want = append(want, c.ID)
		}
		contractEqual(t, statementIDs(t, f, tc.q, tc.args...), want)
		contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
			rows, e := tx.QueryContext(ctx, string(tc.q), tc.args[:len(tc.args)-1]...)
			if e == nil {
				_ = rows.Close()
				t.Fatal("statement accepted missing parameter")
			}
			return nil
		}))
	}
}

// R-B5CY-ABUC
func TestProspectListOperatingOrderUsesRoundedKey(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := []string{}
	for i, weight := range []float64{.8000001, .8000002} {
		o := contractObservation(fmt.Sprint(i), fmt.Sprintf("Rounded %d", i), f.now)
		o.Reading.Estimate = nil
		o.Reading.Evidence = []sources.Evidence{{Name: sources.EvidenceOwnDomain, Source: o.Scraper, Weight: weight}}
		ids = append(ids, contractAttach(t, f, "run", o)[0])
	}
	deriveAll(t, f.s)
	_, _, e := f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, e)
	a, e := f.s.GetJudgments(ctx, ids[0])
	contractOK(t, e)
	b, e := f.s.GetJudgments(ctx, ids[1])
	contractOK(t, e)
	contractEqual(t, a.OperatingKey, b.OperatingKey)
	contractEqual(t, *a.Operating, *b.Operating)
	c := prospectListCreate(t, f, "Rounded")
	contractEqual(t, prospectListIDs(prospectListRead(t, f, c).Members), ids)
}

// R-02L5-D2PT
func TestProspectListCategoryMatchesAnyJudgmentsCategory(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	o := contractObservation("carrier", "Two source categories", f.now)
	o.Reading.Category = sources.CategoryTrucking
	id := contractAttach(t, f, "run", o)[0]
	o.Scraper = "ppp"
	o.SourceID = "loan"
	o.Reading.Category = sources.CategoryConstruction
	contractAttach(t, f, "run", o)
	deriveAll(t, f.s)
	_, _, e := f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, e)
	_, e = f.s.GetJudgments(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(factsAt(ctx, t, f.s, id).Categories), 2)
	c := prospectListCreate(t, f, "Categories")
	for _, unknown := range []bool{false, true} {
		filters := store.ProspectListFilters{Categories: []string{sources.CategoryTrucking}, CategoryUnknown: unknown}
		c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
		contractOK(t, e)
		contractEqual(t, prospectListIDs(prospectListRead(t, f, c).Members), []string{id})
	}
}

// R-V3BX-4SPQ R-LYM7-MTO1 R-UP2C-5BPC
func TestProspectListWebsiteFiltersReadStoredJudgment(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := []string{}
	for i, state := range []string{sources.WebsiteHTTPS, sources.WebsiteHTTP, sources.WebsiteNone, sources.WebsiteUnknown} {
		o := contractObservation(fmt.Sprint(i), fmt.Sprintf("Website prospectList %d", i), f.now)
		o.Reading.Email = fmt.Sprintf("owner@business%d.example", i)
		id := contractAttach(t, f, "run", o)[0]
		ids = append(ids, id)
		if state == sources.WebsiteNone {
			_, e := f.s.AddEnrichments(ctx, []store.Enrichment{searchResult(t, id, "", "")})
			contractOK(t, e)
		} else if state != sources.WebsiteUnknown {
			_, e := f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: fmt.Sprintf("business%d.example", i), Inputs: fmt.Sprintf("business%d.example", i), Found: websiteFound(website.Result{State: state})}})
			contractOK(t, e)
		}
	}
	deriveAll(t, f.s)
	_, _, e := f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, e)
	c := prospectListCreate(t, f, "Websites")
	for _, tc := range []struct {
		states  []string
		indices []int
	}{
		{[]string{sources.WebsiteHTTPS}, []int{0}},
		{[]string{sources.WebsiteHTTP}, []int{1}},
		{[]string{sources.WebsiteHTTPS, sources.WebsiteHTTP}, []int{0, 1}},
		{[]string{sources.WebsiteHTTP, sources.WebsiteHTTPS}, []int{0, 1}},
		{[]string{sources.WebsiteNone}, []int{2}},
	} {
		for _, unknown := range []bool{false, true} {
			filters := store.ProspectListFilters{Website: tc.states, WebsiteUnknown: unknown}
			c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
			contractOK(t, e)
			// The requested order is kept as supplied across serialization and reopening.
			f.reopen()
			got := prospectListRead(t, f, c)
			want := []string{}
			for _, i := range tc.indices {
				want = append(want, ids[i])
			}
			if unknown {
				want = append(want, ids[3])
			}
			actual := prospectListIDs(got.Members)
			slices.Sort(actual)
			slices.Sort(want)
			contractEqual(t, actual, want)
			contractEqual(t, got.Matching, int64(len(want)))
		}
	}
	combined := store.ProspectListFilters{Website: []string{sources.WebsiteHTTPS}, RevenueMin: ptrInt(math.MaxInt64)}
	c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &combined, UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, prospectListRead(t, f, c).Matching, int64(0))
	// A result updates prospectList facts before a judge.
	filters := store.ProspectListFilters{Website: []string{sources.WebsiteHTTPS}}
	c, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	_, e = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids[2], Kind: sources.WebsiteCheck, Source: "business2.example", Inputs: "business2.example", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS})}})
	contractOK(t, e)
	contractEqual(t, prospectListRead(t, f, c).Matching, int64(2))
	_, e = f.s.Judge(ctx, []string{ids[2]})
	contractOK(t, e)
	contractEqual(t, prospectListRead(t, f, c).Matching, int64(2))
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[0], Why: "owner excluded", UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, prospectListIDs(prospectListRead(t, f, c).Members), []string{ids[2]})
}

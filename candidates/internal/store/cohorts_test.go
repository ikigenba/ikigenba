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

// R-DBGM-T9U5 R-DCOJ-71KU R-DDWF-KTBJ R-4B7S-FL5R R-4CFO-TCWG R-4EVH-KWDU R-GM8K-P2AP R-4HBA-CFV8
func TestCohortValidators(t *testing.T) {
	contractEqual(t, []string{store.CohortIDPrefix, store.OrderOperatingConfidence, store.OrderRevenueLow, store.OrderRevenueConfidence, store.OrderDriveMinutes, store.OrderNewestFiling, store.OrderKnownSince, store.DirectionDesc, store.DirectionAsc, store.KindReject, store.KindUnreject}, []string{"coh_", "operating_confidence", "revenue_low", "revenue_confidence", "drive_minutes", "newest_filing", "known_since", "desc", "asc", "reject", "unreject"})
	contractEqual(t, []int{store.CohortSizeMin, store.CohortSizeMax, store.CohortSizeDefault}, []int{1, 100, 30})
	for _, s := range []string{"coh_0123456789abcdef", "coh_0000000000000000"} {
		contractEqual(t, store.ValidCohortID(s), true)
	}
	for _, s := range []string{"coh_0123456789ABCDEF", "coh_0123456789abcde", "coh_0123456789abcdef0", "cand_0123456789abcdef", "coh_", ""} {
		contractEqual(t, store.ValidCohortID(s), false)
	}
	for _, s := range []string{"", " ", "\t\n", "\u2003"} {
		contractEqual(t, store.ValidCohortName(s), false)
	}
	contractEqual(t, store.ValidCohortName(" a "), true)
	for _, n := range []int64{-1, 0, 1, 30, 100, 101} {
		contractEqual(t, store.ValidCohortSize(n), n >= 1 && n <= 100)
	}
	good := []store.CohortFilters{{}, {RevenueUnknown: true, MinutesUnknown: true, CategoryUnknown: true, FleetUnknown: true, WebsiteUnknown: true, FilingUnknown: true, ClassUnknown: true, ShowExcluded: true}, {RevenueMin: ptrInt(5), RevenueMax: ptrInt(5)}, {Categories: []string{sources.CategoryConstruction, sources.CategoryTrucking, sources.CategoryAgriculture, sources.CategoryManufacturing, sources.CategoryWholesale, sources.CategoryMining, sources.CategoryWaste, sources.CategoryOther, sources.CategoryOther}}}
	for _, f := range good {
		contractEqual(t, store.ValidCohortFilters(f), true)
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
		contractEqual(t, store.ValidCohortFilters(store.CohortFilters{Categories: []string{category}}), true)
	}
	contractEqual(t, store.ValidCohortFilters(store.CohortFilters{Categories: []string{sources.CategoryGovernment, sources.CategoryEducation}}), true)
	bad := []store.CohortFilters{{RevenueMin: ptrInt(6), RevenueMax: ptrInt(5)}, {Categories: []string{""}}, {Categories: []string{"trucking"}}, {Categories: []string{"shops"}}, {Categories: []string{"Retail"}}, {Fleet: sources.FleetUnknown}, {Fleet: "PASS"}, {Class: sources.ClassUnknown}, {Class: "other"}}
	for i := 0; i < 4; i++ {
		f := store.CohortFilters{}
		fields := []**int64{&f.RevenueMin, &f.RevenueMax, &f.MaxMinutes, &f.MaxFilingAgeDays}
		*fields[i] = ptrInt(0)
		contractEqual(t, store.ValidCohortFilters(f), true)
		*fields[i] = ptrInt(-1)
		bad = append(bad, f)
	}
	for _, states := range [][]string{{}, {sources.WebsiteNone, sources.WebsiteNone}, {sources.WebsiteUnknown}, {""}, {"HTTPS"}} {
		bad = append(bad, store.CohortFilters{Website: states})
	}
	for _, states := range [][]string{{sources.WebsiteHTTPS}, {sources.WebsiteHTTP}, {sources.WebsiteNone}, {sources.WebsiteHTTPS, sources.WebsiteHTTP}} {
		contractEqual(t, store.ValidCohortFilters(store.CohortFilters{Website: states}), true)
	}
	for _, f := range bad {
		contractEqual(t, store.ValidCohortFilters(f), false)
	}
	for _, fleet := range []string{sources.FleetPass, sources.FleetFail} {
		contractEqual(t, store.ValidCohortFilters(store.CohortFilters{Fleet: fleet}), true)
	}
	for _, class := range []string{sources.ClassForHire, sources.ClassPrivate, sources.ClassMixed} {
		contractEqual(t, store.ValidCohortFilters(store.CohortFilters{Class: class}), true)
	}
	all := []store.CohortOrder{}
	for _, field := range []string{store.OrderOperatingConfidence, store.OrderRevenueLow, store.OrderRevenueConfidence, store.OrderDriveMinutes, store.OrderNewestFiling, store.OrderKnownSince} {
		all = append(all, store.CohortOrder{Field: field, Direction: store.DirectionAsc})
	}
	for _, o := range [][]store.CohortOrder{nil, {}, all, {{Field: store.OrderRevenueLow, Direction: store.DirectionDesc}}} {
		contractEqual(t, store.ValidCohortOrder(o), true)
	}
	for _, o := range [][]store.CohortOrder{{{Field: "minutes", Direction: store.DirectionAsc}}, {{Direction: store.DirectionAsc}}, {{Field: store.OrderRevenueLow, Direction: "DESC"}}, {{Field: store.OrderRevenueLow}}, {{Field: store.OrderRevenueLow, Direction: store.DirectionAsc}, {Field: store.OrderRevenueLow, Direction: store.DirectionDesc}}} {
		contractEqual(t, store.ValidCohortOrder(o), false)
	}
}

// R-IK72-EQQQ R-DGC8-CCSX
func TestCohortJSON(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{{store.CohortFilters{}, `{}`}, {store.CohortFilters{Government: new(bool)}, `{"government":false}`}, {store.CohortFilters{RevenueMin: ptrInt(5), Categories: []string{}}, `{"revenue_min":5}`}, {store.CohortOrder{}, `{"field":"","direction":""}`}} {
		b, e := json.Marshal(tc.v)
		contractOK(t, e)
		contractEqual(t, string(b), tc.want)
	}
	f := store.CohortFilters{RevenueMin: ptrInt(1), RevenueMax: ptrInt(2), RevenueUnknown: true, MaxMinutes: ptrInt(3), MinutesUnknown: true, Categories: []string{sources.CategoryOther}, CategoryUnknown: true, Fleet: sources.FleetPass, FleetUnknown: true, Website: []string{sources.WebsiteHTTPS, sources.WebsiteNone}, WebsiteUnknown: true, Government: new(bool), MaxFilingAgeDays: ptrInt(4), FilingUnknown: true, Class: sources.ClassPrivate, ClassUnknown: true, ShowExcluded: true}
	b, e := json.Marshal(f)
	contractOK(t, e)
	var round store.CohortFilters
	contractOK(t, json.Unmarshal(b, &round))
	contractEqual(t, round, f)
	contractEqual(t, string(b), `{"revenue_min":1,"revenue_max":2,"revenue_unknown":true,"max_minutes":3,"minutes_unknown":true,"categories":["other"],"category_unknown":true,"fleet":"pass","fleet_unknown":true,"website":["https","none"],"website_unknown":true,"government":false,"max_filing_age_days":4,"filing_unknown":true,"class":"private","class_unknown":true,"show_excluded":true}`)
}

func cohortCreate(t *testing.T, f *contractFixture, name string) store.Cohort {
	t.Helper()
	c, e := f.s.CreateCohort(context.Background(), store.Cohort{Name: name, Size: store.CohortSizeDefault, CreatedUserID: "creator", CreatedUserEmail: "creator@example.test"})
	contractOK(t, e)
	return c
}
func cohortRead(t *testing.T, f *contractFixture, c store.Cohort) store.CohortRead {
	t.Helper()
	r, e := f.s.ReadCohort(context.Background(), c.ID)
	contractOK(t, e)
	contractEqual(t, r.Cohort, c)
	if r.Members == nil || r.Rejects == nil {
		t.Fatal("nil cohort read list")
	}
	return r
}
func cohortIDs(rs []store.Ranked) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.Candidate.ID)
	}
	return out
}

// R-DHK4-Q4JM R-DIS1-3WAB R-DNNM-MZ93 R-65R0-29LI R-686S-TT2W R-69EP-7KTL R-DW6X-BDFY R-6AML-LCKA R-4IJ6-Q7LX R-4JR3-3ZCM
func TestCohortCreateEditPersistence(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	in := store.Cohort{ID: "ignored", Name: " Name ", Size: store.CohortSizeDefault, Filters: store.CohortFilters{Categories: []string{sources.CategoryTrucking, sources.CategoryOther, sources.CategoryTrucking}}, CreatedUserID: "creator", CreatedUserEmail: "creator@example.test", ChangedUserID: "ignored", Changed: time.Time{}}
	c, e := f.s.CreateCohort(ctx, in)
	contractOK(t, e)
	want := in
	want.ID = c.ID
	want.Filters.Categories = []string{sources.CategoryOther, sources.CategoryTrucking}
	want.Order = []store.CohortOrder{{Field: store.OrderOperatingConfidence, Direction: store.DirectionDesc}}
	want.Created = f.now.UTC().Truncate(time.Second)
	want.Changed = want.Created
	want.ChangedUserID = in.CreatedUserID
	want.ChangedUserEmail = in.CreatedUserEmail
	contractEqual(t, store.ValidCohortID(c.ID), true)
	contractEqual(t, c, want)
	other, e := f.s.CreateCohort(ctx, in)
	contractOK(t, e)
	if c.ID == other.ID {
		t.Fatal("reused id")
	}
	f.now = f.now.Add(time.Hour)
	for _, edit := range []store.CohortEdit{{ID: c.ID, UserID: "editor"}, {ID: c.ID, UserID: "editor", Filters: &in.Filters}} {
		got, e := f.s.EditCohort(ctx, edit)
		contractOK(t, e)
		contractEqual(t, got, c)
	}
	name := "New"
	size := int64(2)
	filters := store.CohortFilters{MaxMinutes: ptrInt(5)}
	order := []store.CohortOrder{{Field: store.OrderRevenueLow, Direction: store.DirectionAsc}}
	edit := store.CohortEdit{ID: c.ID, Name: &name, Size: &size, Filters: &filters, Order: &order, UserID: "editor", UserEmail: "editor@example.test"}
	got, e := f.s.EditCohort(ctx, edit)
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
	repeated, e := f.s.EditCohort(ctx, edit)
	contractOK(t, e)
	contractEqual(t, repeated, got)
	f.reopen()
	persisted, e := f.s.GetCohort(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, persisted, got)
	emptyOrder := []store.CohortOrder{}
	cleared, e := f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &store.CohortFilters{}, Order: &emptyOrder, UserID: "clear"})
	contractOK(t, e)
	contractEqual(t, cleared.Filters, store.CohortFilters{})
	contractEqual(t, cleared.Order, c.Order)
	unchanged, e := f.s.GetCohort(ctx, other.ID)
	contractOK(t, e)
	contractEqual(t, unchanged, other)
}

// R-DXET-P56N
func TestCohortInvalidArgumentsAndMissingRecords(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	original := cohortCreate(t, f, "Original")
	before := f.rnd.(*contractRandom).calls
	for _, in := range []store.Cohort{{}, {Name: "x", Size: 1}, {Name: " ", Size: 1, CreatedUserID: "owner"}, {Name: "x", CreatedUserID: "owner"}, {Name: "x", Size: 1, Filters: store.CohortFilters{Fleet: sources.FleetUnknown}, CreatedUserID: "owner"}, {Name: "x", Size: 1, Order: []store.CohortOrder{{Field: "bad"}}, CreatedUserID: "owner"}} {
		got, e := f.s.CreateCohort(ctx, in)
		want := store.ErrInvalid
		if in.CreatedUserID == "" {
			want = store.ErrUnreachable
		}
		contractError(t, e, want)
		contractEqual(t, got, store.Cohort{})
	}
	blank := " "
	zero := int64(0)
	badFilters := store.CohortFilters{MaxMinutes: ptrInt(-1)}
	badOrder := []store.CohortOrder{{Field: "bad"}}
	for _, edit := range []store.CohortEdit{{ID: "missing"}, {ID: "missing", Name: &blank, UserID: "owner"}, {ID: "missing", Size: &zero, UserID: "owner"}, {ID: "missing", Filters: &badFilters, UserID: "owner"}, {ID: "missing", Order: &badOrder, UserID: "owner"}, {ID: "missing", UserID: "owner"}} {
		got, e := f.s.EditCohort(ctx, edit)
		want := store.ErrInvalid
		if edit.UserID == "" {
			want = store.ErrUnreachable
		} else if edit.Name == nil && edit.Size == nil && edit.Filters == nil && edit.Order == nil {
			want = store.ErrNotFound
		}
		contractError(t, e, want)
		contractEqual(t, got, store.Cohort{})
	}
	for _, id := range []string{"missing", "coh_ffffffffffffffff"} {
		got, e := f.s.GetCohort(ctx, id)
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, got, store.Cohort{})
		got, e = f.s.DeleteCohort(ctx, id)
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, got, store.Cohort{})
		read, e := f.s.ReadCohort(ctx, id)
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, read, store.CohortRead{})
	}
	contractEqual(t, f.rnd.(*contractRandom).calls, before)
	got, e := f.s.GetCohort(ctx, original.ID)
	contractOK(t, e)
	contractEqual(t, got, original)
}

// R-DJZX-HO10 R-DUZ0-XLP9 R-E2AF-885F R-TF24-ID2R R-4ZLS-2ZZN
func TestCohortRejectHistoryAndDeletion(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	c := cohortCreate(t, f, "First")
	other := cohortCreate(t, f, "Other")
	ids := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now), contractObservation("b", "Beta", f.now))
	id := ids[0]
	empty, e := f.s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, empty, []store.Reject{})
	request := store.Reject{Kind: store.KindUnreject, Cohort: c.ID, CohortName: "ignored", Candidate: id, Why: "because", UserID: "owner", UserEmail: "owner@example.test", At: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)}
	noop, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	contractEqual(t, noop, store.Reject{Kind: store.KindUnreject, Cohort: c.ID, CohortName: c.Name, Candidate: id})
	request.Kind = store.KindReject
	first, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	want := request
	want.CohortName = c.Name
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
	contractEqual(t, len(cohortRead(t, f, c).Rejects), 0)
	request.Kind = store.KindReject
	third, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	otherReq := request
	otherReq.Cohort = other.ID
	fourth, e := f.s.Reject(ctx, otherReq)
	contractOK(t, e)
	history, e := f.s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, history, []store.Reject{fourth, third, second, first})
	contractEqual(t, cohortRead(t, f, c).Rejects, []store.Reject{third})
	contractEqual(t, cohortRead(t, f, other).Rejects, []store.Reject{fourth})
	request.Candidate = ids[1]
	fifth, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	contractEqual(t, cohortRead(t, f, c).Rejects, []store.Reject{fifth, third})
	renamed := "Renamed"
	c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Name: &renamed, UserID: "editor"})
	contractOK(t, e)
	third.CohortName = renamed
	fifth.CohortName = renamed
	contractEqual(t, cohortRead(t, f, c).Rejects, []store.Reject{fifth, third})
	f.reopen()
	history, e = f.s.Rejects(ctx, id)
	contractOK(t, e)
	for i := 1; i < len(history); i++ {
		contractEqual(t, history[i].CohortName, renamed)
	}
	deleted, e := f.s.DeleteCohort(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, deleted, c)
	f.reopen()
	_, e = f.s.GetCohort(ctx, c.ID)
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.ReadCohort(ctx, c.ID)
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.DeleteCohort(ctx, c.ID)
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, UserID: "owner"})
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.Reject(ctx, request)
	contractError(t, e, store.ErrNotFound)
	history, e = f.s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(history), 4)
	contractEqual(t, history[0], fourth)
	for _, r := range history[1:] {
		contractEqual(t, r.CohortName, "")
	}
	for _, reversed := range []bool{false, true} {
		var cs []store.Cohort
		if reversed {
			cs, e = f.s.ListCohortsReversed(ctx, "", "", 100)
		} else {
			cs, e = f.s.ListCohorts(ctx, "", "", 100)
		}
		contractOK(t, e)
		contractEqual(t, cs, []store.Cohort{other})
	}
	missing, e := f.s.Rejects(ctx, "missing")
	contractError(t, e, store.ErrNotFound)
	contractEqual(t, missing, []store.Reject(nil))
}

// R-TCMB-QTLD
func TestCohortRejectRefusals(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	c := cohortCreate(t, f, "Cohort")
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
	base := store.Reject{Kind: store.KindReject, Cohort: c.ID, Candidate: ids[0], Why: "reason", UserID: "owner"}
	for _, tc := range []struct {
		modify func(*store.Reject)
		want   error
	}{{func(r *store.Reject) { r.UserID = ""; r.Why = "" }, store.ErrUnreachable}, {func(r *store.Reject) { r.Kind = "bad"; r.Why = "" }, store.ErrUnreachable}, {func(r *store.Reject) { r.Why = "\t\n"; r.Cohort = "missing" }, store.ErrInvalid}, {func(r *store.Reject) { r.Cohort = "bad"; r.Candidate = "missing" }, store.ErrNotFound}, {func(r *store.Reject) { r.Candidate = "missing" }, store.ErrNotFound}, {func(r *store.Reject) { r.Candidate = merged }, store.ErrInvalid}, {func(r *store.Reject) { r.Candidate = merged; r.Kind = store.KindUnreject }, store.ErrInvalid}} {
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

// R-DRBB-SAH6
func TestCohortIDRandomnessAndRetiredIDs(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	meter := &contractMeter{reader: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8))}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: meter})
	c := cohortCreate(t, f, "First")
	contractEqual(t, c.ID, "coh_5a5a5a5a5a5a5a5a")
	contractEqual(t, meter.requests, []int{8})
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	id := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now))[0]
	_, e := f.s.Reject(ctx, store.Reject{Kind: store.KindReject, Cohort: c.ID, Candidate: id, Why: "reason", UserID: "owner"})
	contractOK(t, e)
	_, e = f.s.DeleteCohort(ctx, c.ID)
	contractOK(t, e)
	meter = &contractMeter{reader: bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 8), bytes.Repeat([]byte{0x6b}, 8)...))}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: meter})
	next := cohortCreate(t, f, "Next")
	contractEqual(t, next.ID, "coh_6b6b6b6b6b6b6b6b")
	contractEqual(t, meter.requests, []int{8, 8})
	for _, data := range [][]byte{bytes.Repeat([]byte{0x5a}, 64), {}} {
		meter = &contractMeter{reader: bytes.NewReader(data)}
		f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: meter})
		got, e := f.s.CreateCohort(ctx, store.Cohort{Name: "Fail", Size: 1, CreatedUserID: "owner"})
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, got, store.Cohort{})
		contractEqual(t, meter.total, len(data))
		if len(data) > 0 {
			contractEqual(t, len(meter.requests), 8)
		}
		cs, e := f.s.ListCohorts(ctx, "", "", 100)
		contractOK(t, e)
		contractEqual(t, cs, []store.Cohort{next})
	}
}

// R-DQ3F-EIQH R-DZUM-GOO1 R-E12I-UGEQ R-TGA0-W4TG R-THHX-9WK5
func TestCohortWindowsAndPlans(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	all := []store.Cohort{}
	for _, name := range []string{"same", "same", "same", "z", "Z", "é"} {
		all = append(all, cohortCreate(t, f, name))
	}
	slices.SortFunc(all, func(a, b store.Cohort) int {
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
		got := []store.Cohort{}
		name, id := "ignored", ""
		for {
			var window []store.Cohort
			var e error
			if reverse {
				window, e = f.s.ListCohortsReversed(ctx, name, id, 2)
			} else {
				window, e = f.s.ListCohorts(ctx, name, id, 2)
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
	for _, pos := range [][2]string{{"same", "coh_0202020202020203"}, {"t", "any"}, {"", "any"}, {"zz", "any"}} {
		for _, reverse := range []bool{false, true} {
			want := []store.Cohort{}
			for _, c := range all {
				cmp := strings.Compare(c.Name, pos[0])
				if cmp == 0 {
					cmp = strings.Compare(c.ID, pos[1])
				}
				if (!reverse && cmp > 0) || (reverse && cmp < 0) {
					want = append(want, c)
				}
			}
			var got []store.Cohort
			var e error
			if reverse {
				slices.Reverse(want)
				got, e = f.s.ListCohortsReversed(ctx, pos[0], pos[1], 100)
			} else {
				got, e = f.s.ListCohorts(ctx, pos[0], pos[1], 100)
			}
			contractOK(t, e)
			contractEqual(t, got, want)
		}
	}
	for _, tc := range []struct {
		q              windowStatement
		args           []any
		prefix, suffix string
	}{{store.CohortsFirst, []any{2}, "SCAN ", " INDEX cohorts_window"}, {store.CohortsAfter, []any{"same", "id", 2}, "SEARCH ", " INDEX cohorts_window ((name,id)>(?,?))"}, {store.CohortsLast, []any{2}, "SCAN ", " INDEX cohorts_window"}, {store.CohortsPreceding, []any{"same", "id", 2}, "SEARCH ", " INDEX cohorts_window ((name,id)<(?,?))"}} {
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

// R-TMDI-SZIX
func TestConcurrentCohortWrites(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	c := cohortCreate(t, f, "Concurrent")
	id := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now))[0]
	var wg sync.WaitGroup
	answers := make(chan store.Reject, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			r, e := f.s.Reject(ctx, store.Reject{Kind: store.KindReject, Cohort: c.ID, Candidate: id, Why: "reason", UserID: "owner"})
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
		wg.Go(func() { _, e := f.s.DeleteCohort(ctx, c.ID); deletes <- e })
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

func cohortPopulation(t *testing.T, f *contractFixture) []string {
	t.Helper()
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55070": 120, "55071": 121, "55072": 0}))
	ids := []string{}
	for i := range 5 {
		o := contractObservation(fmt.Sprint(i), fmt.Sprintf("Cohort business %d", i), time.Time{})
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
	_, e := f.s.JudgeAll(ctx)
	contractOK(t, e)
	return ids
}

// R-4M6V-VIU0 R-4NES-9AKP R-BENE-ZGE3 R-4PUL-0U23 R-4R2H-ELSS R-4SAD-SDJH R-4TIA-65A6 R-4UQ6-JX0V R-BFVB-D84S R-4KYZ-HR3B
func TestCohortFilterBoundariesAndUnknownSwitches(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := cohortPopulation(t, f)
	c := cohortCreate(t, f, "Filters")
	yes, no := true, false
	for _, tc := range []struct {
		name    string
		filter  store.CohortFilters
		indices []int
	}{
		{"none", store.CohortFilters{}, []int{0, 1, 2, 3, 4}},
		{"other switches alone", store.CohortFilters{RevenueUnknown: true, MinutesUnknown: true, FleetUnknown: true, WebsiteUnknown: true, FilingUnknown: true, ClassUnknown: true}, []int{0, 1, 2, 3, 4}},
		{"category unknown alone", store.CohortFilters{CategoryUnknown: true}, []int{3, 4}},
		{"all switches alone", store.CohortFilters{RevenueUnknown: true, MinutesUnknown: true, CategoryUnknown: true, FleetUnknown: true, WebsiteUnknown: true, FilingUnknown: true, ClassUnknown: true}, []int{3, 4}},
		{"revenue bounds", store.CohortFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000)}, []int{0, 1}},
		{"revenue unknown", store.CohortFilters{RevenueMin: ptrInt(5000000), RevenueMax: ptrInt(10000000), RevenueUnknown: true}, []int{0, 1, 4}},
		{"revenue only max", store.CohortFilters{RevenueMax: ptrInt(10000000)}, []int{0, 1, 2}},
		{"drive", store.CohortFilters{MaxMinutes: ptrInt(120)}, []int{0, 2}},
		{"drive unknown", store.CohortFilters{MaxMinutes: ptrInt(120), MinutesUnknown: true}, []int{0, 2, 3, 4}},
		{"category", store.CohortFilters{Categories: []string{sources.CategoryTrucking}}, []int{0}},
		{"category unknown", store.CohortFilters{Categories: []string{sources.CategoryTrucking}, CategoryUnknown: true}, []int{0, 3, 4}},
		{"categories or", store.CohortFilters{Categories: []string{sources.CategoryTrucking, sources.CategoryConstruction}}, []int{0, 2}},
		{"fleet pass", store.CohortFilters{Fleet: sources.FleetPass}, []int{0}},
		{"fleet unknown", store.CohortFilters{Fleet: sources.FleetPass, FleetUnknown: true}, []int{0, 2, 3, 4}},
		{"fleet fail", store.CohortFilters{Fleet: sources.FleetFail}, []int{1}},
		{"government", store.CohortFilters{Government: &yes}, []int{0, 2}},
		{"nongovernment", store.CohortFilters{Government: &no}, []int{1, 3, 4}},
		{"filing", store.CohortFilters{MaxFilingAgeDays: ptrInt(730)}, []int{0, 2, 3}},
		{"filing unknown", store.CohortFilters{MaxFilingAgeDays: ptrInt(730), FilingUnknown: true}, []int{0, 2, 3, 4}},
		{"filing today", store.CohortFilters{MaxFilingAgeDays: ptrInt(0)}, []int{2, 3}},
		{"filing maximum", store.CohortFilters{MaxFilingAgeDays: ptrInt(math.MaxInt64)}, []int{0, 1, 2, 3}},
		{"class", store.CohortFilters{Class: sources.ClassForHire}, []int{0}},
		{"class unknown", store.CohortFilters{Class: sources.ClassForHire, ClassUnknown: true}, []int{0, 2, 4}},
		{"combined", store.CohortFilters{MaxMinutes: ptrInt(120), RevenueMin: ptrInt(5000000), Fleet: sources.FleetPass, Government: &yes, MaxFilingAgeDays: ptrInt(730), Class: sources.ClassForHire, Categories: []string{sources.CategoryTrucking}}, []int{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e error
			c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &tc.filter, UserID: "editor"})
			contractOK(t, e)
			r := cohortRead(t, f, c)
			got := cohortIDs(r.Members)
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
		filters store.CohortFilters
		count   int64
	}{
		{store.CohortFilters{}, 4},
		{store.CohortFilters{RevenueUnknown: true, MinutesUnknown: true, FleetUnknown: true, WebsiteUnknown: true, FilingUnknown: true, ClassUnknown: true}, 4},
		{store.CohortFilters{CategoryUnknown: true}, 1},
		{store.CohortFilters{CategoryUnknown: true, ShowExcluded: true}, 2},
	} {
		c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &tc.filters, UserID: "owner"})
		contractOK(t, e)
		contractEqual(t, cohortRead(t, f, c).Matching, tc.count)
	}
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: ids[3], Why: "restore unknown", UserID: "owner"})
	contractOK(t, e)
	filter := store.CohortFilters{MaxFilingAgeDays: ptrInt(730)}
	c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &filter, UserID: "editor"})
	contractOK(t, e)
	f.now = time.Date(2026, 10, 15, 23, 59, 59, 0, time.FixedZone("read", -5*60*60))
	r := cohortRead(t, f, c)
	got := cohortIDs(r.Members)
	slices.Sort(got)
	want := []string{ids[2], ids[3]}
	slices.Sort(want)
	contractEqual(t, got, want)
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[2], Why: "exclude", UserID: "owner"})
	contractOK(t, e)
	r = cohortRead(t, f, c)
	contractEqual(t, cohortIDs(r.Members), []string{ids[3]})
	filter.ShowExcluded = true
	c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &filter, UserID: "editor"})
	contractOK(t, e)
	r = cohortRead(t, f, c)
	contractEqual(t, r.Matching, int64(2))
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: ids[2], Why: "include", UserID: "owner"})
	contractOK(t, e)
}

// R-4YDV-P88Y R-4JR3-3ZCM
func TestCohortOrderFieldsDirectionsUnknownsAndTies(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := cohortPopulation(t, f)
	c := cohortCreate(t, f, "Order")
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
				order := []store.CohortOrder{{Field: tc.field, Direction: direction}}
				var e error
				c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Order: &order, UserID: "owner"})
				contractOK(t, e)
				indices := tc.asc
				if direction == store.DirectionDesc {
					indices = tc.desc
				}
				want := []string{}
				for _, i := range indices {
					want = append(want, ids[i])
				}
				contractEqual(t, cohortIDs(cohortRead(t, f, c).Members), want)
			})
		}
	}
	order := []store.CohortOrder{{Field: store.OrderRevenueConfidence, Direction: store.DirectionAsc}, {Field: store.OrderOperatingConfidence, Direction: store.DirectionDesc}}
	c, e := f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Order: &order, UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, cohortIDs(cohortRead(t, f, c).Members), []string{ids[1], ids[3], ids[2], ids[0], ids[4]})
}

// R-7WVC-DE2R R-O9NE-3VU4 R-BH37-QZVH R-4ZLS-2ZZN
func TestCohortMembershipReadsStoredJudgmentsAndStandingRejects(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := cohortPopulation(t, f)
	unjudged := contractAttach(t, f, "run", contractObservation("unjudged", "Unjudged business", f.now))[0]
	c := cohortCreate(t, f, "Membership")
	size := int64(2)
	c, e := f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Size: &size, UserID: "owner"})
	contractOK(t, e)
	r := cohortRead(t, f, c)
	contractEqual(t, r.Matching, int64(5))
	contractEqual(t, cohortIDs(r.Members), []string{ids[0], ids[2]})
	for _, member := range r.Members {
		candidate, e := f.s.GetCandidate(ctx, member.Candidate.ID)
		contractOK(t, e)
		assessment, e := f.s.GetAssessment(ctx, candidate.ID)
		contractOK(t, e)
		contractEqual(t, member, store.Ranked{Candidate: candidate, Assessment: assessment})
	}
	_ = unjudged
	request := store.Reject{Kind: store.KindReject, Cohort: c.ID, Candidate: ids[0], Why: "owner rejected", UserID: "owner"}
	first, e := f.s.Reject(ctx, request)
	contractOK(t, e)
	r = cohortRead(t, f, c)
	contractEqual(t, r.Matching, int64(4))
	contractEqual(t, cohortIDs(r.Members), []string{ids[2], ids[1]})
	contractEqual(t, r.Rejects, []store.Reject{first})
	request.Kind = store.KindUnreject
	_, e = f.s.Reject(ctx, request)
	contractOK(t, e)
	r = cohortRead(t, f, c)
	contractEqual(t, r.Matching, int64(5))
	contractEqual(t, cohortIDs(r.Members), []string{ids[0], ids[2]})
	filters := store.CohortFilters{RevenueMin: ptrInt(10000000)}
	c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	request.Kind = store.KindReject
	_, e = f.s.Reject(ctx, request)
	contractOK(t, e)
	filters = store.CohortFilters{}
	c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, cohortRead(t, f, c).Matching, int64(4))
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[1], Other: ids[0], Why: "same", UserID: "owner"})
	contractOK(t, e)
	// The merged candidate's reject stays visible and its history stays available.
	merged, e := f.s.GetCandidate(ctx, ids[0])
	contractOK(t, e)
	if merged.Status != store.StatusMerged {
		t.Fatal("fixture candidate did not merge")
	}
	r = cohortRead(t, f, c)
	contractEqual(t, len(r.Rejects), 1)
	contractEqual(t, r.Rejects[0].Candidate, ids[0])
	rs, e := f.s.Rejects(ctx, ids[0])
	contractOK(t, e)
	contractEqual(t, len(rs), 3)
	f.reopen()
	persisted := cohortRead(t, f, c)
	contractEqual(t, persisted, r)
	// Replacing drive times does not rewrite the judgment used for filtering.
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{}))
	r = cohortRead(t, f, c)
	contractEqual(t, r, persisted)
	empty := cohortCreate(t, f, "Empty")
	filters = store.CohortFilters{RevenueMin: ptrInt(math.MaxInt64)}
	empty, e = f.s.EditCohort(ctx, store.CohortEdit{ID: empty.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	read := cohortRead(t, f, empty)
	contractEqual(t, read.Members, []store.Ranked{})
	contractEqual(t, read.Rejects, []store.Reject{})
	contractEqual(t, read.Matching, int64(0))
}

// R-O9NE-3VU4
func TestCohortReadUsesOneClockSnapshot(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	cohortPopulation(t, f)
	c := cohortCreate(t, f, "Clock")
	filter := store.CohortFilters{MaxFilingAgeDays: ptrInt(730)}
	c, e := f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &filter, UserID: "owner"})
	contractOK(t, e)
	calls := 0
	f.s = store.New(f.d, store.Config{Rand: f.rnd, Now: func() time.Time { calls++; return f.now.Add(time.Duration(calls-1) * 24 * time.Hour) }})
	r := cohortRead(t, f, c)
	contractEqual(t, calls, 1)
	contractEqual(t, r.Matching, int64(3))
}

// R-DHK4-Q4JM R-DIS1-3WAB R-DJZX-HO10 R-7WVC-DE2R R-IK72-EQQQ R-DGC8-CCSX
func TestCohortRecordShapesByUse(t *testing.T) {
	type cohort struct {
		ID, Name                        string
		Size                            int64
		Filters                         store.CohortFilters
		Order                           []store.CohortOrder
		CreatedUserID, CreatedUserEmail string
		Created                         time.Time
		ChangedUserID, ChangedUserEmail string
		Changed                         time.Time
	}
	type edit struct {
		ID                string
		Name              *string
		Size              *int64
		Filters           *store.CohortFilters
		Order             *[]store.CohortOrder
		UserID, UserEmail string
	}
	type reject struct {
		Kind, Cohort, CohortName, Candidate, Why, UserID, UserEmail string
		At                                                          time.Time
	}
	type read struct {
		Cohort     store.Cohort
		Matching   int64
		Classified int64
		Members    []store.Ranked
		Rejects    []store.Reject
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
		Government             *bool
		MaxFilingAgeDays       *int64
		FilingUnknown          bool
		Class                  string
		ClassUnknown           bool
		ShowExcluded           bool
	}
	contractEqual(t, filters(store.CohortFilters{}), filters{})
	contractEqual(t, cohort(store.Cohort{}), cohort{})
	contractEqual(t, edit(store.CohortEdit{}), edit{})
	contractEqual(t, reject(store.Reject{}), reject{})
	contractEqual(t, read(store.CohortRead{}), read{})
	contractEqual(t, order(store.CohortOrder{}), order{})
}

// R-CT65-2PPQ R-CUE1-GHGF R-D0HJ-DC5W R-D1PF-R3WL
func TestCohortExportedWindowStatements(t *testing.T) {
	const first windowStatement = store.CohortsFirst
	const after windowStatement = store.CohortsAfter
	const last windowStatement = store.CohortsLast
	const preceding windowStatement = store.CohortsPreceding
	f := contractDB(t)
	ctx := context.Background()
	for _, name := range []string{"same", "same", "A", "z"} {
		cohortCreate(t, f, name)
	}
	full, e := f.s.ListCohorts(ctx, "", "", 100)
	contractOK(t, e)
	for _, tc := range []struct {
		q        windowStatement
		args     []any
		name, id string
		reverse  bool
	}{{first, []any{2}, "", "", false}, {last, []any{2}, "", "", true}, {after, []any{full[1].Name, full[1].ID, 2}, full[1].Name, full[1].ID, false}, {preceding, []any{full[2].Name, full[2].ID, 2}, full[2].Name, full[2].ID, true}, {after, []any{"t", "arbitrary", 2}, "t", "arbitrary", false}, {preceding, []any{"t", "arbitrary", 2}, "t", "arbitrary", true}} {
		var cs []store.Cohort
		if tc.reverse {
			cs, e = f.s.ListCohortsReversed(ctx, tc.name, tc.id, 2)
		} else {
			cs, e = f.s.ListCohorts(ctx, tc.name, tc.id, 2)
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

// R-4YDV-P88Y
func TestCohortOperatingOrderUsesRoundedKey(t *testing.T) {
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
	_, e := f.s.JudgeAll(ctx)
	contractOK(t, e)
	a, e := f.s.GetAssessment(ctx, ids[0])
	contractOK(t, e)
	b, e := f.s.GetAssessment(ctx, ids[1])
	contractOK(t, e)
	contractEqual(t, a.OperatingKey, b.OperatingKey)
	contractEqual(t, *a.Operating, *b.Operating)
	c := cohortCreate(t, f, "Rounded")
	contractEqual(t, cohortIDs(cohortRead(t, f, c).Members), ids)
}

// R-BENE-ZGE3
func TestCohortCategoryMatchesAnyAssessmentCategory(t *testing.T) {
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
	_, e := f.s.JudgeAll(ctx)
	contractOK(t, e)
	a, e := f.s.GetAssessment(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(a.Categories), 2)
	c := cohortCreate(t, f, "Categories")
	for _, unknown := range []bool{false, true} {
		filters := store.CohortFilters{Categories: []string{sources.CategoryTrucking}, CategoryUnknown: unknown}
		c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
		contractOK(t, e)
		contractEqual(t, cohortIDs(cohortRead(t, f, c).Members), []string{id})
	}
}

// R-PKTA-MKUR R-BFVB-D84S R-BH37-QZVH
func TestCohortWebsiteFiltersReadStoredJudgment(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := []string{}
	for i, state := range []string{sources.WebsiteHTTPS, sources.WebsiteHTTP, sources.WebsiteNone, sources.WebsiteUnknown} {
		o := contractObservation(fmt.Sprint(i), fmt.Sprintf("Website cohort %d", i), f.now)
		o.Reading.Email = fmt.Sprintf("owner@business%d.example", i)
		id := contractAttach(t, f, "run", o)[0]
		ids = append(ids, id)
		if state != sources.WebsiteUnknown {
			_, e := f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: fmt.Sprintf("business%d.example", i), Inputs: fmt.Sprintf("business%d.example", i), Found: websiteFound(website.Result{State: state})}})
			contractOK(t, e)
		}
	}
	deriveAll(t, f.s)
	_, e := f.s.JudgeAll(ctx)
	contractOK(t, e)
	c := cohortCreate(t, f, "Websites")
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
			filters := store.CohortFilters{Website: tc.states, WebsiteUnknown: unknown}
			c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
			contractOK(t, e)
			// The requested order is kept as supplied across serialization and reopening.
			f.reopen()
			got := cohortRead(t, f, c)
			want := []string{}
			for _, i := range tc.indices {
				want = append(want, ids[i])
			}
			if unknown {
				want = append(want, ids[3])
			}
			actual := cohortIDs(got.Members)
			slices.Sort(actual)
			slices.Sort(want)
			contractEqual(t, actual, want)
			contractEqual(t, got.Matching, int64(len(want)))
		}
	}
	combined := store.CohortFilters{Website: []string{sources.WebsiteHTTPS}, RevenueMin: ptrInt(math.MaxInt64)}
	c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &combined, UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, cohortRead(t, f, c).Matching, int64(0))
	// A result updates cohort facts before a judge.
	filters := store.CohortFilters{Website: []string{sources.WebsiteHTTPS}}
	c, e = f.s.EditCohort(ctx, store.CohortEdit{ID: c.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	_, e = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids[2], Kind: sources.WebsiteCheck, Source: "business2.example", Inputs: "business2.example", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS})}})
	contractOK(t, e)
	contractEqual(t, cohortRead(t, f, c).Matching, int64(2))
	_, e = f.s.Judge(ctx, []string{ids[2]})
	contractOK(t, e)
	contractEqual(t, cohortRead(t, f, c).Matching, int64(2))
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[0], Why: "owner excluded", UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, cohortIDs(cohortRead(t, f, c).Members), []string{ids[2]})
}

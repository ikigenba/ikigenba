package tools_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-A0V6-EFAB R-V9H1-Z5PM R-A3AZ-5YRP R-A4IV-JQIE R-A5QR-XI93 R-A6YO-B9ZS R-A9EH-2TH6 R-VAOY-CXGB R-AD26-84P9
func TestCohortExactSchemas(t *testing.T) {
	f := newFixture(t)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	filters := `{"type":"object","properties":{"revenue_min":{"type":"integer","minimum":0},"revenue_max":{"type":"integer","minimum":0},"revenue_unknown":{"type":"boolean"},"max_minutes":{"type":"integer","minimum":0},"minutes_unknown":{"type":"boolean"},"categories":{"type":"array","items":{"type":"string"}},"category_unknown":{"type":"boolean"},"fleet":{"type":"string"},"fleet_unknown":{"type":"boolean"},"website":{"type":"array","items":{"type":"string"}},"website_unknown":{"type":"boolean"},"government":{"type":"boolean"},"max_filing_age_days":{"type":"integer","minimum":0},"filing_unknown":{"type":"boolean"},"class":{"type":"string"},"class_unknown":{"type":"boolean"},"show_excluded":{"type":"boolean"}},"additionalProperties":false}`
	order := `{"type":"array","items":{"type":"object","properties":{"field":{"type":"string"},"direction":{"type":"string"}},"required":["field","direction"],"additionalProperties":false}}`
	want := map[string]string{
		"create_cohort": `{"type":"object","properties":{"name":{"type":"string"},"filters":` + filters + `,"order":` + order + `,"size":{"type":"integer"}},"required":["name"],"additionalProperties":false}`,
		"edit_cohort":   `{"type":"object","properties":{"cohort":{"type":"string"},"name":{"type":"string"},"filters":` + filters + `,"order":` + order + `,"size":{"type":"integer"}},"required":["cohort"],"additionalProperties":false}`,
	}
	for _, n := range []string{"cohort", "delete_cohort"} {
		want[n] = `{"type":"object","properties":{"cohort":{"type":"string"}},"required":["cohort"],"additionalProperties":false}`
	}
	for _, n := range []string{"exclude", "include"} {
		want[n] = `{"type":"object","properties":{"candidate":{"type":"string"},"why":{"type":"string"}},"required":["candidate","why"],"additionalProperties":false}`
	}
	for _, n := range []string{"reject", "unreject"} {
		want[n] = `{"type":"object","properties":{"cohort":{"type":"string"},"candidate":{"type":"string"},"why":{"type":"string"}},"required":["cohort","candidate","why"],"additionalProperties":false}`
	}
	for _, info := range infos {
		if expected, ok := want[info.Name]; ok {
			if string(info.InputSchema) != expected {
				t.Fatalf("%s got %s want %s", info.Name, info.InputSchema, expected)
			}
			delete(want, info.Name)
		}
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
}

func cohortExpected(k store.Cohort) tools.Cohort {
	return tools.Cohort{ID: k.ID, Name: k.Name, Size: k.Size, Filters: k.Filters, Order: k.Order, CreatedBy: k.CreatedUserID, CreatedEmail: k.CreatedUserEmail, Created: stamp(k.Created), ChangedBy: k.ChangedUserID, ChangedEmail: k.ChangedUserEmail, Changed: stamp(k.Changed)}
}
func rejectExpected(r store.Reject) tools.Reject {
	v := tools.Reject{Kind: r.Kind, Cohort: r.Cohort, CohortName: r.CohortName, Candidate: r.Candidate, Why: r.Why, User: r.UserID, Email: r.UserEmail}
	if !r.At.IsZero() {
		v.At = stamp(r.At)
	}
	return v
}
func memberExpected(r store.Ranked) tools.Member {
	a := assessmentExpected(r.Assessment)
	v := tools.Member{Candidate: candidateOf(r.Candidate), OperatingConfidence: a.OperatingConfidence, RevenueLow: a.RevenueLow, RevenueHigh: a.RevenueHigh, RevenueConfidence: a.RevenueConfidence, FleetVerdict: a.FleetVerdict, Website: a.Website, WebsiteURL: a.WebsiteURL, Minutes: a.Minutes, GovernmentMatch: a.GovernmentMatch, KnownSince: a.KnownSince, NewestFiling: a.NewestFiling, Excluded: a.Excluded, Categories: []string{}}
	seen := map[string]bool{}
	for _, c := range a.Categories {
		seen[c.Name] = true
	}
	for name := range seen {
		v.Categories = append(v.Categories, name)
	}
	slices.Sort(v.Categories)
	return v
}

func readExpected(r store.CohortRead) tools.CohortRead {
	v := tools.CohortRead{Cohort: cohortExpected(r.Cohort), Matching: r.Matching}
	for _, m := range r.Members {
		v.Members = append(v.Members, memberExpected(m))
	}
	for _, x := range r.Rejects {
		v.Rejects = append(v.Rejects, rejectExpected(x))
	}
	return v
}
func (f *fixture) create(name string) tools.Cohort {
	f.t.Helper()
	r := f.call("create_cohort", tools.CreateCohortArgs{Name: name})
	k := decodeList[tools.Cohort](f.t, r)
	if r.IsError() {
		f.t.Fatal(decode(f.t, r))
	}
	return k
}

// R-VRRJ-PPU1
// R-AFHY-ZO6N R-ODX3-TRBJ R-AHXR-R7O1 R-AJ5O-4ZEQ R-AXSG-Q8B2 R-BEV2-30OS R-P2HO-Q5JG R-BKYJ-ZVE9 R-1POC-ZXB9 R-1S45-RGSN R-BDN5-P8Y3
func TestCohortLifecycleRejectAndExclusion(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(5)
	k := f.create("  Working cohort  ")
	duplicate := f.create(k.Name)
	if k.ID == duplicate.ID || k.Size != store.CohortSizeDefault || k.CreatedBy != caller.UserID || k.ChangedBy != caller.UserID || k.CreatedEmail != caller.Email || k.ChangedEmail != caller.Email {
		t.Fatal(k, duplicate)
	}
	stored, err := f.st.GetCohort(t.Context(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	success(t, f.call("edit_cohort", tools.EditCohortArgs{Cohort: k.ID}), cohortExpected(stored))
	f.now = f.now.Add(time.Hour)
	size := 2
	name := "changed-name"
	result := f.callAs(other, "edit_cohort", tools.EditCohortArgs{Cohort: k.ID, Name: &name, Size: &size})
	k = decodeList[tools.Cohort](t, result)
	stored, err = f.st.GetCohort(t.Context(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	success(t, result, cohortExpected(stored))
	if k.CreatedBy != caller.UserID || k.ChangedBy != other.UserID {
		t.Fatal(k)
	}
	read, err := f.st.ReadCohort(t.Context(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	success(t, f.call("cohort", tools.CohortArgs{Cohort: k.ID}), readExpected(read))
	if read.Matching != 5 || len(read.Members) != 2 {
		t.Fatal(read)
	}
	id := read.Members[0].Candidate.ID
	args := tools.RejectArgs{Cohort: k.ID, Candidate: id, Why: "  chosen reason  "}
	r := f.call("reject", args)
	rejected := decodeList[tools.Reject](t, r)
	xs, err := f.st.Rejects(t.Context(), id)
	if err != nil || len(xs) != 1 {
		t.Fatal(xs, err)
	}
	success(t, r, rejectExpected(xs[0]))
	success(t, f.callAs(other, "reject", args), rejected)
	read, err = f.st.ReadCohort(t.Context(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	success(t, f.call("cohort", tools.CohortArgs{Cohort: k.ID}), readExpected(read))
	if read.Matching != 4 || len(read.Rejects) != 1 {
		t.Fatal(read)
	}
	detail := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if !reflect.DeepEqual(detail.Rejects, []tools.Reject{rejected}) {
		t.Fatal(detail.Rejects)
	}
	f.call("unreject", args)
	success(t, f.call("unreject", args), tools.Reject{Kind: store.KindUnreject, Cohort: k.ID, CohortName: name, Candidate: id})
	for _, kind := range []string{store.KindExclude, store.KindInclude} {
		result = f.callAs(other, kind, tools.RulingArgs{Candidate: id, Why: " reason "})
		rs, err := f.st.Rulings(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		c, err := f.st.GetCandidate(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		success(t, result, tools.Decision{Ruling: tools.Ruling{Kind: kind, Candidate: id, Why: " reason ", User: other.UserID, Email: other.Email, At: stamp(rs[0].At)}, Candidates: []tools.Candidate{candidateOf(c)}})
		got := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
		if got.Assessment == nil || got.Assessment.Excluded != (kind == store.KindExclude) {
			t.Fatal(got)
		}
		for _, cohort := range []string{k.ID, duplicate.ID} {
			cr := decodeList[tools.CohortRead](t, f.call("cohort", tools.CohortArgs{Cohort: cohort}))
			for _, m := range cr.Members {
				if m.Candidate.ID == id && kind == store.KindExclude {
					t.Fatal(cr)
				}
			}
			if cohort == duplicate.ID && cr.Matching != int64(map[string]int{store.KindExclude: 4, store.KindInclude: 5}[kind]) {
				t.Fatal(cr)
			}
		}
	}
	// Reject history remains for a merged candidate and after its cohort is deleted.
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "fixture-inputs", Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: "https://business.test"}), Source: "business.test"}}); err != nil {
		t.Fatal(err)
	}
	enriched := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if len(enriched.Enrichments) != 1 {
		t.Fatal(enriched)
	}
	f.call("reject", args)
	otherID := ids[0]
	if otherID == id {
		otherID = ids[1]
	}
	f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: otherID, Other: id, Why: "merge"})
	detail = decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if detail.Candidate.MergedInto != otherID || detail.Assessment != nil || len(detail.Links) != 0 || len(detail.Estimates) != 0 || detail.Enrichments == nil || len(detail.Enrichments) != 0 || len(detail.Rejects) != 3 {
		t.Fatal(detail)
	}
	for _, kind := range []string{"reject", "unreject"} {
		refusal(t, f.call(kind, args), passes.RefusalInvalid, tools.RefuseInvalid)
	}
	success(t, f.call("delete_cohort", tools.CohortArgs{Cohort: k.ID}), k)
	for _, kind := range []string{"cohort", "delete_cohort"} {
		refusal(t, f.call(kind, tools.CohortArgs{Cohort: k.ID}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	refusal(t, f.call("edit_cohort", tools.EditCohortArgs{Cohort: k.ID}), passes.RefusalNotFound, tools.RefuseNotFound)
	refusal(t, f.call("reject", args), passes.RefusalNotFound, tools.RefuseNotFound)
	detail = decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	for _, x := range detail.Rejects {
		if x.CohortName != "" {
			t.Fatal(x)
		}
	}
	list := decodeList[tools.CohortList](t, f.call("cohorts", tools.CohortsArgs{}))
	if len(list.Cohorts) != 1 || list.Cohorts[0].ID != duplicate.ID {
		t.Fatal(list)
	}
}

// R-BG2Y-GSFH R-P2HO-Q5JG R-BKYJ-ZVE9 R-VWN5-8SST
func TestCohortFiltersAndReplacement(t *testing.T) {
	f := newFixture(t)
	lo, hi, minutes, days := uint32(0), uint32(20000000), uint32(120), uint32(730)
	websites := []string{sources.WebsiteHTTP, sources.WebsiteHTTPS}
	government := false
	args := tools.FilterArgs{RevenueMin: &lo, RevenueMax: &hi, RevenueUnknown: true, MaxMinutes: &minutes, MinutesUnknown: true, Categories: []string{sources.CategoryTrucking, sources.CategoryOther}, CategoryUnknown: true, Fleet: sources.FleetPass, FleetUnknown: true, Website: &websites, WebsiteUnknown: true, Government: &government, MaxFilingAgeDays: &days, FilingUnknown: true, Class: sources.ClassForHire, ClassUnknown: true, ShowExcluded: true}
	order := []tools.OrderArgs{{Field: store.OrderRevenueLow, Direction: store.DirectionAsc}, {Field: store.OrderKnownSince, Direction: store.DirectionDesc}}
	size := 17
	r := f.call("create_cohort", tools.CreateCohortArgs{Name: "filtered", Filters: &args, Order: &order, Size: &size})
	k := decodeList[tools.Cohort](t, r)
	a, b, c, d := int64(lo), int64(hi), int64(minutes), int64(days)
	want := store.CohortFilters{RevenueMin: &a, RevenueMax: &b, RevenueUnknown: true, MaxMinutes: &c, MinutesUnknown: true, Categories: []string{sources.CategoryOther, sources.CategoryTrucking}, CategoryUnknown: true, Fleet: args.Fleet, FleetUnknown: true, Website: websites, WebsiteUnknown: true, Government: &government, MaxFilingAgeDays: &d, FilingUnknown: true, Class: args.Class, ClassUnknown: true, ShowExcluded: true}
	if !reflect.DeepEqual(k.Filters, want) || k.Size != int64(size) || !reflect.DeepEqual(k.Order, []store.CohortOrder{{Field: store.OrderRevenueLow, Direction: store.DirectionAsc}, {Field: store.OrderKnownSince, Direction: store.DirectionDesc}}) {
		t.Fatal(k)
	}
	stored, err := f.st.GetCohort(t.Context(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	success(t, r, cohortExpected(stored))
	f.now = f.now.Add(time.Hour)
	for _, extra := range []map[string]any{{}, {"filters": nil, "order": nil, "size": nil, "name": nil}} {
		extra["cohort"] = k.ID
		success(t, f.call("edit_cohort", extra), k)
	}
	empty := []tools.OrderArgs{}
	r = f.call("edit_cohort", map[string]any{"cohort": k.ID, "filters": map[string]any{}, "order": empty})
	cleared := decodeList[tools.Cohort](t, r)
	bts, _ := json.Marshal(cleared.Filters)
	if string(bts) != "{}" || !reflect.DeepEqual(cleared.Order, []store.CohortOrder{{Field: store.OrderOperatingConfidence, Direction: store.DirectionDesc}}) {
		t.Fatal(cleared)
	}
	f.now = f.now.Add(time.Hour)
	success(t, f.call("edit_cohort", map[string]any{"cohort": k.ID, "filters": map[string]any{}, "order": empty}), cleared)
}

// R-B2O2-9B9U R-B1G5-VJJ5 R-AZ0D-401R
func TestCohortPagingNameTies(t *testing.T) {
	for _, count := range []int{0, 25, 26, 30} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := newFixture(t)
			success(t, f.call("cohorts", tools.CohortsArgs{}), tools.CohortList{})
			for i := range count {
				f.create([]string{"a", "A", "a", "z"}[i%4])
			}
			if count > paging.DefaultLimit {
				checkDefaultPage(t, f, "cohorts")
			}
			all, err := f.st.ListCohorts(t.Context(), "", "", 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, limit := range []int{1, 7, paging.DefaultLimit, 30} {
				var next *string
				offset := 0
				for {
					end := min(offset+limit, len(all))
					want := tools.CohortList{}
					for _, k := range all[offset:end] {
						want.Cohorts = append(want.Cohorts, cohortExpected(k))
					}
					if end < len(all) {
						last := all[end-1]
						token := paging.Token{Tool: "cohorts"}.Encode(last.Name, last.ID)
						want.Next = &token
					}
					success(t, f.call("cohorts", tools.CohortsArgs{Page: next, Limit: &limit}), want)
					if want.Next == nil {
						break
					}
					next = want.Next
					offset = end
				}
			}
		})
	}
}

// R-GM8K-P2AP
func TestCohortCategoryCreateAndEdit(t *testing.T) {
	f := newFixture(t)
	k := f.create("category edits")
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
		t.Run(category, func(t *testing.T) {
			filters := tools.FilterArgs{Categories: []string{category}}
			created := f.call("create_cohort", tools.CreateCohortArgs{Name: category, Filters: &filters})
			if created.IsError() {
				t.Fatal(decode(t, created))
			}
			if got := decodeList[tools.Cohort](t, created); !reflect.DeepEqual(got.Filters.Categories, filters.Categories) {
				t.Fatal(got.Filters)
			}
			edited := f.call("edit_cohort", tools.EditCohortArgs{Cohort: k.ID, Filters: &filters})
			if edited.IsError() {
				t.Fatal(decode(t, edited))
			}
			if got := decodeList[tools.Cohort](t, edited); !reflect.DeepEqual(got.Filters.Categories, filters.Categories) {
				t.Fatal(got.Filters)
			}
		})
	}
	for _, category := range []string{"shops", "Retail", "", "trucking", "retail ", "EDUCATION", "governments"} {
		filters := tools.FilterArgs{Categories: []string{sources.CategoryGovernment, category}}
		refusal(t, f.call("create_cohort", tools.CreateCohortArgs{Name: "invalid category", Filters: &filters}), passes.RefusalInvalid, tools.RefuseInvalid)
		refusal(t, f.call("edit_cohort", tools.EditCohortArgs{Cohort: k.ID, Filters: &filters}), passes.RefusalInvalid, tools.RefuseInvalid)
	}
}

// R-BHAU-UK66 R-BJQN-M3NK R-1QW9-DP1Y R-BCF9-BH7E R-B8RK-65ZB R-VWN5-8SST R-OL8I-4DRP R-OMGE-I5IE
func TestCohortValidationBeforeFailingStore(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	k := f.create("validation")
	unknown := store.CohortIDPrefix + strings.Repeat("0", 16)
	unknownCandidate := store.CandidateIDPrefix + strings.Repeat("0", 16)
	badParts := []map[string]any{{"name": " \t\n"}, {"size": 0}, {"size": 101}, {"size": -1}, {"filters": map[string]any{"revenue_min": 2, "revenue_max": 1}}, {"filters": map[string]any{"website": []string{}}}, {"filters": map[string]any{"fleet": sources.FleetUnknown}}, {"filters": map[string]any{"class": sources.ClassUnknown}}, {"filters": map[string]any{"categories": []string{"invalid"}}}, {"order": []map[string]any{{"field": "invalid", "direction": store.DirectionAsc}}}, {"order": []map[string]any{{"field": store.OrderRevenueLow, "direction": "invalid"}}}, {"order": []map[string]any{{"field": store.OrderRevenueLow, "direction": store.DirectionAsc}, {"field": store.OrderRevenueLow, "direction": store.DirectionDesc}}}}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, part := range badParts {
			create := map[string]any{"name": "valid"}
			edit := map[string]any{"cohort": "bad"}
			for key, v := range part {
				create[key] = v
				edit[key] = v
			}
			refusal(t, f.call("create_cohort", create), passes.RefusalInvalid, tools.RefuseInvalid)
			refusal(t, f.call("edit_cohort", edit), passes.RefusalInvalid, tools.RefuseInvalid)
		}
		for _, kind := range []string{"cohort", "delete_cohort", "edit_cohort"} {
			refusal(t, f.call(kind, map[string]any{"cohort": "bad"}), passes.RefusalNotFound, tools.RefuseNotFound)
		}
		for _, kind := range []string{"reject", "unreject"} {
			refusal(t, f.call(kind, tools.RejectArgs{Cohort: "bad", Candidate: "bad", Why: " \t\n"}), passes.RefusalInvalid, tools.RefuseInvalid)
			for _, args := range []tools.RejectArgs{{Cohort: "bad", Candidate: ids[0], Why: "reason"}, {Cohort: k.ID, Candidate: "bad", Why: "reason"}} {
				refusal(t, f.call(kind, args), passes.RefusalNotFound, tools.RefuseNotFound)
			}
		}
		for _, kind := range []string{"exclude", "include"} {
			refusal(t, f.call(kind, tools.RulingArgs{Candidate: "bad", Why: " \t\n"}), passes.RefusalInvalid, tools.RefuseInvalid)
			refusal(t, f.call(kind, tools.RulingArgs{Candidate: "bad", Why: "reason"}), passes.RefusalNotFound, tools.RefuseNotFound)
		}
		for _, kind := range []string{store.KindExclude, store.KindInclude, "unknown"} {
			refusal(t, f.call("decide", tools.DecideArgs{Kind: kind, Candidate: "bad", Why: "reason"}), passes.RefusalInvalid, tools.RefuseInvalid)
		}
		for _, kind := range []string{store.KindMerge, store.KindNotSame, store.KindSplit} {
			refusal(t, f.call("decide", tools.DecideArgs{Kind: kind, Candidate: "bad", Why: "reason"}), passes.RefusalInvalid, tools.RefuseInvalid)
			refusal(t, f.call("decide", tools.DecideArgs{Kind: kind, Candidate: "bad", Other: ids[0], Links: []string{"key"}, Why: "reason"}), passes.RefusalNotFound, tools.RefuseNotFound)
		}
		// Typed nested schemas reject malformed values before handlers, even with an unreachable store.
		for _, part := range []map[string]any{{"filters": map[string]any{"unknown": true}}, {"filters": map[string]any{"revenue_min": -1}}, {"filters": map[string]any{"revenue_max": -1}}, {"filters": map[string]any{"max_minutes": -1}}, {"filters": map[string]any{"max_filing_age_days": -1}}, {"order": []map[string]any{{"field": store.OrderRevenueLow}}}, {"order": []map[string]any{{"direction": store.DirectionAsc}}}, {"order": []map[string]any{{"field": store.OrderRevenueLow, "direction": store.DirectionAsc, "unknown": true}}}} {
			for _, kind := range []string{"create_cohort", "edit_cohort"} {
				args := map[string]any{}
				if kind == "create_cohort" {
					args["name"] = "schema"
				} else {
					args["cohort"] = k.ID
				}
				for key, v := range part {
					args[key] = v
				}
				r := f.call(kind, args)
				if !r.IsError() || len(decode(t, r).Structured) != 0 {
					t.Fatal(kind, args, decode(t, r))
				}
			}
		}
	}
	f.d.SetFailing(false)
	for _, kind := range []string{"cohort", "delete_cohort", "edit_cohort"} {
		refusal(t, f.call(kind, map[string]any{"cohort": unknown}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	for _, kind := range []string{"reject", "unreject"} {
		for _, args := range []tools.RejectArgs{{Cohort: unknown, Candidate: ids[0], Why: "reason"}, {Cohort: k.ID, Candidate: unknownCandidate, Why: "reason"}} {
			refusal(t, f.call(kind, args), passes.RefusalNotFound, tools.RefuseNotFound)
		}
	}
	for _, kind := range []string{"exclude", "include"} {
		refusal(t, f.call(kind, tools.RulingArgs{Candidate: unknownCandidate, Why: "reason"}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same"})
	for _, kind := range []string{"exclude", "include"} {
		refusal(t, f.call(kind, tools.RulingArgs{Candidate: ids[1], Why: "reason"}), passes.RefusalInvalid, tools.RefuseInvalid)
	}
}

// R-P76P-0947 R-VWN5-8SST
func TestEveryToolUnavailable(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	k := f.create("unavailable")
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	f.d.SetFailing(true)
	defer f.d.SetFailing(false)
	calls := []struct {
		name string
		args any
	}{{"cohorts", tools.CohortsArgs{}}, {"cohort", tools.CohortArgs{Cohort: k.ID}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: ids[0]}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: p.ID}}, {"rebuild", tools.NoArgs{}}, {"decide", tools.DecideArgs{Kind: store.KindNotSame, Candidate: ids[0], Other: ids[1], Why: "reason"}}, {"exclude", tools.RulingArgs{Candidate: ids[0], Why: "reason"}}, {"include", tools.RulingArgs{Candidate: ids[0], Why: "reason"}}, {"create_cohort", tools.CreateCohortArgs{Name: "valid"}}, {"edit_cohort", tools.EditCohortArgs{Cohort: k.ID}}, {"delete_cohort", tools.CohortArgs{Cohort: k.ID}}, {"reject", tools.RejectArgs{Cohort: k.ID, Candidate: ids[0], Why: "reason"}}, {"unreject", tools.RejectArgs{Cohort: k.ID, Candidate: ids[0], Why: "reason"}}}
	for _, call := range calls {
		refusal(t, f.call(call.name, call.args), passes.RefusalUnavailable, tools.RefuseUnavailable)
	}
}

func ptrInt(n int64) *int64 { return &n }

// R-P8EL-E0UW
func TestToolsContactNoSiblingOrStartPass(t *testing.T) {
	var requests atomic.Int64
	var fetched, prompted atomic.Int64
	f := fixtureWithPrompts(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNotFound) }), pageChecker(&fetched), promptClient(t, false, &prompted))
	ids := f.seed(2)
	k := f.create("local-only")
	setSite(t, f, ids[0], "https://acme.test/")
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct {
		name string
		args any
	}{
		{"cohorts", tools.CohortsArgs{}}, {"cohort", tools.CohortArgs{Cohort: k.ID}}, {"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{Candidate: ids[0]}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{Pass: p.ID}},
		{"cohorts", map[string]any{"page": "bad"}}, {"cohort", tools.CohortArgs{Cohort: "bad"}}, {"candidates", map[string]any{"limit": 0}}, {"candidate", tools.CandidateArgs{Candidate: "bad"}}, {"passes", map[string]any{"page": "bad"}}, {"pass", tools.PassArgs{Pass: "bad"}},
		{"add_page", tools.AddPageArgs{Candidate: ids[0], URL: "https://acme.test/about"}}, {"create_cohort", tools.CreateCohortArgs{Name: "another"}}, {"decide", tools.DecideArgs{Kind: store.KindNotSame, Candidate: ids[0], Other: ids[1], Why: "separate"}}, {"exclude", tools.RulingArgs{Candidate: ids[0], Why: "excluded"}}, {"include", tools.RulingArgs{Candidate: ids[0], Why: "included"}},
		{"edit_cohort", tools.EditCohortArgs{Cohort: k.ID}}, {"reject", tools.RejectArgs{Cohort: k.ID, Candidate: ids[0], Why: "reject"}}, {"unreject", tools.RejectArgs{Cohort: k.ID, Candidate: ids[0], Why: "restore"}}, {"delete_cohort", tools.CohortArgs{Cohort: k.ID}},
	} {
		f.call(call.name, call.args)
		if requests.Load() != 0 || prompted.Load() != 0 {
			t.Fatal("unexpected sibling request", call.name)
		}
		if call.name != "add_page" && fetched.Load() != 0 {
			t.Fatal("read fetched", call.name)
		}
		if call.name == "add_page" {
			if fetched.Load() != 1 {
				t.Fatal("page fetch missing")
			}
			fetched.Store(0)
		}
		ps, err := f.st.ListPasses(t.Context(), "", "", 100)
		if err != nil || len(ps) != 1 || ps[0].ID != p.ID {
			t.Fatal(call.name, ps, err)
		}
	}
}

// R-P76P-0947 R-6QOY-0DLL
func TestUnjudgedCandidateAndPrunedPass(t *testing.T) {
	f := newFixture(t)
	_, ids, err := f.st.Attach(t.Context(), "unjudged", false, []store.Observation{{Scraper: "fixture", SourceID: "unjudged", Digest: "chosen", Reading: sources.Reading{Name: "Unjudged", Names: []string{"Unjudged"}}}})
	if err != nil {
		t.Fatal(err)
	}
	r := f.call("candidate", tools.CandidateArgs{Candidate: ids[0]})
	detail := decodeList[tools.Detail](t, r)
	if detail.Assessment != nil || len(detail.Estimates) != 0 || len(detail.Links) != 1 || detail.Links[0].Filed != "" || detail.Links[0].FleetVerdict != sources.FleetUnknown {
		t.Fatal(detail)
	}
	var first string
	for range 2 {
		p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: caller.UserID})
		if err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = p.ID
		}
		if _, err = f.st.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(time.Second)
	}
	if _, err = f.st.PrunePasses(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call("pass", tools.PassArgs{Pass: first}), passes.RefusalNotFound, tools.RefuseNotFound)
}

func checkDefaultPage(t *testing.T, f *fixture, name string) {
	t.Helper()
	want := f.call(name, map[string]any{"limit": paging.DefaultLimit})
	for _, args := range []map[string]any{{}, {"limit": nil, "page": nil}, {"page": ""}} {
		got := f.call(name, args)
		if !reflect.DeepEqual(decode(t, want), decode(t, got)) {
			t.Fatal("default page differs", name, args, decode(t, got))
		}
	}
}

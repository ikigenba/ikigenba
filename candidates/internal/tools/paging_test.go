package tools_test

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-YDM6-8JN3 R-22J7-XJGS R-XFGZ-IYVN
func TestPagingTagsAndExactSchemas(t *testing.T) {
	pd, _ := json.Marshal(paging.PageDescription)
	ld, _ := json.Marshal(paging.LimitDescription)
	for _, value := range []any{tools.ProspectListsArgs{}, tools.CandidatesArgs{}, tools.PassesArgs{}} {
		typ := reflect.TypeOf(value)
		page, ok := typ.FieldByName("Page")
		if !ok || page.Type != reflect.TypeFor[*string]() || string(page.Tag) != fmt.Sprintf("json:%q description:%q", "page", paging.PageDescription) {
			t.Fatal("page declaration", page)
		}
		limit, ok := typ.FieldByName("Limit")
		if !ok || limit.Type != reflect.TypeFor[*int]() || string(limit.Tag) != fmt.Sprintf("json:%q description:%q", "limit", paging.LimitDescription) {
			t.Fatal("limit declaration", limit)
		}
		if page.Index[0] != typ.NumField()-2 || limit.Index[0] != typ.NumField()-1 {
			t.Fatal("argument member order", typ)
		}
	}
	for _, value := range []any{tools.ProspectListList{}, tools.CandidateList{}, tools.PassList{}} {
		field, ok := reflect.TypeOf(value).FieldByName("Next")
		if !ok || field.Type != reflect.TypeFor[*string]() || field.Tag.Get("description") != paging.NextDescription {
			t.Fatal("next declaration", field)
		}
	}
	f := newFixture(t)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf(`"page":{"type":"string","description":%s},"limit":{"type":"integer","description":%s}},"additionalProperties":false}`, pd, ld)
	want := map[string]string{
		"prospect_lists": `{"type":"object","properties":{` + suffix,
		"candidates":     `{"type":"object","properties":{"name":{"type":"string"},"zip":{"type":"string"},"phone":{"type":"string"},` + suffix,
		"passes":         `{"type":"object","properties":{` + suffix,
	}
	for _, info := range infos {
		if expected, ok := want[info.Name]; ok && string(info.InputSchema) != expected {
			t.Fatalf("%s schema got %s want %s", info.Name, info.InputSchema, expected)
		}
	}
	token := "opaque"
	for _, value := range []any{tools.ProspectListList{Next: &token}, tools.CandidateList{Next: &token}, tools.PassList{Next: &token}} {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if string(out["next"]) != `"opaque"` {
			t.Fatal("next not a string", string(b))
		}
	}
}

func textRefusal(t *testing.T, f *fixture, name string, args any, expected string) {
	t.Helper()
	r := f.call(name, args)
	w := decode(t, r)
	if !r.IsError() || len(w.Content) != 1 || w.Content[0].Type != "text" || w.Content[0].Text != expected || len(w.Structured) != 0 {
		t.Fatalf("%s %+v", name, w)
	}
}

// R-5T7C-OCFY R-WEW0-1UMT R-R5VK-REP6
func TestPagingRefusalsBeforeStoreAndSchema(t *testing.T) {
	f := newFixture(t)
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, name := range []string{"prospect_lists", "candidates", "passes"} {
			for _, limit := range []int{-1, 0, paging.MaxLimit + 1} {
				_, err := paging.Window(&limit)
				textRefusal(t, f, name, map[string]any{"limit": limit, "page": "bogus"}, err.Error())
			}
			textRefusal(t, f, name, map[string]any{"page": "bogus"}, paging.BadPage)
			var bad []string
			otherTool := "candidates"
			if name == otherTool {
				otherTool = "passes"
			}
			bad = []string{paging.Token{Tool: name}.Encode(int64(7), "id"), paging.Token{Tool: name}.Encode("time"), paging.Token{Tool: otherTool}.Encode("time", "id")}

			for _, token := range bad {
				textRefusal(t, f, name, map[string]any{"page": token}, paging.BadPage)
			}
			for _, args := range []map[string]any{{"page": 0}, {"page": false}, {"limit": "1"}, {"limit": 1.5}, {"limit": json.RawMessage(`9223372036854775808`)}} {
				r := f.call(name, args)
				if !r.IsError() || len(decode(t, r).Structured) != 0 {
					t.Fatal("schema accepted malformed paging", name, args)
				}
			}
		}
	}
	f.d.SetFailing(false)
	for _, name := range []string{"prospect_lists", "candidates", "passes"} {
		base := f.call(name, map[string]any{})
		for _, args := range []map[string]any{{"page": nil, "limit": nil}, {"page": ""}, {"limit": paging.DefaultLimit}} {
			if !reflect.DeepEqual(decode(t, base), decode(t, f.call(name, args))) {
				t.Fatal("absent/null/empty default differs", name, args)
			}
		}
	}
}

// R-DACX-6WCH
func TestStoredJudgmentsChangeOnlyAtJudge(t *testing.T) {
	f := newFixture(t)
	f.now = time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	ids := f.seed(1)
	links, err := f.st.Links(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	reading := links[0].Reading
	reading.Filed = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	reading.Estimate = nil
	reading.Evidence = []sources.Evidence{{Name: sources.EvidenceFMCSAActive, Source: "fmcsa", Weight: .6}, {Name: sources.EvidenceFMCSAActivity, Source: "fmcsa", Weight: .3}, {Name: sources.EvidenceOwnDomain, Source: "fmcsa", Weight: .2}}
	if _, _, err = f.st.Attach(t.Context(), "run-judged", true, []store.Observation{{Scraper: "fmcsa", SourceID: links[0].SourceID, Digest: "changed-evidence", Reading: reading}}); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2023, 7, 4, 0, 0, 0, 0, time.UTC)
	if err = walkJudgments(t.Context(), f.st, time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	first := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}))
	a, err := f.st.GetJudgments(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Judgments == nil || first.Judgments.OperatingConfidence == nil || *first.Judgments.OperatingConfidence != *a.Operating || *first.Judgments.OperatingConfidence != .776 || first.Judgments.RevenueLow != nil || first.Judgments.RevenueHigh != nil || first.Judgments.RevenueConfidence != nil {
		t.Fatal(first.Judgments)
	}
	if len(first.Links) != 1 || first.Links[0].Reason != store.LinkSourceKey || len(first.Judgments.OperatingEvidence) != 3 {
		t.Fatal(first)
	}
	for i, w := range []float64{.6, .3, .2} {
		if first.Judgments.OperatingEvidence[i].Name != reading.Evidence[i].Name || math.Abs(first.Judgments.OperatingEvidence[i].Weight-w) > 1e-9 {
			t.Fatal(first.Judgments)
		}
	}
	f.now = time.Date(2023, 7, 4, 0, 0, 0, 0, time.UTC)
	success(t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}), first)
	f.now = time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	if err = walkJudgments(t.Context(), f.st, time.Date(2023, 7, 4, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	updated := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}))
	a, err = f.st.GetJudgments(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if updated.Judgments == nil || updated.Judgments.OperatingConfidence == nil || *updated.Judgments.OperatingConfidence != *a.Operating || *updated.Judgments.OperatingConfidence != .607693 {
		t.Fatal(updated.Judgments)
	}
	activeFound := false
	var storedActive *sources.Evidence
	for i := range a.Evidence {
		if a.Evidence[i].Name == sources.EvidenceFMCSAActive {
			storedActive = &a.Evidence[i]
		}
	}
	for _, e := range updated.Judgments.OperatingEvidence {
		if e.Name == sources.EvidenceFMCSAActive {
			activeFound = true
			if storedActive == nil || math.Abs(e.Weight-.2994525547) > 1e-9 || e.Weight != storedActive.Weight {
				t.Fatal(e)
			}
		}
	}
	if !activeFound {
		t.Fatal("missing active evidence", updated.Judgments)
	}
	success(t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}), tools.Detail{Candidate: first.Candidate, Sources: first.Sources, Enrichments: first.Enrichments, Judgments: judgmentsExpected(a), Estimates: first.Estimates, Links: first.Links, Rulings: first.Rulings})
	reading.Evidence = nil
	if _, _, err = f.st.Attach(t.Context(), "run-no-evidence", true, []store.Observation{{Scraper: "fmcsa", SourceID: links[0].SourceID, Digest: "no-evidence", Reading: reading}}); err != nil {
		t.Fatal(err)
	}
	if err = walkJudgments(t.Context(), f.st, f.now); err != nil {
		t.Fatal(err)
	}
	result := decode(t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}))
	var detail struct {
		Judgments map[string]json.RawMessage `json:"judgments"`
	}
	if err := json.Unmarshal(result.Structured, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Judgments == nil || detail.Judgments["operating_confidence"] != nil || string(detail.Judgments["operating_evidence"]) != "[]" {
		t.Fatal(string(result.Structured))
	}
}

// R-YDM6-8JN3 R-22J7-XJGS R-XFGZ-IYVN
// R-0PR8-MPT0
func TestTiePagesAndFullFinalPage(t *testing.T) {
	for _, count := range []int{25, 26, 30} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := newFixture(t)
			ids := f.seedWithStep(count, 0, true)
			slices.Sort(ids)
			slices.Reverse(ids)
			for _, limit := range []int{1, 5, 25, 30} {
				var next *string
				seen := []string{}
				for {
					r := f.call("candidates", tools.CandidatesArgs{Page: next, Limit: &limit})
					got := decodeList[tools.CandidateList](t, r)
					success(t, r, got)
					if len(got.Candidates) != min(limit, len(ids)-len(seen)) {
						t.Fatal(got)
					}
					for _, c := range got.Candidates {
						seen = append(seen, c.ID)
					}
					if !reflect.DeepEqual(seen, ids[:len(seen)]) || (got.Next != nil) != (len(seen) < len(ids)) {
						t.Fatal(got, seen, ids)
					}
					if got.Next == nil {
						break
					}
					last := got.Candidates[len(got.Candidates)-1]
					if *got.Next != (paging.Token{Tool: "candidates"}).Encode(last.Created, last.ID) {
						t.Fatal(got.Next)
					}
					next = got.Next
				}
			}
		})
	}
}

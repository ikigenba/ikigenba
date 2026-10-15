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

// R-5N3U-RHQH R-5OBR-59H6 R-0FKQ-IWI5
func TestPagingTagsAndExactSchemas(t *testing.T) {
	pd, _ := json.Marshal(paging.PageDescription)
	ld, _ := json.Marshal(paging.LimitDescription)
	for _, value := range []any{tools.ShortlistArgs{}, tools.CandidatesArgs{}, tools.PassesArgs{}} {
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
	for _, value := range []any{tools.ShortlistPage{}, tools.CandidateList{}, tools.PassList{}} {
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
		"shortlist":  `{"type":"object","properties":{"max_minutes":{"type":"integer","minimum":0},"min_revenue":{"type":"integer","minimum":0},"class":{"type":"string"},"cargo":{"type":"string"},` + suffix,
		"candidates": `{"type":"object","properties":{"name":{"type":"string"},"zip":{"type":"string"},"phone":{"type":"string"},` + suffix,
		"passes":     `{"type":"object","properties":{` + suffix,
	}
	for _, info := range infos {
		if expected, ok := want[info.Name]; ok && string(info.InputSchema) != expected {
			t.Fatalf("%s schema got %s want %s", info.Name, info.InputSchema, expected)
		}
	}
	token := "opaque"
	for _, value := range []any{tools.ShortlistPage{Next: &token}, tools.CandidateList{Next: &token}, tools.PassList{Next: &token}} {
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

// R-5T7C-OCFY R-5UF9-246N R-5VN5-FVXC R-5Y2Y-7FEQ
func TestPagingRefusalsBeforeStoreAndSchema(t *testing.T) {
	f := newFixture(t)
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, name := range []string{"shortlist", "candidates", "passes"} {
			for _, limit := range []int{-1, 0, paging.MaxLimit + 1} {
				_, err := paging.Window(&limit)
				textRefusal(t, f, name, map[string]any{"limit": limit, "page": "bogus"}, err.Error())
			}
			textRefusal(t, f, name, map[string]any{"page": "bogus"}, paging.BadPage)
			var bad []string
			if name == "shortlist" {
				bad = []string{paging.Token{Tool: name}.Encode("7", "id"), paging.Token{Tool: name}.Encode(int64(7)), paging.Token{Tool: "candidates"}.Encode(int64(7), "id")}
			} else {
				otherTool := "candidates"
				if name == otherTool {
					otherTool = "passes"
				}
				bad = []string{paging.Token{Tool: name}.Encode(int64(7), "id"), paging.Token{Tool: name}.Encode("time"), paging.Token{Tool: otherTool}.Encode("time", "id")}
			}
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
	for _, name := range []string{"shortlist", "candidates", "passes"} {
		base := f.call(name, map[string]any{})
		for _, args := range []map[string]any{{"page": nil, "limit": nil}, {"page": ""}, {"limit": paging.DefaultLimit}} {
			if !reflect.DeepEqual(decode(t, base), decode(t, f.call(name, args))) {
				t.Fatal("absent/null/empty default differs", name, args)
			}
		}
	}
}

// R-N3IJ-G0NV R-0I0J-AFZJ
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
	if _, err = f.st.JudgeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	first := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}))
	a, err := f.st.GetAssessment(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Assessment == nil || first.Assessment.OperatingConfidence == nil || *first.Assessment.OperatingConfidence != *a.Operating || math.Abs(*first.Assessment.OperatingConfidence-.776) > 1e-9 || first.Assessment.RevenueLow != nil || first.Assessment.RevenueHigh != nil || first.Assessment.RevenueConfidence != nil {
		t.Fatal(first.Assessment)
	}
	for i, w := range []float64{.6, .3, .2} {
		if first.Assessment.OperatingEvidence[i].Name != reading.Evidence[i].Name || math.Abs(first.Assessment.OperatingEvidence[i].Weight-w) > 1e-9 {
			t.Fatal(first.Assessment)
		}
	}
	f.now = time.Date(2023, 7, 4, 0, 0, 0, 0, time.UTC)
	success(t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}), first)
	if _, err = f.st.JudgeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	updated := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}))
	a, err = f.st.GetAssessment(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if updated.Assessment == nil || updated.Assessment.OperatingConfidence == nil || *updated.Assessment.OperatingConfidence != *a.Operating || math.Abs(*updated.Assessment.OperatingConfidence-.6076934307) > 1e-9 {
		t.Fatal(updated.Assessment)
	}
	for _, e := range updated.Assessment.OperatingEvidence {
		if e.Name == sources.EvidenceFMCSAActive {
			if math.Abs(e.Weight-.2994525547) > 1e-9 {
				t.Fatal(e)
			}
		}
	}
	success(t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}), tools.Detail{Candidate: first.Candidate, Assessment: assessmentExpected(a), Estimates: first.Estimates, Links: first.Links, Rulings: first.Rulings})
	list := decodeList[tools.ShortlistPage](t, f.call("shortlist", tools.ShortlistArgs{}))
	if len(list.Shortlist) != 1 || *list.Shortlist[0].OperatingConfidence != *updated.Assessment.OperatingConfidence {
		t.Fatal(list)
	}
}

// R-0KGC-1ZGX R-0MW4-TIYB R-0J8F-O7Q8 R-5UF9-246N
func TestTiePagesAndFullFinalPage(t *testing.T) {
	for _, count := range []int{25, 26, 30} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := newFixture(t)
			ids := f.seedWithStep(count, 0, true)
			ordered := slices.Clone(ids)
			slices.Sort(ordered)
			for _, name := range []string{"shortlist", "candidates"} {
				expected := slices.Clone(ordered)
				if name == "candidates" {
					slices.Reverse(expected)
				}
				for _, limit := range []int{0, 5, 25, 30} {
					args := map[string]any{}
					z := paging.DefaultLimit
					if limit > 0 {
						args["limit"] = limit
						z = limit
					}
					got := []string{}
					for {
						result := f.call(name, args)
						var pageIDs []string
						var next *string
						if name == "shortlist" {
							page := decodeList[tools.ShortlistPage](t, result)
							next = page.Next
							for _, e := range page.Shortlist {
								pageIDs = append(pageIDs, e.Candidate.ID)
							}
							success(t, result, page)
						} else {
							page := decodeList[tools.CandidateList](t, result)
							next = page.Next
							for _, c := range page.Candidates {
								pageIDs = append(pageIDs, c.ID)
							}
							success(t, result, page)
						}
						remaining := len(expected) - len(got)
						if len(pageIDs) != min(z, remaining) {
							t.Fatalf("%s count %d limit %d page size %d remaining %d", name, count, limit, len(pageIDs), remaining)
						}
						got = append(got, pageIDs...)
						if !reflect.DeepEqual(got, expected[:len(got)]) {
							t.Fatalf("%s order got %v want %v", name, got, expected)
						}
						if (next != nil) != (len(got) < len(expected)) {
							t.Fatal("next presence at full final page", name, next)
						}
						if next == nil {
							break
						}
						// Assert each token encodes the last answered element, including the exact score key.
						lastID := got[len(got)-1]
						var want string
						if name == "shortlist" {
							a, err := f.st.GetAssessment(t.Context(), lastID)
							if err != nil {
								t.Fatal(err)
							}
							want = paging.Token{Tool: name}.Encode(a.OperatingKey, lastID)
						} else {
							c, err := f.st.GetCandidate(t.Context(), lastID)
							if err != nil {
								t.Fatal(err)
							}
							want = paging.Token{Tool: name}.Encode(stamp(c.Created), lastID)
						}
						if *next != want {
							t.Fatal("token position differs", *next, want)
						}
						args["page"] = *next
					}
					if !reflect.DeepEqual(got, expected) {
						t.Fatal("missing entries", name, got, expected)
					}
				}
			}
		})
	}
}

// R-0KGC-1ZGX R-0J8F-O7Q8
func TestMixedOperatingOrderAndClass(t *testing.T) {
	f := newFixture(t)
	weights := []float64{.2, -1, .6, .8, -1, .6}
	ids := make([]string, len(weights))
	for i, w := range weights {
		name := fmt.Sprintf("mixed-operating-%d", i)
		reading := sources.Reading{Name: name, Names: []string{name}, NameKey: name}
		if i%2 == 0 {
			reading.Class = sources.ClassForHire
		}
		if w >= 0 {
			reading.Evidence = []sources.Evidence{{Name: sources.EvidenceOwnDomain, Source: "mixed", Weight: w}}
		}
		_, created, err := f.st.Attach(t.Context(), "run-mixed", false, []store.Observation{{Scraper: "mixed", SourceID: name, Digest: name, Reading: reading}})
		if err != nil || len(created) != 1 {
			t.Fatal(created, err)
		}
		ids[i] = created[0]
	}
	if _, err := f.st.JudgeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	order := []int{3, 2, 5, 0, 1, 4}
	if ids[2] > ids[5] {
		order[1], order[2] = order[2], order[1]
	}
	if ids[1] > ids[4] {
		order[4], order[5] = order[5], order[4]
	}
	for _, class := range []string{"", sources.ClassForHire} {
		expected := []string{}
		for _, i := range order {
			if class == "" || i%2 == 0 {
				expected = append(expected, ids[i])
			}
		}
		for _, limit := range []int{1, 2, 3, 25} {
			var token *string
			seen := []string{}
			for steps := 0; ; steps++ {
				if steps > len(ids) {
					t.Fatal("walk did not terminate")
				}
				result := f.call("shortlist", tools.ShortlistArgs{Class: class, Page: token, Limit: &limit})
				got := decodeList[tools.ShortlistPage](t, result)
				success(t, result, got)
				start := len(seen)
				if len(got.Shortlist) != min(limit, len(expected)-start) {
					t.Fatal("window size", got)
				}
				for _, e := range got.Shortlist {
					seen = append(seen, e.Candidate.ID)
					if e.Candidate.ID != expected[len(seen)-1] {
						t.Fatalf("class=%q limit=%d order got %v want %v", class, limit, seen, expected)
					}
					index := slices.Index(ids, e.Candidate.ID)
					if weights[index] < 0 {
						if e.OperatingConfidence != nil {
							t.Fatal("unknown confidence", e)
						}
					} else if e.OperatingConfidence == nil || math.Abs(*e.OperatingConfidence-weights[index]) > 1e-9 {
						t.Fatal("stored confidence", e)
					}
				}
				if (got.Next != nil) != (len(seen) < len(expected)) {
					t.Fatal("continuation", got)
				}
				if got.Next == nil {
					break
				}
				token = got.Next
			}
			if !reflect.DeepEqual(seen, expected) {
				t.Fatal(seen, expected)
			}
		}
	}
}

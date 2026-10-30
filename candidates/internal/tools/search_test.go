package tools_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-7H21-LWH0 R-7N5J-IR6H
func TestSearchSchema(t *testing.T) {
	_ = tools.SearchArgs{"query", nil}
	f := newFixture(t)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name == "search" {
			want := `{"type":"object","properties":{"q":{"type":"string"},"limit":{"type":"integer"}},"required":["q"],"additionalProperties":false}`
			if string(info.InputSchema) != want || info.OutputSchema != nil {
				t.Fatalf("search schemas: %s / %s", info.InputSchema, info.OutputSchema)
			}
			return
		}
	}
	t.Fatal("search not registered")
}

// R-7I9X-ZO7P
func TestSearchMatchEncoding(t *testing.T) {
	c := tools.Candidate{ID: "chosen-id", Name: "Chosen name", Names: []string{"Chosen name", "Alternate"}}
	for _, v := range []tools.Match{{tools.Candidate{}, "", 0}, {c, "Alternate", .625}} {
		candidate, err := json.Marshal(v.Candidate)
		if err != nil {
			t.Fatal(err)
		}
		matched, err := json.Marshal(v.Matched)
		if err != nil {
			t.Fatal(err)
		}
		score, err := json.Marshal(v.Score)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"candidate":` + string(candidate) + `,"matched":` + string(matched) + `,"score":` + string(score) + `}`
		b, err := json.Marshal(v)
		if err != nil || string(b) != want {
			t.Fatal(string(b), want, err)
		}
		b, err = json.Marshal(tools.MatchList{[]tools.Match{v}})
		if err != nil || string(b) != `{"matches":[`+want+`]}` {
			t.Fatal(string(b), err)
		}
	}
	for _, v := range []tools.MatchList{{nil}, {[]tools.Match{}}} {
		b, err := json.Marshal(v)
		if err != nil || string(b) != `{"matches":[]}` {
			t.Fatal(string(b), err)
		}
	}
}

// R-8WFG-C584
func TestSearchMapsStoreResults(t *testing.T) {
	f := newFixture(t)
	f.seed(store.SearchDefaultLimit + 4)
	listing := decodeList[tools.CandidateList](t, f.call("candidates", map[string]any{"limit": 500}))
	byID := map[string]json.RawMessage{}
	for _, c := range listing.Candidates {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		byID[c.ID] = b
	}
	for _, q := range []string{"  BUSINESS ", "trucking alias", "busniess", "no-result-zzzzzzzz"} {
		for _, limit := range []*int{nil, new(1), new(3), new(store.SearchMaxLimit)} {
			l := store.SearchDefaultLimit
			if limit != nil {
				l = *limit
			}
			ms, err := f.st.Search(t.Context(), q, l)
			if err != nil {
				t.Fatal(err)
			}
			want := tools.MatchList{Matches: []tools.Match{}}
			for _, m := range ms {
				want.Matches = append(want.Matches, tools.Match{Candidate: candidateOf(m.Candidate), Matched: m.Matched, Score: m.Score})
			}
			args := map[string]any{"q": q}
			if limit != nil {
				args["limit"] = *limit
			}
			r := f.call("search", args)
			success(t, r, want)
			if limit == nil {
				args["limit"] = nil
				success(t, f.call("search", args), want)
			}
			got := decodeList[tools.MatchList](t, r)
			for _, m := range got.Matches {
				b, err := json.Marshal(m.Candidate)
				if err != nil || string(b) != string(byID[m.Candidate.ID]) {
					t.Fatal(string(b), string(byID[m.Candidate.ID]), err)
				}
			}
		}
	}
	all, err := f.st.Search(t.Context(), "business", store.SearchMaxLimit)
	if err != nil || len(all) <= store.SearchDefaultLimit {
		t.Fatal("fixture does not distinguish default and max limits", len(all), err)
	}
	success(t, f.call("search", tools.SearchArgs{Q: "zzzzzzzz-no-match"}), tools.MatchList{})
}

func schemaRefusal(t *testing.T, r mcp.Result) {
	t.Helper()
	w := decode(t, r)
	if !r.IsError() || len(w.Content) != 1 || len(w.Structured) != 0 {
		t.Fatal("expected schema refusal", w)
	}
	for _, word := range []string{passes.RefusalBusy, passes.RefusalInvalid, passes.RefusalNotFound, passes.RefusalStopping, passes.RefusalUnavailable, passes.RefusalUnreadable} {
		if w.Content[0].Text == word || strings.HasPrefix(w.Content[0].Text, word+":") {
			t.Fatal("schema refusal used handler word", w)
		}
	}
}

// R-8XNC-PWYT R-7T91-FLVY
func TestSearchRefusesBeforeStore(t *testing.T) {
	f := newFixture(t)
	f.d.SetFailing(true)
	defer f.d.SetFailing(false)
	for _, q := range []string{"", "   ", "\t\n", "\u00a0"} {
		refusal(t, f.call("search", tools.SearchArgs{Q: q}), passes.RefusalInvalid, tools.RefuseInvalid)
	}
	for _, l := range []int{0, -1, store.SearchMaxLimit + 1, 500} {
		refusal(t, f.call("search", tools.SearchArgs{Q: "trujillo", Limit: &l}), passes.RefusalInvalid, tools.RefuseInvalid)
	}
	for _, l := range []int{1, store.SearchMaxLimit} {
		refusal(t, f.call("search", tools.SearchArgs{Q: "trujillo", Limit: &l}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	}
	schemaRefusal(t, f.call("search", map[string]any{}))
}

// R-7LXN-4ZFS
func TestSearchLeavesRecordsAndIgnoresCaller(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(3)
	f.create("search-record-snapshot")
	snapshot := func() []any {
		t.Helper()
		out := []any{}
		for _, id := range ids {
			c, err := f.st.GetCandidate(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
		cs, err := f.st.ListCandidates(t.Context(), store.CandidateFilter{}, "", "", 500)
		if err != nil {
			t.Fatal(err)
		}
		ps, err := f.st.ListPasses(t.Context(), "", "", 500)
		if err != nil {
			t.Fatal(err)
		}
		ls, err := f.st.ListProspectLists(t.Context(), "", "", 500)
		if err != nil {
			t.Fatal(err)
		}
		ms, err := f.st.Search(t.Context(), "business", store.SearchMaxLimit)
		if err != nil {
			t.Fatal(err)
		}
		return append(out, cs, ps, ls, ms)
	}
	before := snapshot()
	for _, args := range []tools.SearchArgs{{Q: "business"}, {Q: "no-matches-zzzzzzzz"}, {Q: ""}, {Q: "business", Limit: new(0)}} {
		a, b := f.callAs(caller, "search", args), f.callAs(other, "search", args)
		if !reflect.DeepEqual(decode(t, a), decode(t, b)) {
			t.Fatal("caller changed search answer", decode(t, a), decode(t, b))
		}
		if after := snapshot(); !reflect.DeepEqual(before, after) {
			t.Fatal("search changed records", before, after)
		}
	}
	f.d.SetFailing(true)
	refusal(t, f.call("search", tools.SearchArgs{Q: "business"}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	f.d.SetFailing(false)
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("failed search changed records", before, after)
	}
}

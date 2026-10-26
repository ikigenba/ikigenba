package tools_test

import (
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-KVN5-17PL R-ZKAN-MILE R-QWZN-Q90D
func TestSearchAnswersUseStoredResults(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	if _, _, err := f.st.Derive(t.Context(), enrichers.KindLinkFacts, "", false, 100); err != nil {
		t.Fatal(err)
	}
	co := f.create("search-prospect_list")
	read := func() tools.Detail {
		t.Helper()
		r := f.call("candidate", tools.CandidateArgs{Candidate: id})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		return decodeList[tools.Detail](t, r)
	}
	initial := read()
	if initial.Enrichments.Facts == nil || initial.Enrichments.Facts.Website != sources.WebsiteUnknown || initial.Enrichments.Facts.WebsiteSource != "" || len(initial.Enrichments.Facts.Written) != 1 {
		t.Fatal(initial)
	}
	zero := 0
	good := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "chosen-run", Status: enrichers.RunExited, ExitCode: &zero}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://acme.test/", Why: "chosen-why", Rejected: []enrichers.WebsiteSearchSite{{URL: "https://other.test/", Why: "chosen-one"}, {URL: "https://third.test/", Why: "chosen-two"}}}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://www.acme.test/", Title: "chosen-title", StatusCode: 200}}
	failed := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "failed-run", Status: "failed"}}
	noSite := good
	noSite.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone, Error: "chosen-error"}
	empty := good
	empty.Answer = &enrichers.WebsiteSearchAnswer{}
	empty.Confirmation = nil
	for i, x := range []enrichers.WebsiteSearchFound{failed, good, noSite, empty} {
		f.now = f.now.Add(time.Hour)
		input := store.Enrichment{Candidate: id, Kind: enrichers.KindWebsiteSearch, Source: enrichers.SourcePrompts, Inputs: "chosen-input", Found: encodedJSON(x), Cost: .023714}
		if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{input}); err != nil {
			t.Fatal(err)
		}
		got := read()
		a := got.Enrichments.Facts
		if a.Written[len(a.Written)-1].At != stamp(f.now) {
			t.Fatal(a)
		}
		source, state, url := "", sources.WebsiteUnknown, ""
		// Later unsuccessful searches keep the last confirmed search.
		if i >= 1 {
			source, state, url = store.WebsiteBySearch, sources.WebsiteHTTPS, good.Confirmation.URL
		}
		if a.WebsiteSource != source || a.Website != state || a.WebsiteURL != url {
			t.Fatal(i, a)
		}
		e := got.EnrichmentResults[0]
		if e.Kind != input.Kind || e.Source != input.Source || e.Inputs != input.Inputs || e.Cost != input.Cost || !reflect.DeepEqual(e.Found, input.Found) {
			t.Fatal(e, input)
		}
		encoded, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(objectKeys(t, encoded), []string{"kind", "source", "inputs", "found", "cost", "at"}) {
			t.Fatal(string(encoded))
		}
		m := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: co.ID, Peek: true})).Members[0]
		if m.WebsiteSource != source || m.Website != state || m.WebsiteURL != url {
			t.Fatal(m)
		}
	}
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: enrichers.KindSiteText, Inputs: "chosen-input", Source: enrichers.SourcePrompts, Found: json.RawMessage(`{"run":"text-run","status":"failed","pages":0,"cost_nanos":0}`)}}); err != nil {
		t.Fatal(err)
	}
	got := read()
	if got.EnrichmentResults[0].Kind != enrichers.KindSiteText || got.EnrichmentResults[1].Kind != enrichers.KindWebsiteSearch {
		t.Fatal(got.EnrichmentResults)
	}
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "chosen-input", Source: "check.test", Found: json.RawMessage(`{"state":"http","url":"http://check.test/"}`)}}); err != nil {
		t.Fatal(err)
	}
	got = read()
	if got.Enrichments.Facts.WebsiteSource != store.WebsiteByCheck || got.Enrichments.Facts.Website != sources.WebsiteHTTP || got.Enrichments.Facts.WebsiteURL != "http://check.test/" {
		t.Fatal(got.Enrichments.Facts)
	}
	m := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: co.ID, Peek: true})).Members[0]
	if m.WebsiteSource != store.WebsiteByCheck || m.Website != sources.WebsiteHTTP || m.WebsiteURL != "http://check.test/" {
		t.Fatal(m)
	}
}

// R-SUUJ-VXRW
func TestWebsiteSearchEnrichDispatch(t *testing.T) {
	for _, reject := range []bool{false, true} {
		var calls atomic.Int64
		pc := promptClient(t, reject, &calls, enrichers.KindWebsiteSearch)
		var fetched atomic.Int64
		f := fixtureWithPrompts(t, nil, pageChecker(&fetched), pc)
		id := f.seed(1)[0]
		co := f.create("search-target")
		for _, a := range []tools.EnrichArgs{{Kind: enrichers.KindWebsiteSearch, ProspectList: co.ID}, {Kind: enrichers.KindWebsiteSearch, Candidate: id}} {
			r := f.callAs(other, "enrich", a)
			if reject {
				refusal(t, r, passes.RefusalInvalid, passes.NoPrompt+" "+enrichers.PromptName(enrichers.KindWebsiteSearch)+": chosen refusal")
				continue
			}
			if r.IsError() {
				t.Fatal(decode(t, r))
			}
			p := decodeList[passes.Record](t, r)
			target := a.ProspectList
			if target == "" {
				target = a.Candidate
			}
			if p.Kind != store.KindEnrichTarget || p.Target != target || p.EnrichKind != enrichers.KindWebsiteSearch || p.Status != store.StatusRunning || p.User != other.UserID {
				t.Fatal(p)
			}
		}
		if reject {
			ps, err := f.st.ListPasses(t.Context(), "", "", 100)
			if err != nil || len(ps) != 0 {
				t.Fatal(ps, err)
			}
		}
	}
	var calls atomic.Int64
	noChecker := fixtureWithPrompts(t, nil, nil, promptClient(t, false, &calls, enrichers.KindWebsiteSearch))
	co := noChecker.create("no-checker")
	refusal(t, noChecker.call("enrich", tools.EnrichArgs{Kind: enrichers.KindWebsiteSearch, ProspectList: co.ID}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
	var fetched atomic.Int64
	noPrompts := fixtureWithWebsite(t, nil, pageChecker(&fetched))
	co = noPrompts.create("no-prompts")
	refusal(t, noPrompts.call("enrich", tools.EnrichArgs{Kind: enrichers.KindWebsiteSearch, ProspectList: co.ID}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	noPrompts.d.SetFailing(true)
	refusal(t, noPrompts.call("enrich", tools.EnrichArgs{Kind: enrichers.KindWebsiteSearch}), passes.RefusalInvalid, tools.RefuseInvalid)
	noPrompts.d.SetFailing(false)
	for _, f := range []*fixture{noChecker, noPrompts} {
		ps, err := f.st.ListPasses(t.Context(), "", "", 100)
		if err != nil || len(ps) != 0 {
			t.Fatal(ps, err)
		}
	}
}

// R-KVN5-17PL R-KT7C-9O87
func TestWebsiteDecisionAnswersDistinguishUncheckedAndUnfound(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	all := f.create("website decisions")
	none := []string{sources.WebsiteNone}
	r := f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "unfound sites", Filters: &tools.FilterArgs{Categories: []string{}, Website: &none}})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	unfound := decodeList[tools.ProspectList](t, r)
	https := []string{sources.WebsiteHTTPS}
	r = f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "unknown or secure sites", Filters: &tools.FilterArgs{Categories: []string{}, Website: &https, WebsiteUnknown: true}})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	unknownOrSecure := decodeList[tools.ProspectList](t, r)
	search := func(url string, confirmation *enrichers.WebsiteSearchConfirmation) enrichers.WebsiteSearchFound {
		return enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "decision-run", Status: enrichers.RunExited, ExitCode: new(0)}, Answer: &enrichers.WebsiteSearchAnswer{URL: url, Why: "decision-reason"}, Confirmation: confirmation}
	}
	failed := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "failed-decision-run", Status: "failed"}}
	for _, tc := range []struct {
		name, kind, state, source, url string
		found                          any
	}{
		{name: "check found nothing", kind: sources.WebsiteCheck, found: map[string]string{"state": sources.WebsiteNone}, state: sources.WebsiteUnknown},
		{name: "failed search is unknown", kind: enrichers.KindWebsiteSearch, found: failed, state: sources.WebsiteUnknown},
		{name: "unconfirmed answer is unknown", kind: enrichers.KindWebsiteSearch, found: search("https://unconfirmed.test/", nil), state: sources.WebsiteUnknown},
		{name: "empty answer proves unfound", kind: enrichers.KindWebsiteSearch, found: search("", nil), state: sources.WebsiteNone},
		{name: "failed search preserves unfound", kind: enrichers.KindWebsiteSearch, found: failed, state: sources.WebsiteNone},
		{name: "unconfirmed answer preserves unfound", kind: enrichers.KindWebsiteSearch, found: search("https://unconfirmed.test/", nil), state: sources.WebsiteNone},
		{name: "confirmation found nothing", kind: enrichers.KindWebsiteSearch, found: search("https://missing.test/", &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone}), state: sources.WebsiteNone},
		{name: "check finds site", kind: sources.WebsiteCheck, found: map[string]string{"state": sources.WebsiteHTTPS, "url": "https://checked.test/"}, state: sources.WebsiteHTTPS, source: store.WebsiteByCheck, url: "https://checked.test/"},
		{name: "new check returns to unfound search", kind: sources.WebsiteCheck, found: map[string]string{"state": sources.WebsiteNone}, state: sources.WebsiteNone},
		{name: "confirmed search finds site", kind: enrichers.KindWebsiteSearch, found: search("https://chosen.test/", &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://confirmed.test/"}), state: sources.WebsiteHTTPS, source: store.WebsiteBySearch, url: "https://confirmed.test/"},
		{name: "later check keeps confirmed search", kind: sources.WebsiteCheck, found: map[string]string{"state": sources.WebsiteNone}, state: sources.WebsiteHTTPS, source: store.WebsiteBySearch, url: "https://confirmed.test/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.now = f.now.Add(time.Second)
			if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: tc.kind, Inputs: "decision-input", Found: encodedJSON(tc.found)}}); err != nil {
				t.Fatal(err)
			}
			r := f.call("candidate", tools.CandidateArgs{Candidate: id})
			if r.IsError() {
				t.Fatal(decode(t, r))
			}
			detail := decodeList[tools.Detail](t, r)
			facts := detail.Enrichments.Facts
			if facts == nil || facts.Website != tc.state || facts.WebsiteSource != tc.source || facts.WebsiteURL != tc.url {
				t.Fatal(facts, tc)
			}
			members := peekBasis(t, f, all.ID).Members
			if len(members) != 1 || members[0].Website != tc.state || members[0].WebsiteSource != tc.source || members[0].WebsiteURL != tc.url {
				t.Fatal(members, tc)
			}
			wantNone := int64(0)
			if tc.state == sources.WebsiteNone {
				wantNone = 1
			}
			if got := peekBasis(t, f, unfound.ID); got.Matching != wantNone || int64(len(got.Members)) != wantNone {
				t.Fatal(got, tc)
			}
			if got := peekBasis(t, f, unknownOrSecure.ID); got.Matching != 1-wantNone || int64(len(got.Members)) != 1-wantNone {
				t.Fatal(got, tc)
			}
		})
	}
}

package tools_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-RLP0-M6KF R-RMWW-ZYB4
func TestWalkSchema(t *testing.T) {
	_ = tools.WalkArgs{[]string{"chosen-park"}, true}
	f := newFixture(t)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name == "walk" {
			if string(info.InputSchema) != `{"type":"object","properties":{"parks":{"type":"array","items":{"type":"string"}},"again":{"type":"boolean"}},"required":["parks"],"additionalProperties":false}` {
				t.Fatal(string(info.InputSchema))
			}
			return
		}
	}
	t.Fatal("missing walk")
}

// R-RO4T-DQ1T R-RPCP-RHSI
func TestWalkValueJSON(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{
		{tools.ParkWalk{"pass", "at", 1, 2, 3, 4, 5, true}, `{"pass":"pass","walked":"at","circles":1,"requests":2,"found":3,"new_candidates":4,"cost":5,"ceiling_reached":true}`},
		{tools.ParkWalk{}, `{"pass":"","walked":"","circles":0,"requests":0,"found":0,"new_candidates":0,"cost":0,"ceiling_reached":false}`},
		{tools.Place{"id", "name", "address", "type", "website", "phone", "walk", "candidate", true}, `{"place_id":"id","name":"name","address":"address","primary_type":"type","website":"website","phone":"phone","walk":"walk","candidate":"candidate","new":true}`},
		{tools.Place{}, `{"place_id":"","name":"","address":"","primary_type":"","website":"","phone":"","walk":"","candidate":"","new":false}`},
		{tools.FoundCandidate{"id", "name", "status", true}, `{"id":"id","name":"name","status":"status","new":true}`},
		{tools.FoundCandidate{}, `{"id":"","name":"","status":"","new":false}`},
		{tools.ParkRef{"id", "name"}, `{"id":"id","name":"name"}`},
		{tools.ParkRef{}, `{"id":"","name":""}`},
		{tools.ParkWalkRef{"park", "name", 1, 2, 3, 4, true}, `{"park":"park","name":"name","found":1,"new_candidates":2,"requests":3,"cost":4,"ceiling_reached":true}`},
		{tools.ParkWalkRef{}, `{"park":"","name":"","found":0,"new_candidates":0,"requests":0,"cost":0,"ceiling_reached":false}`},
	} {
		got, err := json.Marshal(tc.v)
		if err != nil || string(got) != tc.want {
			t.Fatalf("%T: %s %v want %s", tc.v, got, err, tc.want)
		}
	}
	p := passes.Record{ID: "chosen-pass"}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, refs := range [][]tools.ParkWalkRef{nil, {}, {{"park", "name", 1, 2, 3, 4, true}}} {
		got, err := json.Marshal(tools.PassDetail{p, refs})
		want := string(b)
		if refs != nil {
			a, _ := json.Marshal(refs)
			want = strings.TrimSuffix(want, "}") + `,"parks":` + string(a) + `}`
		}
		if err != nil || string(got) != want {
			t.Fatal(string(got), want, err)
		}
	}
}

// R-OS29-TGRD R-OVPY-YRZG R-OWXV-CJQ5 R-FOXS-CLZS R-OZDO-437J R-R3EI-VMG0
func TestWalkReadProjections(t *testing.T) {
	f := newFixture(t)
	ps := seedParks(t, f, 2)
	empty := decodeList[tools.ParkDetail](t, f.call("park", tools.ParkArgs{ps[0].ID}))
	if empty.Walks == nil || len(empty.Walks) != 0 || empty.Places == nil || len(empty.Places) != 0 || empty.FoundCandidates == nil || len(empty.FoundCandidates) != 0 || empty.Park.NewCandidates != 0 || empty.Park.CeilingReached {
		t.Fatal(empty)
	}
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	// A walk with no recorded park still carries an empty parks array.
	success(t, f.call("pass", tools.PassArgs{p.ID}), tools.PassDetail{passes.RecordOf(p), []tools.ParkWalkRef{}})
	for i, k := range ps {
		f.now = f.now.Add(time.Minute)
		found := []places.Found{}
		for j := range 2 {
			suffix := k.ID + string(rune('a'+j))
			found = append(found, places.Found{Place: places.Place{ID: "place-" + suffix, Name: "business-" + suffix, Address: "12 Main, Town, MN 55001, USA", PrimaryType: "manufacturer", Website: "https://business-" + suffix + ".test", Phone: "555123" + string(rune('0'+i)) + string(rune('0'+j))}, Circle: places.Circle{Lat: k.Lat, Lon: k.Lon, Radius: k.Radius}})
		}
		_, err := f.st.RecordSweep(t.Context(), store.Sweep{Pass: p.ID, Park: k.ID, Found: found, Circles: 2, Requests: 3, Cost: 3 * places.PriceNanos, CeilingReached: i == 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	ended, err := f.st.FinishWalk(t.Context(), p.ID, store.Counts{ParksWalked: 2, Circles: 4, Requests: 6, Found: 4, NewCandidates: 4, Cost: 6 * places.PriceNanos}, true)
	if err != nil {
		t.Fatal(err)
	}
	refs := []tools.ParkWalkRef{}
	walks, err := f.st.PassWalks(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range walks {
		refs = append(refs, tools.ParkWalkRef{w.Park, w.Name, w.Found, w.NewCandidates, w.Requests, w.Cost, w.CeilingReached})
	}
	success(t, f.call("pass", tools.PassArgs{p.ID}), tools.PassDetail{passes.RecordOf(ended), refs})
	// Give every park two distinct walk rows, with its places still linked to p.
	second, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range ps {
		f.now = f.now.Add(time.Minute)
		if _, err := f.st.RecordSweep(t.Context(), store.Sweep{Pass: second.ID, Park: k.ID, Circles: 1, Requests: 1, Cost: places.PriceNanos}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.st.FinishWalk(t.Context(), second.ID, store.Counts{ParksWalked: 2, Circles: 2, Requests: 2, Cost: 2 * places.PriceNanos}, false); err != nil {
		t.Fatal(err)
	}
	for _, k := range ps {
		got := decodeList[tools.ParkDetail](t, f.call("park", tools.ParkArgs{k.ID}))
		ws, e := f.st.ParkWalks(t.Context(), k.ID)
		if e != nil {
			t.Fatal(e)
		}
		xs, e := f.st.ParkPlaces(t.Context(), k.ID)
		if e != nil {
			t.Fatal(e)
		}
		fs, e := f.st.ParkCandidates(t.Context(), k.ID)
		if e != nil {
			t.Fatal(e)
		}
		want := tools.ParkDetail{Park: parkAnswer(mustPark(t, f, k.ID)), Walks: []tools.ParkWalk{}, Places: []tools.Place{}, FoundCandidates: []tools.FoundCandidate{}, Parcels: got.Parcels}
		for _, w := range ws {
			want.Walks = append(want.Walks, tools.ParkWalk{w.Pass, stamp(w.Walked), w.Circles, w.Requests, w.Found, w.NewCandidates, w.Cost, w.CeilingReached})
		}
		for _, x := range xs {
			want.Places = append(want.Places, tools.Place{x.PlaceID, x.Name, x.Address, x.PrimaryType, x.Website, x.Phone, x.Walk, x.Candidate, x.New})
		}
		for _, x := range fs {
			want.FoundCandidates = append(want.FoundCandidates, tools.FoundCandidate{x.ID, x.Name, x.Status, x.New})
		}
		if len(ws) != 2 || len(xs) != 2 || len(fs) != 2 || !reflect.DeepEqual(got.Walks, want.Walks) || !reflect.DeepEqual(got.Places, want.Places) || !reflect.DeepEqual(got.FoundCandidates, want.FoundCandidates) {
			t.Fatal(got, want)
		}
		success(t, f.call("park", tools.ParkArgs{k.ID}), want)
		d := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{xs[0].Candidate}))
		if d.Park == nil || *d.Park != (tools.ParkRef{k.ID, k.Name}) || len(d.Links) != 1 || d.Links[0].Scraper != places.Name || d.Links[0].Run != "" || d.Links[0].Walk != p.ID || d.Links[0].Park != k.ID {
			t.Fatal(d)
		}
	}
	ids := f.seed(2)
	plain := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{ids[0]}))
	if plain.Park != nil || plain.Links[0].Walk != "" || plain.Links[0].Park != "" {
		t.Fatal(plain)
	}
	found, err := f.st.ParkPlaces(t.Context(), ps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	merged := found[0].Candidate
	r := f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[1], Other: merged, Why: "chosen merge"})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	d := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{merged}))
	if d.Park != nil {
		t.Fatal(d)
	}
	// A pruned pass is refused, while malformed IDs bypass a failing store.
	newer, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.FinishPass(t.Context(), newer.ID, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.PrunePasses(t.Context(), 1, 500); err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call("pass", tools.PassArgs{p.ID}), passes.RefusalNotFound, tools.RefuseNotFound)
	f.d.SetFailing(true)
	refusal(t, f.call("pass", tools.PassArgs{"bogus"}), passes.RefusalNotFound, tools.RefuseNotFound)
}
func mustPark(t *testing.T, f *fixture, id string) store.Park {
	t.Helper()
	p, e := f.st.GetPark(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

// R-1I57-LRF6
func TestWalkToolDelegates(t *testing.T) {
	noKey := newFixture(t)
	k := seedParks(t, noKey, 1)[0]
	refusal(t, noKey.call("walk", tools.WalkArgs{[]string{k.ID}, false}), passes.RefusalInvalid, passes.NoPlacesKey)
	f := fixtureWithPlaces(t, nil, nil, nil, nil, "", &places.Client{HTTP: &http.Client{Transport: places.Fake{}}, Key: "fixture-key", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	ps := seedParks(t, f, 1)
	ceiling := int64(1)
	rulesPass, err := f.st.SetParkRules(t.Context(), store.ParkRulesEdit{WalkCeiling: &ceiling}, store.Pass{Kind: store.KindParks, Origin: store.OriginRules, Trigger: store.TriggerParkRules, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.FailPass(t.Context(), rulesPass.ID, store.ReasonInterrupted); err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call("walk", tools.WalkArgs{[]string{ps[0].ID, ps[0].ID}, false}), passes.RefusalInvalid, tools.RefuseInvalid)
	refusal(t, f.call("walk", tools.WalkArgs{[]string{"park_0123456789ABCDEF"}, false}), passes.RefusalNotFound, tools.RefuseNotFound)
	r := f.callAs(other, "walk", tools.WalkArgs{[]string{ps[0].ID}, false})
	got := decodeList[passes.Record](t, r)
	if r.IsError() || got.Kind != store.KindWalk || got.Origin != store.OriginWalk || got.Trigger != store.TriggerWalk || got.User != other.UserID || got.Status != store.StatusRunning {
		t.Fatal(decode(t, r))
	}
	if ended := f.waitEnrich(got.ID); ended.Status != store.StatusDone || ended.ParksWalked != 1 || mustPark(t, f, ps[0].ID).Walked.IsZero() {
		t.Fatal(ended)
	}
	refusal(t, f.call("walk", tools.WalkArgs{[]string{ps[0].ID}, false}), passes.RefusalInvalid, passes.AlreadyWalked+" "+ps[0].ID)
	repeated := f.call("walk", tools.WalkArgs{[]string{ps[0].ID}, true})
	if repeated.IsError() {
		t.Fatal(decode(t, repeated))
	}
	again := decodeList[passes.Record](t, repeated)
	if again.ID == got.ID || again.Kind != store.KindWalk || again.Origin != store.OriginWalk || again.Trigger != store.TriggerWalk || again.User != caller.UserID {
		t.Fatal(again)
	}
	if ended := f.waitEnrich(again.ID); ended.Status != store.StatusDone || ended.ParksWalked != 1 {
		t.Fatal(ended)
	}
}

// R-WP8E-PSPS
func TestParkRulesWalkCeiling(t *testing.T) {
	f := newFixture(t)
	got := decodeList[tools.ParkRules](t, f.call("park_rules", tools.NoArgs{}))
	ceiling, err := f.st.WalkCeiling(t.Context())
	if err != nil || got.WalkCeiling != ceiling || ceiling != places.SeedCeiling {
		t.Fatal(got, ceiling, err)
	}
}

// R-EWHX-5QXW R-F05M-B25Z R-OLYR-WM1W
func TestAllToolsNormalizeStoreFailureWithoutPlaces(t *testing.T) {
	var requests atomic.Int64
	pc := &places.Client{HTTP: &http.Client{Transport: websiteTransport(func(*http.Request) (*http.Response, error) { requests.Add(1); return nil, nil })}, Key: "fixture-key", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
	f := fixtureWithPlaces(t, nil, &website.Checker{}, nil, nil, "", pc)
	id := f.seed(2)[0]
	list := f.create("fixture-list")
	park := seedParks(t, f, 1)[0]
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	f.d.SetFailing(true)
	args := map[string]any{
		"prospect_lists": tools.ProspectListsArgs{}, "prospect_list": tools.ReadProspectListArgs{list.ID, true}, "candidates": tools.CandidatesArgs{}, "candidate": tools.CandidateArgs{id}, "passes": tools.PassesArgs{}, "pass": tools.PassArgs{p.ID}, "ingest": tools.IngestArgs{}, "judge": tools.NoArgs{}, "enrich": tools.EnrichArgs{Kind: "link_facts"}, "add_page": tools.AddPageArgs{id, "https://fixture.test/"}, "decide": tools.DecideArgs{Kind: store.KindMerge, Candidate: id, Other: "cand_0000000000000000", Why: "fixture reason"}, "exclude": tools.RulingArgs{id, "reason"}, "include": tools.RulingArgs{id, "reason"}, "create_prospect_list": tools.CreateProspectListArgs{Name: "fixture-created"}, "edit_prospect_list": tools.EditProspectListArgs{ProspectList: list.ID}, "delete_prospect_list": tools.ProspectListArgs{list.ID}, "reject": tools.RejectArgs{list.ID, id, "reason"}, "unreject": tools.RejectArgs{list.ID, id, "reason"}, "parks": tools.ParksArgs{}, "park": tools.ParkArgs{park.ID}, "park_rules": tools.NoArgs{}, "edit_park_rules": tools.EditParkRulesArgs{}, "walk": tools.WalkArgs{[]string{park.ID}, false},
	}
	for _, entry := range tools.Catalog() {
		a, ok := args[entry.Name]
		if !ok {
			t.Fatal(entry)
		}
		refusal(t, f.call(entry.Name, a), passes.RefusalUnavailable, tools.RefuseUnavailable)
	}
	if requests.Load() != 0 {
		t.Fatal(requests.Load())
	}
	f.d.SetFailing(false)
}

// R-OLYR-WM1W
func TestOtherToolsNeverSearchPlaces(t *testing.T) {
	var requests atomic.Int64
	pc := &places.Client{HTTP: &http.Client{Transport: websiteTransport(func(*http.Request) (*http.Response, error) { requests.Add(1); return nil, nil })}, Key: "fixture-key", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
	f := fixtureWithPlaces(t, nil, nil, nil, nil, "", pc)
	ids := f.seed(2)
	list := f.create("untouched-places")
	park := seedParks(t, f, 1)[0]
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) == 0 {
		t.Fatal(ps, err)
	}
	for _, call := range []struct {
		name string
		args any
	}{
		{"prospect_lists", tools.ProspectListsArgs{}}, {"prospect_list", tools.ReadProspectListArgs{list.ID, true}},
		{"candidates", tools.CandidatesArgs{}}, {"candidate", tools.CandidateArgs{ids[0]}}, {"passes", tools.PassesArgs{}}, {"pass", tools.PassArgs{ps[0].ID}},
		{"parks", tools.ParksArgs{}}, {"park", tools.ParkArgs{park.ID}}, {"park_rules", tools.NoArgs{}},
		{"add_page", tools.AddPageArgs{ids[0], "https://chosen.test/"}}, {"ingest", tools.IngestArgs{}}, {"edit_park_rules", tools.EditParkRulesArgs{}},
		{"exclude", tools.RulingArgs{ids[0], "chosen exclusion"}}, {"include", tools.RulingArgs{ids[0], "chosen inclusion"}},
		{"reject", tools.RejectArgs{list.ID, ids[0], "chosen reject"}}, {"unreject", tools.RejectArgs{list.ID, ids[0], "chosen unreject"}},
		{"create_prospect_list", tools.CreateProspectListArgs{Name: "created without search"}}, {"edit_prospect_list", tools.EditProspectListArgs{ProspectList: list.ID}},
		{"decide", tools.DecideArgs{Kind: store.KindNotSame, Candidate: ids[0], Other: ids[1], Why: "chosen distinct"}},
		{"delete_prospect_list", tools.ProspectListArgs{list.ID}},
	} {
		r := f.call(call.name, call.args)
		if r.IsError() && call.name != "add_page" && call.name != "ingest" && call.name != "edit_park_rules" {
			t.Fatal(call.name, decode(t, r))
		}
	}
	for _, call := range []struct {
		name string
		args any
	}{{"judge", tools.NoArgs{}}, {"enrich", tools.EnrichArgs{Kind: "link_facts"}}} {
		r := f.call(call.name, call.args)
		if r.IsError() {
			t.Fatal(call.name, decode(t, r))
		}
		f.waitEnrich(decodeList[passes.Record](t, r).ID)
	}
	f.eng.Drain(t.Context())
	if requests.Load() != 0 {
		t.Fatal(requests.Load())
	}
}

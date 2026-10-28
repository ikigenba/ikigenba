package tools_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

func seedParks(t *testing.T, f *fixture, n int) []store.Park {
	t.Helper()
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "parcel-run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.ReplaceZipDrive(t.Context(), map[string]int64{"55001": 42}); err != nil {
		t.Fatal(err)
	}
	plan := parks.Plan{}
	lat, lon := 45.47412, -92.97826
	for i := range n {
		v := int64(i%3)*1000000 - 1850000
		county := "Chisago"
		if i%2 == 1 {
			county = "Anoka"
		}
		qs := []parks.Parcel{}
		for j := range i%3 + 2 {
			qs = append(qs, parks.Parcel{PIN: fmt.Sprintf("pin-%d-%d", i, j), Street: "fixture-street", City: "fixture-city", Zip: "55001", Owners: []string{"owner-one", "owner-two"}, Lat: &lat, Lon: &lon, BuildingValue: &v, Rule: []string{parks.RuleLabel, parks.RuleValue, parks.RuleCandidate}[j%3]})
		}
		zip := "55001"
		if i%3 == 0 {
			zip = "99999"
		}
		plan.News = append(plan.News, parks.New{Cluster: parks.Cluster{County: county, Town: fmt.Sprintf("Town %02d", i), Zip: zip, Lat: lat, Lon: lon, Radius: 548.5, BuildingValue: v, Parcels: qs}, Number: 1})
	}
	if _, err = f.st.ApplyParks(t.Context(), p.ID, "parcel-run", []string{"Anoka", "Chisago", "Carlton"}, plan, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	ps, err := f.st.ListParks(t.Context(), store.ParkFilter{Gone: parks.GoneAll}, parks.SortMinutes, "", "", n+1)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}
func parkAnswer(p store.Park) tools.Park {
	walked := ""
	if !p.Walked.IsZero() {
		walked = stamp(p.Walked)
	}
	return tools.Park{p.ID, p.Name, p.County, p.Town, p.Zip, p.State, p.Lat, p.Lon, p.Radius, p.Parcels, p.BuildingValue, p.Minutes, p.GrownFrom, walked, p.WalkRun, p.Found, p.NewCandidates, p.CeilingReached, p.Run, parks.MapURL(p.Lat, p.Lon), stamp(p.Created), stamp(p.Updated)}
}
func parcelAnswer(p store.ParkParcel) tools.ParkParcel {
	return tools.ParkParcel{p.PIN, p.Owners, p.Street, p.City, p.Zip, p.BuildingValue, p.Rule, p.Lat, p.Lon}
}

// R-E49Y-BU5S R-E5HU-PLWH R-R5UB-N5XE R-R728-0XO3
func TestParkSchemas(t *testing.T) {
	f := newFixture(t)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	dp, _ := json.Marshal(paging.PageDescription)
	dl, _ := json.Marshal(paging.LimitDescription)
	want := map[string]string{"parks": fmt.Sprintf(`{"type":"object","properties":{"county":{"type":"string"},"walked":{"type":"string"},"gone":{"type":"string"},"sort":{"type":"string"},"page":{"type":"string","description":%s},"limit":{"type":"integer","description":%s}},"additionalProperties":false}`, dp, dl), "park": `{"type":"object","properties":{"park":{"type":"string"}},"required":["park"],"additionalProperties":false}`, "edit_park_rules": `{"type":"object","properties":{"distance":{"type":"integer","minimum":0},"min_parcels":{"type":"integer","minimum":0},"value_threshold":{"type":"integer","minimum":0},"county":{"type":"string"},"labels":{"type":"array","items":{"type":"string"}},"walk_ceiling":{"type":"integer","minimum":0}},"additionalProperties":false}`}
	var judge, rule json.RawMessage
	for _, info := range infos {
		if x, ok := want[info.Name]; ok && string(info.InputSchema) != x {
			t.Fatalf("%s schema %s want %s", info.Name, info.InputSchema, x)
		}
		if info.Name == "judge" {
			judge = info.InputSchema
		}
		if info.Name == "park_rules" {
			rule = info.InputSchema
		}
	}
	if string(judge) != string(rule) {
		t.Fatal(rule, judge)
	}
	// Positional construction proves argument field order without reflection.
	_ = tools.ParksArgs{"county", "walked", "gone", "sort", nil, nil}
	_ = tools.ParkArgs{"park"}
	distance, minParcels, valueThreshold, walkCeiling := uint32(1), uint32(2), uint32(3), uint32(4)
	county, labels := "county", []string{"label"}
	_ = tools.EditParkRulesArgs{&distance, &minParcels, &valueThreshold, &county, &labels, &walkCeiling}
}

// R-R8A4-EPES R-EBLC-MGLY R-ECT9-08CN R-R9I0-SH5H R-RAPX-68W6 R-EGGY-5JKQ
func TestParkAnswerJSON(t *testing.T) {
	nextField, ok := reflect.TypeFor[tools.ParkList]().FieldByName("Next")
	if !ok || nextField.Tag.Get("description") != paging.NextDescription {
		t.Fatal(nextField)
	}
	p := tools.Park{"id", "name", "county", "town", "zip", "state", 1, 2, 3, 4, 5, nil, 6, "walked", "walk-run", 7, 8, true, "run", "map", "created", "updated"}
	q := tools.ParkParcel{"pin", nil, "street", "city", "zip", nil, "rule", 8, 9}
	for _, tc := range []struct {
		v    any
		want string
	}{
		{tools.Park{}, `{"id":"","name":"","county":"","town":"","zip":"","state":"","lat":0,"lon":0,"radius":0,"parcels":0,"building_value":0,"minutes":null,"grown_from":0,"walked":"","walk_run":"","found":0,"new_candidates":0,"ceiling_reached":false,"run":"","map_url":"","created":"","updated":""}`},
		{p, `{"id":"id","name":"name","county":"county","town":"town","zip":"zip","state":"state","lat":1,"lon":2,"radius":3,"parcels":4,"building_value":5,"minutes":null,"grown_from":6,"walked":"walked","walk_run":"walk-run","found":7,"new_candidates":8,"ceiling_reached":true,"run":"run","map_url":"map","created":"created","updated":"updated"}`},
		{q, `{"pin":"pin","owners":[],"street":"street","city":"city","zip":"zip","building_value":null,"rule":"rule","lat":8,"lon":9}`},
		{tools.ParkList{}, `{"parks":[]}`}, {tools.CountyLabels{}, `{"county":"","labels":[]}`}, {tools.ParkRules{}, `{"distance":0,"min_parcels":0,"value_threshold":0,"walk_ceiling":0,"counties":[]}`},
	} {
		b, err := json.Marshal(tc.v)
		if err != nil || string(b) != tc.want {
			t.Fatal(string(b), tc.want, err)
		}
	}
	one := int64(1)
	p.Minutes = &one
	numbered, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var numberFields map[string]json.RawMessage
	if err := json.Unmarshal(numbered, &numberFields); err != nil || string(numberFields["minutes"]) != "1" {
		t.Fatal(string(numbered), err)
	}
	q.BuildingValue = &one
	q.Owners = []string{"a", "b"}
	walks := []tools.ParkWalk{{"first-pass", "first-time", 1, 2, 3, 4, 5, true}, {"second-pass", "second-time", 6, 7, 8, 9, 10, false}}
	places := []tools.Place{{"first-place", "first-name", "first-address", "first-type", "first-website", "first-phone", "first-walk", "first-candidate", true}, {"second-place", "second-name", "second-address", "second-type", "second-website", "second-phone", "second-walk", "second-candidate", false}}
	found := []tools.FoundCandidate{{"first-candidate", "first-name", "first-status", true}, {"second-candidate", "second-name", "second-status", false}}
	populated := tools.ParkDetail{p, walks, places, found, []tools.ParkParcel{q}}
	encoded, err := json.Marshal(populated)
	if err != nil {
		t.Fatal(err)
	}
	var collections struct {
		Walks           []tools.ParkWalk       `json:"walks"`
		Places          []tools.Place          `json:"places"`
		FoundCandidates []tools.FoundCandidate `json:"found_candidates"`
	}
	if err := json.Unmarshal(encoded, &collections); err != nil || !reflect.DeepEqual(collections.Walks, walks) || !reflect.DeepEqual(collections.Places, places) || !reflect.DeepEqual(collections.FoundCandidates, found) {
		t.Fatal(string(encoded), collections, err)
	}
	next := "next"
	for _, v := range []any{tools.ParkList{[]tools.Park{p}, &next}, tools.ParkDetail{p, nil, nil, nil, []tools.ParkParcel{q}}, tools.ParkDetail{}, populated, tools.ParkRules{1, 2, 3, 4, []tools.CountyLabels{{"county", []string{"label"}}}}, tools.EditedParkRules{tools.ParkRules{}, passes.Record{}}} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var x any
		if json.Unmarshal(b, &x) != nil {
			t.Fatal(string(b))
		}
		switch x := v.(type) {
		case tools.ParkList:
			if !reflect.DeepEqual(objectKeys(t, b), []string{"parks", "next"}) {
				t.Fatal(string(b))
			}
		case tools.ParkDetail:
			if !reflect.DeepEqual(objectKeys(t, b), []string{"park", "walks", "places", "found_candidates", "parcels"}) || strings.Contains(string(b), `"parcels":null`) || strings.Contains(string(b), `"walks":null`) || strings.Contains(string(b), `"places":null`) || strings.Contains(string(b), `"found_candidates":null`) {
				t.Fatal(string(b))
			}
		case tools.EditedParkRules:
			r, _ := json.Marshal(x.Rules)
			p, _ := json.Marshal(x.Pass)
			if string(b) != `{"rules":`+string(r)+`,"pass":`+string(p)+`}` {
				t.Fatal(string(b))
			}
		}
	}
}

// R-EHOU-JBBF R-EK4N-AUST R-ENSC-G60W R-EP08-TXRL R-EQ85-7PIA
func TestParkReadTools(t *testing.T) {
	f := newFixture(t)
	success(t, f.call("parks", tools.ParksArgs{}), tools.ParkList{})
	before := decodeList[tools.ParkRules](t, f.call("park_rules", tools.NoArgs{}))
	seed := parks.Rules{Distance: parks.SeedDistance, MinParcels: parks.SeedMinParcels, ValueThreshold: parks.SeedValueThreshold}
	if before.Distance != seed.Distance || before.MinParcels != seed.MinParcels || before.ValueThreshold != seed.ValueThreshold {
		t.Fatal(before)
	}
	ps := seedParks(t, f, paging.DefaultLimit+3)
	p, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "gone-run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.ApplyParks(t.Context(), p.ID, "gone-run", []string{"Anoka", "Chisago", "Carlton"}, parks.Plan{Gone: []string{ps[0].ID}}, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	ps, err = f.st.ListParks(t.Context(), store.ParkFilter{Gone: parks.GoneAll}, parks.SortMinutes, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeList[tools.ParkRules](t, f.call("park_rules", tools.NoArgs{}))
	found := false
	for _, c := range r.Counties {
		if c.County == "Carlton" {
			found = true
			if c.Labels == nil || len(c.Labels) != 0 {
				t.Fatal(c)
			}
		}
	}
	if !found {
		t.Fatal(r)
	}
	for _, p := range ps {
		qs, err := f.st.ParkParcels(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := tools.ParkDetail{Park: parkAnswer(p), Parcels: []tools.ParkParcel{}}
		for _, q := range qs {
			want.Parcels = append(want.Parcels, parcelAnswer(q))
		}
		success(t, f.call("park", tools.ParkArgs{p.ID}), want)
	}
	for _, s := range []string{parks.SortMinutes, parks.SortValue, parks.SortParcels} {
		for _, w := range []string{"", parks.WalkedAll, parks.WalkedWalked, parks.WalkedUnwalked} {
			for _, g := range []string{"", parks.GoneAll, parks.GoneActive, parks.GoneGone} {
				for _, county := range []string{"", "Chisago", "Nowhere"} {
					want, err := f.st.ListParks(t.Context(), store.ParkFilter{County: county, Walked: w, Gone: g}, s, "", "", 100)
					if err != nil {
						t.Fatal(err)
					}
					for _, limit := range []int{1, 7, paging.DefaultLimit} {
						var token *string
						offset := 0
						for {
							z := limit
							if offset+z > len(want) {
								z = len(want) - offset
							}
							out := tools.ParkList{Parks: []tools.Park{}}
							for _, p := range want[offset : offset+z] {
								out.Parks = append(out.Parks, parkAnswer(p))
							}
							if offset+z < len(want) {
								last := want[offset+z-1]
								key := ""
								switch s {
								case parks.SortValue:
									key = fmt.Sprint(last.BuildingValue)
								case parks.SortParcels:
									key = fmt.Sprint(last.Parcels)
								default:
									if last.Minutes != nil {
										key = fmt.Sprint(*last.Minutes)
									}
								}
								t := (paging.Token{Tool: "parks"}).Encode(s, key, last.ID)
								out.Next = &t
							}
							success(t, f.call("parks", tools.ParksArgs{County: county, Walked: w, Gone: g, Sort: s, Page: token, Limit: &limit}), out)
							if out.Next == nil {
								break
							}
							token = out.Next
							offset += z
						}
					}
				}
			}
		}
	}
}

// R-EIWQ-X324 R-ELCJ-OMJI R-EMKG-2EA7 R-RBXT-K0MV R-F05M-B25Z R-EP08-TXRL
func TestParkValidationPrecedence(t *testing.T) {
	f := newFixture(t)
	ps := seedParks(t, f, 1)
	id := ps[0].ID
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, limit := range []int{0, paging.MaxLimit + 1} {
			p := "bogus"
			_, err := paging.Window(&limit)
			r := f.call("parks", tools.ParksArgs{Page: &p, Limit: &limit})
			if !r.IsError() || decode(t, r).Content[0].Text != err.Error() {
				t.Fatal(r)
			}
		}
		for _, token := range []string{"bogus", (paging.Token{Tool: "parks"}).Encode(parks.SortValue, "1500000", id), (paging.Token{Tool: "parks"}).Encode(parks.SortMinutes, "12x", id), (paging.Token{Tool: "parks"}).Encode(parks.SortMinutes, "12", store.ProspectListIDPrefix+strings.Repeat("a", 16)), (paging.Token{Tool: "parks-before"}).Encode(parks.SortMinutes, "12", id), (paging.Token{Tool: "parks"}).Encode("12", id), (paging.Token{Tool: "parks"}).Encode(parks.SortValue, "", id), (paging.Token{Tool: "passes"}).Encode(parks.SortMinutes, "12", id)} {
			r := f.call("parks", tools.ParksArgs{Page: &token})
			if !r.IsError() || decode(t, r).Content[0].Text != paging.BadPage {
				t.Fatal(r)
			}
		}
		malformed := "bogus"
		if r := f.call("parks", tools.ParksArgs{Sort: "nope", Page: &malformed}); !r.IsError() || decode(t, r).Content[0].Text != paging.BadPage {
			t.Fatal(r)
		}
		emptyValue := (paging.Token{Tool: "parks"}).Encode(parks.SortValue, "", id)
		if r := f.call("parks", tools.ParksArgs{Sort: parks.SortValue, Page: &emptyValue}); !r.IsError() || decode(t, r).Content[0].Text != paging.BadPage {
			t.Fatal(r)
		}
		for _, a := range []tools.ParksArgs{{Walked: "none"}, {Gone: "Active"}, {Sort: "name"}, {County: " Anoka"}} {
			refusal(t, f.call("parks", a), passes.RefusalInvalid, tools.RefuseInvalid)
		}

		var beforeRules tools.ParkRules
		var beforePasses []store.Pass
		if !failing {
			beforeRules = decodeList[tools.ParkRules](t, f.call("park_rules", tools.NoArgs{}))
			var err error
			beforePasses, err = f.st.ListPasses(t.Context(), "", "", 100)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, a := range []map[string]any{{"walk_ceiling": 0}, {"walk_ceiling": 5001}, {"distance": 0}, {"distance": 5001}, {"min_parcels": 1}, {"value_threshold": 1000000001}, {"labels": []string{"industrial"}}, {"county": "Carlton"}, {"county": "Carlton", "labels": nil}, {"county": "", "labels": []string{}}, {"county": "Carlton", "labels": []string{""}}, {"county": "Carlton", "labels": []string{"label", "label"}}} {
			refusal(t, f.call("edit_park_rules", a), passes.RefusalInvalid, tools.RefuseInvalid)
		}

		if !failing {
			success(t, f.call("park_rules", tools.NoArgs{}), beforeRules)
			after, err := f.st.ListPasses(t.Context(), "", "", 100)
			if err != nil || !reflect.DeepEqual(after, beforePasses) {
				t.Fatal(after, beforePasses, err)
			}
		}
		for _, bad := range []string{"bad", store.ParkIDPrefix + strings.Repeat("A", 16), store.ProspectListIDPrefix + strings.Repeat("a", 16)} {
			refusal(t, f.call("park", tools.ParkArgs{bad}), passes.RefusalNotFound, tools.RefuseNotFound)
		}
		if failing {
			for _, call := range []struct {
				name string
				a    any
			}{{"parks", tools.ParksArgs{}}, {"park", tools.ParkArgs{id}}, {"park_rules", tools.NoArgs{}}} {
				refusal(t, f.call(call.name, call.a), passes.RefusalUnavailable, tools.RefuseUnavailable)
			}
		}
	}
	f.d.SetFailing(false)
	absent := store.ParkIDPrefix + strings.Repeat("0", 16)
	if !store.ValidParkID(absent) {
		t.Fatal(absent)
	}
	refusal(t, f.call("park", tools.ParkArgs{absent}), passes.RefusalNotFound, tools.RefuseNotFound)
	want := decodeList[tools.ParkList](t, f.call("parks", tools.ParksArgs{}))
	for _, a := range []map[string]any{{"page": nil, "limit": nil}, {"page": "", "limit": nil}} {
		success(t, f.call("parks", a), want)
	}
	unknown := (paging.Token{Tool: "parks"}).Encode(parks.SortMinutes, "", id)
	success(t, f.call("parks", tools.ParksArgs{Page: &unknown}), tools.ParkList{})
}

// R-RJ97-UN31 R-YCLM-RES0
func TestEditParkRulesTool(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	headers := make(chan identity.Caller, 16)
	var fetched, prompted atomic.Int64
	f := fixtureWithPrompts(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- identity.Caller{UserID: r.Header.Get("X-User-Id"), Email: r.Header.Get("X-User-Email"), RequestID: r.Header.Get("X-Request-Id")}
		switch r.URL.Path {
		case "/scrapers/mn_parcels/current":
			_, _ = w.Write([]byte(`{"id":"parcel-run","scraper":"mn_parcels","status":"done","started":"2000-01-01T00:00:00Z","rows":0,"new":0,"changed":0,"unchanged":0,"gone":0}`))
		case "/runs/parcel-run/record":
			_, _ = w.Write([]byte(`{"id":"parcel-run","scraper":"mn_parcels","status":"done"}`))
		case "/runs/parcel-run/rows":
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(404)
		}
	}), pageChecker(&fetched), promptClient(t, false, &prompted))
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})

	seedParks(t, f, 2)
	ids := f.seed(1)
	list := f.create("edit preservation")
	if _, err := f.st.AddPage(t.Context(), store.Page{Candidate: ids[0], URL: "https://page.fixture.test/", Text: "kept body", UserID: caller.UserID}); err != nil {
		t.Fatal(err)
	}
	preserved := parkPreservedRecords(t, f, ids[0], list.ID)
	priorPasses, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	before := decodeList[tools.ParkRules](t, f.call("park_rules", tools.NoArgs{}))
	r := f.call("edit_park_rules", map[string]any{"min_parcels": 2})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	got := decodeList[tools.EditedParkRules](t, r)
	before.MinParcels = 2
	if !reflect.DeepEqual(got.Rules, before) || got.Pass.Kind != store.KindParks || got.Pass.Origin != store.OriginRules || got.Pass.Trigger != store.TriggerParkRules || got.Pass.Status != store.StatusRunning || got.Pass.User != caller.UserID {
		t.Fatal(got)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("rows not reached")
	}

	held, err := f.st.GetPass(t.Context(), got.Pass.ID)
	if err != nil {
		t.Fatal(err)
	}
	if held.UserID != caller.UserID || held.UserEmail != caller.Email || held.RequestID != caller.RequestID {
		t.Fatal(held)
	}
	success(t, r, tools.EditedParkRules{Rules: got.Rules, Pass: passes.RecordOf(held)})
	for range 2 {
		select {
		case h := <-headers:
			if h != caller {
				t.Fatal(h, caller)
			}
		default:
			t.Fatal("missing forwarded caller")
		}
	}
	beforeBusy, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call("edit_park_rules", map[string]any{"min_parcels": 5}), passes.RefusalBusy, tools.RefuseBusy)
	success(t, f.call("park_rules", tools.NoArgs{}), before)
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != len(priorPasses)+1 || !reflect.DeepEqual(ps, beforeBusy) {
		t.Fatal(ps, beforeBusy, err)
	}
	if !reflect.DeepEqual(preserved, parkPreservedRecords(t, f, ids[0], list.ID)) {
		t.Fatal("rule edit changed another record while rows held")
	}
	if fetched.Load() != 0 || prompted.Load() != 0 {
		t.Fatal(fetched.Load(), prompted.Load())
	}
	unavailable := newFixture(t)
	unchanged := decodeList[tools.ParkRules](t, unavailable.call("park_rules", tools.NoArgs{}))
	refusal(t, unavailable.call("edit_park_rules", tools.EditParkRulesArgs{}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	success(t, unavailable.call("park_rules", tools.NoArgs{}), unchanged)
	close(release)
	released = true
	if ended := f.waitEnrich(got.Pass.ID); ended.Status != store.StatusDone {
		t.Fatal(ended)
	}
	before = decodeList[tools.ParkRules](t, f.call("park_rules", tools.NoArgs{}))
	for _, a := range []map[string]any{{}, {"distance": 321, "value_threshold": 987}, {"county": "Carlton", "labels": []string{"Industrial Preferred"}}, {"county": "Chisago", "labels": []string{}}, {"walk_ceiling": 250}} {
		out := decodeList[tools.EditedParkRules](t, f.call("edit_park_rules", a))
		if out.Pass.Status != store.StatusRunning || out.Pass.Kind != store.KindParks {
			t.Fatal(out)
		}
		if distance, ok := a["distance"].(int); ok {
			before.Distance = int64(distance)
		}
		if ceiling, ok := a["walk_ceiling"].(int); ok {
			before.WalkCeiling = int64(ceiling)
		}
		if out.Rules.WalkCeiling != before.WalkCeiling {
			t.Fatal(out.Rules, before)
		}
		if threshold, ok := a["value_threshold"].(int); ok {
			before.ValueThreshold = int64(threshold)
		}
		if out.Rules.Distance != before.Distance || out.Rules.MinParcels != before.MinParcels || out.Rules.ValueThreshold != before.ValueThreshold {
			t.Fatal(out.Rules, before)
		}
		if county, ok := a["county"].(string); ok {
			labels := a["labels"].([]string)
			found := false
			for _, x := range out.Rules.Counties {
				if x.County == county {
					found = true
					if !reflect.DeepEqual(x.Labels, labels) {
						t.Fatal(x, labels)
					}
				}
			}
			if found != (len(labels) > 0) {
				t.Fatal(out.Rules)
			}
		} else if !reflect.DeepEqual(out.Rules, before) {
			t.Fatal(out.Rules, before)
		}
		before = out.Rules
		f.waitEnrich(out.Pass.ID)
	}

	fresh := fixtureWithScrapes(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/rows") {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{"id":"fresh-parcel-run","scraper":"mn_parcels","status":"done","started":"2000-01-01T00:00:00Z","rows":0,"new":0,"changed":0,"unchanged":0,"gone":0}`))
	}))
	run, err := fresh.st.ParksRun(t.Context())
	if err != nil || run != "" {
		t.Fatal(run, err)
	}
	cleared := decodeList[tools.EditedParkRules](t, fresh.call("edit_park_rules", map[string]any{"county": "Chisago", "labels": []string{}}))
	for _, c := range cleared.Rules.Counties {
		if c.County == "Chisago" {
			t.Fatal(cleared.Rules)
		}
	}
	fresh.waitEnrich(cleared.Pass.ID)
	f.eng.Drain(t.Context())
	refusal(t, f.call("edit_park_rules", tools.EditParkRulesArgs{}), passes.RefusalStopping, tools.RefuseStopping)
}

func parkPreservedRecords(t *testing.T, f *fixture, candidate, list string) []any {
	t.Helper()
	out := []any{}
	add := func(v any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	add(f.st.GetCandidate(t.Context(), candidate))
	add(f.st.Links(t.Context(), candidate))
	add(f.st.Estimates(t.Context(), candidate))
	add(f.st.GetJudgments(t.Context(), candidate))
	add(f.st.Rulings(t.Context(), candidate))
	add(f.st.Rejects(t.Context(), candidate))
	add(f.st.Pages(t.Context(), candidate))
	add(f.st.CountPages(t.Context(), candidate))
	add(f.st.ListProspectLists(t.Context(), "", "", 100))
	add(f.st.Opens(t.Context(), list))
	add(f.st.ReadProspectList(t.Context(), list))
	add(f.st.Enrichments(t.Context(), candidate))
	add(f.st.CampaignUses(t.Context(), candidate))
	ps, err := f.st.ListParks(t.Context(), store.ParkFilter{Gone: parks.GoneAll}, parks.SortMinutes, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, ps)
	for _, p := range ps {
		add(f.st.GetPark(t.Context(), p.ID))
		add(f.st.ParkParcels(t.Context(), p.ID))
	}
	a, g, err := f.st.CountParks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, a, g)
	add(f.st.ParksRun(t.Context()))
	return out
}

// R-EIWQ-X324
func TestParkTokenDoesNotBindFilters(t *testing.T) {
	f := newFixture(t)
	seedParks(t, f, paging.DefaultLimit+3)
	limit := 1
	first := decodeList[tools.ParkList](t, f.call("parks", tools.ParksArgs{Limit: &limit}))
	if first.Next == nil {
		t.Fatal(first)
	}
	var sort, key, id string
	if err := (paging.Token{Tool: "parks"}).Decode(*first.Next, &sort, &key, &id); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []store.ParkFilter{{County: "Chisago"}, {Walked: parks.WalkedWalked}, {Walked: parks.WalkedUnwalked, Gone: parks.GoneAll}, {County: "Anoka", Walked: parks.WalkedUnwalked, Gone: parks.GoneAll}} {
		rows, err := f.st.ListParks(t.Context(), filter, sort, key, id, limit+1)
		if err != nil {
			t.Fatal(err)
		}
		want := tools.ParkList{Parks: []tools.Park{}}
		for _, p := range rows[:min(limit, len(rows))] {
			want.Parks = append(want.Parks, parkAnswer(p))
		}
		if len(rows) > limit {
			p := rows[limit-1]
			k := ""
			if p.Minutes != nil {
				k = fmt.Sprint(*p.Minutes)
			}
			next := (paging.Token{Tool: "parks"}).Encode(sort, k, p.ID)
			want.Next = &next
		}
		success(t, f.call("parks", tools.ParksArgs{County: filter.County, Walked: filter.Walked, Gone: filter.Gone, Page: first.Next, Limit: &limit}), want)
	}
}

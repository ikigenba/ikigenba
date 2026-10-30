package tools_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-W14O-D7AU R-W2CK-QZ1J R-W3KH-4QS8 R-W609-WA9M
func TestOwnerToolSchemas(t *testing.T) {
	f := newFixture(t)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	h := `{"type":"object","properties":{"count":{"type":"integer"},"year":{"type":"integer"}},"required":["count","year"],"additionalProperties":false}`
	r := `{"type":"object","properties":{"low":{"type":"integer"},"high":{"type":"integer"}},"additionalProperties":false}`
	a := `{"type":"object","properties":{"street":{"type":"string"},"city":{"type":"string"},"state":{"type":"string"},"zip":{"type":"string"},"county":{"type":"string"}},"additionalProperties":false}`
	p := `{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"role":{"type":"string"}},"required":["name"],"additionalProperties":false}}`
	for _, name := range []string{"add_business", "set_facts"} {
		fields := `"name":{"type":"string"},"headcount":` + h + `,"revenue":` + r + `,"category":{"type":"string"},"website":{"type":"string"},"address":` + a + `,"phone":{"type":"string"},"email":{"type":"string"},"people":` + p + `,"known_since":{"type":"integer"},"ein":{"type":"string"},"fleet":{"type":"integer"},`
		required := `["name","note"]`
		if name == "set_facts" {
			fields = `"candidate":{"type":"string"},` + fields + `"clear":{"type":"array","items":{"type":"string"}},`
			required = `["candidate","note"]`
		}
		want := `{"type":"object","properties":{` + fields + `"note":{"type":"string"}},"required":` + required + `,"additionalProperties":false}`
		for _, info := range infos {
			if info.Name == name {
				if string(info.InputSchema) != want {
					t.Fatalf("%s schema %s want %s", name, info.InputSchema, want)
				}
			}
		}
	}
}

// R-W786-A20B R-W8G2-NTR0 R-W9NZ-1LHP R-WAVV-FD8E R-WGZD-C7XV R-F6HE-ZMVO R-1CXH-96BV
func TestOwnerWritesAndCandidateHistory(t *testing.T) {
	f := newFixture(t)
	name, phone, note := "North Star Hauling", "2185550100", "met at the expo"
	firstResult := f.call("add_business", tools.AddBusinessArgs{Name: name, Phone: &phone, Note: note})
	first := decodeList[tools.OwnerWrite](t, firstResult)
	id := first.Record.Candidate
	if firstResult.IsError() || !store.ValidOwnerRecordID(first.Record.ID) || !store.ValidCandidateID(id) || !first.Record.Founded || first.Record.User != caller.UserID || first.Record.Email != caller.Email || first.Record.Note != note || first.Record.At != stamp(f.now) || first.Record.Set.Name == nil || *first.Record.Set.Name != name || first.Record.Set.Phone == nil || *first.Record.Set.Phone != phone || len(first.Record.Cleared) != 0 {
		t.Fatal(first)
	}
	if first.Candidate.Candidate.ID != id || first.Candidate.Candidate.Name != name || first.Candidate.Candidate.Phone != phone || first.Candidate.Sources.Name != sources.Owner || first.Candidate.Sources.Phone != sources.Owner || !reflect.DeepEqual(first.Candidate.Founded, &first.Record) || len(first.Candidate.OwnerSet) != 2 {
		t.Fatal(first)
	}
	for _, w := range []string{sources.FactName, sources.FactPhone} {
		if first.Candidate.OwnerSet[w] != (tools.OwnerSetBy{Record: first.Record.ID, Note: note, At: stamp(f.now)}) {
			t.Fatal(first.Candidate.OwnerSet)
		}
	}
	success(t, f.call("candidate", tools.CandidateArgs{Candidate: id}), first.Candidate)
	f.now = f.now.Add(time.Second)
	email := "ops@northstar.test"
	second := decodeList[tools.OwnerWrite](t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, Email: &email, Clear: []string{sources.FactName}, Note: "new email"}))
	if second.Record.Founded || second.Record.Candidate != id || !reflect.DeepEqual(second.Record.Cleared, []string{sources.FactName}) || second.Candidate.OwnerSet[sources.FactName].Record != "" || second.Candidate.OwnerSet[sources.FactPhone].Record != first.Record.ID || second.Candidate.OwnerSet[sources.FactEmail].Record != second.Record.ID || !reflect.DeepEqual(second.Candidate.OwnerRecords, []tools.OwnerRecord{second.Record, first.Record}) {
		t.Fatal(second)
	}
	f.now = f.now.Add(time.Second)
	third := decodeList[tools.OwnerWrite](t, f.call("add_business", tools.AddBusinessArgs{Name: "North Star Freight", Phone: &phone, Note: "renamed"}))
	if third.Record.Founded || third.Record.Candidate != id || third.Candidate.Candidate.Name != "North Star Freight" {
		t.Fatal(third)
	}
	f.now = f.now.Add(time.Second)
	fourth := decodeList[tools.OwnerWrite](t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, Clear: []string{sources.FactPhone, sources.FactName}, Note: "clear both"}))
	if !reflect.DeepEqual(fourth.Record.Cleared, []string{sources.FactName, sources.FactPhone}) {
		t.Fatal(fourth)
	}
	passes, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(passes) != 0 {
		t.Fatal(passes, err)
	}
	missing := "cand_0000000000000000"
	refusal(t, f.call("set_facts", tools.SetFactsArgs{Candidate: missing, Name: &name, Note: note}), "not_found", tools.RefuseNotFound)
	survivor := f.seed(1)[0]
	f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: survivor, Other: id, Why: "same"})
	merged := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if merged.Founded != nil || merged.EIN != nil || len(merged.OwnerSet) != 0 || len(merged.OwnerRecords) != 0 {
		t.Fatal(merged)
	}
	detail := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: survivor}))
	if detail.Founded != nil || len(detail.OwnerRecords) != 4 {
		t.Fatal(detail)
	}
	for _, r := range detail.OwnerRecords {
		if r.Founded {
			t.Fatal(r)
		}
	}
	refusal(t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, Name: &name, Note: note}), "invalid", tools.RefuseInvalid)
}

// R-WEJK-KOGH R-WFRG-YG76 R-VSLD-OT3Z R-VRDH-B1DA
func TestOwnerValidationBeforeStoreAccess(t *testing.T) {
	f := newFixture(t)
	f.d.SetFailing(true)
	for _, bad := range []map[string]any{{"note": "  "}, {"name": "  "}, {"phone": "218-555-0100"}, {"category": "freight"}, {"website": "northstar.test"}, {"revenue": map[string]any{}}, {"headcount": map[string]any{"count": 26, "year": 0}}, {"people": []any{}}, {"known_since": 0}, {"ein": "41-1234567"}, {"fleet": -1}} {
		args := map[string]any{"name": "North Star Hauling", "note": "note"}
		for k, v := range bad {
			args[k] = v
		}
		refusal(t, f.call("add_business", args), "invalid", tools.RefuseInvalid)
	}
	for _, bad := range []map[string]any{{"clear": []string{"newest_filing"}}, {"clear": []string{"phone", "phone"}}, {"email": "ops@northstar.test", "clear": []string{"email"}}, {}, {"name": "North Star", "note": " "}, {"phone": "218-555-0100"}, {"people": []any{}}} {
		args := map[string]any{"candidate": "bad-candidate", "note": "note"}
		for k, v := range bad {
			args[k] = v
		}
		refusal(t, f.call("set_facts", args), "invalid", tools.RefuseInvalid)
	}
	refusal(t, f.call("set_facts", tools.SetFactsArgs{Candidate: "bad-candidate", Name: new("North Star"), Note: "note"}), "not_found", tools.RefuseNotFound)
	refusal(t, f.call("add_business", tools.AddBusinessArgs{Name: "North Star", Note: "note"}), "unavailable", tools.RefuseUnavailable)
	refusal(t, f.call("set_facts", tools.SetFactsArgs{Candidate: "cand_0000000000000000", Name: new("North Star"), Note: "note"}), "unavailable", tools.RefuseUnavailable)
}

// R-W786-A20B R-W8G2-NTR0 R-VZWR-ZFK5
func TestOwnerAnswerEncoding(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{tools.OwnerRecord{}, `{"id":"","candidate":"","founded":false,"note":"","user":"","email":"","at":"","set":{},"cleared":[]}`},
		{tools.OwnerSetBy{}, `{"record":"","note":"","at":""}`},
		{tools.SourcedCount{}, `{"value":0,"source":""}`},
	} {
		b, err := json.Marshal(tc.value)
		if err != nil || string(b) != tc.want {
			t.Fatal(string(b), tc.want, err)
		}
	}
	r := tools.OwnerRecord{Cleared: []string{}, ID: "record", Candidate: "candidate", Founded: true, Note: "note", User: "user", Email: "email", At: "at", Set: store.OwnerValues{Name: new("name")}}
	b, err := json.Marshal(r)
	if err != nil || string(b) != `{"id":"record","candidate":"candidate","founded":true,"note":"note","user":"user","email":"email","at":"at","set":{"name":"name"},"cleared":[]}` {
		t.Fatal(string(b), err)
	}
	d := tools.Detail{Founded: &r, OwnerSet: map[string]tools.OwnerSetBy{"phone": {Record: "phone-record", Note: "phone-note", At: "phone-at"}, "name": {Record: "name-record", Note: "name-note", At: "name-at"}}, OwnerRecords: []tools.OwnerRecord{r}, EIN: &tools.Sourced{Value: "411234567", Source: sources.Owner}}
	b, err = json.Marshal(tools.OwnerWrite{Record: r, Candidate: d})
	if err != nil {
		t.Fatal(err)
	}
	var got tools.OwnerWrite
	if err = json.Unmarshal(b, &got); err != nil || !reflect.DeepEqual(got.Record, r) {
		t.Fatal(got, err)
	}

	var detailFields map[string]json.RawMessage
	detailJSON, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(detailJSON, &detailFields); err != nil {
		t.Fatal(err)
	}
	rb, _ := json.Marshal(r)
	for k, want := range map[string]string{"ein": `{"value":"411234567","source":"owner"}`, "founded": string(rb), "owner_records": "[" + string(rb) + "]", "owner_set": `{"name":{"record":"name-record","note":"name-note","at":"name-at"},"phone":{"record":"phone-record","note":"phone-note","at":"phone-at"}}`} {
		if string(detailFields[k]) != want {
			t.Fatalf("%s got %s want %s", k, detailFields[k], want)
		}
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil || !reflect.DeepEqual(objectKeys(t, b), []string{"record", "candidate"}) {
		t.Fatal(string(b), err)
	}
}

// R-WJF6-3RF9 R-F59I-LV4Z
func TestOwnerWriteTelemetry(t *testing.T) {
	type eventName string
	const _ eventName = tools.EventOwnerRecordWritten
	f := newFixture(t)
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "candidates", Sink: capture, Now: func() time.Time { return f.now }, Rand: &random{}, Sleep: func(context.Context, time.Duration) {}})
	w.Ready()
	t.Cleanup(func() { w.Shutdown(context.Background(), "fixture-stop") })
	srv := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: w})
	tools.Register(srv, tools.Config{Store: f.st, Telemetry: w})
	host := httptest.NewServer(identity.Require(srv))
	defer host.Close()
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL, HTTPClient: host.Client()})
	first := decodeList[tools.OwnerWrite](t, f.call("add_business", tools.AddBusinessArgs{Name: "event business", Note: "created"}))
	second := decodeList[tools.OwnerWrite](t, f.call("set_facts", tools.SetFactsArgs{Candidate: first.Record.Candidate, Name: new("event renamed"), Phone: new("2185550100"), Clear: []string{sources.FactEmail, sources.FactWebsite}, Note: "updated"}))
	refusal(t, f.call("add_business", tools.AddBusinessArgs{Name: "refused", Note: " "}), "invalid", tools.RefuseInvalid)
	for _, id := range []string{"malformed", "cand_0000000000000000"} {
		refusal(t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, Name: new("missing"), Note: "no write"}), "not_found", tools.RefuseNotFound)
	}
	survivor := f.seed(1)[0]
	f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: survivor, Other: first.Record.Candidate, Why: "same"})
	refusal(t, f.call("set_facts", tools.SetFactsArgs{Candidate: first.Record.Candidate, Name: new("merged"), Note: "no write"}), "invalid", tools.RefuseInvalid)
	f.call("candidate", tools.CandidateArgs{Candidate: survivor})
	// A nil writer is optional and has no effect on the write answer.
	srv = mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: w})
	tools.Register(srv, tools.Config{Store: f.st})
	host2 := httptest.NewServer(identity.Require(srv))
	defer host2.Close()
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: host2.URL, HTTPClient: host2.Client()})
	if r := f.call("add_business", tools.AddBusinessArgs{Name: "without telemetry", Note: "created"}); r.IsError() {
		t.Fatal(r)
	}
	w.Shutdown(context.Background(), "fixture-stop")
	if tools.EventOwnerRecordWritten != "owner_record.written" {
		t.Fatal(tools.EventOwnerRecordWritten)
	}
	got := []telemetry.Event{}
	for _, e := range capture.Events() {
		if e.Name == tools.EventOwnerRecordWritten {
			got = append(got, e)
		}
	}
	if len(got) != 2 {
		t.Fatal(got)
	}
	for i, r := range []tools.OwnerRecord{first.Record, second.Record} {
		set, cleared := "name", ""
		if i == 1 {
			set = "name,phone"
			cleared = "website,email"
		}
		want := telemetry.Attrs{"record": r.ID, "candidate": r.Candidate, "founded": r.Founded, "set": set, "cleared": cleared}
		if got[i].User != caller.UserID || got[i].RequestID != caller.RequestID || !reflect.DeepEqual(got[i].Attrs, want) {
			t.Fatal(got[i], want)
		}
	}

}

// R-1BPK-VEL6 R-1CXH-96BV R-10QH-FGWX R-0YAO-NXFJ
func TestOwnerReadProjections(t *testing.T) {
	f := newFixture(t)
	_, ids, err := f.st.Attach(t.Context(), "owner-run", false, []store.Observation{{Scraper: "form5500", SourceID: "plan", Digest: "digest", Reading: sources.Reading{Name: "owner facts business", NameKey: "facts", EINKey: "411234567", KnownSince: time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC)}}})
	if err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	before := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if before.EIN == nil || *before.EIN != (tools.Sourced{Value: "411234567", Source: "form5500"}) {
		t.Fatal(before)
	}
	if _, _, err = f.st.Derive(t.Context(), enrichers.KindLinkFacts, "", false, 100); err != nil {
		t.Fatal(err)
	}
	found, _ := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://acme.test/"})
	if _, err = f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "check", Found: found}}); err != nil {
		t.Fatal(err)
	}
	write := decodeList[tools.OwnerWrite](t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, KnownSince: new(1988), EIN: new("419876543"), Website: new("https://owner-co.test/"), Note: "owner inputs"}))
	got := write.Candidate
	if got.EIN == nil || *got.EIN != (tools.Sourced{Value: "419876543", Source: sources.Owner}) || got.OwnerSet[sources.FactEIN].Record != write.Record.ID || got.Enrichments.Facts.KnownSince != "1988-01-01" || got.Enrichments.Facts.KnownSinceSource != sources.Owner || !reflect.DeepEqual(got.Enrichments.Facts.OtherKnownSince, []tools.Sourced{{Value: "1995-01-01", Source: "form5500"}}) || got.Enrichments.Facts.WebsiteSource != store.WebsiteByOwner || got.Enrichments.Facts.WebsiteURL != "https://owner-co.test/" || got.Enrichments.Facts.WebsiteCheckedURL != "" {
		t.Fatal(got)
	}
	f.now = f.now.Add(time.Second)
	cleared := decodeList[tools.OwnerWrite](t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, Clear: []string{sources.FactWebsite}, Note: "clear"}))
	if cleared.Candidate.Enrichments.Facts.WebsiteSource != store.WebsiteByCheck || cleared.Candidate.Enrichments.Facts.WebsiteURL != "https://acme.test/" {
		t.Fatal(cleared)
	}
}

// R-WAVV-FD8E R-F6HE-ZMVO
func TestOwnerAllValuesAndPlacesFallback(t *testing.T) {
	f := newFixture(t)
	a := tools.AddBusinessArgs{Name: " North Star Hauling ", Note: "  exact owner words  ", Headcount: &tools.HeadcountArgs{Count: 26, Year: 2024}, Revenue: &tools.RevenueArgs{Low: new(int64(1000000)), High: new(int64(2000000))}, Category: new(sources.CategoryTrucking), Website: new("https://owner-co.test/"), Address: &tools.AddressArgs{Street: " 5 Main St ", City: " Rush City ", State: "MN", Zip: "55069", County: "MN-115"}, Phone: new("2185550100"), Email: new("ops@northstar.test"), People: []tools.PersonArgs{{Name: " Jane Doe ", Role: " owner "}}, KnownSince: new(1988), EIN: new("411234567"), Fleet: new(int64(7))}
	got := decodeList[tools.OwnerWrite](t, f.call("add_business", a))
	want := store.OwnerValues{Name: &a.Name, Headcount: &store.OwnerHeadcount{Count: 26, Year: 2024}, Revenue: &store.OwnerRevenue{Low: a.Revenue.Low, High: a.Revenue.High}, Category: a.Category, Website: a.Website, Address: &sources.Address{Street: a.Address.Street, City: a.Address.City, State: a.Address.State, Zip: a.Address.Zip, County: a.Address.County}, Phone: a.Phone, Email: a.Email, People: []store.OwnerPerson{{Name: a.People[0].Name, Role: a.People[0].Role}}, KnownSince: a.KnownSince, EIN: a.EIN, Fleet: a.Fleet}
	if !reflect.DeepEqual(got.Record.Set, want) || got.Record.Note != a.Note || got.Record.User != caller.UserID || got.Record.Email != caller.Email {
		t.Fatal(got.Record, want)
	}
	park := seedParks(t, f, 1)[0]
	pass, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.RecordSweep(t.Context(), store.Sweep{Pass: pass.ID, Park: park.ID, Found: []places.Found{{Place: places.Place{ID: "owner-place", Name: "Places business", Address: "5 Main St"}}}}); err != nil {
		t.Fatal(err)
	}
	listed, err := f.st.ListCandidates(t.Context(), store.CandidateFilter{Name: "Places business"}, "", "", 100)
	if err != nil || len(listed) != 1 {
		t.Fatal(listed, err)
	}
	id := listed[0].ID
	f.now = f.now.Add(time.Second)
	set := decodeList[tools.OwnerWrite](t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, Name: new("Owner business"), Note: "rename"}))
	if set.Candidate.Candidate.Name != "Owner business" {
		t.Fatal(set)
	}
	f.now = f.now.Add(time.Second)
	cleared := decodeList[tools.OwnerWrite](t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, Clear: []string{sources.FactName}, Note: "restore"}))
	if cleared.Candidate.Candidate.Name != "Places business" || cleared.Candidate.Sources.Name != places.Name {
		t.Fatal(cleared)
	}
	f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: got.Record.Candidate, Other: id, Why: "same"})
	before, err := f.st.OwnerRecords(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	survivorBefore, err := f.st.OwnerRecords(t.Context(), got.Record.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call("set_facts", tools.SetFactsArgs{Candidate: id, Name: new("refused"), Note: "merged"}), "invalid", tools.RefuseInvalid)
	survivorAfter, err := f.st.OwnerRecords(t.Context(), got.Record.Candidate)
	if err != nil || !reflect.DeepEqual(survivorBefore, survivorAfter) {
		t.Fatal(survivorBefore, survivorAfter, err)
	}
	after, err := f.st.OwnerRecords(t.Context(), id)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(before, after, err)
	}
}

// R-W14O-D7AU R-W2CK-QZ1J R-W3KH-4QS8 R-W786-A20B R-W8G2-NTR0
func TestOwnerExportedShapes(_ *testing.T) {
	_ = struct{ Count, Year int }(tools.HeadcountArgs{})
	_ = struct{ Low, High *int64 }(tools.RevenueArgs{})
	_ = struct{ Street, City, State, Zip, County string }(tools.AddressArgs{})
	_ = struct{ Name, Role string }(tools.PersonArgs{})
	_ = struct {
		Name              string
		Headcount         *tools.HeadcountArgs
		Revenue           *tools.RevenueArgs
		Category, Website *string
		Address           *tools.AddressArgs
		Phone, Email      *string
		People            []tools.PersonArgs
		KnownSince        *int
		EIN               *string
		Fleet             *int64
		Note              string
	}(tools.AddBusinessArgs{})
	_ = struct {
		Candidate         string
		Name              *string
		Headcount         *tools.HeadcountArgs
		Revenue           *tools.RevenueArgs
		Category, Website *string
		Address           *tools.AddressArgs
		Phone, Email      *string
		People            []tools.PersonArgs
		KnownSince        *int
		EIN               *string
		Fleet             *int64
		Clear             []string
		Note              string
	}(tools.SetFactsArgs{})
	_ = struct {
		ID, Candidate         string
		Founded               bool
		Note, User, Email, At string
		Set                   store.OwnerValues
		Cleared               []string
	}(tools.OwnerRecord{})
	_ = struct{ Record, Note, At string }(tools.OwnerSetBy{})
	_ = struct {
		Value  int64
		Source string
	}(tools.SourcedCount{})
	_ = struct {
		Record    tools.OwnerRecord
		Candidate tools.Detail
	}(tools.OwnerWrite{})
}

package tools_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-RP1C-IZ2C R-RRH5-AIJQ R-RV4U-FTRT R-TD0R-NEDD R-CS5Y-61GK
func TestLegacyListDispatch(t *testing.T) {
	_ = tools.LegacyArgs{"chosen-id"}
	d := tools.DescribeLegacy
	if d == "" || len(d) > 120 || !strings.HasSuffix(d, ".") || strings.Contains(d, "\n") || strings.Contains(d, ". ") || strings.TrimSpace(d) != d {
		t.Fatal(d)
	}
	f := newFixture(t)
	f.seed(1)
	list := f.create("legacy-reader")
	writer := telemetry.New(telemetry.Config{Service: "candidates", Sink: &telemetry.Capture{}})
	main := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: writer})
	legacy := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: writer})
	cfg := tools.Config{Passes: f.eng, Store: f.st}
	tools.Register(main, cfg)
	tools.RegisterLegacy(legacy, cfg)
	host := httptest.NewServer(identity.Require(tools.Dispatch(main, legacy)))
	t.Cleanup(host.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL, HTTPClient: host.Client()})
	infos, err := client.ListTools(t.Context(), caller)
	if err != nil || len(infos) != len(tools.Catalog()) {
		t.Fatal(infos, err)
	}
	for i, info := range infos {
		if info.Name != tools.Catalog()[i].Name || info.Description != tools.Catalog()[i].Description {
			t.Fatal(info)
		}
		if info.Name == "cohort" {
			t.Fatal(info)
		}
	}
	names := tools.LegacyNames()
	names[0] = "changed"
	if !reflect.DeepEqual(tools.LegacyNames(), []string{"cohort"}) {
		t.Fatal(tools.LegacyNames())
	}
	only := httptest.NewServer(identity.Require(legacy))
	t.Cleanup(only.Close)
	lc := mcp.NewClient(mcp.ClientConfig{Endpoint: only.URL, HTTPClient: only.Client()})
	li, err := lc.ListTools(t.Context(), caller)
	if err != nil || len(li) != 1 || li[0].Name != "cohort" || li[0].Description != tools.DescribeLegacy || li[0].Effect() != mcp.Read {
		t.Fatal(li, err)
	}
	var schema any
	_ = json.Unmarshal(li[0].InputSchema, &schema)
	var wantSchema any
	_ = json.Unmarshal([]byte(`{"type":"object","properties":{"cohort":{"type":"string"}},"required":["cohort"],"additionalProperties":false}`), &wantSchema)
	if !reflect.DeepEqual(schema, wantSchema) {
		t.Fatal(schema)
	}
	call := func(name string, args any) mcp.Result {
		t.Helper()
		b, _ := json.Marshal(args)
		r, e := client.CallTool(t.Context(), caller, name, b)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	want := f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: list.ID, Peek: true})
	for _, id := range []string{list.ID, store.LegacyListIDPrefix + strings.TrimPrefix(list.ID, store.ProspectListIDPrefix)} {
		r := call("cohort", tools.LegacyArgs{Cohort: id})
		if !reflect.DeepEqual(decode(t, r), decode(t, want)) {
			t.Fatal(decode(t, r), decode(t, want))
		}
	}
	opens, err := f.st.Opens(t.Context(), list.ID)
	if err != nil || len(opens) != 0 {
		t.Fatal("legacy recorded an open", opens, err)
	}
	// A current open after legacy reads has the same answer as the untouched peek.
	got := f.call("prospect_list", tools.ReadProspectListArgs{list.ID, false})
	if !reflect.DeepEqual(decode(t, got), decode(t, want)) {
		t.Fatal(decode(t, got), decode(t, want))
	}
	for _, id := range []string{"", "bogus", "coh_0123456789ABCDEF", "coh_0000000000000000"} {
		refusal(t, call("cohort", tools.LegacyArgs{Cohort: id}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	f.d.SetFailing(true)
	refusal(t, call("cohort", tools.LegacyArgs{Cohort: list.ID}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	for _, bad := range []string{"", "bogus", "coh_0123456789ABCDEF"} {
		refusal(t, call("cohort", tools.LegacyArgs{Cohort: bad}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	f.d.SetFailing(false)
	// Compare exact transport responses to the selected server, including boundary bodies.
	base := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cohort","arguments":{"cohort":"` + list.ID + `"}}}`
	for _, tc := range []struct {
		body   string
		chosen *mcp.Server
	}{
		{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, main}, {base, legacy}, {strings.Replace(base, `"method"`, `"Method"`, 1), main}, {strings.Replace(base, `"params"`, `"Params"`, 1), main}, {strings.Replace(base, `"name"`, `"Name"`, 1), main}, {base + strings.Repeat(" ", 65536-len(base)), legacy}, {base + strings.Repeat(" ", 65537-len(base)), main},
		{`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`, main}, {`not JSON`, main},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"prospect_list","arguments":{"prospect_list":"cohort"}}}`, main},
	} {
		request := func(handler http.Handler) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User-Id", caller.UserID)
			r.Header.Set("X-User-Email", caller.Email)
			w := httptest.NewRecorder()
			identity.Require(handler).ServeHTTP(w, r)
			return w
		}
		got, want := request(tools.Dispatch(main, legacy)), request(tc.chosen)
		if got.Code != want.Code || !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
			t.Fatalf("body %d: %d %s vs %d %s", len(tc.body), got.Code, got.Body, want.Code, want.Body)
		}
	}
	primary := httptest.NewServer(identity.Require(main))
	t.Cleanup(primary.Close)
	pc := mcp.NewClient(mcp.ClientConfig{Endpoint: primary.URL, HTTPClient: primary.Client()})
	r, err := pc.CallTool(t.Context(), caller, "cohort", json.RawMessage(`{"cohort":"`+list.ID+`"}`))
	if err == nil && !r.IsError() {
		t.Fatal(r)
	}
}

// R-3347-ENPM R-TFTB-22NR R-1LGM-KR32 R-Q7QE-1TXQ
func TestLegacyListArgumentsMapToCurrentIDs(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	list := f.create("mapped-list")
	old := store.LegacyListIDPrefix + strings.TrimPrefix(list.ID, store.ProspectListIDPrefix)
	success(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: old, Peek: true}), decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: list.ID, Peek: true})))
	renamed := "renamed-mapped-list"
	edited := decodeList[tools.ProspectList](t, f.call("edit_prospect_list", tools.EditProspectListArgs{ProspectList: old, Name: &renamed}))
	if edited.ID != list.ID || edited.Name != renamed {
		t.Fatal(edited)
	}
	for _, name := range []string{"reject", "unreject"} {
		r := decodeList[tools.Reject](t, f.call(name, tools.RejectArgs{ProspectList: old, Candidate: id, Why: "chosen-reason"}))
		if r.ProspectList != list.ID || r.ProspectListName != renamed {
			t.Fatal(r)
		}
	}
	r := f.call("enrich", tools.EnrichArgs{Kind: "link_facts", ProspectList: old})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	p := decodeList[passes.Record](t, r)
	if p.Target != list.ID {
		t.Fatal(p)
	}
	f.waitEnrich(p.ID)
	deleted := decodeList[tools.ProspectList](t, f.call("delete_prospect_list", tools.ProspectListArgs{ProspectList: old}))
	if deleted.ID != list.ID {
		t.Fatal(deleted)
	}
	refusal(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: old, Peek: true}), passes.RefusalNotFound, tools.RefuseNotFound)
}

package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-RK5Q-ZW3K R-RMLJ-RFKY
func TestJudgeStartAndSchema(t *testing.T) {
	f := newFixture(t)
	f.seed(1)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	schemas := map[string]string{}
	for _, info := range infos {
		schemas[info.Name] = string(info.InputSchema)
	}
	if schemas["ingest"] != `{"type":"object","properties":{"scraper":{"type":"string"}},"additionalProperties":false}` || schemas["judge"] != `{"type":"object","additionalProperties":false}` {
		t.Fatal(schemas)
	}
	_ = tools.IngestArgs{"fmcsa"}
	busy, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call("judge", tools.NoArgs{}), passes.RefusalBusy, tools.RefuseBusy)
	if _, err = f.st.FinishPass(t.Context(), busy.ID, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	r := f.callAs(other, "judge", tools.NoArgs{})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	p := decodeList[passes.Record](t, r)
	if p.Kind != store.KindJudge || p.Origin != store.OriginJudge || p.Trigger != store.TriggerJudgeTool || p.Status != store.StatusRunning || p.User != other.UserID || p.Scraper != "" {
		t.Fatal(p)
	}
	if f.waitEnrich(p.ID).Status != store.StatusDone {
		t.Fatal(p)
	}
	f.eng.Drain(context.Background())
	refusal(t, f.call("judge", tools.NoArgs{}), passes.RefusalStopping, tools.RefuseStopping)
	f2 := newFixture(t)
	f2.d.SetFailing(true)
	refusal(t, f2.call("judge", json.RawMessage(`{}`)), passes.RefusalUnavailable, tools.RefuseUnavailable)
}

// R-SV30-JZ1Y
func TestNamedIngestMapsScraper(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	f := fixtureWithScrapes(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	for _, scraper := range []string{"nope", "FMCSA"} {
		refusal(t, f.call("ingest", tools.IngestArgs{Scraper: scraper}), passes.RefusalInvalid, tools.RefuseInvalid)
	}
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
	result := f.callAs(other, "ingest", tools.IngestArgs{"fmcsa"})
	if result.IsError() {
		t.Fatal(decode(t, result))
	}
	p := decodeList[passes.Record](t, result)
	if p.Scraper != "fmcsa" || p.Kind != store.KindIngest || p.Origin != store.OriginIngest || p.Trigger != store.TriggerIngest || p.Status != store.StatusRunning || p.User != other.UserID {
		t.Fatal(p)
	}
	<-entered
	refusal(t, f.call("ingest", tools.IngestArgs{"fmcsa"}), passes.RefusalBusy, tools.RefuseBusy)
	// The corpus ingest has a distinct target and therefore may run alongside it.
	all := f.call("ingest", tools.IngestArgs{})
	if all.IsError() {
		t.Fatal(decode(t, all))
	}
	if q := decodeList[passes.Record](t, all); q.Scraper != "" {
		t.Fatal(q)
	}
	close(release)
	f.eng.Drain(context.Background())
}

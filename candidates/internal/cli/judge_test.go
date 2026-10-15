package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// R-GT1J-G409 R-US0N-6HPJ
func TestRunJudgeDeliveryMakesNoSiblingRead(t *testing.T) {
	fake := &runFake{}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, fake)}))
	h.start()
	e := events.Event{ID: "evt_4444444444444444", Time: h.p.Now(), Service: "cron", Name: trail.AcceptedJudge, Attrs: events.Attrs{"trigger": "judge_candidates"}, Depth: 2, Cause: "evt_1111111111111111", Seq: 1, Received: h.p.Now()}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err = json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	env["attempt"] = 1
	b, err = json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	status, body := h.request("POST", "/events", "judge-request", b)
	var out map[string]string
	if err = json.Unmarshal(body, &out); err != nil || status != 200 || out["outcome"] != events.OutcomeOK {
		t.Fatal(status, string(body), err)
	}
	_, v := h.call(identity.Caller{UserID: "reader"}, "passes", map[string]any{})
	var ps []map[string]json.RawMessage
	if err = json.Unmarshal(v["passes"], &ps); err != nil || len(ps) != 1 {
		t.Fatal(v, err)
	}
	id := runText(ps[0], "id")
	p := h.waitPass(identity.Caller{UserID: "reader"}, id)
	if runText(p, "kind") != store.KindJudge || runText(p, "origin") != store.OriginCron || runText(p, "trigger") != store.TriggerJudge || runText(p, "status") != store.StatusDone {
		t.Fatal(p)
	}
	h.stop()
	runAssertSilent(t, h)
	if len(fake.Requests()) != 0 {
		t.Fatal(fake.Requests())
	}
	bus := h.bus.Events()
	if len(bus) != 2 {
		t.Fatal(bus)
	}
	for i, v := range bus {
		if v.Name != []string{trail.Started, trail.Finished}[i] || v.Attrs["pass"] != id || v.Attrs["kind"] != store.KindJudge || v.Attrs["origin"] != store.OriginCron || v.Attrs["trigger"] != store.TriggerJudge || v.RequestID != "judge-request" || v.User != "" || v.Cause != e.ID || v.Depth != e.Depth+1 {
			t.Fatal(v)
		}
		matches := 0
		for _, te := range h.telemetry.Events() {
			if te.Name == v.Name && te.RequestID == v.RequestID && te.User == v.User && reflect.DeepEqual(map[string]any(te.Attrs), map[string]any(v.Attrs)) {
				matches++
			}
		}
		if matches != 1 {
			t.Fatal(matches, v)
		}
	}
}

// R-ZSRV-9BFP
func TestRunReadsBothFixedSources(t *testing.T) {
	rows := map[string]map[string]any{"fmcsa": runRow(), "ppp": runRow()}
	rows["ppp"]["scraper"] = "ppp"
	rows["ppp"]["run"] = "run_ppp"
	rows["ppp"]["source_id"] = "ppp-id"
	rows["ppp"]["name"] = "Separate loan business"
	rows["ppp"]["facts"] = map[string]any{"amount": "150000.25", "date_approved": "04/01/2020", "naics": "230000"}
	var paths []string
	fake := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		for _, name := range []string{"fmcsa", "ppp"} {
			if r.URL.Path == "/scrapers/"+name+"/current" {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": rows[name]["run"], "scraper": name, "status": "done", "started": "2020-01-01T00:00:00Z", "rows": 1, "new": 1, "changed": 0, "unchanged": 0, "gone": 0})
				return
			}
			if r.URL.Path == "/runs/"+rows[name]["run"].(string)+"/rows" {
				_ = json.NewEncoder(w).Encode(rows[name])
				return
			}
		}
		w.WriteHeader(500)
	})
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, fake)}))
	h.start()
	c := identity.Caller{UserID: "owner"}
	r, p := h.call(c, "rebuild", map[string]any{})
	if r.IsError() {
		t.Fatal(p)
	}
	end := h.waitPass(c, runText(p, "id"))
	if runText(end, "status") != store.StatusDone {
		t.Fatal(end)
	}
	h.stop()
	runAssertSilent(t, h)
	wantPaths := []string{"/scrapers/fmcsa/current", "/runs/" + rows["fmcsa"]["run"].(string) + "/rows", "/scrapers/ppp/current", "/runs/run_ppp/rows"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatal(paths, wantPaths)
	}
	_, st := runStore(t, h.p.Dir, h.p.Now)
	cs, err := st.ListCandidates(context.Background(), store.CandidateFilter{}, "", "", 10)
	if err != nil || len(cs) != 2 {
		t.Fatal(cs, err)
	}
	seen := map[string]bool{}
	for _, c := range cs {
		links, err := st.Links(context.Background(), c.ID)
		if err != nil || len(links) != 1 {
			t.Fatal(links, err)
		}
		l := links[0]
		seen[l.Scraper] = true
		var src sources.Source = fmcsa.New()
		if l.Scraper == "ppp" {
			src = ppp.New()
		}
		if !reflect.DeepEqual(l.Reading, src.Read(runDecodedRow(rows[l.Scraper]))) {
			t.Fatal(l)
		}
	}
	if len(seen) != 2 || !seen["fmcsa"] || !seen["ppp"] {
		t.Fatal(seen)
	}
	for _, p := range paths {
		if strings.Contains(p, "record") {
			t.Fatal(p)
		}
	}
}

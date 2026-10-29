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
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// R-2R7Q-UI05 R-QZFG-HSHR
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
	if len(fake.Requests()) != 1 || fake.Requests()[0].Path != "/scrapers/mn_parcels/current" {
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

// R-1UAG-IOZE
func TestRunReadsFourFixedSources(t *testing.T) {
	rows := map[string]map[string]any{"fmcsa": runRow(), "ppp": runRow(), "form5500": runRow(), "osha_ita": runRow()}
	rows["ppp"]["scraper"] = "ppp"
	rows["ppp"]["run"] = "run_ppp"
	rows["ppp"]["source_id"] = "ppp-id"
	rows["ppp"]["name"] = "Separate loan business"
	rows["ppp"]["names"] = []string{"Separate loan business"}
	rows["ppp"]["facts"] = map[string]any{"amount": "150000.25", "date_approved": "04/01/2020", "naics": "230000"}
	rows["form5500"]["scraper"] = "form5500"
	rows["form5500"]["run"] = "run_form5500"
	rows["form5500"]["source_id"] = "filing-id"
	rows["form5500"]["name"] = "Separate filing business"
	rows["form5500"]["names"] = []string{"Separate filing business"}
	rows["form5500"]["facts"] = map[string]any{"ein": "410123456"}
	rows["osha_ita"]["scraper"] = "osha_ita"
	rows["osha_ita"]["run"] = "run_osha"
	rows["osha_ita"]["source_id"] = "summary-id"
	rows["osha_ita"]["name"] = "Separate summary business"
	rows["osha_ita"]["names"] = []string{"Separate summary business"}
	rows["osha_ita"]["facts"] = map[string]any{"ein": "410123457", "establishment_name": "Summary Site", "annual_average_employees": "23", "total_hours_worked": "47840", "year_filing_for": "2025"}
	var paths []string
	fake := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/scrapers/mn_parcels/current" {
			w.WriteHeader(404)
			return
		}
		paths = append(paths, r.URL.Path)
		for _, name := range []string{"fmcsa", "ppp", "form5500", "osha_ita"} {
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
	r, p := h.call(c, "ingest", map[string]any{})
	if r.IsError() {
		t.Fatal(p)
	}
	end := h.waitPass(c, runText(p, "id"))
	if runText(end, "status") != store.StatusDone {
		t.Fatal(end)
	}
	h.stop()
	runAssertSilent(t, h)
	wantPaths := []string{"/scrapers/fmcsa/current", "/runs/" + rows["fmcsa"]["run"].(string) + "/rows", "/scrapers/ppp/current", "/runs/run_ppp/rows", "/scrapers/form5500/current", "/runs/run_form5500/rows", "/scrapers/osha_ita/current", "/runs/run_osha/rows"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatal(paths, wantPaths)
	}
	_, st := runStore(t, h.p.Dir, h.p.Now)
	cs, err := st.ListCandidates(context.Background(), store.CandidateFilter{}, "", "", 10)
	if err != nil || len(cs) != 4 {
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
		if l.Scraper == "form5500" {
			src = form5500.New()
		}
		if l.Scraper == "osha_ita" {
			src = oshaita.New()
		}
		if !reflect.DeepEqual(l.Reading, src.Read(runDecodedRow(rows[l.Scraper]))) {
			t.Fatal(l)
		}
	}
	if len(seen) != 4 || !seen["fmcsa"] || !seen["ppp"] || !seen["form5500"] || !seen["osha_ita"] {
		t.Fatal(seen)
	}
	for _, p := range paths {
		if strings.Contains(p, "record") {
			t.Fatal(p)
		}
	}
}

// R-1UAG-IOZE
func TestRunFilingDeliveries(t *testing.T) {
	for _, src := range []sources.Source{form5500.New(), oshaita.New()} {
		t.Run(src.Name(), func(t *testing.T) { runFilingDelivery(t, src) })
	}
}

func runFilingDelivery(t *testing.T, src sources.Source) {
	row := runRow()
	row["scraper"], row["run"], row["source_id"] = src.Name(), "run_filing_delivery", "filing-delivery-row"
	row["facts"] = map[string]any{"ein": "410123456", "establishment_name": "Delivery Site", "annual_average_employees": "23", "total_hours_worked": "47840", "year_filing_for": "2025"}
	var paths []string
	currentSeen := make(chan struct{})
	fake := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/scrapers/mn_parcels/current":
			w.WriteHeader(404)
			close(currentSeen)
		case "/runs/run_filing_delivery/record":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "run_filing_delivery", "scraper": src.Name(), "status": "done", "started": "2020-01-01T00:00:00Z", "rows": 1, "new": 1, "changed": 0, "unchanged": 0, "gone": 0})
		case "/runs/run_filing_delivery/rows":
			_ = json.NewEncoder(w).Encode(row)
		default:
			w.WriteHeader(500)
		}
	})
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, fake)}))
	h.start()
	parcelSignal(t, currentSeen)
	event := events.Event{ID: "evt_6666666666666666", Time: h.p.Now(), Service: "scrapes", Name: "scrape.finished", Attrs: events.Attrs{"scraper": src.Name(), "run": "run_filing_delivery", "status": "done"}, Seq: 1, Received: h.p.Now()}
	b, err := json.Marshal(event)
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
	status, body := h.request("POST", "/events", "filing-delivery-request", b)
	var outcome map[string]string
	if err = json.Unmarshal(body, &outcome); err != nil || status != 200 || outcome["outcome"] != events.OutcomeOK {
		t.Fatal(status, string(body), err)
	}
	caller := identity.Caller{UserID: "reader"}
	_, value := h.call(caller, "passes", map[string]any{})
	var records []map[string]json.RawMessage
	if err = json.Unmarshal(value["passes"], &records); err != nil || len(records) == 0 {
		t.Fatal(value, err)
	}
	var passID string
	for _, record := range records {
		if runText(record, "kind") == store.KindIngest {
			passID = runText(record, "id")
		}
	}
	if passID == "" {
		t.Fatal(records)
	}
	done := h.waitPass(caller, passID)
	if runText(done, "status") != store.StatusDone {
		t.Fatal(done)
	}
	h.stop()
	runAssertSilent(t, h)
	if !reflect.DeepEqual(paths, []string{"/scrapers/mn_parcels/current", "/runs/run_filing_delivery/record", "/runs/run_filing_delivery/rows"}) {
		t.Fatal(paths)
	}
	_, st := runStore(t, h.p.Dir, h.p.Now)
	cs, err := st.ListCandidates(t.Context(), store.CandidateFilter{}, "", "", 10)
	if err != nil || len(cs) != 1 {
		t.Fatal(cs, err)
	}
	links, err := st.Links(t.Context(), cs[0].ID)
	if err != nil || len(links) != 1 || links[0].Scraper != src.Name() || !reflect.DeepEqual(links[0].Reading, src.Read(runDecodedRow(row))) {
		t.Fatal(links, err)
	}
}

package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

type parkSecrecyFake struct {
	mu            sync.Mutex
	fallback      http.Handler
	rows          []map[string]any
	enabled, fail bool
	echo          string
	hold          <-chan struct{}
	arrived       chan struct{}
}

func newParkSecrecyFake(t *testing.T, fallback http.Handler, base map[string]any, markers []string) *parkSecrecyFake {
	t.Helper()
	f := &parkSecrecyFake{fallback: fallback}
	for group, county := range []string{"LabelCounty", "ValueCounty", "CandidateCounty"} {
		for i := 0; i < 3; i++ {
			raw, err := json.Marshal(base)
			if err != nil {
				t.Fatal(err)
			}
			var row map[string]any
			if err := json.Unmarshal(raw, &row); err != nil {
				t.Fatal(err)
			}
			row["scraper"], row["run"], row["source_id"] = parks.Scraper, "parcel-secret-run", fmt.Sprintf("parcel-%d-%d", group, i)
			row["lat"], row["lon"] = 45.0+float64(group)*0.1+float64(i)*0.0001, -93.0
			row["address"].(map[string]any)["county"] = county
			row["people"] = []map[string]string{{"name": markers[5], "role": markers[6]}, {"name": markers[5], "role": parks.OwnerRole}}
			facts := row["facts"].(map[string]any)
			facts["owner_name"], facts["tax_name"], facts["use_class"] = markers[5], markers[9], []string{markers[9]}
			facts["homestead"], facts["emv_bldg"] = "No", int64(0)
			if group == 1 {
				facts["emv_bldg"] = parks.SeedValueThreshold + 1
			}
			f.rows = append(f.rows, row)
		}
	}
	return f
}

func (f *parkSecrecyFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/scrapers/"+parks.Scraper+"/current" && !strings.HasPrefix(r.URL.Path, "/runs/parcel-secret-run/") {
		f.fallback.ServeHTTP(w, r)
		return
	}
	f.mu.Lock()
	enabled, fail, rows, echo, hold, arrived := f.enabled, f.fail, f.rows, f.echo, f.hold, f.arrived
	f.mu.Unlock()
	if !enabled {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if fail {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, echo)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/rows") {
		if arrived != nil {
			select {
			case arrived <- struct{}{}:
			default:
			}
		}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		for _, row := range rows {
			_ = json.NewEncoder(w).Encode(row)
		}
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "parcel-secret-run", "scraper": parks.Scraper, "status": "done", "started": "2026-01-01T00:00:00Z", "finished": "2026-01-01T01:00:00Z", "rows": len(rows), "new": len(rows), "changed": 0, "unchanged": 0, "gone": 0})
}

// R-MJ56-QL88: extend the complete run secrecy scenario on that same run.
func exerciseRunParkSecrecy(t *testing.T, h *runHarness, f *parkSecrecyFake, markers []string, forbidden *[]string, call func(string, any, bool) map[string]json.RawMessage, expected map[string]string, answers *[][]byte) {
	t.Helper()
	f.mu.Lock()
	f.enabled = true
	f.echo = strings.Join(*forbidden, " ")
	f.mu.Unlock()
	edit := map[string]any{"county": "LabelCounty", "labels": []string{markers[9]}}
	invalid, _ := h.call(identity.Caller{UserID: "reader", Email: "reader@example.test", RequestID: "secrecy-request"}, "edit_park_rules", map[string]any{"labels": []string{markers[9]}})
	invalidJSON, _ := json.Marshal(invalid)
	if !invalid.IsError() || !strings.Contains(string(invalidJSON), strconv.Quote(passes.RefusalInvalid+": "+tools.RefuseInvalid)) {
		t.Fatal("parks edit was not invalid")
	}
	secrecyAbsent(t, "invalid parks edit", invalid, *forbidden)
	start := func() string {
		t.Helper()
		v := call("edit_park_rules", edit, false)
		var p passes.Record
		if err := json.Unmarshal(v["pass"], &p); err != nil {
			t.Fatal(err)
		}
		expected[p.ID] = "secrecy-request"
		return p.ID
	}
	caller := identity.Caller{UserID: "reader", Email: "reader@example.test", RequestID: "secrecy-request"}
	firstID := start()
	if p := h.waitPass(caller, firstID); runText(p, "status") != store.StatusDone {
		t.Fatal("parks derivation failed", p)
	}
	all := call("parks", map[string]any{}, false)
	var held []tools.Park
	if err := json.Unmarshal(all["parks"], &held); err != nil || len(held) < 2 {
		t.Fatal("marker parks missing", err)
	}
	rules := map[string]bool{}
	for _, park := range held {
		detail := call("park", map[string]any{"park": park.ID}, false)
		var parcels []tools.ParkParcel
		if err := json.Unmarshal(detail["parcels"], &parcels); err != nil {
			t.Fatal(err)
		}
		for _, parcel := range parcels {
			rules[parcel.Rule] = true
			if len(parcel.Owners) != 1 || parcel.Owners[0] != markers[5] || parcel.Street != markers[3] || parcel.City != markers[4] {
				t.Fatal("parcel markers did not reach parks")
			}
			if parcel.Rule == parks.RuleCandidate && parcel.Zip != "55072" {
				t.Fatal("candidate parcel address mismatch")
			}
		}
		if status, body := h.request("GET", "/parks/"+park.ID, "park-page", nil); status != 200 || !strings.Contains(string(body), markers[5]) {
			t.Fatal("marker park page missing", status)
		}
	}
	for _, rule := range []string{parks.RuleLabel, parks.RuleValue, parks.RuleCandidate} {
		if !rules[rule] {
			t.Fatalf("no cluster selected by %s", rule)
		}
	}
	call("park", map[string]any{"park": "absent"}, true)
	call("park_rules", map[string]any{}, false)
	call("park_rules", []any{}, true)
	first := call("parks", map[string]any{"limit": 1}, false)
	token := runText(first, "next")
	if token == "" {
		t.Fatal("parks next token absent")
	}
	*forbidden = append(*forbidden, token)
	second := call("parks", map[string]any{"limit": 1, "page": token}, false)
	if token := runText(second, "next"); token != "" {
		*forbidden = append(*forbidden, token)
	}
	call("parks", map[string]any{"page": markers[0]}, true)
	secondPark := held[1]
	key := ""
	if secondPark.Minutes != nil {
		key = strconv.FormatInt(*secondPark.Minutes, 10)
	}
	before := (paging.Token{Tool: "parks-before"}).Encode(parks.SortMinutes, key, secondPark.ID)
	*forbidden = append(*forbidden, before)
	paths := []string{"/parks", "/parks?page=" + url.QueryEscape(token), "/parks?page=" + url.QueryEscape(before)}
	for _, county := range []string{"LabelCounty", "ValueCounty", "CandidateCounty"} {
		paths = append(paths, "/parks?county="+url.QueryEscape(county))
	}
	for _, pair := range []struct {
		key    string
		values []string
	}{{"walked", []string{parks.WalkedAll, parks.WalkedUnwalked, parks.WalkedWalked}}, {"gone", []string{parks.GoneActive, parks.GoneGone, parks.GoneAll}}, {"sort", []string{parks.SortMinutes, parks.SortValue, parks.SortParcels}}} {
		for _, value := range pair.values {
			paths = append(paths, "/parks?"+pair.key+"="+url.QueryEscape(value))
		}
	}
	for _, path := range paths {
		if status, _ := h.request("GET", path, "parks-view", nil); status != 200 && (path != "/parks?page="+url.QueryEscape(before) || status != 303) {
			t.Fatalf("parks view status %d", status)
		}
	}
	hold, arrived := make(chan struct{}), make(chan struct{}, 1)
	f.mu.Lock()
	f.hold, f.arrived = hold, arrived
	f.mu.Unlock()
	heldID := start()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("parks rows not held")
	}
	result, _ := h.call(caller, "edit_park_rules", edit)
	busyJSON, _ := json.Marshal(result)
	if !result.IsError() || !strings.Contains(string(busyJSON), strconv.Quote(passes.RefusalBusy+": "+tools.RefuseBusy)) {
		t.Fatal("parks edit was not busy")
	}
	secrecyAbsent(t, "busy parks edit", result, *forbidden)
	close(hold)
	h.waitPass(caller, heldID)
	f.mu.Lock()
	f.hold, f.arrived, f.fail = nil, nil, true
	f.mu.Unlock()
	failedID := start()
	if p := h.waitPass(caller, failedID); runText(p, "status") != store.StatusFailed || runText(p, "reason") != store.ReasonScrapes {
		t.Fatal("parks scrapes failure absent")
	}
	f.mu.Lock()
	f.fail = false
	f.mu.Unlock()
	for i, status := range []string{"failed", "done"} {
		e := events.Event{ID: fmt.Sprintf("evt_%016x", i+801), Time: h.p.Now(), Service: "scrapes", Name: "scrape.finished", Attrs: events.Attrs{"scraper": parks.Scraper, "run": "parcel-secret-run", "status": status}, Seq: 1, Received: h.p.Now()}
		raw, _ := json.Marshal(e)
		var env map[string]any
		_ = json.Unmarshal(raw, &env)
		env["attempt"] = 1
		raw, _ = json.Marshal(env)
		code, body := h.request("POST", "/events", "parks-delivery", raw)
		if code != 200 {
			t.Fatal("parks delivery failed", code)
		}
		*answers = append(*answers, body)
	}
	ps := call("passes", map[string]any{}, false)
	var list []passes.Record
	if err := json.Unmarshal(ps["passes"], &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Kind == store.KindParks {
			if p.Origin == store.OriginRun {
				expected[p.ID] = "parks-delivery"
				h.waitPass(caller, p.ID)
			}
			if code, _ := h.request("GET", "/passes/"+p.ID, "parks-pass-page", nil); code != 200 {
				t.Fatal("parks pass page failed")
			}
		}
	}
	// Error results collected by the parent scenario include all refusals except
	// the busy call above, which is searched immediately with the same vocabulary.
}

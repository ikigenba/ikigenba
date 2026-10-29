package cli_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// runParcelRows is the fake parcel run vocabulary: seeded label cluster,
// fallback value match, boundary value refusal, and homesteaded refusal.
// R-LBQG-C74B
func runParcelRows(t *testing.T) []map[string]any {
	t.Helper()
	d, st := runStore(t, t.TempDir(), func() time.Time { return time.Unix(100000, 0) })
	rules, counties, err := st.ParkRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	county, label := "", ""
	for _, c := range counties {
		if len(rules.Labels[c]) > 0 {
			county, label = c, rules.Labels[c][0]
			break
		}
	}
	if county == "" {
		t.Fatal("no seed labels")
	}
	rows := []map[string]any{}
	for i := range parks.SeedMinParcels + 3 {
		row := runRow()
		pin := fmt.Sprintf("parcel-pin-%d", i)
		row["scraper"] = parks.Scraper
		row["run"] = "parcel_run"
		row["source_id"] = pin
		row["name"] = pin
		row["names"] = []string{}
		c := county
		if i >= parks.SeedMinParcels {
			c = "Fallback County"
		}
		row["address"] = map[string]any{"street": fmt.Sprintf("%d Industrial Avenue", i), "city": "Test Town", "state": "MN", "zip": "55072", "county": c}
		row["lat"] = 45.0 + float64(i)*0.0001
		row["lon"] = -93.0
		use, home, value := []string{label}, "Yes", int64(100)
		if i >= parks.SeedMinParcels {
			use = []string{"unmatched"}
			home = "No"
			value = parks.SeedValueThreshold + 1
		}
		if i == parks.SeedMinParcels+1 {
			value = parks.SeedValueThreshold
		}
		if i == parks.SeedMinParcels+2 {
			home = "Yes"
		}
		row["facts"] = map[string]any{"state_pin": pin, "use_class": use, "homestead": home, "emv_bldg": value}
		rows = append(rows, row)
	}
	return rows
}

type runParcelFake struct {
	mu          sync.Mutex
	requests    []runRequest
	rows        []map[string]any
	current     bool
	initial404  bool
	status      int
	holdCurrent <-chan struct{}
	holdRows    <-chan struct{}
	currentSeen chan runRequest
	rowsSeen    chan struct{}
	cancelled   chan struct{}
}

func (f *runParcelFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, runRequest{r.URL.Path, r.Header.Clone()})
	n := len(f.requests)
	f.mu.Unlock()
	if r.URL.Path == "/scrapers/mn_parcels/current" {
		if f.currentSeen != nil {
			select {
			case f.currentSeen <- runRequest{r.URL.Path, r.Header.Clone()}:
			default:
			}
		}
		if f.holdCurrent != nil {
			select {
			case <-f.holdCurrent:
			case <-r.Context().Done():
				if f.cancelled != nil {
					close(f.cancelled)
				}
				return
			}
		}
		if !f.current || f.initial404 && n == 1 {
			w.WriteHeader(404)
			return
		}
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/rows") {
		if f.rowsSeen != nil {
			select {
			case f.rowsSeen <- struct{}{}:
			default:
			}
		}
		if f.holdRows != nil {
			select {
			case <-f.holdRows:
			case <-r.Context().Done():
				return
			}
		}
		for _, row := range f.rows {
			_ = json.NewEncoder(w).Encode(row)
		}
		return
	}
	if r.URL.Path != "/scrapers/mn_parcels/current" && r.URL.Path != "/runs/parcel_run/record" {
		w.WriteHeader(404)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "parcel_run", "scraper": parks.Scraper, "status": store.StatusDone, "started": "2000-01-01T00:00:00Z", "rows": len(f.rows), "new": 0, "changed": 0, "unchanged": 0, "gone": 0})
}
func (f *runParcelFake) Requests() []runRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runRequest(nil), f.requests...)
}
func parcelSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("parcel fake signal")
	}
}
func parcelCurrent(t *testing.T, ch <-chan runRequest) runRequest {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("parcel current missing")
		return runRequest{}
	}
}
func parcelPass(t *testing.T, h *runHarness, origin string) map[string]json.RawMessage {
	t.Helper()
	c := identity.Caller{UserID: "park_reader", RequestID: "parcel_read_request"}
	for range 10000 {
		r, v := h.call(c, "passes", map[string]any{})
		if r.IsError() {
			t.Fatal(r)
		}
		var ps []map[string]json.RawMessage
		_ = json.Unmarshal(v["passes"], &ps)
		for _, p := range ps {
			if runText(p, "kind") == store.KindParks && runText(p, "origin") == origin {
				return h.waitPass(c, runText(p, "id"))
			}
		}
	}
	t.Fatal("parks pass missing")
	return nil
}
func parcelDeliver(t *testing.T, h *runHarness, id, q string, depth int) {
	t.Helper()
	e := events.Event{ID: id, Time: h.p.Now(), Service: "scrapes", Name: trail.Accepted, Attrs: events.Attrs{"scraper": parks.Scraper, "run": "parcel_run", "status": store.StatusDone}, Depth: depth, Seq: 1, Received: h.p.Now()}
	if depth > 0 {
		e.Cause = "evt_eeeeeeeeeeeeeeee"
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["attempt"] = 1
	b, _ = json.Marshal(m)
	status, answer := h.request("POST", "/events", q, b)
	var out map[string]string
	_ = json.Unmarshal(answer, &out)
	if status != 200 || out["outcome"] != events.OutcomeOK {
		t.Fatal(status, string(answer))
	}
}
func assertParcelLifecycle(t *testing.T, h *runHarness, p map[string]json.RawMessage, q, user, cause string, depth int) {
	t.Helper()
	id := runText(p, "id")
	var found []events.Event
	for _, e := range h.bus.Events() {
		if e.Attrs["pass"] == id {
			found = append(found, e)
		}
	}
	if len(found) != 2 {
		t.Fatal(found)
	}
	for i, e := range found {
		if e.Name != []string{trail.Started, trail.Finished}[i] || e.RequestID != q || e.User != user || e.Cause != cause || e.Depth != depth || e.Attrs["kind"] != store.KindParks || e.Attrs["origin"] != runText(p, "origin") || e.Attrs["trigger"] != runText(p, "trigger") || e.Attrs["target"] != "" || e.Attrs["enrich_kind"] != "" {
			t.Fatal(e, p)
		}
		matches := 0
		for _, te := range h.telemetry.Events() {
			if te.Name == e.Name && te.Attrs["pass"] == id && te.RequestID == e.RequestID && te.User == e.User && reflect.DeepEqual(map[string]any(te.Attrs), map[string]any(e.Attrs)) {
				matches++
			}
		}
		if matches != 1 {
			t.Fatal("trail pair", matches)
		}
	}
	for _, k := range []string{"status", "reason", "parcel_run", "rows", "new", "changed", "unchanged", "gone", "created", "linked", "judged", "unranged", "derived", "due", "fetched", "https", "http", "none", "runs", "runs_failed", "pages", "cost", "parcels_read", "by_label", "by_value", "by_candidate", "parks_created", "parks_updated", "parks_gone", "parks_grown"} {
		b, _ := json.Marshal(found[1].Attrs[k])
		want := p[k]
		if len(want) == 0 {
			want = json.RawMessage(`""`)
		}
		if string(b) != string(want) {
			t.Fatal(k, string(b), string(want))
		}
	}
}

// R-73LI-FCRL R-761B-6W8Z R-MFHH-LA05 R-S80V-Z4GK R-S4D6-TT8H
func TestRunStartsParksInBackgroundWithOwnRequest(t *testing.T) {
	release := make(chan struct{})
	f := &runParcelFake{rows: runParcelRows(t), current: true, holdRows: release, currentSeen: make(chan runRequest, 1), rowsSeen: make(chan struct{}, 1)}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.p.Rand = &runBytes{n: 0x5a, fixed: true}
	h.start()
	initial := parcelCurrent(t, f.currentSeen)
	parcelSignal(t, f.rowsSeen)
	if initial.Header.Get("X-User-Id") != "candidates" || initial.Header.Get("X-User-Email") != "" || initial.Header.Get("X-Request-Id") != strings.Repeat("5a", 16) {
		t.Fatal(initial)
	}
	d, st := runStore(t, h.p.Dir, h.p.Now)
	running, err := st.RunningPasses(context.Background())
	if err != nil || len(running) != 1 || running[0].Kind != store.KindParks || running[0].Origin != store.OriginStart || running[0].Trigger != "parcel_run" || running[0].RequestID != initial.Header.Get("X-Request-Id") {
		t.Fatal(running, err)
	}
	_ = d.Close()
	close(release)
	p := parcelStoredPass(t, h, store.OriginStart)
	if runText(p, "status") != store.StatusDone || runText(p, "parcel_run") != "parcel_run" {
		t.Fatal(p)
	}
	h.stop()
	q := initial.Header.Get("X-Request-Id")
	assertParcelLifecycle(t, h, p, q, "", "", 0)
	reqs := f.Requests()
	if len(reqs) != 2 {
		t.Fatal(reqs)
	}
	seen := 0
	paths := map[string]int{}
	for _, r := range reqs {
		paths[r.Path]++
	}
	for _, e := range h.telemetry.Events() {
		if e.RequestID != q {
			continue
		}
		if e.Name == "sibling.called" {
			seen++
			path, _ := e.Attrs["path"].(string)
			if paths[path] == 0 {
				t.Fatal(e, reqs)
			}
			paths[path]--
			if e.User != "candidates" || e.Attrs["target"] != "scrapes" || e.Attrs["method"] != "GET" {
				t.Fatal(e)
			}
		} else if e.Name != trail.Started && e.Name != trail.Finished {
			t.Fatal(e)
		}
	}
	for _, n := range paths {
		if n != 0 {
			t.Fatal(paths)
		}
	}
	if seen != len(reqs) {
		t.Fatal(seen, reqs)
	}
	for _, r := range reqs {
		if r.Header.Get("X-User-Id") != "candidates" || r.Header.Get("X-User-Email") != "" {
			t.Fatal(r)
		}
		if r.Header.Get("X-Request-Id") != q {
			t.Fatal(r)
		}
	}
}

// R-ME9L-7I9G R-761B-6W8Z
func TestRunMissingParcelCurrentRecordsOnlyStartRead(t *testing.T) {
	for _, mode := range []string{"404", "offline"} {
		t.Run(mode, func(t *testing.T) {
			f := &runParcelFake{current: false, currentSeen: make(chan runRequest, 1)}
			socket := runSocket(t, f)
			if mode == "offline" {
				socket = missingParcelSocket(t)
			}
			h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": socket}))
			h.p.Rand = &runBytes{n: 0x5a, fixed: true}
			h.start()
			if mode == "404" {
				parcelCurrent(t, f.currentSeen)
			}
			r, _ := h.call(identity.Caller{UserID: "reader", RequestID: "reader_request"}, "passes", map[string]any{})
			if r.IsError() {
				t.Fatal(r)
			}
			h.stop()
			_, st := runStore(t, h.p.Dir, h.p.Now)
			ps, err := st.ListPasses(context.Background(), "", "", 10)
			if err != nil || len(ps) != 0 {
				t.Fatal(ps, err)
			}
			count := 0
			for _, e := range h.telemetry.Events() {
				if e.Name == "sibling.called" && e.Attrs["target"] == "scrapes" {
					count++
					if e.Attrs["path"] != "/scrapers/mn_parcels/current" || e.RequestID != strings.Repeat("5a", 16) || e.User != "candidates" {
						t.Fatal(e)
					}
					want := int64(404)
					if mode == "offline" {
						want = 0
					}
					if e.Attrs["status"] != want {
						t.Fatal(e)
					}
				}
			}
			if count != 1 {
				t.Fatal(count)
			}
		})
	}
}

type startDraws struct {
	mu   sync.Mutex
	n    byte
	last []byte
}

func (r *startDraws) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range b {
		b[i] = r.n
		r.n++
	}
	r.last = append([]byte(nil), b...)
	return len(b), nil
}
func (r *startDraws) Last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return hex.EncodeToString(r.last)
}

// R-QDQW-B0ZA R-7797-KNZO R-373Q-6C3X
func TestRunSettlesBeforeNonblockingCancellableStartRead(t *testing.T) {
	held := make(chan struct{})
	f := &runParcelFake{current: true, holdCurrent: held, currentSeen: make(chan runRequest, 1), cancelled: make(chan struct{})}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	rnd := &startDraws{}
	h.p.Rand = rnd
	d, st := runStore(t, h.p.Dir, h.p.Now)
	p, err := st.AddPass(context.Background(), store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "prior_owner"})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	h.start()
	initial := parcelCurrent(t, f.currentSeen)
	if initial.Header.Get("X-Request-Id") != rnd.Last() || len(initial.Header.Get("X-Request-Id")) != 32 {
		t.Fatal(initial, rnd.Last())
	}
	d, st = runStore(t, h.p.Dir, h.p.Now)
	ended, err := st.GetPass(context.Background(), p.ID)
	if err != nil || ended.Status != store.StatusFailed || ended.Reason != store.ReasonInterrupted {
		t.Fatal(ended, err)
	}
	_ = d.Close()
	if status, _ := h.request("GET", "/", "while-start-held", nil); status != 200 {
		t.Fatal(status)
	}
	r, _ := h.call(identity.Caller{UserID: "reader", RequestID: "reader_request"}, "passes", map[string]any{})
	if r.IsError() {
		t.Fatal(r)
	}
	if code := h.stop(); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	parcelSignal(t, f.cancelled)
	reqs := f.Requests()
	if len(reqs) != 1 {
		t.Fatal(reqs)
	}
	for _, e := range h.bus.Events() {
		if e.Attrs["kind"] == store.KindParks {
			t.Fatal(e)
		}
	}
	events := h.telemetry.Events()
	if len(events) == 0 || events[0].Name != "service.started" {
		t.Fatal(events)
	}
	recovered := false
	for _, e := range events {
		if e.Name == trail.Finished && e.Attrs["pass"] == p.ID {
			recovered = true
		}
		if (e.Name == "request.started" || e.Name == "sibling.called") && !recovered {
			t.Fatal("read before recover", e)
		}
	}
}

// R-S6SZ-LCPV R-MHXA-CTHJ
func TestRunParcelDeliveryAndRulesLifecycle(t *testing.T) {
	f := &runParcelFake{rows: runParcelRows(t), current: true, initial404: true, currentSeen: make(chan runRequest, 3)}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.start()
	initial := parcelCurrent(t, f.currentSeen)
	caller := identity.Caller{UserID: "rules_owner", Email: "rules@example.test", RequestID: "rules_request"}
	result, v := h.call(caller, "edit_park_rules", map[string]any{})
	if result.IsError() {
		t.Fatal(result)
	}
	var pass map[string]json.RawMessage
	_ = json.Unmarshal(v["pass"], &pass)
	p := h.waitPass(caller, runText(pass, "id"))
	if runText(p, "status") != store.StatusDone {
		t.Fatal(p)
	}
	parcelDeliver(t, h, "evt_abababababababab", "delivery_request", 3)
	dp := parcelPass(t, h, store.OriginRun)
	if runText(dp, "status") != store.StatusDone {
		t.Fatal(dp)
	}
	h.stop()
	assertParcelLifecycle(t, h, p, caller.RequestID, caller.UserID, "", 0)
	assertParcelLifecycle(t, h, dp, "delivery_request", "", "evt_abababababababab", 4)
	q := initial.Header.Get("X-Request-Id")
	counts := map[string]int{}
	for _, r := range f.Requests() {
		id := r.Header.Get("X-Request-Id")
		counts[id]++
		want := "candidates"
		if id == caller.RequestID {
			want = caller.UserID
		}
		if r.Header.Get("X-User-Id") != want || id != q && id != caller.RequestID && id != "delivery_request" {
			t.Fatal(r)
		}
	}
	if counts[q] != 1 || counts[caller.RequestID] < 1 || counts["delivery_request"] < 1 {
		t.Fatal(counts)
	}
	trails := map[string]int{}
	for _, e := range h.telemetry.Events() {
		if e.Name == "sibling.called" {
			trails[e.RequestID]++
		}
	}
	if !reflect.DeepEqual(counts, trails) {
		t.Fatal(counts, trails)
	}
}

// R-S5L3-7KZ6
func TestRunRulesLifecycleAfterStart(t *testing.T) {
	f := &runParcelFake{rows: runParcelRows(t), current: true}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.start()
	parcelPass(t, h, store.OriginStart)
	c := identity.Caller{UserID: "rules_owner", RequestID: "rules_only"}
	r, v := h.call(c, "edit_park_rules", map[string]any{})
	if r.IsError() {
		t.Fatal(r)
	}
	var p map[string]json.RawMessage
	_ = json.Unmarshal(v["pass"], &p)
	p = h.waitPass(c, runText(p, "id"))
	h.stop()
	assertParcelLifecycle(t, h, p, c.RequestID, c.UserID, "", 0)
}

// R-21LU-TBFK
func TestRunParcelDeliveryUnavailableStillAcknowledges(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": missingParcelSocket(t)}))
	h.start()
	parcelDeliver(t, h, "evt_acacacacacacacac", "unavailable_request", 0)
	p := parcelPass(t, h, store.OriginRun)
	if runText(p, "status") != store.StatusFailed || runText(p, "reason") != store.ReasonScrapes {
		t.Fatal(p)
	}
	h.stop()
}

func parcelStoredPass(t *testing.T, h *runHarness, origin string) map[string]json.RawMessage {
	t.Helper()
	d, st := runStore(t, h.p.Dir, h.p.Now)
	defer func() { _ = d.Close() }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		ps, err := st.ListPasses(context.Background(), "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.Kind == store.KindParks && p.Origin == origin && p.Status != store.StatusRunning {
				b, _ := json.Marshal(passes.RecordOf(p))
				var v map[string]json.RawMessage
				_ = json.Unmarshal(b, &v)
				return v
			}
		}
		select {
		case <-deadline.C:
			t.Fatal("stored parks pass never ended")
		default:
			runtime.Gosched()
		}
	}
}

// R-27PC-Q651
func TestRunEmptyParcelDeliveryStartsOnlyParks(t *testing.T) {
	f := &runParcelFake{}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.start()
	parcelDeliver(t, h, "evt_cacacacacacacaca", "empty_parcels", 0)
	r, v := h.call(identity.Caller{UserID: "reader"}, "passes", map[string]any{})
	var ps []passes.Record
	if err := json.Unmarshal(v["passes"], &ps); err != nil || r.IsError() {
		t.Fatal(r, err)
	}
	matched := 0
	for _, p := range ps {
		if p.Kind == store.KindIngest {
			t.Fatal("parcel delivery recorded an ingest", p)
		}
		if p.Trigger == "parcel_run" {
			matched++
			if p.Kind != store.KindParks || p.Origin != store.OriginRun {
				t.Fatal(p)
			}
		}
	}
	if matched != 1 {
		t.Fatal("parcel delivery pass count", matched, ps)
	}
	h.stop()
}

// R-20DY-FJOV R-TQ38-2EBU
func TestRunFreshNoncurrentParcelDeliveryExactDerivation(t *testing.T) {
	rows := runParcelRows(t)
	noPoint := runParcelRows(t)[0]
	noPoint["source_id"] = "no-point-industrial"
	noPoint["facts"].(map[string]any)["state_pin"] = "no-point-industrial"
	delete(noPoint, "lat")
	delete(noPoint, "lon")
	other := runParcelRows(t)[len(rows)-1]
	other["source_id"] = "distinct-county-parcel"
	other["facts"].(map[string]any)["state_pin"] = "distinct-county-parcel"
	other["address"].(map[string]any)["county"] = "Distinct Nonindustrial County"
	rows = append(rows, noPoint, other)
	f := &runParcelFake{rows: rows, current: false}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, f)}))
	h.start()
	parcelDeliver(t, h, "evt_bcbcbcbcbcbcbcbc", "fresh_parcels", 0)
	p := parcelPass(t, h, store.OriginRun)
	if runText(p, "parcel_run") != "parcel_run" || runText(p, "status") != store.StatusDone {
		t.Fatal(p)
	}
	d, st := runStore(t, h.p.Dir, h.p.Now)
	rules, _, err := st.ParkRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs, err := st.ParkCountiesRead(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantCounties := []string{}
	seenCounties := map[string]bool{}
	for _, row := range rows {
		c := row["address"].(map[string]any)["county"].(string)
		if c != "" && !seenCounties[c] {
			seenCounties[c] = true
			wantCounties = append(wantCounties, c)
		}
	}
	sort.Strings(wantCounties)
	if !reflect.DeepEqual(cs, wantCounties) {
		t.Fatal(cs, wantCounties)
	}
	_ = d.Close()
	var kept []parks.Parcel
	var label, value, candidate int64
	for _, m := range rows {
		addr := m["address"].(map[string]any)
		var lat, lon *float64
		if v, ok := m["lat"].(float64); ok {
			lat = &v
		}
		if v, ok := m["lon"].(float64); ok {
			lon = &v
		}
		r := scrapes.Row{Scraper: parks.Scraper, SourceID: m["source_id"].(string), Address: scrapes.Address{Street: addr["street"].(string), City: addr["city"].(string), Zip: addr["zip"].(string), County: addr["county"].(string)}, Lat: lat, Lon: lon, Facts: map[string]json.RawMessage{}}
		for k, v := range m["facts"].(map[string]any) {
			r.Facts[k], _ = json.Marshal(v)
		}
		parcel := parks.ParcelOf(r)
		parcel.Rule = parks.Classify(parcel, rules, nil)
		switch parcel.Rule {
		case parks.RuleLabel:
			label++
		case parks.RuleValue:
			value++
		case parks.RuleCandidate:
			candidate++
		}
		if parcel.Rule != "" {
			kept = append(kept, parcel)
		}
	}
	plan := parks.Match(parks.Clusters(kept, rules), nil)
	want := map[string]int64{"parcels_read": int64(len(rows)), "by_label": label, "by_value": value, "by_candidate": candidate, "parks_created": int64(len(plan.News)), "parks_updated": int64(len(plan.Updates)), "parks_gone": int64(len(plan.Gone)), "parks_grown": plan.Grown}
	for _, key := range []string{"rows", "new", "changed", "unchanged", "gone", "created", "linked", "judged", "unranged", "derived", "due", "fetched", "https", "http", "none", "runs", "runs_failed", "pages", "cost"} {
		want[key] = 0
	}
	for key, n := range want {
		if string(p[key]) != fmt.Sprint(n) {
			t.Fatal(key, string(p[key]), n)
		}
	}
	total := 0
	args := map[string]any{}
	for {
		r, v := h.call(identity.Caller{UserID: "reader", RequestID: "read_parks"}, "parks", args)
		if r.IsError() {
			t.Fatal(r)
		}
		var ps []map[string]json.RawMessage
		_ = json.Unmarshal(v["parks"], &ps)
		total += len(ps)
		for _, park := range ps {
			if runText(park, "run") != "parcel_run" {
				t.Fatal(park)
			}
		}
		next := runText(v, "next")
		if next == "" {
			break
		}
		args["page"] = next
	}
	if total != len(plan.News) {
		t.Fatal(total, plan)
	}
	h.stop()
	for _, r := range f.Requests() {
		if r.Header.Get("X-User-Id") != "candidates" || r.Header.Get("X-User-Email") != "" {
			t.Fatal(r)
		}
		if strings.HasPrefix(r.Path, "/runs/") && r.Header.Get("X-Request-Id") != "fresh_parcels" {
			t.Fatal(r)
		}
	}
}

func missingParcelSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "parcel-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "absent")
}

// R-761B-6W8Z
func TestRunWithoutScrapesDoesNoParksWork(t *testing.T) {
	for _, mode := range []string{"unset", "absent", "invalid", "no_scrapes"} {
		t.Run(mode, func(t *testing.T) {
			path := ""
			switch mode {
			case "absent":
				path = filepath.Join(t.TempDir(), "missing")
			case "invalid":
				path = filepath.Join(t.TempDir(), "services")
				if err := os.WriteFile(path, []byte("invalid services bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "no_scrapes":
				path = runServices(t, map[string]string{})
			}
			h := newRunHarness(t, t.TempDir(), path)
			if mode == "unset" {
				delete(h.env, "IKIGENBA_SERVICES")
			}
			h.start()
			h.stop()
			d, st := runStore(t, h.p.Dir, h.p.Now)
			defer func() { _ = d.Close() }()
			ps, err := st.ListPasses(context.Background(), "", "", 10)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range ps {
				if p.Kind == store.KindParks {
					t.Fatal(p)
				}
			}
			for _, e := range h.telemetry.Events() {
				if e.Name == "sibling.called" && e.Attrs["target"] == "scrapes" {
					t.Fatal(e)
				}
			}
		})
	}
}

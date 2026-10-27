package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
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

type parkSecrecyDiagnostics struct{ writes chan string }

func (w *parkSecrecyDiagnostics) Write(p []byte) (int, error) {
	w.writes <- string(p)
	return len(p), nil
}

type handlerParkSecrecy struct {
	parkID, firstID, heldID string
	hold                    chan struct{}
}

// R-MKD3-4CYX: use the same failing handler and complete marker scenario.
func beginHandlerParkSecrecy(t *testing.T, f *parkSecrecyFake, markers []string, call func(string, any, bool) map[string]json.RawMessage, wait func(string, string)) *handlerParkSecrecy {
	t.Helper()
	f.mu.Lock()
	f.enabled = true
	f.mu.Unlock()
	args := map[string]any{"county": "LabelCounty", "labels": []string{markers[9]}}
	start := func() string {
		t.Helper()
		v := call("edit_park_rules", args, false)
		var p passes.Record
		if err := json.Unmarshal(v["pass"], &p); err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	state := &handlerParkSecrecy{firstID: start()}
	wait(state.firstID, "")
	listed := call("parks", map[string]any{}, false)
	var held []tools.Park
	if err := json.Unmarshal(listed["parks"], &held); err != nil || len(held) == 0 {
		t.Fatal("no marker park", err)
	}
	state.parkID = held[0].ID
	rules := map[string]bool{}
	for _, p := range held {
		detail := call("park", map[string]any{"park": p.ID}, false)
		var parcels []tools.ParkParcel
		if err := json.Unmarshal(detail["parcels"], &parcels); err != nil {
			t.Fatal(err)
		}
		for _, q := range parcels {
			rules[q.Rule] = true
			if len(q.Owners) != 1 || q.Owners[0] != markers[5] || q.Street != markers[3] {
				t.Fatal("parcel markers missing")
			}
		}
	}
	for _, rule := range []string{parks.RuleLabel, parks.RuleValue, parks.RuleCandidate} {
		if !rules[rule] {
			t.Fatal("parcel classification condition absent", rule)
		}
	}
	state.hold = make(chan struct{})
	arrived := make(chan struct{}, 1)
	f.mu.Lock()
	f.hold, f.arrived = state.hold, arrived
	f.mu.Unlock()
	state.heldID = start()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("parks rows not held before first line")
	}
	return state
}

func (s *handlerParkSecrecy) releaseAndWait(t *testing.T, diagnostics *parkSecrecyDiagnostics) {
	t.Helper()
	close(s.hold)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		select {
		case line := <-diagnostics.writes:
			if strings.Contains(line, s.heldID) {
				return
			}
		case <-ctx.Done():
			t.Fatal("parks write refusal was not observed")
		}
	}
}

func (s *handlerParkSecrecy) delivery(t *testing.T, handler http.Handler, now func() time.Time) []byte {
	t.Helper()
	e := events.Event{ID: "evt_bcbcbcbcbcbcbcbc", Time: now(), Service: "scrapes", Name: "scrape.finished", Attrs: events.Attrs{"scraper": parks.Scraper, "run": "parcel-secret-run", "status": "done"}, Seq: 1, Received: now()}
	raw, _ := json.Marshal(e)
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	env["attempt"] = 1
	raw, _ = json.Marshal(env)
	response := request(handler, "POST", "/events", string(raw), "")
	if response.Code != 500 {
		t.Fatal("failing parks delivery status", response.Code)
	}
	return response.Body.Bytes()
}

func (s *handlerParkSecrecy) checkCaptures(t *testing.T, telemetryEvents []telemetry.Event, busEvents []events.Event) {
	t.Helper()
	started, finished, reads := map[string]int{}, map[string]int{}, map[string]int{}
	for _, e := range telemetryEvents {
		id, _ := e.Attrs["pass"].(string)
		if e.Name == "pass.started" {
			started[id]++
		}
		if e.Name == "pass.finished" {
			finished[id]++
		}
		if e.Name == "sibling.called" {
			if path, _ := e.Attrs["path"].(string); path == "/scrapers/"+parks.Scraper+"/current" || path == "/runs/parcel-secret-run/rows" {
				reads[path]++
			}
		}
	}
	if started[s.firstID] != 1 || finished[s.firstID] != 1 || started[s.heldID] != 1 {
		t.Fatal("parks trail pass capture absent")
	}
	for _, path := range []string{"/scrapers/" + parks.Scraper + "/current", "/runs/parcel-secret-run/rows"} {
		if reads[path] != 2 {
			t.Fatal("parks read capture absent", path, reads[path])
		}
	}
	started, finished = map[string]int{}, map[string]int{}
	for _, e := range busEvents {
		id, _ := e.Attrs["pass"].(string)
		if e.Name == "pass.started" {
			started[id]++
		}
		if e.Name == "pass.finished" {
			finished[id]++
		}
	}
	if started[s.firstID] != 1 || finished[s.firstID] != 1 || started[s.heldID] != 1 {
		t.Fatal("parks bus pass capture absent")
	}
}

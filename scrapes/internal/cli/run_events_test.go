package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
)

func runID(t *testing.T, v map[string]any) string {
	t.Helper()
	id, ok := v["id"].(string)
	if !ok || !store.ValidRunID(id) {
		t.Fatalf("run %v", v)
	}
	return id
}
func startScrape(t *testing.T, f *runFixture, c identity.Caller) map[string]any {
	t.Helper()
	r := f.call(c, "scrape", map[string]any{"scraper": "fmcsa"})
	if r.IsError() {
		t.Fatalf("refused %+v", r)
	}
	return resultJSON(t, r)
}
func cronDelivery(name, id string) string {
	return `{"id":"` + id + `","time":"2024-02-03T04:05:06.000000Z","service":"cron","event":"` + name + `","request_id":"fixture-origin","user":"","attrs":{"trigger":"scrape_fmcsa","when":"fixture","scheduled":"fixture"},"cause":"evt_ffffffffffffffff","depth":2,"seq":1,"received":"2024-02-03T04:05:06.000000Z","attempt":1}`
}
func deliverCron(t *testing.T, f *runFixture, id, outcome string) {
	t.Helper()
	r := f.request("POST", "/events", cronDelivery("cron.scrape_fmcsa.fired", id), false)
	var v map[string]any
	err := json.NewDecoder(r.Body).Decode(&v)
	wantStatus := http.StatusOK
	if outcome == "error" {
		wantStatus = http.StatusInternalServerError
	}
	if err != nil || r.StatusCode != wantStatus || v["outcome"] != outcome {
		t.Fatalf("delivery %d %v %v", r.StatusCode, v, err)
	}
}
func currentRun(t *testing.T, f *runFixture) string {
	t.Helper()
	v := resultJSON(t, f.call(fixtureCaller, "runs", map[string]any{}))
	rs, ok := v["runs"].([]any)
	if !ok || len(rs) == 0 {
		t.Fatalf("runs %v", v)
	}
	return runID(t, rs[0].(map[string]any))
}
func runTrail(f *runFixture) []telemetry.Event {
	out := []telemetry.Event{}
	for _, e := range f.Telemetry.Events() {
		if e.Name == trail.Started || e.Name == trail.Finished {
			out = append(out, e)
		}
	}
	return out
}
func equalJSON(t *testing.T, a, b any) bool {
	t.Helper()
	normalize := func(v any) any {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var out any
		if err = json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}

// R-2UZD-AVI9 R-2W79-ON8Y
func TestRunPairedEvents(t *testing.T) {
	f := startRun(t, runOptions{})
	id := runID(t, startScrape(t, f, fixtureCaller))
	done := f.awaitRun(id)
	if done["status"] != store.StatusDone {
		t.Fatal(done)
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	bus, logged := f.Events.Events(), runTrail(f)
	if len(bus) != 2 || len(logged) != 2 {
		t.Fatalf("bus %v trail %v", bus, logged)
	}
	for i, name := range []string{trail.Started, trail.Finished} {
		b, e := bus[i], logged[i]
		if b.Name != name || e.Name != name || b.RequestID != fixtureCaller.RequestID || e.RequestID != fixtureCaller.RequestID || b.User != fixtureCaller.UserID || e.User != fixtureCaller.UserID || b.Attrs["run"] != id || e.Attrs["run"] != id || !equalJSON(t, b.Attrs, e.Attrs) {
			t.Fatalf("pair %d bus %+v trail %+v", i, b, e)
		}
	}
	for _, key := range []string{"scraper", "status", "reason", "rows", "new", "changed", "unchanged", "gone"} {
		want, ok := done[key]
		if !ok {
			want = ""
		}
		if !equalJSON(t, bus[1].Attrs[key], want) || !equalJSON(t, logged[1].Attrs[key], want) {
			t.Errorf("finished %s bus %v trail %v want %v", key, bus[1].Attrs[key], logged[1].Attrs[key], want)
		}
	}
}

// R-2XF6-2EZN
func TestRunCronEventCause(t *testing.T) {
	f := startRun(t, runOptions{})
	const eventID = "evt_aaaaaaaaaaaaaaaa"
	deliverCron(t, f, eventID, "ok")
	id := currentRun(t, f)
	if done := f.awaitRun(id); done["status"] != store.StatusDone {
		t.Fatal(done)
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	es := f.Events.Events()
	if len(es) != 2 {
		t.Fatalf("events %v", es)
	}
	for i, name := range []string{trail.Started, trail.Finished} {
		e := es[i]
		if e.Name != name || e.Attrs["run"] != id || e.RequestID != fixtureCaller.RequestID || e.User != "" || e.Cause != eventID || e.Depth != 3 {
			t.Fatalf("event %+v", e)
		}
	}
}

// R-2YN2-G6QC R-724S-I1KQ
func TestRunReadRequestsAndRefusalsHaveNoRunEventsOrSiblingCalls(t *testing.T) {
	f := startRun(t, runOptions{DefaultSinks: true})
	id := runID(t, startScrape(t, f, fixtureCaller))
	if done := f.awaitRun(id); done["status"] != store.StatusDone {
		t.Fatal(done)
	}
	rows := resultJSON(t, f.call(fixtureCaller, "rows", map[string]any{"run": id}))["rows"].([]any)
	sourceID := rows[0].(map[string]any)["source_id"].(string)
	for _, c := range []struct {
		name string
		args map[string]any
	}{{"scrapers", map[string]any{}}, {"runs", map[string]any{}}, {"run", map[string]any{"run": id}}, {"rows", map[string]any{"scraper": "fmcsa"}}, {"row", map[string]any{"run": id, "source_id": sourceID}}} {
		if r := f.call(fixtureCaller, c.name, c.args); r.IsError() {
			t.Fatalf("%s refused %+v", c.name, r)
		}
	}
	for _, path := range []string{"/", "/tools", "/about", "/runs/" + id, "/runs/" + id + "/rows/" + url.PathEscape(sourceID), "/scrapers/fmcsa/current", "/runs/" + id + "/rows"} {
		if r := f.request("GET", path, "", true); r.StatusCode != 200 {
			t.Fatalf("path %s status %d", path, r.StatusCode)
		}
	}
	if r := f.call(fixtureCaller, "scrape", map[string]any{"scraper": "bogus"}); !r.IsError() {
		t.Fatal("unknown scraper accepted")
	}
	if r := f.request("POST", "/events", cronDelivery("cron.scrape_bogus.fired", "evt_bbbbbbbbbbbbbbbb"), false); r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown delivery status %d", r.StatusCode)
	}
	if rs := resultJSON(t, f.call(fixtureCaller, "runs", map[string]any{}))["runs"].([]any); len(rs) != 1 || runID(t, rs[0].(map[string]any)) != id {
		t.Fatalf("unknown delivery recorded a run: %v", rs)
	}
	const deliveryID = "evt_cccccccccccccccc"
	deliverCron(t, f, deliveryID, "ok")
	second := currentRun(t, f)
	if second == id {
		t.Fatal("delivery did not record another run")
	}
	if done := f.awaitRun(second); done["status"] != store.StatusDone {
		t.Fatal(done)
	}
	deliverCron(t, f, deliveryID, "skip")
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	f.siblingsMu.Lock()
	defer f.siblingsMu.Unlock()
	for name, n := range f.siblings {
		if n != 0 {
			t.Errorf("sibling %s got %d requests", name, n)
		}
	}
	bus, logged := f.Events.Events(), runTrail(f)
	if len(bus) != 4 || len(logged) != 4 {
		t.Fatalf("unexpected events bus %v trail %v", bus, logged)
	}
	for _, id := range []string{id, second} {
		for _, name := range []string{trail.Started, trail.Finished} {
			b, l := 0, 0
			for _, e := range bus {
				if e.Name == name && e.Attrs["run"] == id {
					b++
				}
			}
			for _, e := range logged {
				if e.Name == name && e.Attrs["run"] == id {
					l++
				}
			}
			if b != 1 || l != 1 {
				t.Errorf("run %s event %s bus %d trail %d", id, name, b, l)
			}
		}
	}
}

// R-73CO-VTBF
func TestRunTrustsCallerHeadersAndPersistsThem(t *testing.T) {
	for _, c := range []identity.Caller{{UserID: "arbitrary owner", Email: "not-an-email", RequestID: "chosen request"}, {UserID: "second/owner", Email: "fixture+alias@example.test", RequestID: "another/request"}} {
		t.Run(c.UserID, func(t *testing.T) {
			f := startRun(t, runOptions{})
			v := startScrape(t, f, c)
			id := runID(t, v)
			if v["user"] != c.UserID || v["email"] != c.Email {
				t.Fatalf("caller %v", v)
			}
			f.awaitRun(id)
			if f.stop() != cli.ExitSuccess {
				t.Fatal("stop failed")
			}
			st, _ := openStore(t, f.Dir)
			r, err := st.GetRun(context.Background(), id)
			if err != nil || r.UserID != c.UserID || r.UserEmail != c.Email || r.RequestID != c.RequestID {
				t.Fatalf("persisted %+v error %v", r, err)
			}
		})
	}
}

type failedTelemetrySink struct{}

func (failedTelemetrySink) Deliver(context.Context, telemetry.Event) error {
	return errors.New("fixture sink failure")
}

type failedEventSink struct{}

func (failedEventSink) Deliver(context.Context, events.Event) error {
	return errors.New("fixture sink failure")
}

// R-2ZUY-TYH1
func TestRunSinkFailuresDoNotChangeResults(t *testing.T) {
	baseline := startRun(t, runOptions{})
	accepted := startScrape(t, baseline, fixtureCaller)
	ended := baseline.awaitRun(runID(t, accepted))
	if baseline.stop() != cli.ExitSuccess {
		t.Fatal("baseline stop failed")
	}
	failed := startRun(t, runOptions{Sink: failedTelemetrySink{}, EventSink: failedEventSink{}})
	got := startScrape(t, failed, fixtureCaller)
	final := failed.awaitRun(runID(t, got))
	if failed.stop() != cli.ExitSuccess {
		t.Fatal("failed sinks stop failed")
	}
	for _, v := range []map[string]any{accepted, got} {
		for _, k := range []string{"id", "started", "finished"} {
			delete(v, k)
		}
	}
	if !equalJSON(t, accepted, got) {
		t.Fatalf("accepted baseline %v failure %v", accepted, got)
	}
	for _, key := range []string{"status", "reason", "rows", "new", "changed", "unchanged", "gone"} {
		if !equalJSON(t, ended[key], final[key]) {
			t.Errorf("record %s baseline %v failure %v", key, ended[key], final[key])
		}
	}
}

// R-F1RE-OF5K
func TestRunAssembledRowsPaginationAndRawRecord(t *testing.T) {
	records := make([]map[string]any, 30)
	ids := make([]string, 30)
	for i := range records {
		ids[i] = fmt.Sprint(i + 1)
		records[i] = map[string]any{"dot_number": ids[i], "legal_name": fmt.Sprintf("Fixture carrier %d", i), "phy_state": "MN", "power_units": "7", "fixture_extension": map[string]any{"index": i}}
	}
	f := startRun(t, runOptions{Env: map[string]string{"FMCSA_STATES": "MN"}, Modify: func(p *cli.Process) {
		p.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Query().Get("$select") != "" {
				return jsonResponse(r, []any{map[string]string{"count": "30"}}), nil
			}
			return jsonResponse(r, records), nil
		})}
	}})
	id := runID(t, startScrape(t, f, fixtureCaller))
	if done := f.awaitRun(id); done["status"] != store.StatusDone {
		t.Fatal(done)
	}
	sort.Strings(ids)
	for page := 0; page < 2; page++ {
		v := resultJSON(t, f.call(fixtureCaller, "rows", map[string]any{"scraper": "fmcsa", "page": page}))
		rs := v["rows"].([]any)
		want := ids[:25]
		if page == 1 {
			want = ids[25:]
		}
		if len(rs) != len(want) {
			t.Fatalf("page %d rows %d", page, len(rs))
		}
		for i, w := range want {
			r := rs[i].(map[string]any)
			if r["source_id"] != w {
				t.Errorf("page %d row %d: %v want %s", page, i, r["source_id"], w)
			}
			if _, ok := r["raw"]; ok {
				t.Error("list included raw")
			}
		}
		next, ok := v["next"]
		if page == 0 && next != float64(1) || page == 1 && ok {
			t.Fatalf("page %d next %v present %v", page, next, ok)
		}
	}
	chosen := records[17]
	v := resultJSON(t, f.call(fixtureCaller, "row", map[string]any{"run": id, "source_id": chosen["dot_number"]}))
	if v["source_id"] != chosen["dot_number"] || !equalJSON(t, v["raw"], chosen) {
		t.Fatalf("raw %v want %v", v, chosen)
	}
}

// R-2YN2-G6QC
func TestRunErrorDeliveryAndRefusedScrapeEmitNoRunEvents(t *testing.T) {
	// Reusing the injected random bytes makes a later run id collide with the
	// completed run. The store cannot record either attempted new run.
	f := startRun(t, runOptions{Rand: fixedRandom(0x63)})
	id := runID(t, startScrape(t, f, fixtureCaller))
	if done := f.awaitRun(id); done["status"] != store.StatusDone {
		t.Fatal(done)
	}
	if r := f.call(fixtureCaller, "scrape", map[string]any{"scraper": "fmcsa"}); !r.IsError() {
		t.Fatal("colliding run accepted")
	}
	deliverCron(t, f, "evt_dddddddddddddddd", "error")
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	for _, es := range [][]string{eventNames(f.Events.Events()), trailNames(runTrail(f))} {
		if !reflect.DeepEqual(es, []string{trail.Started, trail.Finished}) {
			t.Fatalf("run events %v", es)
		}
	}
}
func eventNames(es []events.Event) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}
func trailNames(es []telemetry.Event) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}

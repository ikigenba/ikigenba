package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func seedRunWalkParks(t *testing.T, h *runHarness, count int, radius float64, ceilings ...int64) []store.Park {
	t.Helper()
	d, st := runStore(t, h.p.Dir, h.p.Now)
	defer func() { _ = d.Close() }()
	if len(ceilings) > 0 {
		p, err := st.SetParkRules(t.Context(), store.ParkRulesEdit{WalkCeiling: &ceilings[0]}, store.Pass{Kind: store.KindParks, Origin: store.OriginRules, Trigger: store.TriggerParkRules, UserID: "seed-owner"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = st.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
			t.Fatal(err)
		}
	}
	plan := parks.Plan{}
	for i := range count {
		lat, lon, value := 45.0+float64(i), -93.0, int64(2000000)
		plan.News = append(plan.News, parks.New{Number: 1, Cluster: parks.Cluster{County: "Walk County", Town: fmt.Sprintf("Walk Town %d", i), Zip: "55056", Lat: lat, Lon: lon, Radius: radius, BuildingValue: value, Parcels: []parks.Parcel{{PIN: fmt.Sprintf("walk-pin-%d", i), Street: "1 Walk Road", City: fmt.Sprintf("Walk Town %d", i), County: "Walk County", Zip: "55056", Lat: &lat, Lon: &lon, BuildingValue: &value, Rule: parks.RuleValue}}}})
	}
	p, err := st.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "walk-seed-run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.ApplyParks(t.Context(), p.ID, "walk-seed-run", []string{"Walk County"}, plan, store.Counts{ParksCreated: int64(count)}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListParks(t.Context(), store.ParkFilter{County: "Walk County"}, parks.SortMinutes, "", "", count+1)
	if err != nil || len(got) != count {
		t.Fatal(got, err)
	}
	return got
}

func runWalkTimers(t *testing.T, h *runHarness) {
	t.Helper()
	h.p.After = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		switch d {
		case passes.PollInterval, passes.QueueWait, website.Interval:
			ch <- time.Time{}
		case places.Timeout, cli.SweepInterval:
		default:
			t.Errorf("unexpected walk timer %v", d)
		}
		return ch
	}
}

func walkGoogleResponse(r *http.Request, records []map[string]any) *http.Response {
	body, _ := json.Marshal(map[string]any{"places": records})
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}
}

type walkRequest struct {
	Method, URL string
	Header      http.Header
	Body        string
}

func captureWalkRequest(r *http.Request) walkRequest {
	body, _ := io.ReadAll(r.Body)
	return walkRequest{r.Method, r.URL.String(), r.Header.Clone(), string(body)}
}

// R-4MCK-VC4J R-4JWS-3SN5
func TestRunWalkUsesInjectedGoogleClientAndPublishesLifecycle(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	k := seedRunWalkParks(t, h, 1, 80)[0]
	h.env["GOOGLE_MAPS_API_KEY"] = "walk-test-key"
	runWalkTimers(t, h)
	var timeouts atomic.Int64
	after := h.p.After
	h.p.After = func(d time.Duration) <-chan time.Time {
		if d == places.Timeout {
			timeouts.Add(1)
		}
		return after(d)
	}
	var mu sync.Mutex
	var requests []walkRequest
	response := []map[string]any{{"id": "walk-place", "displayName": map[string]string{"text": "Walk Business"}, "formattedAddress": "2 Factory Road, Walk Town, MN 55056, USA", "primaryType": "manufacturer"}}
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, captureWalkRequest(r))
		mu.Unlock()
		if r.URL.String() != places.NearbyURL {
			t.Errorf("unexpected fetch %s", r.URL)
		}
		return walkGoogleResponse(r, response), nil
	})}
	h.start()
	c := identity.Caller{UserID: "walker", RequestID: "walk-request"}
	r, p := h.call(c, "walk", map[string]any{"parks": []string{k.ID}})
	if r.IsError() {
		t.Fatal(r)
	}
	id := runText(p, "id")
	done := h.waitPass(c, id)
	if runText(done, "status") != store.StatusDone {
		t.Fatal(done)
	}
	h.stop()
	if timeouts.Load() != 1 {
		t.Fatal("injected Places timer count", timeouts.Load())
	}
	var expected walkRequest
	cl := places.Client{Key: h.env["GOOGLE_MAPS_API_KEY"], After: after, HTTP: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		expected = captureWalkRequest(r)
		return walkGoogleResponse(r, response), nil
	})}}
	if a := cl.Nearby(t.Context(), places.First(k.Lat, k.Lon, k.Radius)); a.Error != "" {
		t.Fatal(a)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 || !reflect.DeepEqual(requests[0], expected) {
		t.Fatal(requests, expected)
	}
	bus := h.bus.Events()
	var lifecycle []int
	for i, e := range bus {
		if e.Attrs["pass"] == id {
			lifecycle = append(lifecycle, i)
		}
	}
	if len(lifecycle) != 2 {
		t.Fatal(bus)
	}
	lastTrail := -1
	for i, index := range lifecycle {
		e := bus[index]
		if e.Name != []string{"pass.started", "pass.finished"}[i] || e.RequestID != c.RequestID || e.User != c.UserID || e.Cause != "" || e.Depth != 0 {
			t.Fatal(e)
		}
		for key, want := range map[string]string{"kind": "walk", "origin": "walk", "trigger": "walk", "target": "", "enrich_kind": ""} {
			if e.Attrs[key] != want {
				t.Fatal(key, e)
			}
		}
		matches := 0
		for j, te := range h.telemetry.Events() {
			if te.Name == e.Name && te.Attrs["pass"] == id {
				matches++
				if j <= lastTrail || te.RequestID != c.RequestID || te.User != c.UserID || !reflect.DeepEqual(map[string]any(te.Attrs), map[string]any(e.Attrs)) {
					t.Fatal(te, e)
				}
				lastTrail = j
			}
		}
		if matches != 1 {
			t.Fatal("trail lifecycle count", matches)
		}
	}
	finished := bus[lifecycle[1]]
	for key, want := range map[string]int64{"parks_walked": 1, "circles": 1, "requests": 1, "found": 1, "new_candidates": 1, "cost": places.PriceNanos} {
		var actual int64
		if err := json.Unmarshal(done[key], &actual); err != nil || actual != want || finished.Attrs[key] != want {
			t.Fatal(key, string(done[key]), finished.Attrs[key], err)
		}
	}
	if finished.Attrs["status"] != store.StatusDone {
		t.Fatal(finished)
	}
}

// R-4MCK-VC4J
func TestRunWalkWithKeyAndNilHTTPFailsLocally(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	k := seedRunWalkParks(t, h, 1, 80)[0]
	h.env["GOOGLE_MAPS_API_KEY"] = "walk-test-key"
	runWalkTimers(t, h)
	h.start()
	c := identity.Caller{UserID: "walker", RequestID: "nil-http-walk"}
	r, p := h.call(c, "walk", map[string]any{"parks": []string{k.ID}})
	if r.IsError() {
		t.Fatal(r)
	}
	done := h.waitPass(c, runText(p, "id"))
	if runText(done, "status") != store.StatusFailed || runText(done, "reason") != store.ReasonPlaces {
		t.Fatal(done)
	}
}

// R-20FP-CBJL R-QBCU-2WE1
func TestRunWalkWithoutKeyRefusedWithoutRequestOrPass(t *testing.T) {
	for _, key := range []string{"unset", "empty"} {
		for _, sandbox := range []bool{false, true} {
			t.Run(fmt.Sprint(key, "/", sandbox), func(t *testing.T) {
				h := newRunHarness(t, t.TempDir(), "")
				k := seedRunWalkParks(t, h, 1, 80)[0]
				if key == "empty" {
					h.env["GOOGLE_MAPS_API_KEY"] = ""
				}
				if sandbox {
					h.env["IKIGENBA_SANDBOX"] = ""
				}
				h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
					t.Errorf("keyless walk fetched %s", r.URL)
					return nil, io.EOF
				})}
				h.start()
				c := identity.Caller{UserID: "walker", RequestID: "no-key-walk"}
				_, before := h.call(c, "passes", map[string]any{})
				r, _ := h.call(c, "walk", map[string]any{"parks": []string{k.ID}})
				raw, _ := json.Marshal(r)
				if !r.IsError() || !strings.Contains(string(raw), passes.RefusalInvalid+": "+passes.NoPlacesKey) {
					t.Fatal(r)
				}
				_, after := h.call(c, "passes", map[string]any{})
				if !bytes.Equal(before["passes"], after["passes"]) {
					t.Fatal(before, after)
				}
				h.stop()
				if len(h.bus.Events()) != 0 {
					t.Fatal(h.bus.Events())
				}
				for _, e := range h.telemetry.Events() {
					if e.Name == "pass.started" || e.Name == "pass.finished" {
						t.Fatal(e)
					}
				}
			})
		}
	}
}

// R-4L4O-HKDU
func TestRunSandboxWalkUsesPlacesFake(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	h.p.Rand = &secrecyBytes{}
	const ceiling = 3
	k := seedRunWalkParks(t, h, 1, 600, ceiling)[0]
	h.env["GOOGLE_MAPS_API_KEY"], h.env["IKIGENBA_SANDBOX"] = "walk-test-key", ""
	runWalkTimers(t, h)
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == places.NearbyURL {
			t.Error("sandbox walk escaped Places fake")
		}
		return nil, io.EOF
	})}
	h.start()
	c := identity.Caller{UserID: "walker", RequestID: "sandbox-walk"}
	r, p := h.call(c, "walk", map[string]any{"parks": []string{k.ID}})
	if r.IsError() {
		t.Fatal(r)
	}
	done := h.waitPass(c, runText(p, "id"))
	if runText(done, "status") != store.StatusDone || string(done["ceiling_reached"]) != "true" || string(done["requests"]) != fmt.Sprint(ceiling) {
		t.Fatal(done)
	}
	_, st := runStore(t, h.p.Dir, h.p.Now)
	park, err := st.GetPark(t.Context(), k.ID)
	if err != nil || park.Walked.IsZero() || !park.CeilingReached {
		t.Fatal(park, err)
	}
}

type walkSecrecyTransport struct {
	mu      sync.Mutex
	markers []string
	failed  bool
	n       int
}

func (f *walkSecrecyTransport) response(r *http.Request) *http.Response {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	if f.failed && f.n > 1 {
		return &http.Response{StatusCode: 500, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(strings.Join(f.markers, " "))), Request: r}
	}
	n := 1
	if f.failed {
		n = places.MaxResults
	}
	items := []map[string]any{}
	for i := range n {
		items = append(items, map[string]any{"id": fmt.Sprintf("secret-place-%d", i), "displayName": map[string]string{"text": f.markers[0]}, "formattedAddress": f.markers[3] + ", " + f.markers[4] + ", MN 55056, USA", "websiteUri": "https://" + strings.ToLower(f.markers[8]) + ".example/", "nationalPhoneNumber": f.markers[10]})
	}
	return walkGoogleResponse(r, items)
}

func exerciseRunWalkSecrecy(t *testing.T, h *runHarness, f *walkSecrecyTransport, call func(string, any, bool) map[string]json.RawMessage, expected map[string]string) {
	t.Helper()
	ks := seedRunWalkParks(t, h, 2, 600)
	c := identity.Caller{UserID: "reader", Email: "reader@example.test", RequestID: "secrecy-request"}
	for i, k := range ks {
		f.mu.Lock()
		f.failed, f.n = i == 1, 0
		f.mu.Unlock()
		v := call("walk", map[string]any{"parks": []string{k.ID}}, false)
		id := runText(v, "id")
		expected[id] = c.RequestID
		done := h.waitPass(c, id)
		want := store.StatusDone
		if i == 1 {
			want = store.StatusFailed
		}
		if runText(done, "status") != want || i == 1 && runText(done, "reason") != store.ReasonPlaces {
			t.Fatal(done)
		}
		if i == 0 {
			detail := call("park", map[string]any{"park": k.ID}, false)
			data, _ := json.Marshal(detail)
			if !strings.Contains(string(data), f.markers[0]) || !strings.Contains(string(data), f.markers[10]) {
				t.Fatal("walk did not retain private place fields")
			}
			call("walk", map[string]any{"parks": []string{k.ID}}, true)
		}
	}
}

package cli_test

import (
	"context"
	"database/sql"
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
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/settings"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

type enrichTransport func(*http.Request) (*http.Response, error)

func (f enrichTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func seedRunWebsite(t *testing.T, h *runHarness, email string) string {
	t.Helper()
	d, st := runStore(t, h.p.Dir, h.p.Now)
	_, ids, err := st.Attach(t.Context(), "fixture", true, []store.Observation{{Scraper: "fmcsa", SourceID: "web-fixture", Digest: "fixture", Reading: sources.Reading{Name: "chosen", NameKey: "chosen", Email: email}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	return ids[0]
}
func websiteTimers(t *testing.T, h *runHarness, timeout chan chan time.Time) {
	t.Helper()
	h.p.After = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		switch d {
		case cli.SweepInterval:
			h.timers <- ch
		case website.Interval:
			ch <- time.Time{}
		case website.Timeout:
			if timeout != nil {
				timeout <- ch
			}
		default:
			t.Errorf("unexpected timer %v", d)
		}
		return ch
	}
}

// R-4HGZ-C95R
func TestRunSandboxSelectsStub(t *testing.T) {
	for _, supplied := range []bool{false, true} {
		t.Run(map[bool]string{false: "nil", true: "injected"}[supplied], func(t *testing.T) {
			h := newRunHarness(t, t.TempDir(), "")
			h.env["IKIGENBA_SANDBOX"] = ""
			var requests atomic.Int64
			if supplied {
				h.p.HTTP = &http.Client{Transport: enrichTransport(func(*http.Request) (*http.Response, error) { requests.Add(1); return nil, io.EOF })}
			}
			websiteTimers(t, h, nil)
			id := seedRunWebsite(t, h, "ops@secure.test")
			h.start()
			c := identity.Caller{UserID: "owner", RequestID: "manual-enrich"}
			r, p := h.call(c, "enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck})
			if r.IsError() {
				t.Fatal(r)
			}
			if runText(h.waitPass(c, runText(p, "id")), "status") != store.StatusDone {
				t.Fatal(p)
			}
			h.stop()
			_, st := runStore(t, h.p.Dir, h.p.Now)
			es, err := st.Enrichments(t.Context(), id)
			var found website.Result
			if len(es) == 1 {
				if e := json.Unmarshal(es[0].Found, &found); e != nil {
					t.Fatal(e)
				}
			}
			if err != nil || len(es) != 1 || es[0].Kind != sources.WebsiteCheck || found.State != sources.WebsiteHTTPS || found.URL != "https://secure.test" || requests.Load() != 0 {
				t.Fatal(es, err, requests.Load())
			}
		})
	}
}

// R-4IOV-Q0WG
func TestRunInjectedHTTPAndTimeouts(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	arrived := make(chan string, 4)
	timeouts := make(chan chan time.Time, 4)
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		arrived <- r.URL.String()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	websiteTimers(t, h, timeouts)
	id := seedRunWebsite(t, h, "ops@acme.test")
	h.start()
	c := identity.Caller{UserID: "owner"}
	r, p := h.call(c, "enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck})
	if r.IsError() {
		t.Fatal(r)
	}
	for _, want := range []string{"https://acme.test", "https://www.acme.test", "http://acme.test", "http://www.acme.test"} {
		select {
		case got := <-arrived:
			if got != want {
				t.Fatal(got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("request missing")
		}
		select {
		case timer := <-timeouts:
			timer <- time.Time{}
		case <-time.After(3 * time.Second):
			t.Fatal("timeout timer missing")
		}
	}
	if runText(h.waitPass(c, runText(p, "id")), "status") != store.StatusDone {
		t.Fatal(p)
	}
	h.stop()
	_, st := runStore(t, h.p.Dir, h.p.Now)
	es, err := st.Enrichments(t.Context(), id)
	var found website.Result
	if len(es) == 1 {
		if e := json.Unmarshal(es[0].Found, &found); e != nil {
			t.Fatal(e)
		}
	}
	if err != nil || len(es) != 1 || es[0].Kind != sources.WebsiteCheck || found.State != sources.WebsiteNone || found.Error != website.FailTimeout {
		t.Fatal(es, err)
	}
}

// R-IODZ-J5I5
func TestRunNoHTTPRefusesEnrichWithoutPass(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	h.start()
	r, _ := h.call(identity.Caller{UserID: "owner", RequestID: "refused-enrich"}, "enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck})
	b, _ := json.Marshal(r)
	if !r.IsError() || !strings.Contains(string(b), "unavailable: "+tools.RefuseUnavailable) {
		t.Fatal(r)
	}
	_, v := h.call(identity.Caller{UserID: "owner"}, "passes", tools.PassesArgs{})
	if string(v["passes"]) != "[]" {
		t.Fatal(v)
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
}

// R-S4D6-TT8H
func TestRunFollowOnPassKeepsLineage(t *testing.T) {
	fake := &runFake{rows: []map[string]any{runRow()}}
	h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"scrapes": runSocket(t, fake)}))
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(*http.Request) (*http.Response, error) { return nil, io.EOF })}
	websiteTimers(t, h, nil)
	h.start()
	h.deliver("evt_9999999999999999", "lineage-request", "done", 2)
	c := identity.Caller{UserID: "reader"}
	deadline, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		_, v := h.call(c, "passes", tools.PassesArgs{})
		var ps []map[string]json.RawMessage
		if err := json.Unmarshal(v["passes"], &ps); err != nil {
			t.Fatal(err)
		}
		ended := len(ps) == 3
		for _, p := range ps {
			if runText(p, "status") == store.StatusRunning {
				ended = false
			}
		}
		if ended {
			break
		}
		if deadline.Err() != nil {
			t.Fatal("follow-on did not end", ps)
		}
	}
	h.stop()
	seen := map[string]struct {
		request, cause string
		depth          int
	}{}
	follow := 0
	for _, e := range h.bus.Events() {
		if e.Attrs["origin"] == store.OriginPass {
			parent, ok := seen[e.Attrs["trigger"].(string)]
			if !ok || e.RequestID != parent.request || e.Cause != parent.cause || e.Depth != parent.depth || e.User != "" {
				t.Fatal(e, parent, ok)
			}
			follow++
		}
		if e.Name == "pass.started" {
			seen[e.Attrs["pass"].(string)] = struct {
				request, cause string
				depth          int
			}{e.RequestID, e.Cause, e.Depth}
		}
	}
	if follow != 4 {
		t.Fatal(follow, h.bus.Events())
	}
}

// R-DV50-GJD1
func TestRunFillsFactsBeforeStartRead(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	read := make(chan struct{}, 1)
	fake := &runFake{}
	h.env[services.Variable] = runServices(t, map[string]string{"scrapes": runSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := h.err.String(); got != "candidates: facts filled\n" {
			t.Errorf("start read before fill completion: %q", got)
		}
		fake.ServeHTTP(w, r)
		read <- struct{}{}
	}))})
	d, _ := runStore(t, h.p.Dir, h.p.Now)
	st := store.New(d, store.Config{Now: h.p.Now, Rand: &secrecyBytes{}})
	ids := []string{}
	for i := 0; i < 2*passes.BatchSize+1; i++ {
		_, cs, err := st.Attach(t.Context(), "fixture", false, []store.Observation{{Scraper: "fmcsa", SourceID: fmt.Sprint(i), Digest: fmt.Sprint(i), Reading: sources.Reading{Name: fmt.Sprintf("ready business %d", i), Email: "ops@ready.test"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, cs...)
	}
	found, err := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://ready.test/"})
	if err != nil {
		t.Fatal(err)
	}
	var es []store.Enrichment
	for _, id := range ids {
		saved, err := st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "ready.test", Source: "ready.test", Found: found}})
		if err != nil {
			t.Fatal(err)
		}
		es = append(es, saved...)
	}
	if err := d.Write(t.Context(), func(tx *sql.Tx) error { _, err := tx.ExecContext(t.Context(), "DELETE FROM facts"); return err }); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	h.filled()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("start read did not arrive")
	}
	d, st = runStore(t, h.p.Dir, h.p.Now)
	for i, id := range ids {
		f, err := st.GetFacts(t.Context(), id)
		if err != nil || f.Website != sources.WebsiteHTTPS || f.WebsiteURL != "https://ready.test/" || len(f.Written) != 1 || f.Written[0].Kind != sources.WebsiteCheck || !f.Written[0].At.Equal(es[i].At) {
			t.Fatal(id, f, err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	h.stop()
	if got := h.err.String(); got != "candidates: facts filled\n" {
		t.Fatalf("fill completion must occur exactly once: %q", got)
	}
	if requests := fake.Requests(); len(requests) != 1 || requests[0].Path != "/scrapers/mn_parcels/current" {
		t.Fatal(requests)
	}
}

// R-V2ER-KG1T R-V3MN-Y7SI R-V4UK-BZJ7
func TestRunAddPageUsesSelectedWebsiteClient(t *testing.T) {
	for _, mode := range []string{"sandbox-nil", "sandbox-injected", "injected", "nil"} {
		t.Run(mode, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir(), "")
			sandbox := strings.HasPrefix(mode, "sandbox")
			if sandbox {
				h.env["IKIGENBA_SANDBOX"] = ""
			}
			var requests []string
			if mode == "injected" || mode == "sandbox-injected" {
				h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
					requests = append(requests, r.URL.String())
					return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("fixture"))}, nil
				})}
			}
			websiteTimers(t, h, nil)
			id := seedRunWebsite(t, h, "ops@secure.test")
			d, st := runStore(t, h.p.Dir, h.p.Now)
			found, err := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://secure.test"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "secure.test", Source: "secure.test", Found: found}}); err != nil {
				t.Fatal(err)
			}
			if err = d.Close(); err != nil {
				t.Fatal(err)
			}
			h.start()
			r, _ := h.call(identity.Caller{UserID: "owner"}, "add_page", map[string]any{"candidate": id, "url": "https://secure.test/about"})
			if r.IsError() == sandbox {
				t.Fatal(r)
			}
			if !sandbox {
				b, _ := json.Marshal(r)
				if !strings.Contains(string(b), "unavailable:") {
					t.Fatal(r)
				}
			}
			h.stop()
			_, st = runStore(t, h.p.Dir, h.p.Now)
			pages, err := st.Pages(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if sandbox {
				if len(pages) != 1 || pages[0].URL != "https://secure.test/about" || pages[0].Title != "secure.test" || len(requests) != 0 {
					t.Fatal(pages, requests)
				}
			} else {
				if len(pages) != 0 {
					t.Fatal(pages)
				}
				if mode == "injected" && (len(requests) == 0 || requests[0] != "https://secure.test/about") {
					t.Fatal(requests)
				}
			}
		})
	}
}

func runRouteAddress() sources.Address {
	return sources.Address{Street: "1 Fixture St", City: "Fixture City", State: "MN", Zip: "55072"}
}

func seedRunRoute(t *testing.T, h *runHarness) string {
	t.Helper()
	d, st := runStore(t, h.p.Dir, h.p.Now)
	a := runRouteAddress()
	_, ids, err := st.Attach(t.Context(), "route-fixture", true, []store.Observation{{Scraper: "fmcsa", SourceID: "route", Digest: "route", Reading: sources.Reading{Name: "Route business", NameKey: "route business", Street: a.Street, City: a.City, State: a.State, Zip: a.Zip}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	return ids[0]
}

// R-XRMY-LPIY R-XSUU-ZH9N
func TestRunRouteAvailability(t *testing.T) {
	for _, sandbox := range []bool{false, true} {
		for _, mode := range []string{"unset", "empty", "nil-http"} {
			t.Run(fmt.Sprintf("sandbox=%v/%s", sandbox, mode), func(t *testing.T) {
				h := newRunHarness(t, t.TempDir(), "")
				if sandbox {
					h.env["IKIGENBA_SANDBOX"] = ""
				}
				if mode == "empty" {
					h.env["GOOGLE_MAPS_API_KEY"] = ""
				}
				var requests atomic.Int64
				h.p.HTTP = &http.Client{Transport: enrichTransport(func(*http.Request) (*http.Response, error) { requests.Add(1); return nil, io.EOF })}
				if mode == "nil-http" {
					h.env["GOOGLE_MAPS_API_KEY"] = "chosen-key"
					h.p.HTTP = nil
				}
				id := seedRunRoute(t, h)
				h.start()
				r, _ := h.call(identity.Caller{UserID: "owner"}, "enrich", tools.EnrichArgs{Kind: enrichers.KindDriveRoute, Candidate: id})
				raw, _ := json.Marshal(r)
				want := passes.RefusalInvalid + ": " + passes.NoRouteKey
				if mode == "nil-http" {
					want = passes.RefusalUnavailable + ":"
				}
				if !r.IsError() || !strings.Contains(string(raw), want) || requests.Load() != 0 {
					t.Fatal(string(raw), requests.Load())
				}
				h.stop()
			})
		}
	}
}

type routeRequest struct {
	Method, URL, Body string
	Header            http.Header
}

// R-XQF2-7XS9 R-XU2R-D90C
func TestRunRouteClientMatchesStandalone(t *testing.T) {
	for _, sandbox := range []bool{false, true} {
		for _, origin := range []string{"", "1 Main St, Hinckley, MN"} {
			t.Run(fmt.Sprintf("sandbox=%v/origin=%s", sandbox, origin), func(t *testing.T) {
				h := newRunHarness(t, t.TempDir(), "")
				h.env["GOOGLE_MAPS_API_KEY"] = "fixture-key"
				if sandbox {
					h.env["IKIGENBA_SANDBOX"] = ""
				}
				h.env["CANDIDATES_ROUTE_ORIGIN"] = origin
				var mu sync.Mutex
				var got []routeRequest
				transport := func(dst *[]routeRequest) enrichTransport {
					return func(r *http.Request) (*http.Response, error) {
						var body []byte
						if r.Body != nil {
							body, _ = io.ReadAll(r.Body)
						}
						mu.Lock()
						*dst = append(*dst, routeRequest{r.Method, r.URL.String(), string(body), r.Header.Clone()})
						mu.Unlock()
						response := `{"status":"OK","results":[{"formatted_address":"Placed fixture","geometry":{"location_type":"ROOFTOP","location":{"lat":46,"lng":-93}}}]}`
						if r.Method == http.MethodPost {
							response = `{"routes":[{"duration":"600s","distanceMeters":1609.344}]}`
						}
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
					}
				}
				h.p.HTTP = &http.Client{Transport: transport(&got)}
				websiteTimers(t, h, nil)
				id := seedRunRoute(t, h)
				h.start()
				c := identity.Caller{UserID: "owner"}
				r, p := h.call(c, "enrich", tools.EnrichArgs{Kind: enrichers.KindDriveRoute, Candidate: id})
				if r.IsError() {
					t.Fatal(r)
				}
				done := h.waitPass(c, runText(p, "id"))
				if runText(done, "status") != store.StatusDone {
					t.Fatal(done)
				}
				_, candidate := h.call(c, "candidate", map[string]any{"candidate": id})
				o := origin
				if o == "" {
					o = settings.DefaultRouteOrigin
				}
				subject := enrichers.Subject{RecordAddress: sources.Sourced[sources.Address]{Value: runRouteAddress()}, Origin: o}
				var expected []routeRequest
				client := routes.Client{HTTP: &http.Client{Transport: transport(&expected)}, Key: "fixture-key", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
				client.Drive(t.Context(), o, enrichers.Destination(subject))
				h.stop()
				mu.Lock()
				equal := reflect.DeepEqual(got, expected)
				count := len(got)
				mu.Unlock()
				if !equal || count < 2 {
					t.Fatal(got, expected)
				}
				var results []struct{ Kind, Inputs string }
				if err := json.Unmarshal(candidate["enrichment_results"], &results); err != nil {
					t.Fatal(err, candidate)
				}
				found := false
				for _, result := range results {
					if result.Kind == enrichers.KindDriveRoute {
						found = true
						if result.Inputs != (enrichers.DriveRoute{}).Inputs(subject) {
							t.Fatal(result, subject)
						}
					}
				}
				if !found {
					t.Fatal("route result missing", candidate)
				}
			})
		}
	}
}

// R-XVAN-R0R1 R-XQKF-BRHF
func TestRunRouteTimeoutUsesProcessTimer(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	h.env["GOOGLE_MAPS_API_KEY"] = "fixture-key"
	id := seedRunRoute(t, h)
	_, st := runStore(t, h.p.Dir, h.p.Now)
	timers := make(chan chan time.Time, 4)
	h.p.After = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		if d == cli.SweepInterval {
			return ch
		}
		if d != routes.Timeout {
			t.Errorf("unexpected route timer %v", d)
		}
		running, err := st.RunningPasses(t.Context())
		if err != nil {
			t.Error(err)
		}
		active := false
		for _, p := range running {
			active = active || p.Kind == store.KindEnrichTarget
		}
		if !active {
			t.Error("route timer outside targeted pass", running)
		}
		timers <- ch
		return ch
	}
	arrived := make(chan struct{}, 1)
	ended := make(chan struct{}, 1)
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		arrived <- struct{}{}
		<-r.Context().Done()
		ended <- struct{}{}
		return nil, r.Context().Err()
	})}
	h.start()
	c := identity.Caller{UserID: "owner"}
	r, p := h.call(c, "enrich", tools.EnrichArgs{Kind: enrichers.KindDriveRoute, Candidate: id})
	if r.IsError() {
		t.Fatal(r)
	}
	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("route request missing")
	}
	var timer chan time.Time
	select {
	case timer = <-timers:
	case <-time.After(3 * time.Second):
		t.Fatal("route timer missing")
	}
	select {
	case <-ended:
		t.Fatal("request ended before timer delivery")
	default:
	}
	timer <- time.Time{}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("route request not canceled")
	}
	if runText(h.waitPass(c, runText(p, "id")), "status") != store.StatusDone {
		t.Fatal(p)
	}
	h.stop()
}

// R-XQF2-7XS9
func TestRunRouteFailureKeepsOriginInputs(t *testing.T) {
	for _, mode := range []string{"unset", "empty", "custom"} {
		t.Run(mode, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir(), "")
			h.env["GOOGLE_MAPS_API_KEY"] = "fixture-key"
			origin := settings.DefaultRouteOrigin
			if mode == "empty" {
				h.env["CANDIDATES_ROUTE_ORIGIN"] = ""
			}
			if mode == "custom" {
				origin = "1 Main St, Hinckley, MN"
				h.env["CANDIDATES_ROUTE_ORIGIN"] = origin
			}
			h.p.HTTP = &http.Client{Transport: enrichTransport(func(*http.Request) (*http.Response, error) { return nil, io.EOF })}
			websiteTimers(t, h, nil)
			id := seedRunRoute(t, h)
			h.start()
			c := identity.Caller{UserID: "owner"}
			r, p := h.call(c, "enrich", tools.EnrichArgs{Kind: enrichers.KindDriveRoute, Candidate: id})
			if r.IsError() {
				t.Fatal(r)
			}
			if runText(h.waitPass(c, runText(p, "id")), "status") != store.StatusDone {
				t.Fatal(p)
			}
			_, answer := h.call(c, "candidate", map[string]any{"candidate": id})
			raw, _ := json.Marshal(answer)
			var detail tools.Detail
			if err := json.Unmarshal(raw, &detail); err != nil {
				t.Fatal(err)
			}
			want := (enrichers.DriveRoute{}).Inputs(enrichers.Subject{RecordAddress: sources.Sourced[sources.Address]{Value: runRouteAddress()}, Origin: origin})
			if len(detail.EnrichmentResults) != 1 || detail.EnrichmentResults[0].Kind != enrichers.KindDriveRoute || detail.EnrichmentResults[0].Inputs != want {
				t.Fatal(detail.EnrichmentResults, want)
			}
			h.stop()
		})
	}
}

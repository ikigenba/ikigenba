package cli_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
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

// R-N0O7-7ZV6 R-JLLH-JDUS
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

// R-N1W3-LRLV R-S3W5-BXOB
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

// R-IODZ-J5I5 R-YDTV-I753
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

// R-YCLZ-4FEE
func TestRunManualEnrichLifecycle(t *testing.T) {
	for _, fetched := range []bool{false, true} {
		t.Run(map[bool]string{false: "derived", true: "fetched"}[fetched], func(t *testing.T) {
			h := newRunHarness(t, t.TempDir(), "")
			if fetched {
				h.p.HTTP = &http.Client{Transport: enrichTransport(func(*http.Request) (*http.Response, error) { t.Error("empty pass fetched"); return nil, io.EOF })}
			}
			h.start()
			c := identity.Caller{UserID: "chosen-owner", RequestID: "chosen-request"}
			r, p := h.call(c, "enrich", tools.EnrichArgs{})
			if r.IsError() {
				t.Fatal(r)
			}
			id := runText(p, "id")
			done := h.waitPass(c, id)
			h.stop()
			bus := h.bus.Events()
			if len(bus) != 2 {
				t.Fatal(bus)
			}
			for i, e := range bus {
				if e.Name != []string{"pass.started", "pass.finished"}[i] || e.RequestID != c.RequestID || e.User != c.UserID || e.Cause != "" || e.Depth != 0 || e.Attrs["pass"] != id || e.Attrs["kind"] != store.KindEnrich || e.Attrs["origin"] != store.OriginEnrich || e.Attrs["trigger"] != store.TriggerEnrich {
					t.Fatal(e)
				}
				count := 0
				for _, te := range h.telemetry.Events() {
					if te.Name == e.Name && te.RequestID == e.RequestID && te.User == e.User && reflect.DeepEqual(map[string]any(te.Attrs), map[string]any(e.Attrs)) {
						count++
					}
				}
				if count != 1 {
					t.Fatal(count, e)
				}
			}
			for _, k := range []string{"status", "reason", "derived", "due", "fetched", "https", "http", "none"} {
				var value any
				if raw := done[k]; len(raw) > 0 {
					if err := json.Unmarshal(raw, &value); err != nil {
						t.Fatal(err)
					}
				} else {
					value = ""
				}
				got := bus[1].Attrs[k]
				if n, ok := value.(float64); ok {
					if got != int64(n) {
						t.Fatal(k, got, value)
					}
				} else if got != value {
					t.Fatal(k, got, value)
				}
			}
		})
	}
}

// R-YF73-UM6K
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

// R-GBFU-DOJ9
func TestRunFillsFactsBeforeReady(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	id := seedRunWebsite(t, h, "ops@ready.test")
	d, st := runStore(t, h.p.Dir, h.p.Now)
	found, err := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://ready.test/"})
	if err != nil {
		t.Fatal(err)
	}
	es, err := st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "ready.test", Source: "ready.test", Found: found}})
	if err != nil || len(es) != 1 {
		t.Fatal(es, err)
	}
	if err := d.Write(t.Context(), func(tx *sql.Tx) error { _, err := tx.ExecContext(t.Context(), "DELETE FROM facts"); return err }); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	d, st = runStore(t, h.p.Dir, h.p.Now)
	f, err := st.GetFacts(t.Context(), id)
	if err != nil || f.Website != sources.WebsiteHTTPS || f.WebsiteURL != "https://ready.test/" || len(f.Written) != 1 || f.Written[0].Kind != sources.WebsiteCheck || !f.Written[0].At.Equal(es[0].At) {
		t.Fatal(f, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	h.stop()
}

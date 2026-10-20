package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func assertEnrichEvents(t *testing.T, h *runHarness, c identity.Caller, done map[string]json.RawMessage, kind, target, enrichKind string) {
	t.Helper()
	id := runText(done, "id")
	bus := h.bus.Events()
	filtered := bus[:0]
	for _, e := range bus {
		if e.Attrs["pass"] == id {
			filtered = append(filtered, e)
		}
	}
	bus = filtered
	if len(bus) != 2 {
		t.Fatalf("bus lifecycle: %v", bus)
	}
	telemetry := h.telemetry.Events()
	at := -1
	for i, e := range bus {
		if e.Name != []string{"pass.started", "pass.finished"}[i] || e.RequestID != c.RequestID || e.User != c.UserID || e.Cause != "" || e.Depth != 0 {
			t.Fatalf("lifecycle context: %v", e)
		}
		for key, want := range map[string]string{"pass": id, "kind": kind, "origin": store.OriginEnrich, "trigger": store.TriggerEnrich, "target": target, "enrich_kind": enrichKind} {
			if e.Attrs[key] != want {
				t.Fatalf("%s: got %v want %s", key, e.Attrs[key], want)
			}
		}
		count, next := 0, -1
		for j, te := range telemetry {
			if te.Name == e.Name && te.Attrs["pass"] == id {
				count++
				next = j
				if te.RequestID != c.RequestID || te.User != c.UserID || !reflect.DeepEqual(map[string]any(te.Attrs), map[string]any(e.Attrs)) {
					t.Fatalf("trail lifecycle: %v; bus %v", te, e)
				}
			}
		}
		if count != 1 || next <= at {
			t.Fatalf("trail lifecycle count/order: %d %d %d", count, next, at)
		}
		at = next
	}
	for _, key := range []string{"status", "reason", "derived", "due", "fetched", "https", "http", "none", "runs", "runs_failed", "pages", "cost"} {
		got, err := json.Marshal(bus[1].Attrs[key])
		if err != nil {
			t.Fatal(err)
		}
		want := done[key]
		if len(want) == 0 {
			want = json.RawMessage(`""`)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("finished %s: event %s record %s", key, got, want)
		}
	}
}

// R-YR12-G0SQ
func TestRunCorpusEnrichEvents(t *testing.T) {
	for _, httpMode := range []string{"nil", "injected", "sandbox"} {
		t.Run(httpMode, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir(), "")
			if httpMode == "injected" {
				h.p.HTTP = &http.Client{Transport: enrichTransport(func(*http.Request) (*http.Response, error) { t.Error("empty corpus fetched"); return nil, io.EOF })}
			}
			if httpMode == "sandbox" {
				h.env["IKIGENBA_SANDBOX"] = ""
			}
			h.start()
			caller := identity.Caller{UserID: "corpus-owner", RequestID: "corpus-request"}
			r, p := h.call(caller, "enrich", tools.EnrichArgs{})
			if r.IsError() {
				t.Fatal(r)
			}
			done := h.waitPass(caller, runText(p, "id"))
			h.stop()
			assertEnrichEvents(t, h, caller, done, store.KindEnrich, "", "")
		})
	}
}

// R-X39Z-P9GP
func TestRunTargetedEnrichEvents(t *testing.T) {
	for _, kind := range []string{enrichers.KindLinkFacts, sources.WebsiteCheck, enrichers.KindSiteText} {
		for _, targetType := range []string{"candidate", "prospect_list"} {
			t.Run(kind+"/"+targetType, func(t *testing.T) {
				fake := &promptFake{}
				h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"prompts": runSocket(t, fake)}))
				candidate := seedPromptCandidate(t, h)
				h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(bytes.NewBufferString("fixture")), Request: r}, nil
				})}
				h.p.After = func(d time.Duration) <-chan time.Time {
					ch := make(chan time.Time, 1)
					switch d {
					case cli.SweepInterval:
					case website.Timeout:
					case website.Interval, passes.PollInterval, passes.QueueWait:
						ch <- time.Time{}
					default:
						t.Errorf("unexpected timer %v", d)
					}
					return ch
				}
				h.start()
				caller := identity.Caller{UserID: "target-owner", RequestID: "target-request"}
				target := candidate
				args := tools.EnrichArgs{Kind: kind, Candidate: candidate, Force: true}
				if targetType == "prospect_list" {
					r, v := h.call(caller, "create_prospect_list", map[string]any{"name": "fixture prospect list", "filters": map[string]any{}, "size": 10})
					if r.IsError() {
						t.Fatal(r)
					}
					target = runText(v, "id")
					args.Candidate = ""
					args.ProspectList = target
				}
				r, p := h.call(caller, "enrich", args)
				if r.IsError() {
					t.Fatal(r)
				}
				done := h.waitPass(caller, runText(p, "id"))
				if runText(done, "status") != store.StatusDone {
					t.Fatal(done)
				}
				h.stop()
				assertEnrichEvents(t, h, caller, done, store.KindEnrichTarget, target, kind)
			})
		}
	}
}

// R-1WFQ-0ORB
func TestRunPassTimersHavePermittedDurationsAndScope(t *testing.T) {
	t.Run("add_page", func(t *testing.T) {
		h := newRunHarness(t, t.TempDir(), "")
		candidate := seedPromptCandidate(t, h)
		var answering atomic.Bool
		var fetchTimers atomic.Int64
		h.p.After = func(wait time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			if wait != cli.SweepInterval {
				if wait != website.Timeout || !answering.Load() {
					t.Errorf("idle timer %v outside add_page", wait)
				}
				fetchTimers.Add(1)
			}
			return ch
		}
		h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(bytes.NewBufferString("<p>fixture page</p>")), Request: r}, nil
		})}
		h.start()
		caller := identity.Caller{UserID: "page-owner"}
		answering.Store(true)
		r, _ := h.call(caller, "add_page", tools.AddPageArgs{Candidate: candidate, URL: "https://secure.test/about"})
		answering.Store(false)
		if r.IsError() || fetchTimers.Load() != 1 {
			t.Fatal(r, fetchTimers.Load())
		}
		_, v := h.call(caller, "passes", tools.PassesArgs{})
		if string(v["passes"]) != "[]" {
			t.Fatal(v)
		}
		h.stop()
	})

	for _, kind := range []string{sources.WebsiteCheck, enrichers.KindSiteText} {
		t.Run(kind, func(t *testing.T) {
			fake := &promptFake{mode: "queue"}
			h := newRunHarness(t, t.TempDir(), runServices(t, map[string]string{"prompts": runSocket(t, fake)}))
			candidate := seedPromptCandidate(t, h)
			_, st := runStore(t, h.p.Dir, h.p.Now)
			h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(bytes.NewBufferString("fixture")), Request: r}, nil
			})}
			timers := make(chan time.Duration, 32)
			h.p.After = func(wait time.Duration) <-chan time.Time {
				ch := make(chan time.Time, 1)
				if wait != cli.SweepInterval {
					running, err := st.RunningPasses(t.Context())
					if err != nil {
						t.Error(err)
					}
					valid := false
					for _, p := range running {
						switch wait {
						case website.Interval, website.Timeout:
							valid = valid || p.Kind == store.KindEnrich || p.Kind == store.KindEnrichTarget
						case passes.PollInterval, passes.QueueWait:
							valid = valid || p.Kind == store.KindEnrichTarget
						}
					}
					if !valid {
						t.Errorf("timer %v outside permitted running pass: %v", wait, running)
					}
					if wait != website.Timeout {
						ch <- time.Time{}
					}
				}
				timers <- wait
				return ch
			}
			h.start()
			caller := identity.Caller{UserID: "timer-owner"}
			r, p := h.call(caller, "enrich", tools.EnrichArgs{Kind: kind, Candidate: candidate, Force: true})
			if r.IsError() {
				t.Fatal(r)
			}
			done := h.waitPass(caller, runText(p, "id"))
			if runText(done, "status") != store.StatusDone {
				t.Fatal(done)
			}
			h.stop()
			close(timers)
			seen := map[time.Duration]bool{}
			for wait := range timers {
				seen[wait] = true
			}
			if !seen[cli.SweepInterval] {
				t.Fatal(seen)
			}
			if kind == sources.WebsiteCheck && !seen[website.Timeout] {
				t.Fatal(seen)
			}
			if kind == enrichers.KindSiteText && (!seen[passes.PollInterval] || !seen[passes.QueueWait]) {
				t.Fatal(seen)
			}
		})
	}
}

// R-R6QU-SEXX
func TestRunBusyJudgeDeliveryEmitsNoPassEvents(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	h.start()
	_, st := runStore(t, h.p.Dir, h.p.Now)
	p, err := st.AddPass(t.Context(), store.Pass{Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge, Delivery: "evt_4444444444444444"})
	if err != nil {
		t.Fatal(err)
	}
	event := events.Event{ID: "evt_5555555555555555", Time: h.p.Now(), Received: h.p.Now(), Seq: 1, Service: "cron", Name: "cron.judge_candidates.fired", Attrs: events.Attrs{}}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var delivery map[string]any
	if err = json.Unmarshal(raw, &delivery); err != nil {
		t.Fatal(err)
	}
	delivery["attempt"] = 1
	raw, err = json.Marshal(delivery)
	if err != nil {
		t.Fatal(err)
	}
	status, body := h.request("POST", "/events", "refused-judge", raw)
	var outcome map[string]string
	if err = json.Unmarshal(body, &outcome); err != nil || status != http.StatusInternalServerError || outcome["outcome"] != events.OutcomeError {
		t.Fatalf("busy delivery %d %s %v", status, body, err)
	}
	if _, err = st.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
		t.Fatal(err)
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

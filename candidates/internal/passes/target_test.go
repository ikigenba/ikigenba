package passes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

type promptCall struct {
	name                 string
	args                 map[string]json.RawMessage
	user, email, request string
}
type promptFake struct {
	mu     sync.Mutex
	calls  []promptCall
	answer func(string, map[string]json.RawMessage) (any, bool, int)
}

func fakePrompts(t *testing.T, f *fixture, answer func(string, map[string]json.RawMessage) (any, bool, int)) (*prompts.Client, *promptFake) {
	t.Helper()
	dir, err := os.MkdirTemp("", "pass-prompts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	fake := &promptFake{answer: answer}
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string                     `json:"name"`
				Arguments map[string]json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		fake.mu.Lock()
		fake.calls = append(fake.calls, promptCall{request.Params.Name, request.Params.Arguments, r.Header.Get("X-User-Id"), r.Header.Get("X-User-Email"), r.Header.Get("X-Request-Id")})
		fake.mu.Unlock()
		result, refused, status := answer(request.Params.Name, request.Params.Arguments)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		body := map[string]any{"content": []any{}, "structuredContent": result}
		if refused {
			body = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": result}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": body})
	})}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })
	return prompts.NewClient(prompts.ClientConfig{Service: services.Entry{Name: "prompts", Socket: socket}, Telemetry: f.w}), fake
}
func (f *promptFake) snapshot() []promptCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]promptCall(nil), f.calls...)
}
func promptAnswer(name string, args map[string]json.RawMessage) (any, bool, int) {
	switch name {
	case "show":
		var prompt string
		_ = json.Unmarshal(args["name"], &prompt)
		return map[string]any{"id": "catalog", "name": prompt, "model": "model", "prompt": "instructions", "system": "system", "tools": []string{}}, false, 0
	case "run":
		return map[string]any{"id": "run-one", "status": "queued"}, false, 0
	default:
		return map[string]any{"id": "run-one", "prompt": "catalog", "model": "model", "status": "exited", "exit_code": 0, "started": "2000-01-01T00:00:00Z", "cost_nanos": 1000}, false, 0
	}
}
func seedTarget(t *testing.T, f *fixture, n int) []string {
	t.Helper()
	ctx := context.Background()
	observations := make([]store.Observation, n)
	for i := range n {
		observations[i] = store.Observation{Scraper: "fmcsa", SourceID: fmt.Sprint(i), Digest: fmt.Sprint(i), Reading: sources.Reading{Name: fmt.Sprintf("Company %d", i), Email: fmt.Sprintf("owner@company%d.test", i), NameKey: fmt.Sprintf("company%d", i)}}
	}
	_, ids, err := f.st.Attach(ctx, "seed", false, observations)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		b, _ := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: fmt.Sprintf("https://company%d.test", i)})
		_, err = f.st.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: fmt.Sprintf("company%d.test", i), Source: "fixture", Found: b}})
		if err != nil {
			t.Fatal(err)
		}
	}
	return ids
}
func targetEngine(f *fixture, pc *prompts.Client, after func(time.Duration) <-chan time.Time) {
	f.eng.Drain(context.Background())
	f.eng = passes.New(passes.Config{Store: f.st, Scrapes: f.sc, Prompts: pc, Telemetry: f.w, Events: f.em, After: after})
}

// R-SCYD-LA7N R-84KF-D8N0 R-85SB-R0DP R-SE69-Z1YC R-SFE6-CTP1
func TestTargetRecordAndSchedulingValues(t *testing.T) {
	if passes.RunsInFlight != 4 || passes.StartTries != 20 || passes.PollInterval != 5*time.Second || passes.QueueWait != 30*time.Second || passes.NoPrompt == "" || strings.Contains(passes.NoPrompt, "\n") {
		t.Fatal("scheduling values")
	}
	rf := passes.Refusal{"word", "detail"}
	if rf.Error() != "word: detail" {
		t.Fatal(rf.Error())
	}
	p := store.Pass{Kind: store.KindEnrichCohort, Origin: store.OriginEnrich, Target: "cand_target", EnrichKind: enrichers.KindSiteText, Runs: 2, RunsFailed: 1, Pages: 3, Cost: 1000, UserID: "owner", UserEmail: "mail"}
	r := passes.RecordOf(p)
	if r.Target != p.Target || r.EnrichKind != p.EnrichKind || r.Runs != 2 || r.RunsFailed != 1 || r.Pages != 3 || r.Cost != 1000 || r.User != "owner" || r.Email != "mail" {
		t.Fatal(r)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"target":"cand_target","enrich_kind":"site_text"`) || !strings.Contains(string(b), `"runs":2,"runs_failed":1,"pages":3,"cost":1000`) {
		t.Fatal(string(b))
	}
}

// R-TDJD-2EGH R-TER9-G676 R-TFZ5-TXXV R-SGM2-QLFQ
func TestTargetRefusals(t *testing.T) {
	for _, mode := range []string{"kind", "target", "missing", "unavailable", "prompt", "transport", "store", "stopping", "busy"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, false)
			ids := seedTarget(t, f, 1)
			pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
				if mode == "prompt" {
					return "fixture refusal", true, 0
				}
				if mode == "transport" {
					return nil, false, 503
				}
				return promptAnswer(name, args)
			})
			if mode == "unavailable" {
				pc = nil
			}
			targetEngine(f, pc, promptAfter)
			kind, target, want := enrichers.KindSiteText, ids[0], passes.RefusalInvalid
			switch mode {
			case "kind":
				kind = ""
			case "target":
				target = "bad"
				want = passes.RefusalNotFound
			case "missing":
				target = "cand_0000000000000000"
				want = passes.RefusalNotFound
			case "unavailable", "transport":
				want = passes.RefusalUnavailable
			case "store":
				f.d.SetFailing(true)
			case "stopping":
				f.eng.Drain(context.Background())
				want = passes.RefusalStopping
			case "busy":
				_, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindEnrichCohort, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Target: target, EnrichKind: enrichers.KindLinkFacts, UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
				want = passes.RefusalBusy
			}
			p, err := f.eng.EnrichCohort(context.Background(), identity.Caller{UserID: "owner"}, kind, target, false)
			if mode == "store" {
				f.d.SetFailing(false)
				var rf *passes.Refusal
				if !errors.Is(err, store.ErrUnreachable) || errors.As(err, &rf) || p.ID != "" || len(fake.snapshot()) != 0 {
					t.Fatal(p, err, fake.snapshot())
				}
				f.flush()
				if len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 || len(f.list()) != 0 {
					t.Fatal("failed read side effects")
				}
				return
			}
			assertRefusal(t, p, err, want)
			if mode == "prompt" {
				if err.Error() != passes.RefusalInvalid+": "+passes.NoPrompt+" site-text: fixture refusal" {
					t.Fatal(err)
				}
			}
			for _, call := range fake.snapshot() {
				if call.name != "show" {
					t.Fatal(call)
				}
			}
		})
	}
}

// R-TH72-7POK R-TIEY-LHF9 R-TJMU-Z95Y R-TOIG-IC4Q R-33P6-601K R-2NUH-6ZEJ R-TY9N-KI2A R-38KR-P30C R-301H-0OTH
func TestPromptPassResultsAndForce(t *testing.T) {
	for _, status := range []string{"exited", "failed", "killed", "timed_out"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, nil, false)
			id := seedTarget(t, f, 1)[0]
			pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
				result, refused, code := promptAnswer(name, args)
				if name == "result" {
					result.(map[string]any)["status"] = status
				}
				return result, refused, code
			})
			targetEngine(f, pc, promptAfter)
			caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "request"}
			for i, force := range []bool{false, false, true} {
				p, err := f.eng.EnrichCohort(context.Background(), caller, enrichers.KindSiteText, id, force)
				if err != nil {
					t.Fatal(err)
				}
				if p.Kind != store.KindEnrichCohort || p.Target != id || p.EnrichKind != enrichers.KindSiteText || p.Status != store.StatusRunning || p.UserID != caller.UserID || p.UserEmail != caller.Email || p.RequestID != caller.RequestID {
					t.Fatal(p)
				}
				p = f.ended(p.ID)
				want := int64(1)
				if i == 1 {
					want = 0
				}
				if p.Status != store.StatusDone || p.Due != want || p.Runs != want || p.Cost != 1000*want || p.Derived+p.Fetched+p.HTTPS+p.HTTP+p.None != 0 {
					t.Fatal(p)
				}
				if status != "exited" && p.RunsFailed != want {
					t.Fatal(p)
				}
				for _, other := range f.list() {
					if other.Trigger == p.ID {
						t.Fatal("unexpected follow-on", other)
					}
				}
			}
			es, err := f.st.Enrichments(context.Background(), id)
			if err != nil || len(es) != 3 {
				t.Fatal(es, err)
			}
			subject, err := f.st.Subject(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			expected := enrichers.SiteText{}.Input(subject)
			starts := 0
			for _, call := range fake.snapshot() {
				if call.user != caller.UserID || call.email != caller.Email || call.request != caller.RequestID {
					t.Fatal(call)
				}
				if call.name == "run" {
					starts++
					var a, b any
					_ = json.Unmarshal(call.args["input"], &a)
					_ = json.Unmarshal(expected, &b)
					if !reflect.DeepEqual(a, b) {
						t.Fatal(a, b)
					}
				}
			}
			if starts != 2 {
				t.Fatal(starts)
			}
		})
	}
}

// R-TVTU-SYKW R-34X2-JRS9 R-2WDR-VDLE R-319D-EGK6 R-T1CD-8P1J
func TestPromptRetriesAndFailures(t *testing.T) {
	for _, mode := range []string{"retry", "exhausted", "result", "invalid", "show_retry"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, false)
			id := seedTarget(t, f, 1)[0]
			var mu sync.Mutex
			attempts, shows := 0, 0
			waits := []time.Duration{}
			pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
				mu.Lock()
				defer mu.Unlock()
				if name == "show" {
					shows++
					if mode == "show_retry" && shows > 1 {
						return "deleted", true, 0
					}
				}
				if name == "run" {
					attempts++
					if mode == "exhausted" || ((mode == "retry" || mode == "show_retry") && attempts == 1) {
						return "queue", true, 0
					}
				}
				if name == "result" && mode == "result" {
					return nil, false, 500
				}
				result, refused, code := promptAnswer(name, args)
				if name == "result" && mode == "invalid" {
					result.(map[string]any)["cost_nanos"] = -1
				}
				return result, refused, code
			})
			targetEngine(f, pc, func(d time.Duration) <-chan time.Time {
				mu.Lock()
				waits = append(waits, d)
				mu.Unlock()
				return promptAfter(d)
			})
			ctx, cancel := context.WithCancel(context.Background())
			p, err := f.eng.EnrichCohort(ctx, identity.Caller{UserID: "owner"}, enrichers.KindSiteText, id, false)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			p = f.ended(p.ID)
			if mode == "retry" {
				if p.Status != store.StatusDone || p.Runs != 1 {
					t.Fatal(p)
				}
			} else if p.Status != store.StatusFailed || p.Reason != store.ReasonPrompts || p.Runs+p.Cost != 0 {
				t.Fatal(p)
			}
			mu.Lock()
			if mode == "exhausted" && attempts != passes.StartTries {
				t.Fatal(attempts)
			}
			if mode == "retry" && (attempts != 2 || shows != 2 || len(waits) < 2 || waits[0] != passes.QueueWait || waits[1] != passes.PollInterval) {
				t.Fatal(attempts, shows, waits)
			}
			mu.Unlock()
			calls := fake.snapshot()
			if len(calls) == 0 || calls[0].name != "show" {
				t.Fatal(calls)
			}
		})
	}
}

func promptAfter(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

// R-TKUR-D0WN R-TNAK-4KE1 R-TIEY-LHF9 R-38KR-P30C
func TestTargetedDerivedAndWebsite(t *testing.T) {
	for _, kind := range []string{enrichers.KindLinkFacts, sources.WebsiteCheck} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, nil, false)
			ids := seedTarget(t, f, 3)
			f.checker(enrichTransport(func(r *http.Request) (*http.Response, error) {
				return pageResponse(r), nil
			}))
			ctx := context.Background()
			// Every candidate already has a current website result; forced targets
			// record a fresh result while candidates outside the target stay untouched.
			for _, id := range ids {
				if _, err := f.st.DeriveAmong(ctx, enrichers.KindLinkFacts, []string{id}, false); err != nil {
					t.Fatal(err)
				}
			}
			// Avoid judgment membership requirements by targeting one explicit candidate.
			before := map[string]int{}
			for _, id := range ids {
				es, err := f.st.Enrichments(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				before[id] = len(es)
			}
			for _, force := range []bool{false, true} {
				p, err := f.eng.EnrichCohort(ctx, identity.Caller{UserID: "owner"}, kind, ids[0], force)
				if err != nil {
					t.Fatal(err)
				}
				p = f.ended(p.ID)
				want := int64(0)
				if force {
					want = 1
				}
				if p.Status != store.StatusDone {
					t.Fatal(p)
				}
				if kind == enrichers.KindLinkFacts {
					if p.Derived != want || p.Due+p.Fetched != 0 {
						t.Fatal(p)
					}
				} else if p.Due != want || p.Fetched != want || p.HTTPS != want || p.Derived != 0 {
					t.Fatal(p)
				}
				if force {
					waitFollow(t, f, p.ID)
				}
			}
			for _, id := range ids {
				es, err := f.st.Enrichments(ctx, id)
				want := before[id]
				if id == ids[0] {
					want++
				}
				if err != nil || len(es) != want {
					t.Fatal(id, len(es), want, err)
				}
			}
		})
	}
}

// R-TPQC-W3VF R-319D-EGK6 R-T9VN-X38E R-TB3K-AUZ3 R-TH72-7POK
func TestPromptConcurrencyAndDrain(t *testing.T) {
	f := newFixture(t, nil, false)
	ids := seedTarget(t, f, 10)
	ctx := context.Background()
	// A cohort with website results becomes populated after a judge.
	if _, err := f.st.JudgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	cohort, err := f.st.CreateCohort(ctx, store.Cohort{Name: "targets", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 20)
	polls := make(chan chan time.Time, 20)
	var mu sync.Mutex
	next := 0
	pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
		if name == "run" {
			mu.Lock()
			next++
			id := fmt.Sprintf("run-%d", next)
			mu.Unlock()
			started <- struct{}{}
			return map[string]any{"id": id, "status": "queued"}, false, 0
		}
		return promptAnswer(name, args)
	})
	targetEngine(f, pc, func(d time.Duration) <-chan time.Time {
		if d != passes.PollInterval {
			t.Errorf("timer %s", d)
		}
		ch := make(chan time.Time, 1)
		polls <- ch
		return ch
	})
	p, err := f.eng.EnrichCohort(ctx, identity.Caller{UserID: "owner"}, enrichers.KindSiteText, cohort.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for range passes.RunsInFlight {
		waitSignal(t, started)
	}
	first := <-polls
	// With all poll channels held, no result request can have arrived.
	for _, call := range fake.snapshot() {
		if call.name == "result" {
			t.Fatal(call)
		}
	}
	mu.Lock()
	if next != passes.RunsInFlight {
		t.Fatal(next)
	}
	mu.Unlock()
	first <- time.Time{}
	waitSignal(t, started)
	f.eng.Drain(ctx)
	p = f.ended(p.ID)
	if p.Status != store.StatusFailed || p.Reason != store.ReasonInterrupted {
		t.Fatal(p)
	}
	for _, id := range ids {
		es, err := f.st.Enrichments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(es) > 2 {
			t.Fatal(es)
		}
	}
	_, err = f.eng.EnrichCohort(ctx, identity.Caller{UserID: "owner"}, enrichers.KindSiteText, ids[0], false)
	var refused *passes.Refusal
	if !errors.As(err, &refused) || refused.Word != passes.RefusalStopping {
		t.Fatal(err)
	}
}

// R-T3S6-08IX: rebuild requests current then rows for its registered source.
func TestRebuildRequestSequence(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/current") {
			record(w, "run_a", 1, store.StatusDone)
		} else {
			row(w, 1)
		}
	}, true)
	p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	requests := f.requests()
	if p.Status != store.StatusDone || len(requests) != 2 || requests[0].path != "/scrapers/fmcsa/current" || requests[1].path != "/runs/run_a/rows" {
		t.Fatal(p, requests)
	}
}

// R-33P6-601K R-2NUH-6ZEJ: distinct URLs are result data; page copies are pass cost.
func TestPromptCountsPagesAndOutcome(t *testing.T) {
	f := newFixture(t, nil, false)
	id := seedTarget(t, f, 1)[0]
	ctx := context.Background()
	if _, err := f.st.AddPage(ctx, store.Page{Candidate: id, URL: "https://company0.test/preexisting", Text: "old", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	pc, _ := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
		result, refused, code := promptAnswer(name, args)
		if name == "result" {
			for range 2 {
				if _, err := f.st.AddPage(ctx, store.Page{Candidate: id, URL: "https://company0.test/new", Text: "new", UserID: "owner"}); err != nil {
					t.Error(err)
				}
			}
			result.(map[string]any)["exit_code"] = 3
		}
		return result, refused, code
	})
	targetEngine(f, pc, promptAfter)
	p, err := f.eng.EnrichCohort(ctx, identity.Caller{UserID: "owner"}, enrichers.KindSiteText, id, false)
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Runs != 1 || p.RunsFailed != 1 || p.Pages != 2 || p.Cost != 1000 {
		t.Fatal(p)
	}
	es, err := f.st.Enrichments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var found enrichers.SiteTextFound
	for _, e := range es {
		if e.Kind == enrichers.KindSiteText {
			if err := json.Unmarshal(e.Found, &found); err != nil {
				t.Fatal(err)
			}
			if e.Source != enrichers.SourcePrompts || e.Cost != 0.000001 {
				t.Fatal(e)
			}
		}
	}
	if found.Run != "run-one" || found.Status != "exited" || found.ExitCode == nil || *found.ExitCode != 3 || found.Pages != 2 || found.CostNanos != 1000 {
		t.Fatal(found)
	}
}

// R-32H9-S8AV: the default poll reaches a completed run within sixty seconds.
func TestPromptDefaultPoll(t *testing.T) {
	f := newFixture(t, nil, false)
	id := seedTarget(t, f, 1)[0]
	pc, _ := fakePrompts(t, f, promptAnswer)
	targetEngine(f, pc, nil)
	p, err := f.eng.EnrichCohort(context.Background(), identity.Caller{UserID: "owner"}, enrichers.KindSiteText, id, false)
	if err != nil {
		t.Fatal(err)
	}
	p = f.endedWithin(p.ID, 60*time.Second)
	if p.Status != store.StatusDone || p.Runs != 1 {
		t.Fatal(p)
	}
}

// R-TIEY-LHF9 R-TKUR-D0WN R-TNAK-4KE1 R-TOIG-IC4Q R-38KR-P30C
func TestCohortSelectionSkipsNoWebsiteAndPreservesOutside(t *testing.T) {
	f := newFixture(t, nil, false)
	ids := seedTarget(t, f, 4)
	ctx := context.Background()
	// One candidate has a website result whose URL is empty.
	empty, _ := json.Marshal(website.Result{State: sources.WebsiteNone})
	if _, err := f.st.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids[2], Kind: sources.WebsiteCheck, Inputs: "company2.test", Source: "fixture", Found: empty}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.JudgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	cohort, err := f.st.CreateCohort(ctx, store.Cohort{Name: "selected", Size: 3, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	// Stand a rejection for the fourth so the target set is explicit.
	if _, err := f.st.Reject(ctx, store.Reject{Kind: store.KindReject, Cohort: cohort.ID, Candidate: ids[3], Why: "outside", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	pc, fake := fakePrompts(t, f, promptAnswer)
	targetEngine(f, pc, promptAfter)
	p, err := f.eng.EnrichCohort(ctx, identity.Caller{UserID: "owner"}, enrichers.KindSiteText, cohort.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Due != 2 || p.Runs != 2 {
		t.Fatal(p)
	}
	starts := map[string]bool{}
	for _, call := range fake.snapshot() {
		if call.name == "run" {
			var input struct {
				Candidate string `json:"candidate"`
			}
			if err := json.Unmarshal(call.args["input"], &input); err != nil {
				t.Fatal(err)
			}
			starts[input.Candidate] = true
		}
	}
	if !starts[ids[0]] || !starts[ids[1]] || starts[ids[2]] || starts[ids[3]] {
		t.Fatal(starts, ids)
	}
	p, err = f.eng.EnrichCohort(ctx, identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts, cohort.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Derived != 3 {
		t.Fatal(p)
	}
	waitFollow(t, f, p.ID)
	es, err := f.st.Enrichments(ctx, ids[3])
	if err != nil || len(es) != 1 {
		t.Fatal(es, err)
	}
}

// R-34X2-JRS9 R-319D-EGK6: one failed result cancels other held polls without waiting.
func TestPromptFailureCancelsOtherRunsAndKeepsPriorResult(t *testing.T) {
	f := newFixture(t, nil, false)
	ids := seedTarget(t, f, 6)
	ctx := context.Background()
	if _, err := f.st.JudgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	cohort, err := f.st.CreateCohort(ctx, store.Cohort{Name: "fail-fast", Size: 6, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	serial := 0
	accepted := map[string]bool{}
	seen := map[string]int{}
	candidateByRun := map[string]string{}
	started := make(chan string, 10)
	polls := make(chan chan time.Time, 20)
	pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
		mu.Lock()
		defer mu.Unlock()
		if name == "run" {
			serial++
			id := fmt.Sprintf("accepted-%d", serial)
			accepted[id] = true
			var input struct {
				Candidate string `json:"candidate"`
			}
			_ = json.Unmarshal(args["input"], &input)
			candidateByRun[id] = input.Candidate
			started <- id
			return map[string]any{"id": id, "status": "queued"}, false, 0
		}
		if name == "result" {
			var run string
			_ = json.Unmarshal(args["run"], &run)
			if !accepted[run] {
				t.Error("unknown run", run)
			}
			seen[run]++
			if seen[run] == 1 {
				return map[string]any{"id": run, "prompt": "catalog", "model": "model", "status": "running", "started": "2000-01-01T00:00:00Z"}, false, 0
			}
			if serial > passes.RunsInFlight {
				return nil, false, 500
			}
			result, refused, code := promptAnswer(name, args)
			result.(map[string]any)["id"] = run
			return result, refused, code
		}
		return promptAnswer(name, args)
	})
	targetEngine(f, pc, func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); polls <- ch; return ch })
	p, err := f.eng.EnrichCohort(ctx, identity.Caller{UserID: "owner"}, enrichers.KindSiteText, cohort.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for range passes.RunsInFlight {
		<-started
	}
	// Release every initial poll once: all four outcomes remain running.
	for range passes.RunsInFlight {
		(<-polls) <- time.Time{}
	}
	// Each old run now has a second poll; finish one, admitting the fifth.
	second := <-polls
	second <- time.Time{}
	<-started
	// Complete one more old run's poll with a transport failure while others stay held.
	(<-polls) <- time.Time{}
	p = f.ended(p.ID)
	if p.Status != store.StatusFailed || p.Reason != store.ReasonPrompts {
		t.Fatal(p)
	}
	count := 0
	for _, id := range ids {
		es, err := f.st.Enrichments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range es {
			if e.Kind == enrichers.KindSiteText {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatal("prior completed result lost", count)
	}
	calls := len(fake.snapshot())
	f.eng.Drain(ctx)
	if len(fake.snapshot()) != calls {
		t.Fatal("request after pass ended")
	}
}

// R-SK9R-VWNT R-SMPK-NG57 R-SLHO-9OEI R-SGM2-QLFQ
func TestCorpusNeverRunsConfiguredPrompts(t *testing.T) {
	f := newFixture(t, nil, false)
	id := seedTarget(t, f, 1)[0]
	pc, fake := fakePrompts(t, f, promptAnswer)
	targetEngine(f, pc, promptAfter)
	for _, kind := range []string{enrichers.KindSiteText, enrichers.KindBrief} {
		p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, kind)
		assertRefusal(t, p, err, passes.RefusalInvalid)
	}
	f.flush()
	if len(f.list()) != 0 || len(fake.snapshot()) != 0 || len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
		t.Fatal("refusal side effects")
	}
	p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Derived != 1 {
		t.Fatal(p)
	}
	waitFollow(t, f, p.ID)
	es, err := f.st.Enrichments(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.Kind == enrichers.KindSiteText || e.Kind == enrichers.KindBrief {
			t.Fatal(e)
		}
	}
	if len(fake.snapshot()) != 0 {
		t.Fatal(fake.snapshot())
	}
}

// R-TB3K-AUZ3 R-TDJD-2EGH: one target's running pass admits exactly one racing start.
func TestConcurrentTargetStarts(t *testing.T) {
	f := newFixture(t, nil, false)
	id := seedTarget(t, f, 1)[0]
	pc, _ := fakePrompts(t, f, promptAnswer)
	targetEngine(f, pc, func(time.Duration) <-chan time.Time { return make(chan time.Time) })
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			_, err := f.eng.EnrichCohort(context.Background(), identity.Caller{UserID: "owner"}, enrichers.KindSiteText, id, false)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			var rf *passes.Refusal
			if !errors.As(err, &rf) || rf.Word != passes.RefusalBusy {
				t.Fatal(err)
			}
		}
	}
	if successes != 1 || len(f.list()) != 1 {
		t.Fatal(successes, f.list())
	}
	f.eng.Drain(context.Background())
}

// R-2NUH-6ZEJ R-2TXZ-3U40 R-33P6-601K R-TJMU-Z95Y R-TY9N-KI2A R-38KR-P30C
func TestBriefTargetLifecycleAndCounts(t *testing.T) {
	f := newFixture(t, nil, false)
	ctx := context.Background()
	ids := seedTarget(t, f, 6)
	inputs := map[string]json.RawMessage{}
	for _, id := range ids[:5] {
		if _, err := f.st.AddPage(ctx, store.Page{Candidate: id, URL: "https://pages.test/" + id, Text: "original text", UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
		s, err := f.st.Subject(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		inputs[id] = enrichers.Brief{}.Input(s)
	}
	if _, err := f.st.JudgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	cohort, err := f.st.CreateCohort(ctx, store.Cohort{Name: "brief targets", Size: 6, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.Reject(ctx, store.Reject{Kind: store.KindReject, Cohort: cohort.ID, Candidate: ids[5], Why: "outside", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
		if name == "run" {
			var prompt string
			_ = json.Unmarshal(args["name"], &prompt)
			if prompt != enrichers.PromptName(enrichers.KindBrief) {
				t.Error("wrong prompt", prompt)
			}
			var input struct {
				Candidate string `json:"candidate"`
			}
			_ = json.Unmarshal(args["input"], &input)
			var got, want any
			_ = json.Unmarshal(args["input"], &got)
			_ = json.Unmarshal(inputs[input.Candidate], &want)
			if !reflect.DeepEqual(got, want) {
				t.Error("brief input", got, want)
			}
			return map[string]any{"id": input.Candidate, "status": enrichers.RunQueued}, false, 0
		}
		result, refused, code := promptAnswer(name, args)
		if name == "result" {
			var id string
			_ = json.Unmarshal(args["run"], &id)
			r := result.(map[string]any)
			r["id"] = id
			r["stdout"] = `{"summary":"A working business.","categories":[],"labels":[]}`
			switch id {
			case ids[0]:
				r["stdout"] = `{"summary":"","categories":[],"labels":[]}`
			case ids[1]:
				r["stdout"] = "invalid output"
			case ids[2]:
				if _, err := f.st.AddPage(ctx, store.Page{Candidate: id, URL: "https://pages.test/additional", Text: "new text", UserID: "owner"}); err != nil {
					t.Error(err)
				}
			case ids[3]:
				r["status"] = "failed"
			case ids[4]:
				r["exit_code"] = 1
			default:
				t.Error("unexpected target", id)
			}
		}
		return result, refused, code
	})
	targetEngine(f, pc, promptAfter)
	caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "brief-request"}
	p, err := f.eng.EnrichCohort(ctx, caller, enrichers.KindBrief, cohort.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.EnrichKind != enrichers.KindBrief || p.Due != 5 || p.Runs != 5 || p.RunsFailed != 4 || p.Cost != 5000 || p.Pages != 0 || p.Derived+p.Fetched+p.HTTPS+p.HTTP+p.None+p.Rows+p.New+p.Changed+p.Unchanged+p.Gone+p.Created+p.Linked+p.Judged+p.Unranged != 0 {
		t.Fatal(p)
	}
	for i, id := range ids {
		es, err := f.st.Enrichments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, e := range es {
			if e.Kind != enrichers.KindBrief {
				continue
			}
			count++
			var found enrichers.BriefFound
			if err := json.Unmarshal(e.Found, &found); err != nil || found.Run != id || found.CostNanos != 1000 || found.Failed() != (i != 2) || e.Source != enrichers.SourcePrompts || e.Cost != 0.000001 {
				t.Fatal(e, found, err)
			}
		}
		want := 1
		if i == 5 {
			want = 0
		}
		if count != want {
			t.Fatal(id, count, want)
		}
	}
	for _, call := range fake.snapshot() {
		if call.user != caller.UserID || call.email != caller.Email || call.request != caller.RequestID {
			t.Fatal(call)
		}
	}
	for _, other := range f.list() {
		if other.Trigger == p.ID {
			t.Fatal("brief started follow-on", other)
		}
	}
	// A failed brief still records its inputs, so it is not run again unchanged.
	second, err := f.eng.EnrichCohort(ctx, caller, enrichers.KindBrief, ids[0], false)
	if err != nil {
		t.Fatal(err)
	}
	second = f.ended(second.ID)
	if second.Status != store.StatusDone || second.Due+second.Runs+second.RunsFailed+second.Pages+second.Cost != 0 {
		t.Fatal(second)
	}
}

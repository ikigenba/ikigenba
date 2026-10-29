package cli_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

type promptRequest struct {
	Tool   string
	Header http.Header
}
type promptFake struct {
	mu       sync.Mutex
	calls    []promptRequest
	mode     string
	echo     string
	runs     int
	stdout   string
	exitCode int
}

func (f *promptFake) requests() []promptRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]promptRequest{}, f.calls...)
}
func (f *promptFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Params struct {
			Name      string `json:"name"`
			Arguments struct {
				Name string `json:"name"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, promptRequest{req.Params.Name, r.Header.Clone()})
	mode := f.mode
	echo := f.echo
	stdout, exitCode := f.stdout, f.exitCode
	if req.Params.Name == "run" {
		f.runs++
	}
	runs := f.runs
	f.mu.Unlock()
	if mode == "http-"+req.Params.Name {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(echo))
		return
	}
	var value any
	refusal := false
	switch req.Params.Name {
	case "show":
		refusal = mode == "refuse-show"
		value = map[string]any{"id": "prompt_fixture", "name": req.Params.Arguments.Name, "model": "fixture", "prompt": "fixture", "system": "fixture", "tools": []string{}}
	case "run":
		refusal = mode == "queue" && runs == 1 || mode == "refuse-result" && req.Params.Name == "result"
		value = map[string]any{"id": "run_prompt", "status": "running"}
	case "result":
		refusal = mode == "refuse-result"
		value = map[string]any{"id": "run_prompt", "prompt": "site-text", "model": "fixture", "status": map[bool]string{true: "failed", false: "exited"}[mode == "failed"], "exit_code": exitCode, "stdout": stdout, "started": "2026-01-01T00:00:00Z", "finished": "2026-01-01T00:01:00Z"}
	}
	result := map[string]any{"content": []any{}, "structuredContent": value}
	if refusal {
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "fixture refusal"}}, "isError": true}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}
func seedPromptCandidate(t *testing.T, h *runHarness) string {
	t.Helper()
	id := seedRunWebsite(t, h, "ops@secure.test")
	d, st := runStore(t, h.p.Dir, h.p.Now)
	found, _ := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://secure.test"})
	if _, err := st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "secure.test", Source: "secure.test", Found: found}}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	return id
}

// R-49SO-SMNX R-8BVT-NV36
func TestRunPromptsTrailAndTimers(t *testing.T) {
	for _, mode := range []string{"success", "queue", "refuse-show"} {
		t.Run(mode, func(t *testing.T) {
			f := &promptFake{mode: mode}
			path := runServices(t, map[string]string{"prompts": runSocket(t, f)})
			h := newRunHarness(t, t.TempDir(), path)
			id := seedPromptCandidate(t, h)
			type timer struct {
				d  time.Duration
				ch chan time.Time
			}
			timers := make(chan timer, 10)
			h.p.After = func(d time.Duration) <-chan time.Time {
				ch := make(chan time.Time, 1)
				if d != time.Hour {
					timers <- timer{d, ch}
				}
				return ch
			}
			h.start()
			h.filled()
			caller := identity.Caller{UserID: "prompt-owner", RequestID: "prompt-request"}
			r, p := h.call(caller, "enrich", tools.EnrichArgs{Kind: enrichers.KindSiteText, Candidate: id})
			if mode == "refuse-show" {
				if !r.IsError() {
					t.Fatal(r)
				}
			} else {
				if r.IsError() {
					t.Fatal(r)
				}
				waits := []time.Duration{passes.PollInterval}
				if mode == "queue" {
					waits = []time.Duration{passes.QueueWait, passes.PollInterval}
				}
				for _, want := range waits {
					select {
					case tm := <-timers:
						if tm.d != want {
							t.Fatal(tm.d, want)
						}
						for _, q := range f.requests() {
							if q.Tool == "result" {
								t.Fatal("result before timer")
							}
						}
						if want == passes.QueueWait && len(f.requests()) != 2 {
							t.Fatal(f.requests())
						}
						tm.ch <- time.Time{}
					case <-time.After(3 * time.Second):
						t.Fatal("missing timer")
					}
				}
				if runText(h.waitPass(caller, runText(p, "id")), "status") != store.StatusDone {
					t.Fatal(p)
				}
			}
			h.stop()
			calls := f.requests()
			count := 0
			for _, e := range h.telemetry.Events() {
				if e.Name == "sibling.called" && e.Attrs["target"] == "prompts" {
					count++
					if e.Attrs["method"] != "POST" || e.Attrs["path"] != "/mcp" || e.RequestID != caller.RequestID || e.User != caller.UserID {
						t.Fatal(e)
					}
				}
			}
			if count != len(calls) || mode == "refuse-show" && count != 1 || mode != "refuse-show" && count < 3 {
				t.Fatal(count, calls)
			}
		})
	}
}

// R-DXKT-82UF
func TestRunPromptsKeepsInitialSocketAfterFill(t *testing.T) {
	first, second := &promptFake{}, &promptFake{}
	path := runServices(t, map[string]string{"prompts": runSocket(t, first)})
	h := newRunHarness(t, t.TempDir(), path)
	id := seedPromptCandidate(t, h)
	h.p.After = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		if d == passes.PollInterval {
			ch <- time.Time{}
		}
		return ch
	}
	h.start()
	h.filled()
	caller := identity.Caller{UserID: "snapshot-owner", Email: "snapshot@example.test"}
	enrich := func() {
		t.Helper()
		r, p := h.call(caller, "enrich", tools.EnrichArgs{Kind: enrichers.KindSiteText, Candidate: id, Force: true})
		if r.IsError() || runText(h.waitPass(caller, runText(p, "id")), "status") != store.StatusDone {
			t.Fatal(r, p)
		}
	}
	enrich()
	before := len(first.requests())
	if before == 0 {
		t.Fatal("first enrich made no prompts request")
	}
	raw, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "prompts", "socket": runSocket(t, second), "url": "https://prompts.example.test", "description": "fixture", "enabled": true, "mcp": false}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	enrich()
	h.stop()
	calls := first.requests()
	if len(calls) <= before || len(second.requests()) != 0 {
		t.Fatal(calls, second.requests())
	}
	for _, q := range calls {
		if q.Header.Get("X-User-Id") != caller.UserID || q.Header.Get("X-User-Email") != caller.Email {
			t.Fatal(q)
		}
	}
}

// R-DYSP-LUL4
func TestRunMissingPromptsRefusesOnlyPromptEnrich(t *testing.T) {
	for _, mode := range []string{"unset", "missing", "invalid", "no-entry"} {
		t.Run(mode, func(t *testing.T) {
			path := ""
			if mode == "missing" {
				path = t.TempDir() + "/absent"
			}
			if mode == "invalid" {
				path = t.TempDir() + "/services"
				if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "no-entry" {
				path = runServices(t, map[string]string{"unused": runSocket(t, http.NotFoundHandler())})
			}
			h := newRunHarness(t, t.TempDir(), path)
			if mode == "unset" {
				delete(h.env, services.Variable)
			}
			id := seedPromptCandidate(t, h)
			h.start()
			caller := identity.Caller{UserID: "owner"}
			r, _ := h.call(caller, "enrich", tools.EnrichArgs{Kind: enrichers.KindSiteText, Candidate: id})
			b, _ := json.Marshal(r)
			if !r.IsError() || !strings.Contains(string(b), "unavailable:") {
				t.Fatal(r)
			}
			_, v := h.call(caller, "passes", tools.PassesArgs{})
			if string(v["passes"]) != "[]" {
				t.Fatal(v)
			}
			for _, e := range h.telemetry.Events() {
				if e.Name == "sibling.called" {
					t.Fatal(e)
				}
			}
			r, v = h.call(caller, "enrich", tools.EnrichArgs{Kind: enrichers.KindLinkFacts, Candidate: id})
			if r.IsError() || runText(v, "kind") != store.KindEnrichTarget {
				t.Fatal(r, v)
			}
			h.waitPass(caller, runText(v, "id"))
			h.stop()
		})
	}
}

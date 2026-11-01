package passes_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-2GY0-RKR8 R-2I5X-5CHX R-7L54-ZMJN R-2FQ4-DT0J R-7MD1-DEAC R-7NKX-R611
func TestTargetProgressRecord(t *testing.T) {
	t.Parallel()
	const small uint16 = passes.MaxTargetIDs
	const large int64 = passes.MaxTargetIDs
	if small != 500 || large != 500 {
		t.Fatal(small, large)
	}
	target := passes.Target{"", []string{"cand_a", "cand_a", "bad"}}
	entry := passes.KindEntry{enrichers.KindSiteText, 1, 2, 3, 4, 5}
	p := store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, TargetIDs: target.IDs, RunningKind: enrichers.KindBrief, Busy: 6, Unmatched: 7, KindEntries: []store.KindEntry{{Kind: entry.Kind, NewResults: 1, NotDue: 2, NotApplicable: 3, Failed: 4, Cost: 5}}, UserID: "owner", UserEmail: "owner@example.test"}
	record := passes.RecordOf(p)
	if !reflect.DeepEqual(record.TargetIDs, target.IDs) || record.RunningKind != p.RunningKind || record.Busy != 6 || record.Unmatched != 7 || !reflect.DeepEqual(record.KindEntries, []passes.KindEntry{entry}) {
		t.Fatal(record)
	}
	base := passes.RecordOf(store.Pass{})
	base.Kind = store.KindEnrichTarget
	base.Origin = store.OriginEnrich
	base.User = p.UserID
	base.Email = p.UserEmail
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	// The existing record proof checks every base member and its order. The
	// extension follows the starter and keeps every entry count, zero included.
	prefixText := strings.TrimSuffix(string(prefix), `,"busy":0,"unmatched":0,"kinds":[]}`)
	want := prefixText + `,"target_ids":["cand_a","cand_a","bad"],"running_kind":"brief","busy":6,"unmatched":7,"kinds":[{"kind":"site_text","new_results":1,"not_due":2,"not_applicable":3,"failed":4,"cost":5}]}`
	if string(raw) != want {
		t.Fatal(string(raw), want)
	}
	var decoded passes.Record
	if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(decoded, record) {
		t.Fatal(decoded, record, err)
	}
	for _, kind := range []string{store.KindEnrichTarget, store.KindJudge} {
		for _, named := range []string{"", enrichers.KindSiteText} {
			r := passes.Record{Kind: kind, EnrichKind: named}
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(b, &obj); err != nil {
				t.Fatal(err)
			}
			_, busy := obj["busy"]
			_, unmatched := obj["unmatched"]
			kinds, hasKinds := obj["kinds"]
			if busy != (kind == store.KindEnrichTarget) || unmatched != busy || hasKinds != (busy && named == "") || hasKinds && string(kinds) != "[]" {
				t.Fatal(string(b))
			}
			for _, absent := range []string{"target_ids", "running_kind", "target", "user", "email"} {
				if _, ok := obj[absent]; ok {
					t.Fatal(string(b))
				}
			}
		}
	}
}

// R-7Q0Q-IPIF R-7SGJ-A8ZT R-7TOF-O0QI
func TestTargetShapesAndPreflightOrder(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	id := seedTarget(t, f, 1)[0]
	f.d.SetFailing(true)
	for _, target := range []passes.Target{{}, {ID: id, IDs: []string{id}}, {IDs: make([]string, passes.MaxTargetIDs+1)}} {
		p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts, target, false)
		assertRefusal(t, p, err, passes.RefusalInvalid)
	}
	f.d.SetFailing(false)
	for _, missing := range []string{"website-search", "brief", ""} {
		pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
			var prompt string
			_ = json.Unmarshal(args["name"], &prompt)
			if name == "show" && prompt == missing {
				return "fixture missing", true, 0
			}
			return promptAnswer(name, args)
		})
		f.eng.Drain(context.Background())
		checker := &website.Checker{Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
			t.Error("unexpected fetch")
			return pageResponse(r), nil
		})}, After: fastAfter}
		f.eng = passes.New(passes.Config{Store: f.st, Website: checker, Prompts: pc, Telemetry: f.w, Events: f.em, After: promptAfter})
		p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, "", passes.Target{IDs: []string{"malformed"}}, false)
		if missing != "" {
			assertRefusal(t, p, err, passes.RefusalInvalid)
		} else {
			if err != nil {
				t.Fatal(err)
			}
			p = f.ended(p.ID)
			if p.Status != store.StatusDone || p.Unmatched != 1 || len(p.KindEntries) != 4 {
				t.Fatal(p)
			}
		}
		shows := []string{}
		for _, call := range fake.snapshot() {
			if call.name != "show" {
				t.Fatal(call)
			}
			var prompt string
			_ = json.Unmarshal(call.args["name"], &prompt)
			shows = append(shows, prompt)
		}
		want := []string{"website-search", "site-text", "brief"}
		if missing == "website-search" {
			want = want[:1]
		}
		if !reflect.DeepEqual(shows, want) {
			t.Fatal(shows, want)
		}
	}
}

// R-7Q0Q-IPIF: an unnamed request checks every required client before any prompt.
func TestKindlessTargetReadiness(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"checker", "prompts", "route-http"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, false)
			pc, fake := fakePrompts(t, f, promptAnswer)
			checker := &website.Checker{Client: &http.Client{Transport: enrichTransport(pageResponseTransport)}}
			var route *routes.Client
			switch mode {
			case "checker":
				checker = nil
			case "prompts":
				pc = nil
			case "route-http":
				route = &routes.Client{Key: "fixture"}
			}
			f.eng = passes.New(passes.Config{Store: f.st, Website: checker, Prompts: pc, Routes: route, Telemetry: f.w, Events: f.em})
			p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, "", passes.Target{IDs: []string{"bad"}}, false)
			assertRefusal(t, p, err, passes.RefusalUnavailable)
			if len(fake.snapshot()) != 0 {
				t.Fatal(fake.snapshot())
			}
		})
	}
}
func pageResponseTransport(r *http.Request) (*http.Response, error) { return pageResponse(r), nil }

// R-7OSU-4XRQ R-7W48-FK7W R-7XC4-TBYL R-7YK1-73PA R-827Q-CEXD R-YG1Z-034H
func TestKindlessTargetProgressAndLaterInputs(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	id := searchTargets(t, f, 1)[0]
	polls := make(chan chan time.Time, 4)
	pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
		result, refused, code := promptAnswer(name, args)
		if name == "run" {
			var prompt string
			_ = json.Unmarshal(args["name"], &prompt)
			return map[string]any{"id": prompt, "status": enrichers.RunQueued}, false, 0
		}
		if name == "result" {
			var run string
			_ = json.Unmarshal(args["run"], &run)
			r := result.(map[string]any)
			r["id"] = run
			switch run {
			case "website-search":
				r["stdout"] = `{"url":"https://found.test","why":"found","rejected":[]}`
				r["cost_nanos"] = 100
			case "site-text":
				if _, err := f.st.AddPage(context.Background(), store.Page{Candidate: id, URL: "https://found.test/about", Text: "saved during run", UserID: "owner"}); err != nil {
					t.Error(err)
				}
				r["status"] = "failed"
				r["cost_nanos"] = 500
			case "brief":
				r["stdout"] = `{"summary":"A company.","categories":[],"labels":[]}`
				r["cost_nanos"] = 900
			default:
				t.Error(run)
			}
		}
		return result, refused, code
	})
	checker := &website.Checker{Client: &http.Client{Transport: enrichTransport(pageResponseTransport)}, After: fastAfter}
	f.eng = passes.New(passes.Config{Store: f.st, Website: checker, Prompts: pc, Telemetry: f.w, Events: f.em, After: func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); polls <- ch; return ch }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "request"}
	p, err := f.eng.EnrichTarget(ctx, caller, "", passes.Target{ID: id}, false)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	for i, kind := range []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief} {
		var poll chan time.Time
		select {
		case poll = <-polls:
		case <-time.After(30 * time.Second):
			t.Fatal("missing kind poll", kind)
		}
		current, err := f.st.GetPass(t.Context(), p.ID)
		if err != nil || current.Status != store.StatusRunning || current.RunningKind != kind || len(current.KindEntries) != i+1 {
			t.Fatal(current, err)
		}
		holder, err := f.st.RunningTargetPass(t.Context(), id)
		if err != nil || holder != p.ID {
			t.Fatal(holder, err)
		}
		if current.KindEntries[0] != (store.KindEntry{Kind: sources.WebsiteCheck, NotApplicable: 1}) {
			t.Fatal(current)
		}
		if i > 0 && current.KindEntries[1] != (store.KindEntry{Kind: enrichers.KindWebsiteSearch, NewResults: 1, Cost: 100}) {
			t.Fatal(current)
		}
		if i > 1 && current.KindEntries[2] != (store.KindEntry{Kind: enrichers.KindSiteText, Failed: 1, Cost: 500}) {
			t.Fatal(current)
		}
		poll <- time.Time{}
	}
	p = f.ended(p.ID)
	want := []store.KindEntry{{Kind: sources.WebsiteCheck, NotApplicable: 1}, {Kind: enrichers.KindWebsiteSearch, NewResults: 1, Cost: 100}, {Kind: enrichers.KindSiteText, Failed: 1, Cost: 500}, {Kind: enrichers.KindBrief, NewResults: 1, Cost: 900}}
	if p.Status != store.StatusDone || p.RunningKind != "" || !reflect.DeepEqual(p.KindEntries, want) || p.Due != 3 || p.Runs != 3 || p.RunsFailed != 1 || p.Cost != 1500 || p.Pages != 1 {
		t.Fatal(p)
	}
	holder, err := f.st.RunningTargetPass(t.Context(), id)
	if err != nil || holder != "" {
		t.Fatal(holder, err)
	}
	kinds := []string{}
	for _, call := range fake.snapshot() {
		if call.user != caller.UserID || call.email != caller.Email || call.request != caller.RequestID {
			t.Fatal(call)
		}
		if call.name == "run" {
			var name string
			_ = json.Unmarshal(call.args["name"], &name)
			kinds = append(kinds, name)
			if name == "site-text" && !strings.Contains(string(call.args["input"]), "https://found.test") {
				t.Fatal("search's website not selected", call)
			}
			if name == "brief" && !strings.Contains(string(call.args["input"]), "saved during run") {
				t.Fatal("site text's page not selected", call)
			}
		}
	}
	if !reflect.DeepEqual(kinds, []string{"website-search", "site-text", "brief"}) {
		t.Fatal(kinds)
	}
	es, err := f.st.Enrichments(t.Context(), id)
	if err != nil || len(es) != 3 {
		t.Fatal(es, err)
	}
}

// R-7ZRX-KVFZ R-7XC4-TBYL: a failed kind adds no entry and releases all claims.
func TestKindlessTargetFailureKeepsCompletedEntries(t *testing.T) {
	t.Parallel()
	for _, drained := range []bool{false, true} {
		t.Run(map[bool]string{false: "prompts", true: "drain"}[drained], func(t *testing.T) {
			f := newFixture(t, nil, false)
			id := seedTarget(t, f, 1)[0]
			polls := make(chan chan time.Time, 1)
			pc, _ := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
				if name == "result" {
					return nil, false, 500
				}
				return promptAnswer(name, args)
			})
			f.eng = passes.New(passes.Config{Store: f.st, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(pageResponseTransport)}}, Prompts: pc, Telemetry: f.w, Events: f.em, After: func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); polls <- ch; return ch }})
			p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, "", passes.Target{ID: id}, false)
			if err != nil {
				t.Fatal(err)
			}
			var poll chan time.Time
			select {
			case poll = <-polls:
			case <-time.After(30 * time.Second):
				t.Fatal("missing poll")
			}
			if drained {
				f.eng.Drain(context.Background())
			} else {
				poll <- time.Time{}
			}
			p = f.ended(p.ID)
			reason := store.ReasonPrompts
			if drained {
				reason = store.ReasonInterrupted
			}
			if p.Status != store.StatusFailed || p.Reason != reason || p.RunningKind != "" || failedCounts(p) != (store.Counts{}) || !reflect.DeepEqual(p.KindEntries, []store.KindEntry{{Kind: sources.WebsiteCheck, NotDue: 1}, {Kind: enrichers.KindWebsiteSearch, NotApplicable: 1}}) {
				t.Fatal(p)
			}
			holder, err := f.st.RunningTargetPass(t.Context(), id)
			if err != nil || holder != "" {
				t.Fatal(holder, err)
			}
		})
	}
}

// R-7UWC-1SH7 R-80ZT-YN6O R-8I2F-BFKE R-827Q-CEXD R-DUT3-ZLYY
func TestTargetSetClaimsDeduplicateAndAdmitDifferentCandidate(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ids := seedTarget(t, f, 2)
	polls := make(chan chan time.Time, 4)
	pc, fake := fakePrompts(t, f, promptAnswer)
	f.eng = passes.New(passes.Config{Store: f.st, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(pageResponseTransport)}, After: fastAfter}, Prompts: pc, Telemetry: f.w, Events: f.em, After: func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); polls <- ch; return ch }})
	first, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindSiteText, passes.Target{ID: ids[0]}, false)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-polls:
	case <-time.After(30 * time.Second):
		t.Fatal("missing first poll")
	}
	busyAll, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, "", passes.Target{ID: ids[0]}, true)
	if err != nil || busyAll.Busy != 1 {
		t.Fatal(busyAll, err)
	}
	busyAll = f.ended(busyAll.ID)
	if busyAll.Status != store.StatusDone || busyAll.Due != 0 || len(busyAll.KindEntries) != 4 {
		t.Fatal(busyAll)
	}
	for _, k := range busyAll.KindEntries {
		if k.NewResults+k.NotDue+k.NotApplicable+k.Failed+k.Cost != 0 {
			t.Fatal(k)
		}
	}
	supplied := []string{ids[0], ids[1], ids[1], "bad", "bad"}
	second, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindSiteText, passes.Target{IDs: supplied}, false)
	if err != nil || second.Busy != 1 || second.Unmatched != 1 || !reflect.DeepEqual(second.TargetIDs, supplied) || second.Target != "" {
		t.Fatal(second, err)
	}
	select {
	case ch := <-polls:
		ch <- time.Time{}
	case <-time.After(30 * time.Second):
		t.Fatal("missing second poll")
	}
	second = f.ended(second.ID)
	if second.Status != store.StatusDone || second.Due != 1 || second.Runs != 1 || second.Cost != 1000 || second.RunningKind != "" || len(second.KindEntries) != 0 {
		t.Fatal(second)
	}
	holder, err := f.st.RunningTargetPass(t.Context(), ids[0])
	if err != nil || holder != first.ID {
		t.Fatal(holder, err)
	}
	es, err := f.st.Enrichments(t.Context(), ids[0])
	if err != nil || len(es) != 1 {
		t.Fatal(es, err)
	}
	starts := 0
	for _, call := range fake.snapshot() {
		if call.name == "run" {
			starts++
		}
	}
	if starts != 2 {
		t.Fatal(starts)
	}
	f.eng.Drain(context.Background())
	targetEngine(f, pc, promptAfter)
	third, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindSiteText, passes.Target{ID: ids[0]}, false)
	if err != nil || third.Busy != 0 {
		t.Fatal(third, err)
	}
	third = f.ended(third.ID)
	if third.Status != store.StatusDone || third.Due != 1 {
		t.Fatal(third)
	}
}

// R-7TOF-O0QI: a failed target write leaves its preexisting claim unchanged.
func TestTargetSetStoreFailureHasNoClaims(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	id := seedTarget(t, f, 1)[0]
	f.d.SetFailing(true)
	p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts, passes.Target{IDs: []string{id}}, false)
	if !errors.Is(err, store.ErrUnreachable) || !reflect.DeepEqual(p, store.Pass{}) {
		t.Fatal(p, err)
	}
	f.d.SetFailing(false)
	holder, err := f.st.RunningTargetPass(t.Context(), id)
	if err != nil || holder != "" || len(f.list()) != 0 {
		t.Fatal(holder, err)
	}
	f.flush()
	if len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
		t.Fatal("failed call events")
	}
}

// R-7YK1-73PA R-85VF-HQ5G: one kindless pass performs the same writes as its named kinds in order.
func TestKindlessTargetMatchesSequentialNamedKinds(t *testing.T) {
	t.Parallel()
	var want [][]store.Enrichment
	for _, together := range []bool{false, true} {
		f := newFixture(t, nil, false)
		ids := searchTargets(t, f, 3)
		for _, id := range ids {
			if _, err := f.st.AddPage(t.Context(), store.Page{Candidate: id, URL: "https://found.test/about", Text: "page content", UserID: "owner"}); err != nil {
				t.Fatal(err)
			}
		}
		pc, _ := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
			result, refused, status := promptAnswer(name, args)
			if name == "run" {
				var prompt string
				var input struct {
					Candidate string `json:"candidate"`
				}
				_ = json.Unmarshal(args["name"], &prompt)
				_ = json.Unmarshal(args["input"], &input)
				return map[string]any{"id": prompt + "|" + input.Candidate, "status": enrichers.RunQueued}, false, 0
			}
			if name == "result" {
				var run string
				_ = json.Unmarshal(args["run"], &run)
				prompt, _, _ := strings.Cut(run, "|")
				r := result.(map[string]any)
				r["id"] = run
				switch prompt {
				case "website-search":
					r["stdout"] = `{"url":"https://found.test","why":"found","rejected":[]}`
				case "brief":
					r["stdout"] = `{"summary":"Company summary.","categories":[],"labels":[]}`
				}
			}
			return result, refused, status
		})
		f.eng = passes.New(passes.Config{Store: f.st, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(pageResponseTransport)}, After: fastAfter}, Prompts: pc, Telemetry: f.w, Events: f.em, After: promptAfter})
		kinds := []string{sources.WebsiteCheck, enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief}
		if together {
			kinds = []string{""}
		}
		for _, kind := range kinds {
			p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, kind, passes.Target{IDs: ids}, true)
			if err != nil {
				t.Fatal(err)
			}
			p = f.ended(p.ID)
			if p.Status != store.StatusDone {
				t.Fatal(p)
			}
			if together && (p.Derived != 0 || p.Due != 9 || p.Runs != 9 || p.Pages != 0 || p.Cost != 9000 || p.SearchDifferent != 3) {
				t.Fatal(p)
			}
		}
		got := [][]store.Enrichment{}
		for _, id := range ids {
			es, err := f.st.Enrichments(t.Context(), id)
			if err != nil || len(es) != 3 {
				t.Fatal(es, err)
			}
			got = append(got, es)
		}
		if together && !reflect.DeepEqual(got, want) {
			t.Fatal("kindless result differs from named sequence")
		}
		want = got
	}
}

// R-YM0S-0PT5 R-7YK1-73PA: queued subjects keep the input present when the kind began.
func TestTargetQueuedSubjectsUseKindBeginningSnapshot(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ids := seedTarget(t, f, passes.RunsInFlight+1)
	expected := map[string]json.RawMessage{}
	for _, id := range ids {
		if _, err := f.st.AddPage(t.Context(), store.Page{Candidate: id, URL: "https://pages.test/about", Text: "initial page", UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
		s, err := f.st.Subject(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		expected[id] = enrichers.Brief{}.Input(s)
	}
	started := make(chan string, len(ids))
	polls := make(chan chan time.Time, len(ids))
	pc, _ := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
		if name == "run" {
			var input struct {
				Candidate string `json:"candidate"`
			}
			_ = json.Unmarshal(args["input"], &input)
			var got, want any
			_ = json.Unmarshal(args["input"], &got)
			_ = json.Unmarshal(expected[input.Candidate], &want)
			if !reflect.DeepEqual(got, want) {
				t.Error("queued input changed", got, want)
			}
			started <- input.Candidate
			return map[string]any{"id": input.Candidate, "status": enrichers.RunQueued}, false, 0
		}
		result, refused, status := promptAnswer(name, args)
		if name == "result" {
			result.(map[string]any)["stdout"] = `{"summary":"Company.","categories":[],"labels":[]}`
		}
		return result, refused, status
	})
	targetEngine(f, pc, func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); polls <- ch; return ch })
	p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindBrief, passes.Target{IDs: ids}, false)
	if err != nil {
		t.Fatal(err)
	}
	for range passes.RunsInFlight {
		select {
		case <-started:
		case <-time.After(30 * time.Second):
			t.Fatal("missing initial run")
		}
	}
	if _, err := f.st.AddPage(t.Context(), store.Page{Candidate: ids[len(ids)-1], URL: "https://pages.test/about", Text: "changed while queued", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	select {
	case ch := <-polls:
		ch <- time.Time{}
	case <-time.After(30 * time.Second):
		t.Fatal("missing poll")
	}
	select {
	case id := <-started:
		if id != ids[len(ids)-1] {
			t.Fatal(id)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("queued run not admitted")
	}
	f.eng.Drain(context.Background())
	p = f.ended(p.ID)
	if p.Reason != store.ReasonInterrupted {
		t.Fatal(p)
	}
}

// R-7ZRX-KVFZ R-2PU0-C7JL: recovery preserves completed entries and clears an unfinished kind and claims.
func TestRecoverKindlessTargetProgress(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	id := seedTarget(t, f, 1)[0]
	p, _, err := f.st.AddTargetPass(t.Context(), store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Target: id, UserID: "owner"}, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	entry := store.KindEntry{Kind: sources.WebsiteCheck, NotDue: 1}
	if _, err := f.st.StartPassKind(t.Context(), p.ID, entry.Kind); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.EndPassKind(t.Context(), p.ID, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.StartPassKind(t.Context(), p.ID, enrichers.KindWebsiteSearch); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	p, err = f.st.GetPass(t.Context(), p.ID)
	if err != nil || p.Status != store.StatusFailed || p.Reason != store.ReasonInterrupted || p.RunningKind != "" || !reflect.DeepEqual(p.KindEntries, []store.KindEntry{entry}) || failedCounts(p) != (store.Counts{}) {
		t.Fatal(p, err)
	}
	holder, err := f.st.RunningTargetPass(t.Context(), id)
	if err != nil || holder != "" {
		t.Fatal(holder, err)
	}
}

// R-7OSU-4XRQ R-7XC4-TBYL R-84NJ-3YER R-85VF-HQ5G: an available route runs last and contributes its result and exact request tally.
func TestKindlessTargetIncludesConfiguredRouteLast(t *testing.T) {
	t.Parallel()
	requests := 0
	f, route := routeFixture(t, enrichTransport(func(r *http.Request) (*http.Response, error) { requests++; return routeAnswer(r) }))
	ids, _ := routeCandidates(t, f, 1, false)
	pc, _ := fakePrompts(t, f, promptAnswer)
	f.eng = passes.New(passes.Config{Store: f.st, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(pageResponseTransport)}, After: fastAfter}, Prompts: pc, Routes: route, Telemetry: f.w, Events: f.em, After: promptAfter})
	p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, "", passes.Target{ID: ids[0]}, false)
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || len(p.KindEntries) != 5 || p.RunningKind != "" {
		t.Fatal(p)
	}
	entry := p.KindEntries[4]
	if entry.Kind != enrichers.KindDriveRoute || entry.NewResults != 1 || entry.Failed != 0 || entry.Cost == 0 || requests == 0 || p.Runs != int64(requests)+1 {
		t.Fatal(entry, p, requests)
	}
	es, err := f.st.Enrichments(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	routeResults := 0
	for _, e := range es {
		if e.Kind == enrichers.KindDriveRoute {
			routeResults++
		}
	}
	if routeResults != 1 {
		t.Fatal(es)
	}
}

// R-827Q-CEXD: a held corpus fetch owns no claim and permits a named targeted kind on its candidate.
func TestCorpusEnrichDoesNotClaimCandidates(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ids := seedEmails(t, f, 1, func(int) string { return "owner@company.test" })
	entered, release := make(chan struct{}), make(chan struct{})
	f.checker(enrichTransport(func(r *http.Request) (*http.Response, error) {
		close(entered)
		select {
		case <-release:
			return pageResponse(r), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}))
	p, err := f.eng.Enrich(t.Context(), identity.Caller{UserID: "owner"}, sources.WebsiteCheck)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered)
	holder, err := f.st.RunningTargetPass(t.Context(), ids[0])
	if err != nil || holder != "" {
		t.Fatal(holder, err)
	}
	named, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts, passes.Target{ID: ids[0]}, false)
	if err != nil || named.Busy != 0 {
		t.Fatal(named, err)
	}
	named = f.ended(named.ID)
	if named.Status != store.StatusDone || named.Derived != 1 || named.RunningKind != "" || len(named.KindEntries) != 0 {
		t.Fatal(named)
	}
	close(release)
	p = f.ended(p.ID)
	if p.Status != store.StatusDone {
		t.Fatal(p)
	}
}

// R-7Q0Q-IPIF R-7UWC-1SH7: a merged single target is invalid while an id set counts it unmatched.
func TestTargetMergedIDUsesRequestedShape(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ids := seedTarget(t, f, 2)
	if _, _, err := f.st.Decide(t.Context(), store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "merge evidence", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	candidate, err := f.st.GetCandidate(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status == store.StatusActive {
		ids[0], ids[1] = ids[1], ids[0]
	}
	p, err := f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts, passes.Target{ID: ids[0]}, false)
	assertRefusal(t, p, err, passes.RefusalInvalid)
	supplied := []string{ids[1], ids[1], ids[1], ids[0], "cand_0000000000000000"}
	p, err = f.eng.EnrichTarget(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindLinkFacts, passes.Target{IDs: supplied}, true)
	if err != nil || p.Busy != 0 || p.Unmatched != 2 || !reflect.DeepEqual(p.TargetIDs, supplied) {
		t.Fatal(p, err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Derived != 1 {
		t.Fatal(p)
	}
}

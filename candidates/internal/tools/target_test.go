package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-C0AX-F7MW
func TestTargetedDerivedDispatch(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	co := f.create("targeted")
	for i, a := range []tools.EnrichArgs{{Kind: enrichers.KindLinkFacts, Candidate: ids[0]}, {Kind: enrichers.KindLinkFacts, Candidate: ids[0]}, {Kind: enrichers.KindLinkFacts, Candidate: ids[0], Force: true}, {Kind: enrichers.KindLinkFacts, ProspectList: co.ID}} {
		r := f.callAs(other, "enrich", a)
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		p := decodeList[passes.Record](t, r)
		if raw := decodeList[map[string]json.RawMessage](t, r); raw["target_ids"] != nil {
			t.Fatal(raw)
		}
		target := a.ProspectList
		if target == "" {
			target = a.Candidate
		}
		if p.Kind != store.KindEnrichTarget || p.Origin != store.OriginEnrich || p.Trigger != store.TriggerEnrich || p.EnrichKind != a.Kind || p.Target != target || p.Status != store.StatusRunning || p.User != other.UserID {
			t.Fatal(p)
		}
		done := f.waitEnrich(p.ID)
		want := int64(0)
		if a.Force || i == 0 || i == 3 {
			want = 1
		}
		if done.Status != store.StatusDone || done.Derived != want {
			t.Fatal(done, want)
		}
	}
	for _, a := range []tools.EnrichArgs{{Kind: enrichers.KindLinkFacts, ProspectList: "pl_0000000000000000"}, {Kind: enrichers.KindLinkFacts, Candidate: "cand_0000000000000000"}} {
		refusal(t, f.call("enrich", a), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	if r := f.call("delete_prospect_list", tools.ProspectListArgs{ProspectList: co.ID}); r.IsError() {
		t.Fatal(decode(t, r))
	}
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindLinkFacts, ProspectList: co.ID}), passes.RefusalNotFound, tools.RefuseNotFound)
	if r := f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same fixture"}); r.IsError() {
		t.Fatal(decode(t, r))
	}
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindLinkFacts, Candidate: ids[1]}), passes.RefusalInvalid, tools.RefuseInvalid)
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindSiteText, Candidate: ids[0]}), passes.RefusalUnavailable, tools.RefuseUnavailable)
}

type promptArgs struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
type promptRunArgs struct {
	Run string `json:"run"`
}

func promptClient(t *testing.T, reject bool, calls *atomic.Int64, kinds ...string) *prompts.Client {
	t.Helper()
	w := telemetry.New(telemetry.Config{Service: "fixture", Sink: &telemetry.Capture{}, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) {}, Rand: &random{}})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "fixture", Telemetry: w})
	result := func(v any) mcp.Result {
		var r mcp.Result
		b, _ := json.Marshal(map[string]any{"structuredContent": v, "content": []any{map[string]any{"type": "text", "text": "fixture"}}})
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	mcp.AddRawTool(srv, mcp.RawTool[promptArgs]{Name: "show", Description: "Read the chosen prompt.", Effect: mcp.Read, Handler: func(_ context.Context, c identity.Caller, a promptArgs) (mcp.Result, error) {
		calls.Add(1)
		if c.UserID != other.UserID {
			t.Errorf("prompt caller %v", c)
		}
		allowed := []string{enrichers.KindSiteText}
		if len(kinds) > 0 {
			allowed = kinds
		}
		names := make([]string, len(allowed))
		for i, kind := range allowed {
			names[i] = enrichers.PromptName(kind)
		}
		if !slices.Contains(names, a.Name) {
			t.Errorf("prompt name %s", a.Name)
		}
		if reject && (len(kinds) < 2 || a.Name == names[len(names)-1]) {
			return mcp.ErrorResult("chosen refusal"), nil
		}
		return result(map[string]any{"id": "prompt-one", "name": a.Name, "model": "fixture", "prompt": "fixture", "system": "", "tools": []string{}}), nil
	}})
	mcp.AddRawTool(srv, mcp.RawTool[promptArgs]{Name: "run", Description: "Start the chosen prompt.", Effect: mcp.Additive, Handler: func(_ context.Context, _ identity.Caller, _ promptArgs) (mcp.Result, error) {
		calls.Add(1)
		return result(map[string]any{"id": "run-one", "status": "running"}), nil
	}})
	mcp.AddRawTool(srv, mcp.RawTool[promptRunArgs]{Name: "result", Description: "Read the chosen run.", Effect: mcp.Read, Handler: func(_ context.Context, _ identity.Caller, a promptRunArgs) (mcp.Result, error) {
		calls.Add(1)
		return result(map[string]any{"id": a.Run, "prompt": "prompt-one", "model": "fixture", "status": "running", "started": "2026-01-01T00:00:00Z"}), nil
	}})
	dir, err := os.MkdirTemp("", "tools-prompts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	host := &http.Server{Handler: identity.Require(srv), ReadHeaderTimeout: time.Second}
	go func() { _ = host.Serve(ln) }()
	t.Cleanup(func() { _ = host.Close() })
	return prompts.NewClient(prompts.ClientConfig{Service: services.Entry{Name: "prompts", Socket: socket}, Telemetry: w})
}

// R-C0AX-F7MW R-OL8I-4DRP
func TestTargetedPromptDispatchAndRefusalText(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "held", true: "refused"}[reject], func(t *testing.T) {
			var calls atomic.Int64
			pc := promptClient(t, reject, &calls)
			f := fixtureWithPrompts(t, nil, nil, pc)
			id := f.seed(1)[0]
			setSite(t, f, id, "https://acme.test/")
			a := f.create("first")
			br := f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "second", Filters: &tools.FilterArgs{Categories: []string{}, Class: sources.ClassPrivate}})
			if br.IsError() {
				t.Fatal(decode(t, br))
			}
			b := decodeList[tools.ProspectList](t, br)
			args := tools.EnrichArgs{Kind: enrichers.KindSiteText, ProspectList: a.ID}
			r := f.callAs(other, "enrich", args)
			if reject {
				refusal(t, r, passes.RefusalInvalid, passes.NoPrompt+" "+enrichers.PromptName(enrichers.KindSiteText)+": chosen refusal")
				ps, err := f.st.ListPasses(t.Context(), "", "", 100)
				if err != nil || len(ps) != 0 {
					t.Fatal(ps, err)
				}
				return
			}
			if r.IsError() {
				t.Fatal(decode(t, r))
			}
			p := decodeList[passes.Record](t, r)
			if p.Target != a.ID || p.EnrichKind != enrichers.KindSiteText || p.User != other.UserID || p.Kind != store.KindEnrichTarget || p.Status != store.StatusRunning {
				t.Fatal(p)
			}
			second := f.callAs(other, "enrich", args)
			if second.IsError() {
				t.Fatal(decode(t, second))
			}
			if p := decodeList[passes.Record](t, second); p.Status != store.StatusRunning || p.Busy != 1 {
				t.Fatal(p)
			}
			for i, args := range []tools.EnrichArgs{{Kind: enrichers.KindSiteText, ProspectList: b.ID}, {Kind: enrichers.KindSiteText, Candidate: id}} {
				r := f.callAs(other, "enrich", args)
				if r.IsError() {
					t.Fatal(decode(t, r))
				}
				p := decodeList[passes.Record](t, r)
				if p.Status != store.StatusRunning || p.Busy != int64(i) {
					t.Fatal(p)
				}
			}
			if calls.Load() == 0 {
				t.Fatal("no prompt calls")
			}
		})
	}
}

// R-BXV4-NO5I
func TestMalformedTargetsBeforeStoreAndDrain(t *testing.T) {
	f := newFixture(t)
	f.eng.Drain(context.Background())
	f.d.SetFailing(true)
	for _, args := range []tools.EnrichArgs{{Kind: enrichers.KindLinkFacts, ProspectList: "bogus"}, {Kind: enrichers.KindLinkFacts, Candidate: "bogus"}} {
		refusal(t, f.call("enrich", args), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	for _, args := range []tools.EnrichArgs{{Kind: strings.ToUpper(enrichers.KindSiteText)}, {Kind: enrichers.KindSiteText}, {Force: true}, {Candidates: []string{}}, {Candidates: make([]string, passes.MaxTargetIDs+1)}, {Kind: enrichers.KindLinkFacts, ProspectList: "pl_0000000000000000", Candidate: "cand_0000000000000000"}} {
		refusal(t, f.call("enrich", args), passes.RefusalInvalid, tools.RefuseInvalid)
	}
	f.d.SetFailing(false)
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
}

// R-C6EF-C2CD
func TestDriveRouteCorpusRefusalNeverReadsOrRequests(t *testing.T) {
	var requests atomic.Int64
	rc := &routes.Client{Key: "fixture-key", HTTP: &http.Client{Transport: websiteTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected request")
	})}}
	for _, client := range []*routes.Client{nil, rc} {
		f := fixtureWithRoutes(t, nil, nil, nil, client, "Fixture origin")
		for _, failing := range []bool{false, true} {
			f.d.SetFailing(failing)
			for _, force := range []bool{false, true} {
				refusal(t, f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindDriveRoute, Force: force}), passes.RefusalInvalid, tools.RefuseInvalid)
			}
		}
		f.d.SetFailing(false)
		ps, err := f.st.ListPasses(t.Context(), "", "", 100)
		if err != nil || len(ps) != 0 {
			t.Fatal(ps, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal(requests.Load())
	}
}

// R-C7MB-PU32
func TestDriveRouteTargetRefusalsAndRunningRecords(t *testing.T) {
	f := fixtureWithRoutes(t, nil, nil, nil, nil, "Fixture origin")
	ids := f.seed(2)
	list := f.create("Fixture routes")
	for _, a := range []tools.EnrichArgs{{Kind: enrichers.KindDriveRoute, ProspectList: list.ID}, {Kind: enrichers.KindDriveRoute, Candidate: ids[0]}} {
		refusal(t, f.call("enrich", a), passes.RefusalInvalid, passes.NoRouteKey)
	}
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindDriveRoute, ProspectList: "pl_0000000000000000"}), passes.RefusalNotFound, tools.RefuseNotFound)
	if r := f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "Fixture duplicate"}); r.IsError() {
		t.Fatal(decode(t, r))
	}
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindDriveRoute, Candidate: ids[1]}), passes.RefusalInvalid, tools.RefuseInvalid)
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
	g := fixtureWithRoutes(t, nil, nil, nil, &routes.Client{Key: "fixture-key"}, "Fixture origin")
	gl := g.create("Unready fixture")
	refusal(t, g.call("enrich", tools.EnrichArgs{Kind: enrichers.KindDriveRoute, ProspectList: gl.ID}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	ps, err = g.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
	entered := make(chan struct{}, 2)
	rc := &routes.Client{Key: "fixture-key", HTTP: &http.Client{Transport: websiteTransport(func(r *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	h := fixtureWithRoutes(t, nil, nil, nil, rc, "Fixture origin")
	id := h.seed(1)[0]
	hl := h.create("Running fixture")
	for i, a := range []tools.EnrichArgs{{Kind: enrichers.KindDriveRoute, ProspectList: hl.ID}, {Kind: enrichers.KindDriveRoute, Candidate: id}} {
		r := h.callAs(other, "enrich", a)
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		p := decodeList[passes.Record](t, r)
		target := a.ProspectList
		if target == "" {
			target = a.Candidate
		}
		if p.Kind != store.KindEnrichTarget || p.Target != target || p.EnrichKind != enrichers.KindDriveRoute || p.Status != store.StatusRunning || p.User != other.UserID {
			t.Fatal(p)
		}
		if i == 0 {
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("route did not start")
			}
		}
	}
}

// R-C1IT-SZDL
func TestCandidateIDSetDispatch(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(3)
	if r := f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[1], Other: ids[2], Why: "duplicate fixture"}); r.IsError() {
		t.Fatal(decode(t, r))
	}
	unknown := "cand_0000000000000000"
	given := []string{ids[0], ids[0], unknown, ids[2], "bogus"}
	r := f.callAs(other, "enrich", tools.EnrichArgs{Kind: enrichers.KindLinkFacts, Candidates: given})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	p := decodeList[passes.Record](t, r)
	if p.Kind != store.KindEnrichTarget || p.Origin != store.OriginEnrich || p.Trigger != store.TriggerEnrich || p.Target != "" || !slices.Equal(p.TargetIDs, given) || p.EnrichKind != enrichers.KindLinkFacts || p.Status != store.StatusRunning || p.User != other.UserID || p.Busy != 0 || p.Unmatched != 3 {
		t.Fatal(p)
	}
	if raw := decodeList[map[string]json.RawMessage](t, r); raw["target"] != nil {
		t.Fatal(raw)
	}
	if done := f.waitEnrich(p.ID); done.Status != store.StatusDone || done.Derived != 1 {
		t.Fatal(done)
	}
	many := make([]string, passes.MaxTargetIDs)
	for i := range many {
		many[i] = unknown
	}
	r = f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindLinkFacts, Candidates: many})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	p = decodeList[passes.Record](t, r)
	if p.Status != store.StatusRunning || p.Unmatched != 1 {
		t.Fatal(p)
	}
	f.waitEnrich(p.ID)
	f.eng.Drain(context.Background())
	before, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindLinkFacts, Candidates: given}), passes.RefusalStopping, tools.RefuseStopping)
	after, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(after) != len(before) {
		t.Fatal(after, err)
	}
}

// R-C1IT-SZDL R-CBA0-V5B5
func TestTargetClaimsAppearInCandidateAnswer(t *testing.T) {
	for _, form := range []string{"candidate", "candidates", "prospect_list"} {
		t.Run(form, func(t *testing.T) {
			var calls atomic.Int64
			f := fixtureWithPrompts(t, nil, nil, promptClient(t, false, &calls))
			ids := f.seed(3)
			if r := f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[1], Other: ids[2], Why: "merged fixture"}); r.IsError() {
				t.Fatal(decode(t, r))
			}
			setSite(t, f, ids[0], "https://held.test/")
			read := func(id string) map[string]json.RawMessage {
				t.Helper()
				r := f.call("candidate", tools.CandidateArgs{Candidate: id})
				if r.IsError() {
					t.Fatal(decode(t, r))
				}
				return decodeList[map[string]json.RawMessage](t, r)
			}
			if v := read(ids[0]); v["running_pass"] != nil {
				t.Fatal(v)
			}
			args := tools.EnrichArgs{Kind: enrichers.KindSiteText}
			switch form {
			case "candidate":
				args.Candidate = ids[0]
			case "candidates":
				args.Candidates = []string{ids[0]}
			case "prospect_list":
				args.ProspectList = f.create("held candidates").ID
			}
			r := f.callAs(other, "enrich", args)
			if r.IsError() {
				t.Fatal(decode(t, r))
			}
			p := decodeList[passes.Record](t, r)
			var held string
			if err := json.Unmarshal(read(ids[0])["running_pass"], &held); err != nil || held != p.ID {
				t.Fatal(held, p.ID, err)
			}
			if v := read(ids[2]); v["running_pass"] != nil {
				t.Fatal(v)
			}
			second := f.callAs(other, "enrich", tools.EnrichArgs{Kind: enrichers.KindSiteText, Candidates: []string{ids[0], "cand_0000000000000000"}})
			if second.IsError() {
				t.Fatal(decode(t, second))
			}
			if next := decodeList[passes.Record](t, second); next.Status != store.StatusRunning || next.Busy != 1 || next.Unmatched != 1 {
				t.Fatal(next)
			}
			f.eng.Drain(context.Background())
			if done, err := f.st.GetPass(t.Context(), p.ID); err != nil || done.Status == store.StatusRunning {
				t.Fatal(done, err)
			}
			if v := read(ids[0]); v["running_pass"] != nil {
				t.Fatal(v)
			}
		})
	}
}

// R-C3YM-KIUZ
func TestOmittedTargetKindDispatch(t *testing.T) {
	kinds := []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief}
	for _, form := range []string{"candidate", "candidates", "prospect_list"} {
		t.Run(form, func(t *testing.T) {
			var calls, fetched atomic.Int64
			f := fixtureWithPrompts(t, nil, pageChecker(&fetched), promptClient(t, false, &calls, kinds...))
			id := f.seed(1)[0]
			args := tools.EnrichArgs{}
			switch form {
			case "candidate":
				args.Candidate = id
			case "candidates":
				args.Candidates = []string{id}
			case "prospect_list":
				args.ProspectList = f.create("every kind").ID
			}
			omitted := map[string]any{}
			if args.Candidate != "" {
				omitted["candidate"] = args.Candidate
			}
			if args.ProspectList != "" {
				omitted["prospect_list"] = args.ProspectList
			}
			if args.Candidates != nil {
				omitted["candidates"] = args.Candidates
			}
			r := f.callAs(other, "enrich", omitted)
			if r.IsError() {
				t.Fatal(decode(t, r))
			}
			p := decodeList[passes.Record](t, r)
			if p.Kind != store.KindEnrichTarget || p.Status != store.StatusRunning || p.User != other.UserID || p.EnrichKind != "" || len(p.KindEntries) != 0 {
				t.Fatal(p)
			}
			raw := decodeList[map[string]json.RawMessage](t, r)
			if string(raw["kinds"]) != "[]" || raw["enrich_kind"] != nil {
				t.Fatal(raw)
			}
			if form == "candidates" {
				if !slices.Equal(p.TargetIDs, []string{id}) || raw["target"] != nil {
					t.Fatal(p, raw)
				}
			} else {
				want := args.Candidate
				if want == "" {
					want = args.ProspectList
				}
				if p.Target != want || raw["target_ids"] != nil {
					t.Fatal(p, raw)
				}
			}
		})
	}
	for _, mode := range []string{"no website", "no prompts", "brief refused"} {
		t.Run(mode, func(t *testing.T) {
			var calls, fetched atomic.Int64
			pc := promptClient(t, mode == "brief refused", &calls, kinds...)
			checker := pageChecker(&fetched)
			if mode == "no website" {
				checker = nil
			}
			if mode == "no prompts" {
				pc = nil
			}
			f := fixtureWithPrompts(t, nil, checker, pc)
			id := f.seed(1)[0]
			r := f.callAs(other, "enrich", tools.EnrichArgs{Candidates: []string{id}})
			if mode == "brief refused" {
				refusal(t, r, passes.RefusalInvalid, passes.NoPrompt+" "+enrichers.PromptName(enrichers.KindBrief)+": chosen refusal")
			} else {
				refusal(t, r, passes.RefusalUnavailable, tools.RefuseUnavailable)
			}
			ps, err := f.st.ListPasses(t.Context(), "", "", 100)
			if err != nil || len(ps) != 0 {
				t.Fatal(ps, err)
			}
		})
	}
}

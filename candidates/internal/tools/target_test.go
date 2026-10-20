package tools_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-OZVA-PMO1
func TestTargetedDerivedDispatch(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	co := f.create("targeted")
	for i, a := range []tools.EnrichArgs{{Kind: enrichers.KindLinkFacts, Candidate: ids[0]}, {Kind: enrichers.KindLinkFacts, Candidate: ids[0]}, {Kind: enrichers.KindLinkFacts, Candidate: ids[0], Force: true}, {Kind: enrichers.KindLinkFacts, Cohort: co.ID}} {
		r := f.callAs(other, "enrich", a)
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		p := decodeList[passes.Record](t, r)
		target := a.Cohort
		if target == "" {
			target = a.Candidate
		}
		if p.Kind != store.KindEnrichCohort || p.Origin != store.OriginEnrich || p.Trigger != store.TriggerEnrich || p.EnrichKind != a.Kind || p.Target != target || p.Status != store.StatusRunning || p.User != other.UserID {
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
	for _, a := range []tools.EnrichArgs{{Kind: enrichers.KindLinkFacts, Cohort: "coh_0000000000000000"}, {Kind: enrichers.KindLinkFacts, Candidate: "cand_0000000000000000"}} {
		refusal(t, f.call("enrich", a), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	if r := f.call("delete_cohort", tools.CohortArgs{Cohort: co.ID}); r.IsError() {
		t.Fatal(decode(t, r))
	}
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: enrichers.KindLinkFacts, Cohort: co.ID}), passes.RefusalNotFound, tools.RefuseNotFound)
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
		kind := enrichers.KindSiteText
		if len(kinds) > 0 {
			kind = kinds[0]
		}
		if a.Name != enrichers.PromptName(kind) {
			t.Errorf("prompt name %s", a.Name)
		}
		if reject {
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

// R-OZVA-PMO1 R-OL8I-4DRP
func TestTargetedPromptDispatchAndRefusalText(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "held", true: "refused"}[reject], func(t *testing.T) {
			var calls atomic.Int64
			pc := promptClient(t, reject, &calls)
			f := fixtureWithPrompts(t, nil, nil, pc)
			id := f.seed(1)[0]
			setSite(t, f, id, "https://acme.test/")
			a, b := f.create("first"), f.create("second")
			args := tools.EnrichArgs{Kind: enrichers.KindSiteText, Cohort: a.ID}
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
			if p.Target != a.ID || p.EnrichKind != enrichers.KindSiteText || p.User != other.UserID || p.Kind != store.KindEnrichCohort || p.Status != store.StatusRunning {
				t.Fatal(p)
			}
			refusal(t, f.callAs(other, "enrich", args), passes.RefusalBusy, tools.RefuseBusy)
			for _, args := range []tools.EnrichArgs{{Kind: enrichers.KindSiteText, Cohort: b.ID}, {Kind: enrichers.KindSiteText, Candidate: id}} {
				r := f.callAs(other, "enrich", args)
				if r.IsError() {
					t.Fatal(decode(t, r))
				}
				p := decodeList[passes.Record](t, r)
				if p.Status != store.StatusRunning {
					t.Fatal(p)
				}
			}
			if calls.Load() == 0 {
				t.Fatal("no prompt calls")
			}
		})
	}
}

// R-OW7L-KBFY
func TestMalformedTargetsBeforeStoreAndDrain(t *testing.T) {
	f := newFixture(t)
	f.eng.Drain(context.Background())
	f.d.SetFailing(true)
	for _, args := range []tools.EnrichArgs{{Kind: enrichers.KindLinkFacts, Cohort: "bogus"}, {Kind: enrichers.KindLinkFacts, Candidate: "bogus"}} {
		refusal(t, f.call("enrich", args), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	for _, args := range []tools.EnrichArgs{{Kind: strings.ToUpper(enrichers.KindSiteText)}, {Kind: enrichers.KindSiteText}, {Force: true}, {Candidate: "cand_0000000000000000"}, {Kind: enrichers.KindLinkFacts, Cohort: "coh_0000000000000000", Candidate: "cand_0000000000000000"}} {
		refusal(t, f.call("enrich", args), passes.RefusalInvalid, tools.RefuseInvalid)
	}
	f.d.SetFailing(false)
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
}

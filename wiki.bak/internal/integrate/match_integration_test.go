//go:build integration

// The standing integration tier's match slice — see "Integration testing" in
// docs/wiki-redesign-plan.md. It runs the REAL pinned (prompt, model, effort)
// triple for match against a BLUNT fixture (identical name + corroborating claim →
// same) and asserts the output is STRUCTURALLY valid: a clean binary verdict whose
// same(id) resolves to a real offered candidate. It never asserts quality (that is
// Part II's graded sweep). Build-tag gated (`-tags=integration`) so it is always
// in the tree but never in the unit gate.
//
// With no key/network it emits the visible `INTEGRATION CHECKPOINT SKIPPED — no
// keys` line and skips — never passing as if it ran. The first full document-pass
// live checkpoint (extract + match + merge) is P7a2's; this slice lands match's
// live wiring so that checkpoint has it.
package integrate

import (
	"context"
	"os"
	"testing"
	"time"

	"agentkit/model"
	"agentkit/provider/anthropic"
	"agentkit/provider/openai"

	"wiki/internal/config"
	"wiki/internal/llm"
	"wiki/internal/page"
)

// stubExcerptReader serves a blunt, in-memory candidate excerpt to the live match
// call so the test needs no DB — the assertion is about the model's verdict, not
// the registry read (which has its own offline unit test).
type stubExcerptReader struct{ ex map[string]page.Excerpt }

func (s stubExcerptReader) ReadExcerpt(_ context.Context, id string, _ int) (page.Excerpt, error) {
	return s.ex[id], nil
}

func liveMatchFactory() llm.ClientFactory {
	return func(r model.Resolved) (llm.Client, error) {
		switch r.Provider {
		case model.ProviderOpenAI:
			return openai.New(os.Getenv("OPENAI_API_KEY"), r.BareID)
		default:
			return anthropic.New(os.Getenv("ANTHROPIC_API_KEY"), r.BareID)
		}
	}
}

func TestMatchIntegration(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
		t.Log("INTEGRATION CHECKPOINT SKIPPED — no keys")
		t.Skip("no provider keys present")
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	site := cfg.LLM.Match
	if site.Prompt == "" {
		site.Prompt = config.DefaultMatchPrompt
	}

	// A blunt fixture: identical name + corroborating claim → near-certain `same`.
	reg := stubExcerptReader{ex: map[string]page.Excerpt{
		"01HXCANDIDATEAPPLEINC000001": {
			SubjectID:     "01HXCANDIDATEAPPLEINC000001",
			CanonicalName: "Apple Inc.",
			Aliases:       []string{"apple", "apple inc"},
			Body:          "Apple Inc. is a technology company headquartered in Cupertino, California. Tim Cook is its chief executive officer.",
		},
		"01HXCANDIDATEAPPLEREC000002": {
			SubjectID:     "01HXCANDIDATEAPPLEREC000002",
			CanonicalName: "Apple Records",
			Aliases:       []string{"apple records"},
			Body:          "Apple Records is a record label founded by the Beatles in 1968 in London.",
		},
	}}

	w := llm.New(liveMatchFactory(), nil)
	m := NewMatcher(NewWrapperCaller(w), reg, site, cfg.MatchExcerptChars)

	incoming := Subject{
		Type:    TypeEntity,
		Name:    "Apple Inc.",
		Aliases: []string{"Apple"},
		Claims:  []Claim{{Text: "Apple Inc. is a technology company based in Cupertino.", Cites: []string{"01HXBLUNTINBOX0000000000001"}}},
	}
	cands := []page.Candidate{
		{SubjectID: "01HXCANDIDATEAPPLEINC000001", Type: TypeEntity},
		{SubjectID: "01HXCANDIDATEAPPLEREC000002", Type: TypeEntity},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	v, err := m.Match(ctx, incoming, cands)
	if err != nil {
		t.Fatalf("live match failed (checkpoint RED — investigate): %v", err)
	}
	// Structural: a clean binary verdict. On a same verdict the id must be a real
	// offered candidate (ParseMatch enforces this; re-assert for clarity).
	if v.Same != "" {
		if v.Same != "01HXCANDIDATEAPPLEINC000001" && v.Same != "01HXCANDIDATEAPPLEREC000002" {
			t.Errorf("same id %q is not an offered candidate (checkpoint RED)", v.Same)
		}
	}
	// We do not fail on no_match — quality is Part II's job — but a blunt fixture
	// resolving cleanly is the structural signal we want; log it.
	t.Logf("match verdict: same=%q dup_pairs=%d", v.Same, len(v.DupPairs))
}

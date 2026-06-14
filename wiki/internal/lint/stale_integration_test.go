//go:build integration

// The standing integration tier's lint-stale slice — see "Integration testing" in
// docs/wiki-redesign-plan.md (P9c). It runs the REAL pinned (prompt, model,
// effort) triple for the stale-repair call against a BLUNT fixture note and
// asserts the output is STRUCTURALLY valid: a non-empty rewritten body that passes
// the §6.1 citation-preservation gate, plus a disposition for the note. It never
// asserts quality (Part II's graded sweep). Build-tag gated (`-tags=integration`)
// so it is always in the tree but never in the unit gate.
//
// With no key/network it emits the visible `INTEGRATION CHECKPOINT SKIPPED — no
// keys` line and skips — never passing as if it ran.
package lint

import (
	"context"
	"os"
	"testing"
	"time"

	"wiki/internal/config"
	"wiki/internal/llm"
	"wiki/internal/page"
)

func TestLintStaleIntegration(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
		t.Log("INTEGRATION CHECKPOINT SKIPPED — no keys")
		t.Skip("no provider keys present")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	w := llm.New(liveFactory(), nil)
	// No payload source: the note text carries the observation; cited payloads
	// degrade to a marker (the structural check needs no real inbox).
	j := NewStaleJob(NewWrapperCaller(w), nil, nil, cfg.LLM.LintStale)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Blunt fixture: an obviously-stale page plus one note correcting it.
	subj := page.StaleSubject{
		SubjectID: "01STALE",
		Title:     "Initech",
		Body:      "Initech is an independent software company. [01HXBLUNTINBOX0000000000001]",
		Notes: []page.StaleNote{{
			ID:    "01HNOTE0000000000000000001",
			Note:  "Globex acquired Initech in 2021; it is no longer independent.",
			Cites: "01HXBLUNTINBOX0000000000002",
		}},
	}

	res, err := j.Repair(ctx, subj)
	if err != nil {
		t.Fatalf("live stale repair failed the §6.1 gate or call (checkpoint RED): %v", err)
	}
	if len(res.Body) == 0 {
		t.Fatal("stale repair produced an empty body (checkpoint RED)")
	}
	if len(res.Dispositions) == 0 {
		t.Error("stale repair returned no per-note disposition (checkpoint RED)")
	}
	for _, d := range res.Dispositions {
		if d.Status != "repaired" && d.Status != "dismissed" {
			t.Errorf("disposition status %q not in {repaired,dismissed} (checkpoint RED)", d.Status)
		}
	}
	t.Logf("stale repair body len=%d dispositions=%d superseded=%d", len(res.Body), len(res.Dispositions), len(res.Superseded))
}

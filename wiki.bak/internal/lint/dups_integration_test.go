//go:build integration

// The standing integration tier's lint-dups slice — see "Integration testing" in
// docs/wiki-redesign-plan.md. It runs the REAL pinned (prompt, model, effort)
// triple for the dup JUDGE against BLUNT pairs (obviously-same and
// obviously-different) and asserts the output is STRUCTURALLY valid: a verdict that
// resolves to one of merge | dismiss | cant_tell, and on a merge a non-empty
// canonical name; on a merge it also runs the FOLD and asserts the body passes the
// §6.1 citation gate. It never asserts quality (subtle identity is Part II's graded
// sweep — blunt pairs only). Build-tag gated (`-tags=integration`) so it is always
// in the tree but never in the unit gate.
//
// With no key/network it emits the visible `INTEGRATION CHECKPOINT SKIPPED — no
// keys` line and skips — never passing as if it ran.
package lint

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

func liveFactory() llm.ClientFactory {
	return func(r model.Resolved) (llm.Client, error) {
		switch r.Provider {
		case model.ProviderOpenAI:
			return openai.New(os.Getenv("OPENAI_API_KEY"), r.BareID)
		default:
			return anthropic.New(os.Getenv("ANTHROPIC_API_KEY"), r.BareID)
		}
	}
}

func TestLintDupsIntegration(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
		t.Log("INTEGRATION CHECKPOINT SKIPPED — no keys")
		t.Skip("no provider keys present")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	w := llm.New(liveFactory(), nil)
	j := NewDupsJob(NewWrapperCaller(w), nil, cfg.LLM.LintDupJudge, cfg.LLM.LintFold)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Blunt obviously-same pair: identical name + corroborating facts.
	sameA := page.DupSubject{SubjectID: "01A", Type: "entity", CanonicalName: "Apple Inc.",
		Aliases: []string{"apple", "apple inc"},
		Body:    "Apple Inc. is a technology company headquartered in Cupertino, California. [01HXBLUNTINBOX0000000000001]"}
	sameB := page.DupSubject{SubjectID: "01B", Type: "entity", CanonicalName: "Apple Incorporated",
		Aliases: []string{"apple incorporated"},
		Body:    "Apple Incorporated, the Cupertino technology company, makes the iPhone. [01HXBLUNTINBOX0000000000002]"}

	vr, err := j.Judge(ctx, sameA, sameB)
	if err != nil {
		t.Fatalf("live dup-judge failed (checkpoint RED — investigate): %v", err)
	}
	switch vr.Verdict {
	case VerdictMerge, VerdictDismiss, VerdictCantTell:
	default:
		t.Fatalf("verdict %q is not one of merge|dismiss|cant_tell (checkpoint RED)", vr.Verdict)
	}
	t.Logf("obviously-same verdict: %s canonical=%q", vr.Verdict, vr.CanonicalName)
	if vr.Verdict == VerdictMerge {
		if vr.CanonicalName == "" {
			t.Error("a merge verdict must carry a canonical name (checkpoint RED)")
		}
		// On a merge, the fold must yield a body that passes the §6.1 gate (enforced
		// inside Fold itself — a non-nil error is the structural failure).
		fr, err := j.Fold(ctx, vr.CanonicalName, sameA, sameB)
		if err != nil {
			t.Fatalf("live fold failed the §6.1 gate or call (checkpoint RED): %v", err)
		}
		t.Logf("fold body len=%d superseded=%d", len(fr.Body), len(fr.Superseded))
	}

	// Blunt obviously-different pair: same surface token, different type/subject.
	diffA := page.DupSubject{SubjectID: "01C", Type: "entity", CanonicalName: "Apple Inc.",
		Aliases: []string{"apple"}, Body: "Apple Inc. is a technology company. [01HXBLUNTINBOX0000000000003]"}
	diffB := page.DupSubject{SubjectID: "01D", Type: "concept", CanonicalName: "apple (fruit)",
		Aliases: []string{"apple fruit"}, Body: "An apple is an edible fruit produced by the apple tree. [01HXBLUNTINBOX0000000000004]"}
	dv, err := j.Judge(ctx, diffA, diffB)
	if err != nil {
		t.Fatalf("live dup-judge (different) failed (checkpoint RED): %v", err)
	}
	switch dv.Verdict {
	case VerdictMerge, VerdictDismiss, VerdictCantTell:
	default:
		t.Fatalf("verdict %q not in the ternary set (checkpoint RED)", dv.Verdict)
	}
	t.Logf("obviously-different verdict: %s", dv.Verdict)
}

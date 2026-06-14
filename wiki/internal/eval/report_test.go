package eval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// makeMatchResults builds CaseResults for the match site from (caseID, predictedSame,
// cost, latency) tuples, marshaling the match output shape the MatchScorer reads.
func makeMatchResult(t *testing.T, id, model, effort, predSame string, cost float64, lat int64) CaseResult {
	t.Helper()
	out, err := json.Marshal(matchOutput{Same: predSame})
	if err != nil {
		t.Fatal(err)
	}
	return CaseResult{CaseID: id, Model: model, Effort: effort, Output: out, CostUSD: cost, LatencyMS: lat}
}

func matchGoldBytes(t *testing.T, same string) []byte {
	t.Helper()
	b, err := json.Marshal(matchGold{Same: same})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBuildReport_HeadlineAndDangerSeparate proves the report aggregates per config,
// keeps the dangerous axis SEPARATE from the headline, and computes cost/latency.
func TestBuildReport_HeadlineAndDangerSeparate(t *testing.T) {
	golds := map[string][]byte{
		"m1": matchGoldBytes(t, "A"), // gold: same=A
		"m2": matchGoldBytes(t, ""),  // gold: no_match
	}
	// Config "good": correct on both. Config "danger": false-merges m2 (gold no_match,
	// predicts a match) — must show a false_merge rate, NOT a higher headline.
	results := []CaseResult{
		makeMatchResult(t, "m1", "good", "", "A", 0.001, 100),
		makeMatchResult(t, "m2", "good", "", "", 0.001, 100),
		makeMatchResult(t, "m1", "danger", "", "A", 0.002, 200),
		makeMatchResult(t, "m2", "danger", "", "X", 0.002, 200), // false merge
	}
	scorer, err := ScorerFor("match", StubJudge(), 0)
	if err != nil {
		t.Fatal(err)
	}
	rep := BuildReport(context.Background(), 1, "match", results, golds, scorer, DefaultSaturation())

	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 config rows, got %d", len(rep.Rows))
	}
	// The good config must sort first (higher headline) and carry zero false_merge.
	good := rep.Rows[0]
	if good.Model != "good" {
		t.Fatalf("expected the correct config sorted first, got %q", good.Model)
	}
	if good.Dangerous["false_merge"] != 0 {
		t.Errorf("good config should have 0 false_merge, got %v", good.Dangerous["false_merge"])
	}
	danger := rep.Rows[1]
	if danger.Dangerous["false_merge"] <= 0 {
		t.Errorf("danger config must surface a false_merge rate, got %v", danger.Dangerous["false_merge"])
	}
	// The dangerous axis is SEPARATE: the danger config's headline must not be folded
	// up by the false merge; the good config's headline must be strictly higher.
	if !(good.Headline > danger.Headline) {
		t.Errorf("the safe config must have a strictly higher headline (%.3f) than the false-merging one (%.3f)", good.Headline, danger.Headline)
	}
	// Cost + latency aggregated per config.
	if good.MeanCostUSD != 0.001 || danger.MeanCostUSD != 0.002 {
		t.Errorf("mean cost wrong: good=%v danger=%v", good.MeanCostUSD, danger.MeanCostUSD)
	}
	if good.MeanLatency != 100 || danger.MeanLatency != 200 {
		t.Errorf("mean latency wrong: good=%v danger=%v", good.MeanLatency, danger.MeanLatency)
	}
}

// TestRender_NeverLumpsRank proves the rendered table shows the dangerous column
// beside the headline and emits no single composite rank, and is captioned with the
// generation.
func TestRender_NeverLumpsRank(t *testing.T) {
	golds := map[string][]byte{"m1": matchGoldBytes(t, "A")}
	results := []CaseResult{makeMatchResult(t, "m1", "x", "high", "A", 0.001, 100)}
	scorer, _ := ScorerFor("match", StubJudge(), 0)
	rep := BuildReport(context.Background(), 2, "match", results, golds, scorer, DefaultSaturation())
	out := rep.Render()
	for _, want := range []string{"generation=gen-2", "headline", "false_merge", "NO single composite rank"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n%s", want, out)
		}
	}
}

// TestSaturation_FiresWhenClustered proves the question-5 rule fires only when BOTH
// the ceiling and no-separation hold.
func TestSaturation_FiresWhenClustered(t *testing.T) {
	gold := map[string][]byte{"m1": matchGoldBytes(t, "A"), "m2": matchGoldBytes(t, "B")}
	// Two configs both perfect → ceiling 1.0, zero spread, zero dangerous → saturated.
	clustered := []CaseResult{
		makeMatchResult(t, "m1", "a", "", "A", 0.001, 100),
		makeMatchResult(t, "m2", "a", "", "B", 0.001, 100),
		makeMatchResult(t, "m1", "b", "", "A", 0.001, 100),
		makeMatchResult(t, "m2", "b", "", "B", 0.001, 100),
	}
	scorer, _ := ScorerFor("match", StubJudge(), 0)
	rep := BuildReport(context.Background(), 1, "match", clustered, gold, scorer, DefaultSaturation())
	if !rep.Saturation.Saturated {
		t.Fatalf("two perfect configs should be saturated; verdict=%+v", rep.Saturation)
	}
	if !strings.Contains(rep.Render(), "SATURATED — mint gen-2") {
		t.Errorf("render should print the SATURATED advisory:\n%s", rep.Render())
	}

	// One config perfect, one missing both → wide spread → NOT saturated.
	separated := []CaseResult{
		makeMatchResult(t, "m1", "a", "", "A", 0.001, 100),
		makeMatchResult(t, "m2", "a", "", "B", 0.001, 100),
		makeMatchResult(t, "m1", "b", "", "", 0.001, 100), // false split (gold A)
		makeMatchResult(t, "m2", "b", "", "", 0.001, 100), // false split (gold B)
	}
	rep2 := BuildReport(context.Background(), 1, "match", separated, gold, scorer, DefaultSaturation())
	if rep2.Saturation.Saturated {
		t.Errorf("a separated generation must not be saturated; verdict=%+v", rep2.Saturation)
	}
}

// TestSaturation_BelowCeilingNotSaturated proves a low-but-clustered generation is
// not saturated (the ceiling half must also hold).
func TestSaturation_BelowCeilingNotSaturated(t *testing.T) {
	gold := map[string][]byte{"m1": matchGoldBytes(t, "A"), "m2": matchGoldBytes(t, "B")}
	// Both configs identically mediocre (one right, one wrong) → clustered but below
	// the 0.95 ceiling → NOT saturated.
	low := []CaseResult{
		makeMatchResult(t, "m1", "a", "", "A", 0.001, 100),
		makeMatchResult(t, "m2", "a", "", "", 0.001, 100), // false split
		makeMatchResult(t, "m1", "b", "", "A", 0.001, 100),
		makeMatchResult(t, "m2", "b", "", "", 0.001, 100), // false split
	}
	scorer, _ := ScorerFor("match", StubJudge(), 0)
	rep := BuildReport(context.Background(), 1, "match", low, gold, scorer, DefaultSaturation())
	if rep.Saturation.Saturated {
		t.Errorf("a below-ceiling generation must not be saturated; verdict=%+v", rep.Saturation)
	}
	if rep.Saturation.Ceiling {
		t.Errorf("ceiling rule should be false at headline %.3f", rep.Saturation.BestHeadline)
	}
}

// TestRetrievalSideBySide_LiftAndVerdict proves the lexical-vs-hybrid table computes
// the recall lift and renders the licensing verdict (research §8–10).
func TestRetrievalSideBySide_LiftAndVerdict(t *testing.T) {
	lifted := RetrievalSideBySide{
		Site:    "candidates",
		Lexical: RetrievalMode{Name: "lexical", Recall: 0.6, CostUSD: 0},
		Hybrid:  RetrievalMode{Name: "hybrid", Recall: 0.9, CostUSD: 0.0001},
	}
	if l := lifted.RecallLift(); l < 0.299 || l > 0.301 {
		t.Errorf("recall lift want ~0.3 got %v", l)
	}
	if !strings.Contains(lifted.Render(), "vector lane licensed") {
		t.Errorf("a positive lift should license the lane:\n%s", lifted.Render())
	}
	declined := RetrievalSideBySide{
		Site:    "search",
		Lexical: RetrievalMode{Name: "lexical", Recall: 0.8},
		Hybrid:  RetrievalMode{Name: "hybrid", Recall: 0.8, CostUSD: 0.0001},
	}
	if !strings.Contains(declined.Render(), "DECLINED") {
		t.Errorf("a flat lift should decline the lane:\n%s", declined.Render())
	}
}

// TestBuildRetrievalMode_AggregatesRecall proves the per-mode aggregator reads recall
// off the retrieval headline and the miss count off the dangerous axis.
func TestBuildRetrievalMode_AggregatesRecall(t *testing.T) {
	mkOut := func(t *testing.T, ids ...string) json.RawMessage {
		b, err := json.Marshal(retrievalOutput{Results: ids})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	mkGold := func(t *testing.T, ids ...string) []byte {
		b, err := json.Marshal(retrievalGold{Relevant: ids})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	golds := map[string][]byte{
		"c1": mkGold(t, "x", "y"), // 2 relevant
	}
	results := []CaseResult{
		{CaseID: "c1", Model: "m", Output: mkOut(t, "x", "z"), CostUSD: 0.0002}, // recall 0.5, 1 missed
	}
	scorer, _ := ScorerFor("candidates", StubJudge(), 10)
	m := BuildRetrievalMode(context.Background(), "lexical", results, golds, scorer, "missed_candidate")
	if m.Recall != 0.5 {
		t.Errorf("recall want 0.5 got %v", m.Recall)
	}
	if m.Missed != 1 {
		t.Errorf("missed want 1 got %v", m.Missed)
	}
	if m.CostUSD != 0.0002 {
		t.Errorf("cost want 0.0002 got %v", m.CostUSD)
	}
}

// TestChosenConfig_Renders proves the feedback-loop record renders the picked triple
// and any knobs — the line P16d turns into a config default.
func TestChosenConfig_Renders(t *testing.T) {
	c := ChosenConfig{
		Site: "match", Generation: 1, Model: "claude-haiku-4-5", Effort: "",
		PromptVersion: "prompts/v1.txt",
		Knobs:         map[string]string{"WIKI_MATCH_EXCERPT_CHARS": "600"},
		Rationale:     "best headline, zero false-merge",
	}
	out := c.Render()
	for _, want := range []string{"feedback loop → P16d", "claude-haiku-4-5", "prompts/v1.txt", "WIKI_MATCH_EXCERPT_CHARS=600", "rationale:"} {
		if !strings.Contains(out, want) {
			t.Errorf("chosen render missing %q\n%s", want, out)
		}
	}
}

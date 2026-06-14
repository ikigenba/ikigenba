package eval

import (
	"context"
	"strings"
	"testing"
)

// fakeJudge is a deterministic test judge: it answers each rubric question by a
// keyword rule so the judged scorer paths are exercised offline without a key. The
// panel is fixed at 3 (the eval-design default). It lets the tests assert the
// judged path while the production run swaps the real held-out model.
type fakeJudge struct {
	yes     bool // YesNo verdict
	yesVote int  // panel yes-votes when judging
	similar bool // Similar verdict
}

func (f fakeJudge) YesNo(_ context.Context, _, _, _ string) (bool, int, int) {
	return f.yes, f.yesVote, 3
}
func (f fakeJudge) Similar(_ context.Context, _, _ string) bool { return f.similar }

// ---- Set-alignment (extract) ----

func TestExtractScorerRecallAndOverExtraction(t *testing.T) {
	sc := NewExtractScorer(fakeJudge{similar: true})
	gold := []byte(`{"subjects":[
		{"type":"entity","name":"Apple Inc","claims":[{"text":"makes the iPhone","cites":["a"]}]},
		{"type":"entity","name":"Tim Cook","claims":[{"text":"is the CEO","cites":["a"]}]}
	]}`)

	// Known-good: both gold subjects found, none extra.
	good := []byte(`{"subjects":[
		{"type":"entity","name":"Apple Inc","claims":[{"text":"makes the iPhone","cites":["a"]}]},
		{"type":"entity","name":"Tim Cook","claims":[{"text":"is the CEO","cites":["a"]}]}
	]}`)
	s := sc.Score(context.Background(), good, gold)
	if s.Metrics["subject_recall"] != 1 {
		t.Errorf("good recall = %v, want 1", s.Metrics["subject_recall"])
	}
	if s.Dangerous["over_extract"] != 0 {
		t.Errorf("good over_extract = %v, want 0", s.Dangerous["over_extract"])
	}
	if s.Headline != 1 {
		t.Errorf("good headline (F1) = %v, want 1", s.Headline)
	}

	// Known-bad: an extra hallucinated subject — over-extraction must be its OWN axis,
	// not buried in the F1.
	bad := []byte(`{"subjects":[
		{"type":"entity","name":"Apple Inc","claims":[{"text":"makes the iPhone","cites":["a"]}]},
		{"type":"entity","name":"Tim Cook","claims":[{"text":"is the CEO","cites":["a"]}]},
		{"type":"entity","name":"Banana Corp","claims":[{"text":"does not exist","cites":["a"]}]}
	]}`)
	sb := sc.Score(context.Background(), bad, gold)
	if sb.Dangerous["over_extract"] != 1 {
		t.Errorf("bad over_extract = %v, want 1 (the extra subject)", sb.Dangerous["over_extract"])
	}
	if sb.Metrics["subject_recall"] != 1 {
		t.Errorf("bad recall = %v, want 1 (both gold still found)", sb.Metrics["subject_recall"])
	}
	if sb.Metrics["subject_precision"] >= 1 {
		t.Errorf("bad precision should drop below 1, got %v", sb.Metrics["subject_precision"])
	}
}

func TestExtractScorerUnparseableIsZeroNotPanic(t *testing.T) {
	sc := NewExtractScorer(nil)
	s := sc.Score(context.Background(), []byte(`not json`), []byte(`{"subjects":[]}`))
	if len(s.Errs) == 0 {
		t.Error("unparseable output should record an error")
	}
	if s.Headline != 0 {
		t.Errorf("unparseable headline = %v, want 0", s.Headline)
	}
}

func TestCompileScorerDeltas(t *testing.T) {
	sc := NewCompileScorer(fakeJudge{similar: true})
	gold := []byte(`{"subjects":[
		{"type":"event","name":"Q3 earnings","occurred_at":"2024-10","claims":[{"text":"revenue up 5pct","cites":["e1","e2"]}]}
	]}`)
	out := []byte(`{"subjects":[
		{"type":"event","name":"Q3 earnings","occurred_at":"2024-10","claims":[{"text":"revenue up 5pct","cites":["e1"]}]}
	]}`)
	s := sc.Score(context.Background(), out, gold)
	if _, ok := s.Metrics["compression_ratio"]; !ok {
		t.Error("compile scorer must report compression_ratio")
	}
	if _, ok := s.Metrics["cite_recall"]; !ok {
		t.Error("compile scorer must report cite_recall")
	}
	// cite_recall: gold cites {e1,e2}, pred {e1} → 0.5
	if r := s.Metrics["cite_recall"]; r < 0.49 || r > 0.51 {
		t.Errorf("cite_recall = %v, want ~0.5", r)
	}
	if s.Metrics["occurred_at_accuracy"] != 1 {
		t.Errorf("occurred_at_accuracy = %v, want 1", s.Metrics["occurred_at_accuracy"])
	}
}

// ---- Asymmetric confusion (match) ----

func TestMatchScorerFalseMergeIsSeparate(t *testing.T) {
	sc := NewMatchScorer()
	// Gold: no_match (distinct things).
	goldNoMatch := []byte(`{"same":""}`)

	// Known-bad: model said same → FALSE MERGE, the dangerous axis.
	bad := []byte(`{"same":"01SUBJECTAAA","dup_pairs":[]}`)
	sb := sc.Score(context.Background(), bad, goldNoMatch)
	if sb.Dangerous["false_merge"] != 1 {
		t.Errorf("false_merge = %v, want 1", sb.Dangerous["false_merge"])
	}
	if sb.Headline != 0 {
		t.Errorf("a false-merge headline = %v, want 0", sb.Headline)
	}
	if _, ok := sb.Dangerous["false_split"]; !ok {
		t.Error("false_split must always be a named axis, even when 0")
	}

	// Known-good: correctly no_match.
	good := []byte(`{"same":"","dup_pairs":[]}`)
	sg := sc.Score(context.Background(), good, goldNoMatch)
	if sg.Dangerous["false_merge"] != 0 || sg.Headline != 1 {
		t.Errorf("good no_match: merge=%v headline=%v", sg.Dangerous["false_merge"], sg.Headline)
	}
}

func TestMatchScorerFalseSplitAndDupPairsRecall(t *testing.T) {
	sc := NewMatchScorer()
	gold := []byte(`{"same":"01SUBJECTAAA","dup_pairs":[{"a":"01X","b":"01Y"}]}`)
	// Said no_match when gold is a match → false_split; missed the dup pair → recall 0.
	out := []byte(`{"same":"","dup_pairs":[]}`)
	s := sc.Score(context.Background(), out, gold)
	if s.Dangerous["false_split"] != 1 {
		t.Errorf("false_split = %v, want 1", s.Dangerous["false_split"])
	}
	if s.Dangerous["dup_pairs_recall"] != 0 {
		t.Errorf("dup_pairs_recall = %v, want 0", s.Dangerous["dup_pairs_recall"])
	}
	// Now surface the pair (canonical-order insensitive).
	out2 := []byte(`{"same":"01SUBJECTAAA","dup_pairs":[{"a":"01Y","b":"01X"}]}`)
	s2 := sc.Score(context.Background(), out2, gold)
	if s2.Dangerous["dup_pairs_recall"] != 1 {
		t.Errorf("dup_pairs_recall (order-insensitive) = %v, want 1", s2.Dangerous["dup_pairs_recall"])
	}
}

func TestDupJudgeLazinessAndConfusion(t *testing.T) {
	sc := NewDupJudgeScorer()
	// cant_tell when evidence IS present → laziness.
	lazy := sc.Score(context.Background(),
		[]byte(`{"verdict":"cant_tell"}`),
		[]byte(`{"verdict":"merge","evidence_present":true}`))
	if lazy.Dangerous["lazy_cant_tell"] != 1 {
		t.Errorf("lazy_cant_tell = %v, want 1", lazy.Dangerous["lazy_cant_tell"])
	}
	// false_merge: said merge when gold dismiss.
	fm := sc.Score(context.Background(),
		[]byte(`{"verdict":"merge"}`),
		[]byte(`{"verdict":"dismiss","evidence_present":true}`))
	if fm.Dangerous["false_merge"] != 1 {
		t.Errorf("false_merge = %v, want 1", fm.Dangerous["false_merge"])
	}
	// correct dismiss.
	ok := sc.Score(context.Background(),
		[]byte(`{"verdict":"dismiss"}`),
		[]byte(`{"verdict":"dismiss","evidence_present":true}`))
	if ok.Headline != 1 || ok.Dangerous["false_merge"] != 0 {
		t.Errorf("correct dismiss: headline=%v merge=%v", ok.Headline, ok.Dangerous["false_merge"])
	}
}

func TestCanonicalNameAgreement(t *testing.T) {
	sc := NewCanonicalNameScorer()
	g := sc.Score(context.Background(), []byte(`{"name":"Apple Inc."}`), []byte(`{"name":"Apple Inc"}`))
	if g.Headline != 1 || g.Dangerous["disagreement"] != 0 {
		t.Errorf("agreement: headline=%v disagreement=%v", g.Headline, g.Dangerous["disagreement"])
	}
	b := sc.Score(context.Background(), []byte(`{"name":"Banana"}`), []byte(`{"name":"Apple"}`))
	if b.Dangerous["disagreement"] != 1 {
		t.Errorf("disagreement = %v, want 1", b.Dangerous["disagreement"])
	}
}

// ---- Recall@k + RRF (candidates / search / sweep) ----

func TestCandidatesRecallAtKAndMissAxis(t *testing.T) {
	sc := NewCandidatesScorer(3)
	gold := []byte(`{"relevant":["p1","p2"]}`)
	// Top-3 contains p1 only → recall 0.5, one missed candidate (the dangerous axis).
	out := []byte(`{"results":["x","p1","y","p2"]}`)
	s := sc.Score(context.Background(), out, gold)
	if s.Metrics["recall_at_k"] != 0.5 {
		t.Errorf("recall@3 = %v, want 0.5 (p2 is at rank 4)", s.Metrics["recall_at_k"])
	}
	if s.Dangerous["missed_candidate"] != 1 {
		t.Errorf("missed_candidate = %v, want 1", s.Dangerous["missed_candidate"])
	}
	// Bigger k recovers p2.
	sc2 := NewCandidatesScorer(10)
	s2 := sc2.Score(context.Background(), out, gold)
	if s2.Metrics["recall_at_k"] != 1 || s2.Dangerous["missed_candidate"] != 0 {
		t.Errorf("recall@10: recall=%v missed=%v", s2.Metrics["recall_at_k"], s2.Dangerous["missed_candidate"])
	}
}

func TestSearchReportsNDCG(t *testing.T) {
	sc := NewSearchScorer(5)
	gold := []byte(`{"relevant":["p1"]}`)
	first := sc.Score(context.Background(), []byte(`["p1","x","y"]`), gold)
	later := sc.Score(context.Background(), []byte(`["x","y","p1"]`), gold)
	if first.Metrics["ndcg_at_k"] <= later.Metrics["ndcg_at_k"] {
		t.Errorf("nDCG should reward earlier rank: first=%v later=%v",
			first.Metrics["ndcg_at_k"], later.Metrics["ndcg_at_k"])
	}
	if first.Metrics["ndcg_at_k"] != 1 {
		t.Errorf("rank-1 nDCG = %v, want 1", first.Metrics["ndcg_at_k"])
	}
}

func TestSweepPairDiscoveryRecall(t *testing.T) {
	sc := NewSweepScorer(0) // k<=0 → use full list
	gold := []byte(`{"relevant":["pairA","pairB"]}`)
	out := []byte(`{"results":["pairA"]}`)
	s := sc.Score(context.Background(), out, gold)
	if s.Metrics["recall_at_k"] != 0.5 {
		t.Errorf("sweep recall = %v, want 0.5", s.Metrics["recall_at_k"])
	}
	if s.Dangerous["missed_pair"] != 1 {
		t.Errorf("missed_pair = %v, want 1", s.Dangerous["missed_pair"])
	}
}

// ---- Mechanical + rubric (merge) ----

func TestMergeCitationPreservationGate(t *testing.T) {
	sc := NewMergeScorer(nil) // offline: mechanical-only
	// Known-bad: old body cited [c1] and [c2]; new body keeps [c1], drops [c2] WITHOUT
	// declaring it superseded → undeclared cite loss (the dangerous axis).
	gold := []byte(`{
		"write_set":["s1"],
		"old_bodies":{"s1":"Foo [c1] bar [c2]"},
		"must_survive":["c1"]
	}`)
	bad := []byte(`{"pages":[{"subject":"s1","title":"T","body":"Foo [c1] only","superseded":[]}],"claims":[]}`)
	s := sc.Score(context.Background(), bad, gold)
	if s.Metrics["citation_preservation"] != 0 {
		t.Errorf("citation_preservation = %v, want 0 (undeclared drop)", s.Metrics["citation_preservation"])
	}
	if s.Dangerous["undeclared_cite_loss"] != 1 {
		t.Errorf("undeclared_cite_loss = %v, want 1", s.Dangerous["undeclared_cite_loss"])
	}

	// Known-good: drop [c2] but DECLARE it superseded → gate passes.
	good := []byte(`{"pages":[{"subject":"s1","title":"T","body":"Foo [c1] only","superseded":["c2"]}],"claims":[{"text":"x","cites":["c1"]}]}`)
	sg := sc.Score(context.Background(), good, gold)
	if sg.Metrics["citation_preservation"] != 1 {
		t.Errorf("declared-superseded should pass: %v ; errs=%v", sg.Metrics["citation_preservation"], sg.Errs)
	}
	if sg.Dangerous["undeclared_cite_loss"] != 0 {
		t.Errorf("good undeclared_cite_loss = %v, want 0", sg.Dangerous["undeclared_cite_loss"])
	}
}

func TestMergeWriteSetConformance(t *testing.T) {
	sc := NewMergeScorer(nil)
	gold := []byte(`{"write_set":["s1","s2"],"old_bodies":{}}`)
	// Wrote only s1 (missing s2) and an out-of-set page s3.
	out := []byte(`{"pages":[{"subject":"s1","body":""},{"subject":"s3","body":""}],"claims":[]}`)
	s := sc.Score(context.Background(), out, gold)
	if s.Metrics["write_set_conformance"] != 0 {
		t.Errorf("write_set_conformance = %v, want 0", s.Metrics["write_set_conformance"])
	}
	// Exact set passes.
	out2 := []byte(`{"pages":[{"subject":"s1","body":""},{"subject":"s2","body":""}],"claims":[]}`)
	s2 := sc.Score(context.Background(), out2, gold)
	if s2.Metrics["write_set_conformance"] != 1 {
		t.Errorf("exact write set should conform: %v", s2.Metrics["write_set_conformance"])
	}
}

func TestMergeClaimCitePresence(t *testing.T) {
	sc := NewMergeScorer(nil)
	gold := []byte(`{"write_set":["s1"],"old_bodies":{}}`)
	out := []byte(`{"pages":[{"subject":"s1","body":""}],"claims":[{"text":"a","cites":["c1"]},{"text":"b","cites":[]}]}`)
	s := sc.Score(context.Background(), out, gold)
	if s.Metrics["claim_cite_presence"] != 0 {
		t.Errorf("claim_cite_presence = %v, want 0 (one uncited claim)", s.Metrics["claim_cite_presence"])
	}
}

func TestMergeRubricPanelUsedWhenJudgePresent(t *testing.T) {
	// Judge votes the page hallucinates (yes on the hallucination criterion).
	sc := NewMergeScorer(fakeJudge{yes: true, yesVote: 3})
	gold := []byte(`{"write_set":["s1"],"old_bodies":{},"source_text":"src","lead_name":"X"}`)
	out := []byte(`{"pages":[{"subject":"s1","title":"T","body":"prose [c1]"}],"claims":[{"text":"a","cites":["c1"]}]}`)
	s := sc.Score(context.Background(), out, gold)
	if _, ok := s.Metrics["rubric_mean"]; !ok {
		t.Error("rubric_mean must be present when a judge is configured")
	}
	if s.Dangerous["hallucination"] != 1 {
		t.Errorf("hallucination = %v, want 1 (judge said yes)", s.Dangerous["hallucination"])
	}
}

// ---- Mechanical + rubric (ask) ----

func TestAskFabricationOnGapSetIsHeadline(t *testing.T) {
	sc := NewAskScorer(nil)
	// Gap-set case: the right behaviour is to abstain. Answering = FABRICATION.
	gapGold := []byte(`{"should_abstain":true}`)

	fabricated := []byte(`{"answer":"made it up","abstained":false}`)
	s := sc.Score(context.Background(), fabricated, gapGold)
	if s.Dangerous["fabrication"] != 1 {
		t.Errorf("fabrication = %v, want 1", s.Dangerous["fabrication"])
	}
	if s.Headline != 0 {
		t.Errorf("fabricated headline = %v, want 0", s.Headline)
	}

	abstained := []byte(`{"answer":"","abstained":true}`)
	sg := sc.Score(context.Background(), abstained, gapGold)
	if sg.Headline != 1 || sg.Dangerous["fabrication"] != 0 {
		t.Errorf("correct abstain: headline=%v fabrication=%v", sg.Headline, sg.Dangerous["fabrication"])
	}
}

func TestAskCitationFaithfulnessMechanical(t *testing.T) {
	sc := NewAskScorer(nil)
	gold := []byte(`{
		"answer":"the answer",
		"supporting":["p1"],
		"page_bodies":{"p1":"the supporting body text"},
		"spans":{"p1":"supporting body"}
	}`)
	// Cites a page that does not exist → unfaithful.
	bad := []byte(`{"answer":"x","citations":["p9"],"abstained":false,"retrieved":["p1"]}`)
	s := sc.Score(context.Background(), bad, gold)
	if s.Metrics["citation_faithfulness"] != 0 {
		t.Errorf("citation_faithfulness = %v, want 0 (cited nonexistent page)", s.Metrics["citation_faithfulness"])
	}
	if s.Dangerous["citation_unfaithful"] != 1 {
		t.Errorf("citation_unfaithful = %v, want 1", s.Dangerous["citation_unfaithful"])
	}
	// Cites the real page whose body contains the span → faithful.
	good := []byte(`{"answer":"x","citations":["p1"],"abstained":false,"retrieved":["p1"]}`)
	sg := sc.Score(context.Background(), good, gold)
	if sg.Metrics["citation_faithfulness"] != 1 {
		t.Errorf("faithful citation = %v, want 1 ; errs=%v", sg.Metrics["citation_faithfulness"], sg.Errs)
	}
}

func TestAskRetrievalDecomposedFromSynthesis(t *testing.T) {
	// Judge says the answer is wrong; the supporting page was NOT retrieved →
	// retrieval_failure, not synthesis_failure (eval design kind 4).
	sc := NewAskScorer(fakeJudge{yes: false, yesVote: 0})
	gold := []byte(`{
		"answer":"correct","supporting":["p1"],
		"page_bodies":{"p1":"body"},"spans":{}
	}`)
	// retrieved does NOT include p1.
	out := []byte(`{"answer":"wrong","citations":[],"abstained":false,"retrieved":["q9"]}`)
	s := sc.Score(context.Background(), out, gold)
	if s.Metrics["retrieval_failure"] != 1 {
		t.Errorf("retrieval_failure = %v, want 1 (supporting page never retrieved)", s.Metrics["retrieval_failure"])
	}
	if s.Metrics["synthesis_failure"] == 1 {
		t.Error("must NOT be a synthesis_failure when the page was never retrieved")
	}
}

// ---- Dispatch ----

func TestScorerForCoversAllTenSites(t *testing.T) {
	sites := []string{"extract", "compile", "match", "dup_judge", "canonical_name",
		"candidates", "search", "sweep", "merge", "ask"}
	for _, site := range sites {
		s, err := ScorerFor(site, StubJudge(), 5)
		if err != nil {
			t.Errorf("ScorerFor(%q): %v", site, err)
			continue
		}
		if s.Site() != site {
			t.Errorf("ScorerFor(%q).Site() = %q", site, s.Site())
		}
	}
	if _, err := ScorerFor("nope", StubJudge(), 5); err == nil {
		t.Error("ScorerFor of an unknown site should error")
	}
}

// Every scorer must ALWAYS report its dangerous axis as a named separate key, even
// on a perfect case — the asymmetry principle is structural, not conditional.
func TestDangerousAxisAlwaysNamed(t *testing.T) {
	cases := []struct {
		site   string
		out    string
		gold   string
		danger string
	}{
		{"match", `{"same":"","dup_pairs":[]}`, `{"same":""}`, "false_merge"},
		{"dup_judge", `{"verdict":"merge"}`, `{"verdict":"merge","evidence_present":true}`, "false_merge"},
		{"candidates", `["p1"]`, `{"relevant":["p1"]}`, "missed_candidate"},
		{"merge", `{"pages":[],"claims":[]}`, `{"write_set":[],"old_bodies":{}}`, "undeclared_cite_loss"},
		{"ask", `{"answer":"","abstained":true}`, `{"should_abstain":true}`, "fabrication"},
	}
	for _, c := range cases {
		s, err := ScorerFor(c.site, StubJudge(), 5)
		if err != nil {
			t.Fatalf("ScorerFor(%q): %v", c.site, err)
		}
		got := s.Score(context.Background(), []byte(c.out), []byte(c.gold))
		if _, ok := got.Dangerous[c.danger]; !ok {
			t.Errorf("%s: dangerous axis %q not named (keys: %v)", c.site, c.danger,
				keysOf(got.Dangerous))
		}
	}
}

func keysOf(m map[string]float64) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return strings.Join(ks, ",")
}

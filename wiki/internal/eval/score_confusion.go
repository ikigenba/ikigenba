package eval

import (
	"context"
	"encoding/json"
	"fmt"
)

// Asymmetric-confusion scorer (eval design kind 2) — match, dup_judge, and (a
// degenerate case) canonical_name. It reports a binary (match) / ternary (dup judge)
// confusion with FALSE-MERGE named as its own separate axis — the dangerous
// direction, because a false-merge silently fuses two real things and is far costlier
// than a false-split. For dup judge it also reports false-dismiss and a
// "can't-tell-yet-when-evidence-present" laziness metric. The match site's dup_pairs
// SIDE-CHANNEL is scored as its OWN recall number (research obligation 3: the
// side-channel is a distinct output, never folded into the verdict).

// --- match ---

// matchGold is the match site's gold: the expected verdict. Same names the gold
// matched subject id (empty == the gold is no_match), and ExpectedDupPairs is the
// gold side-channel the model should have surfaced.
type matchGold struct {
	Same             string `json:"same"`
	ExpectedDupPairs []struct {
		A string `json:"a"`
		B string `json:"b"`
	} `json:"dup_pairs"`
}

// MatchScorer scores the match site (binary same / no_match) with false-merge and
// false-split as named separate axes, plus the dup_pairs side-channel recall.
type MatchScorer struct{}

// NewMatchScorer builds the match confusion scorer.
func NewMatchScorer() *MatchScorer { return &MatchScorer{} }

func (s *MatchScorer) Site() string { return "match" }

func (s *MatchScorer) Score(_ context.Context, output, gold []byte) Score {
	sc := newScore()
	var out matchOutput
	if err := json.Unmarshal(output, &out); err != nil {
		sc.addErr(fmt.Sprintf("decode output: %v", err))
		// Unparseable verdict is the most dangerous failure: we cannot prove it did not
		// false-merge, so count it as one on the dangerous axis and a 0 headline.
		sc.Dangerous["false_merge"] = 1
		sc.Dangerous["false_split"] = 0
		sc.Dangerous["dup_pairs_recall"] = 0
		return sc
	}
	var g matchGold
	if err := json.Unmarshal(gold, &g); err != nil {
		sc.addErr(fmt.Sprintf("decode gold: %v", err))
		return sc
	}

	predMatched := out.Same != ""
	goldMatched := g.Same != ""

	correct := false
	switch {
	case goldMatched && predMatched:
		// Right call only if it matched the SAME subject id (matching the wrong
		// existing subject is still a false-merge of distinct things).
		correct = out.Same == g.Same
		if !correct {
			sc.Dangerous["false_merge"] = 1 // fused into the wrong subject
		}
	case !goldMatched && predMatched:
		sc.Dangerous["false_merge"] = 1 // FALSE-MERGE: said same when gold is distinct
	case goldMatched && !predMatched:
		sc.Dangerous["false_split"] = 1 // missed a real match
	default: // both no_match
		correct = true
	}
	if correct {
		sc.Headline = 1 // headline: 1 − false-merge-rate aggregated; per-case it's correctness
	}
	// Ensure both dangerous axes are always present (a separate, named axis even when 0).
	ensure(sc.Dangerous, "false_merge")
	ensure(sc.Dangerous, "false_split")

	// dup_pairs side-channel recall — its OWN number, never folded into the verdict.
	sc.Dangerous["dup_pairs_recall"] = dupPairsRecall(out.DupPairs, g.ExpectedDupPairs)
	return sc
}

// dupPairsRecall is the recall of the expected dup_pairs side-channel: of the gold
// pairs, how many the model surfaced (canonical-order insensitive). Returns 1 when
// no pairs are expected (nothing to miss).
func dupPairsRecall(pred, gold []struct {
	A string `json:"a"`
	B string `json:"b"`
}) float64 {
	if len(gold) == 0 {
		return 1
	}
	predSet := map[string]struct{}{}
	for _, p := range pred {
		predSet[pairKey(p.A, p.B)] = struct{}{}
	}
	hit := 0
	for _, g := range gold {
		if _, ok := predSet[pairKey(g.A, g.B)]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(gold))
}

func pairKey(a, b string) string {
	if a <= b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

// --- dup judge ---

// dupJudgeOutput is the dup-judge ternary verdict, preserved VERBATIM (research
// obligation 3): merge | dismiss | cant_tell. The cant_tell arm is load-bearing —
// scoring it requires knowing whether evidence was present (the laziness metric).
type dupJudgeOutput struct {
	Verdict string `json:"verdict"` // "merge" | "dismiss" | "cant_tell"
}

// dupJudgeGold is the dup-judge gold: the correct verdict plus whether decisive
// evidence was present in the input (so "cant_tell when evidence present" can be
// scored as laziness — eval design kind 2).
type dupJudgeGold struct {
	Verdict         string `json:"verdict"`
	EvidencePresent bool   `json:"evidence_present"`
}

// DupJudgeScorer scores the dup_judge site (ternary) with false-merge,
// false-dismiss, and the cant_tell-when-evidence-present laziness metric as named
// separate axes.
type DupJudgeScorer struct{}

// NewDupJudgeScorer builds the dup-judge confusion scorer.
func NewDupJudgeScorer() *DupJudgeScorer { return &DupJudgeScorer{} }

func (s *DupJudgeScorer) Site() string { return "dup_judge" }

func (s *DupJudgeScorer) Score(_ context.Context, output, gold []byte) Score {
	sc := newScore()
	ensure(sc.Dangerous, "false_merge")
	ensure(sc.Dangerous, "false_dismiss")
	ensure(sc.Dangerous, "lazy_cant_tell")
	var out dupJudgeOutput
	if err := json.Unmarshal(output, &out); err != nil {
		sc.addErr(fmt.Sprintf("decode output: %v", err))
		sc.Dangerous["false_merge"] = 1 // unparseable: cannot prove it didn't merge
		return sc
	}
	var g dupJudgeGold
	if err := json.Unmarshal(gold, &g); err != nil {
		sc.addErr(fmt.Sprintf("decode gold: %v", err))
		return sc
	}

	pred := normText(out.Verdict)
	want := normText(g.Verdict)
	if pred == want {
		sc.Headline = 1
	}
	// Dangerous axes are independent of the headline (a wrong call is bad; a wrong
	// DANGEROUS call is named).
	switch {
	case pred == "merge" && want != "merge":
		sc.Dangerous["false_merge"] = 1
	case pred == "dismiss" && want == "merge":
		sc.Dangerous["false_dismiss"] = 1
	case pred == "cant tell" && g.EvidencePresent:
		// Laziness: punted despite decisive evidence being present.
		sc.Dangerous["lazy_cant_tell"] = 1
	}
	return sc
}

// --- canonical_name (degenerate kind-2) ---

// canonicalNameScorer scores the canonical-name pick (low stakes) as a thin
// agreement-with-convention check riding kind-2 plumbing (eval design: a degenerate
// asymmetric-confusion case, not a fifth kind). Gold is the conventional pick; the
// only "dangerous" axis is disagreement.
type canonicalNameScorer struct{}

// NewCanonicalNameScorer builds the canonical-name agreement scorer.
func NewCanonicalNameScorer() Scorer { return &canonicalNameScorer{} }

func (s *canonicalNameScorer) Site() string { return "canonical_name" }

func (s *canonicalNameScorer) Score(_ context.Context, output, gold []byte) Score {
	sc := newScore()
	ensure(sc.Dangerous, "disagreement")
	var out struct {
		Name string `json:"name"`
	}
	var g struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(output, &out); err != nil {
		sc.addErr(fmt.Sprintf("decode output: %v", err))
		sc.Dangerous["disagreement"] = 1
		return sc
	}
	if err := json.Unmarshal(gold, &g); err != nil {
		sc.addErr(fmt.Sprintf("decode gold: %v", err))
		return sc
	}
	if normText(out.Name) == normText(g.Name) {
		sc.Headline = 1
	} else {
		sc.Dangerous["disagreement"] = 1
	}
	return sc
}

func ensure(m map[string]float64, k string) {
	if _, ok := m[k]; !ok {
		m[k] = 0
	}
}

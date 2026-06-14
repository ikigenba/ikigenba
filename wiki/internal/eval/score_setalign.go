package eval

import (
	"context"
	"encoding/json"
	"fmt"
)

// Set-alignment scorer (eval design kind 1) — extract and compile. It aligns the
// predicted subjects to the gold subjects, then reports subject PRECISION and RECALL
// SEPARATELY (over-extraction is the dangerous axis and gets its own counter, never
// hidden inside an F1), type accuracy on matched subjects, claim recall
// (fuzzy / LLM-judged via the injected Judge), and a self-containedness check.
// compile additionally reports compression ratio, per-claim citation
// precision/recall, and occurred_at accuracy (eval design kind 1's compile deltas).

// scoredSubject is the site-shaped subject the set-alignment scorer reads from both
// the predicted output and the gold (the eval-design site-shaped objects). Extract
// and compile both emit subjects[]; the gold is the reference subjects[].
type scoredSubject struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Claims []struct {
		Text  string   `json:"text"`
		Cites []string `json:"cites"`
	} `json:"claims"`
	OccurredAt string `json:"occurred_at"`
}

type setAlignPayload struct {
	Subjects []scoredSubject `json:"subjects"`
}

// SetAlignScorer scores extract or compile. compile=true enables the compile-only
// deltas (compression, per-claim cite precision/recall, occurred_at accuracy).
type SetAlignScorer struct {
	site    string
	compile bool
	judge   Judge
}

// NewExtractScorer builds the set-alignment scorer for the extract site.
func NewExtractScorer(judge Judge) *SetAlignScorer {
	return &SetAlignScorer{site: "extract", compile: false, judge: orStub(judge)}
}

// NewCompileScorer builds the set-alignment scorer for the compile site (with the
// compile-only deltas).
func NewCompileScorer(judge Judge) *SetAlignScorer {
	return &SetAlignScorer{site: "compile", compile: true, judge: orStub(judge)}
}

func (s *SetAlignScorer) Site() string { return s.site }

func (s *SetAlignScorer) Score(ctx context.Context, output, gold []byte) Score {
	sc := newScore()
	pred, err := decodeSubjects(output)
	if err != nil {
		sc.addErr(fmt.Sprintf("decode output: %v", err))
		// Unparseable output is a total miss, not a crash: 0 recall, and over-extraction
		// is unknown so we don't penalize that axis on a parse failure.
		return sc
	}
	g, err := decodeSubjects(gold)
	if err != nil {
		sc.addErr(fmt.Sprintf("decode gold: %v", err))
		return sc
	}

	// Align predicted↔gold subjects greedily by best name match (the eval-design
	// "align predicted↔gold subjects"). Each gold subject matches at most one
	// predicted subject and vice-versa.
	matchedPred, matchedGold := alignSubjects(pred, g)

	tp := len(matchedGold)             // matched golds == true positives
	fp := len(pred) - len(matchedPred) // predicted-but-unmatched == OVER-EXTRACTION
	fn := len(g) - len(matchedGold)    // gold-but-unmatched == misses

	precision := safeDiv(tp, tp+fp)
	recall := safeDiv(tp, tp+fn)
	sc.Metrics["subject_precision"] = precision
	sc.Metrics["subject_recall"] = recall
	sc.Headline = f1(precision, recall) // per-site headline: subject F1 (eval design q5)

	// DANGEROUS AXIS: over-extraction — predicted subjects with no gold (its own
	// counter, never folded into F1, eval design kind 1).
	sc.Dangerous["over_extract"] = float64(fp)

	// Type accuracy on matched subjects.
	typeHits := 0
	for pi, gi := range matchedPred {
		if normText(pred[pi].Type) == normText(g[gi].Type) {
			typeHits++
		}
	}
	if len(matchedPred) > 0 {
		sc.Metrics["type_accuracy"] = safeDiv(typeHits, len(matchedPred))
	}

	// Claim recall (fuzzy / LLM-judged) over matched subjects: of the gold claims,
	// how many a predicted claim expresses. Self-containedness: a predicted claim is
	// self-contained when its text stands alone (a blunt deterministic proxy: it is
	// non-empty and not a bare pronoun fragment); the judge refines it when present.
	var goldClaims, recalledClaims, predClaims, selfContained int
	var citeTP, citeFP, citeFN int
	var occMatch, occTotal int
	var totalPredTextLen, totalGoldTextLen int
	for pi, gi := range matchedPred {
		p, gg := pred[pi], g[gi]
		predClaims += len(p.Claims)
		goldClaims += len(gg.Claims)
		for _, pc := range p.Claims {
			totalPredTextLen += len(pc.Text)
			if isSelfContained(pc.Text) {
				selfContained++
			}
		}
		for _, gc := range gg.Claims {
			totalGoldTextLen += len(gc.Text)
			if s.claimRecalled(ctx, gc.Text, p.Claims) {
				recalledClaims++
			}
		}
		if s.compile {
			// Per-claim citation precision/recall: align predicted claims to gold by
			// text, then compare cite sets.
			ctp, cfp, cfn := citeOverlap(p.Claims, gg.Claims)
			citeTP += ctp
			citeFP += cfp
			citeFN += cfn
			// occurred_at accuracy (events): exact match of the world-time prefix.
			if gg.OccurredAt != "" {
				occTotal++
				if normText(p.OccurredAt) == normText(gg.OccurredAt) {
					occMatch++
				}
			}
		}
	}
	if goldClaims > 0 {
		sc.Metrics["claim_recall"] = safeDiv(recalledClaims, goldClaims)
	}
	if predClaims > 0 {
		sc.Metrics["self_containedness"] = safeDiv(selfContained, predClaims)
	}

	if s.compile {
		// Compression ratio: predicted total claim text length over gold (a digest
		// should be at or below 1; >1 flags expansion not compression).
		if totalGoldTextLen > 0 {
			sc.Metrics["compression_ratio"] = float64(totalPredTextLen) / float64(totalGoldTextLen)
		}
		sc.Metrics["cite_precision"] = safeDiv(citeTP, citeTP+citeFP)
		sc.Metrics["cite_recall"] = safeDiv(citeTP, citeTP+citeFN)
		if occTotal > 0 {
			sc.Metrics["occurred_at_accuracy"] = safeDiv(occMatch, occTotal)
		}
	}

	return sc
}

// claimRecalled reports whether the predicted claim set expresses the gold claim,
// via the injected judge (preferred) or the deterministic Jaccard fallback so the
// scorer runs offline (P14 Verify). Threshold 0.5 on Jaccard is the blunt fallback;
// the judge is the real arbiter when configured.
func (s *SetAlignScorer) claimRecalled(ctx context.Context, goldClaim string, predClaims []struct {
	Text  string   `json:"text"`
	Cites []string `json:"cites"`
}) bool {
	for _, pc := range predClaims {
		if s.judge.Similar(ctx, pc.Text, goldClaim) {
			return true
		}
		if jaccard(pc.Text, goldClaim) >= 0.5 {
			return true
		}
	}
	return false
}

// decodeSubjects accepts either the {"subjects":[...]} object shape or a bare
// subjects array, so an adapter or a generator may emit either.
func decodeSubjects(raw []byte) ([]scoredSubject, error) {
	var obj setAlignPayload
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Subjects != nil {
		return obj.Subjects, nil
	}
	var arr []scoredSubject
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// alignSubjects greedily pairs predicted to gold subjects by best name similarity
// (a name Jaccard ≥ 0.5 is a match). Returns predIdx→goldIdx and goldIdx→predIdx
// for the matched pairs.
func alignSubjects(pred, gold []scoredSubject) (matchedPred, matchedGold map[int]int) {
	matchedPred = map[int]int{}
	matchedGold = map[int]int{}
	var cands []alignCand
	for pi := range pred {
		for gi := range gold {
			sim := jaccard(pred[pi].Name, gold[gi].Name)
			if sim >= 0.5 {
				cands = append(cands, alignCand{pi, gi, sim})
			}
		}
	}
	// Greedy by descending similarity (stable enough for scoring; ties broken by
	// index order for determinism).
	sortCandsDesc(cands)
	for _, c := range cands {
		if _, ok := matchedPred[c.pi]; ok {
			continue
		}
		if _, ok := matchedGold[c.gi]; ok {
			continue
		}
		matchedPred[c.pi] = c.gi
		matchedGold[c.gi] = c.pi
	}
	return matchedPred, matchedGold
}

// alignCand is one candidate predicted↔gold subject pairing with its name
// similarity, used by alignSubjects' greedy matcher.
type alignCand struct {
	pi, gi int
	sim    float64
}

func sortCandsDesc(c []alignCand) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0; j-- {
			better := c[j].sim > c[j-1].sim ||
				(c[j].sim == c[j-1].sim && (c[j].pi < c[j-1].pi ||
					(c[j].pi == c[j-1].pi && c[j].gi < c[j-1].gi)))
			if better {
				c[j], c[j-1] = c[j-1], c[j]
			} else {
				break
			}
		}
	}
}

// isSelfContained is the blunt deterministic self-containedness proxy: a claim is
// self-contained when its normalized text has more than two tokens (a bare pronoun
// fragment like "it grew" fails). The judge refines this when configured.
func isSelfContained(text string) bool {
	return len(tokenSet(text)) > 2
}

// citeOverlap aligns predicted claims to gold claims by text similarity, then sums
// per-claim citation true/false positives and false negatives over the alignment.
func citeOverlap(pred, gold []struct {
	Text  string   `json:"text"`
	Cites []string `json:"cites"`
}) (tp, fp, fn int) {
	used := map[int]bool{}
	for gi := range gold {
		best, bestSim := -1, 0.5
		for pi := range pred {
			if used[pi] {
				continue
			}
			if sim := jaccard(pred[pi].Text, gold[gi].Text); sim >= bestSim {
				best, bestSim = pi, sim
			}
		}
		if best < 0 {
			fn += len(setOf(gold[gi].Cites)) // an unmatched gold claim's cites are all missed
			continue
		}
		used[best] = true
		pset := setOf(pred[best].Cites)
		gset := setOf(gold[gi].Cites)
		for c := range gset {
			if _, ok := pset[c]; ok {
				tp++
			} else {
				fn++
			}
		}
		for c := range pset {
			if _, ok := gset[c]; !ok {
				fp++
			}
		}
	}
	return tp, fp, fn
}

func setOf(xs []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, x := range xs {
		out[x] = struct{}{}
	}
	return out
}

func safeDiv(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func orStub(j Judge) Judge {
	if j == nil {
		return stubJudge{}
	}
	return j
}

package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Ask scorer (eval design kind 4, the ask half) — citation FAITHFULNESS as a
// mechanical "does the cited page exist + contain the span" check (deterministic,
// offline), PLUS a rubric judge panel for answer correctness, abstention correctness
// on the gap set, and contradiction-surfacing. FABRICATION RATE IS THE HEADLINE
// (eval design kind 4): an answer that fabricates on a question with no supporting
// page is the cardinal sin, named on the dangerous axis. Retrieval failure is
// DECOMPOSED from synthesis failure: if the answer is wrong because the supporting
// page was never retrieved, that is a retrieval miss, not a synthesis fabrication.

// askOutput is the ask site's raw output: the answer prose, the cited page ids, and
// whether the agent abstained (declared the answer unknown / unsupported).
type askOutput struct {
	Answer    string   `json:"answer"`
	Citations []string `json:"citations"`
	Abstained bool     `json:"abstained"`
	Retrieved []string `json:"retrieved"` // page ids the retriever surfaced (for the decomposition)
}

// askGold is the ask case's gold: the reference answer, the supporting page ids that
// actually contain the answer, an abstention flag (true == this is a GAP-SET case
// where the right behaviour is to abstain — fabricating here is the headline sin),
// and, per supporting page, the span text the citation must contain (faithfulness).
type askGold struct {
	Answer        string            `json:"answer"`
	Supporting    []string          `json:"supporting"`     // page ids that contain the answer
	ShouldAbstain bool              `json:"should_abstain"` // gap-set case: abstaining is correct
	Spans         map[string]string `json:"spans"`          // page id → the span the citation must contain
	PageBodies    map[string]string `json:"page_bodies"`    // page id → body, for the faithfulness span check
}

// AskScorer scores the ask site.
type AskScorer struct {
	judge Judge
}

// NewAskScorer builds the ask scorer over an injected judge (StubJudge offline).
func NewAskScorer(judge Judge) *AskScorer {
	return &AskScorer{judge: orStub(judge)}
}

func (s *AskScorer) Site() string { return "ask" }

func (s *AskScorer) Score(ctx context.Context, output, gold []byte) Score {
	sc := newScore()
	ensure(sc.Dangerous, "fabrication")
	ensure(sc.Dangerous, "citation_unfaithful")

	var out askOutput
	if err := json.Unmarshal(output, &out); err != nil {
		sc.addErr(fmt.Sprintf("decode output: %v", err))
		sc.Dangerous["fabrication"] = 1 // an unparseable answer is treated as a fabrication risk
		return sc
	}
	var g askGold
	if err := json.Unmarshal(gold, &g); err != nil {
		sc.addErr(fmt.Sprintf("decode gold: %v", err))
		return sc
	}

	// --- The gap set: abstention correctness; fabrication is the headline sin ---
	if g.ShouldAbstain {
		if out.Abstained {
			sc.Headline = 1 // correctly abstained on a gap question
		} else {
			// FABRICATION: answered a question with no supporting page.
			sc.Dangerous["fabrication"] = 1
			sc.Headline = 0
			sc.Metrics["abstention_correct"] = 0
			return sc
		}
		sc.Metrics["abstention_correct"] = 1
		return sc
	}

	// --- Answerable case ---
	if out.Abstained {
		// Abstained on an answerable question: a (non-dangerous) miss, not a fabrication.
		sc.Metrics["abstention_correct"] = 0
		sc.Metrics["answer_correct"] = 0
		// Decompose: was the supporting page even retrieved? If not, it's a retrieval
		// failure, not a synthesis failure.
		sc.Metrics["retrieval_hit"] = boolScore(anyRetrieved(out.Retrieved, g.Supporting))
		sc.Headline = 0
		return sc
	}

	// --- Citation faithfulness (mechanical): each cited page exists and contains its span ---
	faithful := true
	for _, cid := range out.Citations {
		body, exists := g.PageBodies[cid]
		if !exists {
			faithful = false
			sc.addErr(fmt.Sprintf("cited page %q does not exist (fabricated citation)", cid))
			continue
		}
		if span, ok := g.Spans[cid]; ok && span != "" {
			if !strings.Contains(normText(body), normText(span)) {
				faithful = false
				sc.addErr(fmt.Sprintf("cited page %q does not contain the required span", cid))
			}
		}
	}
	sc.Metrics["citation_faithfulness"] = boolScore(faithful)
	if !faithful {
		sc.Dangerous["citation_unfaithful"] = 1
	}

	// --- Retrieval decomposition: was a supporting page retrieved at all? ---
	retrievalHit := anyRetrieved(out.Retrieved, g.Supporting)
	sc.Metrics["retrieval_hit"] = boolScore(retrievalHit)

	// --- Answer correctness (rubric judge) ---
	correct, judged := s.answerCorrect(ctx, out.Answer, g.Answer)
	if judged {
		sc.Metrics["answer_correct"] = boolScore(correct)
		// Headline: 1 − fabrication-rate aggregated; per-case the headline is a faithful,
		// correct, non-fabricated answer.
		sc.Headline = boolScore(correct && faithful)
		// If the answer is wrong AND the supporting page was never retrieved, it's a
		// retrieval failure decomposed from synthesis failure (eval design kind 4).
		if !correct {
			if retrievalHit {
				sc.Metrics["synthesis_failure"] = 1
			} else {
				sc.Metrics["retrieval_failure"] = 1
			}
		}
	} else {
		// No judge (offline): headline is the deterministic faithfulness check alone.
		sc.Headline = boolScore(faithful)
	}
	return sc
}

// answerCorrect asks the judge panel whether the candidate answer is correct against
// the gold answer. Returns (correct, judged); judged=false when no judge is
// configured (the stub abstains) so the scorer falls back to the mechanical headline.
func (s *AskScorer) answerCorrect(ctx context.Context, candidate, gold string) (correct, judged bool) {
	verdict, _, panel := s.judge.YesNo(ctx, fmt.Sprintf("Is this answer correct and consistent with the reference answer %q?", gold), candidate, gold)
	if panel == 0 {
		return false, false
	}
	return verdict, true
}

func anyRetrieved(retrieved, supporting []string) bool {
	if len(supporting) == 0 {
		return true
	}
	rset := setOf(retrieved)
	for _, s := range supporting {
		if _, ok := rset[s]; ok {
			return true
		}
	}
	return false
}

package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

// Recall@k + RRF scorer (eval design kind 3) — candidates, search, sweep. The
// retrieval lanes return a RANKED list of ids; the scorer compares against a gold
// relevant set. The dangerous direction differs per site and is named separately:
//   - candidates: RECALL@k is king — a miss mints a permanent duplicate (the
//     dangerous axis is a missed relevant candidate).
//   - search: a ranking metric (nDCG) plus recall.
//   - sweep: pair-discovery recall — a missed duplicate pair is never swept.

// retrievalOutput is the ranked id list a retrieval lane returns.
type retrievalOutput struct {
	Results []string `json:"results"`
}

// retrievalGold is the gold relevant set for a retrieval case.
type retrievalGold struct {
	Relevant []string `json:"relevant"`
}

// RetrievalScorer scores candidates / search / sweep. K bounds recall@k (the
// shortlist depth the site actually uses); ranking=true adds the nDCG ranking metric
// (search). dangerAxis names this site's dangerous miss counter.
type RetrievalScorer struct {
	site       string
	k          int
	ranking    bool
	dangerAxis string
}

// NewCandidatesScorer builds the candidates retrieval scorer (recall@k is king).
func NewCandidatesScorer(k int) *RetrievalScorer {
	return &RetrievalScorer{site: "candidates", k: k, dangerAxis: "missed_candidate"}
}

// NewSearchScorer builds the search retrieval scorer (recall@k + a ranking metric).
func NewSearchScorer(k int) *RetrievalScorer {
	return &RetrievalScorer{site: "search", k: k, ranking: true, dangerAxis: "missed_relevant"}
}

// NewSweepScorer builds the sweep scorer (pair-discovery recall — a missed dup pair
// is never swept).
func NewSweepScorer(k int) *RetrievalScorer {
	return &RetrievalScorer{site: "sweep", k: k, dangerAxis: "missed_pair"}
}

func (s *RetrievalScorer) Site() string { return s.site }

func (s *RetrievalScorer) Score(_ context.Context, output, gold []byte) Score {
	sc := newScore()
	ensure(sc.Dangerous, s.dangerAxis)
	out, err := decodeRanked(output)
	if err != nil {
		sc.addErr(fmt.Sprintf("decode output: %v", err))
		// No retrieval == everything missed: recall 0, all gold counted as missed below.
		out = nil
	}
	var g retrievalGold
	if err := json.Unmarshal(gold, &g); err != nil {
		sc.addErr(fmt.Sprintf("decode gold: %v", err))
		return sc
	}

	goldSet := setOf(g.Relevant)
	if len(goldSet) == 0 {
		// Nothing relevant: a perfect recall, and any returned id is a (non-dangerous)
		// false positive — recorded but not the headline.
		sc.Headline = 1
		sc.Metrics["false_positives"] = float64(len(out))
		return sc
	}

	k := s.k
	if k <= 0 || k > len(out) {
		k = len(out)
	}
	topK := out
	if k < len(out) {
		topK = out[:k]
	}
	hit := 0
	for _, id := range topK {
		if _, ok := goldSet[id]; ok {
			hit++
		}
	}
	recall := float64(hit) / float64(len(goldSet))
	sc.Metrics["recall_at_k"] = recall
	sc.Headline = recall // per-site headline: recall@k (eval design q5)

	// DANGEROUS AXIS: the count of relevant ids missed in the top-k (a permanent dup
	// for candidates, an unswept pair for sweep, an unfound page for search).
	sc.Dangerous[s.dangerAxis] = float64(len(goldSet) - hit)

	if s.ranking {
		sc.Metrics["ndcg_at_k"] = ndcg(topK, goldSet)
	}
	return sc
}

// decodeRanked accepts either {"results":[...]} or a bare id array.
func decodeRanked(raw []byte) ([]string, error) {
	var obj retrievalOutput
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Results != nil {
		return obj.Results, nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// ndcg computes nDCG@k with binary relevance (1 if in the gold set, else 0).
func ndcg(ranked []string, gold map[string]struct{}) float64 {
	var dcg float64
	for i, id := range ranked {
		if _, ok := gold[id]; ok {
			dcg += 1 / math.Log2(float64(i+2))
		}
	}
	// Ideal DCG: all relevant ranked first, capped at len(ranked).
	ideal := len(gold)
	if ideal > len(ranked) {
		ideal = len(ranked)
	}
	var idcg float64
	for i := 0; i < ideal; i++ {
		idcg += 1 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

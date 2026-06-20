package eval

import "fmt"

// ScorerFor returns the scorer for one of the ten inference sites, wired with the
// injected judge where the site has a subjective half. The four scorer kinds cover
// all ten sites (eval design "The four scorer kinds"); canonical_name rides the
// confusion kind as a degenerate case. The retrieval sites take their shortlist
// depth k (the config knob the sweep tunes — eval obligation 2); a default of 10 is
// used when k≤0.
//
// This is the P14 dispatch surface the P16 sweep driver calls: site name → the
// scorer that reads the cached raw output and the case gold and emits a Score with
// its dangerous axis named separately.
func ScorerFor(site string, judge Judge, k int) (Scorer, error) {
	if k <= 0 {
		k = 10
	}
	switch site {
	case "extract":
		return NewExtractScorer(judge), nil
	case "compile":
		return NewCompileScorer(judge), nil
	case "match":
		return NewMatchScorer(), nil
	case "dup_judge":
		return NewDupJudgeScorer(), nil
	case "canonical_name":
		return NewCanonicalNameScorer(), nil
	case "candidates":
		return NewCandidatesScorer(k), nil
	case "search":
		return NewSearchScorer(k), nil
	case "sweep":
		return NewSweepScorer(k), nil
	case "merge":
		return NewMergeScorer(judge), nil
	case "ask":
		return NewAskScorer(judge), nil
	default:
		return nil, fmt.Errorf("eval: no scorer for site %q", site)
	}
}

package eval

import (
	"strings"
	"unicode"
)

// normText lowercases and collapses a string's whitespace/punctuation to a single
// space, for the deterministic fuzzy-match fallback the scorers use when no LLM
// judge is configured (the mechanical surface must score offline — P14 Verify).
// This is NOT the registry's `normalize` (that is Part I, separately tested); it is
// only the scorer's blunt string-equality helper.
func normText(s string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// tokenSet returns the deduplicated token set of normalized text.
func tokenSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range strings.Fields(normText(s)) {
		out[t] = struct{}{}
	}
	return out
}

// jaccard is the deterministic fuzzy-similarity fallback: the token Jaccard of two
// strings, in [0,1]. Used for claim recall when no judge is injected so the scorer
// is still exercised offline; a real run swaps in the LLM judge (eval design q2).
func jaccard(a, b string) float64 {
	sa, sb := tokenSet(a), tokenSet(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	inter := 0
	for t := range sa {
		if _, ok := sb[t]; ok {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// f1 computes the harmonic mean of precision and recall (0 when either is 0).
func f1(precision, recall float64) float64 {
	if precision+recall == 0 {
		return 0
	}
	return 2 * precision * recall / (precision + recall)
}

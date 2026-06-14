package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The §6.1 citation-preservation gate, inherited by the fold (design §6: "fold
// inherits the merge craft obligations" / §6.1). It is the same invariant the
// document-pass commit enforces (internal/run): every citation present on EITHER
// source body must survive into the merged body OR be declared in the fold's
// `superseded` list. Undeclared loss = paraphrased-away evidence = a failed call.
// lint-dups runs this against the COMBINED old citations (winner ∪ loser) before
// applying the merge transaction, so the fold can never silently drop evidence.

// citePattern matches an inline [inbox-id] citation — deliberately liberal (any
// non-bracket, non-whitespace token in brackets) so a style drift never silently
// blinds the gate (the same stance as the document-pass gate).
var citePattern = regexp.MustCompile(`\[([^\[\]\s]+)\]`)

// extractCites returns the de-duplicated SET of citation ids in a body.
func extractCites(body string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, m := range citePattern.FindAllStringSubmatch(body, -1) {
		if id := strings.TrimSpace(m[1]); id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

// checkCitationPreservation asserts (old − new) is EXACTLY the declared superseded
// set, where old is the union of citations across BOTH source bodies (the fold
// reads two pages). A citation in old but not in new and not declared superseded
// is undeclared evidence loss → error (the failed fold of §6.1). A redundant
// declaration (a superseded id still present in new) is tolerated — the gate
// forbids SILENT loss, not over-declaration.
func checkCitationPreservation(oldBodies []string, newBody string, superseded []string) error {
	old := make(map[string]struct{})
	for _, b := range oldBodies {
		for id := range extractCites(b) {
			old[id] = struct{}{}
		}
	}
	if len(old) == 0 {
		return nil
	}
	newCites := extractCites(newBody)
	declared := make(map[string]struct{}, len(superseded))
	for _, s := range superseded {
		if t := strings.TrimSpace(s); t != "" {
			declared[t] = struct{}{}
		}
	}

	var undeclared []string
	for id := range old {
		if _, kept := newCites[id]; kept {
			continue
		}
		if _, ok := declared[id]; ok {
			continue
		}
		undeclared = append(undeclared, id)
	}
	if len(undeclared) == 0 {
		return nil
	}
	sort.Strings(undeclared)
	return fmt.Errorf("fold citation preservation (§6.1): %d citation(s) dropped without a superseded declaration: %s",
		len(undeclared), strings.Join(undeclared, ", "))
}

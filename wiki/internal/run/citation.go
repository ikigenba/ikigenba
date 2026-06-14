package run

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The citation-preservation gate (design §6.1) — a commit-time invariant, not a
// lint job, because the judgment clause ("a citation survives OR was deliberately
// superseded") is knowable only at write time. Every merge that rewrites a page
// also emits a `superseded` list — the citation ids it deliberately dropped. At
// commit the gate runs pure set arithmetic: `old citations − new citations` must
// EXACTLY equal the declared superseded set. Any undeclared loss means the model
// paraphrased away evidence → a FAILED CALL (the transaction never commits; the
// row stays pending and is retried in-run by the conflict/merge path). It is
// nearly free (two regex scans + a set difference) and its declarations are the
// audit trail for vanished citations.

// citePattern matches an inline `[inbox-id]` citation. Inbox ids are ULIDs
// (Crockford base32: 0-9 A-Z minus I L O U), but the gate is deliberately liberal
// — it matches any non-empty, non-bracket, non-whitespace token in brackets — so a
// citation style drift never silently makes the gate stop seeing citations (which
// would let real evidence loss slip through). Set arithmetic over whatever the gate
// sees is the invariant; the exact token grammar is not load-bearing here.
var citePattern = regexp.MustCompile(`\[([^\[\]\s]+)\]`)

// extractCites returns the SET of inline citation ids in a page body (design §6.1),
// de-duplicated. Order is not significant — the gate compares sets.
func extractCites(body string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, m := range citePattern.FindAllStringSubmatch(body, -1) {
		id := strings.TrimSpace(m[1])
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

// checkCitationPreservation is the §6.1 gate for one page rewrite: given the OLD
// page body, the NEW (merged) page body, and merge's declared `superseded` list, it
// asserts `old − new` (citations the rewrite dropped) is EXACTLY the declared set.
//
//   - A citation in old but not in new AND not declared superseded → undeclared
//     evidence loss → error (the FAILED CALL of §6.1).
//   - A declared-superseded id that is still present in new (not actually dropped)
//     is tolerated: the declaration is an over-statement, not evidence loss, and the
//     gate's job is to forbid SILENT loss, not to police redundant declarations.
//
// On violation it returns a descriptive error naming the undeclared-dropped ids, so
// the failed-run accounting and the human see exactly what was paraphrased away.
func checkCitationPreservation(oldBody, newBody string, superseded []string) error {
	old := extractCites(oldBody)
	if len(old) == 0 {
		return nil // a new/empty page can drop nothing
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
			continue // citation survived the rewrite
		}
		if _, ok := declared[id]; ok {
			continue // deliberately superseded — declared, audited
		}
		undeclared = append(undeclared, id)
	}
	if len(undeclared) == 0 {
		return nil
	}
	sort.Strings(undeclared) // deterministic message for tests + the human
	return fmt.Errorf("citation preservation (§6.1): %d citation(s) dropped without a superseded declaration: %s",
		len(undeclared), strings.Join(undeclared, ", "))
}

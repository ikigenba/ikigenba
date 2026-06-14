package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Merge scorer (eval design kind 4, the merge half) — the deterministic mechanical
// invariants Part I already exposes (the §6.1 citation-preservation gate, write-set
// conformance, claim-cite presence) reused here for free, PLUS a rubric LLM-judge
// panel for the subjective criteria (lead-identity, woven-not-ledgered,
// contradictions-corralled, no-loss, no-hallucination). The mechanical half is pure
// and deterministic (offline, no key — the P14 Verify); the rubric half calls the
// injected Judge (panel of N per eval design q2) and a stub judge abstains so the
// scorer still runs offline.
//
// The §6.1 gate logic is transcribed from wiki/internal/run/citation.go
// (checkCitationPreservation) — that function is unexported, so the scorer carries
// the same pure set-arithmetic here rather than reaching into Part I's internals; the
// invariant is byte-identical (old − new must equal declared superseded).

// mergeOutput is the merge site's raw output the scorer reads: the rewritten pages.
// Each page names its subject (the write-set member), its new body (citation source),
// and the superseded declarations the §6.1 gate checks.
type mergeOutput struct {
	Pages []struct {
		Subject    string   `json:"subject"`
		Title      string   `json:"title"`
		Body       string   `json:"body"`
		Superseded []string `json:"superseded"`
	} `json:"pages"`
	Claims []struct {
		Text  string   `json:"text"`
		Cites []string `json:"cites"`
	} `json:"claims"`
}

// mergeGold is the merge case's gold (eval design "the rubric expectations +
// must-survive cite ids for merge"): the write set the output pages must exactly
// cover, the old page bodies per subject (so the §6.1 gate has the prior citations),
// the cite ids that must survive, and the source text the rubric judge contrasts the
// merged prose against.
type mergeGold struct {
	WriteSet    []string          `json:"write_set"`    // exact target pages the output must cover
	OldBodies   map[string]string `json:"old_bodies"`   // subject → prior page body (for §6.1)
	MustSurvive []string          `json:"must_survive"` // cite ids that must remain present
	SourceText  string            `json:"source_text"`  // the claims the merge folded (rubric context)
	LeadName    string            `json:"lead_name"`    // the identity the lead should establish
}

// MergeScorer scores the merge site. The mechanical checks are deterministic; the
// rubric panel calls the injected judge.
type MergeScorer struct {
	judge Judge
}

// NewMergeScorer builds the merge scorer over an injected judge (StubJudge for the
// offline mechanical-only run; the held-out model in a full sweep).
func NewMergeScorer(judge Judge) *MergeScorer {
	return &MergeScorer{judge: orStub(judge)}
}

func (s *MergeScorer) Site() string { return "merge" }

func (s *MergeScorer) Score(ctx context.Context, output, gold []byte) Score {
	sc := newScore()
	// The dangerous axis for merge is undeclared evidence loss (a citation
	// paraphrased away without a superseded declaration) and hallucination.
	ensure(sc.Dangerous, "undeclared_cite_loss")
	ensure(sc.Dangerous, "hallucination")

	var out mergeOutput
	if err := json.Unmarshal(output, &out); err != nil {
		sc.addErr(fmt.Sprintf("decode output: %v", err))
		// Unparseable merge output fails every mechanical check (0) and is the most
		// dangerous: cannot prove citations survived.
		sc.Dangerous["undeclared_cite_loss"] = 1
		return sc
	}
	var g mergeGold
	if err := json.Unmarshal(gold, &g); err != nil {
		sc.addErr(fmt.Sprintf("decode gold: %v", err))
		return sc
	}

	// --- Mechanical check 1: §6.1 citation-preservation gate (per page) ---
	citePass := true
	for _, p := range out.Pages {
		old := g.OldBodies[p.Subject]
		if err := citationPreserved(old, p.Body, p.Superseded); err != nil {
			citePass = false
			sc.addErr(err.Error())
		}
	}
	// Also: every must-survive cite id is present in some output body.
	allBodies := strings.Builder{}
	for _, p := range out.Pages {
		allBodies.WriteString(p.Body)
		allBodies.WriteByte('\n')
	}
	present := extractCiteSet(allBodies.String())
	for _, id := range g.MustSurvive {
		if _, ok := present[id]; !ok {
			citePass = false
			sc.addErr(fmt.Sprintf("must-survive cite %q absent from merged bodies", id))
		}
	}
	sc.Metrics["citation_preservation"] = boolScore(citePass)
	if !citePass {
		sc.Dangerous["undeclared_cite_loss"] = 1
	}

	// --- Mechanical check 2: write-set conformance ---
	wsPass := writeSetConforms(out, g.WriteSet)
	sc.Metrics["write_set_conformance"] = boolScore(wsPass)
	if !wsPass {
		sc.addErr("write-set conformance: output pages != gold write set")
	}

	// --- Mechanical check 3: claim-cite presence ---
	ccPass := true
	for i, c := range out.Claims {
		if len(c.Cites) == 0 {
			ccPass = false
			sc.addErr(fmt.Sprintf("claim %d has no cite", i))
		}
	}
	sc.Metrics["claim_cite_presence"] = boolScore(ccPass)

	mechMean := (boolScore(citePass) + boolScore(wsPass) + boolScore(ccPass)) / 3
	sc.Metrics["mechanical_mean"] = mechMean

	// --- Rubric judge panel (subjective criteria) ---
	rubricMean, hallucinated := s.rubric(ctx, out, g)
	if rubricMean >= 0 {
		sc.Metrics["rubric_mean"] = rubricMean
		// Per-site headline for merge is the rubric mean (eval design q5), gated by the
		// mechanical checks: a merge that drops citations cannot score well no matter the
		// prose, so the headline is the rubric mean scaled by the mechanical mean.
		sc.Headline = rubricMean * mechMean
	} else {
		// No judge configured (offline mechanical-only run): the headline is the
		// mechanical mean alone, so the scorer still produces a deterministic number.
		sc.Headline = mechMean
	}
	if hallucinated {
		sc.Dangerous["hallucination"] = 1
	}
	return sc
}

// rubric runs the judge panel over merge's five subjective criteria. Returns the
// mean of the graded criteria in [0,1] (or -1 when no judge is configured) and
// whether the no-hallucination criterion failed (the dangerous axis). Each criterion
// is one panel YesNo; the judge already aggregates the panel internally.
func (s *MergeScorer) rubric(ctx context.Context, out mergeOutput, g mergeGold) (mean float64, hallucinated bool) {
	mergedProse := strings.Builder{}
	for _, p := range out.Pages {
		mergedProse.WriteString(p.Title)
		mergedProse.WriteByte('\n')
		mergedProse.WriteString(p.Body)
		mergedProse.WriteByte('\n')
	}
	prose := mergedProse.String()

	criteria := []struct {
		name string
		q    string
	}{
		{"lead_identity", fmt.Sprintf("Does the page's lead clearly establish the identity of %q (its type and what it is) in the first sentence?", g.LeadName)},
		{"woven_not_ledgered", "Is the knowledge woven into coherent prose rather than listed as a ledger of disconnected dated entries?"},
		{"contradictions_corralled", "Where the source facts conflict, does the page surface the contradiction explicitly rather than silently picking one side?"},
		{"no_loss", "Does the merged page retain all the substantive facts present in the source text?"},
	}

	var sum float64
	var n int
	anyJudged := false
	for _, c := range criteria {
		verdict, yes, panel := s.judge.YesNo(ctx, c.q, prose, g.SourceText)
		if panel == 0 {
			continue // judge abstained (stub / offline) — criterion not scored
		}
		anyJudged = true
		// Graded by panel agreement fraction (eval design q2: median for graded; the
		// agreement fraction is the per-criterion graded value).
		sum += float64(yes) / float64(panel)
		n++
		_ = verdict
	}
	// no-hallucination is a binary dangerous criterion: did the page assert something
	// the source does not support?
	hv, _, hp := s.judge.YesNo(ctx, "Does the page assert any fact that the source text does NOT support (a hallucination)?", prose, g.SourceText)
	if hp > 0 {
		anyJudged = true
		hallucinated = hv
	}

	if !anyJudged || n == 0 {
		return -1, hallucinated
	}
	return sum / float64(n), hallucinated
}

// --- §6.1 gate, transcribed (pure, deterministic) ---

var evalCitePattern = regexp.MustCompile(`\[([^\[\]\s]+)\]`)

func extractCiteSet(body string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range evalCitePattern.FindAllStringSubmatch(body, -1) {
		if id := strings.TrimSpace(m[1]); id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

// citationPreserved is the §6.1 gate (transcribed from
// wiki/internal/run.checkCitationPreservation): old − new must EXACTLY equal the
// declared superseded set; any undeclared dropped citation is the FAILED CALL.
func citationPreserved(oldBody, newBody string, superseded []string) error {
	old := extractCiteSet(oldBody)
	if len(old) == 0 {
		return nil
	}
	newCites := extractCiteSet(newBody)
	declared := map[string]struct{}{}
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
	return fmt.Errorf("citation preservation (§6.1): %d citation(s) dropped without a superseded declaration: %s",
		len(undeclared), strings.Join(undeclared, ", "))
}

// writeSetConforms checks the output pages cover EXACTLY the gold write set (every
// output page is in the set, and every set member was written) — the same invariant
// integrate.ApplyMerge enforces.
func writeSetConforms(out mergeOutput, writeSet []string) bool {
	if len(writeSet) == 0 {
		return len(out.Pages) == 0
	}
	want := setOf(writeSet)
	got := map[string]struct{}{}
	for _, p := range out.Pages {
		if _, ok := want[p.Subject]; !ok {
			return false // wrote a page outside the write set
		}
		got[p.Subject] = struct{}{}
	}
	for w := range want {
		if _, ok := got[w]; !ok {
			return false // a write-set member was not written
		}
	}
	return true
}

func boolScore(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

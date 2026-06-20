package eval

import "context"

// The scorer library (plan P14, eval design "The four scorer kinds"). Four kinds
// cover all ten inference sites; EVERY scorer reports the dangerous direction as a
// named separate axis, never lumped into one accuracy number (the research doc's
// asymmetry principle, made a structural requirement on the scorer interface). A
// scorer reads the cached RAW call-site output (the runner's CaseResult.Output) and
// the dataset case's gold, and emits a Score — a headline plus the named
// dangerous-direction rates and any supporting counts. Aggregation across a config's
// cases is the report's job (P16); a Scorer scores ONE (output, gold) pair.
//
// The four kinds:
//   - Set-alignment (extract, compile) — score_setalign.go
//   - Asymmetric confusion (match, dup_judge, canonical_name) — score_confusion.go
//   - Recall@k + RRF (candidates, search, sweep) — score_retrieval.go
//   - Mechanical-checks + rubric-judge-panel (merge, ask) — score_merge.go / score_ask.go
//
// The mechanical halves are pure and deterministic (tested offline against
// hand-built known-good/known-bad inputs, the P14 Verify). The subjective halves
// (fuzzy/LLM-judged claim recall in set-alignment, the merge/ask rubric panels) call
// an injected Judge so the deterministic surface stays testable with no key, and the
// judge model is pinned + held out of the run's sweep (eval design q2).

// Score is one scorer's verdict on a single (output, gold) pair. Headline is the
// site's primary score in [0,1]; Dangerous carries the named dangerous-direction
// rates (e.g. "false_merge", "over_extract", "fabrication") as a separate axis the
// report never folds into Headline; Metrics carries the rest of the scorer's
// supporting numbers (precision, recall, type accuracy, …). Errs records any
// non-fatal scoring problem (e.g. unparseable output counted as a failure, not a
// crash).
type Score struct {
	// Headline is the site's primary score in [0,1] (eval design q5's per-site
	// headline: subject F1, 1−false-merge-rate, recall@k, the rubric mean, …).
	Headline float64
	// Dangerous is the named dangerous-direction axis — one entry per stressor the
	// scorer surfaces, kept SEPARATE from Headline so a 95%-with-one-false-merge
	// config is visibly worse than a 90%-with-none config (research doc's worked
	// example). For per-case scorers a rate is 0 or 1 (the case did/didn't exhibit
	// the dangerous behaviour); the report averages across cases.
	Dangerous map[string]float64
	// Metrics carries the scorer's supporting numbers (precision, recall, type
	// accuracy, compression ratio, citation precision/recall, …).
	Metrics map[string]float64
	// Errs records non-fatal scoring problems (unparseable output, a missing gold
	// field) so a bad case is a recorded zero, not a panic.
	Errs []string
}

// newScore builds a Score with initialized maps.
func newScore() Score {
	return Score{Dangerous: map[string]float64{}, Metrics: map[string]float64{}}
}

// addErr records a non-fatal scoring problem.
func (s *Score) addErr(msg string) { s.Errs = append(s.Errs, msg) }

// Scorer scores one cached call-site output against its gold. The output is the
// runner's raw cache value (site-shaped JSON); the gold is the dataset case's
// site-shaped gold (json.RawMessage). A scorer NEVER calls a provider for the
// mechanical part; only the rubric/fuzzy half reaches the injected Judge.
type Scorer interface {
	// Site is the registry site name this scorer covers.
	Site() string
	// Score scores one (output, gold) pair. ctx carries cancellation for any judge
	// call; a scorer with no judged criteria ignores it.
	Score(ctx context.Context, output, gold []byte) Score
}

// Judge is the held-out LLM judge the subjective scorer halves call (eval design
// q2: a single fixed model, excluded from the run's sweep, a panel of N samples for
// subjective criteria). It is injected so the mechanical scorer surface stays
// testable offline with no key — a nil/stub Judge yields a deterministic abstain.
type Judge interface {
	// YesNo asks a binary rubric question about the candidate text given context,
	// returning the panel-aggregated verdict (majority over the panel) and the
	// number of panelists that voted yes. A scorer uses the rate (yes/panel) for a
	// graded criterion and the bool for a binary one.
	YesNo(ctx context.Context, question, candidate, context_ string) (verdict bool, yesVotes, panel int)
	// Similar asks whether a predicted item expresses the same fact as a gold item
	// (the fuzzy/LLM-judged claim-recall match in set-alignment). It returns the
	// panel-majority verdict.
	Similar(ctx context.Context, predicted, gold string) bool
}

// stubJudge is the default offline judge: it abstains (votes no, reports a panel of
// 0) so a scorer run with no configured judge yields deterministic, key-free
// results for the mechanical surface. The P14 unit tests inject a deterministic
// fakeJudge where the judged path is under test; P16 wires the real held-out model.
type stubJudge struct{}

func (stubJudge) YesNo(context.Context, string, string, string) (bool, int, int) {
	return false, 0, 0
}
func (stubJudge) Similar(context.Context, string, string) bool { return false }

// StubJudge returns the offline abstaining judge (P14's default; P16 swaps the real
// held-out model).
func StubJudge() Judge { return stubJudge{} }

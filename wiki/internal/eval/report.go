package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// The sweep + report (plan P16, eval design question 6). This is the product of
// Part II: it turns the rig's raw CaseResults (P13) and the scorer library (P14)
// into the comparison table a human reads to PICK a config — the feedback loop
// that licenses the (prompt, model, effort) defaults Part I deferred. It is NOT a
// gate; every number is an input to a human decision (eval design "What this is
// not"), and nothing here auto-promotes a config (the q6 "no single composite
// rank" + "nothing auto-promotes" rules are structural here).
//
// The report obeys the q6 presentation lock verbatim:
//   - one row per (model, effort) config, NEVER a single lumped rank;
//   - the dangerous-direction axis sits BESIDE the headline, never folded in;
//   - cost (total + per-case mean) and latency (mean + p95) beside the score;
//   - captioned with the generation it scored (gen-N);
//   - sorted by headline, with rows within HeadlineTie grouped so a human compares
//     their dangerous/cost/latency directly (q6 "the dangerous axis is a tiebreak");
//   - the saturation rule of question 5 evaluated and printed as an advisory;
//   - the retrieval side-by-side (lexical-only vs hybrid) as its OWN table;
//   - the chosen config recorded back as Part I's default — the feedback loop.

// SaturationThresholds are the question-5 knobs (defaults 0.95 / 0.02 / k=3), made
// config so a noisier-judged site can loosen them. A generation is saturated when
// BOTH the ceiling and the no-separation rules hold.
type SaturationThresholds struct {
	Ceiling    float64 // best headline must be ≥ this fraction of the achievable max (default 0.95)
	Separation float64 // spread best..k-th headline must be ≤ this (default 0.02)
	TopK       int     // the k in "best vs k-th config" (default min(3, n))
}

// DefaultSaturation returns the question-5 defaults.
func DefaultSaturation() SaturationThresholds {
	return SaturationThresholds{Ceiling: 0.95, Separation: 0.02, TopK: 3}
}

// ScoredRow is one config's AGGREGATE over a generation's cases — the q6 table row.
// It carries the config (model/effort), the per-case-mean headline, the
// dangerous-direction axis (mean rate per named axis, kept separate from headline),
// the supporting metrics, and cost/latency/coverage. It is what a human ranks.
type ScoredRow struct {
	Model        string
	Effort       string
	Headline     float64            // mean headline over the config's cases [0,1]
	Dangerous    map[string]float64 // mean rate per named dangerous axis (NEVER folded into Headline)
	Metrics      map[string]float64 // mean of each supporting metric over the cases
	Cases        int                // n cases scored
	Cached       int                // n served from cache (zero paid calls)
	TotalCostUSD float64
	MeanCostUSD  float64
	MeanLatency  float64
	P95Latency   int64
	ScoreErrs    int // n cases whose scoring recorded a non-fatal problem (unparseable, etc.)
}

// Report is one generation's scored comparison for one site — the q6 deliverable.
type Report struct {
	Generation int
	Site       string
	Rows       []ScoredRow // sorted by headline desc (the q6 default), ties grouped
	Saturation SaturationVerdict
	Thresholds SaturationThresholds
}

// SaturationVerdict is the question-5 outcome the harness PRINTS (a human decides).
type SaturationVerdict struct {
	Saturated   bool
	Ceiling     bool    // the best headline cleared the ceiling rule
	NoSeparation bool   // best..k-th spread within Separation AND dangerous indistinguishable
	BestHeadline float64
	Spread      float64 // best minus k-th headline
	Reason      string  // human-readable explanation of the verdict
}

// BuildReport scores every CaseResult against its case gold via the site's scorer,
// aggregates per config, sorts by headline, and evaluates the saturation rule. The
// caller supplies the per-case golds keyed by case id (the dataset the results were
// produced from), the site's scorer (from ScorerFor), and the saturation knobs. A
// scorer's dangerous axis is averaged, NEVER folded into the headline.
func BuildReport(ctx context.Context, generation int, site string, results []CaseResult, golds map[string][]byte, scorer Scorer, th SaturationThresholds) Report {
	if th.Ceiling == 0 && th.Separation == 0 && th.TopK == 0 {
		th = DefaultSaturation()
	}

	type agg struct {
		cases        int
		cached       int
		headlineSum  float64
		dangerSum    map[string]float64
		metricSum    map[string]float64
		totalCost    float64
		latencies    []int64
		scoreErrs    int
	}
	order := []string{}
	byConfig := map[string]*agg{}

	for _, r := range results {
		gold := golds[r.CaseID]
		sc := scorer.Score(ctx, r.Output, gold)

		k := r.Model + "\x00" + r.Effort
		a := byConfig[k]
		if a == nil {
			a = &agg{dangerSum: map[string]float64{}, metricSum: map[string]float64{}}
			byConfig[k] = a
			order = append(order, k)
		}
		a.cases++
		if r.Cached {
			a.cached++
		}
		a.headlineSum += sc.Headline
		for name, v := range sc.Dangerous {
			a.dangerSum[name] += v
		}
		for name, v := range sc.Metrics {
			a.metricSum[name] += v
		}
		a.totalCost += r.CostUSD
		a.latencies = append(a.latencies, r.LatencyMS)
		if len(sc.Errs) > 0 {
			a.scoreErrs++
		}
	}

	rep := Report{Generation: generation, Site: site, Thresholds: th}
	for _, k := range order {
		a := byConfig[k]
		parts := strings.SplitN(k, "\x00", 2)
		row := ScoredRow{
			Model:        parts[0],
			Effort:       parts[1],
			Dangerous:    map[string]float64{},
			Metrics:      map[string]float64{},
			Cases:        a.cases,
			Cached:       a.cached,
			TotalCostUSD: a.totalCost,
			ScoreErrs:    a.scoreErrs,
		}
		if a.cases > 0 {
			row.Headline = a.headlineSum / float64(a.cases)
			for name, sum := range a.dangerSum {
				row.Dangerous[name] = sum / float64(a.cases)
			}
			for name, sum := range a.metricSum {
				row.Metrics[name] = sum / float64(a.cases)
			}
			row.MeanCostUSD = a.totalCost / float64(a.cases)
			var s int64
			for _, l := range a.latencies {
				s += l
			}
			row.MeanLatency = float64(s) / float64(a.cases)
			row.P95Latency = p95(a.latencies)
		}
		rep.Rows = append(rep.Rows, row)
	}

	// q6 default sort: headline DESC. Within a HeadlineTie band the renderer groups
	// them; the sort keeps them adjacent and orders the band by ascending total
	// dangerous (the safer config rises) then ascending cost — a stable, deterministic
	// presentation, NEVER a single composite rank that hides the tradeoff.
	sort.SliceStable(rep.Rows, func(i, j int) bool {
		ri, rj := rep.Rows[i], rep.Rows[j]
		if abs(ri.Headline-rj.Headline) > th.Separation {
			return ri.Headline > rj.Headline
		}
		di, dj := totalDanger(ri.Dangerous), totalDanger(rj.Dangerous)
		if di != dj {
			return di < dj
		}
		return ri.TotalCostUSD < rj.TotalCostUSD
	})

	rep.Saturation = evalSaturation(rep.Rows, th)
	return rep
}

// totalDanger sums a row's dangerous-axis rates — used ONLY as a deterministic
// within-tie ordering, never shown as a score (the dangerous axes stay named and
// separate in the table).
func totalDanger(d map[string]float64) float64 {
	var s float64
	for _, v := range d {
		s += v
	}
	return s
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// evalSaturation applies the question-5 two-part rule over the already-sorted rows.
// Ceiling: best headline ≥ th.Ceiling (headlines are already normalized to [0,1],
// so the achievable max is 1). No-separation: the spread best..k-th ≤ th.Separation
// AND their dangerous axes are indistinguishable (all within one case — here, the
// per-config dangerous rates differ by ≤ one case's worth). Both ⇒ saturated.
func evalSaturation(rows []ScoredRow, th SaturationThresholds) SaturationVerdict {
	v := SaturationVerdict{}
	if len(rows) == 0 {
		v.Reason = "no configs scored"
		return v
	}
	best := rows[0]
	v.BestHeadline = best.Headline
	v.Ceiling = best.Headline >= th.Ceiling

	k := th.TopK
	if k <= 0 || k > len(rows) {
		k = min(3, len(rows))
	}
	if k > len(rows) {
		k = len(rows)
	}
	kth := rows[k-1]
	v.Spread = best.Headline - kth.Headline

	// Dangerous indistinguishable: every dangerous axis differs by ≤ one case's
	// worth across the top-k band. With per-case rates in [0,1] and n cases, "one
	// case" is 1/n; we approximate conservatively with ≤ the separation band on each
	// axis (all-zero is the common pass), which the q6 rule allows the harness to
	// report and a human to confirm.
	dangerClose := true
	axes := map[string]bool{}
	for _, r := range rows[:k] {
		for a := range r.Dangerous {
			axes[a] = true
		}
	}
	for a := range axes {
		var lo, hi float64
		lo, hi = rows[0].Dangerous[a], rows[0].Dangerous[a]
		for _, r := range rows[:k] {
			x := r.Dangerous[a]
			if x < lo {
				lo = x
			}
			if x > hi {
				hi = x
			}
		}
		caseWorth := 0.0
		if best.Cases > 0 {
			caseWorth = 1.0 / float64(best.Cases)
		}
		if hi-lo > caseWorth+1e-9 {
			dangerClose = false
			break
		}
	}
	v.NoSeparation = v.Spread <= th.Separation && dangerClose

	v.Saturated = v.Ceiling && v.NoSeparation
	switch {
	case v.Saturated:
		v.Reason = fmt.Sprintf("best headline %.3f ≥ %.2f and top-%d spread %.3f ≤ %.2f with dangerous axes indistinguishable", best.Headline, th.Ceiling, k, v.Spread, th.Separation)
	case !v.Ceiling:
		v.Reason = fmt.Sprintf("best headline %.3f below ceiling %.2f — generation still has headroom", best.Headline, th.Ceiling)
	default:
		v.Reason = fmt.Sprintf("top-%d still separate (spread %.3f or dangerous axes differ) — gen still discriminates", k, v.Spread)
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Render returns the q6 comparison table as text. One row per config, captioned
// with the generation, dangerous axis beside the headline (never folded), cost +
// latency + coverage beside, then the saturation advisory. Rows within a headline
// tie band are visually grouped with a marker so a human compares their
// dangerous/cost/latency directly (q6 tiebreak rule).
func (r Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "REPORT  site=%s  generation=gen-%d\n", r.Site, r.Generation)
	fmt.Fprintf(&b, "(one row per config — rank on the axis you care about; NO single composite rank)\n\n")

	// Collect the union of dangerous-axis names (stable order) so the columns are
	// consistent across rows.
	dangerNames := r.dangerAxes()

	// Header.
	fmt.Fprintf(&b, "%-22s %-8s %9s", "model", "effort", "headline")
	for _, d := range dangerNames {
		fmt.Fprintf(&b, " %14s", short(d))
	}
	fmt.Fprintf(&b, " %12s %12s %9s %8s %9s\n",
		"total_cost", "mean_cost", "mean_ms", "p95_ms", "cases/cch")

	var prev float64
	for i, row := range r.Rows {
		grouped := i > 0 && abs(row.Headline-prev) <= r.Thresholds.Separation
		marker := "  "
		if grouped {
			marker = "↳ " // grouped into the preceding headline-tie band (compare directly)
		}
		fmt.Fprintf(&b, "%s%-20s %-8s %9.3f", marker, row.Model, row.Effort, row.Headline)
		for _, d := range dangerNames {
			fmt.Fprintf(&b, " %14.3f", row.Dangerous[d])
		}
		fmt.Fprintf(&b, " %12.6f %12.6f %9.1f %8d %4d/%-4d\n",
			row.TotalCostUSD, row.MeanCostUSD, row.MeanLatency, row.P95Latency, row.Cases, row.Cached)
		prev = row.Headline
	}

	// Saturation advisory (question 5) — printed, never auto-acted-on.
	fmt.Fprintf(&b, "\n")
	if r.Saturation.Saturated {
		fmt.Fprintf(&b, "SATURATED — mint gen-%d  (%s)\n", r.Generation+1, r.Saturation.Reason)
	} else {
		fmt.Fprintf(&b, "not saturated  (%s)\n", r.Saturation.Reason)
	}
	return b.String()
}

// dangerAxes returns the stable union of dangerous-axis names across the report's
// rows (sorted for deterministic columns).
func (r Report) dangerAxes() []string {
	seen := map[string]bool{}
	for _, row := range r.Rows {
		for d := range row.Dangerous {
			seen[d] = true
		}
	}
	var out []string
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// short truncates a long axis name to fit the column header.
func short(s string) string {
	if len(s) <= 14 {
		return s
	}
	return s[:13] + "…"
}

// --- The retrieval side-by-side (its own table — q6 + research §8–10) ---

// RetrievalMode is one retriever configuration in the lexical-vs-hybrid comparison.
type RetrievalMode struct {
	Name    string  // "lexical" | "hybrid"
	Recall  float64 // mean recall@k for the mode over the cases
	NDCG    float64 // mean nDCG@k where the site reports it (search); 0 otherwise
	CostUSD float64 // mean per-case cost (embeddings make hybrid non-free)
	Missed  float64 // mean dangerous-axis miss count (a missed candidate mints a dup)
}

// RetrievalSideBySide is the per-site lexical-only-vs-hybrid comparison — the
// deliverable that LICENSES or DECLINES the vector lane at each retrieval site
// (research §8–10). It is its OWN table (q6), reporting recall LIFT vs cost so the
// vector lane is a measured decision, not a default-on.
type RetrievalSideBySide struct {
	Site    string
	Lexical RetrievalMode
	Hybrid  RetrievalMode
}

// BuildRetrievalMode scores a retrieval lane's results for one mode (lexical or
// hybrid) and aggregates recall / nDCG / cost / the missed-relevant dangerous axis
// over the cases. The scorer is one of the retrieval scorers (candidates / search /
// sweep). dangerAxis names the site's miss counter so the mean miss is read off the
// right key.
func BuildRetrievalMode(ctx context.Context, name string, results []CaseResult, golds map[string][]byte, scorer Scorer, dangerAxis string) RetrievalMode {
	m := RetrievalMode{Name: name}
	if len(results) == 0 {
		return m
	}
	var recall, ndcg, cost, missed float64
	for _, r := range results {
		sc := scorer.Score(ctx, r.Output, golds[r.CaseID])
		recall += sc.Headline // retrieval headline IS recall@k (eval design q5)
		ndcg += sc.Metrics["ndcg_at_k"]
		missed += sc.Dangerous[dangerAxis]
		cost += r.CostUSD
	}
	n := float64(len(results))
	m.Recall = recall / n
	m.NDCG = ndcg / n
	m.CostUSD = cost / n
	m.Missed = missed / n
	return m
}

// RecallLift is hybrid recall minus lexical recall — the number the vector-lane
// decision turns on (positive lift may not justify the embedding cost).
func (s RetrievalSideBySide) RecallLift() float64 { return s.Hybrid.Recall - s.Lexical.Recall }

// CostDelta is the per-case cost the hybrid lane adds (embeddings).
func (s RetrievalSideBySide) CostDelta() float64 { return s.Hybrid.CostUSD - s.Lexical.CostUSD }

// Render renders the retrieval side-by-side as its own table.
func (s RetrievalSideBySide) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "RETRIEVAL SIDE-BY-SIDE  site=%s  (vector lane: licensed by recall lift vs cost — research §8–10)\n", s.Site)
	fmt.Fprintf(&b, "%-10s %9s %9s %12s %10s\n", "mode", "recall@k", "ndcg@k", "mean_cost", "missed")
	for _, m := range []RetrievalMode{s.Lexical, s.Hybrid} {
		fmt.Fprintf(&b, "%-10s %9.3f %9.3f %12.6f %10.3f\n", m.Name, m.Recall, m.NDCG, m.CostUSD, m.Missed)
	}
	fmt.Fprintf(&b, "recall lift (hybrid−lexical): %+.3f   cost delta: %+.6f/case  →  %s\n",
		s.RecallLift(), s.CostDelta(), s.laneVerdict())
	return b.String()
}

// laneVerdict states the licensing call the table supports (a human still decides):
// a positive recall lift licenses the lane; a flat/negative lift declines it.
func (s RetrievalSideBySide) laneVerdict() string {
	if s.RecallLift() > 0 {
		return fmt.Sprintf("hybrid lifts recall by %.3f — vector lane licensed (weigh vs %+.6f/case)", s.RecallLift(), s.CostDelta())
	}
	return "no recall lift — vector lane DECLINED at this site (lexical-only)"
}

// --- The feedback loop: the chosen config recorded back as a Part I default ---

// ChosenConfig names the (prompt, model, effort) a human picked for a site from the
// report, plus any deferred knobs — the feedback-loop record P16d ships to int (eval
// design q6 "the chosen config is recorded back as Part I's config default").
// NOTHING here auto-promotes; this is the human's recorded pick.
type ChosenConfig struct {
	Site          string
	Generation    int
	Model         string
	Effort        string
	PromptVersion string            // the bundle's prompt artifact (or "config-default")
	Knobs         map[string]string // any deferred-knob picks (WIKI_MATCH_EXCERPT_CHARS, RRF k, …)
	Rationale     string            // the axis the human ranked on (the worked "pick a config" reasoning)
}

// Render renders one chosen-config record — the line P16d turns into a config
// default. It is the documented form of "a human reads the matrix and picks."
func (c ChosenConfig) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "CHOSEN (feedback loop → P16d):  site=%s  gen-%d\n", c.Site, c.Generation)
	fmt.Fprintf(&b, "  model=%s  effort=%s  prompt=%s\n", c.Model, c.Effort, nonEmpty(c.PromptVersion, "config-default"))
	if len(c.Knobs) > 0 {
		keys := make([]string, 0, len(c.Knobs))
		for k := range c.Knobs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  knob %s=%s\n", k, c.Knobs[k])
		}
	}
	if c.Rationale != "" {
		fmt.Fprintf(&b, "  rationale: %s\n", c.Rationale)
	}
	return b.String()
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

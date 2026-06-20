// Command wiki-eval is the offline evaluation harness (plan P13, design
// docs/wiki-evaluation-design.md). It is Part II's measurement tool, NOT a build /
// CI / deploy gate — every number it prints is an input to a human decision
// (eval design "What this is not"). It injects a (prompt, model, effort) triple
// into the REAL Part I call-site functions (the production-code-path principle),
// captures the raw output plus cost + latency, memoizes on a content-hash cache so
// a re-run with an unchanged (dataset, prompt, model, effort) makes zero paid
// calls, and renders a config × metric table per generation.
//
// It lives under the wiki module (cmd/wiki-eval) because the harness must import
// the module-internal call sites it scores — Go forbids reaching internal/ from a
// sibling module. The design's "bin/wiki-eval, wired into go.work" intent is met:
// a standalone Go main in the suite, off-box, paid, networked, and honestly NOT a
// go-test target.
//
// P13 wires ONE site end-to-end to prove the rig — Match (the shared identity
// corpus's headline consumer). P14 adds the scorer library; P15 the per-site
// generators; P16 the full sweep + report.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentkit/model"
	"agentkit/provider/anthropic"
	"agentkit/provider/openai"

	"wiki/internal/config"
	"wiki/internal/eval"
	"wiki/internal/llm"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wiki-eval:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("wiki-eval", flag.ContinueOnError)
	site := fs.String("site", "match", "the inference site to evaluate (P13 supports: match)")
	testsets := fs.String("testsets", "testsets", "root of the test-set bundle tree")
	bundle := fs.String("bundle", "bundles/gen-1.json", "the bundle (site-relative) naming a dataset + prompt")
	sweep := fs.String("sweep", "claude-haiku-4-5", "comma-separated model[:effort] sweep points")
	cacheDir := fs.String("cache", ".wiki-eval-cache", "the content-addressed output cache dir")
	timeout := fs.Duration("timeout", 90*time.Second, "per-call timeout")
	report := fs.Bool("report", false, "score the sweep and render the P16 comparison report (scores beside cost/latency, dangerous axis separate, saturation advisory, a worked pick-a-config example)")
	retrievalK := fs.Int("k", 10, "shortlist depth k for retrieval-site scorers (the config knob the sweep tunes)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *site != "match" {
		return fmt.Errorf("P13 wires only the match site end-to-end; %q is not yet adapted (P14+)", *site)
	}

	grid, err := parseSweep(*sweep)
	if err != nil {
		return err
	}

	siteDir := filepath.Join(*testsets, *site)
	bundlePath := filepath.Join(siteDir, *bundle)
	b, ds, dsBytes, promptBytes, err := eval.LoadBundle(siteDir, bundlePath)
	if err != nil {
		return err
	}

	cache, err := eval.NewCache(*cacheDir)
	if err != nil {
		return err
	}

	// The live wrapper drives the REAL call site. The capture handler pulls cost +
	// latency off P0c's per-call accounting record (one logger, no second timing
	// path); a key absent for a provider only fails if the sweep actually resolves
	// to that provider.
	cap := eval.NewCaptureHandler()
	wrapper := llm.New(liveClientFactory(), cap.Logger())

	adapter := eval.NewMatchAdapter(wrapper, defaultMatchExcerptChars())
	runner := eval.NewRunner(*site, adapter.Run, cache, cap, dsBytes, promptBytes, resolvePrompt(b, promptBytes))

	ctx, cancel := context.WithTimeout(context.Background(), perRunBudget(*timeout, ds, grid))
	defer cancel()

	results, err := runner.Run(ctx, ds, grid)
	if err != nil {
		return err
	}

	// Per-case gold map (the dataset's golds, by case id) — the report scores each
	// raw result against its gold via the site's P14 scorer.
	golds := map[string][]byte{}
	caseGen := map[string]int{}
	for _, c := range ds.Cases {
		golds[c.CaseID] = c.Gold
		caseGen[c.CaseID] = c.Generation
	}

	// Group per generation (the eval-design outer sweep dimension). A
	// single-generation bundle (gen-1) renders one table/report.
	for _, gen := range generations(ds) {
		var sub []eval.CaseResult
		for _, r := range results {
			if caseGen[r.CaseID] == gen {
				sub = append(sub, r)
			}
		}
		if *report {
			if err := renderReport(gen, *site, sub, golds, *retrievalK, b, promptBytes); err != nil {
				return err
			}
		} else {
			fmt.Print(eval.BuildTable(gen, *site, sub).Render())
		}
	}

	// A small operator summary: total paid calls vs cache hits (the P13 Verify
	// "reproduces a second run entirely from cache" signal).
	paid, cached := 0, 0
	for _, r := range results {
		if r.Cached {
			cached++
		} else {
			paid++
		}
	}
	fmt.Printf("\n%d configs × %d cases → %d paid calls, %d cache hits\n", len(grid), countSiteCases(ds, *site), paid, cached)
	return nil
}

// renderReport scores a generation's raw results against their golds (via the P14
// scorer for the site) and renders the P16 comparison report: the q6 table (headline
// + dangerous axis separate + cost + latency + coverage, sorted with the tie band
// grouped), the saturation advisory (question 5), the retrieval side-by-side for a
// retrieval lane, and a worked "pick a config" example that closes the feedback loop
// into Part I's defaults (eval design q6). It is NOT a gate — every number is an
// input to the human's pick.
func renderReport(gen int, site string, sub []eval.CaseResult, golds map[string][]byte, k int, b *eval.Bundle, promptBytes []byte) error {
	scorer, err := eval.ScorerFor(site, eval.StubJudge(), k)
	if err != nil {
		return err
	}
	ctx := context.Background()
	rep := eval.BuildReport(ctx, gen, site, sub, golds, scorer, eval.DefaultSaturation())
	fmt.Print(rep.Render())

	// The retrieval side-by-side is its OWN table (q6 / research §8–10): lexical-only
	// vs hybrid, recall lift vs cost — the deliverable that licenses or declines the
	// vector lane at this site. The harness has one wired retriever today (the swept
	// lane is "lexical"); the hybrid column lands when P11's vector adapter is wired,
	// at which point the same aggregator scores both modes. We still render the table
	// for a retrieval site so the licensing artifact exists and reads honestly.
	if isRetrievalSite(site) {
		danger := retrievalDangerAxis(site)
		lexical := eval.BuildRetrievalMode(ctx, "lexical", sub, golds, scorer, danger)
		// No hybrid lane wired yet (P11); the hybrid mode is the empty baseline, so the
		// table truthfully reports "no measured lift yet" rather than inventing one.
		sbs := eval.RetrievalSideBySide{Site: site, Lexical: lexical, Hybrid: eval.RetrievalMode{Name: "hybrid"}}
		fmt.Print("\n" + sbs.Render())
	}

	// The worked "pick a config" example (the feedback loop + the P16 Verify's
	// "a worked pick-a-config example"). A human ranks the rows; here we demonstrate
	// the documented default reasoning — pick the top row (best headline) whose
	// dangerous axes are all clear, the safer-of-the-tie-band the sort already floats
	// up — and record it as the config default P16d ships. This is illustrative of HOW
	// defaults get set; nothing auto-promotes.
	if len(rep.Rows) > 0 {
		pick := rep.Rows[0]
		chosen := eval.ChosenConfig{
			Site:          site,
			Generation:    gen,
			Model:         pick.Model,
			Effort:        pick.Effort,
			PromptVersion: promptVersion(b),
			Rationale: fmt.Sprintf("top headline %.3f with the lowest dangerous-axis total in its tie band; a human confirms the cost/latency tradeoff (%.6f USD/case, %.0f ms mean) before pinning",
				pick.Headline, pick.MeanCostUSD, pick.MeanLatency),
		}
		if isRetrievalSite(site) {
			chosen.Knobs = map[string]string{"k": fmt.Sprintf("%d", k)}
		}
		fmt.Print("\n" + chosen.Render())
	}
	return nil
}

func isRetrievalSite(site string) bool {
	switch site {
	case "candidates", "search", "sweep":
		return true
	}
	return false
}

func retrievalDangerAxis(site string) string {
	switch site {
	case "candidates":
		return "missed_candidate"
	case "search":
		return "missed_relevant"
	case "sweep":
		return "missed_pair"
	}
	return ""
}

func promptVersion(b *eval.Bundle) string {
	if b != nil && b.Prompt != "" {
		return b.Prompt
	}
	return "config-default"
}

// resolvePrompt returns the prompt string to inject: the bundle's prompt artifact
// when it names one, else the match call site's config default (the production
// prompt is just a prompt artifact — eval design q3).
func resolvePrompt(b *eval.Bundle, promptBytes []byte) string {
	if b.Prompt != "" && len(promptBytes) > 0 {
		return string(promptBytes)
	}
	return config.DefaultMatchPrompt
}

// liveClientFactory dispatches a resolved model to its provider backend, keyed
// purely on the resolved provider — the same composition-root pattern cmd/wiki
// uses. A key is read only when a sweep point resolves to that provider.
func liveClientFactory() llm.ClientFactory {
	return func(r model.Resolved) (llm.Client, error) {
		switch r.Provider {
		case model.ProviderOpenAI:
			return openai.New(os.Getenv("OPENAI_API_KEY"), r.BareID)
		default:
			return anthropic.New(os.Getenv("ANTHROPIC_API_KEY"), r.BareID)
		}
	}
}

// parseSweep parses "model[:effort],model[:effort]" into the model × effort grid.
func parseSweep(s string) ([]eval.ModelEffort, error) {
	var grid []eval.ModelEffort
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		me := eval.ModelEffort{}
		if i := strings.IndexByte(part, ':'); i >= 0 {
			me.Model, me.Effort = part[:i], part[i+1:]
		} else {
			me.Model = part
		}
		grid = append(grid, me)
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("empty sweep spec")
	}
	return grid, nil
}

func generations(ds *eval.Dataset) []int {
	seen := map[int]bool{}
	var out []int
	for _, c := range ds.Cases {
		if !seen[c.Generation] {
			seen[c.Generation] = true
			out = append(out, c.Generation)
		}
	}
	return out
}

func countSiteCases(ds *eval.Dataset, site string) int {
	n := 0
	for _, c := range ds.Cases {
		if c.Site == site {
			n++
		}
	}
	return n
}

// perRunBudget gives the whole sweep a generous ceiling derived from the per-call
// timeout × the grid × the cases, so a large sweep does not trip a single per-call
// deadline. Individual calls are still bounded by the design's own ask budgets.
func perRunBudget(perCall time.Duration, ds *eval.Dataset, grid []eval.ModelEffort) time.Duration {
	n := len(grid) * len(ds.Cases)
	if n < 1 {
		n = 1
	}
	return perCall * time.Duration(n)
}

func defaultMatchExcerptChars() int { return 600 }

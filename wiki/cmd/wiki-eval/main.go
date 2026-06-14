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

	// Group + render per generation (the eval-design outer sweep dimension). A
	// single-generation bundle (gen-1) renders one table.
	for _, gen := range generations(ds) {
		var sub []eval.CaseResult
		caseGen := map[string]int{}
		for _, c := range ds.Cases {
			caseGen[c.CaseID] = c.Generation
		}
		for _, r := range results {
			if caseGen[r.CaseID] == gen {
				sub = append(sub, r)
			}
		}
		fmt.Print(eval.BuildTable(gen, *site, sub).Render())
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

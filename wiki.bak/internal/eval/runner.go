package eval

import (
	"context"
	"encoding/json"
	"fmt"

	"agentkit/model"

	"wiki/internal/config"
)

// ModelEffort is one config point on the sweep's model × effort grid.
type ModelEffort struct {
	Model  string
	Effort string
}

// Config validates that the model resolves and accepts the effort (the same
// structural validation the serve boundary applies), so a mis-specified sweep
// point fails before any paid call.
func (me ModelEffort) Validate() error {
	r, err := model.Resolve(me.Model)
	if err != nil {
		return err
	}
	if err := model.Validate(r); err != nil {
		return err
	}
	if me.Effort != "" {
		if err := model.ValidateEffort(r, me.Effort); err != nil {
			return err
		}
	}
	return nil
}

// CallFn injects a resolved triple into a real call site and returns the raw
// output. The runner builds it from a SiteAdapter; isolating it as a func makes
// the runner unit-testable with a fake (no provider, no network) while production
// drives the real adapter.
type CallFn func(ctx context.Context, site config.CallSite, input json.RawMessage) (json.RawMessage, error)

// Runner sweeps a dataset over a model × effort grid, injecting each triple into
// the real call site (via call), capturing the raw output + cost + latency, and
// memoizing on the content-hash cache so a re-run with an unchanged
// (dataset, prompt, model, effort) costs zero provider calls.
type Runner struct {
	site        string          // the registry site name (e.g. "match")
	call        CallFn          // injects the triple into the real call site
	cache       *Cache          // the output cache
	cap         *CaptureHandler // pulls cost/latency off P0c's accounting record
	datasetHash string          // content hash of the dataset bytes (cache key part)
	promptHash  string          // content hash of the prompt artifact (cache key part)
	prompt      string          // the resolved prompt to inject (bundle prompt or config default)
}

// NewRunner builds a runner for one site against one pinned (dataset, prompt)
// bundle. datasetBytes/promptBytes are the artifact bytes (their content hashes
// pin the run and key the cache, eval design q3/q4); prompt is the resolved
// prompt string to inject (the promptBytes decoded, or the site's config default
// when the bundle names no prompt). cap is the capture handler whose logger is
// already attached to the wrapper the call closes over.
func NewRunner(site string, call CallFn, cache *Cache, cap *CaptureHandler, datasetBytes, promptBytes []byte, prompt string) *Runner {
	return &Runner{
		site:        site,
		call:        call,
		cache:       cache,
		cap:         cap,
		datasetHash: HashBytes(datasetBytes),
		promptHash:  HashBytes(promptBytes),
		prompt:      prompt,
	}
}

// CaseResult is one (case × config) raw outcome the scorer library (P14) will
// score. P13 captures the raw output, cost, and latency; scoring is P14's job.
type CaseResult struct {
	CaseID     string
	FailureTag string
	Model      string
	Effort     string
	Output     json.RawMessage
	CostUSD    float64
	LatencyMS  int64
	Cached     bool // true when served from cache (zero paid calls)
}

// Run sweeps the dataset's cases for this site over the grid. For each
// (case, config) it consults the cache; a miss runs the real call site with the
// injected triple, captures cost/latency, and stores the raw output. The results
// table (config × case) is returned for rendering/scoring.
func (r *Runner) Run(ctx context.Context, ds *Dataset, grid []ModelEffort) ([]CaseResult, error) {
	var out []CaseResult
	for _, me := range grid {
		if err := me.Validate(); err != nil {
			return nil, fmt.Errorf("eval: invalid sweep config %s/%s: %w", me.Model, me.Effort, err)
		}
		for _, c := range ds.Cases {
			if c.Site != r.site {
				continue // a dataset may, in principle, mix sites; run only this one.
			}
			res, err := r.runCase(ctx, c, me)
			if err != nil {
				return nil, err
			}
			out = append(out, res)
		}
	}
	return out, nil
}

func (r *Runner) runCase(ctx context.Context, c Case, me ModelEffort) (CaseResult, error) {
	key := CacheKey{
		DatasetHash: r.datasetHash,
		CaseID:      c.CaseID,
		PromptHash:  r.promptHash,
		Model:       me.Model,
		Effort:      me.Effort,
	}
	if cached, err := r.cache.Get(key); err == nil {
		return CaseResult{
			CaseID:     c.CaseID,
			FailureTag: c.FailureTag,
			Model:      me.Model,
			Effort:     me.Effort,
			Output:     cached.Raw,
			CostUSD:    cached.CostUSD,
			LatencyMS:  cached.LatencyMS,
			Cached:     true,
		}, nil
	}

	site := config.CallSite{
		Name:   r.site,
		Prompt: r.prompt,
		Model:  me.Model,
		Effort: me.Effort,
	}

	r.cap.Reset()
	raw, err := r.call(ctx, site, c.Input)
	if err != nil {
		return CaseResult{}, fmt.Errorf("eval: case %s config %s/%s: %w", c.CaseID, me.Model, me.Effort, err)
	}
	cost, dur, _ := r.cap.Result()

	if err := r.cache.Put(key, CachedOutput{Raw: raw, CostUSD: cost, LatencyMS: dur}); err != nil {
		return CaseResult{}, err
	}
	return CaseResult{
		CaseID:     c.CaseID,
		FailureTag: c.FailureTag,
		Model:      me.Model,
		Effort:     me.Effort,
		Output:     raw,
		CostUSD:    cost,
		LatencyMS:  dur,
		Cached:     false,
	}, nil
}

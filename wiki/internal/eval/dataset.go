// Package eval is the offline evaluation harness's rig (plan P13, design
// docs/wiki-evaluation-design.md). It is Part II's measurement tool: it injects a
// (prompt, model, effort) triple into the REAL Part I call-site functions (never a
// reimplementation — the research doc's production-code-path principle), captures
// the raw output plus cost and latency, caches the raw output keyed on
// (test-set, case, prompt, model, effort) so a re-score costs zero provider calls,
// and renders a config × metric results table per generation.
//
// The harness is NOT a build gate, a CI gate, or a deploy gate — every score is an
// input to a human decision (eval design "What this is not"). It lives inside the
// wiki module because it must import the module-internal call sites it scores; the
// command is wiki/cmd/wiki-eval.
//
// P13 builds the rig once and proves it on ONE site — Match (its identity corpus
// is the headline case shared by three sites). P14 adds the scorer library, P15
// the per-site generators, P16 the full sweep + report.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
)

// Case is one dataset record (eval design "Dataset record format"). The
// input/gold objects are site-polymorphic: the loader returns them as
// json.RawMessage and each site adapter unmarshals into its own typed shape, so
// adding a site never reshapes the record.
type Case struct {
	// CaseID is a stable unique id within the dataset (e.g. "match-0007"). Survives
	// across generations so a case can be tracked / refreshed.
	CaseID string `json:"case_id"`
	// Site is the inference site this case exercises — one of the ten registry
	// names. Ties the case to a scorer kind.
	Site string `json:"site"`
	// Generation is the 1-based numbered generation this case belongs to (the outer
	// sweep dimension).
	Generation int `json:"generation"`
	// FailureTag is the dangerous-direction behaviour the case stresses, drawn from
	// a per-site enumerated vocabulary. NOT a difficulty label.
	FailureTag string `json:"failure_tag"`
	// Input is the exact, byte-identical input the real call-site function
	// consumes — site-shaped, stored verbatim.
	Input json.RawMessage `json:"input"`
	// Gold is the reference the scorer aligns against — site-shaped.
	Gold json.RawMessage `json:"gold"`
}

// Dataset is a list of cases — one on-disk JSON file (eval design "Test-set
// storage").
type Dataset struct {
	Cases []Case
}

// Bundle names one dataset artifact + one prompt artifact (eval design question
// 3). A bundle is the unit a run pins by content hash so a result stays
// attributable after the set is superseded. Paths are relative to the bundle
// file's directory's parent (the site dir), matching the testsets/<site>/ layout.
type Bundle struct {
	// Dataset is the path to the dataset file, relative to the site dir
	// (e.g. "datasets/gen-1.json").
	Dataset string `json:"dataset"`
	// Prompt is the path to the prompt file, relative to the site dir
	// (e.g. "prompts/v1.txt"). May be empty to use the call site's config-default
	// prompt (the production prompt is just a prompt artifact — eval design q3).
	Prompt string `json:"prompt"`
}

// LoadDataset reads and decodes a dataset JSON file. The file is either a bare
// JSON array of cases or an object with a "cases" array — both are accepted so a
// generator may emit either shape.
func LoadDataset(path string) (*Dataset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval: read dataset %q: %w", path, err)
	}
	// Try the bare-array form first (the canonical on-disk shape).
	var cases []Case
	if err := json.Unmarshal(raw, &cases); err == nil {
		return &Dataset{Cases: cases}, nil
	}
	// Fall back to the {"cases": [...]} object form.
	var obj struct {
		Cases []Case `json:"cases"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("eval: decode dataset %q: %w", path, err)
	}
	return &Dataset{Cases: obj.Cases}, nil
}

// LoadBundle reads a bundle file and returns it alongside the dataset and prompt
// bytes it names (resolved relative to siteDir). The prompt bytes are nil when the
// bundle names no prompt (the call site's config default is then used). The raw
// dataset and prompt bytes are returned so the caller can content-hash them for
// the cache key and run-to-bundle pinning (eval design q3/q4).
func LoadBundle(siteDir, bundlePath string) (*Bundle, *Dataset, []byte, []byte, error) {
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("eval: read bundle %q: %w", bundlePath, err)
	}
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("eval: decode bundle %q: %w", bundlePath, err)
	}

	dsPath := joinSiteRel(siteDir, b.Dataset)
	dsBytes, err := os.ReadFile(dsPath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("eval: read bundle dataset %q: %w", dsPath, err)
	}
	ds, err := LoadDataset(dsPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	var promptBytes []byte
	if b.Prompt != "" {
		pPath := joinSiteRel(siteDir, b.Prompt)
		promptBytes, err = os.ReadFile(pPath)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("eval: read bundle prompt %q: %w", pPath, err)
		}
	}
	return &b, ds, dsBytes, promptBytes, nil
}

// joinSiteRel joins a site-relative artifact path under the site dir.
func joinSiteRel(siteDir, rel string) string {
	if siteDir == "" {
		return rel
	}
	return siteDir + string(os.PathSeparator) + rel
}

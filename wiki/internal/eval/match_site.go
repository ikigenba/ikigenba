package eval

import (
	"context"
	"encoding/json"
	"fmt"

	"wiki/internal/config"
	"wiki/internal/integrate"
	"wiki/internal/llm"
	"wiki/internal/page"
)

// matchInput is the Match site's case-input shape (the eval design's site-shaped
// `input`): the incoming subject plus the candidate shortlist with each
// candidate's match excerpt evidence. It is stored verbatim in the dataset so the
// harness feeds the real call site byte-identical input.
type matchInput struct {
	Incoming struct {
		Type    string   `json:"type"`
		Kind    string   `json:"kind"`
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
		Claims  []struct {
			Text  string   `json:"text"`
			Cites []string `json:"cites"`
		} `json:"claims"`
	} `json:"incoming"`
	Candidates []struct {
		SubjectID     string   `json:"subject_id"`
		Type          string   `json:"type"`
		CanonicalName string   `json:"canonical_name"`
		Aliases       []string `json:"aliases"`
		Body          string   `json:"body"`
	} `json:"candidates"`
}

// matchOutput is the Match site's raw-output shape cached and later scored (P14):
// the binary verdict's matched id (empty for no_match) plus the dup_pairs side
// channel — kept as DISTINCT outputs per eval obligation 3, never lumped.
type matchOutput struct {
	Same     string `json:"same"`
	DupPairs []struct {
		A string `json:"a"`
		B string `json:"b"`
	} `json:"dup_pairs"`
}

// fixedExcerptReader serves the case's in-line candidate excerpts to the real
// match stage so the rig needs no DB — the evidence the model judges is exactly
// what the dataset case pins. This is NOT a reimplementation of match: match's own
// renderMatchEvidence + ParseMatch + the live structured call all run unchanged.
type fixedExcerptReader struct {
	ex map[string]page.Excerpt
}

func (r fixedExcerptReader) ReadExcerpt(_ context.Context, id string, _ int) (page.Excerpt, error) {
	return r.ex[id], nil
}

// MatchAdapter scores the Match call site (P13's proof site). It builds the real
// integrate.Matcher over the live llm wrapper and the case-pinned excerpts, then
// calls Matcher.Match — the same function production runs.
type MatchAdapter struct {
	wrapper      *llm.Wrapper
	excerptChars int
}

// NewMatchAdapter builds the Match adapter over a live llm wrapper (with its
// client factory + accounting logger) and the config-injected excerpt length.
func NewMatchAdapter(w *llm.Wrapper, excerptChars int) *MatchAdapter {
	return &MatchAdapter{wrapper: w, excerptChars: excerptChars}
}

func (a *MatchAdapter) Name() string { return "match" }

func (a *MatchAdapter) Run(ctx context.Context, site config.CallSite, input json.RawMessage) (json.RawMessage, error) {
	var in matchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("eval match: decode input: %w", err)
	}

	ex := make(map[string]page.Excerpt, len(in.Candidates))
	cands := make([]page.Candidate, 0, len(in.Candidates))
	for _, c := range in.Candidates {
		ex[c.SubjectID] = page.Excerpt{
			SubjectID:     c.SubjectID,
			CanonicalName: c.CanonicalName,
			Aliases:       c.Aliases,
			Body:          c.Body,
		}
		cands = append(cands, page.Candidate{
			SubjectID:     c.SubjectID,
			Type:          c.Type,
			CanonicalName: c.CanonicalName,
		})
	}

	subj := integrate.Subject{
		Type:    in.Incoming.Type,
		Kind:    in.Incoming.Kind,
		Name:    in.Incoming.Name,
		Aliases: in.Incoming.Aliases,
	}
	for _, cl := range in.Incoming.Claims {
		subj.Claims = append(subj.Claims, integrate.Claim{Text: cl.Text, Cites: cl.Cites})
	}

	m := integrate.NewMatcher(integrate.NewWrapperCaller(a.wrapper), fixedExcerptReader{ex: ex}, site, a.excerptChars)
	v, err := m.Match(ctx, subj, cands)
	if err != nil {
		return nil, fmt.Errorf("eval match: real call site: %w", err)
	}

	out := matchOutput{Same: v.Same}
	for _, p := range v.DupPairs {
		out.DupPairs = append(out.DupPairs, struct {
			A string `json:"a"`
			B string `json:"b"`
		}{A: p.SubjectA, B: p.SubjectB})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("eval match: encode output: %w", err)
	}
	return raw, nil
}

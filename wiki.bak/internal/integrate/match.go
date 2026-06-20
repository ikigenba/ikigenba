package integrate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentkit/provider"

	"wiki/internal/config"
	"wiki/internal/page"
)

// Match is the document pass's one resolution LLM call (design §4.3): a
// structured, tool-less judgment over a candidate shortlist that returns a BINARY
// verdict — same(id) | no_match — plus a candidate-pair side channel that feeds
// dup_flags. It is identity-not-similarity and doubt-is-no_match by prompt; the
// asymmetry is deliberate (a false split is cheap and lint-repairable, a false
// merge poisons a page). Match runs only on the Shortlist arm of resolution
// (P6b); the resolved/create arms never call it.
//
// Match is a clean, externally-callable function over an injected
// (prompt, model, effort) triple (eval obligation 1): the harness scores it by
// swapping config.CallSite, calling the same function. Its TWO outputs — the
// binary verdict AND the dup_pairs side channel — are returned as distinct values
// (obligation 3), never lumped into one pass/fail.

// excerptReader is the minimal slice of *page.Store match needs: the per-candidate
// evidence (canonical name + full alias list + the leading body excerpt). Declared
// as an interface so the stage is unit-testable without a live DB.
type excerptReader interface {
	ReadExcerpt(ctx context.Context, subjectID string, chars int) (page.Excerpt, error)
}

// Matcher runs the match stage with an injected call-site triple and the
// config-injected excerpt length. Construct it once at the composition root; the
// assembler calls Match per shortlisted subject.
type Matcher struct {
	caller       structuredCaller
	reg          excerptReader
	site         config.CallSite
	excerptChars int
}

// NewMatcher builds a Matcher over a structured caller, the excerpt reader, the
// match call-site triple, and the config-injected WIKI_MATCH_EXCERPT_CHARS body
// excerpt length (eval-harness knob, obligation 2; a non-positive value falls back
// to the design default 600 so a mis-set knob never blanks the evidence).
func NewMatcher(caller structuredCaller, reg excerptReader, site config.CallSite, excerptChars int) *Matcher {
	if excerptChars <= 0 {
		excerptChars = DefaultMatchExcerptChars
	}
	return &Matcher{caller: caller, reg: reg, site: site, excerptChars: excerptChars}
}

// DefaultMatchExcerptChars is the design §4.3 "first 600 of the page body"
// candidate-excerpt length used when the config knob is unset.
const DefaultMatchExcerptChars = 600

// MatchVerdict is match's binary output (design §4.3). Exactly one of the two arms
// is meaningful: a Same verdict carries the matched candidate's subject id; a
// no_match verdict has Same == "". The two-output contract (obligation 3) keeps
// the verdict distinct from the DupPairs side channel the assembler folds into the
// manifest.
type MatchVerdict struct {
	// Same is the matched candidate's subject id, or "" for no_match.
	Same string
	// DupPairs are the candidate-pair flags match surfaced (canonical order),
	// distinct from the verdict — the asymmetric side channel an eval scorer reads
	// (obligation 3). The assembler folds these into the manifest's DupPairs.
	DupPairs []DupPair
}

// Match judges one shortlist: it reads each candidate's excerpt, builds the
// evidence message, invokes the injected structured triple, then parses and
// validates the binary verdict plus the dup-pair side channel. The matched id (on
// a Same verdict) is guaranteed to be one of the offered candidates.
func (m *Matcher) Match(ctx context.Context, subject Subject, candidates []page.Candidate) (MatchVerdict, error) {
	if len(candidates) == 0 {
		// No candidates is a create, not a match — the caller (assembler) should not
		// reach here, but treat it as a clean no_match rather than an empty LLM call.
		return MatchVerdict{}, nil
	}

	excerpts := make([]page.Excerpt, 0, len(candidates))
	ids := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		ex, err := m.reg.ReadExcerpt(ctx, c.SubjectID, m.excerptChars)
		if err != nil {
			return MatchVerdict{}, fmt.Errorf("match: read excerpt for %q: %w", c.SubjectID, err)
		}
		excerpts = append(excerpts, ex)
		ids[c.SubjectID] = struct{}{}
	}

	user := renderMatchEvidence(subject, excerpts)
	msgs := []provider.Message{{
		Role:   provider.RoleUser,
		Blocks: []provider.Block{provider.TextBlock{Text: user}},
	}}

	raw, err := m.caller.Structured(ctx, m.site, MatchSchema, msgs)
	if err != nil {
		return MatchVerdict{}, fmt.Errorf("match: structured call: %w", err)
	}
	return ParseMatch(raw, ids)
}

// renderMatchEvidence builds the match user message: the incoming subject (name,
// aliases, claims) and each candidate's canonical name + full alias list + body
// excerpt (design §4.3 "the evidence"). Deterministic for a fixed input.
func renderMatchEvidence(s Subject, excerpts []page.Excerpt) string {
	var b strings.Builder
	b.WriteString("--- incoming subject ---\n")
	fmt.Fprintf(&b, "type: %s\n", s.Type)
	fmt.Fprintf(&b, "name: %s\n", s.Name)
	if len(s.Aliases) > 0 {
		fmt.Fprintf(&b, "aliases: %s\n", strings.Join(s.Aliases, ", "))
	}
	b.WriteString("claims:\n")
	for _, c := range s.Claims {
		fmt.Fprintf(&b, "  - %s\n", c.Text)
	}
	b.WriteString("\n--- candidates ---\n")
	for i, ex := range excerpts {
		fmt.Fprintf(&b, "candidate %d:\n", i+1)
		fmt.Fprintf(&b, "  id: %s\n", ex.SubjectID)
		fmt.Fprintf(&b, "  canonical name: %s\n", ex.CanonicalName)
		if len(ex.Aliases) > 0 {
			fmt.Fprintf(&b, "  aliases: %s\n", strings.Join(ex.Aliases, ", "))
		}
		if strings.TrimSpace(ex.Body) != "" {
			fmt.Fprintf(&b, "  page excerpt: %s\n", ex.Body)
		}
	}
	b.WriteString("--- end ---\n")
	return b.String()
}

// rawMatchVerdict is the match call's wire shape (design §4.3 / MatchSchema).
type rawMatchVerdict struct {
	Verdict struct {
		Same    string `json:"same"`
		NoMatch bool   `json:"no_match"`
	} `json:"verdict"`
	DupPairs []struct {
		A string `json:"a"`
		B string `json:"b"`
	} `json:"dup_pairs"`
}

// ParseMatch parses and validates a match response body into a MatchVerdict. It is
// separated from the call so the prompt-default gate and goldens can exercise the
// parser + schema offline against a committed fixture, with no client (obligation
// 5 / the standing prompt gate). validIDs is the set of offered candidate ids; a
// Same verdict naming an id outside it is rejected (the model must pick from the
// shortlist). The dup_pairs are canonicalized (smaller ULID first) and de-duped.
func ParseMatch(raw string, validIDs map[string]struct{}) (MatchVerdict, error) {
	var rv rawMatchVerdict
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &rv); err != nil {
		return MatchVerdict{}, fmt.Errorf("match: parse response: %w", err)
	}

	same := strings.TrimSpace(rv.Verdict.Same)
	// Exactly one arm must be set: a non-empty same, OR no_match==true (not both,
	// not neither). Binary, no "uncertain" (design §4.3).
	switch {
	case same != "" && rv.Verdict.NoMatch:
		return MatchVerdict{}, fmt.Errorf("match: verdict sets both same and no_match")
	case same == "" && !rv.Verdict.NoMatch:
		return MatchVerdict{}, fmt.Errorf("match: verdict sets neither same nor no_match")
	}

	v := MatchVerdict{}
	if same != "" {
		if validIDs != nil {
			if _, ok := validIDs[same]; !ok {
				return MatchVerdict{}, fmt.Errorf("match: same id %q is not one of the offered candidates", same)
			}
		}
		v.Same = same
	}

	// Fold the side-channel pairs, canonicalized and de-duped.
	seen := make(map[DupPair]struct{})
	for _, p := range rv.DupPairs {
		a, b := strings.TrimSpace(p.A), strings.TrimSpace(p.B)
		if a == "" || b == "" || a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		dp := DupPair{SubjectA: a, SubjectB: b}
		if _, ok := seen[dp]; ok {
			continue
		}
		seen[dp] = struct{}{}
		v.DupPairs = append(v.DupPairs, dp)
	}
	return v, nil
}

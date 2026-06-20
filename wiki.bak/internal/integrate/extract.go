package integrate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agentkit/provider"

	"wiki/internal/config"
)

// Extract is the document pass's first real integrator stage (design §4.2): one
// full-context, structured, tool-less LLM call that reads a whole document plus a
// mechanical context header and emits subjects, each with its type/kind/name/
// aliases/claims and (for events) an occurred_at. It is single-pass — no tools,
// no wiki access, no loop — because single-pass extraction is the model's native
// competence and is golden-testable. Resolution, the registry lookup, and the
// manifest are NOT this stage's job (P6b / P6b2); Extract ends at schema-valid
// subjects[] — the P6a→P6b data-contract seam (design §4.2).
//
// Extract is a clean, externally-callable function over an injected
// (prompt, model, effort) triple (eval obligation 1): the harness scores it by
// swapping config.CallSite, calling the same function.

// structuredCaller is the minimal slice of *llm.Wrapper extract needs: a single
// structured generation over an injected triple. Declared as an interface so the
// stage is unit-testable with a mocked LLM (the unit gate mocks every LLM from
// P6a on) without importing a concrete client.
type structuredCaller interface {
	Structured(ctx context.Context, site config.CallSite, schema json.RawMessage, msgs []provider.Message) (raw string, err error)
}

// Extractor runs the extract stage with an injected call-site triple. Construct
// it once at the composition root with the wrapper adapter and the extract
// CallSite; the worker calls Extract per document.
type Extractor struct {
	caller structuredCaller
	site   config.CallSite
}

// NewExtractor builds an Extractor over a structured caller and the extract
// call-site triple. The triple (prompt/model/effort) is injected — Extract never
// reads a constant or env (design §10 / obligation 1).
func NewExtractor(caller structuredCaller, site config.CallSite) *Extractor {
	return &Extractor{caller: caller, site: site}
}

// DocumentHeader is the mechanical context built from the causing inbox row
// (design §4.2). It is rendered into the prompt verbatim as a few framing lines —
// NEVER as model-inferred context. ReceivedAt is framed "received on", never
// "today is": the document's arrival time anchors relative-time resolution
// without falsely asserting the present.
type DocumentHeader struct {
	// Source is the inbox row's source (e.g. "dropbox", "mcp", "url").
	Source string
	// Title is the document's title, if the row carries one.
	Title string
	// Tags are the inbox row's tags.
	Tags []string
	// ReceivedAt is when the row was accepted (rendered as a date).
	ReceivedAt time.Time
}

// render produces the human-readable header block that precedes the document in
// the extract prompt's user message. Empty fields are omitted so the model is
// never shown a blank label.
func (h DocumentHeader) render() string {
	var b strings.Builder
	b.WriteString("--- document context ---\n")
	if h.Source != "" {
		fmt.Fprintf(&b, "source: %s\n", h.Source)
	}
	if h.Title != "" {
		fmt.Fprintf(&b, "title: %s\n", h.Title)
	}
	if len(h.Tags) > 0 {
		fmt.Fprintf(&b, "tags: %s\n", strings.Join(h.Tags, ", "))
	}
	if !h.ReceivedAt.IsZero() {
		// "received on", never "today is" (design §4.2): the arrival date anchors
		// relative time without asserting the present.
		fmt.Fprintf(&b, "received on: %s\n", h.ReceivedAt.UTC().Format("2006-01-02"))
	}
	b.WriteString("--- end context ---\n")
	return b.String()
}

// rawSubject is the extract call's wire shape — the JSON the model emits, one
// object per subject under "subjects". It mirrors design §4.2's output contract
// exactly; Extract validates and converts it to []Subject (the extracted-fields
// half of the manifest's Subject — resolution annotations are filled later).
type rawSubject struct {
	Type       string     `json:"type"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Aliases    []string   `json:"aliases"`
	Claims     []rawClaim `json:"claims"`
	OccurredAt string     `json:"occurred_at"`
}

type rawClaim struct {
	Text string `json:"text"`
}

type extractEnvelope struct {
	Subjects []rawSubject `json:"subjects"`
}

// Extract runs the document-pass extract call: it builds the context header +
// document user message, invokes the injected structured triple, then parses and
// schema-validates the result into []Subject (the extracted-fields half only).
// The single causing-inbox-row id is stamped onto every claim's Cites (the
// document pass's one citation per claim — Manifest canonicity §4.3). Resolution
// annotations (SubjectID, TargetPage, …) stay zero — P6b/P6b2 fill them.
func (e *Extractor) Extract(ctx context.Context, header DocumentHeader, document string, causedBy string) ([]Subject, error) {
	user := header.render() + "\n" + document
	msgs := []provider.Message{{
		Role:   provider.RoleUser,
		Blocks: []provider.Block{provider.TextBlock{Text: user}},
	}}

	raw, err := e.caller.Structured(ctx, e.site, ExtractSchema, msgs)
	if err != nil {
		return nil, fmt.Errorf("extract: structured call: %w", err)
	}
	return ParseExtract(raw, causedBy)
}

// ParseExtract parses and validates an extract response body into []Subject,
// stamping causedBy as the single cite on every claim. It is separated from the
// call so the prompt-default gate and goldens can exercise the parser + schema
// offline against a committed fixture, with no client (obligation 5 / the
// standing prompt gate).
func ParseExtract(raw string, causedBy string) ([]Subject, error) {
	var env extractEnvelope
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &env); err != nil {
		return nil, fmt.Errorf("extract: parse response: %w", err)
	}

	out := make([]Subject, 0, len(env.Subjects))
	for i, rs := range env.Subjects {
		if err := validateRawSubject(rs); err != nil {
			return nil, fmt.Errorf("extract: subject %d: %w", i, err)
		}
		claims := make([]Claim, 0, len(rs.Claims))
		for _, rc := range rs.Claims {
			claims = append(claims, Claim{
				Text:  strings.TrimSpace(rc.Text),
				Cites: citesFor(causedBy),
			})
		}
		s := Subject{
			Type:    rs.Type,
			Kind:    strings.TrimSpace(rs.Kind),
			Name:    strings.TrimSpace(rs.Name),
			Aliases: cleanAliases(rs.Aliases),
			Claims:  claims,
		}
		// occurred_at is events-only (design §4.1); a non-event value is dropped
		// rather than treated as a contradiction this early.
		if rs.Type == TypeEvent {
			s.OccurredAt = strings.TrimSpace(rs.OccurredAt)
		}
		out = append(out, s)
	}
	return out, nil
}

// validateRawSubject enforces the §4.2 output contract structurally: a closed-set
// type, a non-empty name, at least one non-empty claim. Content quality (no
// pronouns, salience) is the prompt's job and Part II's score, not a parse error.
func validateRawSubject(rs rawSubject) error {
	switch rs.Type {
	case TypeEntity, TypeEvent, TypeConcept:
	default:
		return fmt.Errorf("invalid type %q (must be entity|event|concept)", rs.Type)
	}
	if strings.TrimSpace(rs.Name) == "" {
		return fmt.Errorf("missing name")
	}
	nonEmpty := 0
	for _, c := range rs.Claims {
		if strings.TrimSpace(c.Text) != "" {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		return fmt.Errorf("subject %q has no claims (claim-bearing salience gate)", rs.Name)
	}
	return nil
}

// stripCodeFence removes a leading/trailing Markdown code fence (```json … ```)
// some models wrap structured output in. The Anthropic backend has no native
// structured-output field (structured output is enforced by parse+validate, not a
// schema field — R-WFWM-BKWX), so a fenced reply is a normal, recoverable shape,
// not an error. Returns the inner JSON, trimmed.
func stripCodeFence(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence line (``` or ```json) and the closing fence.
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

func citesFor(causedBy string) []string {
	if strings.TrimSpace(causedBy) == "" {
		return nil
	}
	return []string{causedBy}
}

func cleanAliases(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		if t := strings.TrimSpace(a); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

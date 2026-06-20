package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"wiki/internal/page"
)

// The dup-JUDGE call (design §6): a tool-less structured identity verdict on a
// flagged pair — merge | dismiss | can't-tell-yet — plus, on a merge, the
// canonical name pick (a FIELD of this output, not a separate call). It is a clean,
// externally-callable function over an injected (prompt, model, effort) triple
// (eval obligation 1): the harness scores it by swapping config.CallSite; its
// ternary verdict is preserved VERBATIM, never collapsed to a binary (obligation 3),
// and the canonical-name pick is scored as its own dimension off this output.

// Verdict is the dup-judge's ternary outcome (design §6). The three values are
// preserved verbatim for the eval harness's asymmetric scorer (obligation 3).
type Verdict string

const (
	// VerdictMerge — confident same subject; perform the merge.
	VerdictMerge Verdict = "merge"
	// VerdictDismiss — confident DIFFERENT subjects; permanent, blocks re-flagging.
	VerdictDismiss Verdict = "dismiss"
	// VerdictCantTell — insufficient evidence; re-judge only when a page advances.
	VerdictCantTell Verdict = "cant_tell"
)

// JudgeResult is the parsed dup-judge output: the ternary verdict plus, on a
// merge, the canonical name to keep (the §6 canonical-name pick — empty otherwise).
type JudgeResult struct {
	Verdict       Verdict
	CanonicalName string
}

// JudgeSchema pins the judge's structured output: a closed-set verdict string plus
// an optional canonical_name. ParseJudge validates semantically on top (merge
// requires a non-empty canonical_name; the verdict is one of the three).
var JudgeSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict"],
  "properties": {
    "verdict": {"type": "string", "enum": ["merge", "dismiss", "cant_tell"]},
    "canonical_name": {"type": "string"}
  }
}`)

// Judge runs the dup-judge call over a flagged pair (design §6). It renders both
// subjects' FULL pages + complete alias lists into the user message (the
// better-informed second look the §6 design demands — never a truncated excerpt),
// invokes the injected triple, and parses the ternary verdict.
func (j *Job) Judge(ctx context.Context, a, b page.DupSubject) (JudgeResult, error) {
	raw, err := j.caller.Structured(ctx, j.judgeSite, JudgeSchema, userMsg(renderJudgeInput(a, b)))
	if err != nil {
		return JudgeResult{}, fmt.Errorf("lint: judge call: %w", err)
	}
	return ParseJudge(raw)
}

// renderJudgeInput builds the judge user message: both subjects' identity, full
// alias lists, and full page bodies (design §6). Deterministic for a fixed pair.
func renderJudgeInput(a, b page.DupSubject) string {
	var s strings.Builder
	s.WriteString("Decide whether these two subjects are the SAME real-world subject.\n")
	writeDupSubject(&s, "A", a)
	writeDupSubject(&s, "B", b)
	return s.String()
}

func writeDupSubject(s *strings.Builder, label string, d page.DupSubject) {
	fmt.Fprintf(s, "\n--- subject %s ---\n", label)
	fmt.Fprintf(s, "type: %s\n", d.Type)
	fmt.Fprintf(s, "name: %s\n", d.CanonicalName)
	if len(d.Aliases) > 0 {
		fmt.Fprintf(s, "aliases: %s\n", strings.Join(d.Aliases, ", "))
	}
	body := d.Body
	if strings.TrimSpace(body) == "" {
		body = "(no page yet)"
	}
	fmt.Fprintf(s, "page:\n%s\n", body)
}

// ParseJudge parses+validates a judge response into a JudgeResult. Separated from
// the call so the prompt-default gate and goldens exercise the parser + schema
// offline against a committed fixture, with no client (obligation 5 / the standing
// prompt gate). A merge verdict MUST carry a non-empty canonical name (the §6
// canonical-name pick); dismiss/cant_tell ignore it.
func ParseJudge(raw string) (JudgeResult, error) {
	var out struct {
		Verdict       string `json:"verdict"`
		CanonicalName string `json:"canonical_name"`
	}
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &out); err != nil {
		return JudgeResult{}, fmt.Errorf("lint: parse judge response: %w", err)
	}
	v := Verdict(strings.TrimSpace(out.Verdict))
	switch v {
	case VerdictMerge, VerdictDismiss, VerdictCantTell:
	default:
		return JudgeResult{}, fmt.Errorf("lint: judge returned invalid verdict %q (must be merge|dismiss|cant_tell)", out.Verdict)
	}
	name := strings.TrimSpace(out.CanonicalName)
	if v == VerdictMerge && name == "" {
		return JudgeResult{}, fmt.Errorf("lint: judge merge verdict has no canonical_name (§6 canonical-name pick)")
	}
	return JudgeResult{Verdict: v, CanonicalName: name}, nil
}

// FoldResult is the parsed fold output: the one merged page (title + body) and the
// §6.1 superseded list (citations deliberately dropped in the fold).
type FoldResult struct {
	Title      string
	Body       string
	Superseded []string
}

// FoldSchema pins the fold's structured output (design §6/§6.1): the merged title,
// body, and the dropped-citation list.
var FoldSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "body", "superseded"],
  "properties": {
    "title": {"type": "string"},
    "body": {"type": "string"},
    "superseded": {"type": "array", "items": {"type": "string"}}
  }
}`)

// Fold runs the fold call on a confirmed-same pair (design §6, run only on a merge
// verdict): both page bodies + the chosen canonical name in → one merged body out.
// It parses the result and ENFORCES the §6.1 citation gate against the union of the
// two source bodies' citations, so the fold can never silently drop evidence.
func (j *Job) Fold(ctx context.Context, canonicalName string, winner, loser page.DupSubject) (FoldResult, error) {
	raw, err := j.caller.Structured(ctx, j.foldSite, FoldSchema, userMsg(renderFoldInput(canonicalName, winner, loser)))
	if err != nil {
		return FoldResult{}, fmt.Errorf("lint: fold call: %w", err)
	}
	res, err := ParseFold(raw)
	if err != nil {
		return FoldResult{}, err
	}
	if err := checkCitationPreservation([]string{winner.Body, loser.Body}, res.Body, res.Superseded); err != nil {
		return FoldResult{}, err
	}
	return res, nil
}

// renderFoldInput builds the fold user message: the canonical name plus both page
// bodies to merge (design §6). Deterministic.
func renderFoldInput(canonicalName string, winner, loser page.DupSubject) string {
	var s strings.Builder
	fmt.Fprintf(&s, "Merge these two pages about the same subject into one.\ncanonical name: %s\n", canonicalName)
	fmt.Fprintf(&s, "\n--- page 1 ---\n%s\n", winner.Body)
	fmt.Fprintf(&s, "\n--- page 2 ---\n%s\n", loser.Body)
	return s.String()
}

// ParseFold parses+validates a fold response. Separated from the call so the
// prompt-default gate + goldens exercise the parser offline (obligation 5). A
// non-empty body is required (the merged page must exist).
func ParseFold(raw string) (FoldResult, error) {
	var out struct {
		Title      string   `json:"title"`
		Body       string   `json:"body"`
		Superseded []string `json:"superseded"`
	}
	if err := json.Unmarshal([]byte(stripCodeFence(raw)), &out); err != nil {
		return FoldResult{}, fmt.Errorf("lint: parse fold response: %w", err)
	}
	if strings.TrimSpace(out.Body) == "" {
		return FoldResult{}, fmt.Errorf("lint: fold produced an empty body")
	}
	return FoldResult{
		Title:      strings.TrimSpace(out.Title),
		Body:       out.Body,
		Superseded: cleanList(out.Superseded),
	}, nil
}

// cleanList trims and drops empty entries.
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stripCodeFence removes a leading/trailing Markdown code fence some models wrap
// structured output in (the Anthropic backend has no native structured-output
// field — a fenced reply is a normal, recoverable shape, not an error).
func stripCodeFence(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if k := strings.LastIndex(s, "```"); k >= 0 {
		s = s[:k]
	}
	return strings.TrimSpace(s)
}

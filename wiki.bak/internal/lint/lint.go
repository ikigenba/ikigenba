// Package lint is wiki's family of named maintenance jobs over the integration
// spine (design §6): jobs a worker self-selects and runs as one runs row per
// attempt, with the failure policy applied verbatim. Each lint job is triggered by
// a cron tick OR on demand (the lint_run MCP verb is just another front door that
// Accepts a trigger row). P9a lands the FIRST lint job, lint-dups, plus the shared
// plumbing the later jobs reuse: the dup-queue work list, the two-call (judge +
// fold) structure, and the §6.1 citation gate the fold inherits.
//
// lint-dups consumes the dup_flags queue (a flag is EVIDENCE, never a verdict —
// §6): per eligible pair it runs a dedicated identity JUDGE (both full pages +
// alias lists in), yielding merge | dismiss | can't-tell-yet. On a merge the
// loser is chosen MECHANICALLY (older ULID wins; the judge picks only the canonical
// NAME), a FOLD call produces the one merged body, and the subject merge is applied
// in ONE TRANSACTION PER PAIR (per-pair recovery via the queue itself).
package lint

import (
	"context"
	"encoding/json"

	"agentkit/provider"

	"wiki/internal/config"
)

// caller is the minimal slice of *llm.Wrapper the lint calls (judge, fold) need:
// a single structured generation over an injected (prompt, model, effort) triple.
// Declared as an interface so the jobs are unit-testable with a mocked LLM (the
// unit gate mocks every LLM, from P6a on) without importing a concrete client.
type caller interface {
	Structured(ctx context.Context, site config.CallSite, schema json.RawMessage, msgs []provider.Message) (raw string, err error)
}

// userMsg wraps a single user-role text block — the one message shape both lint
// calls send (the system prompt carries the framing; the body carries the
// evidence).
func userMsg(text string) []provider.Message {
	return []provider.Message{{
		Role:   provider.RoleUser,
		Blocks: []provider.Block{provider.TextBlock{Text: text}},
	}}
}

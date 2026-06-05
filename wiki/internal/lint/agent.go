// Package lint is the wiki's agentic maintenance pass (Task 5.2). It reuses the
// exact agent/job machinery built for ingest (Task 4.1) — the agentkit provider
// client, the agent tool-use loop, the agentkit/job runner, and the wiki
// jobstore — and differs from ingest only in three things:
//
//  1. its TOOLSET adds glob+grep (read-only discovery) on top of read/write/edit,
//     so the lint agent can hunt down duplicate pages, synonymous types, orphans,
//     and missing cross-references across the whole tree;
//  2. its SYSTEM PROMPT is the lint-pass framing (consolidate / merge / flag),
//     not the integration-pass framing; and
//  3. it operates over the EXISTING page tree — there is no new raw doc, no
//     provenance stamp, no wiki_ingest row. It is the gravity that keeps the open
//     vocabulary coherent.
//
// The trigger is MANUAL for now: Linter.Lint is an internal trigger callable
// today and schedulable later. The cadence question (manual / scheduled /
// post-ingest) is DEFERRED per GOALS ("Lint … Trigger cadence TBD") and PLAN
// Task 5.2 ("Cadence left manual + documented as open"); lint is deliberately NOT
// a public MCP verb — the Task-5.1 surface stays exactly five verbs.
//
// Single-flight: lint mutates the SAME surface as ingest (index.md / log.md /
// the page tree), so it shares ingest's per-(owner, collection) flight key
// (ingest.FlightKey). A lint while an ingest runs — or vice-versa — is rejected
// with job.ErrFlightInUse: only one write-pass runs per collection at a time.
package lint

import (
	"agentkit/provider"
	"agentkit/tools/edit"
	"agentkit/tools/glob"
	"agentkit/tools/grep"
	"agentkit/tools/read"
	"agentkit/tools/write"

	wikischema "wiki/internal/store/schema"
)

// DefaultModel is the lint agent's model when WIKI_LINT_MODEL is unset. It
// mirrors the ingest default (a mid-tier model with a per-job cost ceiling from
// config, not hardcoded — PLAN Decision 3); main.go threads the resolved value
// here via the shared Config.
const DefaultModel = "claude-sonnet-4-6"

// DefaultMaxTokens is the per-job output-token ceiling when WIKI_LINT_MAX_TOKENS
// is unset — the cost knob (PLAN Decision 3), a default rather than a hardcoded
// constant in the agent. main.go overrides it from env.
const DefaultMaxTokens = 8192

// lintToolset is the read+write surface the lint agent is allowed: read (consult
// index.md + pages), glob+grep (DISCOVERY — find duplicate/synonymous pages,
// orphans, and missing cross-references across the tree), write (create/replace a
// consolidated page) and edit (amend a page in place — flag a contradiction,
// add a cross-ref, mark a page superseded). NO bash — lint never shells out, and
// the smaller surface is the security floor before OS-level confinement (Phase
// 7). All file paths are confined to the owner+collection root via the agent
// loop's sandboxRoot argument; glob/grep are confined the same way (the dispatch
// resolves their path through effectiveSearchPath under sandboxRoot).
func lintToolset() []provider.Tool {
	return []provider.Tool{
		{Name: read.Name, InputSchema: read.InputSchema},
		{Name: glob.Name, InputSchema: glob.InputSchema},
		{Name: grep.Name, InputSchema: grep.InputSchema},
		{Name: write.Name, InputSchema: write.InputSchema},
		{Name: edit.Name, InputSchema: edit.InputSchema},
	}
}

// systemPrompt builds the lint agent's system prompt: the wiki's embedded schema
// doc (the type set, frontmatter conventions, index-first navigation, the four
// invariants — SCHEMA.md) followed by the lint-pass instructions. The schema doc
// is the single source of truth for the conventions; this function only appends
// the per-run framing.
func systemPrompt() string {
	return wikischema.Doc() + "\n\n" + lintInstructions
}

// lintInstructions is the lint-pass framing appended to the schema doc. It states
// the GOALS lint job (consolidate synonymous types/pages, merge duplicate pages,
// flag orphans / missing cross-references) in operational terms and binds it hard
// to the four invariants: lint SURFACES and MERGES carefully — it never destroys.
// Kept tight and faithful to SCHEMA.md / GOALS (the agent has already read the
// schema above).
const lintInstructions = `## Your task right now: lint and maintain this wiki

This is a maintenance pass over an EXISTING wiki — no new document is being
ingested. Your job is to keep the open vocabulary coherent so "broad" does not
rot into "fragmented." All your file paths are RELATIVE to the wiki's collection
root — you are already confined there; never use absolute paths or ` + "`..`" + `.

Work in this order:

1. READ ` + "`index.md`" + ` first to orient (the catalog / navigation entry point).
2. SURVEY the tree to find problems. Use Glob (e.g. ` + "`sources/*.md`, `concepts/*.md`, `**/*.md`" + `)
   to list pages, and Grep to find duplicate/synonymous content, broken or
   missing cross-references, and orphan pages (pages nothing links to and which
   link to nothing).
3. CONSOLIDATE synonymous types/pages and MERGE duplicate pages: pick one
   canonical page, fold the others' content into it, and leave the now-redundant
   page as a short stub that points to the canonical one (supersede — do not
   delete). Prefer the broad type set; capture finer distinctions in ` + "`kind:`" + `
   rather than inventing new directories.
4. FLAG orphans and missing cross-references: add the missing links, and on a
   page that nothing references, note it (or link it from ` + "`index.md`" + `) so it
   is reachable. If two pages CONTRADICT each other, flag the contradiction on
   the page — do not pick a winner and clobber the other claim.
5. UPDATE ` + "`index.md`" + ` to reflect any merges, renames, or new links.
6. APPEND one line to ` + "`log.md`" + ` describing what you changed (append-only).

Honor the four invariants WITHOUT EXCEPTION — lint surfaces and merges
carefully, it does NOT destroy:
- Provenance: never strip a page's trail back to its source page; a merged page
  keeps every source it consolidates.
- Immutable raw: NEVER write into ` + "`raw/`" + ` or modify any raw document. Lint
  touches only the curated page tree, never the originals.
- Flag, don't overwrite: surface contradictions; never silently clobber a prior
  claim. The human rules on meaning.
- Append, don't destroy: supersede rather than delete (leave a stub that points
  to the successor); ` + "`log.md`" + ` is append-only.

If the wiki is already clean, that is a valid outcome: read, confirm, append a
short ` + "`log.md`" + ` note saying lint found nothing to do, and finish. When done,
finish with a short plain-text summary of what you consolidated, merged, or
flagged. Do not ask the user questions — this runs unattended.`

// userMessage is the per-run user turn handed to the lint agent. Unlike ingest
// (which points the agent at one freshly-stored raw doc), lint operates over the
// whole tree, so the turn just names the scope and kicks off the pass; the schema
// doc + lintInstructions in the system prompt carry the how.
func userMessage(owner, collection string) string {
	return "Run a maintenance/lint pass over this wiki (owner " + owner +
		", collection " + collection + "). Consolidate synonymous types and " +
		"pages, merge duplicates, and flag orphans and missing cross-references — " +
		"honoring every invariant (supersede/flag/append; never destroy; raw/ is " +
		"immutable). Update index.md and append a line to log.md."
}

// Package ingest is the wiki's agentic async ingest pipeline (Task 4.1). It wires
// the generic agentkit machinery (provider client, agent loop, base tools, job
// runner) to the wiki's concrete layers (the filesystem store, the BM25 search
// index, the SQLite job/provenance tables) behind one core entrypoint:
//
//	trigger → persist bytes to immutable raw/ → async agentkit integration job
//	(the agent files/updates pages) → on success, re-index search.
//
// The package is env-free: model, cost ceiling, TTL, client factory, store,
// search index, and DB are all injected by cmd/wiki/main.go. The agent's
// behaviour (system prompt + toolset) lives here; the lifecycle (spawn / cancel /
// sweep / single-flight) lives in agentkit/job.
package ingest

import (
	"agentkit/provider"
	"agentkit/tools/edit"
	"agentkit/tools/read"
	"agentkit/tools/write"

	wikischema "wiki/internal/store/schema"
)

// DefaultModel is the ingest agent's model when WIKI_INGEST_MODEL is unset
// (PLAN Decision 3: a mid-tier model with a per-job cost ceiling from config,
// not hardcoded in the agent). main.go threads the resolved value here.
const DefaultModel = "claude-sonnet-4-6"

// DefaultMaxTokens is the per-job output-token ceiling when WIKI_INGEST_MAX_TOKENS
// is unset. This is the cost knob (PLAN Decision 3): the agent loop bills against
// the resolved model's pricing, and provider.Request.MaxTokens caps each turn's
// output. It is a default, not a hardcoded constant in the agent — main.go
// overrides it from env.
const DefaultMaxTokens = 8192

// integrationToolset is the write-enabled, bash-free surface the ingest agent is
// allowed: read (to consult the raw doc, index, and existing pages), write (to
// create/replace whole pages), edit (to amend pages in place). NO bash, NO glob,
// NO grep — ingest never needs to shell out, and the smaller surface is the
// security floor before OS-level confinement (Phase 7). All writes are confined
// to the owner+collection root via the agent loop's sandboxRoot argument.
func integrationToolset() []provider.Tool {
	return []provider.Tool{
		{Name: read.Name, InputSchema: read.InputSchema},
		{Name: write.Name, InputSchema: write.InputSchema},
		{Name: edit.Name, InputSchema: edit.InputSchema},
	}
}

// systemPrompt builds the ingest agent's system prompt: the wiki's embedded
// schema doc (the type set, frontmatter conventions, index-first navigation, the
// four invariants — SCHEMA.md) followed by the integration-pass instructions. The
// schema doc is the single source of truth for the conventions; this function
// only appends the per-run framing.
func systemPrompt() string {
	return wikischema.Doc() + "\n\n" + integrationInstructions
}

// integrationInstructions is the integration-pass framing appended to the schema
// doc. It restates the GOALS integration pass (read raw → source page → touched
// concept/entity/event pages → index → log) and the four invariants in
// operational terms, keeping it tight and faithful to SCHEMA.md (which the agent
// has already read above).
const integrationInstructions = `## Your task right now: integrate one freshly-ingested raw document

A new raw document has just been stored immutably under ` + "`raw/`" + ` and a
` + "`source`" + ` page may need to be created or updated for it. The user message
tells you the raw document's path (e.g. ` + "`raw/<sha256>.md`" + `) and its
provenance. All your file paths are RELATIVE to the wiki's collection root — you
are already confined there; never use absolute paths or ` + "`..`" + `.

Do the integration pass, in order:

1. READ ` + "`index.md`" + ` first to orient (it may not exist yet — that's fine).
2. READ the raw document at the path you were given.
3. CREATE or UPDATE the document's ` + "`source`" + ` page under ` + "`sources/`" + `
   — the provenance anchor. Carry the raw doc's sha256, ingested_at, and the
   caller-supplied title/source/tags into its frontmatter.
4. CREATE or UPDATE the ` + "`concept`" + ` / ` + "`entity`" + ` / ` + "`event`" + `
   pages the document touches (one source can touch several). Every such page must
   cite the source page (Provenance).
5. UPDATE ` + "`index.md`" + ` so the catalog reflects any new or changed pages.
6. APPEND one line to ` + "`log.md`" + ` describing what you did (append-only).

Honor the four invariants without exception:
- Provenance: every curated page traces back to a source page.
- Immutable raw: NEVER write into ` + "`raw/`" + ` or modify the raw document.
- Flag, don't overwrite: if new info contradicts an existing page, note the
  contradiction on the page — do not silently clobber the prior claim.
- Append, don't destroy: supersede rather than delete; ` + "`log.md`" + ` is
  append-only.

When the integration is complete, finish with a short plain-text summary of the
pages you created or updated. Do not ask the user questions — this runs unattended.`

// userMessage is the per-run user turn handed to the agent: it points the agent
// at the just-stored raw doc and restates the provenance the service stamped, so
// the agent can carry it into the source page's frontmatter without re-reading
// the (frontmatter-stamped) raw file's header to discover it.
func userMessage(rawRelPath, sha256, title, source string, tags []string) string {
	b := &builder{}
	b.line("Integrate this freshly-ingested raw document into the wiki.")
	b.line("")
	b.kv("raw document path", rawRelPath)
	b.kv("sha256", sha256)
	if title != "" {
		b.kv("title", title)
	}
	if source != "" {
		b.kv("source", source)
	}
	if len(tags) > 0 {
		b.kv("tags", joinTags(tags))
	}
	return b.String()
}

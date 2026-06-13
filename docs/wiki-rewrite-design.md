# Design — Wiki Rewrite: a benchmarkable knowledge substrate

This document proposes a clean rewrite of the `wiki` service. The product goal
is unchanged — `wiki` is the **unified knowledge / memory** of the entity the
suite represents — but this draft replaces the earlier graph-cascade design
with a deliberately simpler system built around one organizing target:

> **v1 is a complete, benchmarkable harness.** Everything important about the
> wiki — ingest speed, query speed, retrieval accuracy, cost — is a number on
> a scorecard. Features are admitted by scorecard, not by argument.

The clean rewrite is approved; no knowledge in the current wiki needs
preserving. Everything here honors the suite's operating bet: one box, SQLite
+ files, a single writer, SMB scale, pure-Go `CGO_ENABLED=0` builds. The
ordered steps live in the paired `wiki-rewrite-plan.md`.

## Three principles

**1. The graph is canonical; everything else is a rebuildable projection.**
One SQLite database holds episodes, nodes, edges, and the indexes built over
them. Indexes (FTS, vectors) are disposable and rebuilt from the graph.

**2. Episodes are the dataset.** Every input — suite event, pasted text,
fetched URL, file body, session transcript — is recorded verbatim and
immutably in the episode log *before* anything else happens to it, and the
ingest pipeline is a **replayable function of the episode log and the checked-
out code**. Two consequences fall out:

- Any production backup contains a complete benchmark dataset, because the
  backup is the SQLite database and the episodes ride along inside it.
- A/B comparison is: check out branch X, replay dataset A, record the
  scorecard; check out branch Y, replay dataset A, record the scorecard;
  diff. No special capture step, no synthetic corpus.

**3. The scorecard governs evolution.** This is a development loop, not a
runtime loop — the wiki never tunes itself in production. Proposed changes
(prompts, retrieval, models, new ingest stages) flow through the normal
branch → bench → CI → deploy pipeline, and the bench numbers gate the merge.
Hard metrics gate; fuzzy metrics trend (see Evaluation).

A corollary of principle 2 that constrains all future work:

> **Nothing may influence ingest except (a) the episode log and (b) committed
> code.** Prompts, projection rules, model choices, and config that shape the
> graph are code, not database state — so a branch checkout fully determines
> pipeline behavior, and a replay on the same branch is reproducible.

## The promotion rule — state in the graph, activity in the log

The suite's event stream is dominated by *activity* (transactions, file
touches, messages, cron fires), not *knowledge*. Projecting every event into
the graph would make it 95%+ micro-facts by volume — polluting search,
duplicating the systems of record the services already own, and exploding
every downstream cost. So the graph and the episode log hold different things:

> **An event is projected into the graph only if it creates or retires an
> entity, or changes a durable attribute or relationship of one. Activity
> events stay as episodes** and at most bump an aggregate attribute on the
> entity they touch.

Concretely:

- `contact.created/updated`, org changes, `contact.tagged/untagged` → **graph
  facts** (entity-shaping, low-volume, supersession meaningful).
- `transaction.recorded` → **episode only**. No `txn:<id>` node. Optionally
  bumps `last_activity` / counters on the counterparty entity. The entity
  carries a pointer to the owning service ("details: ledger `register`") —
  the wiki is the map, not a second copy of the territory.
- `file.created/modified/deleted` → episode; at most one live `file:<path>`
  entity whose content-hash fact supersedes, debounced — not an edge per save.
- `cron.<name>` → not consumed at all.

Aggregate attributes are few, cosmetic, and rebuildable from the episode log —
never load-bearing.

The wiki's identity in the suite follows from this rule: **the cross-service
index of entities and their current state, with provenance pointers into the
services that own the detail.** It unifies the suite by linking the services,
not by mirroring them.

## Storage model

One SQLite file (`modernc.org/sqlite`, suite-standard migrations).

### Episodes — immutable raw, and the replay source

```
episodes(
  id            PK,            -- deterministic: hash(kind, source, content_hash, occurred_at)
  kind          TEXT,          -- event | text | url | file | session
  source        TEXT,          -- producer/service, URL, path, run id
  content_hash  TEXT,          -- key into blobs/<hash>: ALL content, every kind
  mime          TEXT,
  size          INTEGER,
  occurred_at   TIMESTAMP,     -- when it happened in the world
  ingested_at   TIMESTAMP,     -- when we received it
  attrs         JSON
)
```

Never mutated, never deleted (retention is an explicit non-goal for v1; SMB
scale fits in SQLite comfortably).

**Body policy — content never lives in the database, no exceptions.** Every
episode's content — event payload, file (text or binary), fetched page,
pasted text, session transcript — is written at ingest to a content-addressed
`blobs/<sha256>` directory beside the database (fan-out sharded,
`blobs/ab/abcd…`); the episode row is metadata plus the hash. All record
kinds act the same. The blob store is canonical wiki state from v1.0: the
`backup`/`restore` verbs include it and datasets carry it, so a backup
remains a *complete* replayable capture — including for rungs that don't
exist yet (image captioning or PDF extraction can later be benchmarked
against historical bytes). Content addressing dedups identical bytes for
free, and superseded file versions are retained as distinct blobs — version
history the dropbox mirror (latest-only) does not keep. URL ingest blobs the
*fetched body*, not the URL alone, so replay never re-fetches the network.
The FTS index necessarily contains the text chunks it indexes, but it is a
rebuildable projection of the blobs (drop and rebuild), not canonical
storage.

### Graph — current state, supersession-aware

Node taxonomy is fixed (`entity | location | event | concept`); relation
types are open vocabulary, SCREAMING_SNAKE.

```
nodes(
  id           PK,            -- deterministic (see Determinism contract)
  slug         TEXT,          -- agent-facing, e.g. entity:contact-7f3a
  node_type    TEXT,
  name         TEXT,
  name_norm    TEXT,
  summary      TEXT,
  attrs        JSON,
  created_at   TIMESTAMP,     -- audit (derived from episode time on replay)
  expired_at   TIMESTAMP      -- NULL = live; set on merge/retirement
)

edges(
  id            PK,           -- deterministic
  source_id     -> nodes.id,
  target_id     -> nodes.id,
  relation_type TEXT,
  fact          TEXT,         -- natural-language fact, preserves specifics
  valid_at      TIMESTAMP,    -- became true in the world
  invalid_at    TIMESTAMP,    -- superseded; NULL = currently true
  created_at    TIMESTAMP,    -- audit
  episode_id    -> episodes.id,
  attrs         JSON
)
```

This is **supersession, not full bi-temporality**: `valid_at`/`invalid_at`
carry world-time and supersession; `created_at` is a plain audit column.
The earlier four-timestamp model (separate transaction-time validity with
`expired_at` retraction semantics on edges) is dropped — the realized value of
bi-temporal systems in the field is "supersede stale facts," which two
timestamps and a tombstone deliver at a fraction of the complexity. If a
genuine "what did we believe at T" requirement ever appears, the immutable
episode log can reconstruct it.

Indexes: `edges(source_id)`, `edges(target_id)`, `edges(relation_type)`, a
partial live index `WHERE invalid_at IS NULL`, `nodes(node_type)`,
`nodes(name_norm)`, `episodes(occurred_at)`. A `live_edges` view serves the
current-state case. Traversal is recursive CTEs with a depth cap — the right
tool precisely because we reject the scales that break it.

### Indexes — rebuildable

- **FTS5** (`modernc.org/sqlite` provides it): `nodes_fts` (name + summary),
  `edges_fts` (fact), `episodes_fts` (chunked bodies, separate scope — see
  Retrieval). Kept in sync by triggers; rebuildable by a `reindex` pass.
- **Vectors — deferred, decided.** When the scorecard admits embeddings, the
  provider is **OpenAI** (`text-embedding-3-small` to start), stored as BLOB
  columns and scanned brute-force in Go (cosine over a few thousand vectors is
  sub-millisecond at our scale). **sqlite-vec is ruled out**: it is a C
  extension and the suite is hard `CGO_ENABLED=0` on pure-Go SQLite — and it
  is brute-force anyway, so Go-side scan is functionally equivalent with zero
  dependencies. Every stored vector records `embedding_model` + dimensions so
  a model rollover is a rebuild, not archaeology.

## Ingest — a ladder, each rung admitted by scorecard

Every path begins identically: persist the input as an episode, then process.
Processing is async (the existing `job_status` pattern); if a model or
embedding API is down, jobs stall and drain — acceptable per the
downtime-tolerant bet.

**v1.0 ingest contains no model calls at all.** This is a feature: the entire
v1.0 pipeline is deterministic, replays are byte-identical, and benchmark runs
cost $0 in tokens.

- **Rung 0 (v1.0) — deterministic event projection.** Suite events carry
  stable producer IDs, so they project to graph edits deterministically: the
  event's `time` becomes `valid_at`, supersession falls out of event semantics
  (untag sets `invalid_at`, etc.), identity is given, not guessed. Covered by
  golden snapshot tests — a hard CI gate.
- **Rung 0 (v1.0) — free text as searchable episodes.** Pasted text, URLs,
  and file bodies are chunked and FTS-indexed as episodes. No extraction —
  nothing is ever lost to an extraction miss, and a smarter future model reads
  the same raw text better for free.
- **Rung 1 (v1.1) — session distillation.** The highest-value LLM stage, and
  the one the field validates: conversations are where decisions, preferences,
  and corrections live, and where supersession provably pays off. One model
  call per session transcript (consumed from `prompts` run-completion events
  or a nightly sweep; external session logs arrive via dropbox sync): extract
  the handful of durable facts — decisions, stated preferences, corrections,
  open threads — each linked to existing entities and citing the session
  episode. Guards: the distiller extracts only information attributable to
  the *user* or non-wiki tool results (never the wiki's own output — the echo
  loop is the known slop-compounding failure); session-derived facts carry a
  distinct provenance kind, auditable and bulk-revocable; raw transcripts are
  excluded from default search scope and never surfaced in snippets; the
  distiller is prompted never to extract secret material.
- **Rung 2 (v1.2) — free-text entity linking.** One cheap model call per text
  episode whose only job is *recognition against the closed list* of existing
  event-derived entities ("this email mentions `entity:contact-jane` and
  `entity:org-acme`") — an annotation edge, not entity creation. Linking to
  known stable IDs is a vastly easier task than open extraction + resolution;
  the graph stays clean because only deterministic events (and vetted
  distillation) create nodes.
- **Rung 3+ (admitted only by scorecard)** — embeddings + RRF hybrid
  retrieval; free-text entity *creation*; contradiction detection; community
  summaries. Each is a branch, a bench run, and a diff — not a design debate.

What we are deliberately **not** building: the five-stage Graphiti-style
extraction cascade. The independent evidence is consistent — LLM-extracted
graphs win only on multi-hop/temporal questions, by a few points, at ~50×
indexing cost; unsupervised entity resolution is the known graph-corruption
vector; and compiled extractions freeze the ingest-time model's
interpretation, which is the wrong side of "does a smarter model make this
unnecessary?". The deterministic projection is the engine; LLM stages are
small, single-purpose, and individually benchmarked.

## Retrieval and the MCP tool surface

Retrieval returns **references, not bodies**. v1.0 is FTS5/BM25; hybrid
(BM25 + OpenAI embeddings, fused with RRF k=60) is rung 3, admitted when
Recall@K on the golden queries says so. No reranker, no ANN, no LLM rerank
unless the scorecard demands them.

```
search(query, type?, relation?, scope?, as_of?, limit=10)
   -> references only: [{ id(slug), node_type, title, snippet, score }]
      Default scope is the graph (nodes + edge facts) — the curated state of
      the world. scope="episodes" explicitly searches raw history.
      NEVER returns bodies. The workhorse verb.

get(id | id[], depth=0, format="concise"|"detailed", relation?, as_of?)
   -> depth=0: the node(s); depth>=1: neighborhood via recursive CTE,
      edges as { source, relation, target, fact, valid_at } + neighbor refs.
      concise = name + summary + 1-hop edge titles;
      detailed = attrs + facts + provenance episode ids + service pointers.

ask(question, as_of?)
   -> cited synthesis (below).

ingest_text / ingest_url / job_status / health   (carry over)
```

`search → get` is the whole navigation loop; there is deliberately no
whole-graph dump. `as_of` filters on valid-time (`valid_at <= T AND
(invalid_at IS NULL OR invalid_at > T)`) — a WHERE clause, essentially free.

### `ask` — server-side, cited

`ask` runs server-side as an agentkit agent: retrieve over the graph →
synthesize a cited answer (node/edge slugs + episode ids) → return.
Synchronous from the caller's view, bounded by a timeout and a hop cap.
It uses the wiki's own `ANTHROPIC_API_KEY`; we do not use MCP sampling
(deprecated as of protocol 2026-07-28, and it would break server-side cost
accounting and model choice). `ask` is query-time only — it never writes to
the graph, so it adds no nondeterminism to ingest.

## The benchmark harness

The centerpiece of v1. The contract: **same branch + same dataset → same
graph and same scorecard; different branches + same dataset → a meaningful
diff.**

### Determinism contract

Rules the pipeline must obey (enforced by a replay-twice-and-diff CI test):

1. **No wall clock in ingest.** All timestamps derive from episode fields
   (`occurred_at`, `ingested_at`). A replay of old episodes produces the
   same `created_at` values it produced in production.
2. **Deterministic IDs.** Node and edge ids are content-derived hashes
   (producer ID for event-derived entities; `hash(source, relation, target,
   episode)` for edges) — no ULIDs, no randomness. This is what lets golden
   query labels reference stable slugs across branches and replays.
3. **Stable order.** Episodes process in `(ingested_at, id)` order; all
   intermediate iteration is sorted.
4. **No side effects in bench mode.** Replay runs with the outbox producer
   and any network fetch disabled; URL/file episodes already carry their
   bodies, so nothing reaches the network except (cached) model calls.
5. **LLM stages are temp-0 and cache-addressed** (below); residual model
   nondeterminism is confined to soft metrics.

### Datasets

A dataset is **an episode log plus labels**:

```
datasets/<name>/
  episodes.db        -- episodes table only (exported, or a whole backup)
  blobs/             -- the content-addressed bodies the episodes reference
  queries.jsonl      -- { query, expected: [slug, ...], k }   (retrieval golden set)
  projections/       -- golden snapshots: event episode -> expected graph mutations
  README.md          -- provenance, date, anonymization status
```

Three tiers: `smoke` (~10 episodes, committed to the repo, runs in CI),
`standard` (~100s, committed, the default dev benchmark), and `prod` (a copy
of a production backup, lives on the box / operator machine, never
committed). `wiki export-dataset` extracts the episodes table and the blobs
they reference from any wiki state — including a restored backup — which is
what makes principle 2 real: **every production backup is a benchmark
dataset**. An anonymization
pass for making shareable golden corpora from prod data is follow-on work.

### Replay and compare

```
wiki bench <dataset-dir> [--out results.json]
   -> fresh temp DB, replay all episodes in order through the full pipeline,
      run the golden queries, emit the scorecard.

wiki bench-diff a.json b.json
   -> side-by-side: deltas, regressions flagged.
```

The A/B flow is exactly: `git checkout branch-x && go build && wiki bench
datasets/standard` → `git checkout branch-y && go build && wiki bench
datasets/standard` → `wiki bench-diff`. Results are stamped with the git SHA,
dataset content hash, and the model + embedding-model IDs in play, so a
results file is self-describing.

### The model-call cache

Replays must be cheap or they won't be run. All model calls in ingest go
through a **content-addressed cache**: key = hash(model, full request),
value = the response, stored in a local cache directory shared across
branches and gitignored. Consequences:

- A branch that changes only retrieval replays ingest entirely from cache —
  $0 and fast.
- A branch that changes one prompt re-pays only the calls that prompt
  touches.
- Cache hits make LLM stages *deterministic* across reruns, so even fuzzy
  stages diff cleanly when unchanged.

(v1.0 has no model calls in ingest, so the cache matters from rung 1 on; it
ships with the harness so rung 1 lands into existing plumbing.)

### Scorecard

| metric | kind | gate |
|---|---|---|
| projection snapshots (event → graph mutations) | exact | **hard CI gate** |
| replay determinism (replay twice, diff DBs) | exact | **hard CI gate** |
| retrieval Recall@K / MRR over golden queries | exact, no LLM | **hard gate** on `smoke`/`standard` |
| graph shape: node/edge counts, type histogram, supersession counts | informational | drift detector |
| ingest throughput (episodes/sec), search latency p50/p95 | measured | trend |
| cost per episode / per session / per ask, by stage (from JSONL) | measured | trend |
| distillation quality (LLM-as-judge, pinned judge, temp 0) | fuzzy | **soft-track only** — threshold with tolerance, manual review on regression |

The hard/soft line is a Goodhart defense: the moment a number gates merges,
the system optimizes the number, so only human-labeled exact metrics gate.
Golden queries are curated from real usage and refreshed periodically so we
don't optimize for last year's questions. Model rollovers (Claude, OpenAI
embeddings) are handled the same way as code changes: branch, bench, diff,
ship if the numbers hold.

## Observability — JSONL accounting in agentkit

One JSON line per model call: `trace_id`, `stage`, model, usage (input /
output / cache-read / cache-write tokens), computed USD cost, latency, finish
reason — field names following the OpenTelemetry GenAI conventions. This
lands in `agentkit` (which already has `trace.Tracer` and `model.PricingSpec`)
so `prompts` and `dropbox` inherit it. Cost-by-stage is a read-time operation
over the log (`jq`, or a small `wiki cost` subcommand) — no parallel
telemetry table to keep in sync. `stage` is a fixed enum: `project_event`,
`distill_session`, `link_text`, `embed`, `ask`. The full forensic
request/response record (redacted, cache prefix logged once by hash) is a
debug flag, not always-on.

## Event-plane producer

The wiki becomes a producer of `wiki.fact.*` via the standard outbox —
designed in from day one (four integration points against `eventplane`;
cheap now, expensive to retrofit) even though nothing consumes it yet.
Disabled in bench mode per the determinism contract. The wiki remains a
consumer for the event-projection path; its consumed-event list is explicit
code (the promotion rule), not "everything on the plane."

## Markdown projection — dropped from v1

The earlier draft rendered the graph to markdown pages as a "greppable,
agent-navigable" surface. The agent reaches the wiki only through MCP, which
has no file verbs — the projection had no consumer. `search`/`get` are the
navigation surface. If a human-auditable rendering proves wanted, it returns
as a small, demand-justified follow-on; the graph-canonical principle means
it can be added at any time without migration.

## Scope

**v1.0 — the substrate and the harness.** Episodes; deterministic event
projection under the promotion rule; chunked FTS over text episodes;
`search`/`get`/`ask` + carried ingest verbs; supersession (two-timestamp)
model; recursive-CTE traversal; event-plane outbox; JSONL accounting in
agentkit; **the full bench harness** — datasets, replay, model-call cache,
scorecard, `bench-diff`, determinism CI gates — with `smoke` and `standard`
datasets and an initial golden query set.

**The ladder (each rung lands via branch + bench + diff):**

- v1.1 — session distillation (first LLM ingest stage, first user of the
  call cache and the judge track).
- v1.2 — free-text entity linking against the closed entity list.
- v1.3 — OpenAI embeddings + RRF hybrid retrieval, if Recall@K demands it.
- Later, only by scorecard: free-text entity creation, contradiction
  detection, community summaries, reranking, dataset anonymization tooling,
  markdown rendering, retention.

## Consequences

- The wiki's evolution becomes empirical: every proposed feature, prompt
  tweak, and model rollover is a branch with a scorecard diff. Simplicity is
  the null hypothesis additions must beat.
- Backups gain a second life: any backup is a replayable benchmark, and the
  restore path doubles as the dataset-extraction path — both get exercised.
- v1.0 ingest is deterministic and model-free: replays are byte-identical,
  CI is hermetic, benchmarks cost nothing to run. LLM spend begins at v1.1
  and is visible per-stage from day one.
- Retrieval is cheap for the calling agent (references, not bodies), which is
  what makes "query the wiki on every explore" viable.
- The graph stays small (state, not activity), which is what keeps recursive
  CTEs, brute-force vectors, and default search comfortable without ANN /
  pruning machinery.
- `agentkit` gains JSONL cost accounting all agent-backed services inherit.
- What we give up: no compiled knowledge graph over arbitrary free text in
  v1, and no transaction-time time-travel. Both have a re-entry path (the
  ladder; the episode log) if evidence ever demands them.

## Status

Proposed — supersedes the earlier graph-cascade draft after a review pass
(field evidence on LLM-extracted graphs, the CGO/sqlite-vec conflict, the
activity-vs-state volume problem) and a redesign around the benchmark
harness as the v1 target. Next: write the paired `wiki-rewrite-plan.md`,
phased per `docs/README.md`, with the ladder rungs as explicit follow-on
phases.

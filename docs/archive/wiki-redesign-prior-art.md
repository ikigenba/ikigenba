# wiki redesign — prior-art comparison

> Status: **research doc** — feeds the wiki redesign (`wiki-redesign-decisions.md`,
> `wiki/GOALS.md` are the ground truth for "our design"). Date: **2026-06-11**.
> Method: multi-agent web study — 24 comparable systems each given a structured
> deep dive across ten fixed dimensions (ingest, representation, identity
> resolution, update/merge, write-vs-read-time, read side, provenance,
> maintenance, ops/failure, maturity), then compared against the locked design.

## Executive summary

Our design sits at an unoccupied intersection of the landscape: nearly every
serious system agrees with us that LLM work belongs at write time, but almost
nobody compiles all the way to a finished, human-readable artifact — the field
compiles *indexes* (triples, fact stores, community summaries, typed memory
rows) and still re-assembles answers per query, while we compile *pages* that
are themselves the answer surface. Three of our pillars are strongly validated
by convergent independent evolution: write-time compilation (GraphRAG's thesis,
ChatGPT Dreaming's bitter-lesson no-RAG memory, MIRIX's benchmark wins,
Granola's enhance pass), the hosted one-call cited read agent (Granola's MCP
`query` tool, Honcho's Dialectic, R2R's agent endpoint, Cloudflare's `recall`),
and the durable acceptance/integration split (Honcho and Cognee built it;
Graphiti, Mem0, and Khoj demonstrate the failure modes of not having it). Our
single most distinctive mechanism is also the field's most glaring shared gap:
no surveyed system has our judge-once-then-string-lookup alias table — identity
is everywhere either exact-string matching (GraphRAG, LightRAG, R2R), an
embedding threshold (Mem0, HippoRAG), or a per-arrival re-judgment that caches
nothing (Graphiti, MIRIX, llm_wiki) — and the one rigorous precedent,
nomenklatura's persisted-judgment Resolver, validates the design almost point
for point while suggesting refinements (judgment provenance, reversible
merges). The most thought-provoking adverse findings: (1) Mem0 ran write-time
LLM conflict resolution at massive scale and reversed it in v3 because the
reconcile pass "was where context got destroyed" — the precise failure our
citation gate and contradiction sections exist to stop, so the gate must stay
strict; (2) cross-subject and temporal-span questions ("what are the themes,"
"what changed between March and May") are the query classes per-subject pages
answer worst — GraphRAG's communities, HippoRAG's PPR, and MIRIX's episodic
store all exist for them, and our only answer is the ask agent's navigation
plus `occurred_at`, which makes ask's quality genuinely load-bearing; (3)
anything that writes durable memory from third-party content inherits the
persistent prompt-injection attack class Tenable documented against ChatGPT
memory, and we have no hardening story yet; (4) nobody in the field has our
ops floor (bounded retries + DLQ + per-run cost ledger + one binary + one
SQLite) — that is a differentiator, not reinvention, and R2R's 14-container
stall is the cautionary tale for the alternative.

## The landscape

Seven families, each defined by its core architectural bet:

| family | systems | core bet |
|---|---|---|
| **Karpathy LLM-wiki lineage** | Karpathy gist, llm_wiki (nashsu), llm-wiki-agent (SamurAIGPT), Basic Memory | Prose markdown pages, compiled once and kept current by an LLM; intelligence in the agent (often the *caller's* agent), conventions over mechanism |
| **Agent memory layers** | Zep/Graphiti, Mem0, Letta, Honcho, A-MEM, MIRIX, Cloudflare Agent Memory | Memory as a service: extract atomic facts/observations from conversation at write time, store fact-granular, assemble context per query |
| **Graph-RAG indexing pipelines** | Microsoft GraphRAG, LightRAG, Cognee, HippoRAG 2, R2R | Spend LLM budget at index time building an entity/relation graph over retained chunks; answers still synthesized per query from graph-guided retrieval |
| **Per-query RAG second brains** | Khoj, txtai, Notion AI Q&A | Store faithful chunked copies of sources, zero write-time synthesis, re-derive every answer; the design we rejected, executed well |
| **Commercial closed memory/knowledge** | ChatGPT Dreaming, Notion, Granola, Cloudflare | Managed-scale datapoints: what the bets cost at planetary scale, where the market is converging |
| **Entity-resolution specialists** | nomenklatura, Splink | Identity as the whole product: persisted pairwise judgments (nomenklatura) vs calibrated batch probability (Splink); no knowledge content at all |
| **Academic memory research** | Generative Agents, A-MEM, HippoRAG 2, MIRIX | The published evidence base: reflection/consolidation ablations, typed-store benchmarks, the lazy-vs-eager compilation argument |

The deepest fault line in the landscape is not RAG-vs-anti-RAG (most systems do
write-time extraction now); it is **what gets compiled**. One pole compiles a
queryable *structure* (triples, vectors, typed rows) and defers synthesis to
read time; the other compiles a *finished artifact* (prose) and makes reads
cheap. Only the Karpathy lineage, ChatGPT Dreaming, and we sit on the second
pole — and within it, only we run the artifact maintenance as an unattended,
transactional, audited service rather than a human-supervised agent session.

## Dimension-by-dimension comparison

### 1. Ingest

The field default is **synchronous, LLM-in-the-request-path** ingestion:
Graphiti runs many LLM calls before an episode is "in" (its MCP server's only
queue is process-memory), Mem0 extracts inline in `add()`, A-MEM blocks
`add_note` on 2–3 LLM calls, Khoj's indexer 500s under load, GraphRAG/Splink
are operator-run batch programs. The systems that decoupled acceptance from
integration look like us: Honcho (synchronous LLM-free message writes +
Postgres-backed queue with jittered-backoff polling and token-threshold
batching), Cognee (cheap hashed `add()`, separate async `cognify`, per-dataset
serialization, pipeline-status idempotency), MIRIX (API/queue with Kafka or
in-memory backends), llm_wiki (persistent JSON serial queue), Cloudflare
(content-addressed idempotent ingest). Nobody combines our full set — durable
SQLite inbox, cheap transactional acceptance contract, event front doors, *and*
a cron-row-as-durable-batch-authorization trick. The field's failures here
(Graphiti's lost in-memory queue, Khoj's 500s, Letta's step-counter triggers
that can't absorb non-conversational feeds) are the strongest available
confirmation of the acceptance/integration decouple.

### 2. Representation

| shape | systems |
|---|---|
| fact/observation atoms + vectors | Mem0, Honcho, A-MEM, Cloudflare, MIRIX (typed rows) |
| graph (triples + entity nodes) over retained chunks | Graphiti, GraphRAG, LightRAG, Cognee, HippoRAG 2, R2R |
| faithful chunks of sources | Khoj, txtai, Notion |
| statement rows, assembled at read | nomenklatura |
| prose pages | Karpathy gist, llm_wiki, llm-wiki-agent, Basic Memory, Letta (blocks/files), Dreaming (one subject: the user), **us** |

Two field signals support the prose-page bet directly. Mem0 v3 *removed* graph
memory entirely, demoting entity links to a retrieval-ranking signal; Cognee's
top documented pain points (fragmented entity descriptions needing LLM
consolidation, undocumented merge semantics, duplicate entities) are precisely
the diseases the merge agent prevents. The honest cost, visible in Graphiti
and HippoRAG: a graph's structure powers retrieval tricks (graph-distance
rerankers, PPR multi-hop association) that pure prose forfeits — our ask
agent's navigation is the substitute, and the page-lead/match-excerpt
discipline is what keeps prose addressable. Note also that the page-shaped
systems closest to us (gist, llm_wiki, llm-wiki-agent) all *keep* source
pages, an index page, and a log — we deliberately killed all three, replacing
them with the inbox, the registry, and run rows; nothing in their experience
argues those page types compound (llm_wiki's own derived graph treats them as
leaf weight).

### 3. Identity resolution

This is where our design is most ahead of the field. The spectrum:

- **None at all**: Letta, Khoj, txtai, Basic Memory (identity = the wikilink
  string), Generative Agents (closed world), A-MEM, Honcho (peers are declared,
  third parties never become subjects).
- **Exact string match only**: GraphRAG `(title,type)`, LightRAG entity_name,
  R2R name-grouping, HippoRAG hash-of-lowercased-phrase (+ soft synonym edges).
- **Embedding threshold, no judge**: Mem0 (cosine ≥ 0.95), Cognee
  (deterministic name-keyed IDs + optional OWL fuzzy match).
- **LLM judge per arrival, judgment not cached as a fast path**: Graphiti
  (embedding shortlist + batched dedupe LLM every time; `IS_DUPLICATE_OF`
  edges record outcomes but don't short-circuit), MIRIX, llm_wiki (re-reads
  the index every ingest).
- **Persisted pairwise judgments**: nomenklatura's Resolver — the only true
  peer of our alias table, and production evidence (OpenSanctions 2026) that
  an LLM judge (98.95% F1) beats an 18-feature tuned regression matcher
  (91.33%), supporting both the small-LLM-judge choice and doubt=no_match.

Splink is the statistical counterpoint: calibrated per-field evidence,
term-frequency adjustments, fully decomposable judgments — overkill for
name-string-plus-claims inputs at our scale, but its lessons (common-name
agreement is weak evidence; judgment provenance; evaluated thresholds) are
listed in Findings.

### 4. Update / merge

Three field strategies: **append-only and let retrieval arbitrate** (Mem0 v3,
HippoRAG, Honcho pre-dream, Generative Agents, nomenklatura — contradictions
simply coexist), **invalidate/supersede with a timeline** (Graphiti's
bi-temporal `valid_at`/`invalid_at`, Cloudflare's topic-key version chains),
and **LLM rewrite in place** (Letta blocks, MIRIX managers, A-MEM evolution,
GraphRAG/LightRAG/R2R description re-summarization, Dreaming — all of which
rewrite *silently*, with no both-sided contradiction convention and no gate).
Nobody else does our combination: whole-page rewrite + corroboration-as-added-
citation + both-sided marked contradiction sections + a mechanical commit-time
citation-loss gate with declared supersession. The only mechanical rewrite
guards found anywhere are llm_wiki's 70% body-shrink rejection and locked
frontmatter fields — cruder cousins of the gate. Mem0's v3 retrospective
("the reconcile pass was where context got destroyed") and the CHI-2026/press
backlash against Dreaming's silent revision are the field's two loudest
warnings that unguarded LLM rewriting loses information and users notice; both
argue the gate is the most load-bearing safety mechanism in the design.

### 5. Write-time vs read-time

The field has converged toward hybrid: extraction at write, assembly at read.
Pure read-time RAG survives in Khoj, txtai, Notion — and Notion's two-year,
multi-system engineering arc (Debezium→Kafka→Hudi→Spark→Ray→Turbopuffer,
span-hash incremental re-embedding) is what refusing write-time compilation
costs at a 90%-update workload. Pure write-time conviction matching ours:
the Karpathy lineage, Dreaming (whole memory state injected, no retrieval at
all), and partially Letta's sleep-time compute (compiled blocks over a RAG
basement). The serious counterargument is economic: MSR's LazyGraphRAG
(~0.1% of index cost by deferring summarization) and HippoRAG's cheap-index/
lazy-association results both attack eager compilation — defensible for us
because personal-scale trickle ingest amortizes the cost per document and the
compiled page is read many times, but it is the bet to re-examine if ingest
volume ever turns batch-shaped. Mem0's v3 shift of *reconciliation* (not
extraction) to read time is the subtler datapoint: at huge scale, write-time
conflict resolution was the bottleneck. Our scale and our gate are the answer,
but it says the merge agent is where quality risk concentrates.

### 6. Read side

Two camps. **Primitives-first** (caller's agent runs the loop): Basic Memory,
llm-wiki-agent, the Karpathy gist, Graphiti OSS, Mem0, txtai, A-MEM, llm_wiki's
MCP surface. **Hosted answer** (server runs the loop, one call returns a
synthesized answer): Khoj chat/research, R2R `/retrieval/agent`, Honcho
Dialectic, Cognee `recall`, MIRIX Chat Agent, Cloudflare `recall`, Granola's
`query_granola_meetings`, Notion Q&A. The market's commercial pole is clearly
hosted — Granola exposing a hosted agentic query tool *as one MCP tool* is the
exact shape of our ask — which validates hosted-ask-first against the
primitives-first fashion in OSS. Khoj's published eval (42.0%→63.5% accuracy
from iterative retrieval) and HippoRAG's recognition-memory result (an LLM
relevance filter over candidates beats raw similarity) are the field's
evidence that ask's multi-step navigation is worth its cost. Citation
discipline in answers is rare: R2R's bracketed short-ID citations with span
extraction and GraphRAG's `[Data: ...]` references are the strongest peers;
most hosted answers (Honcho, MIRIX, Cloudflare, Cognee) return uncited or
coarsely-tagged prose. Our page-level answer contract with the read_source hop
underneath is stricter than anything surveyed except R2R's.

Reads-that-write is the other split: Karpathy, llm-wiki-agent, llm_wiki, and
Cognee file answers/interactions back; our ask-writes-nothing lock forfeits
that compounding deliberately, with the ingest-the-answer escape hatch already
in the design.

### 7. Provenance

Field norm is coarse: chunk- or episode-level lineage (Graphiti episodes,
LightRAG source_id lists that get truncated by `apply_source_ids_limit`,
Cognee chunk linkage, Honcho source_ids), or none (Mem0 facts untraceable to
utterances, Basic Memory, A-MEM, MIRIX discarding raw screenshots, Dreaming
deliberately disconnecting memory from conversation logs). Statement-level
inline citations to immutable arrivals plus a loss gate exists nowhere else.
The two rigorous alternatives are instructive: nomenklatura preserves
provenance *by never rewriting* (every statement row keeps dataset and seen
dates forever) — the lossless pole that puts the burden of our rewriting
stance squarely on the gate; and Generative Agents' `filling` evidence-ids on
reflection thoughts (recursively, thoughts citing thoughts) is the academic
ancestor of digest claims citing arrival ids, including the warning that
citation chains through derived layers need traversal if digests ever cite
digests (our citation-uniformity lock — every citation points at an arrival —
already forecloses that).

### 8. Maintenance

Mostly absent or monolithic. Absent: Mem0 (append forever), Khoj (mechanical
hash pruning only), Basic Memory (human gardening), R2R (a VACUUM cron),
HippoRAG (positioned as a feature). Monolithic: Letta's sleep-time agent (one
undirected "reflect and reorganize" prompt, overwrite-by-default), Karpathy's
single lint health-check prompt, Honcho's Dreamer (agentic consolidation with
delete tools and soft-delete as the only net), Dreaming (continuous silent
curation). The closest structural peers to our typed lint family: llm_wiki's
structural-vs-semantic lint split plus human review queue, llm-wiki-agent's
graph-aware mechanical checks, Cognee's memify pipelines, Graphiti's
caller-invoked community refresh. Nobody has the dup_flags pipeline (flag-only
writers → one judge → version-gated re-judging → transactional five-step merge
surgery) or the stale_notes channel; the dedicated-mechanism approach is a
genuine differentiator, and the field's duplicate-entity rot (LightRAG's
manual `amerge_entities`, Cognee's issue #1831) is what its absence costs.

### 9. Ops / failure

The field is bimodal: research libraries with no failure story (A-MEM,
HippoRAG, Generative Agents, txtai workflows that log-and-continue, Graphiti's
no-DLQ library) and heavy service stacks (R2R full mode ≈14 containers; Letta
FastAPI+Postgres+pgvector; Honcho Postgres+Redis+two processes with *no
retries and no DLQ*; Notion's dozen distributed systems). No surveyed system
has the complete set we locked: bounded retries with exponential jittered
backoff, dead-letter with human re-queue, per-attempt run rows with usage
accounting, optimistic page-version commits, and a one-binary-one-SQLite
footprint. Partial peers: Honcho's jittered queue polling, llm_wiki's 3x
retry + persistent queue, Basic Memory's 3-strike circuit breaker with
content-change auto-retry, Cloudflare's content-addressed idempotency, Letta's
provider_trace/step tables (the closest run-ledger analog). The R2R trajectory
— 7.9k stars, 14 containers, docs site dead, stranded adopters — is the
strongest argument in the corpus for the suite's one-static-binary stance.

### 10. Maturity

The fact-granular memory layer is a funded, crowded market (Mem0 $24M/58k
stars; Zep; Letta $10M; Cognee $7.5M; Honcho $5.35M; Cloudflare entering as a
platform primitive) and the commercial knowledge side is enormous (Notion,
Granola at $1.5B, ChatGPT memory at hundreds of millions of users). The
page-shaped lineage is young and thin: a two-month-old gist (however viral),
one popular desktop app (llm_wiki, pre-1.0, single-user), and template repos.
Nobody ships an unattended, multi-source, service-grade LLM wiki. That is
both the opportunity and the warning: the market is racing along the
"human-curated pages + RAG" ↔ "LLM-maintained pages" spectrum (Notion 3.x
agents now *edit* pages; Letta pivoted to git-backed memory files), and the
churn in the closest systems (Letta deprecating half its architecture mid-
pivot, Mem0 reversing its core algorithm) says the design space is not
settled — our decision-log discipline is the right posture.

## System profiles

### Karpathy LLM-wiki lineage

**Karpathy "LLM wiki" gist** — the origin spec: three layers (immutable raw /
LLM-authored wiki / schema doc), three operations (ingest/query/lint),
compile-once-keep-current as the explicit anti-RAG thesis. Deliberately
abstract; no queue, no identity mechanism, no enforcement, ops out of scope.
*Vs ours*: we are this pattern hardened into a service — inbox for its
conversational ingest, alias table for its per-session identity improvisation,
citation gate for its aspirational citations, typed lint jobs for its one
health-check prompt, hosted ask for its caller-context navigation. Its two
choices we rejected knowingly: answers filing back (our escape hatch is
ingest_text) and the user-tunable schema doc (we have no steering surface —
see Findings).

**LLM Wiki (nashsu/llm_wiki)** — the most popular implementation (~11.2k
stars): Tauri desktop app, Obsidian-compatible vault, persistent serial ingest
queue, two-step analyze-then-generate ingest, three-layer page merge with a
70% body-shrink rejection guard, structural+semantic lint feeding a human
review queue. *Vs ours*: closest philosophical sibling; its identity story
(LLM re-reads index.md every ingest, duplicates leak to a human queue) and
page-level-only provenance are exactly the gaps our alias table and citation
gate close; its human review queue and saved Query pages are the desktop-tool
answers to problems our unattended service must solve mechanically. Steal:
the cheap mechanical shrink guard as belt-and-suspenders beside the gate.

**LLM Wiki Agent (SamurAIGPT)** — the no-service pole: a repo template where
the user's own harness agent executes wiki conventions over a git folder.
Manual ingest, index-scan retrieval, page-level frontmatter provenance,
flag-and-report lint scripts, derived wikilink graph with Louvain-based
structural checks. *Vs ours*: same creed, opposite body; its gaps (no
idempotency, silent token spend, unguarded entity drift, ingest occupying the
user's context) validate the service bet. Steal: graph-shaped zero-LLM lint
signals over a derived link/citation graph.

**Basic Memory** — markdown-files-as-truth over MCP, SQLite index, hybrid
FTS5+vector search, all intelligence delegated to the client LLM; server runs
no LLM. *Vs ours*: surface twin, architecture inverted (intelligence
client-side at read time vs ours server-side at write time). No queue, no
identity beyond the wikilink string, no provenance, no merge semantics. Steal
candidates: the rebuild-from-canonical-data + `doctor` consistency story
(portability/recovery confidence), wikilink-style typed cross-page links, the
3-strike circuit breaker with content-change retry.

### Agent memory layers

**Zep / Graphiti** — the closest rival: write-time integration, LLM entity
resolution, bi-temporal fact invalidation, episode-level provenance — but into
fact triplets on a graph DB, with read-time assembly. Its 2026 Saga feature
(watermarked incremental stream re-summarization) independently converged on
our digest mode. *Vs ours*: identity judgments persist as edges but never
become a fast path (no alias-table equivalent — re-judged per mention); its
LLM-in-the-ingest-path with an in-memory MCP queue is the failure our inbox
prevents; its bi-temporal validity timeline enables point-in-time queries we
cannot do — `occurred_at` + dated claims is our partial answer.

**Mem0** — the adoption leader (58k stars, $24M): write-time fact extraction
into a vector store; v3 (Apr 2026) reversed write-time conflict resolution to
ADD-only because the reconcile pass destroyed context, and removed graph
memory. *Vs ours*: the most important single datapoint in the corpus — a
production-scale failure of exactly the operation our merge agent performs,
survived in our design only if the citation gate stays strict and superseded
content is declared (and ideally temporally qualified) rather than deleted.
Its thin provenance and 0.95-cosine identity validate our depth there.

**Letta (MemGPT)** — stateful agents with self-edited memory blocks + a RAG
archival basement; sleep-time compute is the published precedent for our
async-bigger-model integrator, and its tool split (only the background agent
may edit memory) prefigures ask-writes-nothing. Mid-pivot to git-backed
markdown memory files. *Vs ours*: no queue, no identity mechanism, no
citations, silent-overwrite merges; its pivot to plain files + git versioning
is the field's argument for page version history we chose not to keep (runs +
citations + the dup_flags audit are our answer — an honest tension).

**Honcho** — the closest write-side cousin: LLM-free synchronous acceptance
into a durable Postgres queue, jittered-backoff polling, token-threshold
batching (their digest analog), per-subject serialized work units, a hosted
read-only tool-loop Dialectic agent with a zero-LLM static side door. *Vs
ours*: memory stays an atom pile that their Dreamer must consolidate later —
the debt our merge pays at write time; no entity resolution at all, no answer
citations, no retries/DLQ. Steal candidates: surfacing corroboration strength
(their `times_derived`) to ask; tagging stated-vs-inferred claims; reasoning
tiers as an ask cost knob.

**A-MEM** — NeurIPS 2025 Zettelkasten memory whose "evolution" step (LLM
rewrites neighbor notes' metadata on every add) is the published validation
that write-time restructuring works — and a catalog of the failure modes we
guard against: destructive overwrites with no provenance, no identity
resolution, paper/code gaps, silently lost updates. Its ~1,200-token-per-
operation figure is a useful budget benchmark for the document pass.

**MIRIX** — six typed memory stores with per-type manager agents; the best
published benchmarks for write-time compression (LOCOMO 85.4%, 99.9% storage
reduction). *Vs ours*: validates compilation and queue decoupling empirically;
fragments subjects across typed rows that reads must re-fuse (the work our
merge does once); re-runs duplicate checking per insert with nothing persisted;
discards raw inputs (no provenance floor). Its episodic store is load-bearing
for temporal-span questions — the test our digests must pass (see Findings).
Steal: "active retrieval" (topic-inferred prefetch before the agent loop).

**Cloudflare Agent Memory** — memory as a managed network primitive (private
beta): two-pass extraction with an 8-check verifier, typed atoms in per-profile
Durable Object SQLite, five-channel RRF retrieval with an exact fact-key
channel, content-addressed idempotent ingest. *Vs ours*: striking convergence
on SQLite + RRF + exact-key-pin + verified write-time extraction; diverges on
representation (atoms, per-query synthesis, last-writer-wins supersession, no
visible provenance). Steal candidates: the detail pass for verbatim specifics,
raw-source search as a fused retrieval channel, deterministic temporal
arithmetic.

### Graph-RAG indexing pipelines

**Microsoft GraphRAG** — the canonical compile-at-index-time system and the
strongest published validation of the anti-RAG argument (chunk retrieval
cannot answer corpus-level questions). Batch, operator-run, exact-name entity
merging, conflicts silently blended, no maintenance. *Vs ours*: its
hierarchical community reports are the field's answer to cross-subject
"global" questions our per-subject pages handle worst; MSR's own LazyGraphRAG
is the standing economic critique of eager compilation, defensible at our
trickle-ingest scale.

**LightRAG** — incremental set-mergeable graph (the anti-rebuild economics
match our merge pass) with exact-string identity, capped-and-truncated
provenance, and read-time synthesis. *Vs ours*: validates incremental
write-time integration and content-hash enqueue idempotency; its
`adelete_by_doc_id` → rebuild-affected-knowledge path is the retraction story
we lack (see Findings); dual-level keyword extraction is a cheap
query-understanding trick for ask.

**Cognee** — graph+vector ECL pipelines with the field's best
ingest/integration decoupling outside ours (hashed add, async cognify,
pipeline-status idempotency) and a hosted one-call recall. *Vs ours*: its
documented diseases (fragmented entity descriptions, undocumented merge
semantics, duplicates absent an ontology) strengthen the anti-graph bet; its
TEMPORAL retrieval mode and read-side feedback loops are demand signals worth
noting (a lightweight answer-feedback path into stale_notes would capture the
value without cached syntheses).

**HippoRAG 2** — the cleanest steelman of the opposite bet: cheap mechanical
indexing (one OpenIE call per passage), association computed lazily per query
via Personalized PageRank, an LLM recognition-memory filter as the only online
judgment. *Vs ours*: its multi-hop results say cross-page association is the
genuinely hard read-side problem for compiled pages — ask's navigation is our
PPR substitute and must be good; its recognition-memory win validates giving
ask judgment over RRF rank order; its entity-to-chunk reference counting makes
retraction tractable.

**R2R** — production-shaped agentic RAG whose read side is nearly ours
(hybrid FTS+vector RRF, hosted tool-loop agent, bracketed citations with span
tracking, raw-bytes retention) bolted onto a chunk/graph write side nobody
curates; exact-name dedup, conflicts smoothed by prompt. Project stalled
(no release since mid-2025, docs dead) under a 14-container deployment — the
ops cautionary tale. Steal: citation-span offset tracking; context-enriched
extraction supporting self-contained claims.

### Per-query RAG second brains

**Khoj** — the design we rejected, executed maturely (35k stars, YC): faithful
chunk mirror, zero write-time synthesis, hash-diff re-indexing, server-side
iterative research agent. *Vs ours*: its eval numbers justify ask's iteration;
its effortless source-*mutation* handling (edit a file, stale chunks die) is a
question our compile-once pages must answer for the Dropbox-modify path; its
synchronous-indexing failures validate the inbox.

**txtai** — our retrieval substrate without our knowledge layer: SQLite
content rows + BM25 + dense vectors + hybrid fusion in one embeddable
artifact. *Vs ours*: validates the storage shape at our scale; its
research-backed default of convex-combination fusion over RRF (arXiv
2210.11934) earns a small eval before RRF is locked-for-good; its zero-cost
mechanical similarity graph (embedding edges + Louvain topics) is a cheap
related-pages layer compatible with prose pages.

**Notion AI Q&A** — the canonical foil: identical surface promise (one
question → cited synchronous answer over curated prose pages) on the inverted
architecture (humans maintain pages, RAG re-derives everything). Its
multi-year index-cost war is the price of that inversion. Steal candidates:
span-level content hashing for incremental re-embedding; permission filtering
at the retrieval layer if the wiki ever becomes multi-principal. Its 3.x pivot
to page-editing agents is the market racing toward our bet.

### Commercial closed memory/knowledge

**ChatGPT memory (Dreaming V3)** — the largest deployment of our two most
contrarian bets: no RAG, no vectors, background prose compilation with
temporal revision, whole state injected per chat. Equally large-scale evidence
for our discipline: silent revision with no provenance drew academic and press
condemnation, and Tenable demonstrated persistent-memory prompt injection —
the attack class any third-party-content-to-durable-memory pipeline inherits,
including ours (see Findings).

**Granola** — $1.5B meeting-notes vertical validating write-time compilation
per document, a hosted cited Q&A as a single MCP tool, and citations that
always deep-link to source. Compounding stops at the document: People/
Companies are link indexes, cross-meeting synthesis is per-query RAG — exactly
the second-order compilation our digests perform. Its black-vs-gray visual
attribution of human-vs-AI text is a legible-trust trick (corroborated vs
single-source statements could get similar treatment in pages).

### Entity-resolution specialists

**nomenklatura** — the production precedent for the alias table: pairwise
judgments (including NEGATIVE) persisted forever in one SQL table, decided
once, resolved by lookup; blocking-then-judge candidate generation; 2026
production data showing an LLM judge beating a tuned statistical matcher.
*Vs ours*: three refinements worth adopting are in Findings (negative-judgment
persistence beyond dup-dismissals, reversible merge bookkeeping, judgment
provenance on alias rows). Its never-compress statement model is the lossless
alternative whose existence justifies our gate.

**Splink** — Fellegi-Sunter probabilistic linkage at national-statistics
scale: calibrated probabilities from explicit per-field evidence, term-
frequency adjustments, waterfall-chart judgment audits, evaluation-stage
threshold discipline. Wrong tool for single-name-plus-claims inputs, but its
chronic cluster-ID instability across re-runs validates caching judgments
durably, and its evidence discipline sets the bar for auditing ours.

### Academic memory research

**Generative Agents (Smallville)** — the canonical observe→score→reflect→
retrieve architecture; reflection (batch compilation of low-level observations
into evidence-citing higher-level thoughts) is the ablation-validated academic
ancestor of our digest. Its salience-budget reflection trigger is the one
direct challenge to a locked decision: we locked schedule-only digests with no
volume trigger (see Findings). Append-only forever, no identity, no ops —
a lab artifact, but the most-cited one.

## Findings worth our attention

### Should make us reconsider or stress-test

1. **The merge agent is the design's concentrated quality risk — keep the gate
   strict and bias toward temporal supersession (Mem0, Dreaming).** Mem0
   abandoned write-time LLM reconciliation at scale because it destroyed
   context; Dreaming's silent revision is its most-criticized property. Our
   mitigations (citation gate with declared supersession, both-sided
   contradiction sections) target exactly this, but the lesson sharpens two
   riders for exact-prompts: the merge prompt should prefer rewriting
   superseded statements with temporal qualifiers ("until 2025, …") over
   dropping them with a declaration, and the gate's `superseded` reason lines
   should be treated as reviewed output, not formality.

2. **Cross-subject and temporal-span questions are our weakest query classes
   (GraphRAG, HippoRAG 2, MIRIX).** Communities, PPR, and episodic stores all
   exist because per-subject artifacts answer "what are the themes" and "what
   changed between March and May" poorly. Our answers are ask's multi-tool
   navigation, `occurred_at` on event subjects, and cross-links the merge
   agent happens to write. Action: the eval harness needs golden questions of
   exactly these shapes (multi-hop association, date-range narrative), because
   they will fail first; verify compile preserves enough event-time ordering
   in digest claims for the date-range class; and HippoRAG's recognition-
   memory result says ask should explicitly judge candidate relevance rather
   than trusting RRF order — a prompt obligation for section 3 of the ask
   prompt.

3. **Prompt-injection hardening is a missing design item (Dreaming/Tenable).**
   Tenable demonstrated cross-session exfiltration via content that writes
   itself into persistent memory. Every front door of ours feeds third-party
   content into LLM calls whose outputs become durable pages served back into
   agent contexts. Extract/compile/merge prompts and the commit path need an
   explicit injection posture (content-is-data framing, no tool access in
   extract/compile is already protective, but merge is an agent and ask serves
   page text verbatim). This deserves a section in the design doc; nothing in
   the current decision log addresses it.

4. **The volume-trigger question, re-raised with evidence (Generative
   Agents).** We locked "no volume trigger — batch runs on schedule, full
   stop." Smallville's salience-budget trigger (fire consolidation when
   accumulated importance crosses a threshold) is ablation-supported and
   adaptive to bursty streams; a 500-contact CRM import waits up to a day
   under our lock. The lock's simplicity rationale stands, but this is the
   strongest external evidence against it — worth re-checking once real
   event-volume distributions exist, since the change would be one dispatcher
   clause, not a redesign.

5. **Retraction has no story (LightRAG, HippoRAG).** Corrections enter
   through the front door, but *expunging* an arrival (legal/privacy: "remove
   that document and everything derived from it") is unanswerable today: the
   citation discipline means affected statements are findable
   (`LIKE '%[id]%'`), but no job removes them and recomputes pages from
   surviving evidence. LightRAG's delete-then-rebuild-affected and HippoRAG's
   reference counting show the shape. A lint-style `expunge` job is contained
   future work; the design doc should at least name it.

6. **Alias rows should carry judgment provenance (Splink, nomenklatura).**
   Every nomenklatura edge records user/score/timestamps; every Splink link
   decomposes into evidence. Our alias rows record only (norm, subject_id) —
   a wrong match's "why" is unrecoverable. Cheap fix at schema finals: a
   `created_by_run` column on aliases (the run row already holds the match
   call), and have match's output include a one-line rationale stored in the
   run record. Same spirit: Splink's term-frequency lesson says the match
   judge's riskiest errors cluster on common names — the prompt could receive
   alias-frequency stats from our own registry ("`bob smith` matches 3
   subjects") as evidence.

7. **Merge surgery is irreversible; nomenklatura's is not.** Hard-delete of
   the loser with the dup_flags row as audit is clean, but a wrong merge can
   only be undone by hand-reconstructing the loser from citations. The
   Resolver's edges + connected components + referents forwarding make
   merge/split reversible. Not worth their machinery at our scale, but the
   fold's pre-merge page bodies are about to be destroyed — persisting the
   loser's final body in the dup_flags row (or run record) at merge time is a
   one-column insurance policy worth taking to schema finals.

8. **Two cheap extraction robustness tricks (Cloudflare).** Their detail pass
   exists because single-pass extraction loses verbatim specifics (names,
   prices, versions) — a golden category for extract, and if goldens confirm
   the failure, a contained second pass behind the same contract. And their
   regex/arithmetic temporal resolution suggests extract's relative-date
   resolution could be deterministic-assisted (compute candidate dates
   mechanically from `received_at`, let the model pick) rather than pure
   model arithmetic.

9. **Fusion choice deserves one eval (txtai).** txtai defaults to convex
   combination of normalized scores over RRF, citing arXiv 2210.11934. Our
   retrieval goldens are already a deliverable; add lexical/RRF/convex as a
   three-way comparison before RRF k=60 calcifies.

10. **Pure prose forfeits cheap structure (Basic Memory, SamurAIGPT, txtai).**
    Wikilinks/typed relations give cross-page navigation and zero-LLM
    structural lint (orphans, fragile bridges, isolated clusters) nearly free.
    Our pages cite arrivals but don't link each other; ask navigates by
    search, not links. A derived link graph over page-to-page mentions (the
    registry makes mentions resolvable) could power lint signals and "see
    also" without becoming the representation — contained, additive, and
    three independent systems found it worth having.

11. **No user-tunable steering surface (Karpathy gist).** The schema doc — a
    co-evolved conventions file that adapts the wiki's behavior per domain —
    is load-bearing in the original pattern and absent in ours: our prompts
    are fixed internals. GOALS.md originally carried this idea ("taxonomy is
    schema-driven"). Worth an explicit decision: either a per-box prompt-
    extension config (a "house conventions" block appended to extract/merge)
    or a recorded rejection.

### Locked decisions the field's experience validates

- **Acceptance decoupled from integration, durable inbox.** Graphiti's
  in-memory MCP queue, Mem0's inline extraction, Khoj's 500s, A-MEM's blocking
  adds are the documented failure modes; Honcho/Cognee/MIRIX built our shape
  independently. The cron-row-as-durable-authorization trick additionally
  handles backfill/restart cases Graphiti's Saga needed special watermarks for
  (our stamp-by-id-list sweep is time-agnostic by construction).
- **The alias table (judge once, string lookup forever).** No surveyed system
  has it; every graph system pays for its absence in duplicate nodes; Zep
  re-judges per mention; nomenklatura proves the persisted-judgment pattern in
  production and OpenSanctions' data supports the LLM judge and doubt=no_match
  polarity (false merges poison; their auto-merge threshold + review queue is
  the same asymmetry).
- **Prose pages, not a graph.** Mem0 deleted graph memory in v3; Cognee's
  worst pain is entity-description fragmentation; R2R's write-time graph is
  "an expensive index nobody curates"; GraphRAG's identity story is exact
  string match. The structure-side costs we accepted (no graph rerankers, no
  PPR) are real but named.
- **Citation gate + both-sided contradictions.** Mem0's "where context got
  destroyed," Dreaming's audit-trail backlash, R2R's "favor the most specific
  source" silent smoothing, and the total absence of any rewrite gate anywhere
  else (llm_wiki's 70% heuristic is the field's best) make this the design's
  clearest moat.
- **Hosted ask, read-only, synchronous, one call.** Granola ships exactly this
  shape as its MCP surface; Honcho/R2R/Khoj/Cloudflare run the loop
  server-side; Letta's only-the-background-agent-edits split mirrors
  ask-writes-nothing; the synthesis-page rot we predicted is observable in
  every system that caches syntheses without a repairer. Khoj's eval numbers
  justify the iteration cost.
- **Digest mode.** Graphiti's Saga and Generative Agents' ablation-validated
  reflection independently converged on batch compilation of event streams
  into durable aggregate claims with evidence ids.
- **Zero-LLM search side door returning whole pages.** Honcho's static
  endpoints, Cognee's CHUNKS_LEXICAL, Granola's list/get tools — the
  hosted-answer systems all kept one; whole-curated-page returns are uniquely
  available to us because pages are the compiled artifact.
- **Hybrid FTS+vector with RRF and exact-alias pin.** Cloudflare's five-channel
  RRF with an exact fact-key channel is independent convergence on the same
  recipe, alias-pin included.
- **Bounded retries + DLQ + run ledger + one binary + SQLite.** Nobody else
  has the set; Honcho (no retries, terminal errors), Graphiti (no DLQ), R2R
  (retries=0, 14 containers, stalled) and Notion's index-cost war show both
  halves — the failure semantics and the footprint — are differentiators.
- **Killing source pages, index.md, and the journal.** llm_wiki/SamurAIGPT
  keep them and their value shows up nowhere in their operation; our registry,
  inbox, and run rows cover the same needs transactionally, and Letta's
  filetree-in-prompt navigability trick is replaceable by registry-derived
  views on demand.
- **`occurred_at` + claims carrying their own dates.** Graphiti's bi-temporal
  model and Cognee's TEMPORAL mode confirm time-aware queries are real demand;
  our lighter mechanism covers the common cases (point-in-time *validity*
  queries remain out of scope, knowingly).

## Sources

Consolidated from the per-system deep dives:

- https://github.com/getzep/graphiti · https://arxiv.org/html/2501.13956v1 · https://help.getzep.com/graphiti/getting-started/overview · https://help.getzep.com/concepts · https://www.getzep.com/ · https://www.getzep.com/pricing/
- https://github.com/mem0ai/mem0 · https://arxiv.org/abs/2504.19413 · https://mem0.ai/blog/mem0-the-token-efficient-memory-algorithm · https://docs.mem0.ai/migration/oss-v2-to-v3 · https://github.com/mem0ai/mem0/pull/4805 · https://techcrunch.com/2025/10/28/mem0-raises-24m-from-yc-peak-xv-and-basis-set-to-build-the-memory-layer-for-ai-apps/
- https://github.com/letta-ai/letta · https://www.letta.com/blog/sleep-time-compute · https://docs.letta.com/ · https://www.letta.com/blog/our-next-phase · https://www.letta.com/blog/context-repositories/
- https://github.com/microsoft/graphrag · https://microsoft.github.io/graphrag/ · https://arxiv.org/abs/2404.16130 · https://github.com/microsoft/graphrag/issues/741
- https://github.com/HKUDS/LightRAG · https://pypi.org/project/lightrag-hku/ · https://deepwiki.com/HKUDS/LightRAG
- https://github.com/topoteretes/cognee · https://docs.cognee.ai/ · https://github.com/topoteretes/cognee/issues/1831 · https://www.cognee.ai/blog/cognee-news/cognee-raises-seven-million-five-hundred-thousand-dollars-seed
- https://github.com/plastic-labs/honcho · https://honcho.dev/docs/v2/documentation/core-concepts/architecture · https://deepwiki.com/plastic-labs/honcho
- https://github.com/basicmachines-co/basic-memory · https://docs.basicmemory.com/ · https://basicmemory.com/
- https://github.com/khoj-ai/khoj · https://docs.khoj.dev/ · https://blog.khoj.dev/posts/evaluate-khoj-quality/
- https://github.com/SciPhi-AI/R2R · https://railway.com/deploy/r2r
- https://github.com/neuml/txtai · https://neuml.github.io/txtai/ · https://github.com/neuml/txtai/releases/tag/v9.0.0
- https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f
- https://github.com/nashsu/llm_wiki · https://deepwiki.com/nashsu/llm_wiki
- https://github.com/SamurAIGPT/llm-wiki-agent
- https://openai.com/index/chatgpt-memory-dreaming/ · https://www.techtimes.com/articles/317840/20260605/chatgpt-memory-dreaming-update-openai-rewrites-personalization-engine-limits-audit-trail.htm · https://www.shloked.com/writing/chatgpt-memory-bitter-lesson
- https://www.notion.com/help/guides/get-answers-about-content-faster-with-q-and-a · https://www.notion.com/blog/two-years-of-vector-search-at-notion · https://www.notion.com/releases/2025-09-18 · https://www.notion.com/releases/2026-02-24
- https://www.granola.ai · https://docs.granola.ai/help-center/sharing/integrations/mcp · https://www.granola.ai/blog/series-c · https://techcrunch.com/2026/03/25/granola-raises-125m-hits-1-5b-valuation-as-it-expands-from-meeting-notetaker-to-enterprise-ai-app/
- https://github.com/joonspk-research/generative_agents · https://arxiv.org/abs/2304.03442 · https://arxiv.org/pdf/2411.10109
- https://github.com/opensanctions/nomenklatura · https://arxiv.org/abs/2603.11051 · https://www.opensanctions.org/docs/identifiers/ · https://www.opensanctions.org/docs/statements/
- https://github.com/moj-analytical-services/splink · https://moj-analytical-services.github.io/splink/
- https://github.com/WujiangXu/A-mem · https://arxiv.org/html/2502.12110v11 · https://github.com/WujiangXu/A-mem-sys
- https://arxiv.org/abs/2502.14802 · https://github.com/OSU-NLP-Group/HippoRAG
- https://arxiv.org/abs/2507.07957 · https://github.com/Mirix-AI/MIRIX · https://docs.mirix.io
- https://blog.cloudflare.com/introducing-agent-memory/ · https://www.infoq.com/news/2026/04/cloudflare-agent-memory-beta/ · https://developers.cloudflare.com/agent-memory/concepts/how-agent-memory-works/
- Fusion evidence: https://arxiv.org/abs/2210.11934 (convex combination vs RRF, via txtai)

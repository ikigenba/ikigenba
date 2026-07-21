# wiki — Research

**Status: informational, non-contractual.** This doc feeds the author of
`project/design/README.md` and nothing downstream consumes it (the autonomous
build reads only product, design, plan). It is a single coherent statement of
current research — edited in place as the goal evolves, never appended to.

**Goal of this research.** The wiki service is fully built; `ask` today is
deliberately narrow. This research scopes the **planned, previously-postponed
retrieval upgrade**: replace exact-subject-name lookup with a **fuzzy hybrid
search over pages** (OpenAI embeddings + FTS5/BM25, fused), so a question finds
the right pages even when it does not name a subject verbatim.

> This doc **replaces** the earlier phase-1 *build* research (scaffolding the
> service over appkit/agentkit, porting from `wiki.bak`). That work shipped; its
> research no longer informs the current goal. The two still-load-bearing pieces
> of it — the `Retriever`/`Hit` seam (§8) and the anti-collapse invariant (§10) —
> are carried forward below. Git history holds the rest.

---

## 0. The frame (decided with the owner before research)

These bound everything below; the research operated inside them.

1. **This is the postponed retrieval work, now scheduled** — not a reversal of
   product. The "no fuzzy/semantic matching" lines in `product.md` were "not
   yet," and this is the "yet." `product.md` will be re-authored after design.
2. **Relax retrieval; keep the integrity guarantees.** The three guarantees that
   give the wiki its trust contract stay: answers drawn **only** from ingested
   wiki content, **every answer cited**, and an honest **"nothing here"** when
   retrieval genuinely finds nothing. Only the *matching* step (exact-name →
   fuzzy hybrid) changes.
3. **Pages are the search surface.** The compiled, deliberately-lossy per-subject
   pages (≤12,000 chars) are what we search — not raw claims, not source text.
   Lossiness is the point of the wiki.
4. **Hybrid = OpenAI embeddings (semantic) + FTS5/BM25 (keyword), fused.** A
   **local embedding model is ruled out, permanently.** An external embedding API
   is accepted; the owner chose **OpenAI**.
5. **Query-side decomposition is for the research to settle** — whether to extract
   subjects, claims, expand, or just embed the raw question. (Settled in §6.)
6. **Retrieval evaluation is out of scope** for this work — a later effort. Design
   the mechanism; do not build a recall/precision harness now. (Leave the seam
   clean so it can land later — the `extract` eval harness in `internal/eval/` is
   the template to copy when it does.)
7. **Scale ceiling: 100,000 pages.** SMB knowledge base; typical far less. One
   query at a time, low QPS. Every storage/latency number below is sized to this
   ceiling.

---

## 1. Where `ask` is today, and what changes

`internal/ask/ask.go` — current flow (`Ask`, lines 77–117):

1. **`ask-subject`** call (`extractPrompt`): LLM extracts literal subject *names*
   from the question.
2. **`gatherPages`** (lines 145–187): each name → `Resolver.ResolveByName`
   (**exact normalized name**) → `PageStore.GetBySubject` (one page per subject,
   deduped). An unresolved name contributes nothing.
3. Zero pages → `honestEmpty()` (no synthesis call).
4. **`ask-synthesis`** call (`synthPrompt`): answer from the gathered page bodies
   only, returning `{found, text, citations}`; `validateCitations` (lines
   214–231) requires every citation to map to a gathered page.

**Reuse / rebuild map** (from the codebase agent):

| Component | Disposition |
|---|---|
| `Answer` / `Citation` types | **Keep** — return contract unchanged. |
| `honestEmpty()` gate | **Keep** — but its trigger moves from "0 names resolved" to "0 hits clear the relevance floor" (§7). |
| `ask-synthesis` call + `validateCitations` | **Keep** — citations still resolve to pages; only *which* pages enter the candidate set changes. |
| `gatherPages` (exact-name resolution) | **Rebuild** — replace with a `Retriever` (§8) returning `[]Hit`; drop name-only resolution. |
| `ask-subject` extract call | **Repurpose** — into query analysis (§6), not deleted. |

**Hook points** (file:line, for the design to target):
- Replace retrieval: `internal/ask/ask.go:145–187` (`gatherPages` → consume
  `[]Hit` from a `Retriever`).
- Re-embed a page on write: `internal/wiki/service.go` immediately **after**
  `pages.Upsert(...)` inside the ingest integrate tx (~line 334) **and** in
  `mergeSubjects` (~lines 469–533) — but the network call goes *after commit*,
  not inside the tx (§3).
- New call sites: `internal/wiki/config.go:17–22` (`CallSites` struct +
  `resolveCallSite`).
- Widen `llm_calls.stage` CHECK: a **new** table-rebuild migration (§9).

**Build-loop guard (process constraint).** Per `wiki/CLAUDE.md`, none of this is
hand-coded in an interactive session. Everything lands as design Decisions
(`project/design/DNN.md` + INDEX) and plan phases (`project/plan/phase-NN.md` +
STATUS line); the ralph loop builds it. Migrations are forward-only and
immutable — never edit `20260622001058_drop_pages_fts.sql` or the existing
`llm_calls` migrations; add new ones.

---

## 2. Embedding model facts (execution now via prompts `/embed` — §12)

Embedding *execution* has moved to the prompts service (§12); the model-level
facts below remain the evidence behind the D30/D32/D34 choices:

- **Vectors come back L2-normalized** from `text-embedding-3-*`
  → **cosine similarity = plain dot product.** Store them as-is; no wiki-side
  normalization needed.
- **`float32`** throughout.
- **Roles**: `document` for page bodies, `query` for the question —
  `text-embedding-3` is trained for this asymmetry; use it.
- **Dimension reduction is supported** (a `dimensions` request field); OpenAI
  shortens server-side (Matryoshka). 1536→512 ≈ 99% retrieval quality.
- **Batching**: inputs are batched per request, order preserved.

### Model & dimension choice
| Model | Native dims | Max input tok | Price (USD/M tok) |
|---|---|---|---|
| `text-embedding-3-small` | 1536 | 8192 | $0.02 |
| `text-embedding-3-large` | 3072 | 8192 | $0.13 |

**Recommendation: `text-embedding-3-small` reduced to 512 dims.** ~99% of native
quality at a third of the memory and ~6× lower price than `-large`. Make
**model + dims a configured call-site knob** (§9) and store `model@dims` with
every vector so a future swap is a re-embed, not a schema break. Embedding 100k
pages of ~1–2k tokens each ≈ 100–200M tokens ≈ **$2–4 one-time** on
`-small` — cost is a non-issue; recall quality drives the choice.

---

## 3. Vector storage & search — brute-force cosine in Go

**Recommendation: store vectors as float32 BLOBs in SQLite; brute-force cosine
(dot product) in Go.** No extension, no ANN index.

### The numbers that decide it (100k ceiling)
| dims | RAM (100k × 4B) | brute-force latency / query |
|---|---|---|
| 3072 (3-large native) | 1.23 GB | ~3–15 ms |
| 1536 (3-small native) | 614 MB | ~3–8 ms |
| **512 (recommended)** | **205 MB** | **~1–5 ms** |
| 256 | 102 MB | ~1–3 ms |

Brute-force flat scan stays adequate to **~1M vectors**; at 100k we are **~10×
under**, at 512 dims ~30× under. Per-query cosine time is dwarfed by the OpenAI
query-embedding round-trip (tens–hundreds of ms) anyway. ANN only earns its
complexity past ~500k–1M vectors *or* high QPS — neither applies.

### `sqlite-vec` — ruled out (for the right reason)
`asg017/sqlite-vec` is a C extension; its Go binding needs **CGO**, but wiki's DB
driver is **`modernc.org/sqlite` (pure-Go, CGO-disabled)** and cannot load it.
(A WASM path exists but buys nothing at this scale.) Brute-force in Go sidesteps
the whole question and keeps the static-binary build clean. A pure-Go HNSW
(`coder/hnsw`) exists but is unnecessary index-maintenance complexity at 100k.

### Storage shape
A **side table**, not a column on `pages` (pages are 1:1 with subjects;
`pages.id == subject_id`):
```sql
CREATE TABLE page_embeddings (
    subject_id   TEXT PRIMARY KEY,   -- == page id
    model        TEXT NOT NULL,      -- e.g. "text-embedding-3-small"
    dims         INTEGER NOT NULL,   -- e.g. 512
    vec          BLOB NOT NULL,      -- little-endian float32, L2-normalized
    content_hash TEXT NOT NULL,      -- sha256(title\nbody) at embed time
    updated_at   TEXT NOT NULL
);
```
Side table keeps the hot page-read path narrow, makes "missing vector" a trivial
`NOT EXISTS`, and lets a model/dims swap be a re-embed rather than a schema break.
Vectors and the FTS index live in the **same SQLite file**, so appkit's
`VACUUM INTO` backup/restore snapshots them atomically — **no special handling**.

### Lifecycle — embed *after* commit, never in the tx
The ingest integrate tx (`service.go`, ~lines 297–356) is deliberately
local-only and fast; the expensive compile already runs **before** `BeginTx`.
Putting an OpenAI call inside the tx would hold SQLite's single writer
(`SetMaxOpenConns(1)`) open across a network round-trip and break the
"reads never block on ingest" promise. So:

- **FTS5 sync stays inside the tx** (pure-local SQLite write — see §4).
- **Embedding happens after commit**, via a **catch-up worker**: select pages
  whose `page_embeddings` row is missing, whose `content_hash` ≠ current
  `sha256(title\nbody)`, or whose `model@dims` ≠ configured — and (re-)embed them
  out of band. `hashText`/sha256 already exists in `data_model.go`. (Prior
  research assumed a `pages.version` column for staleness; **there is none** —
  use `content_hash`.)
- Same after-commit treatment for the **merge** page rewrite.

### Backfill (on first ship)
- **FTS5**: backfill deterministically in the migration itself —
  `INSERT INTO pages_fts(pages_fts) VALUES('rebuild');` (pure SQL, no network).
- **Vectors**: no migration (no network in migrations). Every existing page
  starts with a missing/mismatched `content_hash`, so the steady-state catch-up
  worker **drains the backlog on first boot** with no new verb. (If an operator
  wants an explicit trigger, the existing `rerun` machinery is the closest fit,
  but lazy catch-up suffices and never blocks `ask` on a cold vector.)

---

## 4. The keyword lane — re-add FTS5

FTS5 `pages_fts` existed (phase-02 data model) and was **deliberately dropped**
(`20260622001058_drop_pages_fts.sql`) once `ask` became exact-name-only and
nothing consumed keyword search — eliminating tested-but-unreachable code and the
in-tx FTS-sync trap. This upgrade **re-introduces it**, paying that cost back on
purpose.

- Recreate as an **external-content** FTS5 table over `pages(title, body)`
  (`content='pages'`), as before.
- **Sync explicitly in the same tx as each page write** (the phase-02 comment
  intended exactly this): on UPDATE, issue the FTS5 `'delete'` with the **OLD**
  title/body read *before* the row update, then re-insert. `wiki.bak`'s
  `ftsPhrase()` / external-content sync is the reference to copy verbatim.
- **Sanitize the query**: wrap user terms as quoted FTS5 phrase literals
  (`"` → `""`), OR them together — both escapes operators/injection and widens
  lexical recall across aliases/synonyms.

BM25 earns its place in the hybrid: it catches exact/rare terms, identifiers, and
out-of-vocabulary tokens that embeddings smear, and it is nearly free.

---

## 5. Fusion — Reciprocal Rank Fusion

**Recommended pipeline:**
```
query ─┬─ FTS5/BM25  → top 60 (list A)
       └─ cosine     → top 60 (list B)
              └─ RRF fuse (k=60) over the union
                     └─ relevance floor (§7)
                            └─ final K = 8 pages → ask-synthesis
```

- **RRF over weighted score fusion.** `score(p) = Σ_lanes 1/(k + rank_lane(p))`,
  **k = 60** (the Cormack-2009 sweet spot; the default in OpenSearch, Elastic,
  Azure AI Search, Weaviate, Mongo Atlas). BM25 is unbounded and cosine is
  [-1,1] — they share no axis, so rank-based fusion needs no normalization and no
  labeled tuning data, and empirically beats linear fusion. Keep a `WIKI_RRF_K`
  knob.
- **Parameters**: ~**60 candidates per lane**, final **K ≈ 8** pages (range 5–12;
  8 × 12k cap ≈ 96k chars, comfortable for a Claude synthesis context). Make
  candidate-count and K config knobs.
- **Reranking: skip.** No dedicated rerank model (only Anthropic + OpenAI
  embeddings); a separate LLM-reranker call is cost/latency for marginal gain
  once RRF has produced a clean top-8 from same-corpus dual retrieval. If ever
  needed, the cheap path is to **fold it into synthesis** — pass the fused top
  ~12 and let the single synthesis call select/cite what it uses (zero extra
  calls). Add a true two-stage reranker only if (future) eval shows fusion
  surfacing junk.
- **Registry-first option** (from prior research, still apt): pin an exact
  normalized-name match at rank 1 when the question names a subject verbatim,
  then let the hybrid fill the rest — preserves today's precise behavior as a
  special case of the new path.

---

## 6. Query-side strategy

**Recommendation: keep one LLM query-analysis call (you already pay for it), but
change what it emits, and send *different* text to each lane.**

- **Repurpose the `ask-subject` call into query analysis.** Have it emit a small
  JSON: `{ sub_queries: [...], keywords: [...], aliases: [...] }`. The highest-ROI
  thing it does is **decomposition**: the corpus is **one page per subject**, so a
  "compare X and Y" question must fan out to X's page and Y's page separately — a
  single blended query embedding sits between them and matches neither. Cap
  sub-queries at 3–5 (redundancy outweighs recall past ~5).
- **Two different query strings — the key construction detail:**
  - **Dense (embedding) lane** ← the **full natural-language** question / each
    sub-query verbatim (`InputQuery` role). `text-embedding-3`'s query encoder
    expects the sentence, not keywords.
  - **Keyword (FTS5/BM25) lane** ← the **extracted keywords + entities + aliases**,
    sanitized and OR-ed — *not* the whole sentence (stopwords dilute BM25 and
    raise FTS5 syntax/injection risk).
- **HyDE — skip.** It backfires on exactly this setting (lossy summary pages +
  named entities): it invents plausible-but-wrong entity names/dates and is
  discouraged for fact-bound retrieval. Decomposition + alias expansion already
  buys the semantic-alignment win HyDE targets, without the hallucination tax and
  extra generation.
- **Multi-query paraphrase fan-out — skip.** ~1.77× runtime and N× search for
  recall on *broad* queries; decomposition already produces the *useful* kind of
  query multiplicity (distinct subjects).

**Default**: one analysis call → `{sub_queries[≤4], keywords, aliases}`; per
sub-query, embed the raw text (dense) + OR'd sanitized keywords/aliases (BM25);
RRF-fuse all lanes; dedupe by `PageID`. **Cheaper variant** (if multi-subject
questions are rare): no decomposition — embed the raw question + FTS5 on extracted
keywords. **Richer variant** (only if a future eval shows a gap): conditional
HyDE fired only when top fused similarity is below a confidence threshold.

---

## 7. Honest-empty & citations under fuzzy retrieval

Fuzzy search **always** returns *some* top-K, so the old "0 names resolved → empty"
gate disappears. Restore honesty with a **two-layer gate**:

1. **Deterministic relevance floor before synthesis.** Apply a **cosine floor**
   (start ~**0.30–0.35** absolute; tune against a handful of known in/out-of-corpus
   probes) and require the fused top hit to clear a minimum. If nothing survives,
   return `honestEmpty()` **without calling the synthesis LLM** — preserving the
   no-LLM-on-empty property the keyword path had. Make the floor a config knob.
   *(Floor on the raw lane signals, not the RRF score, which is uncalibrated.)*
2. **Keep `found=false`** as the second layer: the synthesis prompt already says
   "if the pages do not answer, return found=false," and `validateCitations`
   enforces that a found answer cites real gathered pages.

**Citations stay well-defined.** A citation resolves to a **page/subject**
regardless of *how* the page was retrieved (named vs. fuzzy). `validateCitations`
maps `{subject,title}` → `{path,title}` unchanged; fuzzy retrieval only changes
which pages enter the candidate set, not the citation contract.

---

## 8. The retrieval seam (carried forward, corrected)

Prior research already designed this seam for exactly this moment; keep it, with
the `Version`-staleness note corrected (no `version` column — see §3).

```go
type Hit struct {
    SubjectID string  // subject the page belongs to
    PageID    string  // stable fusion/dedup key AND citation ref (== subject_id)
    Score     float64 // lane-local: BM25 / cosine / fused RRF
    Snippet   string  // matched excerpt for citation + synthesis context
    Title     string
}
type Retriever interface {
    Search(ctx context.Context, query string, k int) ([]Hit, error)
}
```
Compose behind one interface: `keywordRetriever` (FTS5 `MATCH` + `bm25()`),
`vectorRetriever` (embed query → brute-force cosine), `hybridRetriever` (fan out
+ RRF). `ask` depends only on `Retriever`. **Lock now:** `PageID` is the stable
fusion+citation key every lane populates; `Search` returns a flat `[]Hit` (fusion
is `hybridRetriever`'s private detail). Add a `SearchLimits{Default,Cap}.Resolve()`
clamp.

---

## 9. Embedding as a configured call site (recording since retired)

Embedding is a configured call site (**model + dims** knobs — D34); the two
sides stay genuinely distinct as **embed-page** (ingest/merge time) and
**embed-query** (ask time). The wiki-side `llm_calls` recording this section
originally specified is retired: accounting for every call — chat and embedding
— is now the prompts service's `calls` table (§12), keyed by `name`
(`wiki.embed-page` / `wiki.embed-query`) and `group_id`.

---

## 10. The anti-collapse invariant (carried forward)

Unchanged by this work, but restate so design doesn't accidentally violate it
while touching the ingest/page path:

> **Claims are extracted only from raw source text, never from generated pages.**
> The moment claims are re-derived from pages, the recursive model-collapse loop
> is reborn.

This upgrade touches only **retrieval** (the read path) and **page embedding** (a
read-only derivative of an already-compiled page) — neither re-extracts claims —
so the invariant is preserved by construction. Worth a one-line design note that
embedding a page is a read-only projection, not a re-ingest.

---

## 11. Open questions for design to settle

1. **Cosine floor value** — start 0.30–0.35, but it needs calibrating against
   real in/out-of-corpus questions; design should name the default and make it a
   knob, and note how it'll be tuned (a few manual probes now; eval later).
2. **Final K and per-lane candidate counts** — defaults K≈8, 60/lane; confirm
   against the 12k page cap and synthesis context budget.
3. **Decomposition on by default, or the cheaper single-query variant?** Depends
   on how common multi-subject ("compare X and Y") questions are for this owner.
   Recommend on-by-default with a cap of 4 sub-queries.
4. **Embedding model & dims** — recommend `text-embedding-3-small @ 512`;
   confirm and lock as the page-embed call-site default. `model@dims` stored per
   vector so this can change later.
5. **Catch-up worker shape** — a new `Spec.Workers` poll loop vs. folding into the
   existing ingest worker; and whether to expose an explicit re-embed/backfill
   trigger or rely on lazy catch-up.
6. **Registry-first rank-1** — keep the exact-name-pinned-at-rank-1 behavior, or
   let the hybrid stand alone? (Recommend keep — cheap precision, preserves
   today's behavior as a special case.)
7. **Query-analysis output schema & prompt** — exact JSON
   (`sub_queries/keywords/aliases`) and how it threads into the two lanes.
8. **Vector load strategy** — load all vectors into a RAM slice at startup vs.
   stream from the BLOB column per query. At 100k×512 (~205 MB) an in-RAM cache is
   fine and fastest; design should pick and state cache-invalidation on re-embed.
</content>
</invoke>

---

## 12. The prompts service inference API (the unified-inference dependency)

Facts verified against the prompts spec (its D28–D33) and its deployed v0.21.0
service, which this conversion consumes. Base URL: `registry.BaseURL("prompts")`
(loopback :3002); both endpoints are loopback-only plumbing (unauthenticated —
nginx never routes them; only loopback processes can reach them).

**`POST /complete`** — stateless, synchronous, tool-less chat completion:

```json
{
  "origin":   "user:a@b.com | trigger:<source> | service:wiki",   // required
  "name":     "wiki.<stage>",           // required: ^[a-z0-9][a-z0-9-]*\.[a-z0-9][a-z0-9._-]*$
  "group_id": "…",                      // optional correlation id
  "attempt":  2,                        // optional, default 1
  "model":    "claude-sonnet-4-6",      // required, catalog name
  "provider": "anthropic",              // optional, catalog default
  "config":   { "temperature": 0, "max_tokens": 16384, "effort": "low", "thinking": false },
  "system":   "…",
  "messages": [ { "role": "user|assistant", "text": "…" } ]   // non-empty; last role must be "user"
}
```

`200` → `{"call_id", "text", "usage": {"InputUncached", "CacheReadInput", …,
"Output", "Total"}, "cost_usd"}`. Status taxonomy: `400` envelope/validation
(catalog membership, provider routability, reasoning vocabulary, origin/name
grammar, final-role rule; body `{"error": "…"}` naming the problem, nothing
executed or recorded), `405` non-POST, `500` internal/recording failure, `502`
provider failure. The `config` keys are the prompts config vocabulary
(`temperature`, `top_p`, `max_tokens`, `effort`, `thinking_budget`,
`thinking_level`, `thinking`, retry keys, `base_url`); `effort` maps to a
reasoning level, `thinking:false` disables reasoning explicitly. Verified live:
a haiku one-shot returned `text:"pong"`, usage, and catalog-priced `cost_usd`.

**`POST /embed`** — batch embeddings:

```json
{
  "origin": "…", "name": "wiki.embed-page", "group_id": "…",
  "model": "text-embedding-3-small",     // catalog embedding entry (openai/google only)
  "dimensions": 512,                     // optional; 0/omitted = native; validated to [min,max]
  "role": "document",                    // required: "document" | "query"
  "inputs": ["…"]                        // non-empty, no blank strings
}
```

`200` → `{"call_id", "vectors": [[…]], "usage", "cost_usd"}`, vectors in input
order, one per input. Same `400`/`502` taxonomy; a chat model, an out-of-range
`dimensions`, an embedder-less provider, or a bad `role` each `400` with no row.

**Accounting.** Every executed call lands one durable row in prompts' `calls`
table (class `completion`/`embedding`/`session`) carrying origin, name,
group_id, attempt, model, tokens, cost, error; request/response bodies are
retained ~30 days then pruned (metrics forever). Inspection: prompts' MCP
`calls` (filter by class/origin/name/group_id, detail by `call_id`) and `usage`
(aggregate by name|origin|model|day). This is what replaces wiki's `llm_calls`.

**Admission.** prompts gates concurrent synchronous calls per provider
(default 8 in-flight); a call may therefore block briefly before executing —
another reason wiki's client carries no fixed HTTP timeout.

**Correlation ids.** The suite standard (`docs/correlation-ids.md`): a
correlation id is a bare 26-char Crockford-base32 ULID minted **once at the
initial user action** and propagated verbatim; a durable root entity's own ULID
(an ingest job) serves as the id; otherwise (an ask) mint one fresh.

## 13. The autotune driver's external tools: ralph, the agentrepl config convention, and subscription auth

External ground truth for the one-command tuning driver (D68). Three tools
outside this repository are load-bearing; their observed contracts as of
2026-07-20:

### ralph (the loop executor, `~/.local/bin/ralph`, v0.10.0)

`ralph [flags] <prompt-path>...` runs an agent harness in a fresh-context
loop, dispatching on the model's final status word: `CONTINUE` re-runs the
same prompt, `NEXT` advances (wrapping), `DONE` stops. Flags the driver
passes through verbatim (everything after the driver's `--` separator):

- Budgets: `--max-iterations N`, `--max-time D` (e.g. `30m`), `--max-spend USD`,
  `--max-tokens N` (0 = unlimited); `--max-retries N` (default 3) for
  bad/failed turns.
- `--harness codex|claude|zai|kimi|agentkit` (default **codex**) and repeatable
  `-c key=value` harness config. The agentkit harness's keys/defaults:
  `provider=openrouter auth=key model=glm-5.2 effort=high`; env keys per
  provider (`ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OPENAI_API_KEY`,
  `ZAI_API_KEY`, `OPENROUTER_API_KEY`). As of v0.10.0 ralph's agentkit harness
  rejects `auth` other than `key` (sub support is in progress upstream);
  the codex harness carries its own ChatGPT subscription auth.
- Output: `--format chat|jsonl|raw` on stdout (default chat) — the driver
  streams this through.

ralph runs in the current working directory; the prompt path is positional
and last. Exit occurs on DONE, budget exhaustion, or signal.

### agentrepl's `-c` config convention (`~/.local/bin/agentrepl`)

The suite's de-facto CLI convention for naming an agentkit call, which the
driver's step-model flags mirror: repeatable `-c key=value` with keys
`provider`, `model`, `auth`, `effort` / `thinking_level` / `thinking_budget`,
`auth_file`. Provider set: `anthropic`, `google`, `openai`, `openrouter`,
`zai`, all with `auth=key` via the env vars above; **`auth=sub` exists for
`openai` only**, reading `auth_file` (default `~/.agentrepl/auth.json`).
agentrepl's defaults are `provider=openai model=gpt-5.6-sol auth=sub`; the
driver's defaults instead come from the committed `eval/extract/config.json`
production pins.

### Subscription auth: `oauth-login` and agentkit's store

`~/.local/bin/oauth-login` is a standalone OAuth authorization-code CLI; its
stdout contract is the **raw token-endpoint response saved verbatim** — that
file (e.g. `~/.agentrepl/auth.json`) is exactly what
`github.com/ikigenba/agentkit/openai/subscription` consumes:
`subscription.Load(path)` parses the raw response, requires `access_token`
plus a ChatGPT account claim in the id/access token, refreshes with skew
internally, and rewrites the file as the refresh-token lineage rotates. The
store is **OpenAI/ChatGPT-specific**; agentkit has no subscription store for
any other provider (an openrouter/anthropic `auth=sub` is simply
unsupported). The codex CLI's wrapper file format (`tokens.*`,
`last_refresh`) is *not* accepted — only the raw token response.

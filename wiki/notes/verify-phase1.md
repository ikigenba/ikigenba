# Phase-1 verification — ingest → filed → searchable (Task 5.3)

> **Verdict: PASS (LIVE).** A real, end-to-end Phase-1 round-trip ran over
> loopback `127.0.0.1:3006` against the live Anthropic API: `wiki_ingest_text`
> wrote an immutable provenance-stamped `raw/` doc, an async agentkit
> integration job filed it into curated cross-linked pages (`sources/`,
> `concepts/`, `entities/`) and updated `index.md` + `log.md`, the BM25 index
> was (re)built, and `wiki_search` returned the filed whole pages ranked.
> Re-ingest of identical bytes was a safe no-op on `raw/` (`already_had:true`,
> byte-identical raw doc, no duplicate).
>
> Date: 2026-06-04. Binary version `8d9d81e-dirty`. Model `claude-sonnet-4-6`.
> No secret material appears anywhere below (the key was injected at runtime via
> `$(cat ~/.secrets/ANTHROPIC_API_KEY)` and never printed).

## How wiki was driven (no dashboard / nginx)

Services trust nginx-injected identity headers, so `/mcp` is driven directly on
the loopback port. `bin/start` is intentionally **not** used here (wiki's
`.envrc` first line `source_up` breaks `bin/start`'s plain-bash `source` — a
known suite quirk); wiki is launched standalone with a fresh temp data root/DB.

```sh
# 0. Build (or reuse wiki/build/wiki.bin)
(cd wiki && GOPROXY=off ./bin/build)

# 1. Fresh temp data root + DB
WIKI_TMP="$(mktemp -d /tmp/wiki-verify-XXXXXX)"; mkdir -p "$WIKI_TMP/data"

# 2. Launch on loopback 3006 — key injected at runtime, NEVER printed.
ANTHROPIC_API_KEY="$(cat ~/.secrets/ANTHROPIC_API_KEY)" \
WIKI_DATA_ROOT="$WIKI_TMP/data" \
WIKI_DB_PATH="$WIKI_TMP/wiki.db" \
WIKI_INGEST_JOB_TTL_SECONDS=120 \
  wiki/build/wiki.bin --ip 127.0.0.1 --port 3006 > "$WIKI_TMP/wiki.log" 2>&1 &
```

Boot log (no secret material; ingest enabled because the key was present):

```json
{"level":"INFO","msg":"starting wiki","addr":"127.0.0.1:3006",
 "db_path":".../wiki.db","data_root":".../data",
 "ingest_model":"claude-sonnet-4-6","ingest_enabled":true,"version":"8d9d81e-dirty"}
```

All MCP calls below carry the injected identity headers
`-H 'X-Owner-Email: demo@example.com' -H 'X-Client-Id: demo-client'` (omitted
from each snippet for brevity).

## Network preflight — Anthropic API REACHABLE

```sh
curl -sS -m 8 -o /dev/null -w 'HTTP %{http_code}\n' https://api.anthropic.com/v1/messages
# -> HTTP 405   (GET not allowed = TLS/route reachable)

curl -sS -m 8 -X POST -H 'content-type: application/json' -d '{}' \
  -o /dev/null -w 'HTTP %{http_code}\n' https://api.anthropic.com/v1/messages
# -> HTTP 401   (reachable-but-unauthorized = network OK)
```

Network is open, so this was a **live** round-trip (not stub-proven).

## a. initialize + tools/list — the 5 verbs

```sh
curl -sS -X POST http://127.0.0.1:3006/mcp -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
# -> {"result":{"protocolVersion":"2025-03-26","capabilities":{"tools":{}},
#               "serverInfo":{"name":"Wiki","version":"1"}}}

curl -sS -X POST http://127.0.0.1:3006/mcp -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
# tool names ->
# ['wiki_whoami', 'wiki_ingest_text', 'wiki_ingest_url', 'wiki_search', 'wiki_job_status']
```

## b. wiki_ingest_text — distinctive content with provenance

```sh
curl -sS -X POST http://127.0.0.1:3006/mcp -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
        "name":"wiki_ingest_text","arguments":{
          "content":"The Zorblax Nebula was catalogued by astronomer Mira Velasquez in 2387. It is notable for its triple-helix plasma filaments, which rotate once every 4.2 standard hours.",
          "title":"Zorblax Nebula field note",
          "source":"verify-phase1-demo",
          "tags":["astronomy","zorblax"]}}}'
```

Result (inner JSON):

```json
{
  "already_had": false,
  "job_id": "AGPJK6HJMJGHMF7Y2CAFHXM53I",
  "raw_path": "raw/0daeba0a8f057ddeddae7ee3f4490f629d8b86ecbded6497d456b0cce94d24ba.md",
  "sha256": "0daeba0a8f057ddeddae7ee3f4490f629d8b86ecbded6497d456b0cce94d24ba"
}
```

## c. wiki_job_status — polled to terminal

```sh
curl -sS -X POST http://127.0.0.1:3006/mcp -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{
        "name":"wiki_job_status","arguments":{"job_id":"AGPJK6HJMJGHMF7Y2CAFHXM53I"}}}'
```

Reached terminal after ~7 polls (~21s wall; job ran ~34s):

```json
{
  "job_id": "AGPJK6HJMJGHMF7Y2CAFHXM53I",
  "status": "succeeded",
  "terminal": true,
  "started_at": "2026-06-05T01:49:49.794193172Z",
  "ended_at":   "2026-06-05T01:50:23.910742834Z",
  "usage": "{\"usage\":{\"input_tokens\":16368,\"output_tokens\":2388}}"
}
```

## d. Filesystem evidence (under the temp data root)

Tree after the integration pass (`<root> = <data>/demo@example.com/default`):

```
default/raw/0daeba0a…d24ba.md            <- immutable raw doc
default/sources/0daeba0a…d24ba.md        <- filed source page (agent-authored)
default/concepts/zorblax-nebula.md       <- agent-authored concept page
default/entities/mira-velasquez.md       <- agent-authored entity page
default/index.md                         <- navigation catalog (bootstrapped)
default/log.md                           <- append-only operation log
default/.search/index.sqlite (+ -wal/-shm)  <- BM25 search index built
```

**Immutable raw + provenance** — `raw/<sha256>.md` frontmatter is stamped by the
service (`sha256`, `ingested_at`, `title`, `source`, `tags`, `collection`); the
body is the original bytes verbatim:

```
---
type: source
sha256: "0daeba0a8f057ddeddae7ee3f4490f629d8b86ecbded6497d456b0cce94d24ba"
ingested_at: "2026-06-05T01:49:49Z"
title: "Zorblax Nebula field note"
source: "verify-phase1-demo"
tags: ["astronomy", "zorblax"]
collection: "default"
---

The Zorblax Nebula was catalogued by astronomer Mira Velasquez in 2387. It is
notable for its triple-helix plasma filaments, which rotate once every 4.2
standard hours.
```

**index.md** lists the new source/concept/entity rows; **log.md** appended:

```
2026-06-05T01:49:49Z | ingest | sha256=0daeba0a…d24ba | title="Zorblax Nebula field note" |
  created: sources/0daeba0a…d24ba.md, concepts/zorblax-nebula.md, entities/mira-velasquez.md |
  updated: index.md (bootstrapped)
```

The filed `sources/<sha>.md` page links back to the raw doc and out to the
derived concept/entity pages (provenance + cross-linking intact).

## e. wiki_search — finds the filed pages

```sh
curl -sS -X POST http://127.0.0.1:3006/mcp -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{
        "name":"wiki_search","arguments":{"query":"Zorblax triple-helix plasma","limit":5}}}'
```

Returned the `index.md` page first, then 3 whole curated pages ranked best-first
(higher score = more relevant):

```
query: "Zorblax triple-helix plasma" | count: 3 | index: index.md
  concepts/zorblax-nebula.md     score=6.47e-06   title="Zorblax Nebula"        body_len=1088
  entities/mira-velasquez.md     score=5.16e-06   title="Mira Velasquez"        body_len=741
  sources/0daeba0a…d24ba.md      score=4.70e-06   title="Zorblax Nebula field note" body_len=917
```

Whole pages (not fragments) + the index, as designed. Search round-trip PASS.

## Immutability spot-check — identical re-ingest is a safe no-op on raw/

```sh
# stat the raw doc, re-ingest IDENTICAL bytes, stat again
```

```json
// re-ingest result
{ "already_had": true,
  "job_id": "AGPJK6RPVJF2TUNJCMDT3TGORQ",
  "raw_path": "raw/0daeba0a…d24ba.md",
  "sha256": "0daeba0a8f057ddeddae7ee3f4490f629d8b86ecbded6497d456b0cce94d24ba" }
```

- Same `sha256`, `already_had:true`.
- `raw/<sha>.md` **byte-identical** before/after: same mtime (`1780624189`), same
  size (`418`), same file-sha (`3898783876ce…b370`). No duplicate in `raw/`.
- Note (by design, not a defect): re-ingest still **spawns a new integration
  job** — re-ingest is "safe", not "skipped". The immutable `raw/` is what makes
  the re-file safe; the agent re-integrates against the existing raw doc. That
  2nd job also ran to `succeeded`. (One extra small API call.)

## Cleanup

wiki stopped, port 3006 freed, the temp data root + DB removed after capture.

## Spend

Two ingest integration jobs (the demo ingest + the immutability re-ingest no-op
check). The demo ingest used 16368 input / 2388 output tokens. Minimal.

## Reproduce on any networked machine

```sh
direnv allow wiki/              # with a real ANTHROPIC_API_KEY wired via wiki/.envrc / SSM
# then build + launch as in "How wiki was driven", and replay calls a–e above.
```

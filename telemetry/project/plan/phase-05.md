# Phase 5 — The forensic MCP surface: search, chain, get, guide

*Realizes design Decision 5 (MCP surface). Depends on Phase 02.*

`internal/mcp` gains the tool table over `appkit/mcp`, and `cmd/telemetry`'s
`Handlers` hook mounts it behind `rt.RequireIdentity`.

- Tier 0: the `Instructions` string from D5 — orientation, the investigator's
  routing vocabulary, and exactly one pointer to `guide`.
- Tier 1: four declared tools and no more — `search` (the filter set, AND
  composition, `limit` 1–500 default 50, opaque `cursor`, returning
  `records`/`next_cursor`/`retention_horizon`), `chain` (one `correlation_id`,
  the complete ascending chain plus `count`, `retention_horizon`, and
  `possibly_truncated`, empty-not-error for an unknown id), `get` (one `id`,
  the full record, `not_found` for an unknown id), and `guide`. The chassis
  supplies `health` and `reflection`; nothing declares a mutation or analytics
  tool.
- Tier 2: the `//go:embed`ed guide document — the record field catalog, the
  seven `kind` values, the per-kind `op` shape, `params`/`outcome`/`detail`, the
  `{"$elided": ...}` representation, the statement that bodies and
  conversations are never stored, and basic plus advanced worked examples. The
  guide is referenced in exactly two places: `Instructions` and `guide`'s own
  description.
- `search`/`chain`/`get` declare `outputSchema` and return `StructuredResult`;
  `guide` returns `TextResult` with no output schema. Errors use the closed
  vocabulary (`validation`, `not_found`, `internal`).

Tests drive the real `appkit/mcp` handler over real SQLite — no memory store, no
hand-called handlers where the wire shape is the claim.

**Done when:**

- Every id below is covered by a clearly-named, id-tagged test:
  - R-VW9B-ASIT — `tools/list` returns exactly
    `{search, chain, get, guide, health, reflection}` and no tool name or
    description contains `delete`, `redact`, `annotate`, `purge`, `stats`,
    `aggregate`, or `export`.
  - R-VXH7-OK9I — filters compose with AND across all six axes (each excluded
    in turn), and a `sha256` query returns the matching records from two
    different services.
  - R-VYP4-2C07 — default limit 50, `limit: 500` accepted, `limit: 0` and
    `limit: 501` are `validation` errors naming `limit` with no records
    returned, and the cursor yields a non-overlapping next page with an empty
    `next_cursor` on the last one.
  - R-VZX0-G3QW — `chain` returns the full ordered three-service chain with
    matching `count`, correct `retention_horizon`, and `possibly_truncated`
    true only at the horizon; an unknown id returns an empty non-error result.
  - R-W2CT-7N8A — `get` returns the full record with byte-identical
    `params`/`detail`, and an unknown id returns the `not_found` code.
  - R-W3KP-LEYZ — the three domain tools declare `outputSchema` and return
    conforming `structuredContent` plus a text block parsing to the same value;
    `guide` declares none and is text-only.
  - R-W4SL-Z6PO — the guide names all seven kinds, describes
    `params`/`outcome`/`detail`, shows the elision form, states that bodies are
    never stored, and its worked example's tool and argument names all exist in
    the declared tool set.
- The surface is read-only by construction: no file under `internal/mcp/`
  references `InsertRecords`, `NoteDropped`, or `PruneBefore` —
  `grep -rn 'InsertRecords\|NoteDropped\|PruneBefore' internal/mcp/` run from
  `telemetry/` returns empty (exit 1).
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`,
  `go test ./...` all exit 0 in `telemetry/`.

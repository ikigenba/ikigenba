# Phase 16 — Structured MCP adoption: structuredContent, output schemas, closed error vocabulary

*Realizes design Decision 16 (structured MCP adoption; D6 and D12 were rewritten
in place to the same result/error contract — their retained ids are re-proven by
the existing tool suite conforming). Depends on Phase 12 (the `appkit/mcp` tool
table: `internal/mcp` is `Instructions` + `Tools(svc, baseURL)` +
`NewHandler(svc, rt)`).*

> **⛔ EXTERNAL ORDERING — operator-sequenced.** This phase consumes the
> **revised `appkit/mcp` API** (appkit phases 12–14): `JSONResult` is **deleted**,
> `StructuredResult(v) (map[string]any, error)` and
> `ErrorResult(code appkitmcp.ErrorCode, msg string)` are the new shapes, and
> `Tool.OutputSchema` is surfaced by `tools/list`. webhooks does **not** compile
> against that appkit revision until this phase runs. appkit must be green in the
> tree first; cross-module sequencing is the operator's, not this plan's. If the
> revised appkit is not present, this phase cannot build and must be left `⬜`.

Observable end state (all edits confined to `webhooks/internal/mcp/tools.go` and
its tests):

- The four domain verbs' success paths call `appkitmcp.StructuredResult(v)`
  instead of `appkitmcp.JSONResult(v)`, propagating the returned error (no
  `_`-discard). The emitted value per verb is unchanged from D6: `create` →
  `{name, trigger_url, created_at, last_triggered_at, secret}`; `list` →
  `{items: [webhookView]}`; `delete` → `{deleted: true}`; `rotate` →
  `{name, trigger_url, secret}`.
- Each of the four `appkitmcp.Tool` values sets an `OutputSchema` literal
  (`type`/`properties`/`required`) mirroring its emitted JSON verbatim, authored
  with a small private helper beside the existing `obj`/`typ` descriptor helpers.
  webhooks has no prose tool, so all four carry a schema; `health`/`reflection`
  remain chassis-owned.
- The hand-built `errorEnvelope`/`toolErr(string)` pair is replaced by
  `toolErr(err)` returning `appkitmcp.ErrorResult(code, err.Error())` with
  `code` = `ErrConflict` (`ErrNameTaken`), `ErrValidation` (`ErrInvalidName`),
  `ErrNotFound` (`ErrNotFound`), `ErrInternal` (default). The legacy `duplicate`
  code and the `field:"name"` key are gone.
- The gated `POST /mcp` route (`rt.RequireIdentity`) and the public `POST /in/<name>`
  ingress (D4) are untouched: webhooks has no loopback guard to swap.

**Done when:** the suite is green (design Conventions commands — `go build ./...`,
`go vet ./...`, `go test ./...` from `webhooks/`) and:

- R-DRUS-R3AP — a `tools/list` test asserts a non-nil `outputSchema` on each of
  `create`, `list`, `delete`, `rotate` (table-driven over the four names).
- R-DT2P-4V1E — a `tools/call` test asserts each domain success result carries a
  `structuredContent` object and a `text` block whose parsed JSON equals it
  (table-driven over the four verbs), and that no domain success is text-only.
- R-DUAL-IMS3 — a test asserts each verb's `structuredContent` conforms to its
  declared `outputSchema`: `create` keys `{name, trigger_url, created_at,
  last_triggered_at, secret}`; `list` `{items:[{name, trigger_url, created_at,
  last_triggered_at}]}` with no `secret`/`secret_hash`; `delete` `{deleted:true}`;
  `rotate` `{name, trigger_url, secret}`.
- R-DVIH-WEIS — a test on a two-`create` same-name setup (real SQLite) asserts the
  second returns `isError:true` with `structuredContent.code == "conflict"` (not
  `"duplicate"`).
- R-DWQE-A69H — a table-driven test asserts an invalid-name `create` →
  `structuredContent.code == "validation"`, a `delete`/`rotate` of a not-owned
  name → `"not_found"`, and an injected store failure → `"internal"`; every error
  result sets `isError:true` and carries `structuredContent`.
- R-DXYA-NY06 — `grep -rn 'JSONResult' internal cmd --include='*.go' | grep -v _test.go`
  (run from `webhooks/`) returns empty.

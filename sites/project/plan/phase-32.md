# Phase 32 — Structured MCP surface: structuredContent, output schemas, closed error codes

*Realizes design Decision 25 (Structured MCP adoption). Depends on Phase 22 (the
`internal/mcp` domain-tool table over `appkit/mcp`).*

Convert sites's `internal/mcp` surface (`tools.go`, `files.go`, `sync.go`) to the
suite's structured-MCP contract, against the already-cut-over appkit (`JSONResult`
deleted; `StructuredResult(v) (map, error)` and `ErrorResult(code, msg)` are the
helpers; `Tool.OutputSchema` surfaced by `tools/list`). Observable end state:

- Every domain success result (`create`, `list`, `delete`, `mkdir`,
  `set_visibility`, `sync`, `file_write`, `file_edit`, `file_glob`, `file_grep`,
  `file_list`) returns `appkitmcp.StructuredResult(v)` — `structuredContent` plus a
  mirrored text block — with the `StructuredResult` error return honestly
  propagated. The emitted JSON objects are byte-unchanged.
- Each of those eleven structured tools declares a hand-authored `OutputSchema`
  mirroring its emitted JSON (D25's shape table). `guide` and `file_read` — the two
  prose exceptions (a documentation tool and a raw-content read) — keep `TextResult`
  and declare no schema.
- Every failure returns `appkitmcp.ErrorResult(code, msg)` with `code` a member of
  the closed vocabulary per D25's mapping: domain-sentinel and argument/confinement
  validation → `validation`; already-exists → `conflict`; missing site → `not_found`;
  dropbox mirror unconfigured / `List` / `Fetch` failure → `source_unavailable`;
  internal filesystem failures → `internal`. The human detail (former fine-grained
  code) rides `msg`.
- No `JSONResult` reference remains anywhere under `internal/` or `cmd/`.

The existing `internal/mcp` tests (`tools_test.go`, `files_test.go`, `sync_test.go`)
are brought current against `StructuredResult`/`ErrorResult`; the file-tool
confinement assertion in `files_test.go` (D11's R-0JAJ-OIF8, which now pins
detection only) keeps asserting `isError`, and its code assertion moves to this
phase's R-D110-C4ZF. No handler reshapes a result; no schema, migration, nginx, or
landing-page change.

**Done when:** the following tests are green and the suite is green per design
Conventions (`cd sites && go build ./... && go vet ./... && gofmt -l .` empty
`&& go test ./...`, including the D23 headless-Chrome test — Chrome hard-required):

- R-CW5E-T20N — a `tools/call create` result carries `structuredContent` equal to
  the site object and a text block equal to that object's JSON (dual rendering, not
  text-only).
- R-CXDB-6TRC — table-driven over `tools/list`: each of the eleven structured tools
  has an `outputSchema` (`type:"object"`); `guide` and `file_read` have none.
- R-CYL7-KLI1 — for every structured tool, a successful call's `structuredContent`
  keys include exactly the properties its declared `outputSchema` marks `required`.
- R-CZT3-YD8Q — table-driven error mapping: invalid/reserved slug and a missing
  required argument → `validation`; already-exists → `conflict`; missing site →
  `not_found`; an internal filesystem failure → `internal`; and no error result
  carries a code outside the closed vocabulary or a legacy fine-grained string.
- R-D110-C4ZF — a `..`-escaping/outside-root path to `file_read`, `file_write`, and
  `mkdir` returns `isError` with `structuredContent.code == "validation"` and a
  path-escape message (not `path_escapes_working_dir`).
- R-D28W-PWQ4 — with a fake `MirrorClient`, `sync` returns
  `structuredContent.code == "source_unavailable"` for the unconfigured (nil),
  `List`-error, and `Fetch`-error paths.
- R-D3GT-3OGT — `grep -rn 'JSONResult' internal cmd --include='*.go'` returns empty
  (the new tests assert on `StructuredResult`, not the old token).

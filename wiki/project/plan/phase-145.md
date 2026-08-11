# Phase 145 — Ask gathers under a body-byte budget instead of a fixed page count

*Realizes design Decision 9 (budgeted gather slice: R-NFXF-9QGN, R-NH5B-NI7C, R-NID8-19Y1). Depends on Phase 144.*

`internal/ask` stops truncating synthesis context at a fixed 8 pages. `Ask`
requests `retrieve.LimitCap` (20) hits from `SearchAnalyzed`, and the gather
step walks the fused ranking adding whole page bodies until the byte budget is
spent: a page whose body would overflow the remaining budget is skipped whole
and the walk continues (greedy first-fit); partial bodies are never included.
The `finalK` field and its option are replaced by the budget (a `WithBodyBudget`
option for tests). The budget constant `wiki.AskBodyBudget = 98304` lives in
`internal/wiki` beside `SearchDefault` and is declared in the composition
root's manifest key list as `ASK_BODY_BUDGET`, exactly as `SEARCH_DEFAULT` is.

**Done when:**

- R-NFXF-9QGN — a mock retriever captures the limit `Ask` passes to
  `SearchAnalyzed` and it equals `retrieve.LimitCap` (20) — covered by a named
  test tagged with the id.
- R-NH5B-NI7C — with hits of known body sizes and a budget admitting hits 1–2,
  overflowing on 3, and fitting 4, the synthesis context contains exactly pages
  1, 2, and 4 (page 3 absent entirely, never truncated), and moving the budget
  threshold moves which pages are included — covered by a named test tagged
  with the id.
- R-NID8-19Y1 — the composed manifest declares `ASK_BODY_BUDGET` with value
  `98304` — covered by a named test tagged with the id.
- The suite is green per design Conventions (`go build ./...`, `go vet ./...`,
  `gofmt -l .` empty, `go test ./...` all passing from `wiki/`).

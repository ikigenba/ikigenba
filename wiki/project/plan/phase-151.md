# Phase 151 — Wire the ask cache: knob, composition root, invalidation hooks

*Realizes design Decision 83 (ask response cache) — the wiring slice: R-0DQI-XKD5, R-0EYF-BC3U, R-0G6B-P3UJ, R-0IM4-GNBX, R-0L1X-86TB. Depends on Phase 150.*

The D83 cache goes into production wiring. `Config` gains `AskCacheCap int` resolved from the fail-loud `ASK_CACHE_CAP` manifest knob (unset → `wiki.AskCacheCapDefault` = 500; `0` → disabled; negative/non-numeric → startup error), declared in the composed manifest with default `500`. `cmd/wiki/main.go` builds `ask.NewCache(asker, cfg.AskCacheCap)` and hands it to both the web handler's `Asker` seam and the MCP `ask` dispatch — the bare `*ask.Asker` no longer reaches a handler. `wiki.Service` and `wiki.ScopeStore` gain the optional nil-safe `AskInvalidate func(scope string)` callback, set by the composition root to `cache.Invalidate` and fired on: successful job integrate (the job row's scope — ingest, rerun, and merge jobs alike), successful `SetInstructions`, and successful scope `Delete`.

**Done when:**
- Each of these ids is covered by a clearly-named test tagged verbatim in a `*_test.go` file, asserting the behavior stated in D83:
  - R-0DQI-XKD5 — a successful job integrate invalidates the job's scope's cached asks; other scopes' entries survive.
  - R-0EYF-BC3U — a successful `SetInstructions` invalidates the scope; the recomputed ask carries the composed system.
  - R-0G6B-P3UJ — scope delete invalidates the scope; the cached answer is never served after deletion.
  - R-0IM4-GNBX — `ASK_CACHE_CAP` parses fail-loud with default 500 and is declared in the composed manifest.
  - R-0L1X-86TB — the MCP verb and the web page share one cache: two doors, one synthesis computation.
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed from `wiki/`.

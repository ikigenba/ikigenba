# Phase 29 — The chain crosses the sandbox boundary

*Realizes design Decision 30 (`SUITE_CORRELATION_ID` and `X-Correlation-Id` on
every `suite.*` call). Depends on Phase 28 (the run must carry a chain id
before the runner can inject one).*

What gets built, in `internal/runner`:

- `Runner.Spawn`'s per-run env block gains `SUITE_CORRELATION_ID=<the run's
  correlation id>`, beside the existing `SUITE_RUN_ID`/`SUITE_SCRIPT_ID`/
  `SUITE_OWNER_*` entries.
- The embedded `suite.py` sends `X-Correlation-Id: <SUITE_CORRELATION_ID>` on
  every request it makes — in `mcp`'s header block, in `fetch`'s content-plane
  request, and in `files`' shared `_headers()` — and **omits the header
  entirely** when the env var is absent or empty, rather than sending it blank.
  No other behavior of the module changes: the same URL confinement, the same
  identity headers, the same failure mapping.

**Done when:** the two ids below are each covered by a clearly-named test in the
`python3` probe harness (the embedded `suite.py` materialized exactly as the
runner does, the real `python3` executed on probe scripts, and
`net/http/httptest` loopback servers recording the headers they actually
receive), and the suite is green per design's *Conventions* (`go build ./...`,
`go vet ./...`, `gofmt -l .` silent, `go test ./...` clean):

- **R-4UZN-MWCU** — a probe with `SUITE_CORRELATION_ID=X` calling `suite.mcp`
  causes the recording server to receive `X-Correlation-Id: X` alongside the
  unchanged `X-Owner-Id` and `X-Client-Id: scripts:<script_id>`; with the env
  var unset the same call arrives carrying no `X-Correlation-Id` header at all,
  not an empty one.
- **R-4W7K-0O3J** — the same propagation holds for `suite.fetch` and for a
  `suite.files` verb: each causes its recording server to receive
  `X-Correlation-Id: X`, so no suite surface is a hole in the chain.

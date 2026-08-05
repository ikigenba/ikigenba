# Phase 8 — Adopt the context-delivered chassis recorder and prove the producer wire contract

*Realizes design Decision 7 (test strategy and the end-to-end layer), slice
R-5PIJ-TFHS.*

appkit's MCP chassis no longer carries a `Recorder` field on `appkit/mcp.Options`
— the serve chain installs the process recorder on every request context, and
`dispatchTool` reads it from there. telemetry's MCP fragment still sets the
removed field, so the module does not compile. This phase brings the module back
in line with D1 (telemetry never touches `rt.Recorder()`) and closes the
cross-module blind spot D7 now names.

What gets built:

- `internal/mcp/mcp.go` constructs `appkitmcp.Options` with **no recorder
  wiring** of any kind — the `Recorder:` field assignment is removed and nothing
  replaces it. The rest of the fragment (tools, instructions, chassis hooks) is
  unchanged.
- `internal/e2e` gains the producer wire-contract test (R-5PIJ-TFHS): the real
  composed service served per the existing e2e pattern, a real
  `appkit/telemetry.Recorder` constructed through its public API with
  `IngestURL` pointed at the served instance's `/ingest`, a record emitted via
  `Record`, delivered by flush/`Close`, then retrieved through `POST /mcp`
  (`chain` and `get`) with non-empty recorder-stamped `id` and `time`, and zero
  ingest rejections over the run.

**Done when:**

- R-5PIJ-TFHS is covered by a genuinely-asserting tagged test in
  `internal/e2e` that runs under the plain suite invocation (no skip, no build
  tag, no env gate).
- `grep -rn 'Recorder' internal/mcp/ --include='*.go'` prints nothing.
- The suite is green per design Conventions: from `telemetry/`,
  `go build ./...`, `go vet ./...`, and `go test ./...` all exit 0.

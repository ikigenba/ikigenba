# Phase 25 — Context-delivered recorder for MCP tool-call records

*Realizes design Decision 17 (context-delivered recorder) and its touchpoints
in Decision 15's `appkit/telemetry` package.*

The chassis becomes the sole deliverer of the recorder to the MCP layer.
`appkit/telemetry` gains `WithRecorder(ctx, r)` / `RecorderFromContext(ctx)`;
the `server.New` middleware chain installs the process recorder on **every**
request's context — unconditionally, including `RecordExclude` paths and
`POST /mcp`, where only the HTTP record is suppressed. `mcp.Options` loses its
`Recorder` field; `dispatchTool` reads the recorder from the request context,
no-oping on a bare context. Existing appkit MCP tests that passed the field are
rewritten to deliver the recorder via the context, the way production does.

Known consequence outside this tree (not this phase's to fix): the one
fragment that passed `Recorder:` (`telemetry/internal/mcp/mcp.go`) stops
compiling until telemetry's own spec move deletes that line; appkit's own
module tests and `GOWORK=off go build ./...` in `appkit/` are unaffected.

**Done when:**
- The new ids are covered by clearly-named tagged tests and appkit is green
  (`go test ./...` in `appkit/`, plus `GOWORK=off go build ./...` in
  `appkit/`):
  - R-RI9E-W0G2 — a handler with no recorder in its construction, mounted
    under the real `server.New` chain, lands its `mcp:<tool>` record at a live
    sink with identity and correlation id.
  - R-RKP7-NJXG — `mcp.Options` exposes no recorder-typed field (reflection),
    and `dispatchTool` on a bare context completes normally emitting nothing.
- The rephrased D17 ids stay green under context delivery: R-1PGY-03QK,
  R-1QOU-DVH9, R-1RWQ-RN7Y, R-1T4N-5EYN, R-1UCJ-J6PC, R-1VKF-WYG1.
- `grep -rn 'Recorder' appkit/mcp/ --include='*.go'` (tests included) shows no
  `Options`-field delivery — only context reads via
  `telemetry.RecorderFromContext` / `telemetry.WithRecorder`.

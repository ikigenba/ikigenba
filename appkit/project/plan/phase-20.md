# Phase 20 — Inbound instrumentation: MCP dispatch and the HTTP middleware chain

*Realizes design Decision 17 (inbound instrumentation). Depends on Phase 17 and
Phase 19.*

**End state.** `mcp.Options` carries a `Recorder`, `mcp.Tool` carries
`SensitiveParams []string`, and `dispatchTool` emits one record per tool call —
standard tools included — with kind `request`, op `mcp:<tool>`, the actor from
the threaded `server.Identity`, the correlation id from the context, params
through `telemetry.EncodeParams` honoring the tool's sensitive list, and an
outcome carrying duration, `ok`/`error` status, the response's size and digest,
and an error **class** (an `mcp.ErrorCode`, or `internal` for a handler fault) —
never a raw error message and never the response content. `server.Options`
carries `Recorder` and `RecordExclude []string`; a recording middleware sits
below the correlation middleware and above the mux, recording every other
request with op `<METHOD> <path>`, the URL query as params, and the response
size and digest computed by extending `statusRecorder` with a streaming hash
(bodies are never buffered and request bodies are never read). `/health` is
recorded like anything else; the MCP mount path is always suppressed at the HTTP
layer so a tool call yields exactly one record; paths in `RecordExclude` yield
none. `Spec.TelemetryExclude` plumbs into `server.Options.RecordExclude`.

**Done when:**
- These Verification ids are covered by clearly-named tests tagged with the id
  verbatim, driven through the real `ServeHTTP` JSON-RPC seam and the real
  `server.New` mux via `httptest`:
  - R-1PGY-03QK — one `tools/call` yields exactly one record with the right
    kind, op, service, actor, and correlation id.
  - R-1QOU-DVH9 — its outcome carries duration, `ok`, and the real response's
    size and digest, and the record's JSON contains none of the response's
    distinctive content.
  - R-1RWQ-RN7Y — a coded `ErrorResult` records that `ErrorCode`; a returned Go
    error records `internal` and not the message text; both still emit.
  - R-1T4N-5EYN — a sensitive-declared param is elided while an undeclared
    sibling is captured, and an oversized param is elided per D16.
  - R-1UCJ-J6PC — `GET /health` records op `GET /health` with status `"200"`
    and the real body's size and digest; a `POST /mcp` tool call yields exactly
    one record in total.
  - R-1VKF-WYG1 — an excluded path yields zero records while a sibling path
    yields one.
- The suite is green per design's *Conventions* (`go build ./...`, `go vet
  ./...`, `gofmt -l .` empty, `go test ./...`, all from `appkit/`).

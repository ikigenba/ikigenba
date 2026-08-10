# Phase 72 — The prompts-owned peer MCP client

*Realizes design Decision 60 (the peer MCP client).*

Build `prompts/internal/mcpclient`: the minimal MCP client of D60 — `New(doer,
baseURL, headers)`, `Instructions`, `ListTools`, `CallTool`, the `Tool` and
`Result` types, the `Doer` transport seam — speaking the wire contract of
research §8 (JSON-RPC 2.0 over POST, `initialize` + `notifications/initialized`
+ `tools/list` + `tools/call`, configured headers on every request). Stateless
per call; schemas relayed byte-verbatim, never parsed; the D60 error taxonomy
(peer-naming Go errors for transport/RPC failures, `Result{IsError: true}` for
a peer's error result). Tests drive the real client against recording
`httptest` peers.

**Done when:** R-OLVJ-TO7G (handshake + list with headers, schemas
byte-identical), R-OOBC-L7OU (call forwards args verbatim, relays text +
isError), R-OPJ8-YZFJ (all requests through the injected `Doer`), and
R-OQR5-CR68 (error taxonomy, no panics) are each covered by a test tagged with
its id, and the suite is green (design Conventions: `go test ./...` from
`prompts/`, `gofmt -l .` empty).

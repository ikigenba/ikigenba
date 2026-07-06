# Phase 08 — The `appkit/mcp` JSON-RPC transport

*Realizes design Decision 8 (the transport over a declared tool table). Depends
on no earlier phase (a new standalone package over `appkit/server`'s Identity
and the eventplane types).*

A new package `appkit/mcp` with the D8 surface: `Tool`, `Options`,
`New(Options) (*Handler, error)` (rejecting duplicate and reserved tool names),
`ServeHTTP` speaking JSON-RPC 2.0 over plain POST with the wire semantics D8
pins (`initialize`, `notifications/initialized`, `tools/list`, `tools/call`
with identity threading from `X-Owner-Email`/`X-Client-Id`, `-32700` on parse
error, `-32601` on unknown method, the services' error envelope on unknown
tool), and the `TextResult`/`JSONResult`/`ErrorResult` helpers. The standard
tools themselves are Phase 09; this phase may register them as stubs or leave
the reserved-name validation forward-looking, whichever keeps the phase
self-contained — the D8 ids do not depend on D9 behavior.

**Done when:** the suite is green (design Conventions commands, from `appkit/`)
and R-MCJJ-NXJR, R-MDRG-1PAG, R-MEZC-FH15, R-MG78-T8RU, R-MHF5-70IJ,
R-MIN1-KS98, and R-MJUX-YJZX are each covered by a clearly-named test in
`appkit/mcp` driving the real `ServeHTTP` seam with a test tool table,
genuinely asserting the behavior its D8 Verification line describes.

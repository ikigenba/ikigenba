# Walk answer ordering has no completion seam

Filed during the user-invoked `build-spec` run on `wip-funnel`, starting at `12a2f3d9df953cbe1f15174be772c585236d6356`. The implementation leaf reported the conflict and a fresh read-only verifier independently confirmed it.

## Requirements and friction

R-OKQV-IUB7 requires a `walk` call to make no request to `places.NearbyURL` before it is answered. R-1I57-LRF6 applies that behavior to a server registered with `tools.Register`, including a standalone MCP server. R-18E0-JLHM requires `Engine.Walk` to return a running pass without waiting for background work, R-HT9C-7JEF requires that work to run, and R-UF5P-F79H requires it to survive caller-context cancellation.

The required published appkit MCP API provides no response-completion signal. Candidates cannot establish the required response-before-request ordering across standalone registered servers. The refusal-without-writes part of R-OKQV-IUB7 is implementable, but does not prove the whole requirement; its tag remains absent.

## Evidence

Independent `go doc` inspection of the required published appkit dependency shows `mcp.RawTool.Handler` returns only `(mcp.Result, error)` and receives a context, caller and arguments. `mcp.Server` exposes `NewServer`, `ServeHTTP` and `Tools`; `ServerConfig` has no completion callback, and the opaque result retains no caller-provided callback or marshaler.

In that published dependency, `mcp/server.go` calls the tool handler at line 337, serializes its result at line 338, emits telemetry at lines 339–341, and only then constructs and writes the response at lines 359–360. Handler return therefore precedes answering, with no public notification back to the handler or engine. A background request can run in that interval.

A gate in `tools.Dispatch` or `web.Handler` would not cover the standalone server required by R-1I57-LRF6. A handler defer runs before the response is serialized. Timers and scheduler yields cannot order a delayed response writer. Request-context cancellation is also insufficient: `net/http.Request.Context` cancels on client disconnect as well as handler return; the standard server cancels the context before flushing its final response. Waiting for cancellation can therefore start work before an answer or never start it, contrary to R-HT9C-7JEF and R-UF5P-F79H. A held fake transport proves that acceptance does not await a Places answer, which is weaker than R-OKQV-IUB7.

The leaf and independent verifier reports were recorded in temporary evidence files during this run. No real Google request was made, and no timing workaround or weaker coverage tag was accepted.

## Suggested resolution

Outside this build run, replace the strict chronology with asynchronous acceptance that does not wait for Places, or declare and publish a response-completion seam covering every registered server. Define whether an answer means server write completion or client receipt. The build cannot change the read-only design or the published dependency contract.

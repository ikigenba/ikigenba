# Phase 7 — Vendor MCP client; cut suite.go off local agentkit

*Realizes design Decision 6 (Suite discovery — completion). Depends on Phase 5.*

`prompts/internal/suite/suite.go` no longer imports the local `agentkit/mcpclient`. The published agentkit exports no MCP client (its `mcp` plumbing is `internal/`), so the small, self-contained JSON-RPC-over-HTTP client that suite discovery needs is vendored into a new prompts-internal package `prompts/internal/mcpclient/` — a drop-in copy of the local `agentkit/mcpclient` (the `Client`/`Tool` types, `New`, `ListTools`, `CallTool`), carrying no agentkit dependency of its own.

`suite.go` is repointed at `prompts/internal/mcpclient`; its observable contract is unchanged. `Discover` still returns a non-nil `[]agentkit.Tool` (published agentkit), still wraps each peer verb as an `agentkit.RawTool`, still applies the same `qualify()` naming, and still honors the best-effort, never-crash contract (an unreachable or garbled peer is logged and skipped; no error is returned). The eager spawn-time listing pattern is preserved.

End state: no occurrence of the local `agentkit/mcpclient` import anywhere under `prompts/`; the suite package builds and its tests pass against the vendored client.

**Done when:** D6's Verification ids R-K32H-6XAV (a peer whose `tools/list` errors contributes no tools and does not fail `Discover`) and R-K4AD-KP1K (a reachable peer listing one tool yields exactly one service-qualified `agentkit.Tool` whose `Call` dispatches to the peer) are covered by clearly-named tests exercising the vendored client, and the suite is green.

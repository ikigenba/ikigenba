# Phase 22 — Suite discovery returns deferred tool groups

*Realizes the rewritten design Decision 6 (suite discovery as
`[]agentkit.DeferredToolGroup`, blurbs from MCP `initialize`). Depends on
Phase 07 (the vendored `mcpclient` this extends) and on the agentkit
deferred-tools API being resolvable at build time — locally via the repo-root
`go.work` replace to the agentkit checkout (the published `v0.2.0` tag is
Phase 23's concern).*

`suite.Discover` today returns a flat `[]agentkit.Tool`; this phase reshapes its
output into one `agentkit.DeferredToolGroup` per reachable peer — the shape the
runner hands to `Conversation.DeferredTools` in Phase 23 — and teaches
`mcpclient` to fetch the group blurb from the peer's MCP `initialize`
`instructions` field. All best-effort semantics are preserved exactly.

## Steps

In **`prompts/internal/mcpclient/`**:

- **Add `func (c *Client) Initialize(ctx context.Context) (string, error)`** —
  one more `c.call(...)` against the same endpoint with the same injected
  headers: method `initialize`, standard params (protocol version, client
  info), decoding only `result.instructions` (absent field → `""`, no error).

In **`prompts/internal/suite/`**:

- **Change `Discover`'s return type** to `[]agentkit.DeferredToolGroup` (same
  parameters; still never errors, never panics, always non-nil, self-excluded,
  per-call timeout unchanged).
- **Fetch the blurb concurrently**: alongside each peer's `tools/list`, call
  `client.Initialize`; a failure or empty result yields `Blurb: ""` and is
  logged, never excludes the peer (exclusion remains `tools/list` failure
  only).
- **Build one group per successful listing**: `Name: svc.Name`,
  `Blurb: instructions`, `Tools:` the existing `RawTool` wrapping (qualified
  `ikigenba_<svc>_<verb>`, same dispatch closure, same within-service duplicate
  guard) — the flat-slice assembly moves inside the per-peer group.

In the **package tests**: extend the existing fake-peer HTTP harness to answer
`initialize` (with and without `instructions`), and rework the discovery
assertions to the group shape.

## Done when

The suite is green (design *Conventions* commands, from `prompts/`) and:

- **R-K32H-6XAV** — a clearly-named test asserts a peer whose `tools/list`
  errors yields no group and `Discover` returns normally.
- **R-K4AD-KP1K** — a clearly-named test asserts a reachable peer listing one
  tool yields a group whose `Tools` holds exactly one tool with the
  service-qualified name and discovered schema, and that tool's `Call`
  dispatches to the peer.
- **R-9JNO-RZM2** — a clearly-named test asserts `mcpclient.Initialize` returns
  the `instructions` string when present and `""` (no error) when the field is
  absent.
- **R-9KVL-5RCR** — a clearly-named test asserts a peer with a successful
  `tools/list` but a failing (or instructions-less) `initialize` still yields
  its group with `Blurb == ""`.
- **R-9M3H-JJ3G** — a clearly-named test asserts an inventory of two reachable
  non-self peers plus a `prompts` self entry yields exactly two groups, each
  `Name` the peer's manifest service name and each `Blurb` that peer's
  published instructions, with no `prompts` group.

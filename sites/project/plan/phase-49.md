# Phase 49 — `sites.VersionClient`: the version-plane client and its test double

*Realizes design Decision 32 (the version-plane client seam — the client slice: R-EOTH-IB5B, R-NF7R-V95S, R-NGFO-90WH). Depends on Phase 48.*

`internal/sites/version.go` gains the `VersionClient` interface, the
`Owner`/`Commit`/`FileChange`/`TreeEntry` types, `ErrVersionUnavailable`, and
`NewVersionClient(base string, hc *http.Client)` — the single implementation,
speaking **two transports**: `Create`/`Rename`/`Delete` as `tools/call` against
repos' loopback `/mcp` with asserted `X-Owner-Id` / `X-Owner-Email` /
`X-Client-Id`, and `Commit`/`Export` on repos' loopback content routes. The MCP
half follows the existing in-suite precedent for a loopback peer client
(`repos/internal/repos/ghpeer.go`) — read it, do not reinvent the shape.

**Read repos' sealed design for the wire.** The exact tool names, paths, methods,
and request and response bodies are **repos' spec's**, not this tree's (D32, and
`$ikispec`'s cite-never-restate rule). This phase transcribes them from repos'
own `project/design` and cites `root project/design/D20.md` (one verb surface)
and `root project/design/D24.md` (the plane). Nothing about repos' wire is copied
into sites' `project/`.

`Export` translates repos' "this repository has no commits yet" signal into zero
entries and an empty commit rather than an error — repos pins that signal on its
own side, so it is a stable fact to transcribe, not a case to invent.

The phase also lands the shared test helper every later phase uses: a
**contract-conforming, recording `httptest` repos server** that answers both the
`/mcp` verb surface and the content routes, records every call it received
(transport, tool or route, asserted headers, and the decoded change set or export
request), and can be told to fail a named operation with a chosen error code.

**Done when:**

- R-EOTH-IB5B — a `Create`, a two-change `Commit`, an `Export`, a `Rename`, and
  a `Delete` yield exactly five recorded requests through the injected client,
  every recorded URL beginning with the constructed base, none escaping to
  `http.DefaultClient`, with the three verbs arriving as `tools/call` on the MCP
  endpoint and the two byte operations on the content routes.
- R-NF7R-V95S — every verb request carries `X-Owner-Id` / `X-Owner-Email` equal
  to the `Owner` argument (unrelated id and email values) and an `X-Client-Id`
  distinct from both.
- R-NGFO-90WH — `conflict` on create → `Create` returns nil; `not_found` on
  delete → `Delete` returns nil; `not_found` on rename → `Rename` returns a
  non-nil error; `validation` on create → `Create` returns a non-nil error. A
  client that swallows every code fails the last two; one that swallows none
  fails the first two.
- A connection-refused base yields errors satisfying
  `errors.Is(err, sites.ErrVersionUnavailable)` from every method (this is the
  sentinel R-EXCS-6PC6 depends on in Phase 50; it needs no id of its own).
- The suite is green.

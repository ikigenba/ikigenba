# Phase 42 — Conform the version-plane client to repos' real surface

*Realizes design Decision 36 (version-plane client), rewritten: the MCP domain
verbs, the plumbing byte routes, and the owner-carrying seam.*

`internal/repos.Client` is rebuilt to speak the surface repos actually serves:
`Create`/`Rename`/`Delete` issue JSON-RPC `tools/call` envelopes to
`POST {base}/mcp` with asserted `X-Owner-Id`/`X-Owner-Email`/`X-Client-Id`
headers; `Commit` posts the base64 `changes` batch to `POST /commit`;
`ReadFile` and `Head` use `GET /content` and `GET /list` (`kind=scripts` in
every query, the name key bare); `RunToken` posts `{"kind","name","ttl"}` to
`POST /run-token`, the runner passing its run TTL plus a five-minute margin.
The `script.VersionPlane` seam gains the `Owner` value on the three domain
verbs and drops the composed `RepoKey` addressing; `internal/script` call
sites, the runner's token mint, and the composition root follow the new
signatures. The MCP-envelope error mapping (`structuredContent.code` →
sentinels) joins the existing status mapping. The tests tagged with the
retired ids R-25E7-7ZFH, R-27TZ-ZIWV, and R-291W-DANK are deleted with the
behaviors they pinned; the recording-peer tests for the new ids assert the
**literal paths and envelopes** D36 pins, so a wrong-surface client fails in
this tree.

**Done when:**

- R-IKGJ-ND9W — domain verbs: exactly one `POST /mcp` each, correct tool name
  and arguments, all three identity headers — covered by a tagged test.
- R-ILOG-150L — envelope codes `conflict`/`not_found`/`validation` map to the
  sentinels; non-error envelope → nil; unknown code → non-sentinel error —
  covered by a tagged test.
- R-IO48-SOHZ — `Commit`: one `POST /commit`, base64 byte-identity (NUL and
  non-UTF-8 proven), `actor` in body, server `rev` returned verbatim — covered
  by a tagged test.
- R-IPC5-6G8O — `ReadFile`/`Head` wire shapes, empty `rev` → `ErrNotFound`,
  and the plumbing status mapping discrimination — covered by a tagged test.
- R-IQK1-K7ZD — `RunToken`: one `POST /run-token` with the duration-string
  `ttl`, token and clone URL verbatim; no owner or `X-Forwarded-Proto` headers
  on any plumbing request — covered by a tagged test.
- R-2A9S-R2E9 stays green under the new seam (composition root wiring
  unchanged in kind).
- `grep -rn 'repositories/' --include='*.go' .` under `scripts/` returns
  nothing.
- The suite is green per design Conventions (`go build ./...`,
  `go vet ./...`, `gofmt -l .` silent, `go test ./...` from `scripts/`).

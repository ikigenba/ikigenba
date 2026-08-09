# Phase 70 — Conform the version-plane client to repos' real surface

*Realizes design Decision 52 (version-plane client), rewritten: the MCP domain
verbs, the plumbing byte routes, the archive-tar `Read`, and the
owner-carrying seam.*

`internal/version`'s HTTP client is rebuilt to speak the surface repos
actually serves: `Create`/`Rename`/`Archive` issue JSON-RPC `tools/call`
envelopes (`create`/`rename`/`delete`) to `POST {base}/mcp` with asserted
`X-Owner-Id`/`X-Owner-Email`/`X-Client-Id` headers; `Commit` posts the base64
`changes` batch to `POST /commit`; `Head` reads `GET /list`'s top-level `rev`;
`Read` decodes one `GET /archive` tar into the `Definition`; `RunToken` posts
`{"kind","name","ttl"}` to `POST /run-token` and builds the `Credential` from
the response. The `Client` interface gains the `Owner` value on the three
domain verbs; `internal/prompt` call sites and the composition root follow the
new signatures. The MCP-envelope error mapping joins the existing status
mapping. The test tagged with the retired id R-RH3E-NZW4 is deleted with the
behavior it pinned (its no-owner-headers claim now holds only for plumbing
requests, under R-IXVF-UUFJ); the recording-peer tests for the new ids assert
the **literal paths and envelopes** D52 pins, so a wrong-surface client fails
in this tree.

**Done when:**

- R-IRRX-XZQ2 — domain verbs: exactly one `POST /mcp` each, correct tool name
  and arguments, all three identity headers, kind confinement under a hostile
  name key — covered by a tagged test.
- R-ISZU-BRGR — envelope codes `conflict`/`not_found`/`validation` map to the
  sentinels; non-error envelope → nil; unknown code → `ErrUnavailable` —
  covered by a tagged test.
- R-IU7Q-PJ7G — `Commit` wire shape: `/commit` path, base64 byte-identity
  (NUL and non-UTF-8 proven), delete entries without content — covered by a
  tagged test.
- R-IVFN-3AY5 — `Read` via `GET /archive`: tar decode, missing-`system.md` and
  missing-required-file semantics, 404 → `ErrNotFound` — covered by a tagged
  test.
- R-IWNJ-H2OU — `Head` via `GET /list`, empty `rev` → `ErrNotFound` — covered
  by a tagged test.
- R-IXVF-UUFJ — `RunToken` wire shape with the duration-string `ttl`, the
  built `Credential`, and no owner or `X-Forwarded-Proto` headers on any
  plumbing request — covered by a tagged test.
- R-RIBB-1RMT, R-RJJ7-FJDI, and R-RKR3-TB47 stay green under the new client.
- `grep -rn 'repositories/' --include='*.go' .` under `prompts/` returns
  nothing.
- The suite is green per design Conventions (`go build ./...`,
  `go vet ./...`, `gofmt -l .` silent, `go test ./...` from `prompts/`).

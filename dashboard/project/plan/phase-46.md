# Phase 46 — Accept a PAT as an HTTP Basic password on the git door

*Realizes design Decision 38 (git-door Basic-PAT introspection).*

`dashboard/internal/server` learns one new credential delivery form on exactly
one resource. `authn.go` gains the `gitDoorPrefix` constant and the
`isGitDoorURI` helper (query-stripped prefix match on the forwarded
`X-Original-URI`), and step (c)'s missing-or-malformed-bearer branch gains a
git-door-only fallback: parse `Authorization: Basic`, base64-decode, split at
the first colon, discard the username, and require the `pat.Prefix` on the
password before handing it to the **existing** `handleAuthnPAT` — same
`ValidatePAT`, same skipped resource binding, same workspace check, same
rate-limit key, same allow branch and identity headers. Failures on the git
door emit `WWW-Authenticate: Basic realm="ikigenba"` instead of the Bearer
challenge; failures anywhere else are untouched. The Basic path's audit
`Details` gain `"cred": "basic"` and its denies use the reasons
`missing_credential`, `basic_not_pat`, and `invalid_pat_basic`.

Nothing outside `internal/server` changes: no migration, no schema, no change
to `internal/pat`, no new record kind, and no new emitter — the edge records
come through D31's existing `recordEdge` seam. Tests are co-located in
`internal/server/*_test.go`, driving `(*app).routes()` with `httptest` against a
real migrated temp SQLite database, with the two delivery claims driven against
the live in-process ingest sink the existing edge tests already stand up.

**Done when:** the suite is green — `cd dashboard && go build ./...`,
`go vet ./...`, `gofmt -l .` printing nothing, and `go test ./...` all
succeeding with zero failures — and each id below is covered by a
genuinely-asserting, clearly-named test tagged with it:

- R-39O6-OFFD — Basic + valid PAT on a git-door `X-Original-URI` returns 200
  with all four `X-Owner-*` headers matching the bearer-PAT values, plus
  `X-Client-Id: pat:<public_id>` and `X-Token-Id`, and no `X-Chain-Id`.
- R-3AW3-2762 — usernames `""`, `"x"`, `"git"` with the same password all yield
  200 with byte-identical identity headers (the username is discarded).
- R-3C3Z-FYWR — the same valid-PAT Basic header on `/srv/crm/mcp` returns 401,
  reason `missing_bearer`, with today's `Bearer` challenge carrying
  `resource_metadata`.
- R-3DBV-TQNG — `/srv/repos/mcp` and `/srv/repos/gitolite/x` return 401 while
  `/srv/repos/git/code/foo.git/info/refs` returns 200 (prefix boundary).
- R-3EJS-7IE5 — a genuinely valid `ms_oat_` access token as the Basic password
  is refused on the git door with 401, reason `basic_not_pat`, no `X-Owner-*`.
- R-3FRO-LA4U — revoked and expired PATs via Basic on the git door each return
  401 whose `WWW-Authenticate` is exactly `Basic realm="ikigenba"` (no
  `Bearer`, no `resource_metadata`), reason `invalid_pat_basic`.
- R-3I7H-CTM8 — absent `Authorization`, non-base64 Basic payload, and base64
  without a colon each return 401 with the Basic challenge, reason
  `missing_credential`, without panicking.
- R-3JFD-QLCX — a bearer PAT on the git door still returns 200 with the same
  identity headers, and an invalid bearer there still returns 401.
- R-3KNA-4D3M — the Basic allow delivers exactly one `edge` record to the live
  sink with `detail.decision: "allow"` and `correlation_id` equal to the
  response's `X-Correlation-Id`, and its audit row carries `"cred": "basic"`
  while the bearer-PAT allow's does not.
- R-3LV6-I4UB — across a Basic allow and a Basic deny, the raw bytes the sink
  received contain neither the base64 credential blob, nor the decoded PAT
  plaintext, nor the literal `Authorization` header value.

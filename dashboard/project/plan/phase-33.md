# Phase 33 — Fail-closed introspection: all four identity headers on every allow, sourced from the identities row

*Realizes design Decision 19 (introspection always emits the four identity headers from the identities row, and fails closed). Depends on Phase 32.*

The three introspection endpoints in `internal/server/` (`handleAuthn`,
`handleAuthnPAT` in `authn.go`; `handleSessionAuthn` in `session_authn.go`)
change from best-effort to fail-closed emission: on every allow the endpoint
resolves the artifact's stamped `owner_id` via the identity store and emits
`X-Owner-Id`, `X-Owner-Email`, `X-Owner-Name`, `X-Owner-Picture` — all four
values read from the identities row (the artifact rows' `owner_email` copies
stop feeding headers). Any identity `Lookup` failure (not-found or store
error) is a 500 with no identity headers, logged loudly; the
`if err == nil { …set headers… }` fallback shape is gone. `headerEncode` and
the empty-value semantics for `name`/`picture` are unchanged. Existing tests
tagged R-W0P9-J50Z (behavior deleted from design: allow-on-lookup-miss) are
removed or re-tagged to the ids below; tests tagged R-VULR-MABI, R-VX1K-DTSW,
R-VY9G-RLJL, R-VZHD-5DAA are updated in place to the rewritten D19 behaviors
and keep their tags.

**Done when:**

- R-VULR-MABI — a `handleAuthn` allow carries all four identity headers plus
  `X-Client-Id`/`X-Chain-Id`/`X-Token-Id`, covered by a genuine test.
- R-VX1K-DTSW — a `handleAuthnPAT` allow carries all four identity headers
  with `X-Client-Id` (`pat:<public_id>`) unchanged, covered by a genuine test.
- R-VY9G-RLJL — a `handleSessionAuthn` allow carries all four identity
  headers, covered by a genuine test.
- R-HW5G-3JJF — when an artifact's stored `owner_email` differs from the
  identity row's email, `X-Owner-Email` carries the identities-row value,
  covered by a genuine test.
- R-HSHQ-Y8BC — `handleAuthn` returns 500 with no `X-Owner-*` headers when the
  chain's identity cannot be resolved, covered by a genuine test.
- R-HTPN-C021 — `handleAuthnPAT` returns 500 with no `X-Owner-*` headers when
  the PAT's identity cannot be resolved, covered by a genuine test.
- R-HUXJ-PRSQ — `handleSessionAuthn` returns 500 with no `X-Owner-*` headers
  when the session's identity cannot be resolved, covered by a genuine test.
- R-VZHD-5DAA — non-ASCII / CR-LF `name`/`picture` emit as pure US-ASCII,
  CR/LF-free header values that percent-decode back exactly, covered by a
  genuine test.
- R-HXDC-HBA4 — empty `name`/`picture` yield an allow with present-but-empty
  `X-Owner-Name`/`X-Owner-Picture` and non-empty `X-Owner-Id`/`X-Owner-Email`,
  covered by a genuine test.
- `grep -rn "R-W0P9-J50Z" --include='*_test.go' .` returns no matches (the
  deleted behavior's tag is gone from the test suite).
- The suite is green per design Conventions: `cd dashboard && go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed.

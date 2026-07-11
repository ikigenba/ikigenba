# Phase 23 — introspection emits the identity headers

*Realizes design Decision 19 (introspection emission) — ids R-VULR-MABI,
R-VX1K-DTSW, R-VY9G-RLJL, R-VZHD-5DAA, R-W0P9-J50Z. Depends on Phase 22 (the
stamped `owner_id` on the auth artifacts) and Phase 20 (`identity.Lookup`).*

Adds three response headers — `X-Owner-Id` (the opaque handle, verbatim),
`X-Owner-Name`, `X-Owner-Picture` (percent-encoded, empty when absent) — to the
allow path of all three introspection endpoints: `handleAuthn` (bearer, off
`chain.owner_id`), `handleAuthnPAT` (off the PAT row's `owner_id`), and
`handleSessionAuthn` (off the session's `owner_id`; previously it emitted only
`X-Owner-Email`). Each reads the handle, calls `a.identity.Lookup`, and sets the
headers via a small percent-encoding helper. Every existing header is emitted
unchanged; a not-found lookup still allows and simply omits the new headers.
**Emission only** — nginx forwarding and service consumption are out of this
`project/`'s scope and untouched.

**Done when:** the suite is green (per design *Conventions*) and each id below is
covered by a clearly-named HTTP-level `server`-package test asserting the
introspection response headers directly:

- R-VULR-MABI — `handleAuthn` allow carries `X-Owner-Id`/`X-Owner-Name`/
  `X-Owner-Picture` for the chain's identity, with `X-Owner-Email`/`X-Client-Id`/
  `X-Chain-Id`/`X-Token-Id` unchanged.
- R-VX1K-DTSW — `handleAuthnPAT` allow carries the three new headers, with
  `X-Owner-Email`/`X-Client-Id` (`pat:<public_id>`) unchanged.
- R-VY9G-RLJL — `handleSessionAuthn` allow carries the three new headers in
  addition to the existing `X-Owner-Email`.
- R-VZHD-5DAA — a `name`/`picture` with a non-ASCII rune and/or CR/LF is emitted
  pure US-ASCII with no CR/LF, and percent-decoding round-trips to the original.
- R-W0P9-J50Z — empty `name`/`picture` yields empty header values (no sentinel),
  and a not-found identity lookup still returns allow with existing headers
  intact and no panic.

# Phase 21 — googleidp decodes the identity claims

*Realizes design Decision 18 (capture at login), the claim-decode slice — ids
R-VPQ6-37CQ only. Depends on no earlier phase (isolated to `internal/googleidp`).*

Extends `internal/googleidp` to surface the full identity from the already-
verified id_token. `idTokenClaims` gains `Name` and `Picture` (and `Iss`,
already read for validation, is surfaced), and the `Identity` struct gains
`Iss`, `Name`, `Picture`. `verifyIDToken` populates them after the existing
signature/`iss`/`aud`/`exp` checks; absent `name`/`picture` decode to empty
strings and the identity remains valid. No storage, callback, or header change
here — this phase only makes the claims available on `Identity` for phase 22 to
consume.

**Done when:** the suite is green (per design *Conventions*) and the id below is
covered by a clearly-named test in `internal/googleidp/googleidp_test.go`, using
the package's existing crafted-token seam (no live Google):

- R-VPQ6-37CQ — a verified id_token carrying `iss`/`name`/`picture` yields an
  `Identity` whose `Iss`/`Name`/`Picture` equal those claims; one lacking
  `name`/`picture` yields empty `Name`/`Picture` with `Sub`/`Email` intact and
  no error.

# Phase 33 — Register at lint tier `cheap`

*Realizes design Decision 22 (adopt the suite lint contract at tier `cheap`).*

A structural phase: commit `appkit/.lint-tier` containing exactly `cheap`
(one line). The tree is already clean at the cheap tier, so no code change is
expected; if the pinned linter surfaces a finding anyway, fix it as part of
this phase — the marker and a clean run land together.

**Done when:** `cat appkit/.lint-tier` prints exactly `cheap`;
`bin/lint appkit` (from the repo root) exits 0 reporting tier `cheap`; and
the suite is green (`GOWORK=off go build ./...` and `GOWORK=off go test
./...` from `appkit/` succeed).

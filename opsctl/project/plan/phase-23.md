# Phase 23 — The committed manual-layer verification runbook

*Realizes design Decision 17 (the manual layer as a committed runbook), carrying
its own id `R-2B4O-Z98N` and absorbing the eight real-substrate (live-box) ids
`R-WRJF-H7J9` (D1), `R-66UP-LI59` (D2), `R-6FE0-9WC4` (D3), `R-MYS7-2H2R` (D4),
`R-AXY7-K8GA` (D8), `R-B0E0-BRXO` (D9), `R-JRO8-5Q0R` (D10), `R-MMF1-HFMO`
(D11), whose proof is the runbook entry rather than a tagged test. Depends on
Phase 21.*

What gets built:

- **`opsctl/project/opsctl-verification.md`** — the committed manual-layer
  runbook, following `github/project/github-verification.md`: a short preamble
  saying why these checks are out of gate and how the box is reached
  (`ssh int`, `sudo bash -c 'set -a; . /etc/ikigenba/env; opsctl <verb> …'`),
  then one section per check. Each section names the check, states the
  **positive** check (the exact commands and the observable pass criterion) and
  the **negative** check (what to break, and the loud failure that proves the
  positive check exercised the real box rather than a no-op), and points at
  where the run is recorded. Nine sections: one per absorbed id above, plus the
  `web`-group access check the umbrella's `root project/design/D01.md` places
  here — a process in the `web` group can read `state/www/public` and
  `state/www/private`, and can neither read `state/<svc>.db` nor list `state/`
  (negative form: the same reads as a non-`web` user, and the denied `state/`
  listing as the `web`-group user).
- **`opsctl/internal/opsctl/testing_contract_test.go`** — extended with the
  doc-truth test for `R-2B4O-Z98N`.

**Done when:**

- `R-2B4O-Z98N` — a test collects from `opsctl/project/design/D*.md` every id
  whose Verification entry marks it real-substrate/live-box, reads
  `opsctl/project/opsctl-verification.md`, and asserts (a) every collected id
  appears in the runbook, and (b) each such runbook entry states both a positive
  and a negative check. Adding a real-substrate id to a Decision without a
  runbook entry fails; an entry with only a positive check fails.
- The runbook exists and covers all nine items:
  `test -f opsctl/project/opsctl-verification.md` exits 0, and
  `grep -c -E 'R-WRJF-H7J9|R-66UP-LI59|R-6FE0-9WC4|R-MYS7-2H2R|R-AXY7-K8GA|R-B0E0-BRXO|R-JRO8-5Q0R|R-MMF1-HFMO' opsctl/project/opsctl-verification.md`
  reports at least 8 matching lines, and the runbook contains a section for the
  `web`-group check (`grep -c 'web.*group' …` ≥ 1).
- The suite is green: `GOWORK=off go build ./...` and `GOWORK=off go test ./...`
  from `opsctl/` both succeed.

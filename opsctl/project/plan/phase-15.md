# Phase 15 — init-box adds git and sqlite to the box-baseline package install

*Realizes design Decision 10 (`project/design/D10.md`). Depends on Phase 14 (the
current init-box package-install shape, `install-packages:nginx,certbot,poppler-utils`).
Retires R-WHC0-I9HL and R-WIJW-W18A (the three-package baseline they asserted is
superseded by the five-package baseline). Unit id R-JQGB-RYA2 is loop-driven here;
the live-box id R-JRO8-5Q0R is a real-substrate check the fake `System` cannot
falsify — operator-verified out-of-loop (partial-Decision split). Touches
`internal/opsctl/initbox.go` and the init-box/provision tests only; the `System`
seam is reused unchanged.*

Extend init-box's step-1 package install to carry `git` and `sqlite` alongside
`nginx`, `certbot`, and `poppler-utils`, so every provisioned box has `git`
(repository provisioning for the repos service) and the `sqlite3` CLI (per-app DB
inspection) as box-global substrate, per D10. The observable end state:

- `InitBox` requests **one** package install through the existing
  `System.InstallPackages` seam, now listing `nginx`, `certbot`, `poppler-utils`,
  `git`, `sqlite` — still as step 1, before the cert/nginx branch, so it runs on
  the `--skip-cert` path too.
- Idempotency is unchanged: the seam's `dnf install -y` contract no-ops on
  already-present packages; init-box remains safe to re-run.
- Existing provision tests that assert the recorded op
  `install-packages:nginx,certbot,poppler-utils` are updated to the new
  five-package op; no other init-box behavior changes.

Non-goals: no new seam, verb, flag, or package-list constant; no `git-core`
substitution for the `git` meta-package; no change to per-service `setup
--packages`; nothing in the repos or prompts services (their sandboxes/processes
merely find the binaries on the box).

**Done when** the suite is green — `GOWORK=off go build ./...` succeeds and
`GOWORK=off go test ./...` passes from `opsctl/` — and this id is covered by a
clearly-named test (temp `OPSCTL_ROOT` + the fake `System`):

- **R-JQGB-RYA2** — after `InitBox` (including on the `--skip-cert` path), the
  fake `System` has recorded `install-packages:nginx,certbot,poppler-utils,git,sqlite`.
  The test fails against the phase-14 `initbox.go`, which records
  `install-packages:nginx,certbot,poppler-utils`.

Operator-verified out-of-loop (not loop-driven): **R-JRO8-5Q0R** — on
`int.ikigenba.com` after `opsctl init-box`, `command -v git sqlite3 pdftotext
pdftoppm pdfinfo` succeeds and `git --version`, `sqlite3 --version`, and
`pdftotext -v` exit cleanly, and a rerun of init-box succeeds with the packages
already present.

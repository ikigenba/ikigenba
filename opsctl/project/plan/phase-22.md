# Phase 22 — Prove the backup archive's state-only boundary (umbrella `[proof: opsctl]`)

*Realizes the umbrella backup-boundary ids `R-O3Q6-EB2A` and `R-O4Y2-S2SZ`,
minted by `root project/design/D05.md` and marked `[proof: opsctl]` — opsctl is
the one tree that carries their tagged tests. Depends on nothing pending.*

The suite contract says a backup captures `state/` and **only** `state/`: the
non-state region (`cache/`, including the `<svc>.db.generation` sidecar) is
derived, never archived, and never survives a restore. opsctl's existing
backup/restore tests (`internal/opsctl/backup_test.go`) assert the positive
half — that `state/` round-trips (`R-49DF-LLN5`) — and nothing asserts the
exclusion. Two tests are added to that file, both hermetic: a real temp-dir
service tree, the real `tar` binary, and the real archive listing.

**Done when:**

- `R-O3Q6-EB2A` — backing up a **populated** service tree (a DB file plus at
  least one other durable file under `state/`, and pre-existing content under
  `cache/` including a `<svc>.db.generation` sidecar) produces an archive whose
  listing contains the `state/` entries and **no** entry whose path lies under
  `cache/` and **no** `*.generation` entry. The assertion reads the real archive
  listing, not opsctl's in-memory intent; an implementation that adds `cache/`
  to the archive fails.
- `R-O4Y2-S2SZ` — after a real `restore` into a tree whose `cache/` already
  holds content (including a generation sidecar with distinguishable bytes), the
  non-state region is a clean slate: the pre-existing `cache/` content is gone.
  An implementation that leaves the stale sidecar in place fails; asserting on
  distinguishable bytes rather than mere absence-or-presence keeps a recreated
  empty `cache/` from passing as "restored".
- The suite is green: `GOWORK=off go build ./...` and `GOWORK=off go test ./...`
  from `opsctl/` both succeed.
- Both ids appear verbatim as tags in `opsctl/internal/opsctl/*_test.go`:
  `grep -rl 'R-O3Q6-EB2A' --include='*_test.go' --exclude-dir=project .` and the
  same for `R-O4Y2-S2SZ` each print exactly 1 path.

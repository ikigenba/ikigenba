# Phase 10 — restore re-asserts the served-tree `web` invariant after replacing state

*Realizes design Decision 9 (`project/design/D09.md`). Unit id `R-AZ63-Y06Z` is
loop-driven here; the live-box id `R-B0E0-BRXO` is a real-substrate check
operator-verified out-of-loop (partial-Decision split). Depends on phase 08 (the
`ensureWWWPerms` helper). Touches `internal/opsctl/backup.go` (the `Restore` path)
and its test only.*

Make restore reconstitute the served tree to the invariant rather than replaying
whatever ownership/mode the snapshot captured. The observable end state:

- **Code.** In `(*Opsctl).Restore`, after `replaceStateFromArchive` succeeds and
  alongside the existing `cache/` recreate + chown (D01), before the deferred unit
  restart, add `if err := o.ensureWWWPerms(ctx, app, l); err != nil { return
  fmt.Errorf("restore: restore www perms: %w", err) }`. This re-labels
  `state/www` `<app>:web` **and** re-applies setgid (load-bearing here — an older
  snapshot's tier dirs may lack it), and is a no-op when the app has no
  `state/www`.
- No other restore behavior changes: the state wipe/untar, the `cache/` recreate
  (D01), the `.generation` removal, the dashboard cert restore, and the deferred
  restart are all as before.

Non-goals: no change to the archive format, the state untar, the `cache/` step, or
the restart; no reliance on a later deploy to fix perms (D09 rejects that — restore
must stand on its own).

**Done when** the suite is green — `GOWORK=off go build ./...` succeeds and
`GOWORK=off go test ./...` passes from `opsctl/` — and this id is covered by a
clearly-named test (temp `OPSCTL_ROOT` + the fake `System`):

- **R-AZ63-Y06Z** — after `Restore` of an app whose `state/www` exists, the fake
  records a `ChownTree(app, "web", WWWDir())` and the tier-dir `Chmod(02750)`
  calls, ordered **after** the state replacement and **before** the deferred unit
  restart, in addition to the existing `cache/` chown. Fails against today's
  `Restore`, which chowns only `cache/` and never touches the served tree.

Operator-verified out-of-loop (not loop-driven): **R-B0E0-BRXO** — after a real
`opsctl restore sites <key>` on `int.ikigenba.com`, an anonymous `GET
https://int.ikigenba.com/srv/sites/public/<published-site>/` returns 200,
demonstrating the restored tree is servable regardless of the snapshot's captured
metadata.

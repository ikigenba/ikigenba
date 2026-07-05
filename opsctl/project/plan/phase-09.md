# Phase 09 — deploy re-asserts the served-tree `web` invariant after the state chown

*Realizes design Decision 8 (`project/design/D08.md`). Unit ids `R-AVIE-SOYW` and
`R-AWQB-6GPL` are loop-driven here; the live-box id `R-AXY7-K8GA` is a
real-substrate check operator-verified out-of-loop (partial-Decision split).
Depends on phase 08 (the `ensureWWWPerms` helper). Touches
`internal/opsctl/deploy.go` and its test only.*

Stop the deploy's DB-ownership chown from silently locking nginx out of the served
tree. The observable end state:

- **Code.** In `Deploy`, **immediately after** the existing
  `ChownTree(ctx, app, app, l.StateDir())`, add `if err := o.ensureWWWPerms(ctx,
  app, l); err != nil { return fmt.Errorf("deploy: restore www perms: %w", err)
  }`. The state chown itself is unchanged (its DB-ownership purpose stands); the
  new call re-labels `state/www` `<app>:web` + re-setgids it, and is a no-op when
  the app has no `state/www` (the helper self-guards on existence).
- No other deploy behavior changes: stage, migrate, the state chown, the apex
  block render (D4), the three-symlink swap, and the restart are all as before.

Non-goals: no change to the state chown's scope or the migrate/stage/swap/restart
steps; no narrowing of the sweep and no run-migrate-as-app change (both considered
and rejected in D08); no nginx restart (membership from phase-07 already lets
nginx read a re-labelled tree).

**Done when** the suite is green — `GOWORK=off go build ./...` succeeds and
`GOWORK=off go test ./...` passes from `opsctl/` — and these ids are each covered
by a clearly-named test (temp `OPSCTL_ROOT` + the fake `System`):

- **R-AVIE-SOYW** — after `Deploy` of an app whose `state/www` exists, the fake
  records a `ChownTree(app, "web", WWWDir())` (and the tier-dir `Chmod(02750)`
  calls) ordered **strictly after** the `ChownTree(app, app, StateDir())` sweep.
  Fails against today's `deploy.go` (state sweep, then no `web` re-chown).
- **R-AWQB-6GPL** — after `Deploy` of an app with no `state/www` (e.g. the DEFAULT
  app), the fake records **no** `ChownTree(_, "web", _)` and no www `Chmod` — the
  existence self-guard holds.

Operator-verified out-of-loop (not loop-driven): **R-AXY7-K8GA** — after a real
`opsctl deploy sites <version>` on `int.ikigenba.com` (via `sudo bash -c 'set -a;
. /etc/ikigenba/env; opsctl deploy sites <version>'`), an anonymous `GET
https://int.ikigenba.com/srv/sites/public/<published-site>/` returns 200,
demonstrating the served tree survived the deploy readable by nginx.

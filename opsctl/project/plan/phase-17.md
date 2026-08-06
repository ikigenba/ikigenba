# Phase 17 — App-owned `cache/` in setup and deploy

*Realizes design Decision 12 (`cache/` app-owned by construction).*

The June 2026 telemetry first-deploy crash-looped on
`generation sidecar: open /opt/telemetry/cache/telemetry.db.generation:
permission denied`: setup creates `cache/` root-owned in every tree branch and
deploy's post-migrate hand-back covers `state/` only. Restore (D1) and
rollback already honor app ownership of `cache/`; this phase closes the two
remaining verbs.

What gets built:

- `internal/opsctl/setup.go` — each of the three tree branches (DEFAULT,
  worker, fragment) hands `cache/` to the service user via
  `ChownTree(app, app, l.CacheDir())` after creating the tree.
- `internal/opsctl/deploy.go` — after the root-run migrate and next to the
  existing `state/` hand-back, an unconditional
  `ChownTree(app, app, l.CacheDir())` runs before the unit restart.

**Done when:**

- R-4ZI0-4CH5 covered by a genuinely-asserting tagged test: all three setup
  modes record a `ChownTree(app, app, CacheDir())` seam call.
- R-50PW-I47U covered by a genuinely-asserting tagged test: an ordinary
  successful deploy records `ChownTree(app, app, CacheDir())` after migrate
  and before restart.
- Neither test is skipped, build-tagged, or env-gated.
- The suite is green per design Conventions: from `opsctl/`,
  `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0.

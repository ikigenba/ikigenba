# Phase 4 — Retention window resolution and the pruner

*Realizes design Decision 4 (retention). Depends on Phase 02.*

`internal/retention` gains the only code path that removes records, and
`cmd/telemetry`'s `Handlers` hook starts it.

- `Days(getenv, log)` resolving `TELEMETRY_RETENTION_DAYS` with the 90-day
  fallback for absent, empty, unparseable, or non-positive values, logging the
  fallback at warn with the offending value. `0` never means "prune everything";
  an unparseable value never means "never prune".
- `Pruner` with `PruneOnce` (cutoff = `clock.Now() - days*24h`, delegating to
  `Store.PruneBefore`) and `Run(ctx, ticks)` — an immediate prune at start, then
  one per tick, with prune errors logged and retried on the next tick rather
  than fatal. The ticker is a parameter so tests drive it deterministically.
- `Start(rt, store, clock)` wiring the pruner from config onto a real hourly
  ticker for the process's lifetime.

**Done when:**

- Every id below is covered by a clearly-named, id-tagged test:
  - R-VRDP-RPK1 — with the env var unset, `Days` is 90 and a prune keeps an
    89-day-old record while removing a 91-day-old one.
  - R-VSLM-5HAQ — `""`, `"0"`, `"-5"`, `"forever"`, and `"90.5"` each resolve to
    90; a pruner configured from `"0"` keeps a 1-day-old record and still
    removes a 91-day-old one.
  - R-VTTI-J91F — `Run` prunes at start before any tick, and prunes again on a
    delivered tick after the injected clock advances, with no restart.
  - R-VV1E-X0S4 — a prune leaves `schema_migrations` intact and the database
    usable, and a `PruneBefore` error is logged without stopping `Run` — a
    following tick still prunes successfully.
- Retention is not reachable from the MCP surface and is the only delete path:
  `grep -rn 'PruneBefore' --include='*.go' --exclude='*_test.go' --exclude-dir=project .`
  run from `telemetry/` matches only files under `internal/db/` and
  `internal/retention/`.
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`,
  `go test ./...` all exit 0 in `telemetry/`.

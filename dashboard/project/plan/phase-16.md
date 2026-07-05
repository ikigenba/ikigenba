# Phase 16 — The collector worker: startup sample + 60s ticker, wired on the appkit Workers seam

*Realizes design Decision 13 (collector worker). Depends on Phase 15 (the store +
readers). Adds `Collect`/`Run` to `internal/telemetry` and the `Spec.Workers`
wiring in `cmd/dashboard/main.go`; no HTTP handler, no template, no schema.
Confined to the dashboard tree.*

Turn the Phase 15 readers into a running background sampler bound to the serve
lifecycle.

**1. `Collect` (D13).** One sample sweep: system free memory + free disk, and for
each discovered service its cgroup memory and `/opt/<svc>` disk, all `Append`ed to
the store, with the capacities recorded via `SetCapacities`. A source that is
absent records `0`; a source that returns an **unexpected** error records `0`
**and** logs (via the passed `*slog.Logger`) without aborting the rest of the
sweep. `Config` carries the injectable `ManifestRoot`/`CgroupRoot`/`DiskPath`/
`MemInfoPath`/`Interval`, defaulted to the production paths and `time.Minute`.

**2. `Run` (D13).** Takes one sample **immediately**, then samples on every
`Interval` tick, and returns `nil` when its context is cancelled (clean shutdown).

**3. Wiring (`cmd/dashboard/main.go`).** Construct one shared `telemetry.NewStore()`
at the composition root; add a `Spec.Workers` entry — a closure capturing the store
and the `rt` router (set in the `Handlers` hook, live by the time the worker runs)
— that calls `telemetry.Run(ctx, store, cfg, rt.Logger())` with `cfg.ManifestRoot`
= the resolved manifest root. Follow the `notify`/`dropbox` `var rt *appkit.Router`
capture idiom. (The store is not yet read by any handler — Phase 18 threads it into
`server.Register`; here it is created and written by the collector.)

**4. Tests** in `internal/telemetry/*_test.go` drive `Run`/`Collect` with fake
sources (function-valued or fixture-backed) and a short injected interval, plus a
capturing `slog` handler for the error-path assertion.

**Done when:** the suite is green — `cd dashboard && go build ./...`, `go vet
./...`, `gofmt -l .` (no output), `go test ./...`, and `bin/check-migrations
dashboard` (from the repo root) all succeed with zero failures — and every id below
is covered by a genuine named test:

- **R-FDAM-Q7U8** — `Run` records a sample for every series immediately at startup,
  before one interval elapses.
- **R-FEIJ-3ZKX** — `Run` samples again on each tick (N ticks → N+1 samples).
- **R-FFQF-HRBM** — `Run` returns `nil` promptly when its context is cancelled.
- **R-FGYB-VJ2B** — `Collect` records `0` and logs on an unexpected reader error,
  without blanking sibling series.

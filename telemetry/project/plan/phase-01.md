# Phase 1 — Module skeleton, chassis Spec, composition root, manifest and VERSION

*Realizes design Decision 1 (service skeleton & composition root).*

The `telemetry/` module comes into existence as a buildable, servable appkit
app with no domain behavior yet. End state:

- `go.mod` declaring module `telemetry` with committed `replace` directives for
  `appkit => ../appkit` and `registry => ../registry` (no `eventplane`
  dependency — telemetry has no event-plane role), plus the `modernc.org/sqlite`
  driver the chassis expects. The root `go.work` already wires modules for local
  dev; the module must also build with `GOWORK=off`.
- `cmd/telemetry/main.go` — the composition root exactly as D1 states it:
  `appkit.Main(telemetrySpec())` with `App`, `Mount`, `Port` from
  `registry.MustPort("telemetry")`, `MCP: true`, `Migrations: db.FS`,
  `TelemetryExclude: []string{"/ingest"}` (the chassis's exact-path exclusion
  from HTTP request recording — the route itself arrives in phase 3), and the
  single ordered `ManifestExtras` entry `TELEMETRY_RETENTION_DAYS=90`. No
  `Feed`, no `Consumes`, no `Events`, no `WWW`. The `Handlers` hook exists and,
  for this phase, wires nothing beyond obtaining and validating the DB handle —
  later phases fill in the MCP surface, the ingest route, and the pruner at the
  call sites D1 names.
- `internal/db/` with the embedded migrations FS (`//go:embed migrations/*.sql`)
  and the suite bootstrap `001_schema_migrations.sql` copied verbatim from an
  existing service. The domain schema is phase 2's.
- `internal/telemetry/clock.go` — the `Clock` interface and `RealClock`, the
  seam design's *Conventions* require. (Born here because the Spec and every
  later package need it.)
- `etc/manifest.env` authored to match what the Spec emits, and `VERSION`
  committed as `v0.1.0` (product's contractual constant).
- A `Makefile` for local dev mirroring the sibling services'.

Nothing here reaches outside `telemetry/`. `etc/nginx.conf` is phase 6's; this
phase creates no fragment.

**Done when:**

- `cd telemetry && go build ./...`, `go vet ./...`, and `go test ./...` all exit
  0, and `GOWORK=off go build ./...` also exits 0.
- The phase's ids are covered by clearly-named, id-tagged tests:
  - R-V6NF-9LY8 — the manifest verb's emitted bytes equal the committed
    `etc/manifest.env` byte-for-byte, including `MCP=true`,
    `TELEMETRY_RETENTION_DAYS=90`, `PORT` equal to
    `registry.MustPort("telemetry")`, and no `FEED=`/`CONSUMES=` key.
  - R-V7VB-NDOX — `Spec.Port == registry.MustPort("telemetry")`; that port
    number appears as a literal in no `*.go` file in the module; and a served
    instance on an env-overridden ephemeral loopback port answers a real HTTP
    request.
- `cat telemetry/VERSION` outputs exactly `v0.1.0`.
- The module declares no eventplane dependency:
  `grep -rn 'eventplane' --include='*.go' --include='go.mod' --exclude-dir=project .`
  run from `telemetry/` returns empty (exit 1).

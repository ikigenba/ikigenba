# Phase 121 — The ingest job stores its originating chain id; self-started work roots its own

*Realizes design Decision 65 (chain inheritance and root minting) — the storage and minting slice. D65's remaining id, the sweep's one-root-per-cycle assertion, is proven on the wire and belongs to Phase 122.*

**Cross-workspace dependency.** wiki cannot build until the shared modules land:
`appkit` must carry the correlation middleware (read-or-mint, the context
accessor, the Crockford minter), the telemetry recorder, and the shared
instrumented outbound client. The suite build order is registry/root → appkit
and eventplane → the telemetry service → dashboard → the remaining services,
of which wiki is one. `eventplane` is irrelevant to wiki (no wiki package
imports it).

**What gets built.**

- One new forward-only migration under `wiki/internal/db/migrations/`, authored
  with `bin/create-migration wiki <name>` (never hand-numbered, never editing a
  committed file), adding `correlation_id TEXT NOT NULL DEFAULT ''` to `jobs`.
- `internal/wiki` — `Job` gains `CorrelationID`; the job store reads and writes
  it; `Service.Ingest` captures the chain id from its context onto the row in
  the same insert that records `owner_id`/`owner_email`, write-once (a re-run
  does not re-root the job).
- `internal/wiki` — the per-job attribution derivation (`jobAttribution`)
  sources its correlation value from the row's stored id, and when that value is
  empty mints **one** root for the whole job through appkit's root-start helper
  (mint + install in context + emit the `root` record).
- `internal/wiki` — the catch-up embedding sweep (D35) calls the same root-start
  helper **once per drain cycle**, installing the root on the context the
  cycle's embeds run under. Not once per page, and not once for the loop's
  lifetime. The boot `RequeueWorking` sweep is deliberately left alone: it makes
  no outbound call and publishes no event, so it has no hop to correlate.

The `root` record's shape and the recorder behind it are the chassis's; wiki
asserts neither. The prompts-call wiring that carries these values onto the
wire — including the sweep's per-cycle assertion — is Phase 122.

**Done when:**

- `R-XGME-DMUD` — a test over a real temp migrated SQLite asserts `Service.Ingest`
  under a context carrying chain id `X` writes a job row whose `correlation_id`
  reads back as exactly `X`, and under a bare context writes the empty string —
  not the job id, not an invented value.
- `R-XJ27-56BR` — a test asserts the per-job attribution derivation returns the
  row's stored `correlation_id` when non-empty (proven with a row whose stored
  id differs from its `job.ID`, so returning `job.ID` fails), and when the
  stored value is empty returns one freshly minted 26-character Crockford
  base32 ULID for the whole job — neither empty nor `job.ID`, identical across
  that job's stages, and different for a second empty-correlation job.
- `R-XKA3-IY2G` — a test migrates a database to the previous version, seeds job
  rows, migrates forward through the appkit runner against the real
  `modernc.org/sqlite` engine, and asserts every seeded row survives with its
  original column values and an empty `correlation_id`.
- The suite is green per design's *Conventions*: `go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
  zero failures.

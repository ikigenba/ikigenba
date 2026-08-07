# Phase 11 — Prove the adopted suite contracts at the composition root

*Realizes design Decision 9 (suite-contract conformance: install layout + authored env contract).*

telemetry adopts three `[proof: per-service]` contract ids (D9) and currently
proves none of them. This phase extends `cmd/telemetry/main_test.go` (already
`package main`, so `telemetrySpec()` is directly callable) with a genuine,
offline test per adopted id, matching the shape the other adopting services
carry (`crm/cmd/crm/main_test.go`, `dropbox`, `github`, `repos`).

End state — `cmd/telemetry/main_test.go` additionally holds:

- **A committed-manifest portability test** (`R-8DF1-W89F`): reads
  `../../etc/manifest.env` and fails if the bytes contain an absolute `/opt/`
  path or a line beginning `TELEMETRY_DB_PATH=` or `TELEMETRY_GENERATION_PATH=`.
- **A manifest byte-equality test** (`R-8IAN-FB87`): builds a
  `manifest.Fields` from the `appkit.Spec` that `telemetrySpec()` returns —
  `App`, `Mount`, `Default`, `Port`, `MCP`, the (empty) `Feed`, the (empty)
  consumer sources, and the Spec's `ManifestExtras` — renders it with
  `appkit/manifest.Emit`, and fails unless the result is byte-identical to the
  committed `../../etc/manifest.env`. Read the fields **off the Spec value**,
  not from re-typed literals, so editing the Spec without editing the committed
  file (or the reverse) fails the test.
- **A boot-from-the-install-layout smoke** (`R-4LKF-FB23`): builds the binary
  into a `t.TempDir()` tree shaped like `/opt/telemetry/` — `state/`, `cache/`,
  `libexec/telemetry-<VERSION>` with `bin/run` symlinked to it, and
  `etc/<VERSION>/manifest.env` copied byte-for-byte from the committed authored
  file with `etc/current` → `<VERSION>`. The test asserts `bin/run` and
  `etc/current` resolve to their intended targets and that the manifest selected
  through `etc/current` equals the committed one, then runs `bin/run serve` on a
  free loopback port and polls `/health`. It asserts `200` with
  `service: "telemetry"`, `status: "ok"`, and `details.dropped_total` of `0`
  against the fresh DB, and that the DB landed under `state/` and the generation
  sidecar under `cache/`.

Five constraints this phase must respect, all stated in D9:

- The Spec has **no `Config` hook**: no credentials, key material, or provider
  access is needed to serve. Do not invent throwaway secrets for the smoke — the
  only environment it sets is the composed data paths (or `IKIGENBA_ROOT`
  pointing at the temp tree), the bind IP/port, and the recorder switch below.
- The chassis recorder's ingest URL defaults to
  `registry.BaseURL("telemetry") + "/ingest"` — the **shared host port**, which a
  running suite from another worktree may own. The smoke must set
  `TELEMETRY_ENABLED=false`, or point `TELEMETRY_INGEST_URL` at the temp
  instance's own ephemeral port. It must never bind or post to the registry port,
  and must not depend on any stack being up.
- telemetry declares no `WWW` and embeds no web assets: there is **no `share/`
  tier** to stage and no `TELEMETRY_WWW_PATH` to set. Do not add one to make the
  tree look like `repos`'.
- telemetry is neither an event-plane producer nor a consumer, so nothing here
  waits on a feed. The `cache/` assertion still holds: appkit's `serve` calls
  `outbox.EnsureGeneration` on the composed generation path unconditionally, so
  the sidecar must appear under `cache/`.
- The `Health` reporter reads only the local store, so the envelope is genuinely
  green offline. Do not weaken the assertion to a bare `200` — pin
  `status: "ok"` and the `dropped_total` value.

`R-8IAN-FB87` is a different claim from the existing
`TestManifestMatchesCommittedFile` (`R-V6NF-9LY8`, D1), which pins the binary's
`manifest` **verb**. Leave that test as it is; do not fold either into the other.

No non-test source changes are expected. If a test cannot be written without a
production-code seam, add the smallest seam that makes the behavior observable —
do not weaken the assertion to fit the current shape.

**Done when:**

- `R-4LKF-FB23` — a fresh `/opt/telemetry/`-shaped temp root boots the built
  binary via `bin/run serve` and serves the health envelope, with the DB under
  `state/` and the generation sidecar under `cache/`, and `bin/run` /
  `etc/current` resolving to their intended targets; the id appears verbatim as a
  `// R-4LKF-FB23` tag on that test.
- `R-8DF1-W89F` — the committed `etc/manifest.env` carries no `/opt/` path and
  no `TELEMETRY_DB_PATH=` / `TELEMETRY_GENERATION_PATH=` line; the id appears
  verbatim as a `// R-8DF1-W89F` tag on that test.
- `R-8IAN-FB87` — `manifest.Emit` over the fields read off `telemetrySpec()`
  byte-equals the committed `etc/manifest.env`; the id appears verbatim as a
  `// R-8IAN-FB87` tag on that test.
- All three tests are genuine — each fails when its subject is broken (verify by
  temporarily perturbing the committed manifest, the Spec's declared fields, and
  the staged `etc/current` symlink, seeing the corresponding test fail, then
  restoring). No `t.Skip`, no test that passes vacuously.
- The suite is green per design's *Conventions*, from `telemetry/`:
  `go build ./...` exits 0, `go vet ./...` is clean, and `go test ./...` passes
  with no failures and no `SKIP`.
- From `telemetry/`, all three ids are tagged in **test** files (not merely in
  the spec) — this command prints exactly `3`:

  ```
  grep -rhoE 'R-4LKF-FB23|R-8DF1-W89F|R-8IAN-FB87' --include='*_test.go' --exclude-dir=project . | sort -u | wc -l
  ```

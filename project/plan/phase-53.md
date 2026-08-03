# Phase 53 — Wire the `telemetry` service into go.work, `bin/start`, and `nginx/run`

*Realizes design Decision 15 (suite wiring). Cross-workspace dependency, stated
per the rollout order: this phase runs only after the `telemetry` service's own
greenfield build loop has produced the `telemetry/` module (its `go.mod`,
binary entrypoint, `etc/nginx.conf`, `etc/manifest.env`) — a `go.work` entry
naming a nonexistent module breaks every workspace build. It does not depend on
any pending phase in this plan.*

The three suite-owned seams gain the fifteenth service, exactly as D15 states:
`go.work` gains `./telemetry`; `bin/start` builds/launches it with its peers
(port resolved via the registry / its manifest, logs to `tmp/telemetry.log`);
`nginx/run` copies its fragment so the dev front door routes
`/srv/telemetry/`. No port literal is introduced anywhere.

**Done when** (deterministic):

- `grep -c '\./telemetry' go.work` = 1.
- `grep -c 'telemetry' bin/start` ≥ 1 and `grep -c 'telemetry' nginx/run` ≥ 1.
- `grep -rn '3008' bin/start nginx/run go.work` returns no matches (the port is
  resolved, never restated).
- The suite is green per design Conventions (`go test ./...` from the repo
  root exits 0, now covering the `telemetry` module).

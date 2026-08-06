# Phase 54 — `bintest`: prove the manifest-reader scripts under the green gate

*Realizes design Decision 16 (manifest readers proven under the gate).*

End state: a new Go test module **`bintest/`** exists at the repo root (module
`bintest`, matching the suite's Go version) and the root `go.work` carries a
`use ./bintest` entry, so `go test ./...` from the repo root runs it. The
module contains tests — no non-test production code beyond what locating the
repo root requires — that exec the real repo-root scripts:

- `bin/registry`, pointed via `REGISTRY_ROOT` at fixture roots built in
  `t.TempDir()` (manifests at `<name>/etc/<version>/manifest.env` behind an
  `etc/current` symlink, plus a decoy service with only the sibling
  `etc/manifest.env`), asserting the resolution and exclusion behavior of
  R-V3XG-PB8R.
- `bin/start`, which gains the two D16 seams — a `--stage-only` flag that
  stages the runtime manifest root and exits `0` before building or launching
  anything, and a `START_RUN_DIR` env override of the run dir
  (`RUN_DIR="${START_RUN_DIR:-$repo/tmp}"`) — exercised for the staged shape
  (R-V6D9-GUQ5) and the `APPKIT_PORT_OFFSET` PORT rewrite (R-V7L5-UMGU).
  Without `--stage-only` and without `START_RUN_DIR` set, `bin/start`'s
  behavior is unchanged.

**Done when:**
- R-V3XG-PB8R — `bin/registry` resolves the `current`-shaped fixture service
  (`port`/`addr`/`mount`/`feed-url`/`list-mcp`) and does not resolve the
  sibling-only decoy — tagged in `bintest/*_test.go`.
- R-V6D9-GUQ5 — `bin/start --stage-only` under `START_RUN_DIR` stages
  `opt/<svc>/etc/<version>/manifest.env` + relative `current` symlink for every
  manifest-bearing service, exits `0`, launches/builds nothing, leaves sources
  unmodified — tagged in `bintest/*_test.go`.
- R-V7L5-UMGU — staged `PORT` offsets by `APPKIT_PORT_OFFSET`; unset offset
  stages byte-identical manifests — tagged in `bintest/*_test.go`.
- From the repo root, `go build ./...` and `go test ./...` both exit `0`.

# cron

The `cron` service for the ikigenba suite: a loopback-only scheduled-event emitter
under `/srv/cron/`. It keeps a programmable crontab of named UTC schedules and,
once per matching wall-clock minute, publishes a `tick` event on the event plane.
An appkit binary and event-plane producer, it serves a bearer-gated MCP surface for
agents and a session-gated landing page for humans, with no token logic (nginx is
the sole trust boundary). Module path: `cron`.

## How changes are made

Changes go through the spec under `project/`, not direct edits — settle the
spec, then let the build loop realize it. The spec itself is direction-gated:
`project/**` is written only inside an operator-invoked move (the `$open-spec`
→ `$grill-me` → `$seal-spec` arc, or the build loop's completion mutations).
In any other session `project/` is read-only reference — a stale or wrong spec
is a finding to report, not a license to edit, and a settled discussion is not
direction: say what should change and wait. Edit code directly only on
explicit operator instruction. See the `$ikispec` skill for the `project/`
spec contracts and `$ralph` for the unattended build workflow.

## Layout

- `cmd/cron`: the composition root (the inline `appkit.Spec`, wiring store, MCP,
  landing, and the tick worker).
- `internal/`: `crontab` (store), `cron` (expression parser/matcher), `tick`
  (the minute-aligned firing worker), `event` (the `tick` contract), `mcp`
  (the domain tools), `db` (embedded migrations).
- `etc/`: `manifest.env` and the nginx location fragment.
- `share/www`: the landing page.
- `project/`: the spec the build loop works from.

## Tests

- Default test gate: `go test ./...` from `cron/`.
- Green also means clean `go build ./...`, `go vet ./...`, `gofmt -l .`, and
  `llm-lint "$PWD"` from `cron/`.
- Layers present: **hermetic** and **composed**; there is no **live** layer. The
  composed layer is the boot smokes in `cmd/cron/main_test.go`; everything else
  is hermetic, and there is no tree-local manual layer.
- Environmental preconditions beyond the Go toolchain: `llm-lint` on `PATH`
  and the provider API key required by its configured default model; if either
  is absent, the green gate fails rather than passing vacuously.
- GOWORK mode: workspace for local development; `GOWORK=off` for the production
  build.

## Versioning

The committed `cron/VERSION` file is the single source of truth (v-prefixed SemVer,
currently `v0.10.1`). Advance it with `bin/bump cron <major|minor|patch>`; ship with
`bin/ship cron`. Git tags are not the version mechanism.

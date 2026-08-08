# webhooks

The `webhooks` service for the ikigenba suite: the inbound ingress for the event
plane. An owner mints a named, secret-protected URL an outside system can call, and
a valid call becomes one durable fact on the event plane. It has two surfaces: an
owner-facing MCP table (create/list/delete/rotate) reached through the front-door
auth chain, and a public `POST /srv/webhooks/in/<name>` ingress that third parties
call directly, guarded only by a per-webhook secret (not behind the dashboard
auth_request). An appkit binary and event-plane producer (emits `received`). Module
path: `webhooks`.

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

- `cmd/webhooks`: the composition root (the `appkit.Spec`, both surfaces + the
  producer hook).
- `internal/`: `webhooks` (domain: secret lifecycle, the public ingress with its
  bearer/github-hmac verification and byte-identical 404, the `received` event),
  `mcp` (the four owner tools), `db` (store + embedded migrations), `ids`, `e2e`.
- `etc/`, `share/www`: manifest, nginx fragment, landing page.
- `project/`: the spec the build loop works from.

## Tests

- The default gate is `go test ./...` from `webhooks/`. Green also means clean
  `go build ./...`, `go vet ./...`, and `gofmt -l .` (which prints nothing).
- This tree has two test layers, both in the default gate: **hermetic** tests use
  temp-directory filesystems, real temp-file SQLite through the real migration
  runner, the real eventplane outbox, deterministic injected clocks, `httptest`,
  committed-file reads, and local subprocesses; **composed** boot smokes build
  the real `cmd/webhooks` binary, run its `serve` verb, and reach `/health` over
  loopback. The `internal/e2e` name is an informal package alias, not a layer.
- There is no live layer and no `//go:build live` test file. There is no
  tree-local manual runbook; bringing the full suite up and checking health
  through nginx on `:8080` is the suite-level manual-layer check, not this
  tree's gate.
- Environmental preconditions beyond the Go toolchain are the `go` binary on
  `PATH`, with the module cache already resolving webhooks' `replace` siblings,
  and a POSIX `bash` with `grep`. Their absence is a hard failure, never a skip.
- Tests, builds, and vet run in workspace mode through the repo-root `go.work`.
  The production build via `bin/ship webhooks` forces `GOWORK=off` and is not
  part of the default gate.

## Versioning

The committed `webhooks/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.6.2`). Advance it with `bin/bump webhooks <major|minor|patch>`;
ship with `bin/ship webhooks`. Git tags are not the version mechanism.

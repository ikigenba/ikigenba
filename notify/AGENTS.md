# notify

A path-routed service in the ikigenba suite, mounted at `/srv/notify/` (module
`notify`). notify is the event-plane **consumer** and the worked example for
bringing up a new one: it subscribes to the `crm` and `prompts` feeds and sends
best-effort ntfy.sh push notifications in reaction to contact creation and
prompt-run outcomes. Its MCP `send` tool is the proactive path, letting a
connected agent push to the owner's device on demand. nginx is the trust
boundary (TLS, dashboard auth, prefix strip, identity headers), so notify runs
no token logic. Push credentials (`NTFY_TOPIC`, `NTFY_API_KEY`) and per-source
consumer config (`NOTIFY_<SRC>_FEED_URL`, `NOTIFY_<SRC>_FROM`) reach the process
only through the environment, per the suite convention.

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

- `cmd/notify/`: composition root: builds the `appkit.Spec` (mount, port,
  consumers, migrations, handlers).
- `internal/push`: ntfy client and event consumer handlers behind `send`.
- `internal/mcp`: notify's tool definitions over the `appkit/mcp` transport.
- `internal/db`: embedded migration set (`FS`) and feed-offset guard test.
- `share/www`: landing page and static assets served via `Spec.WWW`.
- `project/`: the spec (product/design/plan) the build loop works from.

## Tests

notify adopts the testing-language contract in `root project/design/D23.md`.

- Default gate: `cd notify && go test ./...`, as part of the green bar (`go build
  ./...`, `go vet ./...`, silent `gofmt -l .`, and `go test ./...`).
- Layers present: **hermetic** and **composed**. There is **no live layer** and
  **no manual layer**. Hermetic tests are in-process and loopback-only, including
  the mock ntfy server on `127.0.0.1`, MCP and database tests, composition-root
  web and event-chain tests, telemetry wiring tests, and committed shipped-file
  guards. The composed layer is the offline boot smoke that builds and runs the
  notify binary against a temporary `/opt/notify/`-shaped tree and checks its
  loopback `/health` endpoint.
- Environmental preconditions beyond the Go toolchain: **none**. Tests need no
  real ntfy credential or running suite and do not read or change behavior based
  on ambient `NTFY_TOPIC` or `NTFY_API_KEY`; push configuration is injected.
- GOWORK mode: **workspace**. The gate resolves `appkit`, `eventplane`, and
  `registry` through the repo-root `go.work` and committed replace-siblings;
  notify has no separate standalone build check.

## Versioning

The committed `notify/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.16.1`). Advance it with `bin/bump notify <major|minor|patch>`;
ship with `bin/ship notify`. Git tags are not the version mechanism.

# prompts

The `prompts` service for the ikigenba suite: a loopback-only service under
`/srv/prompts/` that runs sandboxed Claude agent sessions on the owner's behalf and
exposes them as MCP. The owner defines a prompt (reusable user_prompt + provider
config, optionally event-triggered) and starts runs; each run drives an agentkit
agent loop in its own sandbox. An appkit binary and event-plane producer and
consumer (self-chaining: a run can be fired by another prompt's outcome). Built on
`appkit` plus the tagged `github.com/ikigenba/agentkit` module; no token logic
(nginx is the sole trust boundary). Module path: `prompts`.

## How changes are made

Changes go through the spec under `project/`, not direct edits: settle the spec,
then let the build loop realize it. Edit code directly only on explicit operator
instruction. See the `$ikispec` skill for the `project/` spec contracts and
`$ralph` for the unattended build workflow.

## Layout

- `cmd/prompts`: the composition root (the `appkit.Spec`, the domain wiring).
- `internal/`: `prompt` (domain), `runner` (async run lifecycle over agentkit),
  `sandbox` (per-run workspaces), `tools` (the in-sandbox toolset), `suite` +
  `mcpclient` (on-demand discovery of peer MCP services), `consume` (the consumer
  fan-out), `mcp`, `db` (embedded migrations), `ids`.
- `etc/`, `share/www`, `bin/` (start/stop/secrets/teardown).
- `project/`: the spec the build loop works from.

## Tests

- `go test ./...` from `prompts/`.
- The prod build forces `GOWORK=off` (agentkit via its tagged module,
  appkit/eventplane/registry via committed `replace`); import guards assert the
  service never depends on a retired local agentkit fork.

## Versioning

The committed `prompts/VERSION` file is the single source of truth (v-prefixed
SemVer, currently `v0.18.1`). Advance it with `bin/bump prompts <major|minor|patch>`;
ship with `bin/ship prompts`. Git tags are not the version mechanism.

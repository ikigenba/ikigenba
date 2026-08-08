# Phase 25 — Align the agentkit pin with the suite-wide version

*Realizes design — structural (no ids): the Conventions' suite-wide agentkit
pin, conforming to `root project/design/D22.md`.*

`repos/go.mod` pins `github.com/ikigenba/agentkit` eleven minor versions
behind the pin every other consumer (`prompts`, `telemetry`) states — and the
`go.work` replace that masked this in dev builds is gone, so the divergence is
now live: dev and shipped builds compile different agentkit code. The require
is raised to the suite-wide pin (`v0.17.0`, the version the other consumers
state at the time this phase was written — if they have advanced, match them,
not this number), and any compile or test breakage from the API drift across
those releases is absorbed in `internal/runner` / `internal/tools` as part of
this phase, not deferred.

**Done when:**

- `go mod edit -json` in `repos/` reports the same
  `github.com/ikigenba/agentkit` require version as `go mod edit -json` in
  `prompts/` and in `telemetry/` (three-way agreement).
- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` in `repos/`
  exit 0.

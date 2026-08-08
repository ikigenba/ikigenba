# Phase 12 — Plain-literal `go.mod`: remove the escaped eventplane module path

*Realizes design Decision — structural (no ids): the Conventions'
plain-literal and source-level no-eventplane-import facts, conforming to
`root project/design/D22.md`.*

`telemetry/go.mod` currently writes its eventplane `require` and `replace`
as a Go-quoted string carrying a hex escape (`"event\x70lane"`) — byte-
identical to `eventplane` for the toolchain, invisible to textual checks. The
escaped spellings are replaced with the plain literal `eventplane` in both
directives (`go mod edit` normalizes this); nothing else about the module
graph changes, since the escaped and plain forms name the same module. No
`.go` source changes: telemetry's own source imports no eventplane package
today, and this phase does not add one.

**Done when:**

- Run from the telemetry tree root: `grep -n 'x70' go.mod` returns empty
  (exit 1), and `grep -c 'eventplane' go.mod` prints `2` (the plain-literal
  require and replace lines).
- `grep -rn 'eventplane' --include='*.go' --exclude-dir=project .` from the
  telemetry tree root returns empty (exit 1).
- `GOWORK=off go build ./...` and `GOWORK=off go test ./...` exit 0.

# dory

A CLI that runs one pass of a supervisor/worker agent tree over
`github.com/ikigenba/ikigenba/agentkit`: the prompt on stdin, a tree of
memoryless agents that search a per-session SQLite store, the six `toolkit`
tools for workers, an address-prefixed trace on stdout, and the root's report
plus a cost line at the end. Module path `github.com/ikigenba/ikigenba/dory`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (`cmd/`, `internal/`, `go.mod` are absent until it
does). See the `spec` and `build-spec` skills. Everything below is what the
build run computes the gap and runs the gates against; it is human-authored and
read-only to the run.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- Network access to the Go module proxy for `agentkit` and `toolkit`, both
  published from this monorepo under `agentkit/v*` and `toolkit/v*` tags, and
  for `github.com/google/uuid` and `modernc.org/sqlite`.

## Dependencies

Every direct dependency is approved by a human, and the approval is recorded
in the design: D1 names the exact set of direct requirements, and a gate test
compares `go.mod` against it. The run never adds a module; a phase that
appears to need one files an issue for a human to adjudicate. Adopting a new
release of an approved dependency is a dependency edit, not a design change,
and the run makes it whenever a phase's requirements only compile against
the newer release.

## Test files

The sub-project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** dory neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

**No live provider in the gates.** agentkit sends requests through Go's
default HTTP client, so every test that needs a provider stands up an
`httptest` server and points a role at it with `-c <role>.base_url=` (design
D1, D3). No gate needs a real API key, a real token file, or the network
beyond loopback; a test that reads `~/.dory` or a real `*_API_KEY` variable is
a bug. Every test that needs a session store creates it under a temporary
directory.

## Gates

Run from this directory (`dory/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `golangci-lint run`

A per-finding `//nolint` comment counts as a disabled linter. Never add one
to make a gate pass.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Releasing

Release machinery — the version bump, tags, and (once added) `.goreleaser.yaml`
and `.github/workflows/release-dory.yml` — is hand-maintained infrastructure
outside the spec system: the build run never reads, edits, or tests it.

No release has been cut yet. The first one adds the GoReleaser config and
release workflow, following agent-repl. The version is source-carried in
`internal/cli/cli.go` (D1) and is edited directly to match the tag
`dory/vX.Y.Z`.

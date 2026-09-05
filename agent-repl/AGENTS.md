# agent-repl

An interactive REPL over `github.com/ikigenba/ikigenba/agentkit`: one
conversation per session, the six `toolkit` file tools registered, every
choice a `-c key=value` string, and a help screen generated from agentkit's
catalog. Module path `github.com/ikigenba/ikigenba/agent-repl`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build loop writes the code (`cmd/`, `internal/`, `go.mod` are absent until it
does). See the `spec` skill and `docs/spec-system.md` at the repo root.
Everything below is declared for the loop's verify role, which reads this file
directly.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- `llm-lint` on PATH, with its provider API key present in the environment
- `idgen` on PATH — for spec authoring only; the loop never mints an id.
- Network access to the Go module proxy for `agentkit` (`v0.3.0`+) and
  `toolkit`, both published from this monorepo under `agentkit/v*` and
  `toolkit/v*` tags.

## Test files

The project's tests are all `*_test.go` files under `cmd/` and `internal/`.
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' cmd internal | sort -u
```

**No id-shaped-literal hazard.** agent-repl neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

**No live provider in the gates.** agentkit sends requests through Go's
default HTTP client, so every test that needs a provider stands up an
`httptest` server and points the session at it with `-c base_url=` (design
D1, D3). No gate needs a real API key, a real token file, or the network
beyond loopback; a test that reads `~/.agent-repl` or a real `*_API_KEY`
variable is a bug.

## Gates

Run from this directory (`agent-repl/`), in order; every command must exit 0.
No skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `golangci-lint run`
5. `llm-lint cmd internal`

llm-lint loads this project's own rules from `lint-rules/` (wired via
`.llm-lint.json`, found by ancestor walk — a sibling project's config is not
on that path, so this directory carries its own). Rules are promoted
individually: a promotion flips the rule file to `severity: error` and adds
its id to the `enable` allowlist in `.llm-lint.json`. Un-promoted rules stay
disabled — they make no LLM calls and print nothing — so every finding the
gate reports fails it. The rule set and its allowlist are a verbatim copy of
idgen's.

A per-finding `llm-lint:ignore` directive (and likewise a `//nolint` comment
for golangci-lint) counts as a disabled linter. Never add one to make a gate
pass.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY

Co-Authored-By: Claude <noreply@anthropic.com>
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Releasing (infrastructure — outside the spec loop)

Releases are cut from this monorepo by tag. The release machinery is
hand-maintained infrastructure, not spec-governed code:

- Tag `agent-repl/vMAJOR.MINOR.PATCH` on `main`; the latest is
  `git tag --list 'agent-repl/v*' --sort=-v:refname | head -1`.
- Pushing the tag triggers `.github/workflows/release-agent-repl.yml` (repo
  root), which verifies the tag's version equals the in-source version string
  in `internal/cli/version.go` (a mismatched tag fails the release), then runs
  GoReleaser from this directory using `.goreleaser.yaml` — linux/darwin ×
  amd64/arm64, tar.gz archives, checksums, a GitHub release on the tag.
- The version string is source-carried (see
  `specs/design/D4-help-and-version.md`), never ldflags-injected. Its *value*
  is release data, not spec-governed: edit `internal/cli/version.go` directly
  to the new `vMAJOR.MINOR.PATCH` (the spec fixes only its shape), keep it
  valid against the gates, merge, then tag to match. The first release is
  `v0.8.0`, the successor of the previously installed `agentrepl` binary.

# agentkit

A Go library that talks to LLM chat/completions APIs and runs an agentic tool
loop. It decomposes the old "provider" axis into three orthogonal pieces — a
built-in wire codec, an opaque `Endpoint` (base URL plus auth), and a free-form `Model` string —
so a new vendor or a day-one model needs no library release. Module path
`github.com/ikigenba/ikigenba/agentkit`.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the code (the root package, `retry/`, and `go.mod`'s
dependency graph fill in as it does). See the `spec` and
`build-spec` skills. Everything below
is what the build run computes the gap and runs the gates against; it is
human-authored and read-only to the run.

## Catalog data ground

`specs/_data/catalog_table.go` is the user-authorized authoritative project
ground for versioned catalog records. The design contract projects this data
without repeating release values in requirements, tests, or fixtures; the
build run installs it as the root package's `catalog_table.go` under D21's
byte-identity requirement.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- GNU Make 4.4.1
- `secret-tool` from Debian `libsecret-tools` 0.21.7-1
- For the conditional live gate (below): `GEMINI_API_KEY`, `XAI_API_KEY`, and
  `OPENROUTER_API_KEY` in the environment; `ANTHROPIC_API_KEY` and
  `OPENAI_API_KEY` resolved by the `live` Makefile target itself from the
  keyring via `secret-tool`, so their absence from the shell environment is
  not a missing credential; and OAuth token files at
  `~/.agentkit/openai-auth.json` and `~/.agentkit/x-ai-auth.json` written by
  the `oauth` CLI. Judge a credential present or absent by running `make live`
  as written, never by inspecting the environment: a missing credential fails
  the subtest that needs it with a message naming it.

## Test files

The sub-project's spec tests are all `*_test.go` files under this module,
**excluding** the live tests named `*_live_test.go` (which are guarded by a
`//go:build live` tag and carry no requirement ids — they prove the vendor facts
the designs record, and an offline architecture test carries the id that pins
each live file's existence and shape). This is the file set the canonical gap
greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude='*_live_test.go' . | sort -u
```

Live tests are gated by `//go:build live` only. They never skip: a missing
credential fails the test, because a missing credential is a missing proof. They
are excluded above so they never contribute an id to the gap. Every requirement
id is proved by an offline `*_test.go`; a golden SSE fixture lives under
`testdata/` and carries no id-shaped literal that is not a genuine
requirement-id tag.

## Gates

Run from this directory (`agentkit/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run`
   — the cache lives in this worktree's git directory, so worktrees never
   share it (a shared `~/.cache/golangci-lint` keeps other worktrees' paths and
   stops applying `//nolint` and `.golangci.yml` suppressions; `make lint` runs
   this form)
5. `make live` — **conditional**: run only when the phase's diff (the working
   tree against the last phase commit) adds or modifies a `*_live_test.go`
   file; otherwise it is not run and not counted. It drives one
   lexicographically selected catalog offering for every offering-id/auth-mode
   pair against the real vendor host (D23), so the phase that creates or
   extends a live test must pass it live, and later phases do not pay for it.
   When it applies and a credential from the toolchain list is absent, that is
   a missing tool: file an issue, do not pass or skip.

A per-finding `//nolint` comment counts as a disabled linter. Never add one
to make a gate pass. A finding that cannot be fixed below the contract
seam without changing an exported name, signature, or observable behavior, or
that is wrong, is filed as an issue under `specs/issues/` so a human can
adjudicate — restructure the code or amend the design.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Releasing

Releasing is hand-maintained infrastructure outside the spec system: the build
run never tags or publishes. agentkit is a library consumed by module path;
there is no binary to ship. The spec fixes its shape, never its version number.

1. Tag a green `main` `agentkit/vX.Y.Z` and push the tag. The latest is
   `git tag --list 'agentkit/v*' --sort=-v:refname | head -1`.
2. A consumer pins it with an ordinary `require
   github.com/ikigenba/ikigenba/agentkit vX.Y.Z` in its own `go.mod`.

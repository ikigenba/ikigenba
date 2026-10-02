# appkit

The shared Go library of the Ikigenba services: any functionality that more
than one service needs lives here, one concern per package, and no package is
privileged; the module root holds no exported name. Module path
`github.com/ikigenba/ikigenba/appkit`. The packages:

- `page` — the page chrome every app shows a signed-in user: the banner,
  launcher, and footer templates, and the shared stylesheet, fonts, and
  launcher script served from one fixed path prefix.
- `services` — the one reader of the host's services file
  (`IKIGENBA_SERVICES`, owned by opsctl).
- `identity` — the caller nginx authenticated (`X-User-Id`, `X-User-Email`,
  `X-Request-Id`): the middleware that requires it, and forwarding it on a
  call to a sibling service.
- `mcp` — the Model Context Protocol: the server a service mounts at `/mcp`
  with its tools, and the client the gateway and service tests use.

appkit knows nothing about authentication: nginx and auth establish who the
caller is, and appkit only carries it.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source and tests. See the `spec` and `build-spec`
skills. Everything below is what the build run computes the gap and runs the
gates against; it is human-authored and read-only to the run.

## Assets

`page/assets/` is human-authored and read-only to the build run. It sits
inside the `page` package directory because Go's `embed` reaches only files
at or below the embedding package. It holds the markup templates
(`banner.html`), the launcher script (`launcher.js`), and copies of the repository's `design/` files: the stylesheet, fonts, and their
licences. The interactive agent that changes `design/` refreshes the copies in
the same session. The copied stylesheet replaces the Google Fonts import with
`@font-face` rules for the files beside it, and its header names the `design/`
commit it came from, so that commit lands first. Package `page`
embeds `page/assets/` and never writes markup of its own.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

The module depends on the Go standard library only. Adding any other
dependency requires human approval.

## Test files

The sub-project's tests are all `*_test.go` files under this module. This is the
file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

Test files MUST NOT contain any id-shaped literal that is not a genuine
requirement-id tag.

## Test discipline

- Offline: no network beyond loopback. MCP and identity tests talk HTTP to
  an `httptest` server on loopback, or to a unix socket created in the
  test's own temporary directory; never to `/run/ikigenba` or a real
  service.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something.
- No fixed ports: a test uses `httptest` or binds `127.0.0.1:0`.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state, and never `/var/lib/ikigenba`.
  A services file a test needs is written there and named through
  `IKIGENBA_SERVICES` with `testing.T.Setenv`.
- Logs are captured: anything appkit writes as a diagnostic goes to an
  `io.Writer` the test supplies, and the test asserts on it; a test never
  reads the process's real stderr.
- Hooks, not layout: tests assert on the hooks design names and on visible
  text, never on styles, nor on markup structure beyond the containment and
  order that design names as hooks.

There are no live tests.

## Gates

Run from this directory (`appkit/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `go test -race ./...`
4. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`
   — the cache lives in this worktree's git directory, so worktrees never
   share it; `make lint` runs this form

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

Release machinery — the tags — is hand-maintained infrastructure outside the
spec system: the build run never reads, edits, or tests it.

appkit is a library consumed by module path; there is no binary to ship. The
spec fixes its shape, never its version number.

1. Tag a green `main` `appkit/vX.Y.Z` and push the tag.
2. A consumer pins it with an ordinary `require
   github.com/ikigenba/ikigenba/appkit vX.Y.Z` in its own `go.mod`.

The latest release is
`git tag --list 'appkit/v*' --sort=-v:refname | head -1`.

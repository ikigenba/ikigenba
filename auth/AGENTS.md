# auth

An app of the Ikigenba platform: one Go binary that serves the auth service at
`127.0.0.1:$PORT`, behind the host's nginx. On a host it runs as
`/opt/auth/bin/auth` with `/opt/auth` as its working directory and its
environment from `/opt/auth/etc/env`; a developer runs the same binary from the
checkout. The module path is `github.com/ikigenba/ikigenba/auth`. Its package
layout, import direction, version and manifest declarations, and run seam are
design D01 (`specs/design/D01-layout-and-run-seam.md`); this file does not
restate them.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the source, the tests, `go.mod`, and `etc/manifest.toml`. See
the `spec` and `build-spec` skills. Everything below is the ground the run
computes the gap and runs the gates against; it is human-authored and read-only
to the run.

## Deploy

auth is an app, not a self-installing CLI: it is built into a release tarball
and pushed to a space's host by `devctl`, which drives `opsctl install` there
(`docs/architecture.md`).

1. Set the release version D01 declares (`internal/version/version.go`) to
   `vX.Y.Z`. It is a source literal the binary reports verbatim — no linker
   injection — and `devctl build` refuses a tarball whose `--version` disagrees
   with the tag or whose `manifest` disagrees with the committed
   `etc/manifest.toml`.
2. Commit that on `main` and push `main`.
3. Tag that commit `auth/vX.Y.Z` and push the tag.
4. `devctl build auth` at that tag writes `auth/dist/auth-vX.Y.Z.tar.xz`,
   holding `bin/auth` and `etc/`, with no version recorded anywhere inside.
5. `devctl deploy <space> auth/dist/auth-vX.Y.Z.tar.xz` uploads the tarball to
   the space's `deploy/` prefix and runs `opsctl install` over ssh; the host
   fetches it, writes `etc/env`, replaces the release, publishes
   `ikigenba-auth.service`, regenerates the host's nginx and litestream
   configuration, and restarts the service.

`auth --version` (and `space status`) then report `vX.Y.Z`; the binary is the
only place the version is recorded.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

The toolchain names the tools the gates need; it pins no Go library. Library
release selection is `go.mod`'s job, and the build run writes it.

## Dependencies

Unlike a stdlib-only app, auth requires external modules. They are named here by
import path only; the release each resolves to lives in `go.mod`, which the
build run writes, never in this file:

- `modernc.org/sqlite` — the SQLite driver `internal/store` opens. It is pure
  Go, so the release build stays `CGO_ENABLED=0` (gate 3); a cgo SQLite driver
  would break that gate.
- `golang.org/x/oauth2` — the OAuth2 exchange in `internal/google`.
- `github.com/coreos/go-oidc/v3` — OIDC discovery and ID-token verification in
  `internal/google`.

The run may add exactly these three and their transitive dependencies to
`go.mod`, and nothing else. A phase that appears to need a module not reachable
from these files files an issue for a human to adjudicate, rather than pulling
in a new direct dependency on its own.

## Test files

The project's tests are all `*_test.go` files in the module: `cmd/auth` and
everything under `internal/`. This is the file set the canonical gap greps for
requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`.` covers `cmd/` and `internal/`; `specs/` holds no `*_test.go`, so the design
documents never enter the test side of the grep.

**No id-shaped-literal hazard.** A requirement tag has the shape
`R-XXXX-XXXX`: an `R-` prefix and two hyphen-separated groups. auth's opaque
values carry no such shape. Its ids (user, session, login-state, token) are
26-char Crockford base32 (`internal/idcodec`, alphabet excludes I L O U, no
hyphen); its token secrets are `ikp_` + 52 Crockford chars; its stored secret
hashes are 64 lowercase-hex chars. None contains the hyphenated `R-....-....`
pattern the grep matches, so no auth literal — id, secret, or hash — can be
mistaken for a requirement tag.

## Test discipline

**Offline, deterministic, no fixed ports, no sleeping.** The gates run offline
as an ordinary user. Every input reaches the code through the run seam
(`cli.Process`, design D01) — `Args`, `Getenv`, `Stdout`, `Stderr`, `Now`,
`Rand`, `OIDCIssuer`, `DBSource` — and tests supply each one; a test never reads
the real environment, clock, or randomness, or accesses filesystem or network
state outside the isolated fixtures described below. A test whose
result depends on the developer's machine, wall-clock, environment, or a port
already in use is a bug.

- **Google is faked, never called.** auth talks to Google's OIDC issuer only
  through `Process.OIDCIssuer`. Tests stand up a loopback OIDC issuer (its
  discovery document, keys, and token endpoint on `127.0.0.1:0`) and inject its
  URL through `Process.OIDCIssuer`, so no test reaches `accounts.google.com`.
- **The database is an isolated source.** Tests pass a database path or
  file-backed sqlite DSN confined to a test-owned temporary directory, or an
  in-memory sqlite DSN, through
  `Process.DBSource`. They may create and inspect filesystem fixtures within
  that temporary tree, including an absent database parent or a regular file
  obstructing that parent; nothing touches `/opt/auth` or a shared file.
- **The clock is injected.** `Process.Now` supplies every timestamp. Time-based
  behaviour — session idle (15m) and cap (18h), the 30d token login window,
  token expiry (30d/90d/365d) — is tested by advancing the value `Now` returns,
  never by sleeping. A test that sleeps to age a session or a token is a bug.
- **Randomness is injected.** `Process.Rand` supplies the bytes `idcodec`
  mints ids and secrets from, so id and secret values under test are
  reproducible.

Tests bind only loopback addresses, never a fixed port such as 3000. A
`server`-level test that needs a live listener binds `127.0.0.1:0` and lets the
kernel choose. The `cli.Run` happy path cannot do that directly — `PORT=0` is a
usage error and the bound port must equal `PORT` (D02/D03) — so it picks a free
loopback port by binding `127.0.0.1:0`, reading the port, closing that
listener, and passing the number as `PORT`, then learns the server is up from
what the server reports, never from a fixed sleep.

**One exec'ing test, and only one.** Tests under `internal/` never start a real
process; the run seam exists so they need not. The wiring in `cmd/auth` (D01's
`main` requirement) can be proved no other way, so exactly one kind of test that
execs the binary is admissible, and it lives in `cmd/auth`. It builds the binary
into a temporary directory, runs it with `--version`, runs it with `manifest`,
runs it with a bogus command, and runs a serve case with a runtime-chosen
loopback `PORT` (picked by the probe-and-release technique above) that it stops
with `SIGTERM`. It then runs the serve case a second time, with a freshly picked
port, and stops it with `SIGINT`, asserting the same exit 0 and the same
silence on both streams, because `main` promises both signals return 0. It
learns readiness by connecting to that port, retrying connects until one
succeeds, never by a fixed sleep, and it asserts the child's streams and exit
codes. The child runs in a test-owned temporary working directory. Its
environment is one the test composes, never the developer's,
and it runs offline like everything else — its serve case needs no Google, since
readiness is a successful connect, not a completed login. Any other test that
builds, execs, waits on, or signals a process is a bug.

## Gates

Run from this directory (`auth/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix below the contract seam, or believes is wrong, is
filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/auth` —
   the release build: proves `cmd/auth` builds static for the host without cgo,
   which is why the SQLite driver must be pure Go
4. `go test -race ./...`
5. `golangci-lint run`

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable by
id.

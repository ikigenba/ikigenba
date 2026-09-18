# dummy

An app of the Ikigenba platform: one Go binary that serves one page at
`127.0.0.1:$PORT`, behind the host's nginx. On a host it runs as
`/opt/dummy/bin/dummy` with `/opt/dummy` as its working directory; a developer
runs the same binary from the checkout. The module path is
`github.com/ikigenba/ikigenba/dummy`. Its package layout, import direction,
version and manifest declarations, and run seam are design D01
(`specs/design/D01-layout-and-run-seam.md`); this file does not restate them.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the source, the tests, and `etc/manifest.toml`. See the
`spec` and `build-spec` skills and `docs/spec-system.md` at the repo root.
Everything below is the ground the run computes the gap and runs the gates
against; it is human-authored and read-only to the run.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)

## Dependencies

D01 allows the standard library only, so `go.mod` carries no `require`
directive. The run never adds one; a phase that appears to need a module files
an issue for a human to adjudicate.

## Test files

The project's tests are all `*_test.go` files in the module: `cmd/dummy` and
everything under `internal/`. This is the file set the canonical gap greps
for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`.` covers `cmd/` and `internal/`; `specs/` holds no `*_test.go`, so the
design documents never enter the test side of the grep.

**No id-shaped-literal hazard.** dummy neither mints nor emits
`PREFIX-XXXX-XXXX` values, so no test literal can be mistaken for a
requirement tag.

**No fixed ports, no real environment, no sleeping in the gates.** Tests bind
only loopback addresses, never a fixed port such as 3000. A `server.Serve`-level
test takes a listener, so it binds `127.0.0.1:0` and lets the kernel choose. A
`cli.Run`-level test of the happy path cannot: `PORT=0` is a usage error (D02)
and the bound port must equal `PORT` (D03). It picks a free loopback port by
binding `127.0.0.1:0`, reading the port, closing that listener, and passing
the number as `PORT`, then confirms the address `Listening` reports carries
that port. Failure paths (bind refused, `Accept` failing) inject
`Process.Listen` with a failing factory or a failing listener rather than
occupying a real port. Tests never read the real environment: arguments,
environment lookup, and the output streams come in through the run seam
(`cli.Process`, design D01), and tests inject buffers and a map. A test never
sleeps to wait for the server; for in-process tests `Listening` reports the
bound address, and that is how a test learns the server is up. A test whose
result depends on the developer's machine, environment, or a port already in
use is a bug. The gates run offline as an ordinary user.

**One exec'ing test, and only one.** Tests under `internal/` never start a
real process; the run seam exists so they need not. The wiring in `cmd/dummy`
(D01's `main` requirement) can be proved no other way, so exactly one kind of
test that execs the binary is admissible, and it lives in `cmd/dummy`. It
builds the binary into a temporary directory, runs it with `--version`, runs
it with `bogus`, and runs it with a runtime-chosen loopback `PORT` (picked by
the probe-and-release technique above) and then sends it `SIGTERM`. It then
runs the serve case a second time, with a freshly picked port, and stops it
with `SIGINT`, asserting the same exit 0 and the same silence on both streams,
because `main` promises both signals. It learns readiness by connecting to
that port, retrying connects until one succeeds, never by a fixed sleep, and
it asserts the child's streams and exit codes. The child's environment is one the test composes, never the developer's, and it
runs offline like everything else. Any other test that builds, execs, waits on,
or signals a process is a bug.

## Gates

Run from this directory (`dummy/`), in order; every command must exit 0. No
skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (`go fmt`
   itself always exits 0, so the check form is the gate; fix with `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/dummy` —
   the release build: proves `cmd/dummy` builds static for the host without
   cgo, the way `devctl build` builds it
4. `go test -race ./...`
5. `golangci-lint run`

Gate 5's `formatters` (`gofmt`, `goimports`) repeat gate 1 harmlessly. It also
flags an undocumented `package main` (`revive`) and an `http.Server` without
`ReadHeaderTimeout` (`gosec` G112); both are fixed in code, never suppressed.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

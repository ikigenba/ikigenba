> [!WARNING]
> This is unsupported AI slop.

# devctl

`devctl` is the developer's CLI for the Ikigenba platform. Ikigenba is a PaaS
for running internal apps; each deployment of it is complete on one Linux
host. `devctl` is a Go binary built and run on the developer's own machine,
as an ordinary user, under the developer's own AWS identity. It creates and
manages the platform's deployments and the hosts they run on, talking to AWS
APIs and, over ssh, to those hosts. It never runs as root and holds no
host-side secrets. Its sibling, `opsctl`, is the host-side counterpart that
runs as root on a host.

Built spec-first: `specs/design/` is the contract, `AGENTS.md` the gates. No
code or tests are written by hand; the build run derives them from the
design. See `docs/spec-system.md` at the repo root.

## Building it

Requires **Go 1.26+**. From this directory:

```sh
make build     # bin/devctl
make install   # go install ./cmd/devctl
make test      # go test -race ./...
```

The full verification gates are declared in [`AGENTS.md`](AGENTS.md).

## The spec

- `specs/design/` — design documents; each requirement carries a permanent
  `R-XXXX-XXXX` id, and every test tags the id it proves, so coverage is a
  `grep`.
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions
  the build run verifies against.

To change devctl, change the spec — `draft-spec`, then `check-spec`, then
`build-spec` — rather than editing the code directly.

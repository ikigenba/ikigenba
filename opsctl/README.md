> [!WARNING]
> This is unsupported AI slop.

# opsctl

`opsctl` is the operator CLI for the Ikigenba platform. Ikigenba is a PaaS
for running internal apps; its core platform services all run on a single
Linux host. `opsctl` is installed to `/usr/local/bin` on that host and is
used, by humans and by agents, to bootstrap the platform and to help manage
it. Most configuration and setup work is done over ssh by running `opsctl`
on the host.

Built spec-first: `specs/design/` is the contract, `AGENTS.md` the gates. No
code or tests are written by hand; the build run derives them from the
design. See `docs/spec-system.md` at the repo root.

## Building it

Requires **Go 1.26+**. From this directory:

```sh
make build     # bin/opsctl
make install   # go install ./cmd/opsctl
make test      # go test -race ./...
```

The full verification gates are declared in [`AGENTS.md`](AGENTS.md).

## The spec

- `specs/design/` — design documents; each requirement carries a permanent
  `R-XXXX-XXXX` id, and every test tags the id it proves, so coverage is a
  `grep`.
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions
  the build run verifies against.

To change opsctl, change the spec — `draft-spec`, then `check-spec`, then
`build-spec` — rather than editing the code directly.

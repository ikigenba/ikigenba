> [!WARNING]
> This is unsupported AI slop.

# dummy

`dummy` is an app of the Ikigenba platform. Ikigenba is a PaaS for running
internal apps; each deployment of it is complete on one Linux host. `dummy`
exercises the whole path with a small server-rendered control panel: one Go
binary serving at `127.0.0.1:$PORT` behind the host's nginx. The panel is a
chrome-framed page listing widgets, an HTML table fragment the page re-fetches
with a conditional GET, and a form that creates a widget with per-field
validation. Widgets live in memory and are lost when the process exits. On a
host it runs as `/opt/dummy/bin/dummy`, with its environment read from
`/opt/dummy/etc/env`; a developer runs the same binary from the checkout. Its
version is a `var` in the source, so a developer's build and a deployed binary
report the same string.

Built spec-first: `specs/design/` is the contract, `AGENTS.md` the gates. No
code or tests are written by hand; the build run derives them from the
design. See the `spec` skill (`.agents/skills/spec/SKILL.md` at the repo root).

## Building it

Requires **Go 1.26+**; `make test` also needs a C compiler, since `-race` needs
cgo. From this directory:

```sh
make build     # bin/dummy (CGO_ENABLED=0 go build -o bin/dummy ./cmd/dummy)
make install   # go install ./cmd/dummy
make test      # go test -race ./...
```

The full verification gates are declared in [`AGENTS.md`](AGENTS.md).

The release file, `dist/dummy-v<semver>.tar.xz`, is not a `make` target: it is
written by `devctl build dummy`, which builds `cmd/dummy` itself for
`linux/amd64` and packs it with `etc/manifest.toml`. The checkout's
`etc/manifest.toml` must equal the output of `dummy manifest` byte for byte;
`devctl build` refuses an app where they differ.

## The spec

- `specs/stories/` — what dummy does, as user stories.
- `specs/design/` — design documents; each requirement carries a permanent
  `R-XXXX-XXXX` id, and every test tags the id it proves, so coverage is a
  `grep`.
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions
  the build run verifies against.

To change dummy, change the spec — `draft-stories`, `draft-design`,
`check-spec`, then `build-spec` — rather than editing the code directly.

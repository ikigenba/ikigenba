# idgen

`idgen` is built **spec-first**: the design documents under `specs/design/`
define the contract, and an automated build loop wrote the code, tested it,
and proved it against the spec. Unlike a hand-written tool, every behavior
here traces to a requirement id, and every requirement id to a test.

So this sub-project is two things at once:

1. **A small, useful CLI:** clone the monorepo, run `make build`, get a
   working binary.
2. **A demonstration of spec-first construction:** a project fully specified
   up front, then generated from that spec. See
   [how the spec system works](../docs/spec-system.md).

## What idgen is (the end product)

Some projects, including the ikigenba projects, embed stable, traceable IDs in
their requirements, comments, and test names. Those IDs only need to be unique
within the project, not globally, so they can be much shorter than a UUID.

`idgen` is a small CLI that mints those IDs in the form `PREFIX-XXXX-XXXX`
(default prefix `R`). Each ID encodes the number of milliseconds from a
**2026 UTC epoch** to the moment it was minted.

The millisecond count isn't shown raw. It runs through a reversible affine
bijection (a modular multiply-and-add) before base-36 encoding, so consecutive
milliseconds land far apart and the IDs look random and scattered. Because the
map is one-to-one, every distinct millisecond yields a distinct ID.

The body uses base-36, digits `0-9` and uppercase `A-Z` only. That keeps an ID
case-insensitive, so it survives being lowercased in a URL, said aloud, or
retyped by hand without a normalization step. It stays identifier- and
URL-safe, so it embeds in comments, test names, and links with no escaping.
And it stays short: eight base-36 characters hold 36⁸, about 2.8 trillion
values, which is roughly 89 years of milliseconds past the epoch, in a
fixed-width body that splits cleanly 4-4.

```sh
$ idgen
R-6Q08-HZPS
```

- `-n N` / `--number N`: mint N IDs, one per line, all distinct.
- `-p PREFIX` / `--prefix PREFIX`: override the default `R` prefix.
- `--decode`: decode IDs (from args or whitespace-separated stdin) to their
  ISO-8601 UTC minting time, to the millisecond.
- `-h` / `--help`, `-V` / `--version`: usage and version.

All times are UTC; IDs are minted only from a millisecond that has already
elapsed.

These are the very same ids that this monorepo's spec system uses to track its
own requirements. The designs mint one per checkable behavior, so **`idgen` is
built using `idgen`**.

## Installing it

Grab a released binary (linux/darwin, amd64/arm64) into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/idgen/install.sh | sh
```

Pin a version or change the destination with env vars:

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/idgen/install.sh | IDGEN_VERSION=v0.1.0 BINDIR=/usr/local/bin sh
```

## Building it

Requires **Go 1.26+**. From this directory:

```sh
make build     # bin/idgen
make install   # go install ./cmd/idgen
make test      # go test -race ./...
```

The full verification gates (build, race tests, `golangci-lint`, `llm-lint`)
are declared in [`AGENTS.md`](AGENTS.md).

## The spec

- `specs/design/` — six design documents; each requirement carries a permanent
  `R-XXXX-XXXX` id, and every test tags the id it proves, so coverage is a
  `grep`.
- `specs/loops/` — the gather → build → verify prompts the build loop runs
  (via `ralph`, or any agent driving the same cycle).
- `AGENTS.md` — the toolchain, test-file set, gates, and commit conventions
  the loop verifies against.

To change idgen, change the spec — `$open-spec`, then `$seal-spec`, then run
the loop — rather than editing the code directly.

## Releases

Releases are cut from the monorepo by pushing a tag `idgen/vMAJOR.MINOR.PATCH`
that matches the in-source version string; a GitHub workflow builds
linux/darwin × amd64/arm64 archives with checksums and publishes them on the
tag. Details in [`AGENTS.md`](AGENTS.md).

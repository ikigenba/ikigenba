> [!WARNING]
> This is unsupported AI slop.

# idgen

`idgen` is a CLI that mints short, unique, traceable ids of the form
`PREFIX-XXXX-XXXX`. The ikigenba sub-projects use them for requirement ids.
Each id encodes the millisecond it was minted, scrambled so consecutive ids
look unrelated, in case-insensitive base-36.

## Installing it

```sh
curl -fsSL https://raw.githubusercontent.com/ikigenba/ikigenba/main/idgen/install.sh | sh
```

This installs the newest stable release (Linux and macOS, amd64 and arm64) to
`~/.local/bin`. Set `IDGEN_VERSION=vX.Y.Z` to pin a version, or `BINDIR` to
change the destination.

## Using it

```sh
idgen --help
```

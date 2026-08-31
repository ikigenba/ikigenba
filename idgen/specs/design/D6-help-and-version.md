# D6-help-and-version

**Usage text.** One usage block, printed to stdout for `--help`/`-h` and to
stderr on a usage error (routing: D4). Its structure is part of the product:
a usage line, a one-line description, and an options list — each block
separated by a blank line, with short/long forms and a description per option
row. The exact text:

```
Usage: idgen [options] [ID ...]

Mint an identifier using the current time by default.

Options:
  -n, --number N       mint N identifiers (default 1)
  -p, --prefix PREFIX  use PREFIX (default "R")
      --decode         decode ID arguments, or whitespace-delimited IDs from stdin
  -h, --help           print this help
  -V, --version        print version
```

**Version.** The version string is a product fact carried in source — a single
`var version` in `internal/cli` — not injected at build time, so dev builds and
released builds report the same string. Its *value* is release data, not a
design fact: an agent (or the release process) may change it freely without
touching this spec. The spec fixes only its **shape** — a `v`-prefixed
`MAJOR.MINOR.PATCH` — and the output form. The release workflow (see
`AGENTS.md`) is what ties a given tag to the string; that check is
infrastructure, outside the spec loop. `--version` (and its short form `-V`)
prints the string bare on its own line.

## REQUIREMENTS

- R-TOOA-ASKR: The usage text MUST be byte-for-byte the block quoted above: usage line, blank line, description, blank line, `Options:` header with the five aligned option rows.
- R-TPW6-OKBG: The usage text MUST mention every option: `-n`, `--number`, `-p`, `--prefix`, `--decode`, `-h`, `--help`, `-V`, `--version`.
- R-4251-2274: `--version` MUST print the `internal/cli` version string alone on a line — exactly that string followed by a single newline, with nothing else on stdout — and exit successfully.
- R-43CX-FTXT: The version string MUST be a `v`-prefixed semantic version of the form `vMAJOR.MINOR.PATCH`, where each of MAJOR, MINOR, and PATCH is a non-negative integer with no leading zeros (matching `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).
- R-TSBZ-G3SU: `-V` MUST behave identically to `--version` (same stdout, same exit code).

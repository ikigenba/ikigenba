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
`var version = "v0.1.0"` in `internal/cli` — not injected at build time. Dev
builds and released builds report the same string, which is testable; the
release workflow (see `AGENTS.md`) refuses a tag that disagrees with it.
`--version` (and its short form `-V`) prints the string bare on its own line.

## REQUIREMENTS

- R-TOOA-ASKR: The usage text MUST be byte-for-byte the block quoted above: usage line, blank line, description, blank line, `Options:` header with the five aligned option rows.
- R-TPW6-OKBG: The usage text MUST mention every option: `-n`, `--number`, `-p`, `--prefix`, `--decode`, `-h`, `--help`, `-V`, `--version`.
- R-TR43-2C25: `--version` MUST print exactly `v0.1.0` followed by a newline to stdout.
- R-TSBZ-G3SU: `-V` MUST behave identically to `--version` (same stdout, same exit code).

# D2-cli-grammar

`agent-repl` is a single command with flags — no subcommands, no positional
arguments. Grammar (also the first line of the usage text, D4):

```
agent-repl [-c key=value ...] [-raw] [-V] [-h]
```

| flag              | meaning                                          | default |
|-------------------|--------------------------------------------------|---------|
| `-c key=value`    | set a config value (repeatable, last wins)       | *(none)*|
| `-raw`            | emit the raw, undecorated message stream (D6)    | `false` |
| `-h`, `--help`    | print the catalog and exit                       |         |
| `-V`, `--version` | print the version and exit                       |         |

Parsing uses the standard library `flag` package, so `-c`, `--c`, `-raw`, and
`--raw` are equivalent spellings; the usage text documents the single-dash
forms for `-c` and `-raw` and both forms for help and version, matching idgen
and oauth.

**Parsing is two operations, split on the syntax/semantics seam**, as in the
siblings:

- `options.ParseFlags(args []string) (Flags, error)` applies the grammar and
  nothing else. It returns the raw flags, the sentinel `ErrHelp` for `-h` and
  `--help`, or an error naming an unknown flag or a malformed `-c` argument. A
  `-c` argument is malformed when it has no `=` or an empty key; its value may
  be empty. `Flags.Config` keeps every `-c` occurrence in command-line order
  as `Pair{Key, Value}` values, duplicates included, so precedence is decided
  once, in `Validate`.
- `Flags.Validate() (Options, error)` is the semantic half (D3): it folds the
  pairs into `Options`, last occurrence winning, separates the interpreted keys
  from the pass-through ones, applies every check that needs no I/O, and
  returns an error naming the offending key.

Neither operation writes to any stream: the flag set's output is discarded and
`Usage()` returns the help text as a string for the caller to place (D4). That
is what makes a usage error report its cause exactly once.

Exit-code taxonomy, as package constants in `cli`, `exitSuccess = 0`,
`exitFailure = 1`, `exitUsage = 2`:

- `0` — the session ended normally (end of input, or an interrupt at the
  prompt), or help or version was printed.
- `1` — the session could not start: a missing credential, an unreadable
  token file, a tool that could not be constructed, or a log file that could
  not be created (D3, D5).
- `2` — usage error: an unknown flag, a malformed `-c` argument, or any
  validation failure from D3. Usage errors report to stderr; help requested
  explicitly goes to stdout.

A failure *during* a turn is never an exit: it is rendered (D6) and the loop
continues to the next prompt.

## REQUIREMENTS

- R-U659-GK9Z: Package `internal/options` MUST export a `Pair` struct whose fields are exactly `Key string` and `Value string`.
- R-U7D5-UC0O: Package `internal/options` MUST export a `Flags` struct whose fields are exactly `Config []Pair`, `Raw bool`, and `Version bool`.
- R-U8L2-83RD: Package `internal/options` MUST export `ParseFlags(args []string) (Flags, error)`.
- R-U9SY-LVI2: Package `internal/options` MUST export the method `Validate() (Options, error)` on `Flags`.
- R-UB0U-ZN8R: Package `internal/options` MUST export a sentinel error `ErrHelp` that `ParseFlags` returns when `-h` or `--help` is supplied.
- R-UC8R-DEZG: `options.ParseFlags` MUST accept `-c`, `-raw`, `-V`, and `--version`, populating `Flags.Config`, `Flags.Raw`, and `Flags.Version` respectively, and with every flag absent MUST return the zero `Flags`.
- R-UDGN-R6Q5: Repeated `-c` occurrences MUST each append a `Pair` to `Flags.Config` in command-line order, including when a key repeats, with `Key` the text before the first `=` and `Value` the text after it.
- R-UEOK-4YGU: `options.ParseFlags` MUST return an error whose text contains the argument as given when a `-c` argument contains no `=` or has an empty key, and MUST accept an empty value.
- R-UFWG-IQ7J: `options.ParseFlags` MUST NOT apply any D3 semantic validation: an argv that `Flags.Validate` rejects for a semantic reason MUST still return from `ParseFlags` with a nil error and the parsed `Flags`.
- R-UIC9-A9OX: An unknown flag MUST exit 2, write the usage text to stderr, and write nothing to stdout.
- R-UJK5-O1FM: A usage error MUST report its cause exactly once — the text naming the offending flag or key MUST occur exactly one time across stdout and stderr combined.
- R-UKS2-1T6B: `-h`, `--help`, `-V`, and `--version` MUST each exit 0 without reading stdin, without reading any file under `Deps.Home`, and without constructing any tool, verified with a stdin that fails the test if read and a `Deps.Home` naming a directory that does not exist.

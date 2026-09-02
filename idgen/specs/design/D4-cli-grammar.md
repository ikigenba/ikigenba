# D4-cli-grammar

idgen is a single command with options — no subcommands; `--decode` flips the
mode. Grammar (also the shape of the usage text, D6):

```
idgen [options] [ID ...]
```

Options (short and long forms are equivalent wherever both exist):

| option           | meaning                                              | default |
|------------------|------------------------------------------------------|---------|
| `-n`, `--number` | mint N identifiers                                   | 1       |
| `-p`, `--prefix` | use PREFIX as the id prefix                          | `R`     |
| `--decode`       | decode ID arguments, or whitespace-delimited IDs from stdin | off |
| `-h`, `--help`   | print help                                           |         |
| `-V`, `--version`| print version                                        |         |

Parsing uses the standard library `flag` package (no third-party CLI
dependency); options therefore precede positionals. Positional `ID` arguments
are meaningful only in decode mode — mint mode takes none, and supplying one
is a usage error that names the unexpected argument. `-n`/`-p` supplied
alongside `--decode` are accepted but inert (they are mint concerns with no
decode meaning).

Exit-code taxonomy, as the exported type `ExitCode` (underlying type `int`)
with package constants `exitSuccess = 0`, `exitFailure = 1`, `exitUsage = 2`.
`Run` returns this type (D1); `main` converts it to `int` for `os.Exit`:

- `0` — success (including help and version).
- `1` — decode data failure: at least one malformed id in an otherwise valid
  invocation (D5).
- `2` — usage error: unknown or malformed flags, invalid `--prefix`,
  non-positive `--number`, or positional arguments in mint mode. Usage errors
  report to stderr (the usage text plus, where applicable, an
  `idgen: <problem>` line); help requested explicitly goes to stdout.

## REQUIREMENTS

- R-VKIS-QBJ8: Package `internal/cli` MUST export a type named `ExitCode` — a typed exit-code enumeration whose underlying type is `int` — with exactly three named values: `exitSuccess` = 0, `exitFailure` = 1, and `exitUsage` = 2.
- R-U5PD-1SYE: The command MUST accept exactly this option set, with short and long forms equivalent where both exist: `-n`/`--number`, `-p`/`--prefix`, `--decode`, `-h`/`--help`, `-V`/`--version`; and no other options.
- R-U6X9-FKP3: The `--number` option MUST default to 1 when not supplied.
- R-U855-TCFS: The `--prefix` option MUST default to `R` when not supplied.
- R-U9D2-746H: idgen MUST be a single command (no subcommands) with grammar `idgen [options] [ID ...]`, where options precede positional arguments and positional `ID` arguments are accepted only under `--decode`.
- R-T2Q3-EX89: `--help` and `-h` MUST print the usage text exactly once to stdout, write nothing to stderr, and exit 0 (guarding against the stdlib `flag` double-print of usage).
- R-T3XZ-SOYY: `--version` MUST print the version string to stdout and exit 0.
- R-T55W-6GPN: An unknown option MUST exit 2 with a non-empty stderr that includes the usage text.
- R-T6DS-K8GC: A positional argument in mint mode MUST exit 2 with an stderr message naming the unexpected argument.
- R-T7LO-Y071: `--decode` MUST route the invocation to the decode path (D5) rather than minting.
- R-T8TL-BRXQ: `-n` and `-p` supplied together with `--decode` MUST leave the decode output unchanged.
- R-TA1H-PJOF: A bare invocation (no options, no positionals) MUST print exactly one line matching `^R-[0-9A-Z]{4}-[0-9A-Z]{4}$` to stdout and exit 0.
- R-TB9E-3BF4: `-p X` (for both one-character and multi-character prefixes) MUST print ids matching `^X-[0-9A-Z]{4}-[0-9A-Z]{4}$` with the supplied prefix fully replacing the default `R` (never concatenated with it).

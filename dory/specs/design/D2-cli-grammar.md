# D2-cli-grammar

`dory` is a single command with flags — no subcommands, no positional
arguments. The prompt is stdin. Grammar (also the first line of the usage
text):

```
dory [-c key=value ...] [-resume UUID] [-V] [-h] < prompt
```

| flag              | meaning                                                   | default  |
|-------------------|-----------------------------------------------------------|----------|
| `-c key=value`    | set a config value (repeatable, last wins; D3)            | *(none)* |
| `-resume UUID`    | run the pass in an existing session instead of a new one  | *(none)* |
| `-h`, `--help`    | print the usage text and exit                             |          |
| `-V`, `--version` | print the version and exit                                |          |

Parsing uses the standard library `flag` package, so `-c`/`--c` and
`-resume`/`--resume` are equivalent spellings; the usage text documents the
single-dash forms and both forms for help and version, matching agent-repl.

```
$ dory <<'EOF'
Recreate an Asteroids-like game for Linux, in C, using SDL3. Maintain a
`make verify` target that proves every requirement you decide on.
EOF
1 assistant › ...
1 tool › delegate {"role":"worker","prompt":"Inspect this machine for a C toolchain..."}
1.1 tool › Bash {"command":"cc --version"}
1.1 result › Bash cc (Debian 14.2.0-19) 14.2.0 ...
1.1 assistant › gcc 14, GNU make, libsdl3-dev 3.2 present. No meson/cmake.
1 result › delegate gcc 14, GNU make, libsdl3-dev 3.2 present. No meson/cmake.
1 tool › remember {"text":"Toolchain confirmed: gcc 14, make, libsdl3-dev..."}
1 result › remember ok
1 assistant › Toolchain confirmed. Nothing built yet. Next: decide the
unspecified requirements before writing code.

summary
· tokens   in=8120 cache(r=0 w=0) out=611 reasoning=0 total=8731
· cost     $0.031200 pass
· session  7f0c2e3a-1d5b-4c9e-9a2f-3b8d6e1f0a47

$ dory -resume 7f0c2e3a-1d5b-4c9e-9a2f-3b8d6e1f0a47 <<'EOF'
Continue toward the goal. Decide the unspecified requirements and remember
each with a one-line reason, then create the project skeleton.
EOF
2 assistant › ...
```

**Parsing is two operations, split on the syntax/semantics seam**, as in the
siblings:

- `options.ParseFlags(args []string) (Flags, error)` applies the grammar and
  nothing else. It returns the raw flags, the sentinel `ErrHelp` for `-h` and
  `--help`, or an error naming an unknown flag or a malformed `-c` argument. A
  `-c` argument is malformed when it has no `=` or an empty key; its value may
  be empty. `Flags.Config` keeps every `-c` occurrence in command-line order
  as `Pair{Key, Value}` values, duplicates included.
- `Flags.Validate() (Options, error)` is the semantic half (D3): it folds the
  pairs per role, applies every check that needs no I/O, and returns an error
  naming the offending key. It also checks `Resume`, when given, is a
  lowercase RFC 4122 UUID string, since the value becomes a file name.

Neither operation writes to any stream: the flag set's output is discarded
and `Usage()` returns the help text as a string for the caller to place.

**The prompt** is all of stdin, read once after validation and before any
file or network I/O. It is used verbatim as the root's prompt (D5) except
that trailing white space is trimmed. A prompt that is empty after trimming
is a usage error: there is nothing to run.

Exit-code taxonomy, as package constants in `cli`, `exitSuccess = 0`,
`exitFailure = 1`, `exitUsage = 2`:

- `0` — the root supervisor reported (D5), or help or version was printed.
- `1` — dory failed: the session could not be created or opened, the session's
  recorded root differs from `Deps.Root`, a credential is missing, a tool could
  not be constructed, the root's turn ended in a terminal error, or the pass
  was interrupted.
- `2` — usage error: an unknown flag, a malformed `-c` argument, an empty
  prompt, or any validation failure from D3. Usage errors report to stderr;
  help requested explicitly goes to stdout.

A child agent failing is never an exit: it is the parent's tool result (D5).

## REQUIREMENTS

- R-HIPA-5Z4K: Package `internal/options` MUST export a `Pair` struct whose fields are exactly `Key string` and `Value string`.
- R-HJX6-JQV9: Package `internal/options` MUST export a `Flags` struct whose fields are exactly `Config []Pair`, `Resume string`, and `Version bool`.
- R-HL52-XILY: Package `internal/options` MUST export `ParseFlags(args []string) (Flags, error)`, the method `Validate() (Options, error)` on `Flags`, `Usage() string`, and a sentinel error `ErrHelp` that `ParseFlags` returns when `-h` or `--help` is supplied.
- R-HMCZ-BACN: `options.ParseFlags` MUST accept `-c`, `-resume`, `-V`, and `--version`, populating `Flags.Config`, `Flags.Resume`, and `Flags.Version` respectively, and with every flag absent MUST return the zero `Flags`.
- R-HNKV-P23C: Repeated `-c` occurrences MUST each append a `Pair` to `Flags.Config` in command-line order, including when a key repeats, with `Key` the text before the first `=` and `Value` the text after it, and `ParseFlags` MUST return an error whose text contains the argument as given when a `-c` argument contains no `=` or has an empty key.
- R-HOSS-2TU1: `Flags.Validate` MUST return an error naming `resume` when `Flags.Resume` is non-empty and is not a lowercase RFC 4122 UUID string of the form `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`, and MUST place a valid value in `Options.Resume` unchanged.
- R-HQ0O-GLKQ: `options.Usage` MUST return a string whose first line is exactly `usage: dory [-c key=value ...] [-resume UUID] [-V] [-h] < prompt` and which names every configuration key of D3 for both roles.
- R-HR8K-UDBF: An unknown flag MUST exit 2, write the usage text to stderr, and write nothing to stdout, and the text naming the offending flag or key MUST occur exactly one time across stdout and stderr combined.
- R-HSGH-8524: `-h`, `--help`, `-V`, and `--version` MUST each exit 0 without reading stdin, without reading or creating any file under `Deps.Home`, and without constructing any tool, verified with a stdin that fails the test if read and a `Deps.Home` naming a directory that does not exist.
- R-HTOD-LWST: `Run` MUST read all of stdin as the prompt after validation succeeds and before creating or opening the session, trim trailing white space, and exit 2 writing an error to stderr and nothing to stdout when the trimmed prompt is empty, verified by a `Deps.Home` naming a directory that does not exist remaining absent.

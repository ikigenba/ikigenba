# D08-cli-grammar

`oauth` is a single command with flags — no subcommands, no positional
arguments. A login is described entirely by its flags; the program holds no
provider-specific knowledge, so there is nothing to name but the service's own
endpoints and identifiers. Grammar (also the shape of the usage text, D10):

```
oauth [flags]
```

Flags (short and long forms are equivalent wherever both exist):

| flag              | meaning                                            | default     |
|-------------------|----------------------------------------------------|-------------|
| `--auth-url`      | authorization endpoint                             | *required*  |
| `--token-url`     | token endpoint                                     | *required*  |
| `--client-id`     | OAuth client id                                    | *required*  |
| `--scope`         | space-separated OAuth scopes                       | *(none)*    |
| `--client-secret` | client secret sent in the token request body       | *(none)*    |
| `--callback-host` | host used in the redirect URI                      | `localhost` |
| `--port`          | loopback callback port; 0 chooses an available one | `0`         |
| `--callback-path` | callback route and redirect URI path               | `/callback` |
| `--auth-param`    | extra authorize parameter, `key=value` (repeatable)| *(none)*    |
| `--token-param`   | extra token parameter, `key=value` (repeatable)    | *(none)*    |
| `--token-header`  | extra token request header, `key=value` (repeatable)| *(none)*   |
| `--no-browser`    | print the authorize URL without opening a browser  | `false`     |
| `--timeout`       | maximum time to wait for the callback              | `5m`        |
| `-h`, `--help`    | print help                                         |             |
| `-V`, `--version` | print version                                      |             |

Parsing uses the standard library `flag` package (no third-party CLI
dependency). `--callback-host` names the host string that goes into the
redirect URI providers match against; it does not choose what the listener
binds, which is always the loopback literals (D05).

The three `key=value` flags are repeatable and order-preserving: each
occurrence appends, and the resulting `[]oauth.Param` slices carry the values
in the order supplied. Order is contractual because a provider may be
sensitive to parameter order, and because a repeated key is legal — two
`--auth-param foo=` occurrences send `foo` twice rather than one replacing the
other (D03).

**Parsing is two operations, split on the syntax/semantics seam.** Turning
`args` into a validated login is two distinct responsibilities, and the design
gives each its own operation rather than one function that carries both:

- `options.ParseFlags(args []string) (Flags, error)` applies the flag grammar
  above and nothing else. It returns the raw, syntactically-parsed flags in a
  `Flags` value, the sentinel `ErrHelp` for `-h`/`--help`, or an error naming an
  unknown or malformed flag. It performs none of the D09 semantic checks — a
  missing required flag, an unparseable URL, or a reserved parameter is not its
  concern — so a `Flags` it returns is well-formed as syntax but not yet known
  to describe a valid login.
- `Flags.Validate() (Options, error)` is the semantic half (D09): it applies
  every validation check and returns fully-validated `Options`, with the URL
  strings parsed to `*url.URL`, or an error naming the offending flag.

`Flags` carries the raw flag values — the endpoint URLs as the strings the user
typed, the client id, scope, secret, callback host, port, path, the three
ordered `key=value` slices, `--no-browser`, `--timeout`, and the `--version`
intent. `Options` (D09) is its validated projection.

Neither operation takes a writer or **writes anything to any stream**: the
underlying `flag.FlagSet`'s output is discarded, and `Usage()` returns the help
text as a string for the caller to place (D10). This is deliberate and
load-bearing. The shipping implementation lets the `flag` package print its own
complaint, then prints the same complaint again itself, so `oauth --bogus`
emits `flag provided but not defined: -bogus` on line 1 and `error: flag
provided but not defined: -bogus` on line 58, the same sentence twice with
fifty-six lines of usage text between them. Silencing the flag set and giving
`cli` sole authorship of diagnostics makes that double report *structurally
impossible* rather than something a reviewer must remember to check for.

Splitting syntax from semantics is what keeps either operation legible: each
owns one phase, so neither accretes the construct-dispatch-validate-finalize
sprawl a single `Parse` would. Mode dispatch — the `-h`/`--help` and
`--version` short-circuits that must precede validation — belongs to neither
half but to the composition root, which sequences `ParseFlags`, the help and
version exits, and `Validate` in order (D11).

Exit-code taxonomy, as package constants in `cli`, `exitSuccess = 0`,
`exitFailure = 1`, `exitUsage = 2`:

- `0` — success, including help and version.
- `1` — runtime failure: the login was well-formed but did not complete (bind
  failure, provider error, state mismatch, non-2xx token response, timeout).
- `2` — usage error: an unknown or malformed flag, or any validation failure
  from D09. Usage errors report to stderr; help requested explicitly goes to
  stdout (D10).

The taxonomy is wider than the shipping binary's, which exits `1` for
everything. Separating "you invoked me wrongly" from "the login failed" lets a
calling script retry the second and never the first, and it matches the
sibling `idgen` so the two tools read the same way. Widening a code space is
the compatible direction; narrowing it is not.

## REQUIREMENTS

- R-JL3B-GSOS: `options.ParseFlags` MUST accept every flag in the table above and populate the corresponding `Flags` field with the supplied value.
- R-JMB7-UKFH: With a flag absent, `options.ParseFlags` MUST supply its documented default — `localhost` for the callback host, `0` for the port, `/callback` for the callback path, `5m` for the timeout, and the zero value for every other field of `Flags`.
- R-JNJ4-8C66: Repeated `--auth-param`, `--token-param`, and `--token-header` occurrences MUST each append to their slice in `Flags`, preserving the order supplied on the command line, including when a key repeats.
- R-JOR0-M3WV: `options.ParseFlags` MUST NOT apply any D09 semantic validation: an argv that `Flags.Validate` rejects for a semantic reason — including a missing required flag and a reserved `--auth-param` key — MUST still return from `ParseFlags` with a nil error and the parsed `Flags`.
- R-QOVV-M4XY: An unknown flag MUST exit 2, write the usage text to stderr, and write nothing to stdout.
- R-QQ3R-ZWON: A usage error MUST report its cause exactly once — the text naming the offending flag MUST occur exactly one time across stdout and stderr combined.
- R-QRBO-DOFC: `-h` and `--help` MUST exit 0 without binding a listener and without invoking the browser launcher, verified with a `callback.ListenFunc` and a `browser.Launcher` that fail the test if called.
- R-QSJK-RG61: `-V` and `--version` MUST exit 0 without binding a listener and without invoking the browser launcher, verified with a `callback.ListenFunc` and a `browser.Launcher` that fail the test if called.

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

**Parsing is a pure function.** `options.Parse(args []string) (Options, error)`
returns fully-validated `Options` (D09), the sentinel `ErrHelp`, or an error
naming the offending flag. It takes no writer and **writes nothing to any
stream**: the underlying `flag.FlagSet`'s output is discarded, and `Usage()`
returns the help text as a string for the caller to place (D10). This is
deliberate and load-bearing. The shipping implementation lets the `flag`
package print its own complaint, then prints the same complaint again itself,
so `oauth --bogus` emits `flag provided but not defined: -bogus` on line 1 and
`error: flag provided but not defined: -bogus` on line 58, the same sentence
twice with fifty-six lines of usage text between them. Silencing the flag set
and giving `cli` sole authorship of diagnostics makes that double report
*structurally impossible* rather than something a reviewer must remember to
check for.

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

- R-QK0A-31Z6: `options.Parse` MUST accept every flag in the table above and populate the corresponding `Options` field with the supplied value.
- R-QL86-GTPV: With a flag absent, `options.Parse` MUST supply its documented default — `localhost` for `CallbackHost`, `0` for `Port`, `/callback` for `CallbackPath`, `5m` for `Timeout`, and the zero value for every other field.
- R-QMG2-ULGK: Repeated `--auth-param`, `--token-param`, and `--token-header` occurrences MUST each append to their slice, preserving the order supplied on the command line, including when a key repeats.
- R-QOVV-M4XY: An unknown flag MUST exit 2, write the usage text to stderr, and write nothing to stdout.
- R-QQ3R-ZWON: A usage error MUST report its cause exactly once — the text naming the offending flag MUST occur exactly one time across stdout and stderr combined.
- R-QRBO-DOFC: `-h` and `--help` MUST exit 0 without binding a listener and without invoking the browser launcher, verified with a `callback.ListenFunc` and a `browser.Launcher` that fail the test if called.
- R-QSJK-RG61: `-V` and `--version` MUST exit 0 without binding a listener and without invoking the browser launcher, verified with a `callback.ListenFunc` and a `browser.Launcher` that fail the test if called.

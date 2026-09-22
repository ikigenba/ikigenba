# D02-cli

dummy as a command: what `cli.Run` does with the arguments and the
environment that `D01-layout-and-run-seam` hands it through `Process`. This
design covers the three commands (`--version`, `manifest`, `--help`), the
usage error, the `PORT` check, and the hand-off to `server.Serve` when there
is nothing to complain about. What happens once the listener is bound — the
silent healthy serve, the graceful stop, the bind failure's line and exit
code — is `D03-serve`; what the panel then answers is `D04-panel` and the
designs it leads to.

`Run` takes at most one argument and the argument decides everything. An
empty `Args` means serve. The three recognised single arguments each write
one product to `Stdout` and return `ExitSuccess` without touching `PORT`:
`--version` writes `Version` and a newline, `manifest` writes `Manifest`
exactly as declared, and `--help` writes `Usage`, the constant
`D01-layout-and-run-seam` declares and whose value this design fixes byte for
byte, so the help text is contract. Any other `Args` is a usage error. The
argument dummy complains about is the first one it does not take: the first
element when that is not a recognised word, otherwise the second element,
which is surplus whatever it says. An argument that starts with `-` is an
unknown option; any other is an unknown command. Both diagnostics follow the
repository's command-line conventions: the first line begins `dummy: `, and
after exactly one empty line comes the hint to run `dummy --help`, unprefixed
because dummy is speaking for itself. The usage text is never written to
`Stderr`.

The help text names the exit codes, which is where the contract the rest of
the design realises is declared: 0 for success, 1 for a server that failed,
2 for a usage error. The split matters at the `PORT` check below — a `PORT`
dummy will not accept is the caller's mistake and exits 2, while a port it
accepts but cannot bind is trouble on the host and exits 1 (`D03-serve`).

Arguments are checked before the environment is read. A command never needs
a port, so `dummy --version` succeeds with `PORT` unset and `dummy bogus`
fails as an unknown command however `PORT` is set; `Run` does not call
`LookupEnv` at all unless `Args` is empty.

The serve path reads `PORT` through `LookupEnv`. Unset and empty are the
same failure. A value is a port number only when it is spelled the way the
host's environment file spells one: one to five ASCII decimal digits, no
sign, no surrounding whitespace, no leading zero, denoting an integer from 1
to 65535. Anything else is quoted back verbatim in the diagnostic. Both
failures are usage errors, exit 2, and neither binds a listener. With a port
number in hand, `Run` asks for a TCP listener on `127.0.0.1:<port>` through
`Process.Listen`, which is `net.Listen` unless a test injects its own.

When the bind succeeds, `Run` does two more things and then stops being
interesting. It makes the process's widget set, once, with the store
constructor `D05-widgets` declares, and it hands `server.Serve` its own
context, that listener, and the handler `D04-panel` builds over that store.
That is the whole hand-off: bind, store, serve. The store is made here rather
than inside the handler because this is the one place a process's widget set
comes into being, and it is made after the bind rather than before so that a
bind that fails has built nothing — `--version` never constructs a store
either, since it returns long before this point. From here `D03-serve`
governs: a bind that fails is its one-line diagnostic and exit 1 with no
`Serve` call, and `D01`'s `Listening` callback tells a test the listener is
up.

Every failure leaves `Stdout` empty, so a caller that reads the streams
separately sees a product or a complaint, never a mixture. Every diagnostic
is one write to `Stderr`, so the two-line usage diagnostic lands whole even
when another writer such as journald interleaves with the same stream.

## REQUIREMENTS

- R-11OW-EKKC: `Usage` MUST be exactly `"Usage: dummy [command]\n\nServe the dummy control panel at 127.0.0.1:$PORT. With no command, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"`.
- R-MMB3-ASQ6: When `Args` is exactly `["--version"]`, `Run` MUST write `Version` followed by a single `"\n"` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-MNIZ-OKGV: When `Args` is exactly `["manifest"]`, `Run` MUST write exactly `Manifest` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-MOQW-2C7K: When `Args` is exactly `["--help"]`, `Run` MUST write exactly `Usage` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-MPYS-G3Y9: `Run` MUST treat `Args` as a usage error unless `Args` is empty or is exactly one of `["--version"]`, `["manifest"]`, or `["--help"]`.
- R-MR6O-TVOY: On a usage error from `Args`, the offending argument MUST be `Args[0]` when `Args[0]` is none of `--version`, `manifest`, or `--help`, and `Args[1]` otherwise.
- R-MSEL-7NFN: On a usage error from `Args`, `Run` MUST write exactly `"dummy: unknown option '" + arg + "'\n\nsee 'dummy --help' for usage\n"` to `Stderr` when the offending argument `arg` begins with `-`, and exactly `"dummy: unknown command '" + arg + "'\n\nsee 'dummy --help' for usage\n"` otherwise, MUST write nothing to `Stdout`, and MUST return `ExitUsage`.
- R-MTMH-LF6C: When `Args` is not empty, `Run` MUST return without calling `LookupEnv` and without binding a listener, so that the outcome of a command or a usage error is the same whatever `PORT` holds.
- R-MUUD-Z6X1: When `Args` is empty and `LookupEnv("PORT")` returns `false` or returns the empty string, `Run` MUST write exactly `"dummy: PORT is not set\n"` to `Stderr`, write nothing to `Stdout`, bind no listener, and return `ExitUsage`.
- R-MW2A-CYNQ: `Run` MUST accept a `PORT` value as a port number if and only if it consists of one to five ASCII decimal digits with no leading zero and no other characters, and the integer those digits denote is from 1 to 65535 inclusive.
- R-ZOWQ-BRGU: When `Args` is empty and `LookupEnv("PORT")` returns `true` with a non-empty value `v` that is not a port number, `Run` MUST write exactly `"dummy: PORT is '" + v + "', not a port number\n"` to `Stderr` with `v` verbatim, write nothing to `Stdout`, bind no listener, and return `ExitUsage`.
- R-12WS-SCB1: When `Args` is empty and `PORT` is a port number `<port>`, `Run` MUST attempt to bind a TCP listener on `127.0.0.1:<port>` through `Process.Listen`, calling `net.Listen` when `Listen` is nil, and when the bind succeeds MUST call `widget.NewStore` exactly once and then `server.Serve` exactly once with `Run`'s own `ctx`, that listener, and `panel.Handler(store)`, where `store` is the value `widget.NewStore` returned.
- R-N0XV-W1MI: Whenever `Run` returns a value other than `ExitSuccess` it MUST have written nothing to `Stdout`, and every diagnostic `Run` writes MUST be delivered as a single call to `Stderr.Write` whose first line begins `dummy: `.

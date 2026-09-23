# D02-cli

dummy as a command: what `cli.Run` does with the arguments that
`D01-layout-and-run-seam` hands it through `Process`. This design covers the
three commands (`--version`, `manifest`, `--help`), the usage error, and the
rule that a command never looks at the environment. When `Args` is empty,
dummy serves, and everything from there — the drain deadline, the socket the
host passes in, readiness, the drain on a signal, and the failures of each —
is `D03-serve`; what the panel then answers is `D04-panel` and the designs it
leads to.

`Run` takes at most one argument and the argument decides everything. An
empty `Args` means serve. The three recognised single arguments each write
one product to `Stdout` and return `ExitSuccess` without touching the
environment: `--version` writes `Version` and a newline, `manifest` writes
`Manifest` exactly as declared, and `--help` writes `Usage`, the constant
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

The help text says where dummy serves — on the socket systemd passes in — and
names the exit codes, which is where the contract the rest of the design
realises is declared: 0 for success, 1 for a server that failed, 2 for a usage
error. `D03-serve` leans on that split: a start the caller got wrong (no
socket, several sockets, a drain deadline that is not a number of seconds)
exits 2, while trouble on the host once dummy is serving, a drain that runs
out included, exits 1.

Arguments are checked before the environment is read. A command never needs
a socket, so `dummy --version` succeeds with nothing passed in and
`dummy bogus` fails as an unknown command whatever the environment holds;
`Run` touches none of the environment, the inherited descriptor or systemd's
notification socket unless `Args` is empty.

Every failure leaves `Stdout` empty, so a caller that reads the streams
separately sees a product or a complaint, never a mixture. Every diagnostic
is one write to `Stderr`, so a two-line diagnostic lands whole even when
another writer such as journald interleaves with the same stream.

## REQUIREMENTS

- R-LPBS-669U: `Usage` MUST be exactly `"Usage: dummy [command]\n\nServe the dummy control panel on the socket systemd passes in. With no\ncommand, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"`.
- R-MMB3-ASQ6: When `Args` is exactly `["--version"]`, `Run` MUST write `Version` followed by a single `"\n"` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-MNIZ-OKGV: When `Args` is exactly `["manifest"]`, `Run` MUST write exactly `Manifest` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-MOQW-2C7K: When `Args` is exactly `["--help"]`, `Run` MUST write exactly `Usage` to `Stdout` and nothing else, write nothing to `Stderr`, and return `ExitSuccess`.
- R-MPYS-G3Y9: `Run` MUST treat `Args` as a usage error unless `Args` is empty or is exactly one of `["--version"]`, `["manifest"]`, or `["--help"]`.
- R-MR6O-TVOY: On a usage error from `Args`, the offending argument MUST be `Args[0]` when `Args[0]` is none of `--version`, `manifest`, or `--help`, and `Args[1]` otherwise.
- R-MSEL-7NFN: On a usage error from `Args`, `Run` MUST write exactly `"dummy: unknown option '" + arg + "'\n\nsee 'dummy --help' for usage\n"` to `Stderr` when the offending argument `arg` begins with `-`, and exactly `"dummy: unknown command '" + arg + "'\n\nsee 'dummy --help' for usage\n"` otherwise, MUST write nothing to `Stdout`, and MUST return `ExitUsage`.
- R-MUSD-6DHG: When `Args` is not empty, `Run` MUST return without calling `LookupEnv`, `Unsetenv`, or `Inherit`, without taking file descriptor 3, and without sending anything to a notification socket, so that the outcome of a command or a usage error is the same whatever the environment holds.
- R-N0XV-W1MI: Whenever `Run` returns a value other than `ExitSuccess` it MUST have written nothing to `Stdout`, and every diagnostic `Run` writes MUST be delivered as a single call to `Stderr.Write` whose first line begins `dummy: `.

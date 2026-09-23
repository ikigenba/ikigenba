# D02-cli-grammar

What `cli.Run` does with the arguments `D01-layout-and-run-seam` hands it:
how an argument is read, what each kind of argument decides, the diagnostics
for a usage error and for output that cannot be written, and which exit code
each outcome returns. The help text and the version string themselves are
`D03-help-and-version`.

With no arguments, agent-monitor greets: it writes `hello, world` and a
newline to standard output and succeeds.

Arguments are read strictly left to right, and the first argument that
decides the outcome wins; nothing after it is looked at. Today every argument
decides, so the first argument alone determines everything, and anything that
follows it — known, unknown, or malformed — changes nothing. An argument that
begins with `-` is an option. It is a known option only when it is exactly
one of `--help`, `-h`, `--version`, or `-V`; any other option is unknown and
is named back as typed, in escaped form. There is no bundling of short options and no
`--name=value` form, and `--` has no end-of-options meaning, so `-hV`,
`--help=x`, `-`, and `--` are all unknown options. An argument that does not
begin with `-`, the empty string included, is a command; agent-monitor has no
commands yet, so every command is an unknown command.

Both usage-error diagnostics follow the repository's command-line
conventions: the first line begins `agent-monitor: ` and names the argument
in single quotes, then exactly one empty line, then the unprefixed hint to
run `agent-monitor --help`.

The argument is echoed in its escaped form, so that no argument can forge
output: ordinary printable text, ASCII or not, passes through unchanged, so
every plain argument reads exactly as typed, while `\`, `'`, control bytes,
DEL, bytes that are not valid UTF-8, and non-printable Unicode characters
(line and paragraph separators, bidirectional overrides, C1 controls,
non-ASCII spaces) are escaped Go-style. A newline therefore never reaches
standard error from an argument, an empty argument shows as `''`, and a
terminal escape sequence is shown rather than rendered. The usage text itself is never written to
standard error, and a usage error writes nothing to standard output.

Every product — the greeting, the help text, the version line — is handed to
standard output in one write, and every diagnostic to standard error in one
write, so a diagnostic lands whole and a failed product write is one
well-defined event. When that write fails, agent-monitor says so on standard
error with the write error's own text as the reason, writes nothing more to
standard output, and exits 1. If standard error cannot be written either,
there is nowhere left to report to: the exit code stays the one the outcome
already chose, and nothing is retried.

## REQUIREMENTS

- R-2MRX-ZE0A: Every call to `Run` MUST return one of `ExitSuccess`, `ExitWriteFailed`, or `ExitUsage`, and no other value.
- R-2NZU-D5QZ: When `args` is empty, `Run` MUST write exactly `"hello, world\n"` to `stdout`, write nothing to `stderr`, and return `ExitSuccess`.
- R-2P7Q-QXHO: `Run` MUST classify an argument as a known option if and only if it is byte-for-byte equal to one of `--help`, `-h`, `--version`, or `-V`; as an unknown option if and only if it begins with `-` and is not a known option; and as an unknown command if and only if it does not begin with `-`, the empty string included.
- R-2QFN-4P8D: When `args` is not empty, `Run` MUST behave exactly as it does for `args[:1]` — the same bytes written to `stdout`, the same bytes written to `stderr`, and the same return value — whatever the later elements of `args` are.
- R-HALR-4IFD: The escaped form of an argument `arg` MUST be the concatenation, reading `arg` from its first byte to its last and taking at each position the element `utf8.DecodeRuneInString` decodes there, of: for a byte that does not begin a valid UTF-8 encoding (the decode yields `utf8.RuneError` with width 1), `\x` followed by the byte's value as two lowercase hexadecimal digits; for byte 0x5C (`\`), `\\`; for byte 0x27 (`'`), `\'`; for byte 0x0A, `\n`; for byte 0x09, `\t`; for byte 0x0D, `\r`; for any other byte 0x00–0x1F and for byte 0x7F, `\x` followed by the byte's value as two lowercase hexadecimal digits; for any other byte 0x20–0x7E, that byte unchanged; for a validly encoded rune `r` of U+0080 or above with `unicode.IsPrint(r)` true, its encoding unchanged; and for a validly encoded rune `r` of U+0080 or above with `unicode.IsPrint(r)` false, `\u` followed by `r` as four lowercase hexadecimal digits when `r` is at most U+FFFF, otherwise `\U` followed by `r` as eight lowercase hexadecimal digits — so that, for example, `bogus` escapes to `bogus`, `a` TAB `b` to `a\tb`, `it's` to `it\'s`, `é` to `é`, the single byte 0xFF to `\xff`, ESC `[31m` to `\x1b[31m`, U+202E to `\u202e`, and U+E0001 to `\U000e0001`.
- R-HD1J-W1WR: When `args[0]` is an unknown option `arg`, `Run` MUST write exactly `"agent-monitor: unknown option '" + e + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, where `e` is the escaped form of `arg`, write nothing to `stdout`, and return `ExitUsage`.
- R-HE9G-9TNG: When `args[0]` is an unknown command `arg`, `Run` MUST write exactly `"agent-monitor: unknown command '" + e + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, where `e` is the escaped form of `arg`, write nothing to `stdout`, and return `ExitUsage`.
- R-2VB8-NS75: `Run` MUST treat each of `-`, `--`, `-hV`, `-Vh`, `--help=x`, `--version=x`, `--HELP`, and `-H` as an unknown option, and MUST give `--` no end-of-options meaning, so that `Run` with `args` `["--", "--help"]` reports unknown option `--`.
- R-2WJ5-1JXU: `Run` MUST deliver each product it writes to `stdout` as exactly one call to `stdout.Write`, and each diagnostic it writes to `stderr` as exactly one call to `stderr.Write`.
- R-2XR1-FBOJ: When the call `Run` makes to `stdout.Write` returns a non-nil error `err`, whichever product was being written, `Run` MUST make no further call to `stdout.Write`, MUST write exactly `"agent-monitor: write error: " + err.Error() + "\n"` to `stderr`, and MUST return `ExitWriteFailed`.
- R-2YYX-T3F8: When `Run` is writing the write-error diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitWriteFailed`.
- R-306U-6V5X: When `Run` is writing an unknown-option or unknown-command diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitUsage`.
- R-31EQ-KMWM: When `Run` returns `ExitUsage` it MUST have made no call to `stdout.Write`, and when it returns `ExitWriteFailed` its only call to `stdout.Write` MUST be the one that returned the error.
- R-32MM-YENB: When `Run` returns `ExitSuccess` it MUST have made no call to `stderr.Write`, and every call `Run` makes to `stderr.Write` MUST pass exactly an unknown-option, unknown-command, or write-error diagnostic, so that `Run` never writes `Usage` to `stderr`.

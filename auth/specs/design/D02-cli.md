# D02-cli

auth is one binary. Before it serves anything it is a command-line program, and
this document fixes that surface: the grammar of commands and options it
accepts, the text it prints for `--help`, the manifest it prints for `manifest`,
what `--version` reports, how it refuses a command or option it does not know,
and the rule that a command never looks at the environment. Running the binary
with no command serves the auth service; the serving behavior itself — the
Google settings, the drain deadline, the socket the host passes in, readiness,
the drain on a signal, and the failures of each — is D03's contract, and this
document only fixes that a bare invocation is the one that serves.

The manifest is a fact about the binary, so the binary emits it. The committed
`etc/manifest.toml` in the checkout is a copy kept so the tree can be read
without a build, and the two are byte-identical. The manifest declares auth's
name, that it is not the host's default app, the secrets it needs, its
Workspace domain, and its SQLite database. It declares no port: auth serves on
the socket the host passes it, and a manifest carrying `port` is refused by
`devctl build` and by opsctl. The domain `michaelgreenly.dev` and the database
path are data the manifest fixes, not release versions.

The help text says where auth serves — on the socket systemd passes in — and
names the exit codes: `0` on success, `1` when the server fails, and `2` on a
usage error. D03 leans on that split: a start the caller got wrong (a missing
Google setting, a drain deadline that is not a number of seconds, no socket,
several sockets) exits 2, while trouble on the host once auth has its socket —
a database it cannot open, a drain that runs out — exits 1.

Arguments are checked before the environment is read. A command never needs a
socket, so `auth --version` succeeds with nothing passed in and `auth bogus`
fails as an unknown command whatever the environment holds; `Run` touches none
of the environment, the inherited descriptor or systemd's notification socket
unless `Args` is empty.

The program follows the repository's stream and exit conventions: the product
of a command goes to stdout, a diagnostic goes to stderr with a first line that
begins `auth: `, and every failure leaves stdout empty, so a caller that reads
the streams separately sees a product or a complaint, never a mixture. Every
diagnostic is one write to stderr, so a two-line diagnostic lands whole even
when another writer such as journald interleaves with the same stream. The
version reported by `--version` is the value of `Version` in
`internal/version` (D01); it is read through that package and never written as
a literal here.

## REQUIREMENTS

- R-OV73-80LN: auth's command-line grammar MUST recognize exactly these forms and no others: an invocation with no command (which serves), the `manifest` subcommand, the `--help` option, and the `--version` option.
- R-M5Q7-Q174: The usage text MUST be exactly the following, and nothing else (a trailing newline follows the last line):
  ```
  Usage: auth [command]

  Serve the auth service on the socket systemd passes in. With no command,
  serve.

  Commands:
    manifest   print the app manifest

  Options:
    --help      print this help
    --version   print the version

  Exit codes:
    0  success
    1  the server failed
    2  usage error
  ```
- R-M6Y4-3SXT: The app manifest MUST be exactly the following text, and nothing else (a trailing newline follows the last line):
  ```
  app = "auth"
  default = false
  secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

  [env]
  WORKSPACE_DOMAIN = "michaelgreenly.dev"

  [database]
  engine = "sqlite"
  path = "state/auth.db"
  ```
- R-OYUS-DBTQ: Invoking auth with no command MUST serve the auth service (the serving behavior is D03's contract); it MUST NOT print the usage text or the manifest.
- R-P02O-R3KF: `auth --version` MUST write the value of `Version` from `internal/version`, followed by a single newline, to stdout, write nothing to stderr, and exit `0`.
- R-P1AL-4VB4: `auth --help` MUST write the usage text to stdout, write nothing to stderr, and exit `0`.
- R-P2IH-IN1T: `auth manifest` MUST write the app manifest to stdout, write nothing to stderr, and exit `0`.
- R-P4YA-A6J7: The `etc/manifest.toml` file in the checkout MUST be byte-identical to the output of `auth manifest`.
- R-P666-NY9W: Given a first argument that is not a recognized command or option, auth MUST write to stderr exactly the line `auth: unknown command '<x>'` (where `<x>` is that argument), then one empty line, then the line `see 'auth --help' for usage`; it MUST write nothing to stdout and exit `2`.
- R-P7E3-1Q0L: Given an unrecognized option of the form `--<x>`, auth MUST write to stderr exactly the line `auth: unknown option '--<x>'`, then one empty line, then the line `see 'auth --help' for usage`; it MUST write nothing to stdout and exit `2`.
- R-P8LZ-FHRA: On a usage error, auth MUST emit only the diagnostic described above; it MUST NOT write the usage text to stderr or to stdout.
- R-M860-HKOI: When `Args` is not empty, `Run` MUST return without calling `LookupEnv`, `Unsetenv`, or `Inherit`, without taking file descriptor 3, without opening a store, and without sending anything to a notification socket, so that the outcome of a command or a usage error is the same whatever the environment holds.
- R-M9DW-VCF7: Whenever `Run` returns a value other than `0` it MUST have written nothing to `Stdout`, and every diagnostic `Run` writes MUST be delivered as a single call to `Stderr.Write` whose first line begins `auth: `.

## Canonical usage for review

```
$ auth --version
v<semver>
$ echo $?
0

$ auth manifest
app = "auth"
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"

$ diff <(auth manifest) etc/manifest.toml && echo same
same

$ auth --help
Usage: auth [command]

Serve the auth service on the socket systemd passes in. With no command,
serve.

Commands:
  manifest   print the app manifest

Options:
  --help      print this help
  --version   print the version

Exit codes:
  0  success
  1  the server failed
  2  usage error

$ auth bogus
auth: unknown command 'bogus'

see 'auth --help' for usage
$ echo $?
2

$ auth --bogus
auth: unknown option '--bogus'

see 'auth --help' for usage
$ echo $?
2
```

The `v<semver>` above is a placeholder; `--version` prints whatever
`internal/version` declares.

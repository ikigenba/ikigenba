# D02-cli

auth is one binary. Before it serves anything it is a command-line program, and
this document fixes that surface: the grammar of commands and options it
accepts, the text it prints for `--help`, the manifest it prints for `manifest`,
what `--version` reports, and how it refuses a command or option it does not
know. Running the binary with no command serves the auth service; the serving
behavior itself is D03's contract, and this document only fixes that a bare
invocation is the one that serves.

The manifest is a fact about the binary, so the binary emits it. The committed
`etc/manifest.toml` in the checkout is a copy kept so the tree can be read
without a build, and the two are byte-identical. The manifest declares auth's
name, the port it serves on, that it is not the host's default app, the secrets
it needs, its Workspace domain, and its SQLite database. The port `3001`, the
domain `michaelgreenly.dev`, and the database path are data the manifest fixes,
not release versions.

The program follows the repository's stream and exit conventions: the product
of a command goes to stdout, a diagnostic goes to stderr with a first line that
begins `auth: `, and the exit status is `0` on success, `1` when the server
fails, and `2` on a usage error. The version reported by `--version` is the
value of `Version` in `internal/version` (D01); it is read through that package
and never written as a literal here.

## REQUIREMENTS

- R-OV73-80LN: auth's command-line grammar MUST recognize exactly these forms and no others: an invocation with no command (which serves), the `manifest` subcommand, the `--help` option, and the `--version` option.
- R-OWEZ-LSCC: The usage text MUST be exactly the following, and nothing else (a trailing newline follows the last line):
  ```
  Usage: auth [command]

  Serve the auth service at 127.0.0.1:$PORT. With no command, serve.

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
- R-OXMV-ZK31: The app manifest MUST be exactly the following text, and nothing else (a trailing newline follows the last line):
  ```
  app = "auth"
  port = 3001
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

## Canonical usage for review

```
$ auth --version
v<semver>
$ echo $?
0

$ auth manifest
app = "auth"
port = 3001
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

Serve the auth service at 127.0.0.1:$PORT. With no command, serve.

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

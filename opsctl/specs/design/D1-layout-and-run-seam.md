# D1-layout-and-run-seam

`opsctl` is a Go CLI. Module path `github.com/ikigenba/ikigenba/opsctl`,
targeting the Go version pinned in `go.mod`. Standard library only for now;
the AWS SDK arrives with the DNS and backup designs as a re-mint of the
dependency requirement.

```
opsctl/                              (this sub-project; go.mod lives here)
├── cmd/opsctl/main.go               thin: os.Args/stdio/real deps → cli.Run(); os.Exit
├── internal/cli/                    grammar, usage, dispatch, exit codes, version, root check
└── internal/config/                 the host configuration store
```

- **`internal/config`** — the store at `/etc/ikigenba/config.json`: open,
  get, set, del, list, with the locking and atomic-write guarantees (D3). It
  knows nothing about flags or output.
- **`internal/cli`** — everything between the process and the packages that
  do work: the command grammar and usage text, the exit-code taxonomy, the
  root check, the version string, and the dispatch to each command (D2).

  ```go
  package cli

  // Run executes the CLI. args are the program arguments without the program
  // name; all I/O flows through the injected streams and every environmental
  // dependency through deps. Run never terminates the process; it returns
  // the process exit code.
  func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int

  // Deps carries what a command cannot be deterministic about.
  type Deps struct {
      Root string // filesystem root every host path is resolved under ("/" in production)
      EUID int    // effective user id of the process
  }
  ```

- **`cmd/opsctl`** — `main` wires `os.Args[1:]`, `os.Stdin`, `os.Stdout`,
  `os.Stderr`, `Root: "/"`, and `EUID: os.Geteuid()` into `cli.Run` and
  passes the result to `os.Exit`. It contains no other logic.

**`Deps.Root` is why the gates need no root.** Every host path in every design
is stated as an absolute path such as `/etc/ikigenba/config.json`, and every
package resolves it under `Deps.Root`. Tests pass a temporary directory as
`Root` and `EUID: 0`, so the whole surface is exercised in-process by an
ordinary user with no privileged file touched. Later designs extend `Deps`
with a command runner and cloud clients as they need them; each extension is
a re-mint of the `Deps` requirement.

Dependencies point one way: `cmd/opsctl` → `internal/cli` → `internal/config`.
`internal/config` imports nothing else in this module.

**Wiring proof at the binary level.** One test builds the real binary and
runs it as a subprocess with `--help`, which is exempt from the root check,
so the proof runs as any user.

## REQUIREMENTS

- R-MTHR-5KL5: The module MUST be `github.com/ikigenba/ikigenba/opsctl` with its own `go.mod` that specifies a Go version, and that `go.mod` MUST contain no `require` entry, verified by a test that reads `go.mod`.
- R-MUPN-JCBU: Package `internal/cli` MUST export `Run(args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.
- R-MVXJ-X42J: Package `internal/cli` MUST export a `Deps` struct whose fields are exactly `Root string` and `EUID int`.
- R-MYDC-ONJX: Every host path a command reads or writes MUST be resolved under `Deps.Root`, verified by running `config set` and `config get` through `Run` with a temporary directory as `Root` and observing the file appear under that directory and nowhere else.
- R-MZL9-2FAM: The module's import graph MUST flow one way only — `cmd/opsctl` imports `internal/cli`, `internal/cli` imports `internal/config`, and `internal/config` imports no other package of this module.
- R-N0T5-G71B: The binary built from `./cmd/opsctl`, run with the single argument `--help`, MUST print the usage text of D2 to stdout, write nothing to stderr, and exit 0.

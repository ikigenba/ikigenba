# D1-layout-and-run-seam

`devctl` is a Go CLI built and run on the developer's own machine, as an
ordinary user, under the developer's own AWS identity. Module path
`github.com/ikigenba/ikigenba/devctl`, targeting the Go version pinned in
`go.mod`. It has no direct dependencies yet: every package is standard
library. When a later design needs the AWS SDK or an ssh client, that
design re-mints the dependency requirement below with the exact modules and
pinned versions, and a gate test keeps `go.mod` honest against it, so the
build run can never promote a module a human has not approved.

```
devctl/                              (this sub-project; go.mod lives here)
├── cmd/devctl/main.go               thin: os.Args/stdio/real deps → cli.Run(); os.Exit
└── internal/cli/                    grammar, usage, dispatch, exit codes, version, root check
```

- **`internal/cli`** — everything between the process and the packages that
  do work: the command grammar and usage text, the exit-code taxonomy, the
  root check, the version string, and the dispatch to each command (D2).
  With one command in the tree it is also the only package; later designs
  add packages by re-minting the layout requirement, and a package is split
  here, as a design decision, when it would grow past what a reader or a
  build agent can hold whole.

  ```go
  package cli

  // Run executes the CLI. args are the program arguments without the program
  // name; all I/O flows through the injected streams and every environmental
  // dependency through deps. Run never terminates the process; it returns
  // the process exit code.
  func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int

  // Deps carries what a command cannot be deterministic about.
  type Deps struct {
      EUID int // effective user id of the process
  }
  ```

- **`cmd/devctl`** — `main` wires `context.Background()`, `os.Args[1:]`,
  `os.Stdin`, `os.Stdout`, `os.Stderr`, and `EUID: os.Geteuid()` into
  `cli.Run` and passes the result to `os.Exit`. It contains no other logic.

**The seam carries a context from the start.** devctl's work is AWS API calls
and ssh sessions, both of which take a context, and an interrupt during a
long host operation must reach them. Adding `ctx` later would re-mint `Run`
and churn every test call site; adding a field to `Deps` later does not, which
is why `Deps` holds only what this scope reads. `EUID` is the whole seam
today: it is what the root check of D2 consults, and tests pass `0` or any
other value to exercise both sides without a privileged process. Later
designs extend `Deps` with the home directory, the environment, and cloud
clients as they need them; each extension is a re-mint of the `Deps`
requirement, as opsctl's D5 extended its own.

Dependencies point one way. `cmd/devctl` imports `internal/cli` and nothing
else in this module; `internal/cli` imports nothing else in this module; and
no package imports anything outside the standard library and this module.
That last clause is what turns "no dependencies" from a hope into a test.

**Wiring proof at the binary level.** Every in-process test injects its own
streams and `Deps`, so none can prove `main` wired the real ones. One test
builds the real binary and runs it as a subprocess with `--help`, which is
exempt from the root check, so the proof runs as any user.

## REQUIREMENTS

- R-CYBP-C09U: The module MUST be `github.com/ikigenba/ikigenba/devctl` with its own `go.mod` that specifies a Go version, and that `go.mod` MUST contain no `require` entries, verified by a test that reads `go.mod`.
- R-CZJL-PS0J: Package `internal/cli` MUST export `Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.
- R-D0RI-3JR8: Package `internal/cli` MUST export a `Deps` struct whose fields are exactly `EUID int`.
- R-D1ZE-HBHX: Within this module, `cmd/devctl` MUST import only `internal/cli`; `internal/cli` MUST import no package of this module; and no package MUST import a package outside the standard library and this module, verified by a test over the packages' import lists.
- R-D37A-V38M: The binary built from `./cmd/devctl`, run with the single argument `--help`, MUST print the top-level usage text of D2 to stdout, write nothing to stderr, and exit 0.

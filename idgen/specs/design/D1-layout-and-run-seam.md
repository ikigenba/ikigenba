# D1-layout-and-run-seam

`idgen` is a small Go CLI that mints short, traceable ids of the form
`PREFIX-XXXX-XXXX` (default prefix `R`) and decodes them back to timestamps. Module path
`github.com/ikigenba/ikigenba/idgen`, Go 1.26. Three seams:

- **`internal/idgen`** — the pure core: epoch, affine bijection, base-36
  encoding, mint/decode. No I/O, no flags, no clock. Contract in D2.
- **`internal/cli`** — all flag parsing, stdin reading, stderr reporting, and
  exit codes behind one exported entry point:

  ```go
  package cli

  // Run executes the CLI: args are the program arguments (without the
  // program name), all I/O flows through the injected streams, time flows
  // through the injected Clock (D3). Run never terminates the process; it
  // returns the process exit code as an ExitCode value (D4).
  func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, clock Clock) ExitCode
  ```

- **`cmd/idgen`** — `main` wires the real values (`os.Args[1:]`, `os.Stdin`,
  `os.Stdout`, `os.Stderr`, the real clock) into `cli.Run`, converts the
  returned `ExitCode` to `int`, and passes it to `os.Exit`. Every behavior is
  testable in-process through `Run` with injected dependencies.

## REQUIREMENTS

- R-VLQP-439X: Package `internal/cli` MUST export `Run(args []string, stdin io.Reader, stdout, stderr io.Writer, clock Clock) ExitCode`, and calling it MUST return an `ExitCode` in-process without terminating the calling program.
- R-SHZS-WTMG: The binary built from `./cmd/idgen` MUST, when executed with no arguments, print exactly one line matching `^R-[0-9A-Z]{4}-[0-9A-Z]{4}$` to stdout and exit 0.

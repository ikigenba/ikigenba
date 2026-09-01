# D1-layout-and-run-seam

`idgen` is a small Go CLI that mints short, traceable ids of the form
`PREFIX-XXXX-XXXX` (default prefix `R`) and decodes them back to timestamps. Module path
`github.com/ikigenba/ikigenba/idgen`, Go 1.26. Three seams:

```
idgen/                                  (this sub-project; go.mod lives here)
├── cmd/idgen/main.go                   thin: os.Args/stdio → cli.Run(); os.Exit
├── internal/cli/                       the testable CLI core
└── internal/idgen/                     pure encode/decode core
```

- **`internal/idgen`** — the pure core: epoch, affine bijection, base-36
  encoding, mint/decode. No I/O, no flags, no clock. Contract in D2.
- **`internal/cli`** — all flag parsing, stdin reading, stderr reporting, and
  exit codes behind one exported entry point:

  ```go
  package cli

  // Run executes the CLI: args are the program arguments (without the
  // program name), all I/O flows through the injected streams, time flows
  // through the injected Clock (D3). Run never terminates the process; it
  // returns the process exit code.
  func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, clock Clock) int
  ```

- **`cmd/idgen`** — `main` wires the real values (`os.Args[1:]`, `os.Stdin`,
  `os.Stdout`, `os.Stderr`, the real clock) into `cli.Run` and passes the
  result to `os.Exit`. It contains no other logic; every behavior is testable
  in-process through `Run` with injected dependencies.

Dependencies point one way: `cmd/idgen` → `internal/cli` → `internal/idgen`.
Standard library only; no third-party modules.

## REQUIREMENTS

- R-UAKY-KVX6: The `idgen` module's import graph MUST flow one way only — `cmd/idgen` imports `internal/cli` and `internal/cli` imports `internal/idgen`, with no reverse edge: `internal/idgen` MUST NOT import `internal/cli`, and `internal/cli` MUST NOT import `cmd/idgen`.
- R-UBSU-YNNV: The `idgen` module MUST depend on the Go standard library only; its `go.mod` MUST require no third-party (non-stdlib) modules.
- R-SGRW-J1VR: Package `internal/cli` MUST export `Run(args []string, stdin io.Reader, stdout, stderr io.Writer, clock Clock) int`, and calling it MUST return an exit code in-process without terminating the calling program.
- R-SHZS-WTMG: The binary built from `./cmd/idgen` MUST, when executed with no arguments, print exactly one line matching `^R-[0-9A-Z]{4}-[0-9A-Z]{4}$` to stdout and exit 0.

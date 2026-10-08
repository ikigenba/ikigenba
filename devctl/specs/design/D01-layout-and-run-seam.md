# D01-layout-and-run-seam

The module remains a Go CLI with isolated cloud and process boundaries. The
platform is one root domain in one account: the checkout's root file (D04) is
the only configuration. All environmental dependencies enter through the run
seam; offline tests supply fakes. The seam already carries everything the
single-root platform needs: the working directory that checkout discovery
starts from, the effective uid, the environment, the cloud opener that takes
the root as the profile name, the process runners, and the clock.

## REQUIREMENTS

- R-U72C-BSYD: Package `internal/cli` MUST export `Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps seam.Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.

- R-9ZWO-GWW7: Package `internal/seam` MUST export a `Cmd` struct whose fields are exactly `Path string`, `Args []string`, `Dir string`, and `Env []string`; a `Result` struct whose fields are exactly `Stdout []byte`, `Stderr []byte`, and `ExitCode int`; the type `Runner func(ctx context.Context, cmd Cmd) (Result, error)`; and `Exec(ctx context.Context, cmd Cmd) (Result, error)`, which MUST satisfy `Runner`.

- R-A14K-UOMW: `seam.Exec` MUST report a process that exited non-zero as a `Result` carrying that exit status in `ExitCode` with a nil error, MUST return a non-nil error only when the process could not be started or `ctx` ended first, and MUST return a non-nil error without starting any process when `Cmd.Path` or `Cmd.Dir` is empty.

- R-A2CH-8GDL: `seam.Exec` MUST run the program named by `Cmd.Path`, found on `PATH`, with `Cmd.Args` as its arguments and `Cmd.Dir` as its working directory, MUST give it the environment the devctl process was started with plus `Cmd.Env`, whose entries override inherited entries of the same name, and MUST capture its standard output and standard error into separate `Result` fields.

- R-F59F-A9GW: The binary built from `./cmd/devctl`, run with the single argument `--help` as a non-root user, MUST print the top-level usage text of D2 to stdout, write nothing to stderr, and exit 0.

- R-BMSH-ORLH: Package `internal/seam` MUST export a `Deps` struct whose fields are exactly `Dir string`, `EUID int`, `Getenv func(key string) string`, `Cloud cloud.Opener`, `Exec Runner`, `Stream StreamRunner`, `Now func() time.Time`, and `After func(d time.Duration) <-chan time.Time`; a nil `Getenv` MUST read as an empty environment, a nil `Exec` MUST read as `seam.Exec`, a nil `Now` MUST read as `time.Now`, and a nil `After` MUST read as `time.After`.

- R-BO0E-2JC6: Package `internal/seam` MUST export `StreamRunner func(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error)`.

- R-BP8A-GB2V: Package `internal/seam` MUST export `Stream(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error)` implementing `StreamRunner`; a nil `Deps.Stream` MUST select this implementation.

- R-BQG6-U2TK: `seam.Stream` MUST apply the same command, directory, environment and startup-error contract as `Exec`, but deliver process stdout bytes to its writer as they arrive, without waiting for process exit, returning empty `Result.Stdout` and separately captured stderr; a writer failure MUST stop the process and return an error.

- R-BRO3-7UK9: `seam.Stream` MUST terminate and reap its process when its context is cancelled; tests MUST prove delivery before exit and cancellation completion with controlled local processes and no network access.

- R-RGP5-0NYO: Every relative path read or written by a command MUST resolve under `Deps.Dir` or the checkout root returned by `checkout.Open`. Commands MUST NOT discover or read the developer’s home directory. Every process directory MUST be explicitly supplied through `seam.Cmd.Dir`; tests MUST use temporary paths and fake process runners.

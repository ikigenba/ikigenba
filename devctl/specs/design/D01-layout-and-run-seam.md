# D01-layout-and-run-seam

The module remains a Go CLI with isolated cloud and process boundaries. Command
packages own their grammar and application behavior. Shared host setup, app-
name/version grammar, and streamed process execution have explicit boundaries so
the expanded lifecycle stays manageable. All environmental dependencies enter
through the run seam; offline tests supply fakes. Existing direct module
approvals remain unchanged.

## REQUIREMENTS

- R-DM8X-DRGU: The module MUST be `github.com/ikigenba/ikigenba/devctl` with its own `go.mod` that specifies a Go version, and the module path/version pairs of the direct (non-`// indirect`) `require` entries of that `go.mod` MUST be exactly `github.com/BurntSushi/toml v1.6.0`, `github.com/aws/aws-sdk-go-v2 v1.47.0`, `github.com/aws/aws-sdk-go-v2/config v1.33.5`, `github.com/aws/aws-sdk-go-v2/service/ec2 v1.332.0`, `github.com/aws/aws-sdk-go-v2/service/iam v1.64.0`, `github.com/aws/aws-sdk-go-v2/service/route53 v1.70.0`, `github.com/aws/aws-sdk-go-v2/service/s3 v1.113.1`, `github.com/aws/aws-sdk-go-v2/service/ssm v1.78.0`, `github.com/aws/aws-sdk-go-v2/service/sts v1.51.0`, and `github.com/aws/smithy-go v1.28.1`, verified by a test that reads `go.mod`.

- R-U72C-BSYD: Package `internal/cli` MUST export `Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, deps seam.Deps) int`, and calling it MUST return an exit code in-process without terminating the calling program.

- R-9ZWO-GWW7: Package `internal/seam` MUST export a `Cmd` struct whose fields are exactly `Path string`, `Args []string`, `Dir string`, and `Env []string`; a `Result` struct whose fields are exactly `Stdout []byte`, `Stderr []byte`, and `ExitCode int`; the type `Runner func(ctx context.Context, cmd Cmd) (Result, error)`; and `Exec(ctx context.Context, cmd Cmd) (Result, error)`, which MUST satisfy `Runner`.

- R-A14K-UOMW: `seam.Exec` MUST report a process that exited non-zero as a `Result` carrying that exit status in `ExitCode` with a nil error, MUST return a non-nil error only when the process could not be started or `ctx` ended first, and MUST return a non-nil error without starting any process when `Cmd.Path` or `Cmd.Dir` is empty.

- R-A2CH-8GDL: `seam.Exec` MUST run the program named by `Cmd.Path`, found on `PATH`, with `Cmd.Args` as its arguments and `Cmd.Dir` as its working directory, MUST give it the environment the devctl process was started with plus `Cmd.Env`, whose entries override inherited entries of the same name, and MUST capture its standard output and standard error into separate `Result` fields.

- R-A4S9-ZZUZ: Within this module, `cmd/devctl` MUST import only `internal/cli`, `internal/seam`, and `internal/cloud/awssdk`; `internal/cli` MUST be imported by no other package of this module; `internal/cloud` MUST import no other package of this module and `internal/seam` only `internal/cloud`; and no package MUST import a package outside the standard library and this module, except `internal/cloud/awssdk`, which MAY import the `github.com/aws/` modules the dependency requirement names, and `internal/checkout`, which MAY import `github.com/BurntSushi/toml`; verified by a test over the packages' import lists.

- R-A782-RJCD: `cmd/devctl` MUST consist of a `main` that builds one `seam.Deps` composite literal naming every field of `seam.Deps` and passes the result of `cli.Run` to `os.Exit`, verified by a test that parses `cmd/devctl/main.go` and compares the literal's field names with the fields of `seam.Deps` obtained by reflection.

- R-F59F-A9GW: The binary built from `./cmd/devctl`, run with the single argument `--help` as a non-root user, MUST print the top-level usage text of D2 to stdout, write nothing to stderr, and exit 0.

- R-BLKL-AZUS: The module MUST contain no package other than `cmd/devctl`, `internal/seam`, `internal/cli`, `internal/cloud`, `internal/cloud/awssdk`, `internal/account`, `internal/checkout`, `internal/keyring`, `internal/secrets`, `internal/space`, `internal/spacecreate`, `internal/host`, `internal/build`, `internal/deploy`, `internal/restore`, `internal/hostsetup`, `internal/spaceinit`, `internal/spaceapps`, `internal/remove`, and `internal/appref`, verified by a test that lists the module's packages.

- R-BMSH-ORLH: Package `internal/seam` MUST export a `Deps` struct whose fields are exactly `Dir string`, `EUID int`, `Getenv func(key string) string`, `Cloud cloud.Opener`, `Exec Runner`, `Stream StreamRunner`, `Now func() time.Time`, and `After func(d time.Duration) <-chan time.Time`; a nil `Getenv` MUST read as an empty environment, a nil `Exec` MUST read as `seam.Exec`, a nil `Now` MUST read as `time.Now`, and a nil `After` MUST read as `time.After`.

- R-BO0E-2JC6: Package `internal/seam` MUST export `StreamRunner func(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error)`.

- R-BP8A-GB2V: Package `internal/seam` MUST export `Stream(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error)` implementing `StreamRunner`; a nil `Deps.Stream` MUST select this implementation.

- R-BQG6-U2TK: `seam.Stream` MUST apply the same command, directory, environment and startup-error contract as `Exec`, but deliver process stdout bytes to its writer as they arrive, without waiting for process exit, returning empty `Result.Stdout` and separately captured stderr; a writer failure MUST stop the process and return an error.

- R-BRO3-7UK9: `seam.Stream` MUST terminate and reap its process when its context is cancelled; tests MUST prove delivery before exit and cancellation completion with controlled local processes and no network access.

- R-BU3V-ZE1N: Every relative path read or written by a command MUST resolve under `Deps.Dir` or the checkout root returned by `checkout.Open`; an explicit absolute deploy file operand MUST be read at that path. Commands MUST NOT discover or read the developer’s home directory. Every process directory MUST be explicitly supplied through `seam.Cmd.Dir`; tests MUST use temporary paths and fake process runners.

- R-BVBS-D5SC: No package of this module other than `cmd/devctl` MUST reference `os.Getenv`, `os.Getwd`, `os.UserHomeDir`, or `os.Geteuid`; no package other than `cmd/devctl` and `internal/seam` MUST reference `os.Environ`; no package other than `cmd/devctl` and `internal/seam` MUST reference `time.Now`, `time.Since`, `time.Sleep`, `time.After`, or `time.Tick`; and no package other than `internal/seam` MUST import `os/exec`; verified by a test over the packages' syntax trees.

- R-BWJO-QXJ1: `cmd/devctl` MUST supply `seam.Exec`, `seam.Stream`, `awssdk.Open`, `os.Getwd`, `os.Geteuid`, `os.Getenv`, `time.Now`, and `time.After` as the corresponding real dependencies, wire the process streams and arguments to `cli.Run`, and cancel its run context on an interrupt so a following log command can exit.

- R-YHTO-8IAI: `internal/hostsetup` MUST own release discovery, opsctl installation and configuration; `internal/spaceinit` MUST own `space init`; `internal/spaceapps` MUST own `space restart` and `space logs`; `internal/remove` MUST own `remove`; `internal/appref` MUST own usable app names and version/file-name grammar. The shared helper packages `internal/hostsetup` and `internal/appref` MUST NOT import command packages or `internal/cli`; command packages MAY depend on those helpers and on the shared space operations.

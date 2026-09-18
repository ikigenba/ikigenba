# D01-layout-and-run-seam

dummy is one Go binary that serves one page. This design is the structural
ground the other three designs stand on: where the code lives, which package
exports which name, how the version and the manifest are declared, and the
seam through which the process is run so that the CLI and the server can be
tested without a real process.

The module is `github.com/ikigenba/ikigenba/dummy`, rooted at the sub-project
directory, with `go.mod` beside `specs/`. Its `main` package sits at
`cmd/dummy` because `devctl build` finds an app as a `package main` in
`cmd/<app>` under the directory that holds `etc/manifest.toml`, like every
other binary in the repository; a `package main` at the root would not be
found. The `cmd/dummy` package is wiring only. Two internal packages hold the
concerns: `internal/cli` owns the
program as a command (the version, the manifest text, the exit codes, the run
seam, and the argument and `PORT` handling that `D02-cli` specifies), and
`internal/server` owns the listener's life and the HTTP handler (`D03-serve`
and `D04-pages`). Dependencies point one way: `cmd/dummy` imports
`internal/cli`, which imports `internal/server`; `internal/server` knows
nothing of the CLI.
The standard library is enough; the module requires no other module.

The version is a `var` in `internal/cli`, initialised in its declaration and
never injected by the linker, so a developer's `go build` and a release build
report the same string. Its value is data: a requirement fixes the name, that
it is a `var`, and its shape, and nothing else. The manifest is a constant in
the same package holding exactly the four lines the bootstrap story shows,
with a trailing newline. The committed `etc/manifest.toml` is a copy of that
constant kept so the checkout can be read without a build, and the two are
byte-identical. The package story lists exactly two members in the release
file, which holds only if the checkout keeps nothing else under `etc/` and has
no `share/`; that is stated as an invariant of the checkout.

The run seam is `cli.Run`. It takes a context and a `cli.Process` value that
carries everything the program would otherwise take from the `os` package:
the arguments without the program name, an environment lookup, and the two
output streams. Its return value is the process exit code. `main` fills the
`Process` from the real process and cancels the context on `SIGTERM` or
`SIGINT`; a test fills it with buffers and a map and cancels the context
itself. Nothing below `main` reads `os.Args`, the real environment, or the
real streams, or installs a signal handler, so a test that drives `Run` sees
the whole program's behaviour and nothing leaks past it. That is stated over
the non-test files of the two packages; their tests may name the real streams
freely. The one writer below the seam that could reach the real standard
error on its own, the HTTP server's diagnostics that `net/http` would send
through the `log` package, is silenced by `D03-serve`: `Serve` discards the
server's own diagnostics, so the `log` package needs no ban here. `Process`
also carries an optional `Listening` callback that `Run` invokes once the
listener is bound, which is how a serve test learns the server is up without
sleeping, and an optional `Listen` factory that stands in for `net.Listen`,
so a test can hand `Run` a factory that fails, or a listener whose `Accept`
fails, and exercise the bind-failure and server-failure paths without
contending for a real port. `main` leaves both nil: the real binary binds
with `net.Listen` and has no readiness hook.

Below the seam, `internal/cli` binds `127.0.0.1:$PORT` itself and hands the
bound listener to `server.Serve` along with the handler. `Serve` owns the
listener from then on and returns when the context is cancelled or the server
fails; what it does between those points, and the handler it serves, are
`D03-serve` and `D04-pages`.

## REQUIREMENTS

- R-DJO3-6OQP: The Go module MUST be `github.com/ikigenba/ikigenba/dummy` with its `go.mod` at the sub-project root, and MUST consist of exactly three packages: `package main` at `cmd/dummy`, `internal/cli`, and `internal/server`, where `cmd/dummy` imports `internal/cli`, `internal/cli` imports `internal/server`, and `internal/server` does not import `internal/cli`.
- R-AK3S-P0DU: The module MUST require no other module; `go.mod` MUST contain no `require` directive.
- R-DM3V-Y883: The `package main` at `cmd/dummy` MUST run `cli.Run` with `Process.Args` set to the process arguments after the program name, `LookupEnv` reading the process environment, `Stdout` and `Stderr` the process's standard output and standard error, and a context that is cancelled when the process receives `SIGTERM` or `SIGINT`, and MUST exit the process with the value `Run` returned.
- R-AMJL-GJV8: The `internal/cli` package MUST export `var Version string`.
- R-ANRH-UBLX: `Version` MUST be the letter `v` followed by a valid Semantic Versioning version as defined at semver.org, prerelease and build metadata included when present.
- R-AOZE-83CM: `Version` MUST be set by the initializer of its declaration in source, so that a binary produced by `go build` with no linker flags reports the same `Version` the source declares.
- R-ARF6-ZMU0: The `internal/cli` package MUST export `const Manifest = "app = \"dummy\"\nport = 3000\ndefault = false\nsecrets = []\n"`.
- R-ASN3-DEKP: The committed file `etc/manifest.toml` at the sub-project root MUST be byte-identical to `Manifest`.
- R-ATUZ-R6BE: The sub-project's `etc/` directory MUST contain exactly one entry, `manifest.toml`, and the sub-project MUST have no `share/` directory.
- R-STSK-D18S: The `internal/cli` package MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Stdout io.Writer; Stderr io.Writer; Listen func(network, address string) (net.Listener, error); Listening func(addr net.Addr) }` and `func Run(ctx context.Context, p Process) int`, where `Args` excludes the program name and `Run` uses `net.Listen` when `Listen` is nil.
- R-AWAS-IPSS: The `internal/cli` package MUST export the constants `ExitSuccess = 0`, `ExitServerFailed = 1`, and `ExitUsage = 2`.
- R-AXIO-WHJH: `Run` MUST return one of `ExitSuccess`, `ExitServerFailed`, or `ExitUsage`, and no other value.
- R-N4IA-6LSH: The non-test `.go` files of `internal/cli` and `internal/server` MUST NOT reference `os.Args`, `os.Environ`, `os.Getenv`, `os.LookupEnv`, `os.Stdin`, `os.Stdout`, `os.Stderr`, or `os.Exit`, and MUST NOT import `os/signal`.
- R-TN2J-IWT8: When `Run` binds a listener it MUST call `Listening` exactly once, with the bound listener's address, after the listener is bound; `Run` MUST NOT call `Listening` when it is nil or when `Run` returns without binding a listener.
- R-B16E-1SRK: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler) error`.

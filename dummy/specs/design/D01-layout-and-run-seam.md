# D01-layout-and-run-seam

dummy is one Go binary that serves a small control panel. This design is the
structural ground the other six designs stand on: where the code lives, which
package exports which name, how the version, the manifest and the usage text
are declared, and the seam through which the process is run so that the
command, the server and the panel can be tested without a real process.

The module is `github.com/ikigenba/ikigenba/dummy`, rooted at the sub-project
directory, with `go.mod` beside `specs/`. Its `main` package sits at
`cmd/dummy` because `devctl build` finds an app as a `package main` in
`cmd/<app>` under the directory that holds `etc/manifest.toml`, like every
other binary in the repository; a `package main` at the root would not be
found. The `cmd/dummy` package is wiring only. The standard library is enough;
the module requires no other module.

Four internal packages hold the concerns, each one concern and each small
enough for a reader to hold whole. `internal/cli` owns the program as a
command: the version, the manifest text, the usage text, the exit codes, the
run seam, the argument and `PORT` handling that `D02-cli` specifies, and the
construction of the process's widget store and handler before the bound
listener is handed on. `internal/server` owns the listener's life and nothing
else — it is handed a context, a bound listener and a handler, and `D03-serve`
says what it does with them; it knows nothing of widgets, routes, identity or
HTML. `internal/panel` owns dummy's whole HTTP surface: the one handler, the
identity precondition, routing, the chrome, rendering, the two failure shapes,
the table fragment and the form (`D04-panel`, `D06-table`, `D07-form`).
`internal/widget` owns the domain: the widget entity, the status enumeration,
the in-memory store, and the validation of a submission (`D05-widgets`).

Dependencies point one way, and the layout requirement fixes the whole
direction rather than one edge of it. `cmd/dummy` imports `internal/cli`;
`internal/cli` imports `internal/server`, `internal/panel` and
`internal/widget`; `internal/panel` imports `internal/widget`.
`internal/server` and `internal/widget` import nothing of dummy's, and nothing
imports `internal/cli` but `cmd/dummy`. There is no cycle to break, and the
two packages a test most wants to drive on their own — the listener's life and
the domain — are the two that depend on nothing.

This layout replaces the three packages the first draft of this design
settled, and the reasons for the new lines are worth keeping. The handler left
`internal/server` because it is no longer a pure function of method and path:
it reads request headers, renders several page shapes and a fragment, and
holds a store, so left where it was it would be that package's whole mass,
under a name that means transport. `internal/server` is nonetheless kept as a
package holding only the serve call, because a listener's life — the graceful
drain, the silenced server diagnostics, returning non-nil only on a real
failure — is a separate concern, separately testable with a fake listener and
a handler that panics. `internal/widget` is separate from `internal/panel`
because the rules that accept or reject a submission are decidable without
HTTP and are the part of dummy most worth testing on its own. An interface
between the two was considered and rejected: there is one store and there will
be one, so an interface would be a name with no second member.

The version is a `var` in `internal/cli`, initialised in its declaration and
never injected by the linker, so a developer's `go build` and a release build
report the same string. Its value is data: a requirement fixes the name, that
it is a `var`, and its shape, and nothing else. The manifest is a constant in
the same package holding exactly the four lines the bootstrap story shows,
with a trailing newline. The committed `etc/manifest.toml` is a copy of that
constant kept so the checkout can be read without a build, and the two are
byte-identical. The usage text is a constant in that package too, declared
here so that every name `internal/cli` exports is declared in one place; its
value belongs to `D02-cli`, because the help output is that design's subject,
and it is fixed there byte for byte. The package story lists exactly two
members in the release file, which holds only if the checkout keeps nothing
else under `etc/` and has no `share/`; that is stated as an invariant of the
checkout, and it is also what obliges `internal/panel` to carry its templates
inside the binary rather than beside it.

The run seam is `cli.Run`. It takes a context and a `cli.Process` value that
carries everything the program would otherwise take from the `os` package:
the arguments without the program name, an environment lookup, and the two
output streams. Its return value is the process exit code. `main` fills the
`Process` from the real process and cancels the context on `SIGTERM` or
`SIGINT`; a test fills it with buffers and a map and cancels the context
itself. Nothing below `main` reads `os.Args`, the real environment, or the
real streams, or installs a signal handler, so a test that drives `Run` sees
the whole program's behaviour and nothing leaks past it. That is stated over
the non-test files of all four internal packages — the rule widened with the
layout, because a handler or a domain type can reach the real process exactly
as easily as a command can — and their tests may name the real streams
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

`Process` carries no store. The widget set is neither an argument, an
environment value nor a stream, so it is not part of the process seam; it
comes into being below it. Below the seam, `internal/cli` binds
`127.0.0.1:$PORT` itself and, when the bind succeeds, makes the process's one
widget store and hands the bound listener to `server.Serve` together with the
handler `internal/panel` builds over that store. Making the store there, and
after the bind, puts "the widget set is created once, at process start, and
dies with the process" at the one place that happens, keeps a bind that fails
from building anything, and keeps `--version` from building a store it will
never use; it also lets a test build a handler over a store it has already
filled. `Serve` owns the listener from then on and returns when the context is
cancelled or the server fails. What it does between those points is
`D03-serve`, what the handler answers is `D04-panel` and the designs it leads
to, and the hand-off itself — bind, store, serve — is `D02-cli`.

## REQUIREMENTS

- R-0Y17-99C9: The Go module MUST be `github.com/ikigenba/ikigenba/dummy` with its `go.mod` at the sub-project root, and MUST consist of exactly five packages: `package main` at `cmd/dummy`, `internal/cli`, `internal/server`, `internal/panel`, and `internal/widget`; the imports of one of those packages by another MUST be exactly `cmd/dummy` importing `internal/cli`, `internal/cli` importing each of `internal/server`, `internal/panel`, and `internal/widget`, and `internal/panel` importing `internal/widget`, so that `internal/server` and `internal/widget` import no package of this module and nothing but `cmd/dummy` imports `internal/cli`.
- R-AK3S-P0DU: The module MUST require no other module; `go.mod` MUST contain no `require` directive.
- R-DM3V-Y883: The `package main` at `cmd/dummy` MUST run `cli.Run` with `Process.Args` set to the process arguments after the program name, `LookupEnv` reading the process environment, `Stdout` and `Stderr` the process's standard output and standard error, and a context that is cancelled when the process receives `SIGTERM` or `SIGINT`, and MUST exit the process with the value `Run` returned.
- R-AMJL-GJV8: The `internal/cli` package MUST export `var Version string`.
- R-ANRH-UBLX: `Version` MUST be the letter `v` followed by a valid Semantic Versioning version as defined at semver.org, prerelease and build metadata included when present.
- R-AOZE-83CM: `Version` MUST be set by the initializer of its declaration in source, so that a binary produced by `go build` with no linker flags reports the same `Version` the source declares.
- R-ARF6-ZMU0: The `internal/cli` package MUST export `const Manifest = "app = \"dummy\"\nport = 3000\ndefault = false\nsecrets = []\n"`.
- R-ASN3-DEKP: The committed file `etc/manifest.toml` at the sub-project root MUST be byte-identical to `Manifest`.
- R-ATUZ-R6BE: The sub-project's `etc/` directory MUST contain exactly one entry, `manifest.toml`, and the sub-project MUST have no `share/` directory.
- R-10H0-0STN: The `internal/cli` package MUST export `Usage` as a string constant, holding the usage text whose value `D02-cli` fixes.
- R-STSK-D18S: The `internal/cli` package MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Stdout io.Writer; Stderr io.Writer; Listen func(network, address string) (net.Listener, error); Listening func(addr net.Addr) }` and `func Run(ctx context.Context, p Process) int`, where `Args` excludes the program name and `Run` uses `net.Listen` when `Listen` is nil.
- R-AWAS-IPSS: The `internal/cli` package MUST export the constants `ExitSuccess = 0`, `ExitServerFailed = 1`, and `ExitUsage = 2`.
- R-AXIO-WHJH: `Run` MUST return one of `ExitSuccess`, `ExitServerFailed`, or `ExitUsage`, and no other value.
- R-0Z93-N12Y: The non-test `.go` files of `internal/cli`, `internal/server`, `internal/panel`, and `internal/widget` MUST NOT reference `os.Args`, `os.Environ`, `os.Getenv`, `os.LookupEnv`, `os.Stdin`, `os.Stdout`, `os.Stderr`, or `os.Exit`, and MUST NOT import `os/signal`.
- R-TN2J-IWT8: When `Run` binds a listener it MUST call `Listening` exactly once, with the bound listener's address, after the listener is bound; `Run` MUST NOT call `Listening` when it is nil or when `Run` returns without binding a listener.
- R-B16E-1SRK: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler) error`.

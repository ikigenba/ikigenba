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
run seam, the argument handling that `D02-cli` specifies, and the serve path
that `D03-serve` specifies — reading the drain deadline, taking the socket the
host passes in, building the process's widget store and handler, handing them
on, and telling systemd it is ready. `internal/server` owns the listener's
life and nothing else — it is handed a context, a listener, a handler and a
drain deadline, and `D03-serve` says what it does with them; it knows nothing
of widgets, routes, identity, HTML, sockets passed in or systemd.
`internal/panel` owns dummy's whole HTTP surface: the one handler, the
identity precondition and the line it writes when that fails, routing, the
chrome, rendering, the two failure shapes, the table fragment and the form
(`D04-panel`, `D06-table`, `D07-form`). `internal/widget` owns the domain: the
widget entity, the status enumeration, the in-memory store, and the validation
of a submission (`D05-widgets`).

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
the same package holding exactly the three lines the bootstrap story shows,
with a trailing newline; it declares no port, because dummy serves on the
socket the host passes it and a manifest carrying `port` is refused by
`devctl build` and by opsctl. The committed `etc/manifest.toml` is a copy of
that constant kept so the checkout can be read without a build, and the two
are byte-identical. The usage text is a constant in that package too, declared
here so that every name `internal/cli` exports is declared in one place; its
value belongs to `D02-cli`, because the help output is that design's subject,
and it is fixed there byte for byte. The package story lists exactly two
members in the release file, which holds only if the checkout keeps nothing
else under `etc/` and has no `share/`; that is stated as an invariant of the
checkout, and it is also what obliges `internal/panel` to carry its templates
inside the binary rather than beside it.

The run seam is `cli.Run`. It takes a context and a `cli.Process` value that
carries everything the program would otherwise take from the `os` package:
the arguments without the program name, an environment lookup, a way to
remove a variable from the environment, the process's own id, the two output
streams, and a way to turn an inherited file descriptor into a listener. Its
return value is the process exit code. `main` fills the `Process` from the
real process and cancels the context on `SIGTERM` or `SIGINT`; a test fills it
with buffers, a map, a pid of its choosing and a listener it made itself, and
cancels the context itself. Nothing below `main` reads `os.Args`, the real
environment, the real pid or the real streams, changes the real environment,
or installs a signal handler, so a test that drives `Run` sees the whole
program's behaviour and nothing leaks past it. That is stated over the
non-test files of all four internal packages — a handler or a domain type can
reach the real process exactly as easily as a command can — and their tests
may name the real streams freely. The one writer below the seam that could
reach the real standard error on its own, the HTTP server's diagnostics that
`net/http` would send through the `log` package, is silenced by `D03-serve`:
`Serve` discards the server's own diagnostics, so the `log` package needs no
ban here.

Socket activation hands the process a listening socket as file descriptor 3,
and the one thing below the seam that touches a real descriptor is turning
that number into a `net.Listener`. `Process.Inherit` is that step: `main`
leaves it nil, and `Run` then makes the listener from the real descriptor; a
test supplies a function that returns a listener it bound itself, on a
loopback port the kernel chose or a Unix socket in a temporary directory, so
the inherited-socket path runs in process without systemd and without a real
descriptor 3. The environment half of the protocol — `LISTEN_PID`,
`LISTEN_FDS`, and `NOTIFY_SOCKET` — arrives through `LookupEnv` and is checked
against `Process.Pid`, and removing the `LISTEN_*` variables goes through
`Process.Unsetenv`, which a test may leave nil or replace with a recorder.
Readiness needs no hook of its own: `Run` sends `READY=1` to the datagram
socket `NOTIFY_SOCKET` names, and a test that wants to know the server is up
binds such a socket in a temporary directory, names it in the environment map
it hands `Run`, and waits for the datagram. That is the same signal systemd
waits for, so an in-process test and the host learn readiness the same way.
There is no listen factory and no readiness callback in the seam: dummy binds
nothing, so there is no bind to fake, and the datagram is the readiness
signal. dummy opens no listening socket anywhere in its code, which is how
"dummy listens on no other socket and no port" is kept.

`Process` carries no store. The widget set is neither an argument, an
environment value nor a stream, so it is not part of the process seam; it
comes into being below it. Below the seam, `internal/cli` takes the socket
and, when that succeeds, makes the process's one widget store and hands the
listener to `server.Serve` together with the handler `internal/panel` builds
over that store and the drain deadline. Making the store there, and after the
socket is taken, puts "the widget set is created once, at process start, and
dies with the process" at the one place that happens, keeps a start that
fails from building anything, and keeps `--version` from building a store it
will never use; it also lets a test build a handler over a store it has
already filled. The handler is also handed the writer its per-request
diagnostic goes to, because a 5xx is the one answer that is trouble and the
handler is the one that knows which request it was. `Serve` owns the listener
from then on and returns when the drain after the context is cancelled has
ended or the server fails; when the drain deadline passes with requests still
running it returns a `server.DrainError` counting them, which `Run` reports
like any other serve failure. What `Serve` does between those points is
`D03-serve`, what the handler answers is `D04-panel` and the designs it leads
to, and the hand-off itself — drain deadline, socket, store, readiness, serve
— is `D03-serve` too.

## REQUIREMENTS

- R-0Y17-99C9: The Go module MUST be `github.com/ikigenba/ikigenba/dummy` with its `go.mod` at the sub-project root, and MUST consist of exactly five packages: `package main` at `cmd/dummy`, `internal/cli`, `internal/server`, `internal/panel`, and `internal/widget`; the imports of one of those packages by another MUST be exactly `cmd/dummy` importing `internal/cli`, `internal/cli` importing each of `internal/server`, `internal/panel`, and `internal/widget`, and `internal/panel` importing `internal/widget`, so that `internal/server` and `internal/widget` import no package of this module and nothing but `cmd/dummy` imports `internal/cli`.
- R-AK3S-P0DU: The module MUST require no other module; `go.mod` MUST contain no `require` directive.
- R-LGSH-HS2Z: The `package main` at `cmd/dummy` MUST run `cli.Run` with `Process.Args` set to the process arguments after the program name, `LookupEnv` reading the process environment, `Unsetenv` removing a variable from the process environment, `Pid` the process's own id, `Stdout` and `Stderr` the process's standard output and standard error, `Inherit` nil, and a context that is cancelled when the process receives `SIGTERM` or `SIGINT`, and MUST exit the process with the value `Run` returned.
- R-AMJL-GJV8: The `internal/cli` package MUST export `var Version string`.
- R-ANRH-UBLX: `Version` MUST be the letter `v` followed by a valid Semantic Versioning version as defined at semver.org, prerelease and build metadata included when present.
- R-AOZE-83CM: `Version` MUST be set by the initializer of its declaration in source, so that a binary produced by `go build` with no linker flags reports the same `Version` the source declares.
- R-LI0D-VJTO: The `internal/cli` package MUST export `const Manifest = "app = \"dummy\"\ndefault = false\nsecrets = []\n"`.
- R-ASN3-DEKP: The committed file `etc/manifest.toml` at the sub-project root MUST be byte-identical to `Manifest`.
- R-ATUZ-R6BE: The sub-project's `etc/` directory MUST contain exactly one entry, `manifest.toml`, and the sub-project MUST have no `share/` directory.
- R-10H0-0STN: The `internal/cli` package MUST export `Usage` as a string constant, holding the usage text whose value `D02-cli` fixes.
- R-LJ8A-9BKD: The `internal/cli` package MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Unsetenv func(key string) error; Pid int; Stdout io.Writer; Stderr io.Writer; Inherit func(fd uintptr) (net.Listener, error) }` and `func Run(ctx context.Context, p Process) int`, where `Args` excludes the program name, `Pid` is the id of the process `Run` speaks for, and a nil `Unsetenv` means `Run` removes no variable.
- R-092E-Q4T0: The `internal/cli` package MUST export `ExitSuccess`, `ExitServerFailed` and `ExitUsage` as constants, so that each of the three names is usable as an operand of a constant expression — the initializer of a `const` declaration in a package that imports `internal/cli` included — and their constant values MUST be 0, 1 and 2 respectively.
- R-AXIO-WHJH: `Run` MUST return one of `ExitSuccess`, `ExitServerFailed`, or `ExitUsage`, and no other value.
- R-LKG6-N3B2: The non-test `.go` files of `internal/cli`, `internal/server`, `internal/panel`, and `internal/widget` MUST NOT reference `os.Args`, `os.Environ`, `os.Getenv`, `os.LookupEnv`, `os.Setenv`, `os.Unsetenv`, `os.Clearenv`, `os.Getpid`, `os.Stdin`, `os.Stdout`, `os.Stderr`, or `os.Exit`, and MUST NOT import `os/signal`.
- R-LLO3-0V1R: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error`.
- R-LMVZ-EMSG: The `internal/server` package MUST export `type DrainError struct { Unfinished int }` and `func (e *DrainError) Error() string`.
- R-WBD3-T7XX: The non-test `.go` files of the module MUST NOT call `net.Listen`, `net.ListenTCP`, `net.ListenUnix`, `net.ListenUDP`, `net.ListenUnixgram`, `net.ListenIP`, `net.ListenMulticastUDP`, `net.ListenPacket`, the `Listen` or `ListenPacket` method of a `net.ListenConfig`, `http.ListenAndServe`, `http.ListenAndServeTLS`, the `ListenAndServe` or `ListenAndServeTLS` method of an `http.Server`, `syscall.Socket`, `syscall.Bind`, or `syscall.Listen`, so that the only listening socket dummy serves on is the one it is passed.

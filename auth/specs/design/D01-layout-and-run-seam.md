# D01-layout-and-run-seam

auth is one Go binary that serves the platform's auth service. This document
fixes the skeleton the rest of the auth design hangs on: the module path, the
package set and what each package owns, the one-way import direction, the one
platform library it depends on, the hand-authored files the checkout carries,
and the run seam through which the program touches the outside world. It designs no
endpoint, no config validation, no OAuth flow, and no persistence behavior —
only where those things live and how they are wired and tested. The later
design documents attach their contracts to the packages this document names:
`internal/server` owns the HTTP handlers and router and the listener's life
(D03), `internal/store` owns persistence (D04), `internal/google` owns the
Google/OIDC client (D05), and `internal/idcodec` owns the opaque-id and secret
encoding (D04).

## The one platform dependency

auth depends on `github.com/ikigenba/ikigenba/appkit`, the platform's shared
banner kit, beside its Google and SQLite libraries. It is fetched through the
ordinary Go module proxy like any published module, with no `replace`
directive pointing into the repository, and which release is data: it lives
in `go.mod`, and no requirement names it. appkit is a sibling sub-project, so
auth reaches it only through its published package, whose exported surface is
`StaticPrefix = "/_appkit/"`, `Static() http.Handler`, `Templates()
*template.Template`, `New(service string) *Kit`, `(*Kit).Banner(User)
Banner`, and the plain data types `User`, `Banner` and `Service`. appkit owns
the banner's markup, the launcher's markup and script, the stylesheet, the
fonts and their licences; auth authors none of them and carries no copy.

There is no package at the module root any more. It existed only to embed the
hand-maintained `assets/` directory, which a package under `internal/` could
not reach; the style files now come from appkit, which embeds and serves them
itself (D08), so auth embeds no style file and has nothing for a root package
to do. The pages auth draws are still its own templates in `internal/server`
(D05, D07), drawn around appkit's banner templates.

`appkit.New` is the one place the kit touches the process: its documentation
says "New captures the host services path for the named app", and it reads
`IKIGENBA_SERVICES` from the real environment when it is called and offers no
form that takes the path from a caller. Each call to `(*Kit).Banner` then
reads that file afresh and returns no services when the path is empty or the
file is missing, unreadable or malformed; it never writes anything or returns
an error. Reading the real environment is exactly what the run seam keeps out
of everything below `main`, so `appkit.New("auth")` is called in `main` and
nowhere else, once, at start: that is the one read of `IKIGENBA_SERVICES` the
serve story describes, and since `New` cannot fail, the variable can never
stop auth starting. auth's own code never reads the variable.

What crosses the seam is not the kit but a function, the banner source,
`func(appkit.User) appkit.Banner`. `main` passes the `Banner` method of the
kit it made; a test passes a closure of its own that returns whatever banner
data the case needs, a launcher's services included, without a services file
and without touching the environment. `internal/cli` hands the source on,
unchanged, to `internal/server` through `server.Config` (D03) and never calls
it itself. So appkit is imported by exactly three packages: `cmd/auth`, to
build the kit; `internal/cli`, whose `Process` names appkit's types; and
`internal/server`, which draws the banner and serves the shared files.

The checkout carries two hand-authored inputs beside the code. `etc/` holds
only `manifest.toml` (D02), and `share/` holds only `icon.svg`, auth's icon,
an SVG image a human draws from Tabler's outline `fingerprint` icon; its
presence in the package is what lists auth in the platform's launcher on a
space. The build run never writes it; the design fixes only that it is there
and that it is an SVG document, because that is all the package story fixes.
Everything auth serves is inside the binary — its own templates and appkit's
embedded files alike.

## The run seam

The platform's apps share a run seam so their gates run offline and
deterministically; dummy, the platform's reference app, set its shape, and
auth follows it. A `cli` package exposes `Run` plus a `Process` value that
carries everything the program would otherwise take from the `os` package:
the arguments without the program name, an environment lookup, a way to
remove a variable from the environment, the process's own id, the two output
streams, and a way to turn an inherited file descriptor into a listener. `Run`
also takes a context, and its return value is the process exit code. `main`
in `cmd/auth` is thin wiring — it builds the appkit kit, fills the `Process`
from the real process, cancels the context on `SIGTERM` or `SIGINT`, calls
`Run`, and exits with what it returned. A test fills the `Process` with
buffers, a map, a pid of its choosing, a listener it made itself and a banner
source of its own, and cancels the context itself; `Run`'s behaviour when it
serves with a nil `Banner` is not contract, so a test that serves supplies
one. Nothing below `main` reads `os.Args`, the real environment, the real pid
or the real streams, changes the real environment, calls `appkit.New`, or
installs a signal handler,
so a test that drives `Run` sees the whole program's behaviour and nothing
leaks past it. auth needs more injected than a plain app, because it reads the
clock, mints random ids and secrets, talks to Google, and opens a database; so
`Process` also carries a clock, a randomness source, the Google OIDC issuer
location (so a loopback fake stands in for Google offline), and the database
source (so a temporary or in-memory database stands in for `state/auth.db`),
and, last, the banner source described above.

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
There is no listen factory and no readiness callback in the seam: auth binds
nothing, so there is no bind to fake, and the datagram is the readiness
signal. auth opens no listening socket anywhere in its code, which is how
"auth listens on no other socket and no port" is kept.

`internal/server` keeps the handlers and gains the listener's life. `*Server`
is the handler — it implements `http.Handler` — and `server.Serve` is handed a
context, a listener, a handler and a drain deadline, and returns when the
drain after the context is cancelled has ended or the server fails; when the
drain deadline passes with requests still running it returns a
`server.DrainError` counting them. The two are exported side by side, in the
same shape dummy gives them, so a test can drive the listener's life with a
fake listener and a handler that blocks, without a store or Google. Splitting
the listener's life into its own package, as dummy does, was considered and
rejected: in dummy the split moved the handler out of a package named for
transport, while auth's handlers already sit in `internal/server` and every
later design names them there, so the split would rename the handler package
for no gain in clarity. What `Serve` does, and the hand-off from `Run` — the
Google settings, the drain deadline, the socket, the store, readiness, serve —
is `D03-serve`.

auth keeps its exit codes as the plain numbers `0`, `1` and `2` that its
contract has always fixed, where dummy names them `ExitSuccess`,
`ExitServerFailed` and `ExitUsage`: nothing outside `cli` would use the names,
so adding them would re-mint every requirement that states a code for no
change in behaviour.

The version is a value, not a build artifact: `internal/version` holds it as a
plain source-level `var` of shape `v<semver>`, so a developer's build and a
deployed binary report the same string and nothing is injected at link time.

## REQUIREMENTS

- R-3FKW-RNJY: The Go module path MUST be `github.com/ikigenba/ikigenba/auth`.
- R-SHLR-BYZ5: Package `main` in directory `cmd/auth` (file `cmd/auth/main.go`) MUST own `func main()` and MUST contain only wiring — calling `appkit.New`, building a `cli.Process`, making a context cancelled on `SIGTERM` or `SIGINT`, calling `cli.Run`, and exiting with what it returned — with no other logic.
- R-3I0P-J71C: Package `internal/cli` MUST own the run seam: it owns the `Process` type and the `Run` function and nothing that belongs to another package's concern.
- R-3J8L-WYS1: Package `internal/server` MUST own the auth service's HTTP router and all of its HTTP handlers; every exported HTTP-handling name in the design lives here.
- R-3KGI-AQIQ: Package `internal/store` MUST own persistence: the domain entity types, the store handle, and every operation that reads or writes the database.
- R-3LOE-OI9F: Package `internal/google` MUST own the Google/OIDC client: every exported name that starts a Google sign-in, exchanges a code, or verifies an ID token lives here.
- R-3MWB-2A04: Package `internal/idcodec` MUST own opaque-id and secret encoding: the Crockford base32 encoder, the id and secret minters, and the secret hash.
- R-3O47-G1QT: Package `internal/version` MUST own the release version value and export it as `Version`.
- R-SITN-PQPU: `internal/cli` MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Unsetenv func(key string) error; Pid int; Stdout io.Writer; Stderr io.Writer; Inherit func(fd uintptr) (net.Listener, error); Now func() time.Time; Rand io.Reader; OIDCIssuer string; DBSource string; Banner func(u appkit.User) appkit.Banner }`, with exactly those fields in that order, where `appkit` is the package `github.com/ikigenba/ikigenba/appkit`, `Args` excludes the program name, `Pid` is the id of the process `Run` speaks for, a nil `Unsetenv` means `Run` removes no variable, and `Banner` is the banner source from which every page auth draws with the banner is drawn.
- R-LUR4-A3IV: `internal/cli` MUST export `func Run(ctx context.Context, p Process) int`.
- R-3SZS-Z4PL: `internal/version` MUST export `var Version string`, initialized in source to a string literal matching the shape `v<semver>` (a leading `v` followed by a semantic version), and its value MUST NOT depend on linker flags — a plain build with no `-ldflags` MUST yield that same literal value.
- R-SK1K-3IGJ: The Go module MUST consist of exactly seven packages, with no package at the module root — `package main` at `cmd/auth`, `internal/cli`, `internal/server`, `internal/store`, `internal/google`, `internal/idcodec`, and `internal/version` — so that no directory of the module other than those seven, the module root among them, holds a `.go` file.
- R-SL9G-HA78: The imports written in the non-test `.go` files of the module's packages MUST form a one-way graph in which `cmd/auth` imports `internal/cli`; `internal/cli` may import `internal/server`, `internal/store`, `internal/google`, `internal/idcodec`, and `internal/version`; `internal/server` may import `internal/store`, `internal/google`, `internal/idcodec`, and `internal/version`; `internal/store` and `internal/google` may import `internal/idcodec` and `internal/version`; `internal/idcodec` and `internal/version` MUST import no package of this module; and no `internal/*` package MUST import `internal/cli`.
- R-SMHC-V1XX: The package `github.com/ikigenba/ikigenba/appkit` MUST be imported by the non-test `.go` files of `cmd/auth`, `internal/cli`, and `internal/server`, and by the non-test `.go` files of no other package of this module.
- R-SOX5-MLFB: `go.mod` MUST require the module `github.com/ikigenba/ikigenba/appkit` and MUST contain no `replace` directive.
- R-SQ52-0D60: `main` MUST call `appkit.New("auth")` exactly once, before it calls `cli.Run`, and MUST run `cli.Run` with `Process.Args` set to the process arguments after the program name, `LookupEnv` reading the process environment, `Unsetenv` removing a variable from the process environment, `Pid` the process's own id, `Stdout` and `Stderr` the process's standard output and standard error, `Inherit` nil, `Now` the system wall clock, `Rand` a cryptographically secure random source, `OIDCIssuer` the production Google OIDC issuer location, `DBSource` the database path `state/auth.db`, and `Banner` the `Banner` method of the `*appkit.Kit` that call to `appkit.New` returned, and with a context that is cancelled when the process receives `SIGTERM` or `SIGINT`.
- R-3WNI-4FXO: `main` MUST terminate the process with the exact integer that `cli.Run` returns as the process exit status.
- R-3XVE-I7OD: `cli.Run` MUST return `0` on success, `1` when the server fails, and `2` on a usage error.
- R-SRCY-E4WP: Given a `Process` whose `Stdout` and `Stderr` are in-memory buffers, `Args` an explicit slice, `LookupEnv` a fake lookup, `Unsetenv` nil or a recorder, `Pid` a value the test chose, `Inherit` a function returning a listener the test made, `Now` a fixed clock, `Rand` a deterministic reader, `OIDCIssuer` a loopback URL, `DBSource` a temporary database, and `Banner` a function the test wrote, `cli.Run` MUST take every input and produce every output through that `Process` — reading arguments only from `Args`, environment only through `LookupEnv`, every time it records or compares against stored state only through `Now`, randomness only through `Rand`, and banner data only through `Banner`, and removing environment variables only through `Unsetenv` — and MUST NOT read the real process arguments, the real process environment, the real process id, or a global random source; the drain deadline of D03 and `net/http`'s own deadlines are measured in real elapsed time and are not read through `Now`.
- R-LZMP-T6HN: The non-test `.go` files under `internal/` MUST NOT reference `os.Args`, `os.Environ`, `os.Getenv`, `os.LookupEnv`, `os.Setenv`, `os.Unsetenv`, `os.Clearenv`, `os.Getpid`, `os.Stdin`, `os.Stdout`, `os.Stderr`, or `os.Exit`, and MUST NOT import `os/signal`.
- R-M0UM-6Y8C: The non-test `.go` files of the module MUST NOT call `net.Listen`, `net.ListenTCP`, `net.ListenUnix`, `net.ListenUDP`, `net.ListenUnixgram`, `net.ListenIP`, `net.ListenMulticastUDP`, `net.ListenPacket`, the `Listen` or `ListenPacket` method of a `net.ListenConfig`, `http.ListenAndServe`, `http.ListenAndServeTLS`, the `ListenAndServe` or `ListenAndServeTLS` method of an `http.Server`, `syscall.Socket`, `syscall.Bind`, or `syscall.Listen`, so that the only listening socket auth serves on is the one it is passed.
- R-M22I-KPZ1: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error`.
- R-M3AE-YHPQ: The `internal/server` package MUST export `type DrainError struct { Unfinished int }` and `func (e *DrainError) Error() string`.
- R-L9FH-DHDY: `*server.Server` MUST implement `http.Handler` through `func (*Server) ServeHTTP(w http.ResponseWriter, r *http.Request)`.
- R-STSR-5OE3: A running `auth` server MUST be self-contained in its executable: it MUST depend on no shared library at run time, and MUST open no file of its own at run time other than its SQLite database at `state/auth.db`, the files SQLite keeps beside it, and the services file the banner source reads — no configuration file and no file of the sub-project's `etc/` or `share/` directory among them; files the Go runtime and standard library read on their own account (such as the system's time-zone, TLS root-certificate, or resolver files) are not auth's own files; the inherited file descriptor 3 and the `NOTIFY_SOCKET` datagram socket are not files it opens.
- R-SSKU-RWNE: The non-test `.go` files under `internal/` MUST NOT reference `appkit.New`, so that the one read of `IKIGENBA_SERVICES` from the process environment is the one `main` makes through `appkit.New`.
- R-SV0N-JG4S: The sub-project's `etc/` directory MUST contain exactly one entry, `manifest.toml`, and the sub-project's `share/` directory MUST contain exactly one entry, a regular file named `icon.svg`.
- R-SW8J-X7VH: The file `share/icon.svg` at the sub-project root MUST be a well-formed XML document whose root element is an `svg` element in the namespace `http://www.w3.org/2000/svg`.

# D01-layout-and-run-seam

auth is one Go binary that serves the platform's auth service. This document
fixes the skeleton the rest of the auth design hangs on: the module path, the
packages the later documents name, the one platform library it depends on, and
the run seam through which the program touches the outside world. It designs no
endpoint, no config validation, no OAuth flow, and no persistence behavior —
only where those things live and how they are wired and tested. The later
design documents attach their contracts to the packages this document names:
`internal/server` owns the HTTP handlers and router and the listener's life
(D03), `internal/store` owns persistence (D04), `internal/google` owns the
Google/OIDC client (D05), and `internal/idcodec` owns the opaque-id and secret
encoding (D04).

## The one platform dependency

auth depends on `github.com/ikigenba/ikigenba/appkit`, the platform's shared
banner kit, beside its Google and SQLite libraries. appkit is a sibling
sub-project, so auth reaches it only through its published package, whose exported surface is
`StaticPrefix = "/_appkit/"`, `Static() http.Handler`, `Templates()
*template.Template`, `New(service string) *Kit`, `(*Kit).Banner(User)
Banner`, and the plain data types `User`, `Banner` and `Service`. appkit owns
the banner's markup, the launcher's markup and script, the stylesheet, the
fonts and their licences; auth authors none of them and carries no copy.

The style files come from appkit, which serves them itself (D08). The pages
auth draws are its own templates in `internal/server` (D05, D07), drawn around
appkit's banner templates.

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
it itself.

The checkout carries two hand-authored inputs beside the code:
`etc/manifest.toml` (D02) and `share/icon.svg`, auth's icon, an SVG image a
human draws from Tabler's outline `fingerprint` icon; its presence in the
package is what lists auth in the platform's launcher on a space. The build
run never writes it. Everything auth serves is inside the binary — its own templates and appkit's
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
signal. auth opens no listening socket of its own, which is how "auth
listens on no other socket and no port" is kept.

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

The version is a value: `internal/version` exports it as a `var` of shape
`v<semver>`.

## REQUIREMENTS

- R-3FKW-RNJY: The Go module path MUST be `github.com/ikigenba/ikigenba/auth`.
- R-2B1J-WL7R: When the `auth` executable runs with no command, the three Google settings in its environment, and a listening socket passed as file descriptor 3 by the socket-activation protocol, it MUST open its database at `state/auth.db` relative to its working directory, MUST draw its pages' banner from the services file that `IKIGENBA_SERVICES` names in its environment at start, and on `SIGTERM` or `SIGINT` MUST stop, write nothing to stdout or stderr, and exit `0`.
- R-3O47-G1QT: Package `internal/version` MUST own the release version value and export it as `Version`.
- R-SITN-PQPU: `internal/cli` MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Unsetenv func(key string) error; Pid int; Stdout io.Writer; Stderr io.Writer; Inherit func(fd uintptr) (net.Listener, error); Now func() time.Time; Rand io.Reader; OIDCIssuer string; DBSource string; Banner func(u appkit.User) appkit.Banner }`, with exactly those fields in that order, where `appkit` is the package `github.com/ikigenba/ikigenba/appkit`, `Args` excludes the program name, `Pid` is the id of the process `Run` speaks for, a nil `Unsetenv` means `Run` removes no variable, and `Banner` is the banner source from which every page auth draws with the banner is drawn.
- R-LUR4-A3IV: `internal/cli` MUST export `func Run(ctx context.Context, p Process) int`.
- R-2C9G-ACYG: `internal/version` MUST export `var Version string` whose value is a leading `v` followed by a semantic version.
- R-3WNI-4FXO: `main` MUST terminate the process with the exact integer that `cli.Run` returns as the process exit status.
- R-3XVE-I7OD: `cli.Run` MUST return `0` on success, `1` when the server fails, and `2` on a usage error.
- R-SRCY-E4WP: Given a `Process` whose `Stdout` and `Stderr` are in-memory buffers, `Args` an explicit slice, `LookupEnv` a fake lookup, `Unsetenv` nil or a recorder, `Pid` a value the test chose, `Inherit` a function returning a listener the test made, `Now` a fixed clock, `Rand` a deterministic reader, `OIDCIssuer` a loopback URL, `DBSource` a temporary database, and `Banner` a function the test wrote, `cli.Run` MUST take every input and produce every output through that `Process` — reading arguments only from `Args`, environment only through `LookupEnv`, every time it records or compares against stored state only through `Now`, randomness only through `Rand`, and banner data only through `Banner`, and removing environment variables only through `Unsetenv` — and MUST NOT read the real process arguments, the real process environment, the real process id, or a global random source; the drain deadline of D03 and `net/http`'s own deadlines are measured in real elapsed time and are not read through `Now`.
- R-2DHC-O4P5: A running `auth` server MUST hold no listening socket other than the one passed to it as file descriptor 3.
- R-M22I-KPZ1: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error`.
- R-M3AE-YHPQ: The `internal/server` package MUST export `type DrainError struct { Unfinished int }` and `func (e *DrainError) Error() string`.
- R-L9FH-DHDY: `*server.Server` MUST implement `http.Handler` through `func (*Server) ServeHTTP(w http.ResponseWriter, r *http.Request)`.
- R-STSR-5OE3: A running `auth` server MUST be self-contained in its executable: it MUST depend on no shared library at run time, and MUST open no file of its own at run time other than its SQLite database at `state/auth.db`, the files SQLite keeps beside it, and the services file the banner source reads — no configuration file and no file of the sub-project's `etc/` or `share/` directory among them; files the Go runtime and standard library read on their own account (such as the system's time-zone, TLS root-certificate, or resolver files) are not auth's own files; the inherited file descriptor 3 and the `NOTIFY_SOCKET` datagram socket are not files it opens.

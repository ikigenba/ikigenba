# D01-layout-and-run-seam

dummy is one Go binary that serves a small control panel. This design is the
structural ground the other seven designs stand on: where the code lives, which
package exports which name, what the module depends on, how the version, the
manifest and the usage text are declared, and the seam through which the
process is run so that the command, the server and the panel can be tested
without a real process.

The module is `github.com/ikigenba/ikigenba/dummy`, rooted at the sub-project
directory, with `go.mod` beside `specs/`. Its `main` package sits at
`cmd/dummy` because `devctl build` finds an app as a `package main` in
`cmd/<app>` under the directory that holds `etc/manifest.toml`, like every
other binary in the repository; a `package main` at the root would not be
found. The `cmd/dummy` package is wiring only.

## The one dependency

dummy depends on one module besides the standard library:
`github.com/ikigenba/ikigenba/appkit`, the platform's shared banner kit,
fetched through the ordinary Go module proxy like any published module, with
no `replace` directive pointing into the repository. Which release is data: it
lives in `go.mod`, and no requirement names it. appkit is a sibling
sub-project, so dummy reaches it only through its published package, whose
documented surface (`go doc -all github.com/ikigenba/ikigenba/appkit`) is
`StaticPrefix = "/_appkit/"`, `Static() http.Handler`, `Templates()
*template.Template`, `New(service, version string) *Kit`,
`(*Kit).Banner(User) Banner`, and the plain data types `User`, `Banner` and
`Service`; a `Banner` carries the service and the version `New` was given,
unaltered. appkit owns the banner's markup, the footer's markup, the
launcher's markup and script, the stylesheet, the fonts
and their licences; dummy authors none of them and carries no copy.

`appkit.New` is the one place the kit touches the process: it reads
`IKIGENBA_SERVICES` from the real environment with `os.Getenv` when it is
called and keeps the path, and it offers no form that takes the path from a
caller (its documentation: "New captures the host services path for the
named app"; a probe against the published module confirms that the path is
taken from the process environment at the call and that a file written after
it is read by the next `Banner`). Each call to
`(*Kit).Banner` then reads that file afresh, returns no services when the path
is empty or the file is missing, unreadable or malformed, and never writes
anything or returns an error. Reading the real environment is exactly what the
run seam keeps out of everything below `main`, so `appkit.New` is called in
`main` and nowhere else, once, at start: that is the one read of
`IKIGENBA_SERVICES` the serve story describes, and since `New` cannot fail,
the variable can never stop dummy starting.

What crosses the seam is not the kit but a function, the banner source,
`func(appkit.User) appkit.Banner`. `main` passes the `Banner` method of the
kit it made; a test passes a closure of its own that returns whatever banner
data the case needs, a launcher's services included, without a services file
and without touching the environment. The same unnamed function type is the
handler's parameter (`D04-panel` declares `panel.Handler`), so `internal/cli`
hands the source on unchanged and never calls it itself. Because the kit is
built in `main` and must name dummy's service and its release version, `main`
passes it `panel.ServiceName`, the one declaration of the name, and
`cli.Version`, the one declaration of the version, the same value `--version`
prints; that is why `cmd/dummy` imports `internal/panel` as well as
`internal/cli`. The footer every page ends with (`D04-panel`) shows those two
values, so it names the release that is serving the page. That `main` hands
`New` the version is wiring, proved like the launcher's wiring by the one
exec'ing test: the page the running binary serves carries a footer naming
`ServiceName` and `Version`.

## Packages

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
page frame the appkit banner is drawn in, rendering, the two failure shapes,
the table fragment, the form, and the mounting of appkit's shared files under
`/_appkit/` (`D04-panel`, `D06-table`, `D07-form`, `D08-assets`).
`internal/widget` owns the domain: the widget entity, the status enumeration,
the in-memory store, and the validation of a submission (`D05-widgets`).

There is no package at the module root any more. It existed only to embed a
hand-maintained `assets/` directory, which `internal/` could not reach; the
style files now come from appkit, which embeds them itself, so dummy embeds no
style file and has nothing for a root package to do.

Dependencies point one way. `cmd/dummy` imports `internal/cli` and
`internal/panel`; `internal/cli` imports `internal/server`, `internal/panel`
and `internal/widget`; `internal/panel` imports `internal/widget`. appkit is
imported by `cmd/dummy` (to build the kit), `internal/cli` (whose `Process`
names appkit's types) and `internal/panel` (which renders the banner and
serves the shared files); `internal/server` and `internal/widget` import
neither appkit nor anything of dummy's. Nothing imports `internal/cli` but
`cmd/dummy`. There is no cycle to break, and the two packages a test most
wants to drive on their own — the listener's life and the domain — depend on
nothing.

The handler lives in `internal/panel` rather than `internal/server` because it
is not a pure function of method and path: it reads request headers, renders
several page shapes and a fragment, and holds a store, so left in
`internal/server` it would be that package's whole mass, under a name that
means transport. `internal/server` is nonetheless kept as a package holding
only the serve call, because a listener's life — the graceful drain, the
silenced server diagnostics, returning non-nil only on a real failure — is a
separate concern, separately testable with a fake listener and a handler that
panics. `internal/widget` is separate from `internal/panel` because the rules
that accept or reject a submission are decidable without HTTP and are the
part of dummy most worth testing on its own. An interface between the two was
considered and rejected: there is one store and there will be one, so an
interface would be a name with no second member.

## Declarations and the checkout

The version is a `var` in `internal/cli`. Its value is data: a requirement fixes the name, that
it is a `var`, and its shape, and nothing else. The manifest is a constant in
the same package holding exactly the three lines the bootstrap story shows,
with a trailing newline; it declares no port, because dummy serves on the
socket the host passes it and a manifest carrying `port` is refused by
`devctl build` and by opsctl. The usage text is a constant in that package too, declared
here so that every name `internal/cli` exports is declared in one place; its
value belongs to `D02-cli`, because the help output is that design's subject,
and it is fixed there byte for byte.

The package story lists exactly three members in the release file —
`bin/dummy`, `etc/manifest.toml` and `share/icon.svg`. `share/icon.svg` is
dummy's icon, an SVG image a human draws, and its presence in the package is
what lists dummy in the platform's launcher on a space. It is a human-authored
input like `etc/`'s hand-read copy of the manifest, never written by the build
run. Everything dummy serves is inside
the binary — `internal/panel`'s templates and appkit's embedded files alike —
and `D04-panel`'s rule about the working directory is what shows that no
answer depends on a file beside the binary.

## The run seam

The run seam is `cli.Run`. It takes a context and a `cli.Process` value that
carries everything the program would otherwise take from the `os` package:
the arguments without the program name, an environment lookup, a way to
remove a variable from the environment, the process's own id, the two output
streams, a way to turn an inherited file descriptor into a listener, and the
banner source built from the one environment read appkit makes. Its return
value is the process exit code. `main` fills the `Process` from the real
process and cancels the context on `SIGTERM` or `SIGINT`; a test fills it with
buffers, a map, a pid of its choosing, a listener it made itself and a banner
source of its own, and cancels the context itself. `Run`'s behaviour when it
serves with a nil `Banner` is not contract, so a test that serves supplies
one. Nothing below `main` reads `os.Args`, the real environment, the real pid
or the real streams, changes the real environment, calls `appkit.New`, or
installs a signal handler, so a test that drives `Run` sees the whole
program's behaviour and nothing leaks past it. The one writer below the seam that could
reach the real standard error on its own, the HTTP server's diagnostics that
`net/http` would send through the `log` package, is silenced by `D03-serve`:
`Serve` discards the server's own diagnostics.

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
signal.

`Process` carries no store. The widget set is neither an argument, an
environment value nor a stream, so it is not part of the process seam; it
comes into being below it. Below the seam, `internal/cli` takes the socket
and, when that succeeds, makes the process's one widget store and hands the
listener to `server.Serve` together with the handler `internal/panel` builds
over that store and `Process.Banner`, and the drain deadline. Making the store
there, and after the socket is taken, puts "the widget set is created once, at
process start, and dies with the process" at the one place that happens, keeps
a start that fails from building anything, and keeps `--version` from building
a store it will never use; it also lets a test build a handler over a store it
has already filled and a banner source it wrote. The handler is also handed
the writer its per-request diagnostic goes to, because a 5xx is the one answer
that is trouble and the handler is the one that knows which request it was.
`Serve` owns the listener from then on and returns when the drain after the
context is cancelled has ended or the server fails; when the drain deadline
passes with requests still running it returns a `server.DrainError` counting
them, which `Run` reports like any other serve failure. What `Serve` does
between those points is `D03-serve`, what the handler answers is `D04-panel`
and the designs it leads to, and the hand-off itself — drain deadline, socket,
store, banner source, readiness, serve — is `D03-serve` too.

## REQUIREMENTS

- R-Z46Q-6N1H: The `dummy` binary MUST behave as `cli.Run` does when given the binary's arguments after the program name, the process's environment, its process id, and its standard output and standard error, and MUST exit with the value `Run` returns.
- R-Z5EM-KES6: When the `dummy` binary is serving and receives `SIGTERM` or `SIGINT`, it MUST stop as `Run` does when its context is cancelled.
- R-Z7UF-BY9K: When the `dummy` binary starts with `IKIGENBA_SERVICES` naming a services file that lists one or more services, the page it serves for `GET /widgets` MUST carry the appkit launcher: a `button` start tag whose `class` is `launcher`.
- R-7J0U-GFGT: When the `dummy` binary is serving with `IKIGENBA_SERVICES` unset or naming a services file no part of which contains the sequence `footer` compared case-insensitively, the page it serves in answer to a `GET /widgets` request carrying a non-empty `X-User-Id` header MUST, read as a whole body, contain exactly one `footer` start tag, as `D04-panel` defines start tags and end tags (R-KDGH-2SDG), and the normalisation (`D04-panel` R-KIC2-LVC8) of the characters from that start tag's `>` up to the `<` of the first `</footer>` end tag following it MUST be exactly the value of `panel.ServiceName` (`D04-panel`), one space, and the value of `Version`.
- R-AMJL-GJV8: The `internal/cli` package MUST export `var Version string`.
- R-ANRH-UBLX: `Version` MUST be the letter `v` followed by a valid Semantic Versioning version as defined at semver.org, prerelease and build metadata included when present.
- R-LI0D-VJTO: The `internal/cli` package MUST export `const Manifest = "app = \"dummy\"\ndefault = false\nsecrets = []\n"`.
- R-10H0-0STN: The `internal/cli` package MUST export `Usage` as a string constant, holding the usage text whose value `D02-cli` fixes.
- R-S3OH-HHQH: The `internal/cli` package MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Unsetenv func(key string) error; Pid int; Stdout io.Writer; Stderr io.Writer; Inherit func(fd uintptr) (net.Listener, error); Banner func(u appkit.User) appkit.Banner }` and `func Run(ctx context.Context, p Process) int`, where `appkit` is the package `github.com/ikigenba/ikigenba/appkit`, `Args` excludes the program name, `Pid` is the id of the process `Run` speaks for, a nil `Unsetenv` means `Run` removes no variable, and `Banner` is the source of the banner data every page the handler draws with the banner is drawn from.
- R-092E-Q4T0: The `internal/cli` package MUST export `ExitSuccess`, `ExitServerFailed` and `ExitUsage` as constants, so that each of the three names is usable as an operand of a constant expression — the initializer of a `const` declaration in a package that imports `internal/cli` included — and their constant values MUST be 0, 1 and 2 respectively.
- R-AXIO-WHJH: `Run` MUST return one of `ExitSuccess`, `ExitServerFailed`, or `ExitUsage`, and no other value.
- R-LLO3-0V1R: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error`.
- R-LMVZ-EMSG: The `internal/server` package MUST export `type DrainError struct { Unfinished int }` and `func (e *DrainError) Error() string`.

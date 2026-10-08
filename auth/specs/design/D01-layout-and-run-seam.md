# D01-layout-and-run-seam

auth is one Go binary that serves the platform's auth service. This document
fixes the skeleton the rest of the auth design hangs on: the module path, the
packages the later documents name, the one platform library it depends on,
the run seam through which the program touches the outside world, the
migrations that give its database its schema, and the page templates its
`assets/` directory holds. It designs no
endpoint, no config validation, no OAuth flow, and no persistence behavior —
only where those things live and how they are wired and tested. The later
design documents attach their contracts to the packages this document names:
`internal/server` owns the HTTP handlers and router and the listener's life
(D03), `internal/store` owns persistence (D04), `internal/google` owns the
Google/OIDC client (D05), and `internal/idcodec` owns the opaque-id and secret
encoding (D04).

## The one platform dependency

auth depends on appkit, the platform's shared library, beside auth's Google
libraries, and on four of its packages. The first is
`github.com/ikigenba/ikigenba/appkit/page`, the banner kit, whose exported
surface is `StaticPrefix = "/_appkit/"`, `Static() http.Handler`,
`Templates() *template.Template`, `PreloadURL() string`,
`New(service, version string) *Kit`,
`(*Kit).Banner(User) Banner`, and the plain data types `User`, `Banner` and
`Service`. The second is `github.com/ikigenba/ikigenba/appkit/telemetry`, the
suite's event trail: the `Writer` auth records its events through, the
`Sink` they are delivered to, the request middleware that records every
request, and, for tests, the capturing sink. The third is
`github.com/ikigenba/ikigenba/appkit/identity`, and of it auth uses only the
`Caller` a context carries (`NewContext`, `FromContext`), so that an event it
records while answering a request carries that request's id and the user the
event is about (D05, D06, D07). The fourth is
`github.com/ikigenba/ikigenba/appkit/db`, the suite's one way to keep a SQLite
database: `db.Open` opens or creates the database at a path, creating its
missing directories, applies the migrations it is given that the database has
not had, and refuses a database it cannot open; a database that records a
migration it was not given, one a newer binary has migrated, it accepts
unchanged, warning once on the `Stderr` it is given; `DB.Read` and `DB.Write`
run the store's transactions, `Write` on the one writer; `DB.Close` closes the
handle; `DB.SetFailing` makes a handle fail every transaction, which is how
auth's tests make its store fail; and `db.Status` writes, changing nothing,
which migrations a database has had and which it has not. The SQLite driver,
the pure-Go one that keeps the release build cgo-free, arrives through that
package; auth never imports it. All of that is appkit's contract (its D15 and
D16): auth restates none of it and its tests re-prove none of it. appkit is a
sibling sub-project, so auth
reaches it only through those published packages. `identity.Require` is not
auth's: auth is the identity provider, so no identity gate stands in front of
it (D08), and its own code never reads the services file, which `page` and
telemetry's socket sink read through appkit's `services` package. appkit owns
the banner's markup, the launcher's markup and script, the page footer's
markup, the feedback script, the favicon, the stylesheet, the fonts and
their licences, and the font preload link and its URL; auth authors none of
them and carries no copy.

The style files come from appkit, which serves them itself (D08). The pages
auth draws are its own templates, drawn around appkit's banner templates: the
sign-in pages and the profile's older parts are templates in
`internal/server` (D05, D07), and the newer ones are human-authored files in
`assets/`, embedded by the root package (below): the approve page an MCP
client sends its user to (D09) and the profile's `MCP clients` card (D07).

`page.New` is the one place the kit touches the process: its documentation
says "New captures the host services path for the named app", and it reads
`IKIGENBA_SERVICES` (`services.Variable`) from the real environment when it is
called and offers no form that takes the path from a caller. Each call to
`(*Kit).Banner` then reads that file afresh through appkit's `services.Read`,
whose format is opsctl's published one: a JSON object whose `services` array
holds one entry per service, each carrying `name`, `url`, `description` and
`socket` (strings), `enabled` and `mcp` (booleans), and optionally `icon`. An
entry missing one of those six members, holding one of the wrong type, or with
an empty `name` is skipped, and the launcher shows only the entries that carry
an icon. `Banner` returns no services when the path is
empty or not already clean (`services.Read` refuses a path `filepath.Clean`
would change), or the file is missing, unreadable or malformed; it never
writes anything or returns an error. A test that writes a services file — the
one exec'ing test — writes it in that format, at a path that is already clean:
an entry with only `name`, `url`, `icon` and `enabled` is skipped, so a file of
such entries draws no launcher, and the entry the test looks for in the
launcher carries all six members and an `icon`. Reading the real environment
is exactly what the run seam keeps out of everything below `main`, so
`page.New("auth", version.Version)` is called in `main` and nowhere else,
once, at start: that is the one read of `IKIGENBA_SERVICES` the serve story
describes, and since `New` cannot fail, the variable can never stop auth
starting. auth's own code never reads the
variable. The second argument is auth's release version, the value of
`Version` in `internal/version`, the same value `--version` prints (D02);
appkit hands it back unaltered in every `Banner` it returns, and its `footer`
template writes it after the service's name, so every page auth draws with
the banner ends `auth <version>`. The one exec'ing test proves that wiring by
reading the footer of the page it requests and comparing it with
`version.Version`, which it imports; it never writes a version literal.

What crosses the seam is not the kit but a function, the banner source,
`func(page.User) page.Banner`. `main` passes the `Banner` method of the
kit it made; a test passes a closure of its own that returns whatever banner
data the case needs, a launcher's services included, without a services file
and without touching the environment. `internal/cli` hands the source on,
unchanged, to `internal/server` through `server.Config` (D03) and never calls
it itself.

Beside the code the checkout carries `etc/manifest.toml` (D02), the page
templates in `assets/` (below), and `share/icon.svg`, auth's icon, an SVG
image a human draws from Tabler's outline `user-circle` icon; its presence in
the package is what lists auth in the platform's launcher on a space. The
build run never writes the icon or `assets/`. Everything auth serves is inside the binary — its own templates, those in
`internal/server` and those in `assets/`, and appkit's embedded files alike.

`assets/` at the module root holds auth's own page templates as Go
`html/template` files: `approve.html`, whose template is `approve`, and
`mcp-clients.html`, whose template is `mcp-clients`. They follow the platform's
visual style, and the user or the delivering agent writes them;
the build run reads them and never writes them. Go's `embed` reaches only
files at or below the embedding package's directory, so the root package
`auth` embeds them, beside the migrations, and exports `Assets`, a file system
holding exactly those two files, the same on every call and depending on no
file beside the binary. **auth's template set** is what the handlers draw
those pages from: a fresh set from appkit's `page.Templates()`, which already
defines `banner`, `launcher`, `footer` and `preload`, with every file of
`Assets()` parsed into it and no template function added. The code adds no
markup of its own around them, so a test renders the same template with the
same data and compares the bytes. A template in `assets/` that writes a
page's head, as `approve.html` does, invokes `{{template "preload"}}` there,
so the page preloads upright Inter (D05) without auth writing a font name or
hash.

The database's schema is inside the binary too. It is a sequence of migration
files in `migrations/` at the module root, which only a package at the module
root can embed, so the root package `auth` embeds them too and exports
`Migrations`, a file system holding exactly the three migrations this auth
carries. `0001_baseline.sql` is the schema auth has always created, its users,
sessions, login states and tokens (D04 declares it), written so that it
creates each table and index only if it does not exist and seeds nothing, so
a database an earlier auth wrote before it carried migrations is adopted as it
is. `0002_token_id_prefix.sql` gives every token that still carries a bare
26-character id the `tok_` prefix (D04); it transforms rows that are there and
adds none. `0003_mcp_clients.sql` gives every token a kind and a host, making
each existing one a personal token bound to no host, and adds the tables that
hold MCP clients' registrations and authorization codes (D04). That file system is what `Run` hands `db.Open` and `db.Status`, and
what a test hands `db.Open` to make a database of auth's shape in its own
temporary directory. It is the same on every call and depends on no file
beside the binary. A later migration is a new file there with the next
four-digit version; none is ever edited once released.

So auth needs nothing on disk but its database. The release build is cgo-free
(`CGO_ENABLED=0`), and a cgo-free Go build links no C library and yields an
executable that loads no shared library. A working directory holding only
`state/` is all it runs from: it reads no configuration file and nothing of
the checkout's `etc/` or `share/`, and it leaves behind nothing but its
database and the journal, WAL, and shared-memory files SQLite names after it
(`auth.db-journal`, `auth.db-wal`, `auth.db-shm`; SQLite's "Temporary Files
Used By SQLite"). The one exec'ing test proves this from the outside, in the
directory it already runs the child in; which files a running process opens
is visible only in its `/proc` entries, which the tests never read.

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
from the real process, cancels the context on `SIGTERM` or `SIGINT` with the
signal's name, `SIGTERM` or `SIGINT`, as the cancellation's cause, calls
`Run`, and exits with what it returned. A test fills the `Process` with
buffers, a map, a pid of its choosing, a listener it made itself and a banner
source of its own, and cancels the context itself; `Run`'s behaviour when it
serves with a nil `Banner` is not contract, so a test that serves supplies
one. Nothing below `main` reads `os.Args`, the real environment, the real pid
or the real streams, changes the real environment, calls `page.New`, or
installs a signal handler,
so a test that drives `Run` sees the whole program's behaviour and nothing
leaks past it. auth needs more injected than a plain app, because it reads the
clock, mints random ids and secrets, talks to Google, and keeps a database; so
`Process` also carries a clock, a randomness source, the Google OIDC issuer
location (so a loopback fake stands in for Google offline), and the directory
its database's path is resolved against, then the banner source described
above, and, last, the telemetry sink described below.

`Process.Dir` is that directory. `Run` keeps its database at the **database
path**, `state/auth.db` resolved against `Dir`, which it opens with appkit's
`db.Open`, the migrations `auth.Migrations()` and the clock `Process.Now`, so
the migrations it applies are stamped with the injected clock like everything
else it records. `main` leaves `Dir` empty, so the path is `state/auth.db`
relative to the working directory, which is `/opt/auth` on a host, and passes
`time.Now` as the clock; a test sets a temporary directory it owns, so two
`Run`s given the same `Dir` share one database and two given different ones do
not. There is no field naming another database: appkit's `db` takes only an
ordinary file path, so there is no in-memory database. The one exec'ing test
seeds the child's `state/auth.db` the way any test makes a database of auth's
shape: `db.Open` with `auth.Migrations()`, a store over that handle with
`store.New`, and the handle closed before the child starts.

Socket activation hands the process a listening socket as file descriptor 3,
and the one thing below the seam that touches a real descriptor is turning
that number into a `net.Listener`. `Process.Inherit` is that step: `main`
leaves it nil, and `Run` then makes the listener from the real descriptor; a
test supplies a function that returns a listener it bound itself, so
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
listens on no other socket" is kept: the seam's `Process` (R-7ALZ-I7QX)
carries exactly one way to come by a listener, `Inherit`, and nothing that
binds one, and `Run` takes every input and produces every output through that
`Process` (R-7BTV-VZHM). No requirement asserts the absence of other sockets in a running
process, because only the process's `/proc` entries could show it and the
tests read nothing there.

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

## The trail

auth records what it does as a trail of events, through one
`telemetry.Writer` that `Run` builds once it is about to serve (D03): its
service is `auth`, its version the `Version` of `internal/version`, the same
value `--version` prints and the footer shows, its clock `Process.Now`, its
random source `Process.Rand`, and its diagnostic stream `Process.Stderr`. The
events go to a `telemetry.Sink`, and the sink is the last thing the seam
carries, `Process.Sink`. `main` passes the socket sink appkit's
`telemetry.NewSocketSink` returns, which on every delivery reads the services
file `IKIGENBA_SERVICES` names, finds the entry named `telemetry`, and posts
the event to `/ingest` on that entry's socket; that read of the real
environment happens inside a value `main` made, the way the banner source
reads the services file, so nothing below `main` reads the environment for
it. A test passes `&telemetry.Capture{}`, which records every event it is
handed, and reads it back once `Run` has returned; or a sink of its own that
rejects or holds events, to see what auth does when telemetry will not take
them. A nil `Sink` is not contract, so a test that serves supplies one.

The reason auth records when it stops is the name of the signal that stopped
it. `main` learns the signal and `Run` does not, so the signal crosses the
seam as the context's cancellation cause: `main` cancels with an error whose
text is `SIGTERM` or `SIGINT`, and `Run` records the cause's text. A test
that wants a particular reason cancels with a cause of its own choosing
(`context.WithCancelCause`); one that cancels plainly gets the text of
`context.Canceled`. No new name crosses the seam for it.

## REQUIREMENTS

- R-3FKW-RNJY: The Go module path MUST be `github.com/ikigenba/ikigenba/auth`.
- R-AUXO-9DST: When the `auth` executable runs with no command, the three Google settings in its environment, a listening socket passed as file descriptor 3 by the socket-activation protocol, and `IKIGENBA_SERVICES` naming a services file whose entry named `telemetry` names a Unix socket on which an HTTP server answers every `POST /ingest` with `204`, it MUST open its database at `state/auth.db` relative to its working directory, MUST draw its pages' banner from the services file that `IKIGENBA_SERVICES` names in its environment at start, and on `SIGTERM` or `SIGINT` MUST stop, write nothing to stdout or stderr, and exit `0`.
- R-AW5K-N5JI: When the `auth` executable serves as R-AUXO-9DST describes, every page it draws with the banner MUST end its `body` element's content, apart from trailing ASCII whitespace, with a `footer` element whose content reads `auth`, a single space, and the value of `Version` from `internal/version`.
- R-3O47-G1QT: Package `internal/version` MUST own the release version value and export it as `Version`.
- R-7ALZ-I7QX: `internal/cli` MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Unsetenv func(key string) error; Pid int; Stdout io.Writer; Stderr io.Writer; Inherit func(fd uintptr) (net.Listener, error); Now func() time.Time; Rand io.Reader; OIDCIssuer string; Dir string; Banner func(u page.User) page.Banner; Sink telemetry.Sink }`, with exactly those fields in that order, where `page` is the package `github.com/ikigenba/ikigenba/appkit/page` and `telemetry` is the package `github.com/ikigenba/ikigenba/appkit/telemetry`, `Args` excludes the program name, `Pid` is the id of the process `Run` speaks for, a nil `Unsetenv` means `Run` removes no variable, `Now` is the clock every time `Run` records or compares against is read from, the time of every migration it applies included, `Dir` is the directory against which `Run` resolves the path `state/auth.db` of its database, empty meaning the process working directory, `Banner` is the banner source from which every page auth draws with the banner is drawn, and `Sink` is the sink to which `Run` delivers the events it records.
- R-LUR4-A3IV: `internal/cli` MUST export `func Run(ctx context.Context, p Process) int`.
- R-2C9G-ACYG: `internal/version` MUST export `var Version string` whose value is a leading `v` followed by a semantic version.
- R-3WNI-4FXO: `main` MUST terminate the process with the exact integer that `cli.Run` returns as the process exit status.
- R-7D1S-9R8B: `cli.Run` MUST return `0` on success, `1` on a failure that is not a usage error (the server failing, a database it cannot open, or a `db status` that fails, D02 and D03), and `2` on a usage error.
- R-G55N-V09H: The module's root package, imported from the path `github.com/ikigenba/ikigenba/auth` with the package name `auth`, MUST export `func Migrations() fs.FS`, where `fs` is the standard library's `io/fs`, returning a file system whose root directory holds exactly the regular files `0001_baseline.sql`, `0002_token_id_prefix.sql`, and `0003_mcp_clients.sql` and no other entry.
- R-G6DK-8S06: Every call to the root package's `Migrations` MUST return a file system holding the same three files with the same contents, whatever the process working directory is, a directory that holds no `migrations/` directory included.
- R-G7LG-MJQV: The module's root package, imported from the path `github.com/ikigenba/ikigenba/auth` with the package name `auth`, MUST export `func Assets() fs.FS`, where `fs` is the standard library's `io/fs`, returning a file system whose root directory holds exactly the regular files `approve.html` and `mcp-clients.html` and no other entry, embedded from the files of the same names in the module's `assets/` directory.
- R-G8TD-0BHK: Every call to the root package's `Assets` MUST return a file system holding the same files with the same contents, whatever the process working directory is, a directory that holds no `assets/` directory included.
- R-GA19-E389: auth's design defines **auth's template set** as the `*template.Template`, where `template` is the standard library's `html/template`, that results from calling the `ParseFS` method of a set `page.Templates()` returned with the file system the root package's `Assets()` returns (R-G7LG-MJQV) and the single pattern `*.html`, no function having been added to that set by `Funcs`; and every requirement of auth's design that names auth's template set MUST denote a set so made.
- R-GB95-RUYY: Making auth's template set MUST succeed, its `ParseFS` call returning a nil error, and the set made MUST define a template named `approve` and a template named `mcp-clients`.
- R-7GPH-F2GE: auth's design defines the **database path** of a `Run` as `filepath.Join(p.Dir, "state", "auth.db")`, where `filepath` is the standard library's `path/filepath` and `p` is the `Process` given to that `Run`, a relative path resolved against the process working directory when `p.Dir` is empty; every requirement of auth's design that names the database path MUST denote this.
- R-7BTV-VZHM: Given a `Process` whose `Stdout` and `Stderr` are in-memory buffers, `Args` an explicit slice, `LookupEnv` a fake lookup, `Unsetenv` nil or a recorder, `Pid` a value the test chose, `Inherit` a function returning a listener the test made, `Now` a fixed clock, `Rand` a deterministic reader, `OIDCIssuer` a loopback URL, `Dir` a temporary directory the test owns, `Banner` a function the test wrote, and `Sink` a `*telemetry.Capture` or a sink the test wrote, `cli.Run` MUST take every input and produce every output through that `Process` — reading arguments only from `Args`, environment only through `LookupEnv`, every time it records or compares against stored state, every event's time, and the time of every migration it applies only through `Now`, randomness, the request ids it mints included, only through `Rand`, except the request id appkit's `telemetry.Middleware` mints after its read of `Rand` fails (appkit D14, R-DRSL-TU2H), which MAY come from `crypto/rand`, and banner data only through `Banner`, delivering events only through `Sink`, and removing environment variables only through `Unsetenv` — and MUST NOT read the real process arguments, the real process environment, the real process id, or a global random source other than for such a request id; the drain deadline of D03 and `net/http`'s own deadlines are measured in real elapsed time and are not read through `Now`.
- R-M22I-KPZ1: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error`.
- R-M3AE-YHPQ: The `internal/server` package MUST export `type DrainError struct { Unfinished int }` and `func (e *DrainError) Error() string`.
- R-L9FH-DHDY: `*server.Server` MUST implement `http.Handler` through `func (*Server) ServeHTTP(w http.ResponseWriter, r *http.Request)`.
- R-AXDH-0XA7: The `auth` executable built from `./cmd/auth` with `CGO_ENABLED=0`, run as R-AUXO-9DST describes from a working directory that holds nothing but `state/auth.db`, a database holding a live session (no `etc/` directory, no `share/` directory, and no configuration file), with any services file `IKIGENBA_SERVICES` names and the socket its `telemetry` entry names kept outside that directory, MUST answer `GET /` carrying that session's `SessionCookieName` cookie with the page R-AW5K-N5JI describes.
- R-AYLD-EP0W: After the `auth` executable built from `./cmd/auth` with `CGO_ENABLED=0` has run as R-AUXO-9DST describes and exited, from a working directory that held nothing but a `state/` directory, empty or holding a database at `state/auth.db`, with any services file `IKIGENBA_SERVICES` names and the socket its `telemetry` entry names kept outside that directory, that working directory MUST hold nothing but `state/`, `state/auth.db`, and, in `state/`, only files named `auth.db-journal`, `auth.db-wal`, or `auth.db-shm`.
- R-AZT9-SGRL: When the `auth` executable serves as R-AUXO-9DST describes, the HTTP server on the socket its `telemetry` entry names MUST receive, as the body of a `POST /ingest`, an event whose `event` is `service.started`, whose `service` is `auth`, whose `request_id` and `user` are empty, and whose `attrs` hold exactly `version`, the value of `Version` from `internal/version`, before any other event; and, before the process exits on `SIGTERM` or `SIGINT`, an event whose `event` is `service.stopping`, whose `request_id` and `user` are empty, and whose `attrs` hold exactly `reason`, `SIGTERM` or `SIGINT` respectively, after every other event.
- R-B116-68IA: When the `auth` executable serves as R-AUXO-9DST describes and, while it serves, its services file is replaced by one whose `telemetry` entry names a second Unix socket on which an HTTP server answers every `POST /ingest` with `204`, the events of a request it receives after the replacement, and its `service.stopping`, MUST be received by the server on the second socket and not by the server on the first, without the process being restarted.

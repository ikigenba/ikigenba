# D01-layout-and-run-seam

dummy is one Go binary that serves a small control panel, and offers the same
widgets to MCP clients at `/mcp`. This design is the structural ground the
other designs stand on: where the code lives, which package exports which
name, what the module depends on, how the version, the manifest, the usage
text and the database's migrations are declared, and the seam through which the process is run so that the
command, the server, the panel and the MCP tools can be tested without a real
process.

The module is `github.com/ikigenba/ikigenba/dummy`, rooted at the sub-project
directory, with `go.mod` beside `specs/`. Its `main` package sits at
`cmd/dummy` because `devctl build` finds an app as a `package main` in
`cmd/<app>` under the directory that holds `etc/manifest.toml`, like every
other binary in the repository; a `package main` at the root would not be
found. The `cmd/dummy` package is wiring only.

## The one dependency

dummy depends on one module besides the standard library:
`github.com/ikigenba/ikigenba/appkit`, the platform's shared library, required
by its released version like any published module, with no `replace`
directive pointing into the repository; until that release's tag is pushed,
the module cache is seeded from the local repository as `AGENTS.md` records,
and once `go.sum` holds it the module is fetched like any other. Which release is data:
`AGENTS.md` names it, and no requirement names a version. appkit is a sibling
sub-project, so dummy reaches it only through its published packages and their
documented contracts, and uses five of them:

- `page` (`github.com/ikigenba/ikigenba/appkit/page`) — the page chrome:
  `page.New(service, version)` makes a `*page.Kit` whose `Banner(page.User)
  page.Banner` method yields the data the banner and footer are drawn from;
  `page.Templates()` is a fresh template set defining `banner`, `launcher` and
  `footer`, into which dummy parses its own templates; `page.Static()` serves
  the shared stylesheet, fonts, licences, launcher script, feedback script
  and favicon under `page.StaticPrefix` (`/_appkit/`).
- `identity` (`github.com/ikigenba/ikigenba/appkit/identity`) —
  `identity.Require(next)` wraps dummy's whole handler, so every request
  reaches dummy's routes and `/mcp` only with an `X-User-Id`, and is otherwise
  answered 500 with `identity.MissingBody`.
- `mcp` (`github.com/ikigenba/ikigenba/appkit/mcp`) —
  `mcp.NewServer(mcp.ServerConfig{...})` makes the `*mcp.Server` dummy mounts
  at `/mcp` and registers its tools on with `mcp.AddTool`; `mcp.Client` is how
  dummy's tests drive it.
- `telemetry` (`github.com/ikigenba/ikigenba/appkit/telemetry`) — the
  suite's event trail: `telemetry.New(telemetry.Config{...})` makes the one
  `*telemetry.Writer` every event of the process goes through;
  `telemetry.Middleware` wraps dummy's whole handler and records every
  request; `Writer.Ready` and `Writer.Shutdown` record the start and the
  stop; `Writer.Emit` records dummy's own `widget.created`; and
  `telemetry.Capture` is the sink dummy's tests read the events from. With
  no sink configured, the writer posts each event to the socket of the
  services-file entry named `telemetry`, read afresh for every event, and
  writes an event it cannot deliver to its standard error.
- `db` (`github.com/ikigenba/ikigenba/appkit/db`) — the shared SQLite
  handle: `db.Open` opens or creates the database at a path, creating its
  missing directories, applies the migrations it is given that the database
  has not had, and refuses a database that records a migration it was not
  given (`db.ErrUnknownVersion`); `DB.Read` and `DB.Write` run a transaction;
  `DB.Close` closes the handle; `DB.SetFailing` makes a handle fail every
  transaction, which is how dummy's tests provoke its answers for widgets it
  cannot reach; and `db.Status` writes, without changing anything, which
  migrations a database has had and which it has not. All of that is
  appkit's contract (its D15 and D16); dummy restates none of it and its
  tests re-prove none of it.

appkit owns the banner's, the footer's and the launcher's markup, the
launcher's script, the feedback script, the favicon, the stylesheet, the
fonts and their licences, the MCP transport, the reading of the services file, the database
handle, how migrations are applied and recorded and what `db status` prints
of them, and the event envelope, its delivery and the framework events (`service.started`,
`service.stopping`, `request.started`, `request.finished`, `tool.called`);
dummy authors none of them and carries no copy. dummy's own part of the trail
is its one domain event, `widget.created`, naming the widget by its id under
the entity type `widget`, whose id prefix `wgt_` appkit's D11 registers to
dummy.

## The two reads of the environment, both in main

`page.New` and `mcp.NewServer` each read `IKIGENBA_SERVICES`
(`services.Variable`) from the real process environment once, during the call,
and keep the path; neither offers a form that takes the path from a caller (as
appkit's published contracts for `page` and `mcp` state). Each later
`Kit.Banner` call, and each `initialize` or `server/discover` the server
answers with no `Instructions` function, reads that file afresh, and an unset
or empty variable, or a file that is missing, unreadable or malformed, yields
no services and no instructions, never an error. Reading the real environment
is exactly what the run seam keeps out of everything below `main`, so these
two constructors are called in `main` and nowhere else, once each, at start.
That is the read of `IKIGENBA_SERVICES` the serve story describes ("dummy
reads the variable once, when it starts"): two constructors read it in the
same instant, and since neither can fail on its account, the variable can
never stop dummy starting. `mcp.NewServer` panics only on an empty name or a
nil `Telemetry`; dummy's name is the constant `panel.ServiceName`, and its
`Telemetry` is the writer `main` builds below, never nil.

`main` builds:

- the gate, `cli.NewGate(telemetry.NewSocketSink())`, and passes it as
  `Process.Gate`;
- the writer, `telemetry.New(telemetry.Config{Service: panel.ServiceName,
  Version: cli.Version, Sink: gate, Stderr: os.Stderr})`, over that gate, so
  events go to the telemetry service through it, and the real clock, pause
  and random source, and passes it as `Process.Telemetry`;
- the kit, `page.New(panel.ServiceName, cli.Version)`, and passes its `Banner`
  method as `Process.Banner`;
- the server, `mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName,
  Version: cli.Version, Telemetry: w})`, over that same writer, with
  `Instructions` nil, and passes it as `Process.MCP`.

It leaves `Process.Rand` nil, so the widget ids come from `crypto/rand`;
`Process.Dir` empty, so the database is `state/dummy.db` under the process's
working directory, which is `/opt/dummy` on a host; and `Process.Now` nil,
so the migrations it applies are stamped with the real time. The exec test
sees the second as `state/dummy.db` appearing under the working directory it
started the child in. On
`SIGTERM` or `SIGINT` it cancels `Run`'s context with a cause whose text is
the signal's name, `SIGTERM` or `SIGINT`, which `Run` records as the reason
in `service.stopping` (`D03-serve`).

The writer is built in `main`, not in `Run`, because `mcp.NewServer` needs it
and `mcp.NewServer` may only be called in `main`; it crosses the seam as a
value, like the server, and a test builds its own over appkit's capturing
sink, with a clock, a pause and a random source it controls, a buffer as its
`Stderr`, and the same writer handed to the MCP server it makes. The socket
sink the writer uses in the binary reads `IKIGENBA_SERVICES` from the real
environment on every delivery; it is never used below `main` in a test,
because a test always names a sink.

The gate is dummy's own sink, standing between the writer and the sink
behind it, because the writer is built before `Run` and `Run` has no other
way to keep a late delivery from reaching telemetry. appkit's writer hands a
delivery a done context only once the context passed to `Shutdown` is done,
and when the drain runs out `Run` calls `Shutdown` with a context that is
already done. appkit's own design forbids what follows, but the code of the
release dummy requires does it: its writer can still hand the
`service.stopping` it has just queued to its sink with a context that is not
yet done, and a sink that answers at once delivers it. dummy therefore does
not rely on that part of appkit's design until a release keeps it. The gate passes every
`Deliver` straight to the sink behind it, its **next sink**, until the
drain deadline: the moment `Run`'s context is done, `Run` tells the gate the
deadline, and from that instant on (or from the moment `Run`'s stop function
finds the drain's context already done, if that comes first, so that the
gate's deadline is never later than the one `Serve` gives `stop` and the gate
refuses before any `Shutdown` with a done context begins), whenever `Shutdown` is called and
whether or not `Run` has returned, the gate refuses every `Deliver` with an
error, calling nothing behind it, so the writer counts each such event
undelivered and writes its line (`D03-serve`). A nil `Gate` takes away only
the two guarantees `D03-serve` states of the gate, never makes `Run` panic,
and changes nothing else `Run` does. Three `Run`-level tests wrap their own
sink in `cli.NewGate`, build the writer over the gate, and hand both to
`Run`: the overrun test, which observes both guarantees; a test that holds a
request open, cancels the context and at once calls `Deliver` on the gate,
finding it passed through to the sink, since the deadline is still a whole
drain away, then completes the request, lets `Run` return well before the
deadline, and finds that the sink behind the gate received
`service.stopping`, which a gate keyed to `Shutdown` rather than to the
deadline would have refused (R-HT8M-I3MS); and the test whose sink blocks until its context
is done, which calls `Deliver` on the gate after `Run` has returned and finds
it refused. Any other test may build the writer over its sink directly.

`Instructions` is nil on purpose: appkit then gives each client, as the
server's instructions, the `description` of dummy's own entry in the host's
services file, which opsctl copies from dummy's manifest. The description is
therefore written once, in the manifest, and no description string exists in
Go. `ServiceName` is the one declaration of the name and `cli.Version` the one
declaration of the version, the value `--version` prints; that is why
`cmd/dummy` imports `internal/panel` as well as `internal/cli`. The footer
every page ends with (`D04-panel`) shows those two values, and every MCP
result on the 2026-07-28 revision names them in its `serverInfo`, so both name
the release that is answering. The server writes nothing to standard error:
a tool that panics is a `tool.called` event with the outcome `panicked`.

What crosses the seam is not the kit but a function, the banner source,
`func(page.User) page.Banner`. A test passes a closure of its own that returns
whatever banner data the case needs, a launcher's services included, without a
services file and without touching the environment. The MCP server does cross
the seam as a value, because it has to: a server has no form a test could
fake, and the handler must register dummy's tools on it and mount it. A test
makes its own with `mcp.NewServer` after setting `IKIGENBA_SERVICES` with
`testing.T.Setenv`, as dummy's `AGENTS.md` asks. `internal/cli` hands both on
unchanged and never calls either itself.

That `main` hands appkit the right name and version and the right
`Instructions` is wiring, proved by the one exec'ing test against the running
binary: the page it serves opens with exactly the banner and ends with
exactly the footer that appkit's templates draw for the data a kit made with
`ServiceName` and `Version` over the same services file returns, which the
test computes by making such a kit itself after setting `IKIGENBA_SERVICES`
as dummy's `AGENTS.md` allows, so the comparison rests only on what appkit
promises for `page.New`, `Kit.Banner` and the two templates, never on how
appkit's markup is written; an
`mcp.Client` call to `list_widgets` over the socket succeeds and names the
same two values in its `serverInfo`; and a `server/discover` request answers
with dummy's description from the services file, read when the request is
answered, or with no instructions when the variable is unset. That `main`
hands the writer dummy's name and version, the process's standard error and
the socket sink, and hands the MCP server a writer that reaches telemetry, is
proved the same way (that the server's writer is the very one `Run` shuts
down is wiring no outside observer can tell from a second writer configured
alike, so it is guidance, not a requirement): with a services file whose `telemetry` entry names a socket the test
serves with appkit's `telemetry.IngestHandler`, the test's stand-in receives
`service.started` with dummy's version, the `tool.called` of its MCP call,
and `service.stopping`, and the binary writes nothing to either stream; with
the variable unset, every event the binary records reaches its standard error
as an `undelivered event` line. `mcp.Client`
offers only `ListTools` and `CallTool`, so the `server/discover` request is
the one the test sends as raw HTTP, which dummy's `AGENTS.md` admits for what
the client cannot send.

## Packages

The module's root package, `dummy`, holds the embedded templates. dummy's
markup is the four human-authored `html/template` files in `assets/` —
`page.html`, `table.html`, `form.html` and `script.html` — which the build run
reads and never writes. Go's `embed` reaches only files at or below the
embedding package's directory, so neither `internal/panel` nor any other
package under `internal/` can embed `assets/`; the root package, the directory
holding `go.mod`, is the one that can. It exports `Assets`, a file
system holding exactly those four files and nothing else, each embedded from
the file of the same name in `assets/`, and `internal/panel`
parses them from it into the set `page.Templates()` returns.

The root package also carries the database's schema, for the same reason: the
migrations are files in `migrations/` beside `assets/`, which only the root
package can embed. It exports `Migrations`, a file system holding exactly the
one migration this dummy carries, `0001_widgets.sql`, the baseline that
creates the `widgets` table (`D05-widgets` declares the schema). That file
system is what `Run` hands `db.Open` and `db.Status`, and what a test hands
`db.Open` to make a database of dummy's shape in its own temporary directory.
Like `Assets`, it is the same on every call and depends on no file beside the
binary. A later migration is a new file there with the next four-digit
version; none is ever edited once released. Nothing else lives at the root.

Five internal packages hold the concerns, each one concern and each small
enough for a reader to hold whole. `internal/cli` owns the program as a
command: the version, the manifest text, the usage text, the exit codes, the
run seam, the argument handling that `D02-cli` specifies, and the serve path
that `D03-serve` specifies — reading the drain deadline, taking the socket the
host passes in, opening the database, building the process's widget store
over it and the handler, handing them on, telling systemd it is ready, and opening and closing the trail with the
writer's `Ready` and `Shutdown`. `internal/server` owns the listener's
life and nothing else — it is handed a context, a listener, a handler, a
drain deadline and a function to call when the drain is over, and `D03-serve`
says what it does with them; it knows nothing of widgets, routes, identity,
HTML, MCP, telemetry, sockets passed in or systemd.
`internal/panel` owns dummy's whole HTTP surface: the one handler, wrapped in
`identity.Require` and that in `telemetry.Middleware`, routing, the page frame the appkit banner is drawn in,
rendering, the failure shapes, the table fragment, the form, the mounting of
appkit's shared files under `/_appkit/`, and the mounting of the MCP server at
`/mcp` with dummy's tools registered on it (`D04-panel`, `D06-table`,
`D07-form`, `D08-assets`). `internal/tools` owns dummy's two MCP tools,
`list_widgets` and `create_widget`: their names, descriptions, effects,
argument and result shapes, the text of a rule offence, and the
`widget.created` a creation records (`D09-mcp`).
`internal/widget` owns the domain: the widget entity, the status enumeration,
the store over the database, the ids it gives widgets, and the rules a new widget must
meet, shared by the form and the tools (`D05-widgets`).

Dependencies point one way. `cmd/dummy` imports `internal/cli`,
`internal/panel` (for `ServiceName`), and appkit's `page`, `mcp` and
`telemetry`. `internal/cli` imports the root package `dummy` (for `Migrations`),
`internal/server`, `internal/panel`, `internal/widget`, and appkit's `db`,
`page`, `mcp` and `telemetry` (whose types `Process` names). `internal/panel` imports the root package `dummy`,
`internal/widget`, `internal/tools`, and appkit's `page`, `identity`, `mcp`
and `telemetry`. `internal/tools` imports `internal/widget` and appkit's
`mcp`, `identity` and `telemetry`. `internal/widget` imports appkit's `db`
and nothing else of appkit's — its `Status` satisfies `mcp.Enumerator`
structurally — and `internal/server` and the root package import nothing of
dummy's or appkit's. Nothing imports `internal/cli` but `cmd/dummy`. There is no cycle,
and the packages a test most wants to drive on their own — the listener's life
and the domain — depend on nothing. Which package imports which is not
observable, so this paragraph is guidance, not a requirement.

The handler lives in `internal/panel` rather than `internal/server` because it
is not a pure function of method and path: it renders several page shapes and
a fragment, holds a store and mounts the MCP server, so left in
`internal/server` it would be that package's whole mass, under a name that
means transport. `internal/server` is nonetheless kept as a package holding
only the serve call, because a listener's life — the graceful drain, the
silenced server diagnostics, returning non-nil only on a real failure — is a
separate concern, separately testable with a fake listener and a handler that
panics. `internal/widget` is separate from `internal/panel` and
`internal/tools` because the rules that accept or reject a widget are
decidable without HTTP or MCP and are the part of dummy most worth testing on
its own; both ways in apply the same rules by calling it. `internal/tools` is
separate from `internal/panel` because the tools' schemas, descriptions and
offence texts are a contract with models, not with browsers, and are proved
through `mcp.Client` rather than by reading HTML. An interface between the
panel and the store was considered and rejected: there is one store and there
will be one, so an interface would be a name with no second member.

## Declarations and the checkout

The version is a `var` in `internal/cli`. Its value is data: a requirement
fixes the name, that it is a `var`, and its shape, and nothing else. The
manifest is a constant in the same package holding exactly the text the
bootstrap story shows, with a trailing newline: dummy's name; its description,
the one line the host publishes in its services file and dummy's MCP endpoint
gives its clients as instructions; that it is not the host's default app; that
it offers an MCP endpoint, so the platform's MCP gateway may reach it; no
secrets; after an empty line, a `[database]` table declaring the SQLite
database at `state/dummy.db`, which the host replicates; and, after another
empty line, a `[resources]` table capping its memory at 64M. The path there and
the path `Run` opens are the same relative path, resolved against the
working directory the host starts dummy in. It declares no port, because dummy serves on the socket the host
passes it and a manifest carrying `port` is refused by `devctl build` and by
opsctl. The usage text is a constant in that package too, declared here so
that every name `internal/cli` exports is declared in one place; its value
belongs to `D02-cli`, because the help output is that design's subject, and it
is fixed there byte for byte.

The package story lists exactly three members in the release file —
`bin/dummy`, `etc/manifest.toml` and `share/icon.svg`. `share/icon.svg` is
dummy's icon, an SVG image a human draws, and its presence in the package is
what lists dummy in the platform's launcher on a space. It is a human-authored
input like `assets/`, never written by the build run. `etc/manifest.toml` is
not: the build run writes it, holding exactly the text of the `Manifest`
constant. The database is not in the package either: dummy creates it on its
first start (`D03-serve`). Everything dummy serves is inside the binary — its
templates, embedded by the root package, and appkit's embedded files alike —
and `D04-panel`'s rule about the working directory is what shows that no
answer depends on a file beside the binary.

## The run seam

The run seam is `cli.Run`. It takes a context and a `cli.Process` value that
carries everything the program would otherwise take from the `os` package or
from the two environment-reading constructors: the arguments without the
program name, an environment lookup, a way to remove a variable from the
environment, the process's own id, the two output streams, a way to turn an
inherited file descriptor into a listener, the banner source, the MCP
server, the telemetry writer, the gate that writer delivers through, the
random source widget ids are drawn from, the directory the database path is
resolved against, and the clock that stamps applied migrations. Its return
value is the process exit code. `main` fills the `Process`
from the real process and cancels the context on `SIGTERM` or `SIGINT`; a test
fills it with buffers, a map, a pid of its choosing, a listener it made
itself, a banner source of its own, a server it made, a temporary directory
of its own as `Dir` and a clock it controls as `Now`, and cancels the
context itself. `Run`'s behaviour when it serves with a nil `Banner`, a nil
`MCP` or a nil `Telemetry` is not contract, so a test that serves supplies
all three; a nil `Gate`, or a `Telemetry` not built over `Gate`, only takes
away the two guarantees `D03-serve` states of the gate; and an `MCP` that
already has tools registered or has already served is not one `Run` can serve
with, since the handler registers dummy's tools on it (`D04-panel`). Nothing
below `main` reads `os.Args`, the real environment, the real pid or the real
streams, changes the real environment, calls `page.New` or `mcp.NewServer`, or
installs a signal handler, so a test that drives `Run` sees the whole
program's behaviour and nothing leaks past it. The one writer below the seam
that could reach the real standard error on its own, the HTTP server's
diagnostics that `net/http` would send through the `log` package, is silenced:
`D03-serve` requires that neither `Serve` nor `Run` writes to the `log`
package's default logger.

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
There is no listen factory and no readiness callback in the seam: dummy binds
nothing, so there is no bind to fake, and the datagram is the readiness
signal.

`Process` carries no store and no database handle. The widgets live in the
database `Run` opens at `state/dummy.db` resolved against `Process.Dir`, the
way sites and scripts place their catalogs; an empty `Dir` is the process's
working directory, and a test hands in a temporary directory of its own, so
two `Run`s given the same `Dir` share their widgets and two given different
ones do not. There is no field naming another database: appkit's `db` takes
only a file path, so the in-memory catalog a sibling's `Database` field
exists for has no counterpart here. `Process.Now` is the clock `Run` passes
to `db.Open` to stamp the migrations it applies, nil meaning `time.Now`, so a
test knows the time a fresh database records. `Process.Rand` is the source
`Run` builds its store over (`D05-widgets`), so a test that hands in known
bytes knows the ids of the widgets the run creates, and nil means
`crypto/rand.Reader`. The writer's own randomness, which the request
middleware mints request ids from, is the writer's `Config.Rand`, set by
whoever builds the writer. What a client of a serving `Run` sees is one set of
widgets, the ones the database holds, shared by the pages and the MCP tools,
so a widget created either way is seen by the other at once and by every
later `Run` over the same database; a test that wants a store it has filled
builds a handler over a store of its own directly (`D04-panel`). What
`Run` does once it has the socket — readiness, serving, the drain and how it
ends — is `D03-serve`, stated as what a client and the streams see; what the
handler answers is `D04-panel` and the designs it leads to.

## REQUIREMENTS

- R-2YS2-0A31: The `dummy` binary MUST behave as `cli.Run` does when given the binary's arguments after the program name, the process's environment, its process id, and its standard output and standard error, with an empty `Dir` and a nil `Now`, and MUST exit with the value `Run` returns.
- R-K1I1-7X3J: When the `dummy` binary is serving and receives `SIGTERM` or `SIGINT`, it MUST stop as `Run` does when its context is cancelled with a cause whose `Error` method returns `SIGTERM` or `SIGINT` respectively.
- R-HVOF-9N46: When the `dummy` binary is serving with `IKIGENBA_SERVICES` unset or naming a services file, the body of its answer to a `GET /widgets` request carrying a non-empty `X-User-Id` header MUST contain, beginning immediately after the `>` of its first `body` start tag, as `D04-panel` defines start tags and end tags (R-LPDH-LA2H), with nothing but ASCII whitespace between them, the text that executing the template `banner` of a set `page.Templates()` returns writes for `b`, and, ending immediately before the `<` of its last `</body>` end tag with nothing but ASCII whitespace between them, the text that executing the template `footer` of such a set writes for `b`, where `b` is the `page.Banner` that the `Banner` method of the `Kit` `page.New(panel.ServiceName, Version)` returns, in a process whose `IKIGENBA_SERVICES` has the same value, returns for the banner user (`D04-panel` R-YV2Y-1CAU) of that request while the services file holds what it held when the request was answered.
- R-E051-E4GO: When the `dummy` binary is serving, a `CallTool` call for the tool `list_widgets` with nil `args`, made by an appkit `mcp.Client` whose requests reach the socket the binary serves on with the URL path `/mcp`, on behalf of an `identity.Caller` whose `UserID` is not empty, MUST return a nil error and a `Result` whose `IsError` is false and whose `MarshalJSON` output is an object with a member `_meta` whose member `io.modelcontextprotocol/serverInfo` is exactly the JSON object `{"name":<n>,"version":<v>}`, where `<n>` is the value of `panel.ServiceName` (`D04-panel`) and `<v>` the value of `Version`, each as a JSON string.
- R-E3SQ-JFOR: When the `dummy` binary is serving with `IKIGENBA_SERVICES` naming a services file, a `server/discover` request POSTed over the socket the binary serves on to the URL path `/mcp` with a non-empty `X-User-Id` header, `Content-Type: application/json`, `MCP-Protocol-Version` and `Mcp-Method` headers equal to `mcp.ProtocolVersion` and `server/discover`, and a body whose `params._meta` holds `io.modelcontextprotocol/protocolVersion` equal to `mcp.ProtocolVersion` and `io.modelcontextprotocol/clientCapabilities` equal to `{}`, MUST be answered with status 200 and a JSON-RPC result whose `instructions` member is exactly the `Description` of the entry that appkit's `List.Find` returns for `panel.ServiceName` in what `services.Read` returns for that file at the time the request is answered, whenever that `Description` is not empty, so that rewriting the file's description between two such requests changes the second answer.
- R-E68J-AZ65: When the `dummy` binary is serving with `IKIGENBA_SERVICES` unset, a `server/discover` request as R-E3SQ-JFOR describes it MUST be answered with status 200 and a JSON-RPC result that has no `instructions` member.
- R-K2PX-LOU8: When the `dummy` binary starts with `IKIGENBA_SERVICES` naming a services file whose entry named `telemetry` (`telemetry.ServiceName`) has as its `Socket` a Unix socket on which a server answers with the handler `telemetry.IngestHandler` returns over a sink that answers every `Deliver` call with a nil error, the first event that sink receives MUST be a `service.started` event whose `Service` is the value of `panel.ServiceName`, whose `RequestID` and `User` are empty, and whose `Attrs` hold exactly the key `version`, whose value is the value of `Version`.
- R-K3XT-ZGKX: When the `dummy` binary is serving as R-K2PX-LOU8 describes, a `CallTool` call for `list_widgets` made as R-E051-E4GO describes, on behalf of an `identity.Caller` whose `UserID` and `RequestID` are not empty, MUST cause that sink to receive a `tool.called` event whose `RequestID` and `User` are that `Caller`'s `RequestID` and `UserID` and whose `tool` attribute is `list_widgets`.
- R-K55Q-D8BM: When the `dummy` binary serves as R-K2PX-LOU8 describes and is stopped by `SIGTERM` or `SIGINT` with no request being handled, it MUST write nothing to its standard output or its standard error from its start to its exit, and the last event that sink receives MUST be a `service.stopping` event.
- R-K7LJ-4RT0: When the `dummy` binary serves with `IKIGENBA_SERVICES` absent from its environment and is stopped by `SIGTERM` or `SIGINT` with no request being handled, it MUST write nothing to its standard output, and everything it writes to its standard error MUST be lines each consisting of `dummy: undelivered event: `, a JSON object, and a newline, of which the first holds the member `event` with the value `service.started` and the last holds the member `event` with the value `service.stopping`.
- R-317U-RTKF: When the `dummy` binary, started from a working directory that holds no entry, has served as R-K2PX-LOU8 describes and has exited after `SIGTERM`, that working directory MUST hold a regular file `state/dummy.db`, so that the database the binary keeps its widgets in is `state/dummy.db` under its working directory.
- R-E5L5-F828: The module's root package, imported from the path `github.com/ikigenba/ikigenba/dummy` with the package name `dummy`, MUST export `func Assets() fs.FS`, where `fs` is the standard library's `io/fs`, returning a file system whose root directory holds exactly the regular files `page.html`, `table.html`, `form.html` and `script.html` and no other entry.
- R-DNY1-KF1Q: Every call to `dummy.Assets` MUST return a file system holding the same four files with the same contents, whatever the process working directory is, a directory that holds no `assets/` directory included.
- R-2TWG-H749: The module's root package `dummy` MUST export `func Migrations() fs.FS`, where `fs` is the standard library's `io/fs`, returning a file system whose root directory holds exactly the regular file `0001_widgets.sql` and no other entry.
- R-2V4C-UYUY: Every call to `dummy.Migrations` MUST return a file system holding the same one file with the same contents, whatever the process working directory is, a directory that holds no `migrations/` directory included.
- R-JWQT-BFRO: The `internal/cli` package MUST export `var Version string`.
- R-KHH3-TJDH: `Version` MUST be the letter `v` followed by a valid Semantic Versioning version as defined at semver.org, prerelease and build metadata included when present.
- R-SCNW-L802: The `internal/cli` package MUST export `const Manifest = "app = \"dummy\"\ndescription = \"Demo widgets to list and create\"\ndefault = false\nmcp = true\nsecrets = []\n\n[database]\nengine = \"sqlite\"\npath = \"state/dummy.db\"\n\n[resources]\nmemory_max = \"64M\"\n"`.
- R-F2D6-M8QD: The `internal/cli` package MUST export `Usage` as a string constant, holding the usage text whose value `D02-cli` fixes.
- R-2WC9-8QLN: The `internal/cli` package MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Unsetenv func(key string) error; Pid int; Stdout io.Writer; Stderr io.Writer; Inherit func(fd uintptr) (net.Listener, error); Banner func(u page.User) page.Banner; MCP *mcp.Server; Telemetry *telemetry.Writer; Gate *Gate; Rand io.Reader; Dir string; Now func() time.Time }` and `func Run(ctx context.Context, p Process) int`, where `page`, `mcp` and `telemetry` are the packages `github.com/ikigenba/ikigenba/appkit/page`, `github.com/ikigenba/ikigenba/appkit/mcp` and `github.com/ikigenba/ikigenba/appkit/telemetry` and `time` is the standard library's `time`, `Args` excludes the program name, `Pid` is the id of the process `Run` speaks for, a nil `Unsetenv` means `Run` removes no variable, `Banner` is the source of the banner data every page the handler draws with the banner is drawn from, `MCP` is the server the handler registers dummy's tools on and mounts at `/mcp`, `Telemetry` is the writer every event the handler records goes through and on which `Run` calls `Ready` and `Shutdown`, `Gate` is the gate (R-HS0Q-4BW3) that `Telemetry` delivers its events through, `Rand` is the source of the ids of the widgets `Run` creates, nil meaning `crypto/rand.Reader`, `Dir` is the directory against which `Run` resolves the path `state/dummy.db` of its database, empty meaning the process working directory, and `Now` is the clock `Run` stamps the migrations it applies to that database with, nil meaning the standard library's `time.Now`.
- R-HS0Q-4BW3: The `internal/cli` package MUST export `type Gate struct`, with no exported field, `func NewGate(next telemetry.Sink) *Gate`, and `func (g *Gate) Deliver(ctx context.Context, e telemetry.Event) error`, so that `*Gate` implements `telemetry.Sink`, where `telemetry` is the package `github.com/ikigenba/ikigenba/appkit/telemetry`; the sink passed as `next` is that gate's **next sink**.
- R-HT8M-I3MS: Every `Deliver(ctx, e)` call on a `*Gate` that `NewGate(next)` returned MUST call the `Deliver` method of its next sink exactly once, with `ctx` and `e`, and return the error that call returns, unless the call begins after the drain deadline (`D03-serve` R-PE6U-PF31) of a `Run` that was given that gate as `p.Gate` has elapsed since that `Run`'s `ctx` was done.
- R-EBJE-7AF3: The `internal/cli` package MUST export `ExitSuccess`, `ExitServerFailed` and `ExitUsage` as constants, so that each of the three names is usable as an operand of a constant expression — the initializer of a `const` declaration in a package that imports `internal/cli` included — and their constant values MUST be 0, 1 and 2 respectively.
- R-L27E-BMZA: `Run` MUST return one of `ExitSuccess`, `ExitServerFailed`, or `ExitUsage`, and no other value.
- R-K8TF-IJJP: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration, stop func(ctx context.Context)) error`.
- R-LMVZ-EMSG: The `internal/server` package MUST export `type DrainError struct { Unfinished int }` and `func (e *DrainError) Error() string`.

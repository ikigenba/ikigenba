# D01-layout-and-run-seam

dummy is one Go binary that serves a small control panel, and offers the same
widgets to MCP clients at `/mcp`. This design is the structural ground the
other designs stand on: where the code lives, which package exports which
name, what the module depends on, how the version, the manifest and the usage
text are declared, and the seam through which the process is run so that the
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
`github.com/ikigenba/ikigenba/appkit`, the platform's shared library, fetched
through the ordinary Go module proxy like any published module, with no
`replace` directive pointing into the repository. Which release is data:
`go.mod` requires appkit `v0.5.0`, the first release with the packages below,
and no requirement names a version. appkit is a sibling sub-project, so dummy
reaches it only through its published packages and their documented contracts,
and uses three of them:

- `page` (`github.com/ikigenba/ikigenba/appkit/page`) — the page chrome:
  `page.New(service, version)` makes a `*page.Kit` whose `Banner(page.User)
  page.Banner` method yields the data the banner and footer are drawn from;
  `page.Templates()` is a fresh template set defining `banner`, `launcher` and
  `footer`, into which dummy parses its own templates; `page.Static()` serves
  the shared stylesheet, fonts, licences and launcher script under
  `page.StaticPrefix` (`/_appkit/`).
- `identity` (`github.com/ikigenba/ikigenba/appkit/identity`) —
  `identity.Require(app, stderr, next)` wraps dummy's whole handler, so every
  request reaches dummy's routes and `/mcp` only with an `X-User-Id`, and is
  otherwise answered 500 with `identity.MissingBody` and one stderr line.
- `mcp` (`github.com/ikigenba/ikigenba/appkit/mcp`) —
  `mcp.NewServer(mcp.ServerConfig{...})` makes the `*mcp.Server` dummy mounts
  at `/mcp` and registers its tools on with `mcp.AddTool`; `mcp.Client` is how
  dummy's tests drive it.

appkit owns the banner's, the footer's and the launcher's markup, the
launcher's script, the stylesheet, the fonts and their licences, the MCP
transport and the reading of the services file; dummy authors none of them and
carries no copy.

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
never stop dummy starting. `mcp.NewServer` panics only on an empty name, and
dummy's name is the constant `panel.ServiceName`.

`main` builds:

- the kit, `page.New(panel.ServiceName, cli.Version)`, and passes its `Banner`
  method as `Process.Banner`;
- the server, `mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName,
  Version: cli.Version, Stderr: os.Stderr})`, with `Instructions` nil, and
  passes it as `Process.MCP`.

`Instructions` is nil on purpose: appkit then gives each client, as the
server's instructions, the `description` of dummy's own entry in the host's
services file, which opsctl copies from dummy's manifest. The description is
therefore written once, in the manifest, and no description string exists in
Go. `ServiceName` is the one declaration of the name and `cli.Version` the one
declaration of the version, the value `--version` prints; that is why
`cmd/dummy` imports `internal/panel` as well as `internal/cli`. The footer
every page ends with (`D04-panel`) shows those two values, and every MCP
result on the 2026-07-28 revision names them in its `serverInfo`, so both name
the release that is answering. The server's own diagnostics (a tool that
panics, say) go straight to `os.Stderr`, one line per write; dummy's tools are
not expected to produce any.

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
binary: the page it serves carries the launcher when the services file lists
services with icons and a footer naming `ServiceName` and `Version`; an
`mcp.Client` call to `list_widgets` over the socket succeeds and names the
same two values in its `serverInfo`; and a `server/discover` request answers
with dummy's description from the services file, read when the request is
answered, or with no instructions when the variable is unset. `mcp.Client`
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
holding `go.mod`, is the one that can. It exports one name, `Assets`, a file
system holding exactly those four files and nothing else, and `internal/panel`
parses them from it into the set `page.Templates()` returns. Nothing else
lives at the root.

Five internal packages hold the concerns, each one concern and each small
enough for a reader to hold whole. `internal/cli` owns the program as a
command: the version, the manifest text, the usage text, the exit codes, the
run seam, the argument handling that `D02-cli` specifies, and the serve path
that `D03-serve` specifies — reading the drain deadline, taking the socket the
host passes in, building the process's widget store and handler, handing them
on, and telling systemd it is ready. `internal/server` owns the listener's
life and nothing else — it is handed a context, a listener, a handler and a
drain deadline, and `D03-serve` says what it does with them; it knows nothing
of widgets, routes, identity, HTML, MCP, sockets passed in or systemd.
`internal/panel` owns dummy's whole HTTP surface: the one handler, wrapped in
`identity.Require`, routing, the page frame the appkit banner is drawn in,
rendering, the failure shapes, the table fragment, the form, the mounting of
appkit's shared files under `/_appkit/`, and the mounting of the MCP server at
`/mcp` with dummy's tools registered on it (`D04-panel`, `D06-table`,
`D07-form`, `D08-assets`). `internal/tools` owns dummy's two MCP tools,
`list_widgets` and `create_widget`: their names, descriptions, effects,
argument and result shapes and the text of a rule offence (`D09-mcp`).
`internal/widget` owns the domain: the widget entity, the status enumeration,
the in-memory store, and the rules a new widget must meet, shared by the form
and the tools (`D05-widgets`).

Dependencies point one way. `cmd/dummy` imports `internal/cli`,
`internal/panel` (for `ServiceName`), and appkit's `page` and `mcp`.
`internal/cli` imports `internal/server`, `internal/panel`, `internal/widget`,
and appkit's `page` and `mcp` (whose types `Process` names). `internal/panel`
imports the root package `dummy`, `internal/widget`, `internal/tools`, and
appkit's `page`, `identity` and `mcp`. `internal/tools` imports
`internal/widget` and appkit's `mcp` and `identity`. `internal/widget` imports
nothing of appkit's — its `Status` satisfies `mcp.Enumerator` structurally —
and `internal/server` and the root package import nothing of dummy's or
appkit's. Nothing imports `internal/cli` but `cmd/dummy`. There is no cycle,
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
manifest is a constant in the same package holding exactly the five lines the
bootstrap story shows, with a trailing newline: dummy's name; its description,
the one line the host publishes in its services file and dummy's MCP endpoint
gives its clients as instructions; that it is not the host's default app; that
it offers an MCP endpoint, so the platform's MCP gateway may reach it; and no
secrets. It declares no port, because dummy serves on the socket the host
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
constant. Everything dummy serves is inside the binary — its
templates, embedded by the root package, and appkit's embedded files alike —
and `D04-panel`'s rule about the working directory is what shows that no
answer depends on a file beside the binary.

## The run seam

The run seam is `cli.Run`. It takes a context and a `cli.Process` value that
carries everything the program would otherwise take from the `os` package or
from the two environment-reading constructors: the arguments without the
program name, an environment lookup, a way to remove a variable from the
environment, the process's own id, the two output streams, a way to turn an
inherited file descriptor into a listener, the banner source, and the MCP
server. Its return value is the process exit code. `main` fills the `Process`
from the real process and cancels the context on `SIGTERM` or `SIGINT`; a test
fills it with buffers, a map, a pid of its choosing, a listener it made
itself, a banner source of its own and a server it made, and cancels the
context itself. `Run`'s behaviour when it serves with a nil `Banner` or a nil
`MCP` is not contract, so a test that serves supplies both, and an `MCP` that
already has tools registered or has already served is not one `Run` can serve
with, since the handler registers dummy's tools on it (`D04-panel`). Nothing
below `main` reads `os.Args`, the real environment, the real pid or the real
streams, changes the real environment, calls `page.New` or `mcp.NewServer`, or
installs a signal handler, so a test that drives `Run` sees the whole
program's behaviour and nothing leaks past it. The one writer below the seam
that could reach the real standard error on its own, the HTTP server's
diagnostics that `net/http` would send through the `log` package, is silenced
by `D03-serve`: `Serve` discards the server's own diagnostics.

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
listener to `server.Serve` together with the handler `panel.Handler` builds
over that store, `Process.Banner`, `Process.MCP` and a writer for the
handler's diagnostics, and the drain deadline. Making the store there, and
after the socket is taken, puts "the widget set is created once, at process
start, and dies with the process" at the one place that happens, keeps a start
that fails from building anything, and keeps `--version` from building a store
it will never use; it also lets a test build a handler over a store it has
already filled, a banner source it wrote and a server it made. The page routes
and the MCP tools are built over that one store, so a widget created either
way is seen by the other at once. The handler is handed the writer its
per-request diagnostic goes to — the missing-identity line `identity.Require`
writes, the only line the handler writes (`D04-panel`) — because the handler
is the one that knows which request it was. `Serve` owns the listener from then on and returns
when the drain after the context is cancelled has ended or the server fails;
when the drain deadline passes with requests still running it returns a
`server.DrainError` counting them, which `Run` reports like any other serve
failure. What `Serve` does between those points is `D03-serve`, what the
handler answers is `D04-panel` and the designs it leads to, and the hand-off
itself — drain deadline, socket, store, banner source, MCP server, readiness,
serve — is `D03-serve` too.

## REQUIREMENTS

- R-49MF-7KF1: The `dummy` binary MUST behave as `cli.Run` does when given the binary's arguments after the program name, the process's environment, its process id, and its standard output and standard error, and MUST exit with the value `Run` returns.
- R-4UCP-PO0U: When the `dummy` binary is serving and receives `SIGTERM` or `SIGINT`, it MUST stop as `Run` does when its context is cancelled.
- R-DXP8-MKZA: When the `dummy` binary starts with `IKIGENBA_SERVICES` naming a services file in which appkit's `services.Read` finds one or more entries whose `HasIcon` is true, the page it serves in answer to a `GET /widgets` request carrying a non-empty `X-User-Id` header MUST carry the appkit launcher: a `button` start tag whose `class` is `launcher`.
- R-JC0I-TC5V: When the `dummy` binary is serving with `IKIGENBA_SERVICES` unset or naming a services file no part of which contains the sequence `footer` compared case-insensitively, the page it serves in answer to a `GET /widgets` request carrying a non-empty `X-User-Id` header MUST, read as a whole body, contain exactly one `footer` start tag, as `D04-panel` defines start tags and end tags (R-LPDH-LA2H), and the normalisation (`D04-panel` R-NGS9-HCML) of the characters from that start tag's `>` up to the `<` of the first `</footer>` end tag following it MUST be exactly the value of `panel.ServiceName` (`D04-panel`), one space, and the value of `Version`.
- R-E051-E4GO: When the `dummy` binary is serving, a `CallTool` call for the tool `list_widgets` with nil `args`, made by an appkit `mcp.Client` whose requests reach the socket the binary serves on with the URL path `/mcp`, on behalf of an `identity.Caller` whose `UserID` is not empty, MUST return a nil error and a `Result` whose `IsError` is false and whose `MarshalJSON` output is an object with a member `_meta` whose member `io.modelcontextprotocol/serverInfo` is exactly the JSON object `{"name":<n>,"version":<v>}`, where `<n>` is the value of `panel.ServiceName` (`D04-panel`) and `<v>` the value of `Version`, each as a JSON string.
- R-E3SQ-JFOR: When the `dummy` binary is serving with `IKIGENBA_SERVICES` naming a services file, a `server/discover` request POSTed over the socket the binary serves on to the URL path `/mcp` with a non-empty `X-User-Id` header, `Content-Type: application/json`, `MCP-Protocol-Version` and `Mcp-Method` headers equal to `mcp.ProtocolVersion` and `server/discover`, and a body whose `params._meta` holds `io.modelcontextprotocol/protocolVersion` equal to `mcp.ProtocolVersion` and `io.modelcontextprotocol/clientCapabilities` equal to `{}`, MUST be answered with status 200 and a JSON-RPC result whose `instructions` member is exactly the `Description` of the entry that appkit's `List.Find` returns for `panel.ServiceName` in what `services.Read` returns for that file at the time the request is answered, whenever that `Description` is not empty, so that rewriting the file's description between two such requests changes the second answer.
- R-E68J-AZ65: When the `dummy` binary is serving with `IKIGENBA_SERVICES` unset, a `server/discover` request as R-E3SQ-JFOR describes it MUST be answered with status 200 and a JSON-RPC result that has no `instructions` member.
- R-DKAC-F3TN: The module's root package, imported from the path `github.com/ikigenba/ikigenba/dummy` with the package name `dummy`, MUST export `func Assets() fs.FS`, where `fs` is the standard library's `io/fs`, returning a file system whose root directory holds exactly the regular files `page.html`, `table.html`, `form.html` and `script.html` and no other entry, each embedded from the file of the same name in the module's `assets/` directory.
- R-DNY1-KF1Q: Every call to `dummy.Assets` MUST return a file system holding the same four files with the same contents, whatever the process working directory is, a directory that holds no `assets/` directory included.
- R-JWQT-BFRO: The `internal/cli` package MUST export `var Version string`.
- R-KHH3-TJDH: `Version` MUST be the letter `v` followed by a valid Semantic Versioning version as defined at semver.org, prerelease and build metadata included when present.
- R-DV9F-V1HW: The `internal/cli` package MUST export `const Manifest = "app = \"dummy\"\ndescription = \"Demo widgets to list and create\"\ndefault = false\nmcp = true\nsecrets = []\n"`.
- R-F2D6-M8QD: The `internal/cli` package MUST export `Usage` as a string constant, holding the usage text whose value `D02-cli` fixes.
- R-DSTN-3I0I: The `internal/cli` package MUST export `type Process struct { Args []string; LookupEnv func(key string) (string, bool); Unsetenv func(key string) error; Pid int; Stdout io.Writer; Stderr io.Writer; Inherit func(fd uintptr) (net.Listener, error); Banner func(u page.User) page.Banner; MCP *mcp.Server }` and `func Run(ctx context.Context, p Process) int`, where `page` and `mcp` are the packages `github.com/ikigenba/ikigenba/appkit/page` and `github.com/ikigenba/ikigenba/appkit/mcp`, `Args` excludes the program name, `Pid` is the id of the process `Run` speaks for, a nil `Unsetenv` means `Run` removes no variable, `Banner` is the source of the banner data every page the handler draws with the banner is drawn from, and `MCP` is the server the handler registers dummy's tools on and mounts at `/mcp`.
- R-EBJE-7AF3: The `internal/cli` package MUST export `ExitSuccess`, `ExitServerFailed` and `ExitUsage` as constants, so that each of the three names is usable as an operand of a constant expression — the initializer of a `const` declaration in a package that imports `internal/cli` included — and their constant values MUST be 0, 1 and 2 respectively.
- R-L27E-BMZA: `Run` MUST return one of `ExitSuccess`, `ExitServerFailed`, or `ExitUsage`, and no other value.
- R-LLO3-0V1R: The `internal/server` package MUST export `func Serve(ctx context.Context, ln net.Listener, h http.Handler, drain time.Duration) error`.
- R-LMVZ-EMSG: The `internal/server` package MUST export `type DrainError struct { Unfinished int }` and `func (e *DrainError) Error() string`.

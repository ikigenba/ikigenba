# D01-scope-and-layout

`appkit` is the shared library code of the Ikigenba services. Anything that more than one service needs — the page chrome every app shows a signed-in user, the reader for the host's services file, the identity nginx hands each request, the Model Context Protocol server and client — belongs here, so that every service gets it from one place and behaves the same way. The module path is `github.com/ikigenba/ikigenba/appkit`; it depends on the Go standard library only. Every command runs from this sub-project directory.

## One concern per package

The library is a set of packages, each holding one concern small enough to hold whole. No package is privileged: the module root holds no exported Go name (it may carry a package doc comment, which is not contract). A consumer imports only the packages it uses.

- `page` — the page chrome: the `banner`, `launcher`, and `footer` templates, the `Banner` value they render, and the shared stylesheet, fonts, and launcher script served from one fixed path prefix. D02 (the banner value), D03 (the templates), D04 (the static files).
- `services` — the one reader of the host's services file, the published format opsctl owns and names in the variable `IKIGENBA_SERVICES`. D05.
- `identity` — the caller nginx authenticated for a request: the middleware that requires it, the context that carries it, and forwarding it on a call to a sibling service. D06.
- `mcp` — the Model Context Protocol over Streamable HTTP: the server a service mounts with its tools, and the client the gateway and service tests use. D07-mcp-server, D08-mcp-tools, D09-mcp-schema, and D10-mcp-client define its contents.

Dependencies point one way: `page` uses `services`; `mcp` uses `identity` and `services`; `identity` and `services` use no other appkit package. Which package imports which is not observable, so this is guidance, not a requirement.

## Assets

`page` embeds the human-authored directory `page/assets/`: the markup template file `banner.html`, the launcher script `launcher.js`, and copies of the repository's design files (`theme.css`, the three fonts, and their licences). Go's embedding only reaches files in the package's own directory or below, which is why the assets sit inside `page/`. They are an input to the spec: the build run reads them and never writes them. Because they are embedded, a service binary carries them and never looks for them on disk.

## What every package promises

A library shared by every service must not surprise any of them. No package reads its own files from the working directory (D04 states this for `page`'s embedded assets), and none writes to the process's standard output or standard error or through the standard library's default logger: those streams belong to the service. When a package has something to report, it writes to an `io.Writer` the consumer passed in, so the service decides where its diagnostics go.

## Consumer tasks

These are the tasks the packages exist for, in outline; the documents named above hold the contract.

An app wires its pages. At start-up it builds a `page.Kit` with `page.New`, naming its service and release version; it parses its own page templates into the set `page.Templates` returns; it mounts `page.Static` at `page.StaticPrefix`. Its whole handler tree is wrapped in `identity.Require`, so each page handler takes the signed-in person from `identity.FromContext`, passes a `page.User` to `Kit.Banner`, and renders the page with the returned `page.Banner`. The page carries the banner, the launcher when the host lists services with icons, and the footer naming the app and its version.

A service offers tools to an MCP client. It builds an `mcp` server with its name and version, registers its tools, and mounts the server at `/mcp` on the same mux the pages use, the whole mux wrapped in `identity.Require`; each tool handler receives the caller explicitly (D07, D08).

The gateway finds the suite's MCP services. On each request it calls `services.Read` with the value of `services.Variable` from its environment, keeps the entries whose `MCP` is true, and reaches each one over its `Socket` with the `mcp` client (D10), which carries the caller to the backend with `identity.Forward`.

A service's own test drives its MCP tools end to end: it serves the server wrapped in `identity.Require` from a test server and calls a tool through the `mcp` client with an `identity.Caller` it makes up.

## REQUIREMENTS

- R-HLTX-LS8Q: Package `page` MUST be imported from the path `github.com/ikigenba/ikigenba/appkit/page`, and its package name MUST be `page`.
- R-HN1T-ZJZF: Package `services` MUST be imported from the path `github.com/ikigenba/ikigenba/appkit/services`, and its package name MUST be `services`.
- R-HO9Q-DBQ4: Package `identity` MUST be imported from the path `github.com/ikigenba/ikigenba/appkit/identity`, and its package name MUST be `identity`.
- R-HPHM-R3GT: Package `mcp` MUST be imported from the path `github.com/ikigenba/ikigenba/appkit/mcp`, and its package name MUST be `mcp`.
- R-YFJ3-L9UN: An exported function or method of packages `page`, `services`, `identity`, or `mcp` MUST NOT write to the process's standard output, to its standard error, or through the standard library `log` package's default logger, except through an `io.Writer` the consumer passed to that function or method or to the value it belongs to.
- R-9TRL-4U3V: Every exported function and method of packages `services`, `identity`, and `mcp`, and `page.New` and `Kit.Banner`, MUST behave identically whatever the process working directory is, apart from how a relative path the consumer supplies resolves.

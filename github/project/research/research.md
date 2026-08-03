# github — Research

Collected ground truth the design references. Non-contractual; the build loop
never reads it. Its subject is the **suite contracts `github` consumes but does
not own** — the correlation-id standard, appkit's shared outbound HTTP client,
and what the chassis records on the service's behalf. Each is quoted here so a
design Decision never has to go read another module's spec.

## The correlation id

- **`eventplane/correlation`** is the stdlib-only leaf package both appkit and
  eventplane import (neither could own it: eventplane must never import
  appkit, and the id must sit on one shared context key). Its surface:
  `Header = "X-Correlation-Id"`, `New() string` (mints a 26-character
  **Crockford** base32 ULID — alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ`),
  `Valid(id string) bool`, `WithContext(ctx, id) context.Context`,
  `FromContext(ctx) string` (empty when absent), and `Ensure(ctx)`.
- **The chassis is the read-or-mint point.** appkit's
  `logging.CorrelationMiddleware` (the renamed `RequestIDMiddleware`, which
  used to mint a *fresh* id per hop) trusts a well-formed inbound
  `X-Correlation-Id` verbatim — loopback callers are inside the trust boundary
  — and mints only when the header is absent or malformed. `github` therefore
  needs **no Go code** to participate in correlation.
- **The edge strips and mints.** The dashboard's introspection endpoints mint
  the id for gated routes and return it as a response header on the nginx
  `auth_request` subrequest. Each service's nginx fragment must capture it
  (`auth_request_set`) and `proxy_set_header X-Correlation-Id`, which
  **overwrites** anything the client sent; ungated public locations set the
  header to `""` so the service mints. A public caller can never inject an id.
- **`github`'s loopback twins inherit it for free.** `GET /pr` (`scripts`) and
  `GET /token` (`repos`) are reached over loopback without crossing nginx; the
  calling service's own outbound client puts the header on the wire (loopback
  peers are the one destination class it propagates to), and `github`'s chassis
  middleware reads it. So a `repos` session that fetches a git credential and
  then opens a PR is one chain end to end, with no work on `github`'s part.

## The shared instrumented outbound client

- **`appkit/httpclient`** returns an ordinary `*http.Client` whose transport
  records every round trip: `func New(Options) *http.Client` and
  `func NewTransport(Options) http.RoundTripper`, with
  `Options{Recorder *telemetry.Recorder; Timeout time.Duration; Base
  http.RoundTripper}`. A **nil `Recorder` yields a working client that records
  nothing**; the default `Timeout` is 30s and the default `Base` is
  `http.DefaultTransport`. Because it is a plain `*http.Client`, a service
  swaps what it passes and changes nothing else about how it makes requests —
  which is what lets `github` keep its existing `httpClient *http.Client`
  injection seam and its offline stub-`RoundTripper` tests untouched.
- **It records** `kind` `outbound`, `op` `<METHOD> <host><path>` (no query
  string — third-party query strings routinely carry tokens), the numeric
  status, duration, and streaming size+SHA-256 digests of both bodies. A
  transport failure still records, carrying an error **class**
  (`timeout`, `connection_refused`, `dns`), never the raw error text.
- **It propagates `X-Correlation-Id` only to loopback IP literals**
  (`127.0.0.0/8`, `::1`) — deliberately not even to the *name* `localhost`.
  Calls to `api.github.com` therefore never carry a suite-internal identifier
  off the box.
- **Reaching the recorder from a service.** appkit constructs the recorder in
  `runServe`; a service's `Spec.Handlers` hook reads it from the runtime
  (`rt.Recorder()`, nil when telemetry is disabled) and hands it to
  `httpclient.New`. *(This accessor is the one piece of the seam still being
  settled in appkit's spec at the time of writing — `github`'s Decision names
  it, and the phase that realizes it must build after appkit's.)*

## What `github`'s outbound traffic looks like today

Established by reading the module (2026-08-03):

- `internal/gh/client.go` — `Client.client()` returns the injected
  `*http.Client`, else the token source's, else **`http.DefaultClient`**.
- `internal/gh/token.go` — `tokenSource.client()` returns the injected client,
  else **`http.DefaultClient`**. This is the path that signs the RS256 app JWT
  and exchanges it for an installation token against `api.github.com`.
- `internal/githubapp/spec.go` — the composition root passes `nil` in **both**
  the `Config` hook (which constructs a client only to validate that
  `IKIGENBA_APP_PRIVATE_KEY` parses; it issues no request) and the `Handlers`
  hook (the real one).
- Consequence: every GitHub call runs on `http.DefaultClient`, whose zero
  `Timeout` means **no timeout at all** — a hung GitHub response parks the
  caller indefinitely, and `prompts`, `scripts`, and `repos` all block on this
  connector.

## Recording `github` gets for free

Inbound MCP tool calls (`kind` `request`, `op` `mcp:<tool>`, actor from the
nginx identity headers, arguments captured under a 1024-byte per-value and
8192-byte per-record budget), plain HTTP requests including the loopback `/pr`
and `/token` twins, and `lifecycle` `start`/`stop` records are all instrumented
**inside** the chassis — `mcp.Handler.dispatchTool`, the `server` middleware
chain, and `runServe`. `github` adopts them by recompiling against the new
appkit; it writes no recording code and re-verifies none of that behavior.

`github` is **not** an event-plane producer or consumer (no `/feed`, no
outbox), so eventplane's `Append`-takes-a-context change and the `publish` /
`consume` record kinds do not touch this module at all.

## `github` is not a chain root, verified

The settled suite rule makes any **background poll/watch/sync cycle** that
publishes events or makes outbound calls with no inbound request to inherit
from a chain root — one root id per cycle, emitted through appkit's root-start
helper. That rule names gmail's poll loop, dropbox's sync cycle, and wiki's
self-started pipeline work, alongside cron ticks and `prompts`/`scripts` run
spawns.

**`github` has no such cycle.** Verified against the source (2026-08-03): its
`appkit.Spec` declares no `Workers`, and no package under `internal/` or
`cmd/` starts a goroutine, ticker, or timer. Every GitHub call this connector
makes is driven by an inbound request — an MCP tool call, or a loopback `GET
/pr` / `GET /token` from `scripts` or `repos` — and therefore inherits a
correlation id the chassis middleware already put on the request context.
`github` mints nothing and needs no root-start call. Should a background
refresher ever be added here, the rule applies to it and this fact must be
revisited.

## Options evaluated and not chosen

- **A `github`-local recording `RoundTripper`** — would work and would need no
  cross-workspace dependency, but the loopback-only correlation rule would then
  have eight independent implementations across the suite to get wrong, and the
  record shape would drift per service. The shared transport is the point.
- **Reading the correlation id in `github` and attaching it to GitHub
  requests** — actively harmful: it exports a suite-internal chain identifier
  into a third party's logs. The transport's loopback-only rule exists to make
  this impossible by construction.

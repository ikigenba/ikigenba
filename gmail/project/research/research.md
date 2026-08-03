# gmail — Research

Collected ground truth the design references, from two sources: the **Gmail
API** (external), and the **suite contracts** gmail consumes but does not own
(appkit, eventplane, the correlation-id standard). Non-contractual; the build
loop never reads this. Gmail API facts below were established against the
**live** API (the deployed connector, 2026-07-12) or from Google's API
reference, as noted.

## Consumed suite facts: telemetry and correlation

These are owned by other workspaces' specs. gmail cites them and designs none
of their internals; each is quoted here so a design Decision never has to go
read another module's spec.

### The correlation id

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
  — and mints only when the header is absent or malformed. gmail therefore
  needs **no Go code** to participate in correlation.
- **The edge strips and mints.** The dashboard's introspection endpoints mint
  the id for gated routes and return it as a response header on the nginx
  `auth_request` subrequest. Each service's nginx fragment must capture it
  (`auth_request_set`) and `proxy_set_header X-Correlation-Id`, which
  **overwrites** anything the client sent; ungated public locations set the
  header to `""` so the service mints. A public caller can never inject an id.

### The shared instrumented outbound client

- **`appkit/httpclient`** returns an ordinary `*http.Client` whose transport
  records every round trip: `func New(Options) *http.Client` and
  `func NewTransport(Options) http.RoundTripper`, with
  `Options{Recorder *telemetry.Recorder; Timeout time.Duration; Base
  http.RoundTripper}`. A **nil `Recorder` yields a working client that records
  nothing**; the default `Timeout` is 30s and the default `Base` is
  `http.DefaultTransport`. Because it is a plain `*http.Client`, a service
  swaps what it passes and changes nothing else about how it makes requests.
- **It records** `kind` `outbound`, `op` `<METHOD> <host><path>` (no query
  string — third-party query strings routinely carry tokens), the numeric
  status, duration, and streaming size+SHA-256 digests of both bodies. A
  transport failure still records, carrying an error **class**
  (`timeout`, `connection_refused`, `dns`), never the raw error text.
- **It propagates `X-Correlation-Id` only to loopback IP literals**
  (`127.0.0.0/8`, `::1`) — deliberately not even to the *name* `localhost`.
  Calls to `gmail.googleapis.com` and `oauth2.googleapis.com` therefore never
  carry a suite-internal identifier off the box.
- **The service-facing seam is on the Router, not the package.** appkit
  constructs the recorder in `runServe` and exposes two accessors, so most
  services never import `appkit/telemetry` at all:

  ```go
  func (rt *Router) HTTPClient(timeout time.Duration) *http.Client  // instrumented, wired to this service's recorder
  func (rt *Router) Recorder() *telemetry.Recorder                  // nil when telemetry is disabled; every method nil-safe
  ```

  `rt.HTTPClient(t)` is exactly
  `httpclient.New(Options{Recorder: rt.Recorder(), Timeout: t})`. Call
  `httpclient.New`/`NewTransport` directly only where a custom transport,
  redirect policy, or client shape is needed — the offline stub-transport tests
  (`Options.Base`) and the below-the-composition-root fallbacks are gmail's
  only such sites.
- **`Spec.Config` gets no recorder** — it runs before the serve wiring exists
  and is construction-validation only; a client built there records nothing,
  which is correct because no chain exists yet.
- **There is deliberately no exported transport type or predicate** to assert
  on. Proving a client is instrumented is done by the **record that arrives**:
  drive a real request at an `httptest` server with a recorder draining to a
  live in-process sink and assert the `outbound` record shows up. A type
  assertion is a proxy a stub also satisfies and would not prove the service
  re-pointed its client.

### Recording gmail gets for free

Inbound MCP tool calls (`kind` `request`, `op` `mcp:<tool>`, actor from the
nginx identity headers, arguments captured under a 1024-byte per-value and
8192-byte per-record budget), plain HTTP requests, `publish` hops on outbox
append, and `lifecycle` `start`/`stop` records are all instrumented **inside**
the chassis — `mcp.Handler.dispatchTool`, the `server` middleware chain, the
eventplane observation hooks wired from `runServe`. gmail adopts them by
recompiling against the new appkit and eventplane; it writes no recording code
and re-verifies none of that behavior.

### The eventplane producer change

- **`outbox.Append` gains a leading `context.Context`.** `correlation_id`
  becomes a first-class outbox column (additive migration, owned by
  eventplane) and wire-envelope field, populated from the append context. The
  payload-field convention in `docs/correlation-ids.md` is superseded.
- The change is **compile-caught** at all nine in-suite call sites (gmail's is
  `internal/gmail/events.go`); each service adopts it in its own spec.
  `Ring()`, the outbox schema gmail owns, and the routing key are unchanged.
- **eventplane never mints at `Append`.** A context carrying no id stores
  `correlation_id = ""` — a recorded gap, not a license to mint mid-chain,
  because the library cannot tell "this is a root" from "someone forgot to
  thread the context". `outbox.Event` gains **no** `CorrelationID` field: the
  context is the only population path, so a producer threads a ctx and sets
  nothing.
- **The migration is additive.** `outbox.SchemaSQL` gains
  `correlation_id TEXT NOT NULL DEFAULT ''` declared **last**, and eventplane
  exports `outbox.AddCorrelationIDSQL` = `ALTER TABLE outbox ADD COLUMN
  correlation_id TEXT NOT NULL DEFAULT '';`. Each service appends one new
  timestamped migration whose body is exactly that constant. `ADD COLUMN`
  rewrites no rows and leaves `seq` and the `sqlite_sequence` high-water mark
  untouched, so no cursored consumer moves. Any service DDL drift guard
  asserting "the newest outbox migration contains `SchemaSQL` verbatim" stops
  holding and must be re-pointed (gmail's is in
  `internal/db/migrations_outbox_test.go`).
- **Everything else is unchanged for a producer:** `Ring()`, `outbox.New` /
  `outbox.Options` (a new optional `Observe` hook field defaults to nil and is
  wired only by `appkit/feed`), and the routing key.

### Self-originated work is a chain root

- The settled suite rule: any **background poll/watch/sync cycle** that
  publishes events or makes outbound calls with **no inbound request to inherit
  from** is a chain root, alongside cron ticks, `prompts`/`scripts` run spawns,
  and a consumer processing an uncorrelated event. gmail's poll loop is named
  explicitly, as are dropbox's sync cycle and wiki's self-started pipeline work.
- **One root id per cycle, not per event**, so everything one cycle caused
  shares one chain.
- **appkit owns the root-start helpers**, on the recorder, so services do not
  hand-roll the idiom:

  ```go
  func (r *Recorder) StartRoot(ctx, op string, detail map[string]any) (context.Context, string)
  func (r *Recorder) StartChain(ctx, op string, detail map[string]any) (context.Context, string)
  ```

  `StartRoot` **always mints**, ignoring any id the context carries;
  `StartChain` adopts an ambient id when there is one and mints otherwise. The
  choice rule appkit states once: **`StartRoot` when the work has no cause
  outside itself; `StartChain` when it might.** Both emit a `root` record and
  both are nil-safe on a nil `*Recorder` (still mint and install, record
  nothing), so correlation behavior never depends on telemetry being enabled.
- **appkit fixes the mechanism, never the `op` vocabulary** — the op is the
  caller's (`cron:tick/<name>`, `run:<run-id>`, `gmail:poll-cycle`,
  `dropbox:sync-cycle`).

## Attachment addressing: `attachmentId` is ephemeral, `partId` is stable

- **`attachmentId` rotates on every `users.messages.get`.** Observed live:
  two consecutive `messages.get` calls for the same message returned two
  different `attachmentId` values for the same MIME part (`ANGjdJ-p7Ap…` then
  `ANGjdJ-_Rpr…`). The id is a per-response retrieval token, not a durable
  handle. Google's docs never promise stability; observation confirms active
  rotation. Consequence: any design that stores or transmits an
  `attachmentId` for later comparison or resolution is broken by construction
  — a reference minted by one fetch never matches the ids seen by a later
  fetch.
- **`partId` is stable for the life of a message.** A Gmail message is
  immutable once created; its MIME part tree — and each part's `partId`
  (e.g. `"1"`, `"2"`, `"0.1"`) — is fixed. `partId` is therefore the correct
  durable component for addressing an attachment within a message.
- **A fresh `attachmentId` used promptly resolves.** `users.messages.attachments.get`
  accepts the token returned by the `messages.get` response it came from.
  Resolution must therefore mint and spend the token inside one request
  window: refetch the message, take the *current* `attachmentId` from that
  response, and call `attachments.get` with it immediately.

## Send-to-self and cleanup facts (for the live check)

- **`users.getProfile` returns the authorized mailbox's `emailAddress`** — a
  live test can discover "self" at runtime and hardcode no address.
- **A send-to-self lands in the same mailbox** (SENT and INBOX copies share
  the message/thread), so one authorized account covers send, fetch, and
  cleanup.
- **The connector's consent flow requests the full `https://mail.google.com/`
  scope** (`cmd/consent/main.go`), which permits permanent
  `users.messages.delete` — cleanup can remove the test message outright
  rather than leaving it in Trash (which `trash` — modify scope — would).
- **Which account the connector "is"** is decided solely by the refresh token
  installed on the box (minted by the one-time `cmd/consent` flow). The
  deployed box is being moved to michaelgreenly@logic-refinery.com as an ops
  action; nothing in the codebase names a mailbox.

## Options evaluated and not chosen

- **Filename as the stable part locator** — human-friendly but not unique
  (two attachments may share a filename in one message); `partId` is unique
  within the tree.
- **Caching `attachmentId`→bytes server-side to outlive rotation** — adds
  blob state to a stateless connector to work around a token that is free to
  re-mint per request.

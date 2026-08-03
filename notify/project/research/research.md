# notify — Research

**Non-contractual.** This file collects the **external ground truth** notify's
design leans on so no Decision has to re-derive it. Nothing here is a promise
and nothing downstream reads it mechanically: the build loop never opens it.
Every fact below is owned by a workspace **outside** `notify/` (appkit,
eventplane, the suite root, the dashboard) and is recorded here as *consumed
truth* — if one of them changes, this file is corrected, not argued with.

It is the single current statement of that ground truth: no history, no
superseded paragraphs.

## The suite telemetry capability (what notify is adopting)

The suite is growing a `telemetry` service that lets a forensic agent
reconstruct any sequence of events across service boundaries: who did what,
when, in what order. Telemetry stores a **skeleton** — calls, actors, timing,
parameters, outcomes, digests — never bulk content. Services never talk to it
from application code: the appkit chassis records on their behalf and POSTs
batches to the telemetry service, best-effort, so telemetry being down never
blocks or crashes anything.

The record kinds are `edge`, `request`, `outbound`, `publish`, `consume`,
`root`, and `lifecycle`. notify is a **consumer and a caller-out**, never a
producer, so the two that matter to its own source are:

- **`outbound`** — the ntfy POST, which is only recorded if it runs through the
  chassis's shared instrumented client rather than a privately-constructed
  `http.Client`.
- **`consume`** — recorded by the chassis-run consumer loop when an event is
  handed to a handler. It arrives free; what does **not** arrive free is
  notify's own handling of the correlation id across the asynchronous seam it
  deliberately puts between the handler and the push.

`request` (the MCP `send` call) and `lifecycle` (start/stop) records likewise
arrive from the rebuilt chassis with no notify code.

## The correlation id

- Header name: **`X-Correlation-Id`**. Value: a bare 26-character **Crockford**
  base32 ULID (alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ` — no `I`, `L`, `O`,
  `U`).
- One id per **causal chain**, propagated verbatim on every hop; never re-minted
  mid-chain.
- The id lives in `context.Context`, on a key owned by the shared leaf package
  **`eventplane/correlation`** — below both appkit and eventplane so the two read
  the same key without eventplane importing appkit. Its consumed surface:

  ```go
  // eventplane/correlation — owned by the eventplane workspace.
  const Header = "X-Correlation-Id"
  func New() string                                        // mint a Crockford ULID
  func Valid(id string) bool                               // 26 chars, Crockford alphabet
  func With(ctx context.Context, id string) context.Context
  func From(ctx context.Context) string                    // "" when absent
  ```

  (The context-read accessor is spelled `From` in the correlation Decision that
  owns the package and `FromContext` in one appkit Decision that consumes it;
  the eventplane spec settles the name. Nothing in notify's design depends on
  which spelling wins.)

- **Inbound HTTP**: appkit's chassis middleware is the universal read-or-mint
  point — a well-formed inbound `X-Correlation-Id` is trusted verbatim (loopback
  callers are inside the trust boundary), an absent or malformed one is replaced
  by a fresh mint. notify writes no code for this.
- **Outbound**: the shared instrumented client propagates `X-Correlation-Id`
  **only to loopback suite peers** (host `127.0.0.1`) and **never to a third
  party**. Enforced inside appkit's client, not by notify.

  A consequence worth stating plainly, because it shapes what notify's tests can
  honestly assert: in production `ntfy.sh` is a **third party**, so no
  correlation id ever travels with a push — but notify's whole test substrate is
  a **mock ntfy on `127.0.0.1`**, which the rule classifies as a loopback peer.
  The propagation policy is therefore appkit's claim, proven in appkit against a
  substrate that can falsify it; notify asserts neither direction of it.

## The event plane, consumer side

- `correlation_id` becomes a first-class **outbox column and wire-envelope
  field**, populated by the producing service from context at append time.
- The consumer surfaces it **into the handler's context**: a handler invoked by
  the chassis-run loop receives a `ctx` for which `correlation.From(ctx)` returns
  the producer's chain id, so a push caused by a crm contact creation lands on
  the same chain as the MCP call that created the contact.
- An event that carries **no** correlation id makes the consumer mint a **root**
  chain before invoking the handler, so a handler ctx is never id-less.
- The `consume` hop is recorded through an injectable observation hook eventplane
  exposes and appkit installs — no notify code, and no new import.
- The payload-field correlation convention formerly described in
  `docs/correlation-ids.md` is superseded by the envelope field. notify's
  handlers decode no new payload field.
- notify produces nothing, so the context-taking `Outbox.Append` change that
  compile-breaks every *producer* does not touch notify: there is no outbox, no
  append site, and no migration.

## The edge: where a gated request's id comes from

nginx is the sole trust boundary and the only place a public caller's header can
be neutralised. The suite-wide fragment contract every service now implements in
its own `etc/nginx.conf`:

- The dashboard's introspection endpoints (`/_authn`, `/_session-authn`)
  **mint** the correlation id for an allowed request and return it on the auth
  subrequest response as `X-Correlation-Id`.
- Each **gated** location captures it (`auth_request_set`) and forwards it with
  `proxy_set_header X-Correlation-Id <var>;`. Because nginx does not pass an
  inbound client header as a proxy header unless explicitly set, and the last
  `proxy_set_header` for a name wins, that single set **overwrites** whatever the
  client supplied — identical to the `X-Owner-*` hygiene already in force.
- Each **ungated public** location sets `proxy_set_header X-Correlation-Id "";`
  so the service's chassis middleware mints rather than trusting a public value.

notify's fragment has exactly one ungated proxying location (the RFC 9728 PRM
bootstrap) and three gated ones (the session-gated landing root, the
session-gated static tier, the bearer-gated prefix).

## What appkit exports, and what notify owns

appkit's design owns this; notify consumes it and designs none of it. The
instrumented outbound HTTP client is reached through the **`Router`** the chassis
hands a service — `rt.HTTPClient(…)` returns a client already wired to the
recorder — so a composition root never assembles chassis machinery itself.

Its transport records kind `outbound` with op `<METHOD> <host><path>`, sets the
correlation header **only when the URL host is a loopback IP literal**
(`127.0.0.0/8` or `::1` — deliberately not the *name* `localhost`), records
transport failures with an error class rather than raw text, and never captures
query strings.

notify takes the seam's plain client shape: the ntfy client has no custom
redirect or cookie policy. Its **timeout stays notify's to set** — the ntfy push
timeout (10s) bounds a fire-and-forget goroutine and must not be replaced by a
chassis default.

notify needs no root-chain minting of its own: it originates nothing. Every
chain it is on was started by an inbound MCP request or by the consumer, which
mints a root when a delivered envelope carries no id. (Minting happens at the
consumer, never at `Append`.)

## Go facts the async seam depends on

notify's consumer handlers deliberately return before the push completes, firing
it on a goroutine so a slow or dead ntfy never stalls the feed cursor. Today that
goroutine starts from `context.Background()`, which drops everything on the
handler context — including the correlation id the consumer just put there.

`context.WithoutCancel(ctx)` (standard library, Go 1.21+) returns a context that
keeps its parent's **values** while being immune to the parent's cancellation —
exactly the two properties the seam needs. Wrapping that in
`context.WithTimeout(…, PushTimeout)` keeps the existing termination guarantee.

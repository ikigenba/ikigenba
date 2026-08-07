# dropbox — Research

**Non-contractual.** This file collects the **external ground truth** dropbox's
design leans on so no Decision has to re-derive it. Nothing here is a promise
and nothing downstream reads it mechanically: the build loop never opens it.
Every fact below is owned by a workspace **outside** `dropbox/` (appkit,
eventplane, the suite root, the dashboard) and is recorded here as *consumed
truth* — if one of them changes, this file is corrected, not argued with.

It is the single current statement of that ground truth: no history, no
superseded paragraphs.

## The suite telemetry capability (what dropbox is adopting)

The suite is growing a `telemetry` service that lets a forensic agent
reconstruct any sequence of events across service boundaries: who did what,
when, in what order. Telemetry stores a **skeleton** — calls, actors, timing,
parameters, outcomes, digests — never bulk content. Services do not talk to it
directly in application code: the appkit chassis records on their behalf and
POSTs batches to the telemetry service, best-effort, so telemetry being down
never blocks or crashes anything.

The record kinds are `edge`, `request`, `outbound`, `publish`, `consume`,
`root`, and `lifecycle`. Of those, dropbox's own source has to do something
deliberate about exactly two:

- **`outbound`** — every call dropbox makes to a host outside its own process
  is recorded, which requires those calls to run through the chassis's shared
  instrumented client rather than a privately-constructed `http.Client`.
- **`root`** — work that no inbound request caused (dropbox's two background
  daemons) has no chain to join, so it must **mint** one.

`request`, `publish`, `consume`, and `lifecycle` records arrive for free from
the rebuilt chassis and libraries; `edge` is the dashboard's.

## The correlation id

- Header name: **`X-Correlation-Id`**. Value: a bare 26-character **Crockford**
  base32 ULID (alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ` — no `I`, `L`, `O`,
  `U`).
- One id per **causal chain**, propagated verbatim on every hop. It is never
  re-minted mid-chain.
- The id lives in `context.Context`, on a key owned by the shared leaf package
  **`eventplane/correlation`** — a package below both appkit and eventplane so
  the two libraries read the same key without eventplane importing appkit. Its
  consumed surface:

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
  the eventplane spec settles the name. Nothing in dropbox's design depends on
  which spelling wins.)

- **Inbound**: appkit's chassis middleware is the universal read-or-mint point.
  A well-formed inbound `X-Correlation-Id` is trusted verbatim (loopback callers
  are inside the trust boundary); an absent or malformed one is replaced by a
  fresh mint. dropbox writes no code for this — it arrives with the rebuild.
- **Outbound**: the shared instrumented client propagates `X-Correlation-Id`
  **only to loopback suite peers** (host `127.0.0.1`) and **never to a third
  party**. This is the rule that keeps a suite-internal id off the wire to
  `api.dropboxapi.com`; it is enforced inside appkit's client, not by dropbox.

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
  `proxy_set_header` for a name wins, that single set **overwrites** anything
  the client supplied — identical to the identity-header hygiene already in
  force for `X-Owner-*`.
- Each **ungated public** location sets `proxy_set_header X-Correlation-Id "";`
  so the service's chassis middleware mints a fresh id rather than trusting a
  public caller's value.

Net effect for dropbox: a public caller can never inject a correlation id
through any location in `dropbox/etc/nginx.conf`.

## The eventplane revision dropbox compiles against

The eventplane workspace adds `correlation_id` as a **first-class outbox column
and wire-envelope field**, populated from context at append time. The consumed
consequence for a producer:

- `Outbox.Append` takes a `context.Context` as its first parameter —
  `Append(ctx context.Context, tx *sql.Tx, ev Event) error` — and reads the
  chain id from it via `correlation.From(ctx)`. This is a **compile-caught**
  change: dropbox's single call site (`internal/dropbox/events.go`) does not
  build until it threads a context through.
- The `publish` telemetry hop is recorded at append through an injectable
  observation hook eventplane exposes and appkit installs, so a producer records
  publishes without importing anything new.
- The payload-field correlation convention formerly described in
  `project/design/D14.md` at the repo root is superseded by the envelope field. dropbox's event
  payload gains **no** new field.

Where a context is not already in hand at the append site, the chain id is the
one the *caller* is on: a write driven by an MCP tool or a loopback route is on
that request's chain, and a change applied by the sync engine is on that sync
cycle's root chain.

## What appkit exports, and what dropbox owns

appkit's design owns these; dropbox consumes them and designs none of them.
Both are reached through the **`Router`** the chassis hands a service, so a
composition root never assembles chassis machinery itself:

- **The instrumented outbound HTTP client** — `rt.HTTPClient(…)` returns a
  client already wired to the chassis recorder. Its transport records kind
  `outbound` with op `<METHOD> <host><path>`, sets the correlation header **only
  when the URL host is a loopback IP literal** (`127.0.0.0/8` or `::1` —
  deliberately not the *name* `localhost`), records transport failures with an
  error class rather than raw text, and never captures query strings.

  dropbox needs **three** clients from that seam, not one, because it runs three
  deliberately different policies that must not be flattened: the ~100s
  rpc/content client, the ≥600s parked-longpoll client, and the `source_url`
  fetcher with its 5s dial / 5s response-header transport and its
  `http.ErrUseLastResponse` redirect policy. How the seam admits per-call-site
  policy — options on the call, an adjustable returned client, an installable
  transport — is appkit's to settle.

- **The root-start helper** — one call that mints a correlation id, returns a
  context carrying it, and emits the `root` record naming the origin. This is
  the *only* sanctioned way a service opens a self-originated chain: dropbox
  neither constructs an id nor assembles a record nor touches the recorder. The
  recorder behind it is best-effort (ring buffer, batched, fire-and-forget), so
  opening a chain never blocks a daemon cycle and never returns an error.

  Two variants exist — one that always mints a fresh id, and one that adopts an
  ambient id when the context already has one. **dropbox's daemons want the
  always-mint variant**: a sync cycle and an uploader drain have no cause
  outside themselves, and adopting a stale ambient id would silently glue two
  unrelated cycles together.

  Both are **nil-safe when telemetry is disabled**: the id is still minted and
  installed on the context, only the `root` record is skipped. Correlation
  therefore never depends on telemetry being enabled — which is also what makes
  the root-chain claims testable without a live recorder.

Note that **eventplane never mints at `Append`** — it reads whatever id the
context already carries. If a daemon cycle failed to open a root, its events
would simply land with an empty `correlation_id`; nothing downstream repairs
that, which is why the per-cycle root is dropbox's responsibility.

## Third-party surface dropbox actually calls

Unchanged by this work, recorded here because it determines which calls carry
the header (none of them do — every host below is a third party):

| host | used for |
|---|---|
| `api.dropboxapi.com` | rpc calls (`list_folder`, `create_folder`, `delete`, `move`) |
| `content.dropboxapi.com` | content calls (`download`, `upload`, `upload_session/*`) |
| `notify.dropboxapi.com` | the parked `list_folder/longpoll` read (no bearer sent) |
| `api.dropboxapi.com/oauth2/token` | the refresh-token exchange |

The one outbound call dropbox makes that **is** a loopback suite peer is the MCP
`put` tool's `source_url` fetch, which is already confined to `127.0.0.1` on a
registry-derived port set (design D19) — so it is the only dropbox outbound call
that carries `X-Correlation-Id`.

## Scope exclusions the suite decided (relevant to dropbox)

- Operator tooling (`opsctl`, `bin/`) is not recorded; its effects show up as
  `lifecycle` version changes.
- Requests nginx answers without reaching a service are not recorded.
- Telemetry's own ingest path is never recorded, so no dropbox call can recurse
  into it.

# crm — Research

Collected ground truth crm's design leans on but does not own. Non-contractual:
nothing here is a promise, and the build loop never reads this file. It exists
so a Decision can cite a fact instead of re-deriving it.

## The suite correlation-id standard

The suite carries one correlation id per causal chain so a forensic agent can
reconstruct who did what, in what order, across service boundaries.

- **Header name:** `X-Correlation-Id`. **Value:** a bare 26-character Crockford
  base32 ULID — no prefix, no separators, opaque to every consumer.
- **The edge mints.** The dashboard's introspection endpoint mints the id for
  every gated route and returns it on the auth subrequest response. Each
  service's nginx fragment captures it with `auth_request_set` and forwards it
  with `proxy_set_header X-Correlation-Id`, which **replaces** anything the
  client sent. This is the same identity-hygiene mechanism the fragment already
  uses for `X-Owner-Email` / `X-Client-Id`: nginx does not pass an inbound
  client header through as a proxy header unless explicitly set, and the last
  `proxy_set_header` for a name wins.
- **Ungated locations strip.** A location that runs no `auth_request` has no
  minted id to capture, so it sets `proxy_set_header X-Correlation-Id ""` and
  lets the service mint its own. Without that line a public caller could inject
  a chosen id and graft itself onto someone else's chain.
- **appkit is the read-or-mint point.** The chassis middleware trusts an inbound
  `X-Correlation-Id` (loopback callers are inside the trust boundary) and mints
  one when it is absent, then exposes it on the request context. Every service
  therefore has a chain id in hand without writing any correlation code.

## What the chassis records without crm asking

The telemetry recorder lives in appkit, not in the services. Once crm is rebuilt
against the new appkit it emits, with no crm-side code:

- `request` records for MCP dispatch (`mcp:<tool>`) and plain HTTP requests,
  with actor, elapsed, outcome status/error class, and structured params
  captured under a per-value 1024-byte / per-record 8192-byte budget.
- `lifecycle` records at boot (`start`, with version) and graceful shutdown
  (`stop`).

Records are posted best-effort to the telemetry service on loopback; telemetry
being down never blocks or fails a crm request. crm makes **no outbound HTTP
calls of its own** — it constructs no `http.Client` anywhere — so the
Router-provided instrumented outbound client is not a crm concern.

## The event-plane correlation field

`correlation_id` becomes a first-class outbox column and wire-envelope field,
populated by the eventplane library from the calling context at append time.
The old convention of carrying a correlation value inside the event *payload*
(`docs/correlation-ids.md`) is superseded by the envelope field; crm never
carried one in a payload, so no crm payload shape changes.

The consequence for crm is a **compile-caught signature change**: `Outbox.Append`
takes a `context.Context` so it can read the chain id, i.e.
`Append(tx *sql.Tx, ev Event) error` becomes
`Append(ctx context.Context, tx *sql.Tx, ev Event) error`. crm's single emit
site (`internal/crm/service.go`, inside `Save`) already has the request context
in scope and passes it through. Everything else about crm's four first-wave
contact events — kinds, subjects, payload structs, the reflection `Registry` —
is untouched.

## Deliberately out of scope for crm

- crm records nothing itself and stores no telemetry: the recorder, the ingest
  client, the ring buffer, and the param-capture encoder are all appkit's, and
  the store is the telemetry service's.
- crm does not mint root correlation ids. It originates no self-driven work —
  no timer, no run spawn, no event consumption — so every chain crm participates
  in was rooted elsewhere and arrives on the header.

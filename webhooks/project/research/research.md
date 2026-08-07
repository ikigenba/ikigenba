# webhooks — Research

**Non-contractual.** This doc informs the *author* before design. The autonomous
build reads only product, design, and plan — never this file. Nothing downstream
consumes it. It records the external ground truth design leans on — the shape of
libraries and suite-wide protocols webhooks does **not** own — so a Decision can
cite a fact instead of re-deriving it. Where this doc and the design spine
disagree, **design wins**: design is the sole authority for *how*. Cited code is
a pointer, not gospel; re-read the file before relying on an exact line. This is
the **single current** statement of the research — superseded findings are
deleted, not stacked.

Paths are relative to the repo root; the service lives at `webhooks/`.

---

## 1. nginx location precedence and the tiering rule

The load-bearing rule the fragment depends on: an **exact `=` match always wins
over a prefix match**, regardless of order in the file, and among prefixes the
**longest** match wins. That is what lets one `/srv/webhooks/` mount carry an
open ingress *prefix* alongside gated *exact* endpoints without either shadowing
the other, and what lets `/srv/webhooks/in/` and `/srv/webhooks/static/` both
beat a `return 404` catch-all.

Prior art in the suite: `sites/etc/nginx.conf` (four tiers under one mount, the
multi-tier template), `crm/etc/nginx.conf` (the simpler gated + PRM shape with a
hard `= /srv/crm/feed { return 404; }` feed shield).

**Identity hygiene.** On an `auth_request` route, nginx captures values from the
introspection subresponse with `auth_request_set $var $upstream_http_<header>`
and re-emits them with `proxy_set_header`. A `proxy_set_header` for a name
**replaces** any inbound client header of that name — that single set-per-name is
the entire anti-smuggling property. On an **open** tier nginx sets no identity
headers at all, and the suite convention is that the handler is then the primary
guard and rejects any request that arrives carrying internal headers (precedent:
dropbox's loopback `/content` handler, crm's `/feed`).

**Variable-name collisions are real.** Every service fragment is `include`d into
the **one** apex `server` block the dashboard owns, so `auth_request_set`
variable names must be unique per service (the suite convention is a
service-prefixed namespace, e.g. `$wh_*`). Named locations (`@…`) are tolerated
in duplicate by `nginx -t`, but the same convention applies for the same reason.

**Dev wiring.** `nginx/run` generates `locations/<svc>.conf` for the dev front
door on `:8080`, mirroring prod routing; `webhooks` is already in that loop, so a
fragment change needs no harness change.

---

## 2. eventplane: the producer contract

Producing one fact is a single outbox `INSERT` **in the same transaction as the
domain write**, followed by a non-blocking wake. That co-transaction *is* the
durability guarantee behind webhooks' durable-before-ack promise: the `202` is
returned only after `tx.Commit()` succeeds.

- `Outbox.Append(tx, ev)` appends inside the caller's transaction; the library
  mints the event **id (ULID)** and **timestamp** — the producer does not.
  `Outbox.Ring()` after a successful commit wakes parked `/feed` connections,
  off the request's critical path.
- The outbox table is created by a migration in the service's own
  `internal/db/migrations/`, its DDL byte-identical to `eventplane`'s
  `outbox.SchemaSQL` constant. `seq INTEGER PRIMARY KEY AUTOINCREMENT` is
  load-bearing: retention may empty the table, and AUTOINCREMENT prevents `seq`
  reuse that would make consumers skip post-trim rows. Each service keeps a DDL
  **drift guard** test pointed at its newest outbox migration.
- Wire-up is via appkit: when `Spec.Feed != ""` the chassis constructs the
  outbox over the shared DB, starts retention, mounts the SSE handler, and calls
  `Spec.Producer(ob)` so the service injects it into its domain.
- `/feed` is loopback-only, unauthenticated, and never reachable through nginx.

**Addressing (the event-routing revision, `project/design/D18.md` at the repo root).** An
event is addressed by a routing key `<source>:<kind><subject>`: `kind` is the
fact class with any redundant noun prefix dropped (`source` already names the
domain), `subject` a `/`-rooted producer-chosen routing name. The suite key map
records webhooks' direction as `webhooks:received/<hook name>`. The consumer
envelope carries `id`, `kind`, `subject`, `source`, `time`, `payload`;
consumers cursor on `<generation>.<seq>` and dedup on the ULID `id`. webhooks
owes **at-least-once** only.

**In revision for telemetry (see §4).** `correlation_id` becomes a first-class
outbox **column** (additive migration) and **envelope field**, populated at
append time from the caller's `context.Context` — which makes `Append` take a
context, a compile-caught signature change every producer absorbs at its
emission site. eventplane must not import appkit (the reverse dependency is the
real one), so the header name, the ULID minter, the validity rule and the
context accessors live in the stdlib-only leaf package
`eventplane/correlation`, and appkit injects an observation hook
(`eventplane/observe`) on the publish and consume paths so those hops are
recorded without eventplane knowing telemetry exists.

References: `eventplane/outbox/` (`outbox.go`, `feed.go`, `cursor.go`,
`schema.go`), `appkit/feed/feed.go`, `crm/internal/crm/{service,events}.go`.

---

## 3. Verification primitives already established in the suite

There is **no shared appkit crypto package** — these helpers are deliberately
duplicated per service.

- **Random generation:** `crypto/rand` → 128 bits → Crockford base32, unpadded,
  26 chars (`dashboard/internal/ids/ids.go`). Human-distinguishable tokens carry
  a typed prefix (`ms_pat_`, `ms_oat_`, …).
- **Verifier at rest:** unsalted **SHA-256 hex** of the plaintext, stored as
  `*_hash`; verification is a lookup/compare on the hash
  (`dashboard/internal/pat/pat.go`, `dashboard/internal/oauth/authcodes.go`).
- **Constant-time compare** when a fetched hash is compared in memory:
  `crypto/subtle.ConstantTimeCompare` (`dashboard/internal/oauthstate/store.go`).
  A `WHERE secret_hash = ?` lookup is itself timing-safe enough for the common
  path.
- **Show-once:** the plaintext is returned to the caller exactly once and only
  the hash persisted; render the full response before writing, and set
  `Cache-Control: no-store`.

These are ordinary application data in webhooks' own SQLite DB — not
`~/.secrets/` material.

**GitHub's signed-delivery scheme (external ground truth).** GitHub signs each
delivery with the shared secret and sends
`X-Hub-Signature-256: sha256=<hex>` — HMAC-SHA256 over the **raw request body**,
lowercase hex. Because the signature covers the body, a verifier must read the
body (under its size cap) *before* it can authenticate, unlike a bearer secret
which authenticates first. GitHub additionally sends `X-GitHub-Event` (the
delivery's event name) and `X-GitHub-Delivery` (a per-delivery UUID).

---

## 4. Suite telemetry: the external contract webhooks conforms to

Settled suite-wide; webhooks owns none of it and uses it by value. The normative
statement is `project/design/D14.md` at the repo root, which carries both the
record contract and the correlation standard.

- **Correlation header `X-Correlation-Id`**, value a bare **26-character
  Crockford base32 ULID** (alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ` — note
  appkit's older RFC 4648 minter was wrong and is corrected as part of this
  work). One id per causal chain, propagated verbatim on every hop.
- **appkit middleware is the universal read-or-mint point:** an inbound header
  is **trusted** (every loopback caller is inside the trust boundary); absent →
  mint. The id is readable from the request context.
- **The edge strips and mints.** The dashboard's introspection endpoint mints
  the id for gated routes and returns it on the auth subresponse; each service's
  fragment captures it with `auth_request_set` and overwrites the forwarded
  header. **Ungated public locations set `X-Correlation-Id ""`** so the service
  mints — the rule that exists because webhooks' public ingress is the one place
  the "callers are trusted" premise fails.
- **Records are allowlist-by-construction.** Raw header dumps and raw bodies are
  **never** captured. A record carries `id`, `time`, `correlation_id`,
  `service`, `kind` (`edge|request|outbound|publish|consume|root|lifecycle`),
  `actor`, `op`, `params` (per-value cap 1024 bytes, per-record budget 8192
  bytes, elided values replaced in place by
  `{"$elided":{"bytes":N,"sha256":"<hex>"}}`), `outcome`
  (`{status,error,duration_ms,bytes,sha256}` — response content is never stored
  literally), and small kind-specific `detail`. Digests are lowercase-hex
  SHA-256.
- **Ingest** is `POST http://127.0.0.1:3008/ingest`, loopback-only,
  best-effort: telemetry being down never blocks or crashes a service, and the
  ingest path itself is never recorded.
- **`root` records are for self-originated work** (a cron tick, a run spawn, a
  consumer processing an event that carries no id). An inbound HTTP delivery is
  not self-originated — it already produces a `request` record.

---

## 5. Operational facts

- **Loopback port `3006`**, resolved by name through the shared `registry`
  library rather than a Go-side literal; the committed nginx fragment still
  carries the port as a **literal** because it ships verbatim and must be
  directly loadable by nginx.
- The dashboard auto-discovers MCP services by globbing `*/etc/manifest.env`
  (`appkit/inventory`), so `MCP=true` in the manifest is enough — but the
  dashboard must be **restarted** to re-read manifests after a service is added.
- Build/deploy (only when explicitly told to deploy): `bin/bump webhooks <level>`
  → `bin/ship webhooks` (static linux/amd64, `GOWORK=off`, version+commit
  stamped) → on box `opsctl stage` → `opsctl deploy`.
- New migrations are minted with `bin/create-migration webhooks <name>`
  (timestamped); numbers are never hand-picked and committed migrations are
  never edited.

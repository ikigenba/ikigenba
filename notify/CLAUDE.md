# notify

The **notify** service for the ikigenba single-tenant suite. A pure MCP API with
**no UI** and **no token logic**, deployed at `<account>.ikigenba.com/srv/notify/`
(e.g. `int.ikigenba.com/srv/notify/`). First demo account: **int**.

notify is the suite's **first event-plane consumer**. It was duplicated from
`../ledger` (the health-only chassis skeleton) and given a domain: it subscribes
to crm's and prompts's east/west event feeds and fires a best-effort ntfy.sh push
in reaction (every contact created; every prompts run that succeeds or fails).

notify has **two faces on ntfy**. The east/west consumer loop is *reactive* —
best-effort pushes driven by events. The north/south MCP surface is *proactive*:
the **`send`** tool lets a connected agent push a notification to the owner's
device on demand (see `../../docs/plan-notify-mcp-send.md`). Alongside `send`, the
chassis `health` tool is the north/south auth proof and `reflection` self-describes
notify's event-graph edges.

**Read the decisions first — do not re-derive them:**

- `../../docs/event-protocol.md` — the **normative** event-plane wire contract.
  On any conflict it wins over this file. notify is a *consumer* under §10.
- `../../docs/event-plane-decisions.md` — the design rationale for this consumer.
- `../../metaspot/AGENTS.md` — platform spec (Service layer = path routing).
- `../../metaspot/docs/path-routing-architecture.md` — server-side topology + the
  auth contract you live under.
- `../crm` — the producer this consumer reacts to (owns the `contact.created`
  payload shape, §8.6); `../ledger` — the chassis skeleton this was cloned from;
  `../eventplane` — the shared library whose `consumer` package is the engine.

If anything here conflicts with those docs, the docs win — and flag the conflict.

## The two planes notify lives on

- **North/south (external, owner-facing).** nginx terminates TLS, introspects
  every request via `auth_request` against the dashboard, strips the
  `/srv/notify/` prefix, and injects `X-Owner-Email` / `X-Client-Id`. notify
  trusts those headers and does NO token logic. Surface: `POST /mcp` (`send`,
  `health`, `reflection`) and the unauthenticated RFC 9728 PRM doc. notify is a
  consumer, **not** a producer — it serves **no** `/feed` endpoint, and its nginx
  fragment (`etc/nginx.conf`, dev mirror `../nginx/locations/notify.conf`) has no
  feed block.
- **East/west (internal, service-to-service).** A background goroutine runs
  `eventplane/consumer.Run`, holding one long-lived SSE connection to crm's
  `http://127.0.0.1:3001/feed` (loopback-direct — the event plane bypasses nginx,
  §2). It is unauthenticated and loopback-only by construction.

## What the consumer does

- **Engine, not hand-rolled.** All the hard parts — the SSE client, the
  reconnect/backoff loop, the durable per-upstream cursor, and all four
  connect-time resync reasons — live in `eventplane/consumer`. notify supplies
  only a `Config` and a `Handler`.
- **The effect is best-effort (§11.2).** `internal/push` maps `contact.created`
  → one ntfy POST (`Title: New contact`, body = the contact's `display_name`,
  `Authorization: Bearer <NTFY_API_KEY>`), fired **asynchronously** in a
  timeout-bounded goroutine. The engine commits the cursor regardless of the push
  outcome, so the controlled leg (crm → notify) stays at-least-once while end-user
  delivery is intentionally unreliable. A non-`contact.created` event runs no
  push but **still advances the cursor** (consumer-side filtering, §7.3). There is
  no dedup table — duplicate pushes on reconnect are expected and acceptable.
- **First-subscription = `tail` by default** (`NOTIFY_FROM`), so a fresh notify
  only pushes for contacts created from now on, not the entire backlog.
- **Structural vs transport (decision 11).** A `feed_offset` read/write failure
  (a missing table — a deploy bug) crashes the whole process so systemd
  restart-loops visibly; crm being down is a transport fault the engine retries
  indefinitely without bringing notify down. `cmd/notify` runs the HTTP server and
  the consumer under one context: a structural consumer fault cancels the server
  too — no half-alive (HTTP up / consumer dead) state.

## Secrets

The ntfy **topic** and **key** are deployment secrets (`~/.secrets/NTFY_TOPIC`,
`~/.secrets/NTFY_API_KEY`). They reach the process only via the environment: the
committed `.envrc` injects them locally (run `direnv allow` once); app-config
injects them in prod. notify reads them with `getenv` at its composition root
(`cmd/notify/main.go`) and **fails loudly at boot** if either is absent. Never
read, log, or commit their values (the `secrets` skill's hard rule). The ntfy
**base URL** (`NOTIFY_NTFY_BASE_URL`, default `https://ntfy.sh`) is plain config,
so tests point it at a mock.

## Layout

- **`internal/push`** — the domain: the ntfy `Client` and the `consumer.Handler`
  that filters and pushes. Mirrors how crm's `internal/contacts` owns the producer
  domain. `Client.Send` is the consumer's best-effort, fire-and-forget hop;
  `Client.Publish(ctx, Notification) error` is the synchronous hop behind the MCP
  `send` verb (`Send` delegates to it, so there is one ntfy-POST code path).
- **`internal/db`** — SQLite open (WAL, FK, single-writer) + migration runner.
  `001_schema_migrations`, then `002_feed_offset` which applies
  `consumer.SchemaSQL` verbatim (asserted by `migrations_feed_offset_test.go`).
- **`internal/mcp`** — the JSON-RPC `/mcp` transport and notify's tool surface:
  the `send` write verb (validates args → `push.Client.Publish` → `{ok:true}` or a
  closed-vocab `validation`/`upstream` error envelope) plus the chassis `health`
  and `reflection` tools. The handler holds a `*push.Client` built at the
  composition root.
- **`internal/server`, `internal/logging`, `internal/ids`** — the carried-over
  chassis (PRM, identity gate, security headers, request ids).

## Tests

`go test ./...` (workspace mode via `ikigai/go.work`). The migration-assertion test
guards that `002_feed_offset.sql` stays byte-identical to `consumer.SchemaSQL`.
The §13c e2e (`internal/push`) wires the **real** `outbox.FeedHandler` to a
consumer whose handler points at a **mock** ntfy server, and asserts a
`contact.created` yields exactly one correctly-shaped POST while a
non-`contact.created` event yields none but still advances the cursor. The
`internal/mcp` send tests (`send_test.go`) drive `tools/call` against a **mock**
ntfy and assert the header mapping (priority→1..5, tags join, click), that
validation rejects (no POST fired) and that an `upstream` failure never leaks the
topic or token. Real ntfy.sh is never contacted.

## Manifest / deploy

notify is one static appkit binary (the `appkit.Main(appkit.Spec{…})` contract,
`Consumes:["crm"]`, with the consumer loop run as an appkit `Worker`): `<app>`
serve + the fixed `version`/`manifest`/`migrate`/`schema`/`backup`/`restore`
verbs, no `run` wrapper. `etc/manifest.env` (`APP=notify`, `MOUNT=/srv/notify/`,
`DEFAULT=false`, `PORT=3003`, `MCP=true` so the dashboard inventory lists it,
`CONSUMES=crm`) is emitted by `notify manifest`; the public consumer config
(`NOTIFY_FROM`, `NOTIFY_NTFY_BASE_URL`, the feed URL resolved by name via
`bin/registry`) is read from env at the composition root, and the ntfy secrets
flow via app-config only. Shipping is the shared repo-root `bin/ship notify`
(no version arg; version is the committed `notify/VERSION`, advanced by
`bin/bump notify <field>`) → `opsctl stage` + `opsctl deploy` (which regenerates
the on-box manifest on every swap); provisioning is `opsctl setup notify`. The only `bin/*` scripts notify
still carries are `start`/`stop` (systemd control) and `secrets` (SSM seeding). No
`plugin/` in this repo. notify is a consumer with no generation sidecar, so
restore is trivial: a consumer restored from an older snapshot simply replays from
its rolled-back cursor, and best-effort tolerates the duplicates (§11.1).

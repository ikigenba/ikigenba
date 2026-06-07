# Gmail-Connector Implementation Plan

The phased build plan for the `gmail` owner-mailbox connector. **Decisions** are
recorded in `docs/gmail-connector-decisions.md`; this file turns those decisions
into a sequence of **subagent-sized phases**.

> **Note — credential bootstrap was reworked in the 2026-06-07 plan review** and
> `decisions §2` has been updated to match: a **dedicated GCP project**, a
> **dedicated desktop OAuth client** (`GMAIL_CLIENT_ID`/`GMAIL_CLIENT_SECRET`,
> not reused `GOOGLE_*`), publishing status **"In production" (unverified)**
> rather than "Testing", and a **self-writing consent CLI**. Read `decisions`
> first as always; it and this plan now agree.

Each phase is a single coherent unit of work that one subagent can complete in
one context: it compiles, its tests pass, and it is independently committable.
Phases are executed **strictly in the listed order**; each builds on the
committed output of the prior ones. There is **no parallel work** — an
orchestrator drives one subagent per phase, sequentially.

The service is **structurally `dropbox`'s twin** (decisions §1): an
external-OAuth connector with a `tokenSource`, an MCP surface, an internal poll
daemon, and an event-plane **producer** half. Where this plan says "mirror
dropbox," the named dropbox file is the concrete template. Reference points:

| Concern | Template to copy |
|---|---|
| Composition root + verb dispatch | `dropbox/cmd/dropbox/main.go` (appkit.Spec hooks) |
| `tokenSource` (refresh→access, cache, 401 force-refresh) | `dropbox/internal/dropbox/client.go` lines 131–276 |
| Cursor store (`sync_state`, id=1) | `dropbox/internal/dropbox/store.go` lines 46–77 |
| Outbox migration + byte-equality guard | `dropbox/internal/db/migrations/003_outbox.sql`, `migrations_outbox_test.go` |
| Atomic emit + cursor advance in one tx | `cron/internal/tick/tick.go` lines 137–166 (`fireOne`) |
| Producer wiring (`Spec.Feed`/`Producer`/`Events`) | `cron/cmd/cron/main.go` lines 32–100 |
| MCP handler + tool descriptors | `dropbox/internal/mcp/{mcp,tools}.go` |
| Internal interval daemon lifecycle | `dropbox/internal/dropbox/sync.go` (`Run`/bootstrap/steady-state) |
| Deploy config + SSM secret seeding | `dropbox/etc/{deploy.env,manifest.env}`, `dropbox/bin/secrets`, `dropbox/.envrc` |
| nginx fragment | `cron/etc/nginx.conf` (`__PORT__` placeholders) |
| Consent CLI | *net-new* — no repo template; stdlib loopback auth-code flow (see P1) |

**Allocated loopback port: `3008`** (3000–3007 are taken).

---

## Execution model — front-loaded manual help, then unattended

This effort is structured so that **every human-in-the-loop step collapses into a
single one-time gate that happens as early as possible**, after which the bulk of
the work — build, live verification, and deploy — runs **unattended**.

```
P1  (Claude, unattended)  ── scaffold + ALL bootstrap tooling
       │
   ★ BOOTSTRAP SITTING  ── the ONE attended step (you; ~minutes, once)
       │
P2 ─ P3 ─ P4 ─ P5  (Claude, unattended)  ── client, producer, MCP, deploy
```

- **The only attended step is the BOOTSTRAP SITTING** (the "P0b sitting" from the
  design review). P1 deliberately produces *everything that sitting needs* —
  the consent CLI, the deploy config, `bin/secrets`, the `.envrc` — so the
  sitting is one uninterrupted pass with no return trips.
- **Nothing after the sitting requires a human.** Once `~/.secrets/GMAIL_*`
  exist, direnv is allowed, and SSM is seeded, P2–P5 build, live-verify against
  the real mailbox, and deploy to the `int` box with no further input.
- **Full unattended deploy is authorized** (decisions review): `bin/bump`
  (commit + push `main`), `bin/ship`, `opsctl setup/stage/deploy`, and the
  dashboard restart all run without a gate.

### Live-verification policy (applies to every phase's Verify)

Once the bootstrap sitting is done the refresh token exists, so phases may verify
against **real Gmail**. Verification is tiered by blast radius:

- **Read-only — run live and unattended freely:** `getProfile`, `history.list`,
  `list`, `labels`, `read`, `thread`.
- **One controlled mutation — allowed unattended:** `send` a single email **to
  yourself** (the only way to make the producer observe a real `mail.sent` +
  `mail.received`); `draft`, and `label`/`unlabel` on that test message.
- **Never unattended:** `trash` and **`delete`** (permanent expunge). Their
  correctness is covered by fake-client unit tests only; live exercise is a
  human spot-check. `mail.deleted` is therefore **not** live-verified
  unattended.

## Sizing principle

A phase is scoped so that one subagent, starting fresh, can hold the relevant
files, do the work, and verify it within a single context budget. The natural
seam between the two largest phases (**P3 producer-engine** and **P4 MCP**) is
the Gmail REST client built in **P2**: both consume it, so P2 is built and
tested once, in isolation, before either consumer. If a coarser cut is ever
wanted, the natural merges are P4→P2 (client + its MCP consumer) and P5→P3.

## Dependency chain

```
P1 ──▶ ★SITTING ──▶ P2 ──▶ P3 ──▶ P5
                          └─▶ P4 ─┘
```

P1 (scaffold + bootstrap tooling) is the prerequisite for everything and for the
sitting. The BOOTSTRAP SITTING gates P2+ (they need the credentials it produces
for live verification). P2 (the Gmail client) is the shared dependency of both
the producer engine (P3) and the MCP surface (P4). P3 and P4 are independent of
each other but, per the no-parallel rule, run in numeric order. P5 (deploy) lands
last, once the binary is feature-complete. Executing in listed order satisfies
every edge.

## Scope guardrails (from decisions §Scope, §3 deferrals)

Producer-only on the event plane — **consumes nothing**. **No** Cloud Pub/Sub /
`users.watch`, **no** SES/AWS/DKIM change, **no** outbound event-reaction story,
**no** per-user token store. The following are explicitly **deferred** and must
not creep into any phase: `reply` tool, attachment download, `sync_now` tool,
permanent-expunge (`messagesDeleted`) events, mail-triggered agent sessions.

---

## P1 — Service scaffold + DB schema + chassis wiring + bootstrap tooling

*Decisions §1, §3 build-order step 1. Claude, unattended. The foundation for
everything; also produces every artifact the BOOTSTRAP SITTING consumes, so the
sitting needs no code written mid-pass.*

### Service scaffold (mirror dropbox)

- New `gmail/` module: `go.mod` (module path matching siblings) with committed
  `replace appkit => ../appkit` and `replace eventplane => ../eventplane`
  directives so the prod `GOWORK=off` build is deterministic (mirror
  `dropbox/go.mod`).
- `cmd/gmail/main.go` composition root calling `appkit.Main(appkit.Spec{...})`
  with the minimal producer shape: `App:"gmail"`, `Mount:"/srv/gmail/"`,
  `Port:3008`, `MCP:true`, `Feed:"/feed"`, `Migrations: db.FS`,
  `ManifestExtras` carrying `OUTBOX_RETENTION_DAYS=7` /
  `OUTBOX_RETENTION_MAX_ROWS=1000000` (copy dropbox/cron). `Handlers` mounts a
  **stub MCP handler** exposing only `health` + `reflection` so the binary
  serves; `Producer`/`Workers` left for P3.
- `internal/db/` with embedded `migrations/`:
  - `001_schema_migrations.sql` — chassis table (copy verbatim).
  - `002_gmail.sql` — domain schema: the `sync_state` table holding the
    `historyId` cursor (single row, `id INTEGER PRIMARY KEY CHECK (id = 1)`,
    `history_id TEXT`, `updated_at TEXT` — mirror dropbox `sync_state` shape).
  - `003_outbox.sql` — **byte-identical** to `outbox.SchemaSQL` (copy from
    dropbox/cron, including the header comment).
- `internal/db/migrations_outbox_test.go` — the byte-equality guard asserting
  `003_outbox.sql` ≡ `outbox.SchemaSQL` (copy from dropbox).
- `internal/mcp/` stub: `mcp.go` transport + `tools.go` with `health`
  (envelope + identity from `X-Owner-Email`/`X-Client-Id`) and `reflection`
  (static, `publishes` = the three mail events even though emission lands in
  P3). Tool prefix `ikigenba_gmail_`.
- `VERSION` = `0.1.0`; `Makefile` (copy dropbox's build/run/test/vet/fmt).
- Add `./gmail` to the root `go.work` `use` block (alphabetical, before
  `./ledger`).

### Bootstrap tooling (moved forward from the old P5, so the sitting is one pass)

- `cmd/consent/main.go` — the **one-time consent CLI** (net-new; stdlib only).
  Reads `GMAIL_CLIENT_ID` / `GMAIL_CLIENT_SECRET` from the environment, starts a
  transient loopback HTTP server on `127.0.0.1:<ephemeral>` (desktop OAuth
  clients allow loopback redirects implicitly — no URI registration needed),
  builds the auth URL (`scope=https://mail.google.com/`, `access_type=offline`,
  `prompt=consent`), opens the browser (`xdg-open`), captures the returned
  `code`, exchanges it at `https://oauth2.googleapis.com/token`
  (`grant_type=authorization_code`), and **writes the refresh token directly to
  `~/.secrets/GMAIL_REFRESH_TOKEN` (`0600`)**. It prints **only a masked
  confirmation** (e.g. `wrote GMAIL_REFRESH_TOKEN (1a2b…wxyz)`) and **never**
  emits the token value — so it is safe to run under a `! <command>` whose stdout
  lands in the agent transcript (secrets skill).
- `etc/deploy.env` — `ACCOUNT=int`, `SSH_USER`, `SSH_KEY`, `APEX_SUFFIX` (copy
  `dropbox/etc/deploy.env`).
- `etc/manifest.env` — static committed mirror (`APP=gmail`, `MOUNT=/srv/gmail/`,
  `DEFAULT=false`, `PORT=3008`, `MCP=true`, `FEED=/feed`, + the two outbox
  retention keys). The binary regenerates this at deploy; the committed copy lets
  `bin/secrets` resolve `$APP` during the sitting (dropbox works the same way).
- `bin/secrets` — workstation-side SSM seeder (copy/adapt `dropbox/bin/secrets`):
  non-destructive read-modify-write of the `gmail` key in
  `/ikigenba/${ACCOUNT}/app-config`, seeding **`GMAIL_CLIENT_ID`,
  `GMAIL_CLIENT_SECRET`, `GMAIL_REFRESH_TOKEN`**. Values come from
  `~/.secrets/<NAME>`, are masked in the summary, and are never printed. Requires
  a live `aws sso login --profile int` (the operator runs that interactively).
- `.envrc` — committed (references only, no literals; mirror `dropbox/.envrc`):
  ```sh
  source_up
  export GMAIL_CLIENT_ID="$(cat ~/.secrets/GMAIL_CLIENT_ID)"
  export GMAIL_CLIENT_SECRET="$(cat ~/.secrets/GMAIL_CLIENT_SECRET)"
  export GMAIL_REFRESH_TOKEN="$(cat ~/.secrets/GMAIL_REFRESH_TOKEN)"
  ```
- `docs/gmail-bootstrap-sitting.md` (or a section in this plan's runbook) — the
  ordered operator checklist the sitting follows (see ★ below).

**Touches:** `gmail/go.mod`, `gmail/go.sum`, `gmail/Makefile`, `gmail/VERSION`,
`gmail/cmd/gmail/main.go`, `gmail/cmd/consent/main.go`,
`gmail/internal/db/{db.go,migrations/*.sql,migrations_outbox_test.go}`,
`gmail/internal/mcp/{mcp.go,tools.go,tools_test.go}`,
`gmail/etc/{deploy.env,manifest.env}`, `gmail/bin/secrets`, `gmail/.envrc`,
`go.work`, bootstrap-sitting runbook.
**Verify:** `go build ./gmail/...`; `go test ./gmail/...`; `GOWORK=off go build
./...` from `gmail/`; run `migrate` then `serve` against a temp DB and confirm
`POST /mcp` returns the health envelope; outbox byte-equality test passes;
`go vet ./gmail/...` clean (the consent CLI compiles). No live credentials exist
yet — verification here is build/test only.

---

## ★ BOOTSTRAP SITTING — the one-time manual gate (you)

*The single attended step. Runs after P1, before P2. Mints all credentials and
seeds them everywhere they're consumed. Strict order — each step feeds the next.
This is the "P0b sitting" from the design review.*

1. **Create a dedicated GCP project** for gmail (isolation: decisions §2 revised
   — keeps the production/restricted-scope changes off the dashboard's identity
   project).
2. **Configure the consent screen** in that project: app name + support email;
   **add the `https://mail.google.com/` scope**; set publishing status to
   **"In production"** and leave the app **unverified**. *(Testing status revokes
   refresh tokens after 7 days; production-unverified yields a durable token with
   no CASA assessment — verification is only needed to remove the warning or
   serve other users, neither of which a single-owner box needs.)*
3. **Create a Desktop-type OAuth client** → note `GMAIL_CLIENT_ID` /
   `GMAIL_CLIENT_SECRET`.
4. Write those two into `~/.secrets/GMAIL_CLIENT_ID` and
   `~/.secrets/GMAIL_CLIENT_SECRET`.
5. `direnv allow` in `gmail/` (the committed `.envrc`; the `GMAIL_REFRESH_TOKEN`
   `cat` is harmlessly empty until step 7).
6. Run the consent CLI (`! go run ./cmd/consent` from `gmail/`, or the built
   binary). Click through the **one-time "unverified app"** warning
   (*Advanced → proceed*). The CLI **self-writes** `~/.secrets/GMAIL_REFRESH_TOKEN`.
7. `direnv reload` (picks up the now-populated token).
8. `aws sso login --profile int`, then `gmail/bin/secrets` → seed the three
   `GMAIL_*` secrets into SSM `/ikigenba/int/app-config` under the `gmail` key.

**Contingency:** if step 6 fails to mint a working token (Google blocks the
restricted-scope consent on an unverified production app), **stop and surface it**
— do not loop. The heavyweight fallback is CASA verification, which is a separate
discussion. After step 8 succeeds, the rest of the build proceeds unattended.

---

## P2 — Gmail REST client + `tokenSource`

*Decisions §1 (connector half). Claude, unattended. Depends on P1 + the sitting.
The shared dependency of P3 and P4; pure library, not yet wired into serve.*

- `internal/gmail/client.go`:
  - `Config{ClientID, ClientSecret, RefreshToken string}` — the three OAuth
    credentials, all dedicated: **`GMAIL_CLIENT_ID` / `GMAIL_CLIENT_SECRET` /
    `GMAIL_REFRESH_TOKEN`** (decisions §2 revised — dedicated desktop client, no
    `GOOGLE_*` reuse).
  - `tokenSource` — copy dropbox's directly (mutex, cached `accessTok` + expiry
    with 60s slack, `invalidate()`, force-refresh once on 401). Point
    `refreshLocked` at Google's token endpoint
    `https://oauth2.googleapis.com/token` with `grant_type=refresh_token` +
    `client_id`/`client_secret`/`refresh_token`. A dead/revoked refresh token
    surfaces as `invalid_grant` on the refresh call itself — **fail loudly** with
    a clear log line (do not spin); this is the signal the token needs re-minting.
  - `rpcCall`-style helper: `Authorization: Bearer <tok>`, one 401 retry after
    `invalidate()` + force-refresh (mirror dropbox lines 229–276).
  - The full Gmail REST method set the service needs, against
    `https://gmail.googleapis.com/gmail/v1/users/me/...`:
    - **Producer-side (P3 consumes):** `GetProfile()` → `{historyId}`;
      `HistoryList(startHistoryId, pageToken)` →
      `messagesAdded`/`messagesDeleted`/`labelsAdded`/`labelsRemoved` + next
      page + `historyId`; `MessageGet(id, format)` → headers/labels/snippet
      (and attachment **metadata** only — filename/size/mime; no blob download).
    - **MCP-side (P4 consumes):** `MessagesList(q, pageToken)`
      (`messages.list`, the one call behind both list & search);
      `ThreadGet(id)`; `MessagesSend(rawRFC2822)` (base64url); `DraftCreate`;
      `LabelsList`; `MessageModify(id, add[], remove[])` (powers label/unlabel,
      archive, mark-read); `MessageTrash(id)`; `MessageDelete(id)` (permanent).
  - Typed result structs + error sentinels (mirror dropbox `types.go`); the
    refresh token is **never** logged.
- `internal/gmail/client_test.go` — drive every method through an injected fake
  `http.RoundTripper`: token cache hit, expiry refresh, 401→force-refresh→retry,
  `invalid_grant`→loud failure, and the JSON decode of each endpoint's canned
  response.

**Touches:** `gmail/internal/gmail/{client.go,types.go,client_test.go}`.
**Verify:** `go test ./gmail/internal/gmail/...`; binary still builds. **Live
smoke (read-only, per policy):** with the real token, `GetProfile()` returns a
`historyId` and `MessagesList("")` returns real messages.

---

## P3 — Store + History-API producer + internal poll daemon

*Decisions §1 (producer half + scheduled half + cursor lifecycle). Claude,
unattended. Depends on P2. The logic core — change detection, event derivation,
atomic cursor advance.*

- `internal/gmail/store.go` — `sync_state` accessors on `*sql.Tx`:
  `GetHistoryID(tx) (string, ok, err)` and `SetHistoryID(tx, id, updatedAt)`
  (upsert on `id=1`). Mirror dropbox `GetCursor`/`SetCursor`.
- `internal/gmail/events.go` — the three **static** event types and payloads
  (decisions §1 table):
  - `mail.received` ← `messagesAdded` carrying `INBOX` →
    `{id, thread_id, from, subject, snippet, received_at}`.
  - `mail.sent` ← `messagesAdded` carrying `SENT` (not `INBOX`) →
    `{id, thread_id, to, subject, snippet, sent_at}`.
  - `mail.deleted` ← `labelsAdded: TRASH` → `{id, thread_id, subject,
    deleted_at}`.
  - `Events outbox.Registry` (with samples) for reflection + Append-time
    validation; an `outboxProducer` wrapper over `*outbox.Outbox` (mirror
    dropbox `events.go`).
- `internal/gmail/sync.go` — the engine (mirror dropbox `Engine`, but **poll**
  not longpoll):
  - **Bootstrap / fresh boot** (no stored `historyId`): seed the cursor from
    `GetProfile().historyId`, emit **nothing** for pre-existing mail (decisions
    §"Cursor lifecycle").
  - **Steady state**: tick every `GMAIL_POLL_INTERVAL` (config-from-env,
    default `60s`, via `config.EnvOrDuration`). Each tick: `HistoryList` from
    the stored cursor, paginate, derive events per the table above. Added
    messages are **enriched with one `MessageGet` per message** (no batching —
    fine at single-owner volume).
  - **Atomic per-poll commit**: in **one transaction**, `outbox.Append` the
    derived events **and** `SetHistoryID` to the new `historyId` — the same
    "emitted == recorded as emitted" pattern as cron's `fireOne`
    (`cron/internal/tick/tick.go` 137–166). `Ring()` after commit.
  - **Stale-cursor resync**: a `history.list` 404 (Gmail retains ~1 week)
    is treated **identically to a fresh boot** — reset the cursor to the
    current `GetProfile().historyId`, emit nothing for the gap, log a warning.
    Never backfill (decisions §"Stale-cursor resync").
- Wire into `cmd/gmail/main.go`: build the client + service in `Handlers`;
  `Producer` injects the outbox into the service and constructs the engine over
  the shared DB handle; `Workers` runs `engine.Run(ctx)` for the serve lifetime
  (SIGTERM cancels; clean `ctx.Err()` returns nil — mirror dropbox lines
  192–199). `Spec.Events = gmail.Events`. Read the three `GMAIL_*` secrets +
  `GMAIL_POLL_INTERVAL` at the composition root.
- `internal/gmail/sync_test.go` — drive the engine with a fake client: fresh
  boot emits nothing, a `messagesAdded`+INBOX page emits one `mail.received`
  and advances the cursor in one tx, INBOX-filter keeps own-sends out of
  `mail.received` while emitting `mail.sent`, `labelsAdded:TRASH` emits
  `mail.deleted`, and a 404 resets the cursor without emitting.

**Touches:** `gmail/internal/gmail/{store.go,events.go,sync.go,*_test.go}`,
`gmail/cmd/gmail/main.go`.
**Verify:** `go test ./gmail/...`; `serve` against a temp DB and confirm the poll
daemon boots and advances the cursor. **Live (per policy):** `send` one email
**to yourself**, then confirm the next poll emits both `mail.sent` and
`mail.received` on `GET /feed` and advances `historyId` in one tx. `mail.deleted`
is **not** live-verified (trash is a never-unattended mutation) — its derivation
is covered by `sync_test.go`.

---

## P4 — MCP surface (full normal-mailbox tool set)

*Decisions §1 (connector half, MCP surface). Claude, unattended. Depends on P2
(the client); independent of P3 but runs after it per the no-parallel rule.*

- Replace the P1 stub with the full tool set in `internal/mcp/tools.go`, each a
  descriptor (schema) + handler calling the P2 client (mirror dropbox
  `tools.go` / `dispatchTool`):
  - `list` — list/search messages; optional Gmail `q` (`from:`, `subject:`,
    `is:unread`, …) + pagination. **One tool** over `MessagesList` (list and
    search are the same call).
  - `read` — full message: headers + body + **attachment metadata** (filename,
    size, mime). No blob download.
  - `thread` — whole thread via `ThreadGet`.
  - `send` — RFC-2822 message (base64url `MessagesSend`).
  - `draft` — create a draft via `DraftCreate` (distinct from `send`).
  - `labels` — list available labels.
  - `label` / `unlabel` — apply / remove a label on a message via
    `MessageModify` (covers archive = remove `INBOX`, mark-read = remove
    `UNREAD`).
  - `trash` — `MessageTrash` (recoverable).
  - `delete` — `MessageDelete` (permanent).
  - Retain `health` + `reflection` from P1.
- `internal/mcp/tools_test.go` — per-tool dispatch + schema tests against a fake
  client (argument validation, the q/pagination passthrough, the
  archive/mark-read label mappings).
- **Do not** add `reply`, attachment download, or `sync_now` (deferred,
  decisions §3).

**Touches:** `gmail/internal/mcp/{mcp.go,tools.go,tools_test.go}`.
**Verify:** `go test ./gmail/internal/mcp/...`; via local nginx `./run`, call a
**read-only** tool (`list`/`labels`) over `POST /srv/gmail/mcp` and confirm the
envelope returns real mail. `send`/`draft` may be live-exercised once
(send-to-self) per policy; **`trash`/`delete` are not run live unattended** —
they are covered by `tools_test.go` and left for a human spot-check.

---

## P5 — Deploy plumbing + on-box deploy

*Decisions §2 (OAuth/credential bootstrap), §3 build-order step 2. Claude,
unattended (full deploy authorized). Depends on P3 + P4 (feature-complete
binary). The deploy config / `bin/secrets` / `.envrc` already landed in P1 and
the secrets were seeded in the sitting; P5 is the on-box half only.*

- `etc/nginx.conf` — the location fragment with `__PORT__` placeholders (copy
  `cron/etc/nginx.conf`): the unauthenticated PRM bootstrap location, the
  `= /srv/gmail/feed { return 404; }` public-feed block, the authenticated
  `/srv/gmail/` location with `auth_request /_authn` and the
  `X-Owner-Email`/`X-Client-Id` passthrough, and the 5xx error-page handler.
- Local-dev nginx: add the `/srv/gmail/` fragment under `nginx/` so `./run`
  routes it on :8080 (mirror how cron/dropbox are wired there).
- Confirm `VERSION` (`0.1.0`), the `go.work` entry, `etc/deploy.env`,
  `etc/manifest.env`, and `ManifestExtras` from P1 (so `gmail manifest` emits
  `APP/MOUNT/DEFAULT/PORT/MCP/FEED` + the outbox retention keys).
- **On-box bring-up (unattended):**
  - `ssh int sudo opsctl setup gmail --port 3008 --fragment <path>` (once). **Do
    not** pass `--defer-nginx` — the `int` box already runs the dashboard + apex
    cert, so setup should run the real `nginx -t`/reload.
  - Standard deploy flow: `bin/bump gmail <…>` (commit + push `main`) →
    `bin/ship gmail` → `ssh int sudo opsctl stage gmail v<ver> --artifact …` →
    `ssh int sudo opsctl deploy gmail v<ver>`.
  - **Restart the dashboard** afterward so it re-reads manifests and lists the
    new MCP service.
  - (SSM secrets were already seeded in the sitting; the box injects them via its
    instance role at launch — no operator AWS creds needed at deploy time.)

**Touches:** `gmail/etc/nginx.conf`, `nginx/` (dev fragment); on-box state via
`opsctl`. (Deploy config, `bin/secrets`, `.envrc` already committed in P1.)
**Verify:** local nginx `./run` routes `/srv/gmail/`; `gmail manifest` prints the
expected key set; after deploy, `ssh int sudo opsctl status` shows the unit; a
live **read-only** `list` over MCP through nginx returns real mail and the poll
daemon emits a real `mail.received`/`mail.sent` on `/feed` (send-to-self per
policy). Rollback if any step fails: `ssh int sudo opsctl rollback gmail`.

---

## Done criteria

The effort is complete when: the `gmail` binary builds static `linux/amd64`
under `GOWORK=off`; all `gmail/...` tests pass; the MCP surface exposes the full
normal-mailbox tool set (decisions §1) behind nginx with trusted identity
headers; the internal poll daemon advances a `historyId` cursor and emits
`mail.received` / `mail.sent` / `mail.deleted` on `/feed` with the atomic
emit-and-advance guarantee; fresh-boot and stale-cursor (404) both reset without
backfilling; the service is deployed to the `int` box via the standard
bump→ship→stage→deploy flow with a **dedicated** `GMAIL_CLIENT_ID` /
`GMAIL_CLIENT_SECRET` / `GMAIL_REFRESH_TOKEN` (dedicated GCP project + desktop
client, publishing status In-production/unverified so the token is durable); and
the dashboard lists the new MCP service. All items in the scope guardrails remain
unbuilt — and the **only** human step in the entire effort was the one BOOTSTRAP
SITTING.

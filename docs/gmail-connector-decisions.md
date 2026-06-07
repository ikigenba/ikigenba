# Gmail-Connector Decisions

A record of decisions made in a design discussion about giving the suite an
**owner-mailbox connector** — concretely, a new `gmail` service that connects to
the owner's Google account, exposes the **normal mailbox operations as MCP
tools**, and **polls for mail changes on an internal interval and emits them as
events** (event-plane producer). This file records **decisions only**, not a
plan. Detailed planning happens in a follow-up session on top of these (a
`gmail-connector-plan.md`, mirroring `event-triggering-plan.md`).

This work builds on the just-completed event-triggering effort
(`event-triggering-decisions.md` / `-plan.md`, phases P1–P9): the fixed
at-least-once consumer engine, the `cron` service, `agent` (formerly `ralph`),
and `notify`. It reuses the event-plane **producer** primitives rather than
adding new machinery.

Date of discussion: 2026-06-06 (revised 2026-06-07 — scope reduced to the
connector + inbound-event producer, see "Scope" below; revised again 2026-06-07
during plan review — credential bootstrap reworked: dedicated GCP project +
dedicated desktop OAuth client, publishing status **In production (unverified)**
not Testing, and a self-writing consent CLI; see §2).

---

## Scope

This effort is **only the `gmail` connector**: an OAuth-backed mailbox service
with an MCP surface over the normal Gmail operations, plus a producer half that
emits events when mail changes. It is **producer-only** on the event plane — it
consumes nothing.

An earlier draft of this doc also covered an outbound, event-driven email path
(an `agent` `notify_owner` tool emitting `notice.posted`, and `gmail` consuming
that event to send mail, driven by a weekly-research cron example). **That whole
outbound-reaction story is dropped** and recorded in "Deferred / out of scope".
The MCP `send` / `draft` tools remain — sending is a normal mailbox operation —
but sending is **not** wired to any event.

---

## 0. Framing

- **Drive from missing functionality** (same philosophy as the event-triggering
  work). The gap: the platform has no first-class **mailbox connector** — nothing
  it can read/operate through, and nothing that surfaces incoming mail as a fact
  on the event plane.
- **The target:**

  ```
  gmail (internal-daemon polled) ─▶ emits mail.received / mail.sent / mail.deleted
  owner (or future agent) ────────▶ operates the mailbox via gmail's MCP tools
  ```

- **It generalizes the platform's trigger sources.** `cron` emits *time* events;
  `gmail` emits *mail* events. A future `agent` session could be triggered by mail
  via the same `session_triggers` seam — "run this agent when mail matching X
  arrives" becomes possible for free later. Not in scope now, but it is the reason
  the producer half matters.

---

## 1. The `gmail` service

- **New, dedicated, deployable service** (joining `cron` as a recent addition),
  **not** folded into `notify`. Full mailbox access is its own domain, far larger
  than a push service needs.
- **Structurally `dropbox`'s twin.** `dropbox` is *"an external-OAuth connector
  that keeps a local mirror in sync via a loopback daemon + event-plane
  producer."* `gmail` is the same shape pointed at Google mail: an external-OAuth
  connector that **polls the mailbox and emits change events**. The dropbox
  service is the template to mirror (token source, client, MCP surface, producer
  wiring, internal daemon).
- **Two roles only: connector + producer.** It runs a producer (`/feed`) and an
  internal poll daemon. It is **not** a consumer of `cron`, `agent`, or anything
  else.

### Connector half (mirror `dropbox`)

- **Full mailbox scope: `https://mail.google.com/`** (read + send + permanent
  delete). This is Google's **restricted** scope tier. *(Least-privilege note,
  recorded once and then accepted: a leaked full-scope refresh token can read and
  permanently delete the entire mailbox; the owner has chosen full access
  deliberately on their single-tenant box.)*
- **Single owner refresh token as a deployment secret** — mirrors dropbox's
  single `DROPBOX_REFRESH_TOKEN`, fitting the one-box-one-owner model. A
  `tokenSource` exchanges refresh → short-lived access token, caches it, and
  force-refreshes on a 401 (copy dropbox's `tokenSource` directly).
- **MCP surface — the full normal-mailbox set:**
  - `list` — list/search messages; takes an optional Gmail `q` query
    (`from:`, `subject:`, `is:unread`, …) with pagination. **`list` and `search`
    are one tool** — Gmail's `messages.list` is the same call either way.
  - `read` — full message: headers + body, plus **attachment metadata**
    (filename, size, mime). Attachment *download* (base64 blobs through MCP) is
    deferred.
  - `thread` — read a whole thread.
  - `send` — send an RFC-2822 message (base64url `messages.send`).
  - `draft` — create a draft (distinct from `send`; included).
  - `labels` — list available labels.
  - `label` / `unlabel` — apply / remove a label on a message (covers
    archive = remove `INBOX`, mark-read = remove `UNREAD`).
  - `trash` — move to Trash (recoverable).
  - `delete` — permanent delete.
  - `reply` is **deferred** — it is `send` plus `In-Reply-To` / `References`
    threading headers and a `threadId`; a thin helper to add later.

### Producer half — "emit events on changes"

- **Change detection via the Gmail History API**, not real-time push.
  `users.history.list(startHistoryId=cursor)` returns `messagesAdded` /
  `messagesDeleted` / `labelsAdded` / `labelsRemoved` since the stored
  `historyId`. The service holds **`historyId` as its sync cursor** — exactly
  analogous to `cron`'s `last_slot` and a consumer's `feed_offset`.
- **Per poll, in one transaction:** `outbox.Append` the derived events **and**
  advance the stored `historyId` (same atomic "emitted == recorded as emitted"
  pattern as cron's tick worker). Producer wiring mirrors crm/ledger/cron:
  `outbox.SchemaSQL` migration, `Spec.Feed = "/feed"` + `Spec.Producer`,
  **static** `Spec.Events`.
- **No Cloud Pub/Sub / `users.watch`.** Real-time Gmail push needs a public
  webhook + a GCP broker, which directly contradicts the suite's operating bet
  ("no broker, accept scheduled downtime"). **Polling is the deliberate choice.**

#### Events emitted — three static types

Each maps to a History signal. Added messages are enriched with one
`messages.get` per message (fine at a single owner's volume — no batching).

| Event | History signal | Detection rule | Payload |
|---|---|---|---|
| `mail.received` | `messagesAdded` | added message carries `INBOX` | `{id, thread_id, from, subject, snippet, received_at}` |
| `mail.sent` | `messagesAdded` | added message carries `SENT` (not `INBOX`) | `{id, thread_id, to, subject, snippet, sent_at}` |
| `mail.deleted` | `labelsAdded: TRASH` | message moved to Trash | `{id, thread_id, subject, deleted_at}` |

- **`mail.sent` covers our own sends uniformly.** An MCP `send` shows up as a
  `messagesAdded` + `SENT`, so it naturally emits `mail.sent` — same path as mail
  sent from the Gmail UI. The `INBOX` filter keeps our sends out of
  `mail.received`.
- **`mail.deleted` fires on move-to-Trash, not permanent expunge.** Discarding
  mail (the `labelsAdded: TRASH` moment) is the meaningful human signal; the
  message still exists in Trash, so its payload is still fetchable. A later
  permanent expunge of that same message (`messagesDeleted`) is **not** modeled
  and emits nothing — an accepted asymmetry.

### Scheduled half — internal interval daemon (mirror `dropbox`)

- **An internal interval daemon polls once per interval.** Cadence is
  config-from-env: **`GMAIL_POLL_INTERVAL`, default `60s`**. This mirrors
  dropbox's self-contained daemon style.
- **Chosen over a cron-consumed poll.** A cron tick would make cadence tunable
  via the crontab without redeploy, but it adds a cross-service dependency (cron
  down → no polling) and a consumer loop. With no outbound/event-reaction story
  in scope, that justification fell away, so `gmail` stays a pure self-contained
  connector exactly like dropbox. Re-tuning poll cadence is a redeploy, which a
  single owner rarely needs.
- A manual `sync_now` MCP tool (on-demand poll) is a cheap later add, not a
  decision to make now.

#### Cursor lifecycle

- **Fresh boot** — no stored `historyId`: bootstrap the cursor from
  `users.getProfile().historyId` and emit **nothing** for pre-existing mail. The
  cursor starts "now"; the inbox is **not** backfilled into `mail.received`.
- **Stale-cursor resync** — Gmail retains history for only ~a week. If `gmail` is
  down longer than that, the stored `historyId` goes stale and `history.list`
  returns 404. Treat this **identically to a fresh boot**: reset the cursor to
  the current `getProfile().historyId`, emit nothing for the gap, log a warning.
  This is the best-effort philosophy applied to mail (same flavor as cron's
  no-catch-up) — never a backfill flood.

---

## 2. OAuth / credential bootstrap

- **Three dedicated secrets, no reuse of `GOOGLE_*`:** `GMAIL_CLIENT_ID`,
  `GMAIL_CLIENT_SECRET`, and `GMAIL_REFRESH_TOKEN` (full `https://mail.google.com/`
  scope). The `GOOGLE_CLIENT_ID`/`SECRET` used by `dashboard/internal/googleidp`
  for "Sign in with Google" are **not** reused, and the
  `~/.secrets/hal-google-oauth.json` credential (a different project's, without
  the mail scope) is not reused either. The same isolation argument the dropbox
  service applied to its refresh token applies here to the **whole client**:
  explicit ownership, independent rotation, isolated blast radius. All three
  follow the uniform secret pattern (one `.envrc` line each referencing
  `~/.secrets/<NAME>`, read from the environment at the composition root).
- **Dedicated GCP project + dedicated Desktop-type OAuth client.** A *separate*
  GCP project hosts the consent screen and a new **Desktop ("installed app")**
  OAuth client (which is the correct type for a CLI loopback consent and gets
  implicit loopback-redirect support — no redirect URI to register). The
  separate project is the isolation boundary that matters: the two project-wide
  changes below (adding a restricted scope, flipping publishing status) cannot
  touch the dashboard's production "Sign in with Google" client, which lives in
  its own project.
- **Publishing status "In production" (unverified), NOT "Testing".** The earlier
  "keep it in Testing" decision was wrong on a critical point: Google **revokes
  refresh tokens after 7 days** for apps in Testing status with External user
  type — which would break the connector weekly and force recurring manual
  re-consent. Production-status tokens are durable. Leaving the app **unverified**
  still requires **no CASA assessment** — verification is only needed to *remove*
  the one-time "unverified app" warning or to serve *other* users, neither of
  which a single-owner box needs. So the original goal (no assessment) is met,
  and the token no longer expires. The consent screen adds the
  `https://mail.google.com/` scope. **Contingency:** if Google blocks the
  restricted-scope consent on an unverified production app, stop and reconsider —
  the heavyweight fallback (CASA verification) is a separate discussion.
- **One-time consent flow via a self-writing CLI.** A small stdlib Go tool
  (`gmail/cmd/consent`), run as a `! <command>` human-in-the-loop step, performs
  the offline-access loopback auth-code exchange and **writes the refresh token
  directly to `~/.secrets/GMAIL_REFRESH_TOKEN` (`0600`)**, printing only a masked
  confirmation. It **never prints the token value** — so even though `!`-command
  stdout lands in the agent transcript, the secret never enters the agent's
  context (secrets skill). *(The earlier "prints the refresh token for the owner
  to paste" wording contradicted that guarantee and is dropped.)*
- **No metaspot / SES / DKIM / infrastructure change.** An earlier SES design
  (its own domain identity, DKIM in the `int/` account, an `ses:SendEmail` IAM
  policy) is fully dropped — operating the mailbox via Google needs none of it.
  The only AWS touch is the **standard per-app SSM `app-config` secret seeding**
  every service does (`bin/secrets` writing the `gmail` key under
  `/ikigenba/<account>/app-config`); that is routine deploy plumbing, not an
  infrastructure change.

---

## 3. Sequencing and deferrals

### Build order

1. **The `gmail` service** — connector (`tokenSource` + MCP surface) +
   History-API producer (three events) + internal poll daemon with cursor
   lifecycle.
2. **Deploy plumbing** — `opsctl setup gmail`, manifest, nginx fragments,
   `VERSION` / `go.work`, and the one-time token bootstrap.

The `ralph` → `agent` rename already landed (reflected in the top-level
CLAUDE.md). Decomposition into subagent-sized phases is the follow-up plan doc's
job.

### Deferred / out of scope

- **Outbound, event-driven email** — the dropped story: an `agent` `notify_owner`
  tool emitting `notice.posted`, `gmail` consuming `notice.posted` to send mail,
  and the weekly-research cron example that motivated it. Sending stays available
  as an MCP tool (`send` / `draft`) but is wired to no event. Its own future
  track if the platform wants event-driven outbound mail.
- **Agent-as-MCP-client** — the sandboxed agent calling sibling services' MCP
  tools directly (read / triage / delete mail mid-run, send directly). Its own
  future track (egress + auth from inside the sandbox is a large,
  security-sensitive change).
- **Mail-triggered agent sessions** — wiring `gmail`'s `mail.received` into
  `session_triggers` so a session runs on incoming mail. The producer half makes
  it possible; not built this phase.
- **Cron-consumed polling** — rejected in favor of the internal daemon (§1).
- **`reply` MCP tool** and **attachment download** — cheap later adds.
- **Permanent-expunge (`messagesDeleted`) events**, richer mail payloads,
  recipient routing / multiple recipients, and any channel-abstraction.
- **Per-user / multi-account Gmail** (a token store keyed by owner, the dashboard
  grants story) — single owner refresh-token secret for now.
- **Real-time Gmail push** (`users.watch` + Pub/Sub) — rejected, see §1.
- **`sync_now` manual-poll MCP tool** — cheap later add.

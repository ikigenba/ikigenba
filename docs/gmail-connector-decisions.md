# Gmail-Connector Decisions

A record of decisions made in a design discussion about giving the suite an
**owner-mailbox connector and an outbound email path** — concretely, a new
`gmail` service that connects to the owner's Google account, **polls for mail
changes on a cron schedule and emits them as events** (event-plane producer),
and **sends mail in reaction to `notice.posted` events** the `agent` service
emits. This file records **decisions only**, not a plan. Detailed planning
happens in a follow-up session on top of these (a `gmail-connector-plan.md`,
mirroring `event-triggering-plan.md`).

This work builds directly on the just-completed event-triggering effort
(`event-triggering-decisions.md` / `-plan.md`, phases P1–P9): the fixed
at-least-once consumer engine, the `cron` service, `agent` (formerly `ralph`)
as a cron consumer + outcome producer, and `notify` as an outcome consumer. It
reuses every one of those primitives rather than adding new machinery.

Date of discussion: 2026-06-06.

---

## 0. Framing

- **Drive from missing functionality** (same philosophy as the event-triggering
  work). The originating gap: *"create a scheduled research session in `agent`
  that runs Sunday morning and emails me its results."* That surfaced two missing
  pieces — an agent way to emit **its own exact text** as an owner-facing message,
  and an **outbound email** path — plus, on inspection, a third latent want: a
  **first-class mailbox connector** the platform can both react to and act on.
- **The end-to-end target:**

  ```
  cron.research-weekly ─▶ agent runs research ─▶ agent emits notice.posted ─▶ gmail sends email to owner
  gmail (cron-polled)  ─▶ emits mail.received  ─▶ (future) agent sessions triggered by incoming mail
  notify               ─▶ ntfy push on agent's run.succeeded / run.failed (unchanged)
  ```

- **Two notification surfaces, deliberately distinct:**
  - `notify` keeps its **best-effort ntfy pings** on run outcomes (`run.*`) — the
    short "your run finished / failed" signal, including the failure case where
    the agent never got to compose a message.
  - `gmail` carries the **at-least-once full-text email** (`notice.posted`) — the
    agent's actual report.

---

## 1. The `gmail` service

- **New, dedicated, deployable service** (joining `cron` as a recent addition),
  **not** folded into `notify`. The original idea of widening `notify` into a
  full-mailbox sender was rejected once full read/send/delete access was in scope:
  full mailbox access is its own domain, far larger than a push service needs.
- **Structurally `dropbox`'s twin.** `dropbox` is *"an external-OAuth connector
  that keeps a local mirror in sync via a loopback daemon + event-plane
  producer."* `gmail` is the same shape pointed at Google mail: an external-OAuth
  connector that **polls the mailbox and emits change events**. The dropbox
  service is the template to mirror (token source, client, MCP surface, producer
  wiring).
- **It generalizes the platform's trigger sources.** `cron` emits *time* events;
  `gmail` emits *mail* events. An `agent` session can be triggered by either via
  the same `session_triggers` seam — so "run this agent when mail matching X
  arrives" becomes possible for free later (not in scope now, but the reason the
  producer half matters).

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
- **MCP surface:** `list` / `read` / `send` / `trash` / `delete` message tools,
  for the owner (and, under a future agent-as-MCP-client track, the agent).

### Producer half — "emit events on changes"

- **Change detection via the Gmail History API**, not real-time push.
  `users.history.list(startHistoryId=cursor)` returns `messagesAdded` /
  `messagesDeleted` / label changes since the stored `historyId`. The service
  holds **`historyId` as its sync cursor** — exactly analogous to `cron`'s
  `last_slot` and a consumer's `feed_offset`.
- **Per change, in one transaction:** `outbox.Append` the event **and** advance
  the stored `historyId` (same atomic "emitted == recorded as emitted" pattern as
  cron's tick worker). Producer wiring mirrors crm/ledger/cron: `outbox.SchemaSQL`
  migration, `Spec.Feed = "/feed"` + `Spec.Producer`, **static** `Spec.Events`.
- **No Cloud Pub/Sub / `users.watch`.** Real-time Gmail push needs a public
  webhook + a GCP broker, which directly contradicts the suite's operating bet
  ("no broker, accept scheduled downtime"). **Polling is the deliberate choice.**
- **Accepted downtime trade-off** (same flavor as cron's no-catch-up): Gmail
  retains history for only ~a week. If `gmail` is down longer than that, the
  stored `historyId` goes stale and `history.list` returns 404 → the service does
  a **full resync and misses per-message events for the gap**. This is the
  best-effort philosophy applied to mail.
- **Events emitted — start minimal.** `mail.received` first (payload
  `{id, thread_id, from, subject, snippet, received_at}`). `mail.sent` /
  `mail.deleted` are deferred until a consumer needs them.

### Scheduled half — "run on schedules" (cron-consumed)

- **`gmail` consumes a `cron.<name>` tick and polls once per tick.** The poll
  cadence is therefore **owner-tunable at runtime via the crontab** (`*/5 * * * *`,
  hourly, etc.) with no redeploy. This is `cron`'s second consumer, proving the
  primitive generalizes beyond `agent`.
- Chosen over an internal interval daemon (the dropbox style) so scheduling stays
  centralized in `cron` and tunable without a code change. The trade-off: if
  `cron` is down, `gmail` stops polling — the same accept-downtime bet already
  embraced everywhere; the next poll's History diff covers the gap.
- A manual `sync_now` MCP tool (on-demand poll) is a cheap later add, not a
  decision to make now.
- `gmail` is thus simultaneously a **consumer** of `cron` (poll trigger) and of
  `agent` (`notice.posted`, §3) and a **producer** of `mail.*` — three roles on
  one service. appkit supports multiple consumer loops + a producer with no
  exclusivity assumption (already exercised: `notify` runs two consumer loops,
  `agent` is consumer + producer).

---

## 2. `agent`: the "notify the owner with my own exact text" tool (Gap 1)

- **A new agent-facing tool, `notify_owner(subject, body)`**, lets a running
  session emit an owner-facing message **with the agent's exact text** — distinct
  from the run-outcome events, which only carry the task name (the "agent saying
  I'm done" signal). This is the explicitly-wanted capability: the agent *decides*
  to send and controls *exactly what*, rather than the runner shipping whatever
  the final assistant message happened to be.
- **Mechanism = filesystem harvest, keeping `agentkit` pure** (chosen over
  injecting a domain sink into `agentkit`'s `Dispatch`). The agent loop runs
  in-process inside `agent`, but `agentkit`'s tools are deliberately pure,
  filesystem-confined ops with no DB/outbox handle. So `notify_owner` **writes the
  notice to a reserved sink inside the sandbox root** (e.g. `.outbox/notices.jsonl`);
  after `agent.Run` returns, the runner reads that sink and `outbox.Append`s one
  `notice.posted` event per notice — alongside the terminal `run.*` outcome Append
  it already does. Zero change to `agentkit`'s `Dispatch` signature; `agentkit`
  stays domain-free; `agent` remains the only thing touching its own DB.
  - Trade-off: notices flush at **end-of-run** (batched), not the instant the tool
    is called. Acceptable for a research report. (Real-time per-notice emission is
    the rejected "inject a sink into Dispatch" option; revisit only if needed.)
- **The agent stays sandboxed.** It gains no network egress and no MCP-client
  reach to sibling services. Emitting an event is its only new outbound effect.

### The `notice.posted` event

- **New static event type, source = `agent`.** Kept separate from
  `run.succeeded` / `run.failed` (not a widening of the outcome payload) so a
  consumer filters at the type level: `notice.posted` → email; `run.*` → ntfy.
- **Payload `{subject, body, session_id, session_name, trigger_event,
  scheduled_for}`** — the agent's exact `subject`/`body` plus the run context.
  Recipient is implicit (the box owner); no recipient routing this phase.
- Wired as a second static entry in `agent`'s producer `Spec.Events` (the producer
  role already exists from event-triggering P8).

---

## 3. Delivery: event-driven, `gmail` consumes `notice.posted` (Gap 2)

- **The agent emits, `gmail` sends** (the "(ii) event-driven" choice, taken over
  "(i) agent-as-MCP-client"). `gmail` adds `agent` to its consumed sources and
  subscribes to `notice.posted`; the handler sends the email via the Gmail API
  (`users.messages.send`, a base64url RFC-2822 message), **from and to the owner's
  own account**. So the research report lands in the owner's inbox (and Sent).
- **At-least-once delivery, not best-effort.** Unlike the ntfy push, a Gmail send
  failure returns a **plain error** from the handler → the fixed consumer engine
  (P1) stalls and replays from the cursor. A weekly report is worth retrying; a
  rare duplicate email on replay beats a silently-lost report. (A malformed
  `notice.posted` payload is semantic poison → `consumer.ErrSkip`, as everywhere.)
- **Why not (i) agent-as-MCP-client:** letting the sandboxed agent call sibling
  services' MCP tools directly would collapse both gaps into nothing and is the
  suite's eventual north star ("users connect an agent and work through tools") —
  but it's a large, security-sensitive change (egress + auth from inside the
  sandbox) that deserves its own design pass. (ii) ships the goal, stays uniform
  with the event mesh, and doesn't foreclose (i) later.

---

## 4. OAuth / credential bootstrap

- **Mint a fresh, dedicated `GMAIL_REFRESH_TOKEN`** (full `https://mail.google.com/`
  scope) — **not** reused from the existing `~/.secrets/hal-google-oauth.json`
  (a different project's credential, near-certainly without the mail scope).
  Explicit ownership, independent rotation, isolated blast radius; follows the
  uniform secret pattern (one `.envrc` line referencing `~/.secrets/GMAIL_REFRESH_TOKEN`,
  read from the environment at the composition root).
- **Reuses the existing `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET`** OAuth client
  (already in `~/.secrets`, already used by `dashboard/internal/googleidp` for
  "Sign in with Google"). The client's consent screen must have the full mail
  scope added, with the owner as a **test user**.
- **OAuth client stays in "Testing" publishing status.** A restricted scope
  normally triggers Google's CASA security assessment — but **only to publish to
  general users**. Single-tenant (owner is the only user, on their own box) means
  Testing + owner-as-test-user uses the restricted scope with **no assessment** —
  just a one-time scarier consent screen.
- **One-time consent flow**, scripted as a `! <command>` human-in-the-loop step
  (mirrors however dropbox's refresh token was obtained): an offline-access
  consent that prints the refresh token for the owner to drop into
  `~/.secrets/GMAIL_REFRESH_TOKEN`. The value never enters the agent's context.
- **No metaspot / AWS / SES / DKIM change at all.** The earlier SES design (its
  own domain identity, DKIM in the `int/` account, an `ses:SendEmail` IAM policy)
  is fully dropped — sending *as the owner via Google* needs none of it.

---

## 5. The originating example, concretely

- **cron schedule** `research-weekly` = **`0 10 * * 0`** — Sunday 05:00 US Central,
  pinned UTC. cron evaluates in UTC (event-triggering decisions §2); 05:00 CDT =
  10:00 UTC. **DST drift accepted:** fires 05:00 in summer (CDT), 04:00 in winter
  (CST). A weekly digest does not need DST exactness; per-schedule timezone is the
  documented future cron upgrade if it ever does.
- **agent session** "weekly research" with trigger `cron.research-weekly`
  (event-triggering `session_set_trigger`). On the tick it runs research,
  synthesizes a report, and calls `notify_owner(subject, body)` with the report.
- `agent` emits `notice.posted` → `gmail` consumes → emails the report to the
  owner. If the run instead fails, `notify` still sends its ntfy `run.failed` ping.

---

## 6. Sequencing and deferrals

### Sequencing

- **The `ralph` → `agent` rename lands FIRST, as its own isolated, mechanical
  phase, before any of this work.** (Already reflected in the top-level CLAUDE.md.)
  All gmail/notice work is designed against `agent` / `source: "agent"` from line
  one — no freshly-written code retrofitted, and `notify`'s single `feed_offset`
  cursor row keyed on the old source is migrated once by the rename, before
  `gmail` becomes a second consumer of agent's feed.
- **Build order:** (1) the `agent` `notify_owner` tool + `notice.posted` producer
  event; (2) the `gmail` service — connector + History-API producer + cron-consumed
  poll + `notice.posted`→send consumer; (3) deploy plumbing (`opsctl setup gmail`,
  manifest, nginx fragments, `VERSION`/`go.work`) and the one-time token bootstrap.
  Decomposition into subagent-sized phases is the follow-up plan doc's job.

### Deferred / out of scope

- **Agent-as-MCP-client** (the (i) path): the sandboxed agent calling sibling
  services' MCP tools directly (read/triage/delete mail mid-run, send directly).
  Its own future track.
- **Mail-triggered agent sessions** — wiring `gmail`'s `mail.received` into
  `session_triggers` so a session runs on incoming mail. The producer half makes
  it possible; not built this phase.
- **`mail.sent` / `mail.deleted` events**, richer mail payloads, recipient routing
  / multiple recipients, and any channel-abstraction beyond type→channel.
- **Per-user / multi-account Gmail** (a token store keyed by owner, the dashboard
  grants story) — single owner refresh-token secret for now.
- **Real-time Gmail push** (`users.watch` + Pub/Sub) — rejected, see §1.
- **`sync_now` manual-poll MCP tool** — cheap later add.

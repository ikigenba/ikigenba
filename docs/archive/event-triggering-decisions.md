# Event-Triggering Decisions

A record of decisions made in a design discussion about giving the suite
**scheduled / event-driven triggering** — concretely, a `cron` service that
publishes named time events, `prompts` running sessions in reaction to them, and
`notify` announcing the outcome. This file records **decisions only**, not a
plan. Detailed planning happens in future sessions on top of these.

Where a decision corrects or supersedes existing docs (`event-protocol.md`,
`event-plane-decisions.md`), that is called out; those docs are **not yet
amended** — doing so is future work.

Date of discussion: 2026-06-06. **Revised 2026-06-06** in a follow-up planning
session that turned these decisions into implementation detail and, in the
process, **simplified prompts** (dropping the durable-intent model for in-memory
fire-and-run) and pinned the previously-open items (the consumer skip signal,
the cron publishes seam, outcome event names). Revisions are marked inline.

---

## 0. Framing

- **Enhancement approach: drive from missing functionality.** "I want X and
  can't" justifies each change. Build for the named gap, not hypothetical
  futures. Treat friction where an X doesn't fit cleanly as a signal about the
  structure, not something to paper over. The suite is primitive and will pivot;
  decisions here are expected to evolve.
- **Originating gap.** Be able to create a scheduled research task in `prompts`
  (run periodically, synthesize, notify the owner), and be able to watch prompts
  session logs live. This surfaced three missing pieces: durable triggering,
  outbound comms, and live log following.

---

## 1. The event-plane consumer model (correction)

- **The shared `consumer` engine's "commit the cursor regardless of what the
  handler returned" behavior was an over-broad assumption from a prior session,
  not the intended design.** It is to be corrected. (This contradicts the
  current framing in `event-protocol.md` §11.2 / `eventplane/consumer` package
  docs, which present best-effort as the engine's model.)
- **Intended design: the handler's return value gates the cursor.**
  - `nil` → advance (commit the cursor).
  - `error` → do **not** commit; the same event is re-delivered before any later
    one (the §10 in-order stall). This is exactly the at-least-once,
    controlled-side model already specified in `event-protocol.md` §10.
- **Best-effort is a handler choice, not an engine policy.** A best-effort
  handler swallows its external failure and returns `nil` (this is how `notify`'s
  ntfy push stays best-effort).
- **Poison / unprocessable messages advance the cursor.** A message that can
  never succeed (unparseable envelope/payload) must not stall the consumer
  forever. The explicit typed "skip" signal ("log but advance") is preferred over
  relying on every handler remembering to return `nil`, because conflating
  "skip" with "error" is exactly the distinction that was lost before.
  - **Skip signal = a sentinel error, `consumer.ErrSkip`, matched with
    `errors.Is`** (revised). The `Handler` signature is unchanged
    (`func(ctx, ev) error`); the three outcomes are: `nil` → advance;
    `consumer.ErrSkip` (possibly wrapped) → log loud + advance; any other error →
    stall. The default-on-unknown-error is therefore **stall** (the safe
    at-least-once direction) — a handler must opt in to loss. The engine's own
    unparseable-*envelope* skip (detected before the handler runs) stays as-is;
    `ErrSkip` is for *semantic* poison a handler discovers after parsing.
- **Stall mechanism = replay from the last committed cursor.** On a handler
  error the consumer tears down the connection and reissues its request with the
  prior (committed) cursor; the producer serves it statelessly. Backoff engages
  on a handler-error reconnect so a persistently-failing handler does not
  hot-loop.
  - **Backoff rule** (revised): the reconnect curve resets on *progress* (a
    connection that committed at least one event) and only **engages** on a
    **no-progress** stall (a reconnect that re-fails the same event having
    committed nothing). So a transient blip after a long healthy run retries
    fast, while a genuinely stuck handler climbs to the 30s cap. Self-correcting,
    no extra state beyond a `committedAny` flag on the attempt result.
- **Engine-level dedup table stays deferred.** Consumers that need
  exactly-once-effect dedup in their **own domain state**, not via a generic
  engine dedup table. (Our work-bearing consumers are either idempotent — `wiki`
  — or tolerate a rare duplicate effect — `prompts`, per the in-memory model in
  Section 3.)
- **Handler audit is required as part of the fix:**
  - `notify` — poison → skip/advance; push effect stays best-effort (`nil`).
  - `wiki` — its existing fetch/ingest error returns *become* correct
    at-least-once retries under the fixed engine; poison → skip; a genuinely
    terminal/gone fetch → skip. **This also fixes a real latent bug:** today wiki
    silently drops a file from the index on a transient fetch/ingest failure.
    - **Concrete classification** (revised): the fetch layer (`httpFetch`) owns
      the split, where the HTTP status is in scope — a **404/410/409** ("gone")
      wraps `consumer.ErrSkip`; **5xx, transport errors, and everything else**
      stay a plain error → stall+retry. The handler stays dumb and just
      propagates. Line-level: the malformed-payload decode → `ErrSkip`; the
      `Ingest` failure → plain error (this is the bug-fix path); empty content
      stays a silent `nil` (a valid empty file is not poison).
  - `notify` — one line: the malformed `contact.created` decode → `ErrSkip`. The
    push itself is already a detached goroutine returning `nil`, so best-effort is
    untouched.

### Producer / cursor ownership

- **The producer is stateless about consumer position.** It never stores or
  remembers a consumer's cursor.
- **There is never an ack of any kind from a consumer — only a future request.**
  The cursor presented on each request is the producer's sole signal; the
  producer serves that request from that cursor like any other and waits for
  nothing.
- **The consumer fully owns its cursor** — both storage (`feed_offset` lives in
  the consumer's own DB) and the advance/replay decision (via the handler
  return). The engine is dumb plumbing running inside the consumer's process.
- **Transport stays SSE.** "One connection = one request": the cursor is
  presented once per GET (`Last-Event-ID`) and that GET streams many events
  forward; replaying is simply the consumer's next GET carrying the prior cursor.
  There is no special "drop" mechanic on the producer.
- **Per-consumer high-water-mark tracking / retain-until-committed is
  postponed.** The producer keeps no persistent confirmed-read mark for now;
  retention stays the blunt time/row horizon. (This is the deferred
  `event-protocol.md` §11.3 upgrade; the required `X-Consumer-Id` seam already
  exists for it.)

---

## 2. The `cron` service

- **New, dedicated, deployable service** (an 8th app), not folded into the
  dashboard. It is a **programmable scheduled event emitter** with an MCP
  "crontab" interface.
- **MCP surface = a crontab:** create / list / update / delete named schedules,
  each `{name, expr}`.
- **`expr` is 5-field cron syntax** (`minute hour day-of-month month
  day-of-week`).
  - **Vixie day-of-month / day-of-week semantics:** when **both** DOM and DOW are
    restricted (neither is `*`), fire when **either** matches (OR).
  - Operators: `*`, a number, lists `a,b`, ranges `a-b`, steps `*/n` and
    `a-b/n`. (Field names like `mon`/`jan` are a possible later add.)
  - **Hand-rolled matcher, no dependency** (in the spirit of the hand-rolled SSE
    parser). Validate/parse at MCP create time; fail loudly on malformed input.
  - **Evaluated in UTC.** Agents convert local time → UTC when setting a
    schedule. (Per-schedule / box-level timezone is a possible later add.)
- **Chosen over a simplified enum.** The original `hourly/daily/weekly/monthly`
  idea was dropped: ad-hoc anchoring (minute-of-hour, day-of-week, day-of-month,
  time) does not generalize cleanly across periods, whereas cron syntax already
  solves it. Raw cron syntax at the machine boundary is fine because the agent is
  the client and does the translation. `minutely` is **not** offered.
- **Stateless minute-match firing (real-cron semantics).** A worker wakes each
  minute and, for each schedule, emits if `matches(expr, now)`. No `next_fire_at`
  / `last_fired_at` scheduling state.
  - **No catch-up.** If cron is down across a matching minute, that fire never
    happens; on restart it resumes matching from the current minute. (This is
    vixie cron, not anacron.)
  - A small per-schedule "last emitted slot" guard prevents double-emitting the
    same minute on a restart-within-the-minute (belt-and-suspenders; consumer
    dedup would also absorb it).
- **Emitted event:**
  - **Type = `cron.<name>`.** cron owns the `cron.*` type namespace. The period
    is **not** encoded in the type (cron expressions have no single period; the
    earlier "period in the type" idea is dropped).
  - **Payload = `{name, scheduled_for, fired_at}`.** `scheduled_for` is the
    matched slot; `fired_at` is the actual emit time (they diverge after a
    restart blink — the signal a consumer uses for staleness).
- **Subscriber-blind producer.** cron serves a `/feed` and knows nothing about
  who listens, about sessions, or about payloads beyond the above. "Wire things
  up to listen" = a consumer subscribes to `cron.<name>` (or `cron.*`).

### cron implementation detail (revised — pinned in planning)

- **Schema = one `crontab` table.** `name` is the PRIMARY KEY (it *is* the
  identity and the `cron.<name>` suffix), constrained to event-type-safe chars by
  a DB CHECK: `CHECK (name GLOB '[a-z0-9-]*' AND name <> '')`. Columns:
  `name, expr, created_at, updated_at, last_slot`. `last_slot` (minute-truncated
  RFC3339) is the per-schedule double-emit guard; it is **not** scheduling state
  and is **not** cleared on an `expr` update.
- **Tick worker wakes aligned to the wall-clock minute boundary** (not a 60s
  interval timer), computes `slot = now.Truncate(minute)` in UTC, and for each
  schedule where `matches(expr, slot)` and `slot != last_slot`: in **one
  per-schedule transaction**, `outbox.Append` the event *and* `UPDATE last_slot`
  (same tx = "emitted" and "recorded as emitted" are atomic), then a single
  `Ring()` after the minute's scan. MCP create/update/delete races are handled by
  SQLite single-writer serialization — no extra locking.
- **The matcher** parses `expr` to a validated form at MCP create/update time
  (authority; fail loud, naming the bad field), stores the raw string, and
  re-parses/caches at tick. Vixie "restricted" = the field token is not literally
  `*` (so `*/2` counts as restricted for the DOM/DOW OR-rule). Numeric `7` is
  accepted as Sunday; field-*name* aliases (`mon`/`jan`) stay deferred.
- **Dynamic published types via a live provider** (revised). The producer
  reflection seam becomes symmetric with the existing consumer `Subscriptions`
  provider: a new `Spec.Publishes func() outbox.Registry` (and `rt.Publishes()`),
  preferred by the reflection tool when set. cron implements it by reading the
  crontab at reflection time, so reflection reports the **live** `cron.foo`,
  `cron.bar`, … each with the one shared payload shape. Static producers (crm,
  ledger) keep the static `Spec.Events` and never set `Publishes`.
- **Append-time validation: none for cron** (empty validation registry). The
  type is valid by construction (the tick worker emits `"cron."+name` from a
  charset-CHECKed row), so the boundary guarantee lives at the DB CHECK, not a
  per-emit registry guard — zero change to the shared registry's matching. An
  agent discovers *which* `cron.*` names exist via the crontab `list` MCP tool;
  event reflection reports shapes/live types, the crontab reports instances.

---

## 3. `prompts`: event-trigger processing

- **prompts gains both roles** (it was neither producer nor consumer): it
  **consumes** cron's feed and **produces** outcome events.
- **A session declares an event trigger:** `event: cron.<name>`. This activates
  prompts' already-designed-but-deferred `trigger=event-subscribe` seam. The
  session↔event linkage lives in **prompts**, not in cron's payload. Multiple
  sessions may share one `cron.<name>`.
- **prompts consumes cron's feed via the fixed consumer engine** (Section 1).

### Trigger declaration

- **A session's trigger lives in a separate 1:1 `session_triggers` table**, keyed
  by `session_id` (PK), not nullable columns on `sessions` — triggering is an
  attached capability, most sessions are manual. Columns: `trigger_event`,
  `max_staleness_secs`, `max_attempts`, timestamps; indexed on `trigger_event`
  for the event→sessions fan-out query. One trigger per session for now (the PK
  relaxes to a composite later if a session ever needs to fan in from multiple
  events).
- **Set via a dedicated `session_set_trigger` / `session_clear_trigger` MCP
  tool**, not folded into `session_create` — lets a schedule be attached/detached
  on an existing session and keeps `session_create` unchanged.

### Run model: in-memory fire-and-run (revised — simplified)

The earlier draft chose a **durable-intent model ("Option B")**: a `run_intents`
occurrence table, dedup on `(session_id, trigger_event, scheduled_for)`, a drain
worker, run-to-success durability, and a crash-recovery sweep. **That is dropped
as over-built for the named gap.** The simpler model:

- **The consumer handler starts the run(s) in-process and returns `nil`** (cursor
  advances). On a `cron.<name>` event it fans out over every subscribing session
  (`SELECT … FROM session_triggers WHERE trigger_event = ?`) and starts a run for
  each via the existing `session_run` path.
- **No new persistent tables.** No `run_intents`, no occurrence/dedup unique
  constraint, no `next_attempt_at`. The existing `runs` table records each
  execution as it already does; per-session serialization rides the existing
  `session.status` (idle/running) invariant.
- **Retry on failure is in-memory only**: the in-process goroutine runs →
  on failure `sleep(fixed_delay)` ("read and wait", a configurable constant —
  **not** exponential backoff) → retry, capped at a **small attempt count**
  (default 3). No durable retry state.
- **Staleness guard at receipt** (kept): if `scheduled_for` is older than the
  session's `max_staleness`, skip that session — start nothing, advance the
  cursor, emit **no** failed event. Evaluated **per-session** in the handler.
  This also **coalesces** a replay storm after long downtime down to the freshest
  occurrence, for free.
- **Crash semantics (the accepted trade-off):** a crash loses the in-memory
  run/retry state, and the cron event was already consumed (cursor advanced), so
  **that occurrence is missed** — the next cron tick recovers. No sweep, no
  re-drive. On rare crash-replay a duplicate run is possible and tolerated.
  (`max_staleness` bounds how stale a replayed occurrence can be before it is
  skipped instead.) This is the best-effort philosophy applied to prompts; if
  paying twice for an LLM run ever proves painful, a single dedup constraint can
  be added later.

### Outcome events

- **prompts emits two terminal events** (source = `prompts`), each in the **same
  transaction as the run's terminal state write** (at-most-once per run):
  - **`run.succeeded`** on success.
  - **`run.failed`** on terminal failure (so notify can announce failures too).
  - *(Revised: `succeeded`, not `completed` — it matches prompts' existing
    `runs.status` vocabulary `running|succeeded|failed|cancelled`. `run.*`, not
    `session.*` — the event is a run outcome, not a session lifecycle change.)*
- **Two types, not one `run.finished` + a `status` field**, so a consumer can
  filter at the type level (§7.3 grain).
- **Outcome payload (minimal for now):**
  `{session_id, session_name, trigger_event, scheduled_for, error?}` — `status`
  is **dropped** (the type encodes it); `name`→`session_name` (the human-readable
  task name; the cron identity is already in `trigger_event`); `error` present
  only on `run.failed`. The full report stays in prompts (read via MCP); the
  payload is widened in the future email phase.
- **prompts-as-producer wiring** mirrors crm/ledger: an `outbox.SchemaSQL`
  migration in prompts' DB, `Spec.Feed = "/feed"` + `Spec.Producer`, a **static**
  `Spec.Events` registry for the two compile-time-known types (not the live
  `Publishes` provider — that is cron-only), and an nginx `/feed` fragment. prompts
  is thus simultaneously a consumer (cron) and a producer (outcomes); appkit
  supports both roles plus `Workers` with no exclusivity assumption (verified).

---

## 4. `notify` (this phase only)

- **Minimal extension only.** `notify` adds `prompts` to `Consumes`
  (`["crm", "prompts"]`), subscribes to `run.succeeded` / `run.failed`, and fires
  its existing **best-effort ntfy push** for each, keeping its existing
  `crm/contact.created` push.
- **Two upstreams = two consumer loops.** notify runs one `consumer.Run` worker
  per upstream (crm, prompts), each with its own `feed_offset` cursor row — not one
  multiplexed connection.
- **No email, no channel abstraction, no recipient routing, no delivery retry**
  in this phase.

---

## 5. Scope and deferrals

### In scope for the phase these decisions describe

1. The event-plane consumer engine fix (Section 1) + the `notify`/`wiki` handler
   audit.
2. The `cron` service (Section 2).
3. prompts event-trigger processing: consume cron, in-memory fire-and-run with
   in-memory retry, produce outcome events (Section 3).
4. The minimal `notify` push extension (Section 4).

**Build order** (revised — agreed in planning): 1 → 2 → 3 → 4, in that sequence.
Each step ships and is independently testable. The engine fix is the prerequisite
for everything; cron must exist before prompts can consume it; prompts must emit
outcomes before notify can push them.

These four steps are decomposed into nine subagent-sized, strictly-sequential
phases (P1–P9) in `docs/event-triggering-plan.md` — that file is the build plan;
this one remains the decision record.

### Deferred / out of scope

- **notify outbound-comms expansion** — email (Amazon SES, which needs a
  `metaspot` infra change: domain verification / DKIM for `ikigenba.com`),
  channel abstraction, recipient routing, delivery retry. Separate later phase.
- **Per-consumer high-water-mark / retain-until-committed retention** (the
  `event-protocol.md` §11.3 upgrade, keyed on the existing `X-Consumer-Id`).
  Postponed; the blunt time/row horizon stays for now.
- **Engine-level dedup table** — consumers dedup in domain state instead.
- **prompts durable-intent machinery** — the `run_intents` occurrence table, the
  occurrence-key dedup constraint, the drain worker, run-to-success durability,
  and the crash-recovery sweep (the original Section 3 "Option B"). Dropped in
  favor of in-memory fire-and-run; revisit only if a missed occurrence or a
  duplicate LLM run proves painful in practice.
- **Live log viewer** — an operator-facing live follower (modeled on the prior
  `ralph-logs` project: glob-tail with rotation handling), adapted to the suite
  (SSE rather than WebSocket; pretty-render prompts' stream-json run logs rather
  than raw bytes). Independent track; not in this phase.
- **Amending `event-protocol.md` / `event-plane-decisions.md`** to reflect the
  Section 1 correction (best-effort is a handler choice, not the engine model).
  Future work.

# Event-Triggering Implementation Plan

The phased build plan for the event-triggering work. **Decisions** are recorded
in `docs/event-triggering-decisions.md` (the source of truth — read it first);
this file turns those decisions into a sequence of **subagent-sized phases**.

Each phase is a single coherent unit of work that one subagent can complete in
one context: it compiles, its tests pass, and it is independently committable.
Phases are executed **strictly in the listed order**; each builds on the
committed output of the prior ones.

## Sizing principle

A phase is scoped so that one subagent, starting fresh, can hold the relevant
files, do the work, and verify it within a single context budget. Three phases
(**P2, P3, P9**) are deliberately small because each carries a *distinct
verification surface* worth isolating — not because the work is trivial. If a
coarser cut is ever wanted, the natural merges are P2→P1, P3→P5, P6→P5
(collapsing 9 phases to 6).

## Dependency chain

```
P1 ──▶ P2
 └───▶ P3 ──▶ P4 ──▶ P5 ──▶ P6
                      └──────┴──▶ P7 ──▶ P8 ──▶ P9
```

P1 is the prerequisite for everything. cron (P3–P6) must exist before prompts
(P7–P8) can consume it; prompts must emit outcomes before notify (P9) can push
them. Executing the phases in numeric order satisfies every edge.

---

## Step 1 — consumer engine fix + handler audit

### P1 — Consumer engine fix
*Decisions §1. The prerequisite for the whole phase.*

- Add the `consumer.ErrSkip` sentinel (matched with `errors.Is`); handler
  signature unchanged.
- Make the handler's return value gate the cursor: `nil` → advance;
  `ErrSkip` (possibly wrapped) → log loud + advance; any other error → **stall**.
- Stall = tear down the connection and reconnect from the last committed cursor
  (new stop sentinel, e.g. `errStopHandlerStall`).
- Add `committedAny` to `attemptResult`; new backoff rule — the reconnect curve
  resets on *progress* and engages only on a **no-progress** stall (replaces
  "reset on any 200").
- Update `eventplane/CLAUDE.md` and the `consumer` package docs (they currently
  describe the old best-effort-engine model).
- Update / extend `consumer_test.go`.

**Touches:** `eventplane/consumer/consumer.go` (`handleFrame`, run loop,
`attemptResult`), `eventplane/consumer/*_test.go`, `eventplane/CLAUDE.md`.
**No handler changes in this phase.**
**Verify:** `go test ./eventplane/consumer/...`.

### P2 — Handler audit (notify + wiki) *(small)*
*Decisions §1 "Handler audit". Depends on P1.*

- `notify` (`internal/push/push.go`): malformed `contact.created` decode →
  `ErrSkip`. Push stays a detached goroutine returning `nil` (best-effort
  untouched).
- `wiki` (`internal/consume/consume.go`): in `httpFetch`, 404/410/409 ("gone") →
  wrap `ErrSkip`; 5xx / transport / everything else → plain error (stall+retry).
  Handler stays dumb and propagates. Malformed decode → `ErrSkip`; `Ingest`
  failure → plain error (**the latent-bug fix** — today it silently drops the
  file); empty content stays a silent `nil`.

**Verify:** `go test ./notify/... ./wiki/...`.

---

## Step 2 — the `cron` service

### P3 — appkit `Publishes` reflection seam *(small, shared-lib blast radius)*
*Decisions §2 "Dynamic published types via a live provider". Depends on P1.*

- Add `Spec.Publishes func() outbox.Registry` and `rt.Publishes()`, symmetric
  with the existing consumer `Subscriptions` provider.
- The reflection tool prefers `Publishes()` when set; static producers (crm,
  ledger) keep `Spec.Events` and never set it.

**Touches:** `appkit/appkit.go` (Spec), `appkit/server/server.go`
(`rt.Publishes()`), the reflection tool, `appkit/feed/*` as needed, + tests.
**Verify:** `go test ./appkit/...` **and confirm all 7 existing services still
build** (shared `replace`d library — this is the reason the seam is isolated).

### P4 — cron scaffold + matcher
*Decisions §2 + "cron implementation detail". Depends on P3.*

- New module: `cmd/cron/main.go`, `internal/db` with the migration for the
  `crontab` table (`name` PK with `CHECK (name GLOB '[a-z0-9-]*' AND name <> '')`;
  columns `name, expr, created_at, updated_at, last_slot`), and a CRUD store.
- The **hand-rolled 5-field cron matcher** (no dependency): parse + validate +
  `matches(expr, t)`. Vixie DOM/DOW OR when both restricted ("restricted" = token
  not literally `*`, so `*/2` is restricted); operators `* N a,b a-b */n a-b/n`;
  numeric `7` = Sunday; UTC; no `minutely`; no field-name aliases yet.
- Wire the module into `go.work`; add `cron/VERSION`.

**Verify:** exhaustive matcher tests (field parsing, DOM/DOW OR, steps, ranges,
lists, 7-as-Sunday) + a `GOWORK=off` static build. The matcher is the
algorithmic heart and gets a dedicated phase.

### P5 — cron MCP + tick worker + producer
*Decisions §2. Depends on P4.*

- MCP crontab surface: create / list / update / delete `{name, expr}`; parse +
  validate at create/update (authority; fail loud, naming the bad field); store
  raw.
- Tick worker: wake aligned to the wall-clock minute boundary; `slot =
  now.Truncate(minute)` UTC; for each schedule where `matches(expr, slot)` and
  `slot != last_slot`, do **one per-schedule tx** (`outbox.Append` +
  `UPDATE last_slot`); one `Ring()` after the scan.
- Producer wiring: `outbox.SchemaSQL`, `Spec.Feed = "/feed"` + `Spec.Producer`;
  implement `Spec.Publishes` by reading the crontab → live `cron.foo`,
  `cron.bar`. Append-time validation: none (empty registry; valid by
  construction + DB CHECK). Emits `cron.<name>` with payload
  `{name, scheduled_for, fired_at}`.

**Verify:** unit tests + local run observing an emit on the feed.

### P6 — cron deploy plumbing
*Decisions §2 (new-service plumbing). Depends on P5. Different file surface.*

- `opsctl setup cron` case; `etc/manifest` entry; nginx fragment (prod +
  dev-mirror under `nginx/`); confirm `VERSION` / `go.work`; note the
  dashboard-restart-to-re-read-manifests step.

**Verify:** nginx local `./run` routes `/srv/cron/`; `opsctl status`. (Some
checks are on-box and may only be partially verifiable locally.)

---

## Step 3 — `prompts` event-trigger processing

### P7 — prompts triggers + consumer
*Decisions §3 "Trigger declaration" + "Run model". Depends on P5/P6.*

- `session_triggers` table (1:1, PK `session_id`; columns `trigger_event,
  max_staleness_secs, max_attempts`, timestamps; index on `trigger_event`) +
  migration.
- New MCP tools `session_set_trigger` / `session_clear_trigger` (not folded into
  `session_create`).
- Consume cron's feed via the fixed engine. Handler = **in-memory fire-and-run**:
  on `cron.<name>`, `SELECT session_id FROM session_triggers WHERE
  trigger_event = ?`, start a run per session via the existing `session_run`
  path, return `nil`. In-memory retry (fixed `sleep(delay)`, **not** exponential;
  cap default 3). Per-session staleness guard at receipt (`now - scheduled_for >
  max_staleness` → skip that session, no run, no event). **No new persistent
  tables** beyond `session_triggers`; reuse `runs`; serialize via
  `session.status`.

**Verify:** tests + local run reacting to a cron event.

### P8 — prompts as producer (outcome events)
*Decisions §3 "Outcome events". Depends on P7.*

- Producer wiring mirroring crm/ledger: `outbox.SchemaSQL` migration in prompts'
  DB, `Spec.Feed = "/feed"` + `Spec.Producer`, **static** `Spec.Events` for the
  two compile-time types, nginx `/feed` fragment.
- Emit `run.succeeded` / `run.failed` (source `prompts`) in the **same tx as the
  run's terminal state write** (at-most-once per run). Payload
  `{session_id, session_name, trigger_event, scheduled_for, error?}` (`error`
  only on failed). Touches `runner`.

**Verify:** tests + observe an outcome emit on prompts' `/feed`.

---

## Step 4 — `notify` extension

### P9 — notify push for prompts outcomes *(small)*
*Decisions §4. Depends on P8.*

- `Consumes: ["crm", "prompts"]`; subscribe `prompts/run.succeeded` +
  `prompts/run.failed`; keep `crm/contact.created`.
- **Two `consumer.Run` worker loops** (crm, prompts), each its own `feed_offset`
  cursor row — not one multiplexed connection.
- Best-effort ntfy push for each outcome. No email / channels / routing / retry
  this phase.

**Verify:** `go test ./notify/...` + local push.

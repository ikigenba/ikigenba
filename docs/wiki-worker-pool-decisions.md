# wiki integration execution model — dispatcher-free worker pool (decisions)

> Status: **in-progress decision log**, captured from a design detour off the
> main wiki redesign walk. It **revises the dispatch/concurrency model** in
> `wiki-redesign-decisions.md`: it replaces the single central **dispatcher
> goroutine + channel signaling** with **N identical self-selecting workers**
> over a mutex-guarded in-memory in-flight set. The headline finding is that
> the two are the *same design under two concurrency idioms* — every concrete
> objection to the worker model reduced to "where the in-flight set lives" —
> so the choice was made on **legibility, not correctness or performance**,
> both of which were shown equivalent. When the main walk is folded into a
> proper `<slug>-design.md`, the parent's dispatcher sections (listed under
> "What this supersedes") are rewritten per this doc.

## The decision

**Adopt N identical worker goroutines that self-select work, in place of one
dispatcher goroutine that hands work to a dumb pool.** Each worker runs the
same loop: under a single mutex, pick the highest-priority eligible unit of
work and claim it in an in-memory in-flight set; release the lock; run the
appropriate integrator; commit; on completion, remove the claim. The workers
are homogeneous — no special decider role exists.

Basis for the choice: the central-dispatcher model and the worker-pull model
are the same design expressed two ways —

- **central dispatcher**: the mutable selection state (the in-flight set) is
  *confined to one goroutine*, which is why it needs no locks; coordination is
  via channel signals (the Go-canonical CSP idiom, "share memory by
  communicating").
- **identical workers**: that *same* selection state is *shared behind one
  mutex*; coordination is the lock itself.

The mutex-guarded selection critical section **is** the single dispatcher,
re-expressed as a lock. They are contention-equivalent and crash-equivalent
(see items below). The worker model was preferred because identical behavior
is easier to reason about and verify, and it removes the signaling apparatus
the team found hard to follow — aligning with the project principles
(*explicit over implicit*, *favor mechanical verification*, *simplicity
through discipline*).

What stays exactly as the parent doc has it: the worker **pool size**
(`WIKI_INTEGRATION_WORKERS`, default 4), optimistic page-version commit, the
alias-`UNIQUE` duplicate-minting guard, the shared conflict loop (cap 3,
`conflicts` counter on `runs`), stamp-by-id-list for digests, and
arrival-order integration explicitly forfeited. Only the *who decides what
runs* layer changes.

## Item 1 — at-most-one-in-flight is a `TryLock` per job, not central enforcement

The parent doc enforces "at most one in-flight run per batch-table entry name"
in the single dispatcher (one map lookup, no race because one goroutine). In
the worker model this is a **non-blocking `TryLock` keyed by the work item**:

- **Digests and lint jobs → key = job name** (`crm-digest`, `lint-dups`).
  This is where the rule actually bites: their work is chosen by a *shared
  selector*, so two concurrent runs of the same entry would read the same
  pending rows before either stamps. A worker tries the lock; if held, that
  job "is not a current option" and the worker moves on. Different job names
  run concurrently (selectors partition — see item 7).
- **Document pass → key = inbox row id**, NOT the integrator. A single lock
  for the whole document pass would serialize the very pool we built to run
  documents concurrently. The dedup is per-row: many document passes run at
  once, the guard only stops two workers grabbing the *same* row.

Both are "tryLock on a key"; workers stay identical and compute the key from
the work item's kind. This is the in-flight set re-expressed as locks; it
costs one mutex where the dispatcher cost zero, and a mutex is trivial.

## Item 2 — crash-resume and the in-progress mark

**Requirement (held firm): a crash + restart must leave the database in a
state that resumes correctly.** Met by a single rule already in the parent
design — *stamp only at commit, never at claim*.

There are three row states, but only **two are durable**:

| state | durable? | representation |
|---|---|---|
| pending | yes | `integrated_by = ''` (and not dead, not ineligible) |
| done | yes | `integrated_by = <run id>`, written **only inside the end-of-run commit** |
| in progress | **no** | membership in an in-memory in-flight set (RAM only) |

Because "done" is stamped only inside the end-of-run transaction (atomic with
the page/registry writes), a crash can only ever leave a row **pending**.
There is no durable "in progress" to misinterpret. The partial run wrote
nothing (all writes live in that one closing transaction), so restart simply
re-selects the still-pending row. **Crash-resume is automatic and needs no
cleanup** — the DB only ever records "not yet" or "done."

**Dedup is a separate, purely in-RAM concern** — and it is in RAM precisely so
a crash ignores it. Selection is the critical section; the run is not:

```
lock(inflight)
  row := SELECT id FROM inbox
         WHERE integrated_by='' AND dead_at IS NULL
           AND (ineligible_until IS NULL OR ineligible_until <= now)
         ORDER BY received_at
         -- walk results, skip any id already in `inflight`, take first free
  inflight.add(row.id)        -- (for digests: also TryLock the job name)
unlock(inflight)

run(row)                       -- minutes; concurrent across workers; no lock held

lock(inflight); inflight.remove(row.id); unlock(inflight)   -- on completion
```

Two workers cannot grab the same row because select-plus-claim is one critical
section. The set is RAM-only; a crash wipes it; restart begins empty and the
DB is the sole truth.

**The durable `running` row in `runs` is kept — but for accounting, not dedup
or resume.** It is the status record (the MCP poll resolves through it), the
provenance key, and — critically — how *process death* gets counted: a clean
failure marks its own run `failed`, but a process that dies mid-run writes
nothing, so the **boot sweep flips orphaned `running` → `crashed`**, and a
crashed run counts as one attempt toward `WIKI_RUN_ATTEMPTS_MAX`. Without it, a
document that reliably crashes the process would retry forever and never
dead-letter. This durable row does **not** gate re-selection (selection keys
off `integrated_by` + the in-memory set), so the boot sweep reconciles
*accounting*, not *resume* — and it is exactly the "crash/restart ignores the
mark" the requirement asked for: an orphaned `running` row is explicitly
converted to `crashed` so nothing mistakes it for live work.

## Item 3 — SQLite contention is identical; no distinguishing argument

Once the claim is in-memory (item 2), the per-run **DB write footprint is
identical** in both models:

| write | central dispatcher | identical workers |
|---|---|---|
| insert `running` runs-row at start | 1 per run | 1 per run |
| end-of-run commit (pages + registry + stamp + run row) | 1 per run | 1 per run |
| dedup claim | none (in-goroutine set) | none (in-memory set) |

Selection is a *serialized* critical section in the worker model (one worker
selects at a time under the mutex) — the same one-at-a-time selection the lone
dispatcher does, so it is not even N concurrent select-reads. Under WAL, reads
don't block the writer regardless. The only real write contention in *either*
model is N workers' **commits** occasionally queuing on SQLite's single writer
— milliseconds-long, "never while an LLM thinks," a non-event at this scale
(commits are ms, runs are minutes, the writer sits ~idle). Both need WAL +
`busy_timeout` equally.

Conclusion: **there is no SQLite contention argument that distinguishes the
two models.** (The earlier worry assumed DB-based row claims, which items 1–2
eliminated.)

## What the worker model must re-home (not free, but not messy)

The central dispatcher centralized a few jobs the worker model must place
explicitly. None reintroduce locks beyond the single in-flight-set mutex.

- **Idle-wait / wake.** Workers block when there is no eligible work and wake
  on the same three events the dispatcher's `select` watched: a new arrival
  (`Accept`), a run completing (frees a row id or a job `TryLock`), and an
  `ineligible_until` timer expiring. One `sync.Cond` (or a shared channel)
  broadcast on those events is the worker-model analog of the select. The
  worker model thus *keeps* the design's "signals decide when to look, the
  table decides what exists" property — the broadcast is a latency
  optimization over polling; correctness rests on the DB.
- **Cron-row stamp = a completion-time join, done worker-locally.** No single
  run owns a cron row, so its stamp cannot ride one commit. The worker
  finishing a bound digest run queries "do all bound entries for this
  `caused_by` now have a *succeeded* run?"; if yes, it stamps the cron row with
  `UPDATE inbox SET integrated_by=… WHERE id=? AND integrated_by=''`. The
  `WHERE integrated_by=''` makes a double-stamp race (two workers finishing the
  last two entries at once) a harmless no-op. This is identical to the
  dispatcher's "stamp once all bound runs succeeded," run as a query at
  completion instead of from central tracking.

## Digests add no significant complication

The cron row lives as a **normal pending row** — in the DB it is just
`integrated_by = ''` (its in-flight-ness is RAM membership, like any row).
This makes the crash story uniform: crash mid-digest → cron row still pending →
restart re-picks it → bound digests re-run → the already-locked **re-fire
idempotency** drains any event rows the crashed run had already stamped (cheap
empty runs). Two genuine differences from a document, both being the parent
design's rules executed worker-locally:

1. **A cron row authorizes one-or-more runs (a tiny fan-out), not one.** The
   grabbing worker looks up the bound job entries for the trigger (config,
   every worker has it) and runs each bound digest. Each digest run is its own
   `runs` row with `caused_by = cron-row-id` and stamps its **event rows by
   id-list** at commit (unchanged).
2. **The cron row is stamped by the all-succeeded join** (above), not by a
   single commit.

Interaction with the item-1 `TryLock`: if a worker can't acquire an entry's
lock (another cron row's run of the same entry is in flight — e.g. boot finds
yesterday's and today's cron rows), it does not run that entry now, cannot
satisfy the join, and so **leaves the cron row pending for a later wake** —
verbatim the parent's "the skipped cron row just stays pending; durable
authorization waiting is its designed behavior." When re-picked, the other run
has drained that entry's selector → cheap empty run succeeds → join satisfied →
stamp. Re-fire idempotency again.

Failure is uniform: a bound run **fails** → no commit, event rows unstamped,
join unsatisfied, cron row unstamped → the failure path sets `ineligible_until`
on the **cron row** (`caused_by`) → re-fire after the delay (drained entries
are cheap) → `WIKI_RUN_ATTEMPTS_MAX` failures dead-letter the cron row. This is
exactly "Batch failures — a failed run blocks the cron row's stamp," with zero
new machinery in the worker model.

**Open sub-question (not yet decided):** the claimable unit for digests —
- *Framing 1*: a worker grabs the **cron row** (one claim, like any row) and
  runs all its bound entries sequentially. Simplest selection (rows are rows);
  matches the "cron row is just another claim" mental model; needs the
  leave-pending-if-locked-out handling above.
- *Framing 2*: the claimable unit is a single **`(cron-row, entry)` pair**, so
  two workers naturally run two entries of one trigger concurrently and the
  stamp is a pure completion-time join with no leave-pending case. Costs a
  selection step that expands cron rows into entry candidates.

Both are correct and contention-equivalent. Framing 1 matches the simpler
mental model; Framing 2 recovers per-trigger entry concurrency. Decide at
build (or when the main walk resumes).

## Event consumption semantics (confirmed)

- **A digest sweeps all *pending* rows its selector matches**, at any grain:
  `source LIKE 'crm:%'` (a whole service), `source = 'crm:contact.created'`
  (one event type) — plain SQL over the inbox row. It sweeps the rows pending
  *at the moment compile reads them* (stamped by id-list at commit); rows that
  arrive mid-run stay pending and sweep next cycle.
- **Exactly one integrator per content row.** `integrated_by` is a single
  `TEXT` run id — no array, no many-to-many. A content row is consumed by one
  run, **permanently** (stamps never cleared; re-integration rejected).
- **Two integrators never share an event — because of selector *partition*,
  not the stamp.** Two distinct mechanisms, two jobs:
  1. **Partition (routing guarantee):** selectors are required to partition the
     event rows; **boot refuses to start on overlap.** So two digests never
     *target* the same row — routing is deterministic and intended (crm events
     go to crm-digest, not "whichever ran first").
  2. **Stamp (once-only guarantee):** a set `integrated_by` removes the row
     from all future *pending* queries, so the same digest doesn't re-sweep it
     and concurrent/re-fire runs of that entry find it drained. The stamp makes
     consumption *final*; it does not arbitrate between *different* integrators.
- **Partition must also be covering** for any consumed source: a source matched
  by no selector would sit pending forever, so boot **surfaces**
  consumed-but-unmatched sources (no overlaps *and* no gaps).
- **Cron rows are the exception to "consumed by an integrator":** no run
  consumes a cron row as content — it is *authorization*, stamped by the
  all-bound-runs-succeeded join, not a content sweep.

## What this supersedes in `wiki-redesign-decisions.md`

These parent sections describe the central-dispatcher idiom and are revised by
this doc (substance preserved, mechanism re-homed to identical workers):

- **Process topology** — "one dispatcher goroutine — single select over
  channels." → N identical workers + a `sync.Cond`/channel broadcast wake.
- **Dispatch — the decision loop** — the dispatcher's numbered check. → the
  per-worker selection critical section under the in-flight-set mutex (same
  priority: cron before documents, oldest-first, eligibility predicate).
- **Stamping — who clears `integrated_by`** — the dispatcher's after-stamp for
  cron rows. → the worker-local all-succeeded join.
- **Concurrency — the worker pool** — "the dispatcher stays one goroutine … it
  alone starts runs"; "at most one in-flight run per batch-table entry,
  dispatcher-enforced." → workers self-select; at-most-one-in-flight is a
  `TryLock` per job name. (Optimistic commit, alias-UNIQUE guard, conflict
  loop, stamp-by-id-list, forfeited ordering — all unchanged.)
- **Failure policy → Backoff (`ineligible_until`)** — the dispatcher's "fourth
  wake source, a timer armed at sleep time." → the workers' timer/`Cond` wake.
- **Failure policy → Batch failures** — "the dispatcher stamps a cron row only
  when all bound runs succeeded." → the same rule as the worker-local join.

Unchanged and still authoritative in the parent: acceptance/inbox, the document
and digest pipelines (extract/compile → resolve → merge → commit), optimistic
commit, the failure policy's *substance* (bounded retries + dead-letter,
`ineligible_until`/`dead_at`/`requeued_at`, threshold, notification), lint,
search/ask, and all schema riders.

# BUG: `opsctl rollback` can silently lose events (epoch not re-minted)

**Status:** FIXED 2026-06-07. Found 2026-06-07 by probing, not previously known.
**Severity:** latent silent data-loss in the event plane. Narrow preconditions today; widens the moment a real consumer goes live.

## Resolution

Fixed by re-minting the epoch at the appkit restore **chokepoint**: `runRestore`
(the verb-dispatch entry point in `appkit/verbs.go`) deletes the generation
sidecar (`<db>.generation`) on **every** restore — covering both appkit's default
restore and any `Spec.Restore` override — so the next boot mints a fresh epoch and
the `stale-epoch` check fires. This makes `opsctl rollback` / the binary `restore`
verb safe with zero operator memory required. The plan and rationale are in
`docs/plan-rollback-epoch-remint.md`. The problem-statement body below is retained
as a description of the mechanism.

---

## The failure in one sentence

`opsctl rollback` (and the appkit binary `restore` verb) can rewind a producer's
event outbox **without re-minting the event-plane generation/epoch token**, so a
consumer that already advanced past the rewound point silently loses every event
the producer re-issues under the reused sequence numbers.

## Why that causes loss (the mechanism)

The event plane resumes a consumer by sequence number. Each feed cursor is
`<generation>.<seq>` (see `eventplane/outbox/cursor.go`). The `generation` (epoch)
token exists for exactly one job: detect when the producer's outbox has been
restored/rewound so that `seq` numbers get **reused**, and reject pre-rewind
cursors with a `resync: stale-epoch` instead of silently resuming onto the reused
seqs.

The generation lives in a sidecar file **outside** the DB
(`<db>.generation`, e.g. `crm.db.generation`) so a DB-file restore doesn't roll it
back. The protection only works if the restore **deletes that sidecar** so the
next boot mints a fresh epoch. If the sidecar survives, the epoch is unchanged,
the stale-epoch check never fires, and a forward consumer cursor is honored onto
reused seqs.

Concrete loss sequence (single box — multi-box is NOT required):

1. crm (producer) outbox is at seq 100; notify (consumer) has committed cursor
   `GEN_A.130` after consuming through 130.
2. A schema-advancing deploy is rolled back. `opsctl rollback` restores the
   pre-migration DB snapshot (seq 100) — **rewinding the outbox** — but leaves
   `crm.db.generation` untouched, so the live epoch is still `GEN_A`.
3. crm resumes and appends new, *different* events as seq 101..150.
4. notify reconnects with `GEN_A.130`. Generation matches (it was never
   re-minted), 130 < head 150, so the producer resumes at 131 and streams
   new-131..150.
5. **new-101..new-130 are never delivered. Silent loss.** No error, no resync.

## Why it's silent / not obvious

- The correct disaster-recovery path (`crm/bin/restore`) DOES delete the sidecar
  and re-mint — so "restore" *seems* handled.
- The gap is the **automated** path: `opsctl rollback` and the binary `restore`
  verb use appkit's default restore (`appkit/backup.go` `defaultRestore`), which
  deliberately leaves the generation sidecar in place. The two restore paths
  diverge on this one behavior.
- It was documented only as a packaging TODO in `crm/CLAUDE.md` ("the S3 workflow
  + epoch re-mint isn't folded into `Spec.Backup` yet"). That sentence describes
  the *tooling*, not the *failure mode*, and is unreadable without this context —
  so the data-loss consequence was effectively undocumented.

## Preconditions (all must hold for actual loss)

1. A live consumer holding a forward cursor on the producer's feed. Today the only
   real consumer is **notify**; confirm whether it is deployed and consuming at
   `int` before assessing exposure.
2. A rollback/restore that actually **rewinds the outbox DB** — i.e. across a
   schema-advancing deploy (a plain binary-only rollback leaves the DB untouched →
   no rewind → no hazard).
3. The producer re-issuing new events that climb back **above** the consumer's
   committed seq before the consumer would otherwise reconnect.

Miss any one and there is no loss. So this is a narrow latent bug right now, not
an active fire — but precondition (1) is satisfied as soon as notify (or any
consumer) goes live, which makes it urgent to fix before then.

## Where the relevant code lives

- `appkit/backup.go` — `defaultRestore`: replaces DB + clears WAL/SHM, **does not
  touch the generation sidecar**. This is the path `opsctl rollback` uses.
- `eventplane/outbox/outbox.go` — `loadOrMintGeneration`: mints a fresh epoch only
  when the sidecar file is absent/empty.
- `eventplane/outbox/cursor.go` — cursor `<generation>.<seq>` format and parse.
- `crm/bin/restore` — the CORRECT path: deletes `<db>.generation` (re-mints) after
  replacing the DB. `crm/bin/backup` deliberately excludes the sidecar.
- `crm/cmd/crm/main.go` — `appkit.Spec{...}` with **no `Restore` override**, so crm
  inherits the unsafe default for the binary verb.
- `crm/CLAUDE.md` — the packaging-TODO note that under-describes this.

## Fix direction (for the next session to plan — not decided here)

The principle: **any restore that rewinds the outbox must re-mint the epoch**, and
that guarantee must not depend on which tool the operator happens to run or on
anyone remembering this. Two candidate shapes (not exclusive):

- **Close the gap:** make the outbox-rewinding restore path (appkit
  `defaultRestore` / `Spec.Restore` / a new `opsctl restore` verb) delete the
  sidecar itself, so `opsctl rollback` re-mints automatically.
- **Fail loud at the call site:** if a rollback would rewind the outbox, `opsctl`
  refuses or warns at the prompt rather than silently proceeding.

Prefer the option that requires zero operator memory (close the gap) over the one
that relies on reading a doc at the right moment.

# Phase 164 — Replay-safe apply: ensure only unstaged units, one transaction, job-handle attribution

*Realizes design Decision 4 (ingest pipeline), the apply slice. Depends on Phase 163.*

The handoff phase becomes an internal `phase` column on `jobs`
(`bin/create-migration wiki add-job-phase`, `'' | 'extract' | 'compile'`),
replacing `waiting` as a durable status; the boot sweep requeues only unphased
`working` jobs and leaves phased ones to the inbox tick. The existing tests
tagged R-K73J-J3W3 and R-KFMU-7I2Y are updated to the phase vocabulary as part
of this phase; their behavior statements in D4 already read that way.

Extract apply ensures compile items **only** for units with no staged compile
row. `Ack` deletes the queue row, so blanket re-ensuring on replay mints fresh
paid items without bound; staging-row presence is the durable guard that makes
extract replay free.

The compile apply becomes a single transaction: upsert the unit's payload,
recount the job's staged compile rows filtered to `stage='compile'`, and when
the count reaches `units`, integrate, mark the job `done`, clear staging, and
release the lease — then ack. The count is always recomputed, never incremented.

Every prompts call a job causes is submitted under that job's id as its group
handle, so an ingest's calls are retrievable by the handle the caller was given.

**Done when:** these ids are covered by clearly-named tests and the suite is
green:

- R-OQJZ-K337 — replaying a done extract item after some units are staged
  ensures items only for the unstaged units, and none when all are staged
- R-OVFL-361Z — a failure injected between the unit's staged write and the
  terminal write leaves no staged row for that unit and the job non-terminal
- R-OWNH-GXSO — querying recorded prompts calls by the job id returns that
  ingest's extract and compile calls

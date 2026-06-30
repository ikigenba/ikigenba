# Design — S3-only backups, one backup / one restore

> **Status: normative target.** This doc defines how backup, restore, and
> rollback are *supposed* to work once the tooling is brought into compliance.
> Parts already work this way (the box-level S3 path); parts are the target
> (deleting the per-binary verbs, routing deploy/rollback through opsctl). The
> [What changes](#what-changes) section is the compliance backlog, not steady
> state.
>
> Authoritative implementation: `opsctl/internal/opsctl/backup.go`,
> `deploy.go`, `rollback.go`. When code and this doc disagree, that is a
> divergence to fix — change the design here deliberately, then bring code to it.

## The core principle

> **A backup is only a backup if it is off the box. Anything on local disk is
> transient staging on its way to S3 — never a backup of record.**

Everything below falls out of that one rule. There is **one** way to create a
backup and **one** way to restore one; both live in `opsctl`; both treat S3 as
the only durable store. No per-service backup logic, no special naming, no
local retention.

## Context — what we are replacing

There were two backup mechanisms, and only one obeyed the principle.

1. **`opsctl backup` / `restore`** (box-level, S3 — the reference).
   Stops the unit, tars `state/` into a `/tmp` workdir, ships it to
   `s3://<IKIGENBA_BACKUP_BUCKET>/<app>/snapshots/<UTC-ts>.tar`, writes an
   `<app>/latest` pointer, prunes to the most recent 30, deletes the local tar.
   The local archive is *already* transient staging — this mechanism already
   obeys the principle. Run nightly by `ikigenba-backup.service`.

2. **`<app> backup` / `restore`** (per-binary verb, appkit — the mistake).
   `VACUUM INTO` a single `.db` file. Deploy used it to write a **local-only**
   `backups/pre-<version>.db` before a schema-advancing migration; rollback read
   it back from local disk. This file was never shipped — the *only* backup that
   escaped S3, and the entire violation of the principle.

The per-binary verb was a wrong split: backup/restore need **zero** of the
binary's compiled-in knowledge — they move `state/` bytes and talk to S3, which
is opsctl's whole job. (Contrast the verbs that *must* stay in the binary —
`serve`, `version`, `manifest`, `migrate` — each acts on something embedded in
the binary: the server itself, the build stamp, the binary's own `Spec`, its
embedded migrations. opsctl cannot do those.)

## Decisions

### 1. backup/restore live only in opsctl

The binary verb set shrinks to **`serve / version / manifest / migrate`** (plus
the read-only `schema` introspection seam opsctl calls). The appkit
`backup`/`restore` verbs are **deleted**, along with the `Spec.Backup` /
`Spec.Restore` per-service override hooks — that override mechanism was the one
genuinely per-service thing in this area, and it is exactly the wrongness being
removed.

### 2. Backups are S3-only; local disk is transient staging

There is no retained local backup. The `backups/` tier disappears from the
on-box layout (see [app-layout impact](#impact-on-app-layout)). The only local
artifact is the throwaway tar in a `/tmp` workdir, created and `RemoveAll`'d
within a single backup call — exactly as `backupState` already does.

### 3. One uniform namespace — no special naming

Every backup is the same kind of object: a timestamped tar of `state/`. There
are **no** `pre-migration/`, `pre-restore/`, or version-keyed namespaces. A
backup carries no metadata about *why* it was taken; a nightly backup and a
pre-deploy backup are byte-identical in kind and indistinguishable except by
timestamp. (The existing `latest` pointer stays — it is not a backup, it is the
bare-box disaster-restore entry point. The apex cert snapshot, below, is the one
genuinely distinct *content set*, not a distinct *kind* of state backup.)

### 4. Every deploy backs up first — unconditionally

A deploy takes a backup before doing anything else, **whether or not it advances
the schema**. This replaces today's conditional "back up iff the schema
advances" step (`deploy.go:158-179`). The unconditional rule is what makes
rollback's guarantee hold (next decision): after any deploy there is *always* a
fresh backup of the pre-deploy state, with no condition to reason about.

### 5. Rollback = restore the most recent backup + swap the binary

Rollback needs no pointer, no naming, and no snapshot introspection. Because
every deploy creates a backup first, **the most recent backup is guaranteed to
exist and to be the pre-deploy state**. Rollback restores it and swaps `bin/run`
back to the prior binary. Nothing is stored mapping version → snapshot; nothing
needs to be.

This also subsumes the event-plane epoch re-mint that the deleted appkit
`restore` verb used to own: opsctl's restore already deletes the `.generation`
sidecars (`removeGenerationFiles`, `backup.go:287,323`), forcing a re-mint on
next boot. The concern is covered without the binary verb.

### 6. Scope — rollback is the *immediate* undo of the deploy you just made

"Most recent backup == pre-deploy state" holds only until another backup
supersedes it. Two cases fall outside the scope, and both degrade **loudly**,
never silently:

- **A nightly backup lands between the deploy and the rollback.** The most
  recent backup is now post-migration. Restoring it under the old binary trips
  the forward-only downgrade guard, which refuses to boot. The window is small
  (deploys are undone in minutes; nightly is once a day) and the failure is
  safe.
- **Delayed rollback.** A deploy that ran for days (several nightlies) is not
  "rolled back" — the most recent backup is long past the pre-deploy state.
  Reverting to an old version after the fact is a **forward deploy of an older
  version**, a different operation, not rollback.

The downgrade guard is the system that enforces correctness here, so the
simplicity of "restore the most recent backup" costs nothing in safety: a wrong
restore fails at boot with a clear error instead of corrupting data.

### 7. The apex cert snapshot folds fully into opsctl

The dashboard's Let's Encrypt cert tree (`/etc/letsencrypt/{archive,renewal,live}`)
is a distinct content set, not part of `state/`, so it keeps its own
`<apex>/cert/<ts>.tar` + `<apex>/cert/latest`. This already lives in opsctl
(`backupCert`, `restoreLatestCert` in `backup.go`); deleting the binary
`Spec.Backup` override that *also* did it simply removes the redundant second
copy. Cert handling is opsctl-only, like everything else.

## What changes

Two call sites stop shelling the deleted per-binary verbs and call opsctl's own
backup/restore instead:

- **`deploy.go` step 4** — replace the conditional
  `<binary> backup --out backups/pre-<version>.db` (lines 158-179) with an
  **unconditional** `o.Backup`/`backupState` call before migrate.
- **`rollback.go` step 2** — replace
  `<targetBin> restore --from backups/pre-<from>.db` (lines 59-71) with a
  restore of the most recent S3 snapshot.

Deletions:

- `appkit/backup.go` (`defaultBackup`, `defaultRestore`, `copyFile`) and the
  `backup`/`restore` cases + `runBackup`/`runRestore` dispatchers in
  `appkit/verbs.go`.
- The `Spec.Backup` / `Spec.Restore` hooks and any service overrides.
- `Layout.PreMigrationBackup` / `Layout.BackupsDir` and the `backups/` tier in
  `layout.go`; the matching prune of `backups/pre-*.db`.
- The dashboard's `Spec.Backup` cert override (now redundant with `backupCert`).

## Impact on app-layout

`docs/app-layout.md` lists `backups/` as a Local (`/var`) tier folder ("local
deploy scaffolding"). That tier **disappears**: the Local tier becomes `state/`
(the only thing backup captures, the only S3-backed set) and `cache/`
(transient). Update the layout doc's Local-tier table row and any prose that
calls `backups/` a retained local set — under this design nothing retained ever
lives on the box.

## Non-goals

- Reconstructing arbitrary historical state from S3 beyond the 30 retained
  snapshots.
- Rolling back a deploy after subsequent backups have superseded its
  pre-deploy snapshot (that is a forward deploy of an older version).
- Any backup that does not ship to S3.

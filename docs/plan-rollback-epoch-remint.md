# PLAN: re-mint the event-plane epoch on every appkit restore

Fixes `docs/bug-rollback-epoch-remint.md` — `opsctl rollback` / the binary
`restore` verb can rewind a producer's outbox without re-minting the
generation/epoch sidecar, silently losing re-issued events.

## Root cause (confirmed by reading the code)

The plumbing is already complete; one line is missing.

- `opsctl rollback` drives the DB restore through the **target binary** *on
  purpose* so a producer re-mints its epoch — see the comment at
  `opsctl/internal/opsctl/rollback.go:60-61` ("drive restore through the TARGET
  binary so a producer re-mints its event-plane generation (Spec.Restore)").
- The restore verb resolves and plumbs the sidecar path all the way into the
  hook: `appkit/config/config.go:49` derives `GenerationPath` (`<db>.generation`,
  always non-empty), and `appkit/verbs.go:299` copies it into `RestoreReq`.
- **But** `appkit/backup.go` `defaultRestore` replaces the DB and clears
  `-wal`/`-shm` and then *deliberately ignores* `req.GenerationPath`
  (backup.go:57-59). And `crm/cmd/crm/main.go` declares **no `Spec.Restore`**, so
  crm inherits that unsafe default. The sidecar survives → epoch unchanged →
  `checkCursor` (`eventplane/outbox/outbox.go:278`) never returns `stale-epoch` →
  a forward consumer cursor resumes onto reused seqs.

So the rollback path's stated intent ("the binary re-mints") is silently false
for every producer that uses the default restore — which is all of them today.

## Decision: close the gap at the appkit chokepoint (zero operator memory)

The bug doc prefers "close the gap" over "fail loud at the call site," and
`principles` prefers deleting a divergence over a per-service shim. So:

**Re-mint the epoch in `runRestore` (the single dispatch chokepoint), after the
restore hook succeeds — for *both* the default and any `Spec.Restore` override.**

Rationale for putting it in the dispatcher rather than inside `defaultRestore`:

- A restore *by definition* replaces the DB = rewinds the outbox. There is no
  restore that should NOT re-mint. So the invariant "any restore re-mints" is
  unconditional and belongs at the verb boundary, not buried in one of two hooks.
- Putting it in `defaultRestore` only would leave a residual footgun: a future
  `Spec.Restore` override fully *replaces* `defaultRestore`, so its author would
  have to remember to re-mint. The dispatcher placement makes the guarantee hold
  no matter what hook runs.
- For a non-producer service the sidecar simply doesn't exist; the delete is an
  ignored `ErrNotExist` no-op. Safe universally.

This makes `opsctl rollback`, the binary `restore` verb, and any future
`opsctl restore` re-mint automatically. No per-service `Spec.Restore` is needed
and crm's `main.go` stays as-is.

## Changes

### 1. `appkit/verbs.go` — re-mint in `runRestore` after the hook

In `runRestore` (verbs.go:293), after the hook returns `nil`, delete the
generation sidecar so the next boot mints a fresh epoch:

```go
if spec.Restore != nil {
    if err := spec.Restore(context.Background(), req); err != nil {
        return err
    }
} else if err := defaultRestore(context.Background(), req); err != nil {
    return err
}
// Any restore rewinds the outbox: re-mint the event-plane epoch so every
// pre-restore cursor is rejected with `stale-epoch` instead of silently
// resuming onto reused seqs (docs/bug-rollback-epoch-remint.md). Absent sidecar
// (non-producer) is a no-op.
if req.GenerationPath != "" {
    if err := os.Remove(req.GenerationPath); err != nil && !errors.Is(err, os.ErrNotExist) {
        return fmt.Errorf("restore: re-mint epoch (remove %s): %w", req.GenerationPath, err)
    }
    fmt.Fprintf(stdout, "re-minted event-plane epoch (removed %s)\n", req.GenerationPath)
}
```

Add `errors` / `os` imports to verbs.go if not already present.

### 2. `appkit/backup.go` — correct the now-stale comment

`defaultRestore`'s comment (backup.go:57-59) claims the sidecar is "left
untouched … a producer that must re-mint supplies Spec.Restore." Replace it with
a note that the dispatcher (`runRestore`) owns the re-mint, so the default and
overrides are both covered. No behavioral change to `defaultRestore` itself.

### 3. `appkit/appkit.go` — tighten the `RestoreReq` doc

Update the `BackupReq`/`RestoreReq` comment (appkit.go:66-69) so it no longer
implies epoch re-mint is a `Spec.Restore` responsibility; it is the verb's.

## Tests

- **`appkit` (new, the regression test):** through `Main`/dispatch, run `backup`
  then `restore` on a temp DB with a pre-seeded `<db>.generation` sidecar; assert
  the sidecar is **gone** after restore (so the next boot re-mints). Mirror of
  `appkit/appkit_test.go:125` `TestDispatch_BackupThenRestore`. Add a second case
  with **no** sidecar present asserting restore still succeeds (ErrNotExist
  ignored).
- **`appkit` with a `Spec.Restore` override:** assert the dispatcher still removes
  the sidecar even when a custom hook ran (proves the chokepoint guarantee).
- **`eventplane/outbox` (already covers the consumer-facing half):** the existing
  `checkCursor` stale-epoch test confirms that a changed generation rejects the
  old cursor — no change needed, but cross-reference it.
- **`opsctl` end-to-end:** `TestSchemaAdvance_BackupAndRollbackRestores`
  (deploy_test.go:191) drives rollback through the fake app. Extend the
  `testdata/fakeapp` `restore` case so it touches a sidecar file, then assert the
  rollback removed it — proving the opsctl→binary→runRestore wiring re-mints.

## Docs

- `docs/bug-rollback-epoch-remint.md` — mark **Status: fixed** with a pointer to
  this plan / the commit.
- `crm/CLAUDE.md` — the "epoch re-mint isn't folded into `Spec.Backup` yet" note
  now over-states the gap for *restore*: appkit's default restore re-mints. Narrow
  the note to the remaining S3-backup packaging item only.
- `eventplane/CLAUDE.md` and `outbox.go:303` `loadOrMintGeneration` doc say "the
  consumer's bin/restore deletes the sidecar" — generalize to "any appkit restore
  re-mints (the restore verb removes the sidecar); the operator S3 `bin/restore`
  does the same for its out-of-band path."

## Validation

1. `GOWORK=… go test ./...` across `appkit`, `eventplane`, `opsctl` (workspace
   mode via `go.work`).
2. `go vet ./...` on the three modules.
3. Manual reasoning replay of the bug doc's 5-step loss sequence against the new
   path: step 2 (rollback restores pre-migration DB) now also removes
   `crm.db.generation`; step 4 (notify reconnects with `GEN_A.130`) now hits
   `generation != o.generation` → `reasonStaleEpoch`, consumer discards and
   resyncs instead of silently skipping new-101..new-130. Loss closed.

## Rejected alternatives

- **Add `Spec.Restore` to crm only.** Fixes crm but leaves ledger/dropbox/any
  future producer exposed and relies on each author remembering — exactly the
  "operator memory" the bug doc says to avoid.
- **Sidecar-delete inside `defaultRestore`.** Closes today's gap but a future
  `Spec.Restore` override silently reopens it. Dispatcher placement is strictly
  safer for the same cost.
- **Fail loud in `opsctl rollback`.** The doc's non-preferred option; relies on an
  operator reading a warning at the right moment and does nothing for the binary
  `restore` verb.

## Sequencing

`appkit` change + tests (1 commit) → doc updates (1 commit). `opsctl` fakeapp
test tweak can ride with the appkit commit or its own. No deploy/version bump:
`appkit`/`opsctl` are libraries/tooling, not versioned; the fix reaches the box
on the next normal `bin/ship` of each producer (rebuilds against the new appkit).

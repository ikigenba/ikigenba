# Phase 17 — The uploader worker (drain, echo-suppress, poison, health)

*Realizes design Decision 17, the uploader slice (R-KKM6-83XW, R-KLU2-LVOL,
R-KN1Y-ZNFA). Depends on phase 15 (queue store) and phase 16 (write client), and
on phase 13 (dir ops mapping to `CreateFolder`/`DeletePath`).*

Observable end state:

- `internal/dropbox/uploader.go` adds `Service.RunUploader(ctx)`, wired at the
  composition root through the `Spec.Workers` hook (clean shutdown on `ctx`
  cancel). The loop picks due rows (`DueUploads`), and for each **re-reads the
  mirror** (via streaming `Open`, phase 12) and calls the matching client op:
  `put` → `Upload(overwrite)`, `mkdir` → `CreateFolder`, `delete` → `DeletePath`,
  `move` → `Move`.
- **Echo suppression:** on a successful `put` the uploader persists the
  Dropbox-returned `rev` into the `files` row **atomically** with `ClearUpload`,
  so a later `list_folder` delta at that rev is a no-op on the existing
  rev/content_hash dedup (no re-download, no duplicate event).
- **Failure:** a failing op calls `FailUpload`, advancing `next_attempt_at` by
  exponential backoff and incrementing `attempts`; past the poison threshold the
  row is `state='failed'`, retained (never deleted), with `last_error` set.
- **Health:** the `Health` reporter's `details` gains `pending_uploads`,
  `failed_uploads`, and `oldest_pending_age_seconds` from the queue; the same
  reporter feeds `/health` and the `health` tool.

**Done when:** the suite is green (design Conventions commands, from `dropbox/`)
and:

- R-KKM6-83XW is covered by a test asserting a successful upload persists the
  returned rev and clears the queue row atomically, and a subsequent `list_folder`
  delta at that rev is a no-op (no re-download, no duplicate event) — driven with
  a fake client returning a known rev.
- R-KLU2-LVOL is covered by a test asserting a failing upload advances
  `next_attempt_at` (backoff) and increments `attempts`, and that past the
  threshold the row is `failed`, retained, with `last_error`.
- R-KN1Y-ZNFA is covered by a test asserting `Health.details` reports
  `pending_uploads`/`failed_uploads`/`oldest_pending_age_seconds` from the queue,
  through the shared reporter that feeds both `/health` and the `health` tool.

# Phase 56 — Publish root, domain half: the `path` column, `ValidatePath`/`Subtree`, and the mapped materializer

*Realizes design Decision 15 (data model — the `path` slice) and Decision 38
(the publish root — the validator, subtree mapping, and push-materializer
slice).*

The domain layer learns the publish root. A new additive migration (created
with `bin/create-migration sites <name>`) adds the `path` TEXT NOT NULL DEFAULT
`''` column, carrying every existing row forward at the repository root.
`internal/sites` gains: the `Path` field on `Site`; the `path` parameter on
`Store.Create` and the new `Store.SetPath` (bumps `updated_at`, touches nothing
else); `ValidatePath` (normalization + `ErrInvalidPath` per D38); and
`Subtree(entries, root)` (filter to the root, strip the prefix; identity for
`""`). The push materializer (D35's handler) maps every export through
`Subtree(entries, site.Path)` before its existing refusal checks and in-place
reconcile. Callers of the changed `Store.Create` signature are threaded through
with `""` so the build stays green; the MCP surface itself does not change in
this phase.

**Done when** the suite is green (design Conventions) and each id below is
covered by a clearly-named, genuinely-asserting test:

- R-3SOP-GMQL — the `path` column exists with the `''` default, its migration
  preserves pre-existing rows byte-identically, `Create` persists a passed path
  verbatim, and `SetPath` changes only the path (with `updated_at` advanced;
  `ErrNotFound` for a missing slug).
- R-3TWL-UEHA — `ValidatePath` normalizes `""`/`"/"`/slash-wrapped paths and
  rejects empty, `.`, `..`, and case-insensitive `.git` segments, backslashes,
  control characters, and over-length paths with `ErrInvalidPath`.
- R-42FW-ISO5 — the push materializer on a rooted site writes only the
  root-subtree entries, prefix-stripped, deletes local files absent from the
  mapped set, never writes an out-of-root entry anywhere under `SITES_ROOT`,
  records the pushed sha, and still refuses a whole export whose mapped set
  carries a `.git` segment.

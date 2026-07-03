# bin — cleanup findings

## High-priority (named migrations)
- none

Both named migrations are CLEAN in `bin/`:
- Deploy format → tar.gz: `bin/ship`, `bin/bump`, and `bin/deploy-doc.test.sh` all
  describe the current tar.gz-bundle model (versioned bundle scp'd to /tmp, opsctl
  stage unpack, three-symlink atomic swap). No flat-bin / single-binary-copy /
  per-binary-swap wording found.
- Service names → registry/: `bin/registry` resolves names from
  `etc/current/manifest.env`; `bin/start`/`bin/live-smoke.test.sh` hold hardcoded
  SERVICES lists but as dev-orchestrator operational config, not a naming
  source-of-truth claim that contradicts registry. (See Notes.)

## Other stale info
- bin/test:13 — references `docs/adr-migration-timestamps.md`; file now lives at `docs/archive/adr-migration-timestamps.md` (dead path)
- bin/test:15 — references `docs/plan-migration-timestamps.md`; now at `docs/archive/plan-migration-timestamps.md` (dead path)
- bin/check-migrations:7 — references `docs/adr-migration-timestamps.md` (dead path; now under docs/archive/)
- bin/check-migrations:27 — references `docs/adr-migration-timestamps.md` and `docs/plan-migration-timestamps.md` (both dead paths; now under docs/archive/)
- bin/new-migration:13 — references `docs/adr-migration-timestamps.md` (dead path)
- bin/new-migration:46 — references `docs/adr-migration-timestamps.md` (dead path)
- bin/new-migration:101 — references `docs/adr-migration-timestamps.md` (dead path)
- bin/new-migration:120 — references `docs/adr-migration-timestamps.md` (dead path)

(CLAUDE.md itself points at the moved location `docs/archive/adr-migration-timestamps.md`, confirming these bin/ references are stale.)

## Notes
- Top-level `registry/` currently contains only an in-design project workspace
  (`registry/project/` with design/plan docs, no built artifact). The
  registry-as-source-of-truth migration appears not yet landed, so `bin/registry`
  and the hardcoded SERVICES arrays in `bin/start:20` / `bin/live-smoke.test.sh:15`
  are still the working mechanism — flagged only as a future tension, not confirmed
  stale.

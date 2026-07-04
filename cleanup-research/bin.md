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

✅ **ALL RESOLVED 2026-07-03 (moot by prior deletion).** Re-checked against the
current tree: every finding below is gone. `docs/archive/` was deleted wholesale
(commit 84099d3), `bin/test` and `bin/check-migrations` no longer exist, and the
surviving `bin/create-migration` (renamed from `new-migration`) now carries **no**
`migration-timestamps` references at all (`grep -rn migration-timestamps bin/` →
none). Nothing to fix here.

- ~~bin/test:13 — references `docs/adr-migration-timestamps.md`~~ (file `bin/test` deleted)
- ~~bin/test:15 — references `docs/plan-migration-timestamps.md`~~ (file `bin/test` deleted)
- ~~bin/check-migrations:7 — references `docs/adr-migration-timestamps.md`~~ (script purged, 84099d3)
- ~~bin/check-migrations:27 — references both migration-timestamps docs~~ (script purged, 84099d3)
- ~~bin/create-migration:13/46/101/120 — reference `docs/adr-migration-timestamps.md`~~ (refs no longer present in the file)

## Notes
- **Updated 2026-07-03:** top-level `registry/` is now a built Go module (not just
  a design workspace), so the framing below is itself outdated. But `bin/registry`
  and the hardcoded SERVICES array in `bin/start` remain the working shell-side
  mechanism; having those `.sh` orchestrators consume the Go `registry` module is
  **registry-adoption work, deferred** (same call as nginx/run) — not a stale-info
  scrub. `bin/live-smoke.test.sh` no longer exists. Left as-is.

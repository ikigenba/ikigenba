# nginx — cleanup findings

## High-priority (named migrations)
- nginx/run:17 — hardcodes the service list `crm cron dropbox gmail ledger notify prompts scripts sites webhooks wiki` in a for-loop. Service inventory is now maintained in `registry/` (`registry.Services`, seeded in registry/project/plan/phase-02.md). This list duplicates and can drift from the registry: e.g. the registry seeds `github` (3203, Connectors) which is absent here. It is the registry-as-source-of-truth contradiction the migration warns about (though a future github/ frag would still be picked up by the `[ -f ]` guard, so the drift is latent).

## Other stale info
- nginx/nginx.conf:1 — header comment `# ~/projects/nginx/nginx.conf` and line 3 run hint `nginx -p ~/projects/nginx -c ~/projects/nginx/nginx.conf ...` reference a standalone `~/projects/nginx` path. This is a mono-repo subfolder (`nginx/`), not a top-level `~/projects/nginx`. (dead/misleading path)
- nginx/README.md:24 — "crm.conf etc. drop here in Phase 2b" phrased as future work; enforcement has landed (line 28 already says "enforcement landed"). Stale plan-phase phrasing. (obsolete phase reference)
- nginx/nginx.conf:76 — "Path-routed service fragments land here in Phase 2b" — same landed-Phase-2b forward phrasing for work that is done. (obsolete phase reference)
- nginx/README.md:28-34 — the "Map" section documents only `/`, `/_authn`, and the crm `/srv/crm/` paths; it omits `/_session-authn` and the `sites` PRIVATE static tier that nginx.conf:62-74 now implements. Incomplete rather than contradictory. (stale/incomplete map)

## Notes
- No flat-bin / single-binary / per-binary-swap deploy language exists anywhere in nginx/ — the tar.gz deploy migration has nothing to flag here. run:8-12 correctly frames fragment copying as "the dev mirror of each service's opsctl setup".
- Port scheme in README.md:32-34 (crm 3100) matches the current registry blocks (Apps 3100–3199), so it is NOT stale despite the CLAUDE.md context mentioning :3000–:3006.
- The `(When the service registry lands crm's port becomes 3100...)` note lives in crm/etc/nginx.conf, outside this folder's scope — not reported here.

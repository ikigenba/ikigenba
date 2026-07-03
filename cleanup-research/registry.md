# registry — cleanup findings

## High-priority (named migrations)
- none

Notes on why: `registry/` is a design/plan/build-loop workspace for a
**not-yet-built** leaf library (`project/README.md:8-14` — "There is no code in
`registry/` yet"). It *is* the intended source of truth for name→port, so it does
not contradict registry-as-source-of-truth — it defines it. It describes the
pre-registry world (ports in five places, hardcoded peer maps) only as the
**motivating problem it solves**, and its own product scope explicitly states it
does **not** migrate consumers (`project/product/product.md:51-54`), so those
descriptions are current, not stale. Nothing here describes the flat-bin deploy
model; the only deploy touch is `opsctl` reading `manifest.env`
(`project/design/D01.md:15-17`), which is orthogonal to the tar.gz bundle change.

## Other stale info
- none

## Notes
- Potential future drift (NOT stale today, but worth an eye): `project/product/product.md:16-22`
  and `:57-62` describe the current suite as having ports in ~5 places incl.
  "hardcoded peer maps inside the services that consume it (`notify`, `scripts`,
  `prompts`, `sites`)". This is accurate as motivation *because registry adoption
  is out of scope here*. If/when registry is actually built and adopted
  suite-wide, this problem statement becomes a historical description — but that
  is the plan's history to hold, not a current contradiction.
- The registry table (`project/design/D02.md:28-45`) proposes a NEW block-based
  numbering (dashboard 3000; core 3001-3006; crm 3100/ledger 3101; connectors
  3200+) that renumbers most services vs. the current flat 3000-3006 map implied
  by root `CLAUDE.md`. This is a deliberate design target, not stale info — but a
  future agent should not read it as "the ports services run on today."
- `github` at 3203 (`D02.md:44`) is a reserved name not in root CLAUDE.md's
  12-app list; intentional forward reservation, flagged only for awareness.
- `plan/` was skipped per scope rules; `bugs/`, `requests/`, `research/` contain
  only `.keep` placeholders.

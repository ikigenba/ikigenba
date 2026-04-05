# 2026-04-05 — add devlog skill to default skillset

## What
Added the `devlog` skill to `.claude/skillsets/default.json` preload list, and created this top-level `devlog/` directory to hold project-wide entries.

## Why these choices
**Preload, not advertise.** Devlog reading/writing is a baseline expectation for every session touching this repo (per `CLAUDE.md`: read relevant devlog entries before changing an environment, add a new entry when making meaningful changes). Something invoked on every session belongs in `preload`, not `advertise`.

**Top-level `devlog/` in addition to per-env ones.** `CLAUDE.md` describes per-subfolder devlogs under `bootstrap/`, `test/`, `prod/`. But changes to the repo as a whole — skillset composition, agent infra, cross-cutting conventions — don't belong to any single environment. A top-level `devlog/` gives those changes a home without polluting env-specific histories.

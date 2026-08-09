# prompts — Plan Status

This is the manifest: one line per **pending** phase in build order, the
**only** place a phase's `⬜` marker lives. Each phase line is a Markdown
bullet beginning `- Phase` and carries `⬜` (pending). The build loop finds its
next unit of work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
and reads only that phase's `project/plan/phase-NN.md`. On completion the build
loop **deletes** that phase's line and its `phase-NN.md` — there is no done
marker; done is gone. This file deliberately carries **no bare status glyph**
outside phase lines, so the anchored grep matches only phase lines.

Next phase: 70

- Phase 62 ⬜ realizes D51 — the name key: slug, schema, and uniqueness
- Phase 63 ⬜ realizes D53 — the write path commits: create, update, import, rename, delete
- Phase 64 ⬜ realizes D54 — seeding: the one-time definition backfill at boot
- Phase 65 ⬜ realizes D55, D39, D40 — the run workspace is a clone pinned to a sha
- Phase 66 ⬜ realizes D56 — the run token and the authenticated git door
- Phase 67 ⬜ realizes D57 — the framing prompt tells the run about its clone
- Phase 68 ⬜ realizes D58, D24 (slice) — `repos` joins the trigger sources
- Phase 69 ⬜ realizes D59 — retire the content columns

# Phase 2 — Route repos through the dev front door

*Realizes design Decision 2 (`run`: fragment regeneration) — the service-list
correction only.*

`nginx/run`'s literal `for svc in …` list gains `repos` (alphabetical, between
`prompts` and `scripts`), making it the fourteen path-routed services D2 now
states. Nothing else in the script changes: the copy loop, the skip-if-missing
posture, and the `mkdir -p tmp` line stay byte-identical. After this phase the
default `bin/start` path (which execs this script) serves `/srv/repos/` —
including the git smart-HTTP door — exactly as its port-offset path already
does.

**Done when:**

- `bash -n run` exits 0.
- `grep -E 'for svc in' run` prints one line containing `repos` between
  `prompts` and `scripts`.
- `grep -c 'repos' run` returns a count ≥ 1 (the name appears nowhere else in
  the script today, so the list edit is the only hit).
